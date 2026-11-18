#include "api.h"
#include "avatar.h"
#include "tokenstore.h"

#include <QBuffer>
#include <QClipboard>
#include <QDateTime>
#include <QDesktopServices>
#include <QDir>
#include <QFile>
#include <QFileInfo>
#include <QImage>
#include <QMimeData>
#include <QMimeDatabase>
#include <QStandardPaths>
#include <QUrlQuery>
#include <QGuiApplication>
#include <QJSEngine>
#include <QJsonDocument>
#include <QJsonObject>
#include <QNetworkReply>
#include <QProcess>
#include <QSettings>
#include <QUrl>
#include <QVariantList>

namespace {

constexpr int kTimeoutMs = 20'000;

QString trimServer(QString s)
{
    s = s.trimmed();
    while (s.endsWith('/'))
        s.chop(1);
    if (!s.isEmpty() && !s.contains("://"))
        s.prepend("https://");
    return s;
}

void fail(const QJSValue &callback, const QString &why)
{
    QJSValue cb = callback;
    if (cb.isCallable())
        cb.call({QJSValue(why)});
}

QString savedWorkspace(const QString &server)
{
    if (server.isEmpty())
        return {};
    return QSettings().value(QStringLiteral("workspaces")).toMap().value(server).toString();
}

void saveWorkspace(const QString &server, const QString &id)
{
    if (server.isEmpty())
        return;
    QSettings settings;
    QVariantMap map = settings.value(QStringLiteral("workspaces")).toMap();
    if (id.isEmpty())
        map.remove(server);
    else
        map.insert(server, id);
    settings.setValue(QStringLiteral("workspaces"), map);
}

QString savedPerson(const QString &server)
{
    if (server.isEmpty())
        return {};
    return QSettings().value(QStringLiteral("workspacePeople")).toMap().value(server).toString();
}

void savePerson(const QString &server, const QString &personId)
{
    if (server.isEmpty() || personId.isEmpty())
        return;
    QSettings settings;
    QVariantMap map = settings.value(QStringLiteral("workspacePeople")).toMap();
    map.insert(server, personId);
    settings.setValue(QStringLiteral("workspacePeople"), map);
}

// Server errors come back as plain text or {"error":"..."}; network errors as Qt's message.
QString errorText(QNetworkReply *reply, const QByteArray &body, int status)
{
    const QString text = QString::fromUtf8(body).trimmed();
    if (!text.isEmpty()) {
        if (text.startsWith('{')) {
            const QString msg = QJsonDocument::fromJson(body).object().value(QStringLiteral("error")).toString().trimmed();
            if (!msg.isEmpty())
                return msg;
        }
        if (status == 401 && text.compare(QStringLiteral("unauthorized"), Qt::CaseInsensitive) == 0)
            return QStringLiteral("Token recusado");
        return text;
    }
    if (status == 401)
        return QStringLiteral("Token recusado");
    if (status == 0)
        return QStringLiteral("Sem conexão com o servidor: %1").arg(reply->errorString());
    return QStringLiteral("Erro %1").arg(status);
}

} // namespace

namespace {
Api *g_api = nullptr;
}

Api *Api::instance()
{
    return g_api;
}

Api::Api(QObject *parent) : QObject(parent)
{
    g_api = this;
    m_net.setTransferTimeout(kTimeoutMs);
    m_server = QSettings().value("server").toString();
    m_token = tokenstore::load();
    m_workspace = savedWorkspace(m_server);

    m_liveRetry.setSingleShot(true);
    connect(&m_liveRetry, &QTimer::timeout, this, &Api::startLive);
    m_liveWatch.setSingleShot(true);
    m_liveWatch.setInterval(40'000);
    connect(&m_liveWatch, &QTimer::timeout, this, [this] {
        if (m_liveReply)
            m_liveReply->abort(); // finished() reconnects
    });
    connect(this, &Api::signedInChanged, this, [this] {
        if (signedIn())
            startLive();
        else
            stopLive();
    });
    if (signedIn()) {
        QTimer::singleShot(0, this, [this] {
            startLive();
            refreshMe();
        });
    }
}

// startLive opens the server's live stream, from the last event seen.
void Api::startLive()
{
    if (m_liveReply || m_token.isEmpty())
        return;
    QNetworkRequest req = makeRequest(m_server, "/v1/live?after=" + QString::number(m_liveAfter), m_token);
    req.setRawHeader("Accept", "text/event-stream");
    req.setTransferTimeout(0);
    m_liveBuffer.clear();
    m_liveReply = m_net.get(req);
    m_liveWatch.start();
    QNetworkReply *reply = m_liveReply;
    connect(reply, &QNetworkReply::readyRead, this, [this, reply] {
        if (reply != m_liveReply)
            return;
        if (reply->attribute(QNetworkRequest::HttpStatusCodeAttribute).toInt() == 200)
            readLive();
    });
    connect(reply, &QNetworkReply::finished, this, [this, reply] {
        reply->deleteLater();
        if (reply != m_liveReply)
            return;
        const int status = reply->attribute(QNetworkRequest::HttpStatusCodeAttribute).toInt();
        m_liveReply = nullptr;
        if (status == 401) {
            setLive(false);
            signOut();
            return;
        }
        liveLost();
    });
}

void Api::stopLive()
{
    m_liveRetry.stop();
    m_liveWatch.stop();
    if (QNetworkReply *r = m_liveReply) {
        m_liveReply = nullptr;
        r->abort();
    }
    m_liveAfter = 0;
    setLive(false);
}

// liveLost tries again soon, waiting longer after each failure (up to 15 s).
void Api::liveLost()
{
    m_liveWatch.stop();
    setLive(false);
    if (m_token.isEmpty())
        return;
    static const int waits[] = {500, 1000, 2000, 5000, 10000, 15000};
    m_liveRetry.start(waits[qMin(m_liveBackoff, 5)]);
    m_liveBackoff++;
}

void Api::readLive()
{
    if (!m_liveReply)
        return;
    m_liveWatch.start();
    m_liveBuffer += m_liveReply->readAll();
    qsizetype cut;
    while ((cut = m_liveBuffer.indexOf("\n\n")) >= 0) {
        const QByteArray block = m_liveBuffer.left(cut);
        m_liveBuffer.remove(0, cut + 2);
        QByteArray event;
        QByteArray data;
        for (const QByteArray &line : block.split('\n')) {
            if (line.startsWith("event: "))
                event = line.mid(7).trimmed();
            else if (line.startsWith("data: "))
                data += line.mid(6);
        }
        if (data.isEmpty())
            continue;
        const QVariantMap obj = QJsonDocument::fromJson(data).toVariant().toMap();
        if (event == "hello") {
            // A reconnect that came back in time replays what was missed;
            // a first connect or a long drop reads everything instead.
            const bool missed = m_liveAfter == 0 || obj.value("reset").toBool();
            m_liveAfter = obj.value("last").toLongLong();
            m_liveBackoff = 0;
            setLive(true);
            if (missed)
                emit liveReset();
        } else if (event == "ev") {
            m_liveAfter = qMax(m_liveAfter, obj.value("id").toLongLong());
            emit liveEvent(obj);
        }
    }
}

void Api::setLive(bool on)
{
    if (m_live == on)
        return;
    m_live = on;
    emit liveChanged();
}

QNetworkRequest Api::makeRequest(const QString &server, const QString &path, const QString &token) const
{
    QNetworkRequest req(QUrl(server + path));
    if (!token.isEmpty())
        req.setRawHeader("Authorization", "Bearer " + token.toUtf8());
    if (!token.isEmpty() && !m_workspace.isEmpty() && server == m_server)
        req.setRawHeader("X-Avora-Workspace", m_workspace.toUtf8());
    req.setHeader(QNetworkRequest::ContentTypeHeader, "application/json");
    req.setHeader(QNetworkRequest::UserAgentHeader, "avora-desktop/0.1");
    return req;
}

void Api::respond(QNetworkReply *reply, QJSValue callback, const std::function<void(int)> &after)
{
    connect(reply, &QNetworkReply::finished, this, [this, reply, callback, after]() mutable {
        reply->deleteLater();
        const int status = reply->attribute(QNetworkRequest::HttpStatusCodeAttribute).toInt();
        const QByteArray body = reply->readAll();
        const bool ok = status >= 200 && status < 300;
        if (after)
            after(status);

        QJSEngine *engine = qjsEngine(this);
        if (!callback.isCallable() || !engine)
            return;
        if (!ok) {
            callback.call({QJSValue(errorText(reply, body, status)), QJSValue(), QJSValue(status)});
            return;
        }
        QJSValue data;
        if (!body.isEmpty())
            data = engine->toScriptValue(QJsonDocument::fromJson(body).toVariant());
        callback.call({QJSValue(), data, QJSValue(status)});
    });
}

void Api::call(const QByteArray &verb, const QString &path, const QByteArray &body, const QJSValue &callback)
{
    QNetworkReply *reply = m_net.sendCustomRequest(makeRequest(m_server, path, m_token), verb, body);
    respond(reply, callback, [this](int status) {
        if (status == 401)
            signOut();
    });
}

void Api::completeSignIn(const QString &url, const QString &tok, const QVariantMap &person)
{
    stopLive();
    const bool serverDiff = (m_server != url);
    m_server = url;
    m_token = tok;

    const QString personId = person.value(QStringLiteral("id")).toString();
    const QString prevPerson = savedPerson(url);
    QString nextWs = savedWorkspace(url);
    if (!personId.isEmpty() && !prevPerson.isEmpty() && prevPerson != personId) {
        nextWs.clear();
        saveWorkspace(url, {});
    }
    if (!personId.isEmpty())
        savePerson(url, personId);

    const bool wsDiff = (m_workspace != nextWs);
    m_workspace = nextWs;

    if (!person.isEmpty()) {
        m_me = QVariantMap{{QStringLiteral("person"), person}, {QStringLiteral("workspaces"), QVariantList{}}};
        emit meChanged();
    } else if (!m_me.isEmpty()) {
        m_me.clear();
        emit meChanged();
    }

    QSettings().setValue("server", url);
    tokenstore::save(tok);
    if (serverDiff)
        emit serverChanged();
    if (wsDiff)
        emit workspaceChanged();
    emit signedInChanged();
    startLive();
    refreshMe();
}

void Api::authCall(const QString &server, const QString &path, const QJsonObject &payload, const QJSValue &callback)
{
    const QString url = trimServer(server);
    if (url.isEmpty()) {
        fail(callback, QStringLiteral("Informe o endereço do servidor"));
        return;
    }
    QNetworkReply *reply = m_net.post(makeRequest(url, path, {}), QJsonDocument(payload).toJson(QJsonDocument::Compact));
    connect(reply, &QNetworkReply::finished, this, [this, reply, url, callback]() mutable {
        reply->deleteLater();
        const int status = reply->attribute(QNetworkRequest::HttpStatusCodeAttribute).toInt();
        const QByteArray body = reply->readAll();
        const bool ok = status >= 200 && status < 300;
        const QVariantMap map = ok ? QJsonDocument::fromJson(body).toVariant().toMap() : QVariantMap{};
        const QString tok = map.value(QStringLiteral("token")).toString().trimmed();
        if (ok && !tok.isEmpty())
            completeSignIn(url, tok, map.value(QStringLiteral("person")).toMap());

        QJSEngine *engine = qjsEngine(this);
        if (!callback.isCallable() || !engine)
            return;
        if (!ok) {
            callback.call({QJSValue(errorText(reply, body, status)), QJSValue(), QJSValue(status)});
            return;
        }
        if (tok.isEmpty()) {
            callback.call({QJSValue(QStringLiteral("O servidor não devolveu uma sessão válida")), QJSValue(), QJSValue(status)});
            return;
        }
        callback.call({QJSValue(), engine->toScriptValue(map), QJSValue(status)});
    });
}

void Api::login(const QString &server, const QString &token, const QJSValue &callback)
{
    const QString url = trimServer(server);
    const QString tok = token.trimmed();
    if (url.isEmpty()) {
        fail(callback, QStringLiteral("Informe o endereço do servidor"));
        return;
    }
    QNetworkReply *reply = m_net.get(makeRequest(url, "/v1/capabilities", tok));
    respond(reply, callback, [this, url, tok](int status) {
        if (status != 200)
            return;
        completeSignIn(url, tok);
    });
}

void Api::signIn(const QString &server, const QString &email, const QString &password, const QJSValue &callback)
{
    QJsonObject payload{
        {QStringLiteral("email"), email.trimmed()},
        {QStringLiteral("password"), password},
    };
    authCall(server, QStringLiteral("/v1/auth/login"), payload, callback);
}

void Api::signUp(const QString &server, const QString &email, const QString &name, const QString &password, const QString &invite, const QJSValue &callback)
{
    QJsonObject payload{
        {QStringLiteral("email"), email.trimmed()},
        {QStringLiteral("name"), name.trimmed()},
        {QStringLiteral("password"), password},
    };
    if (!invite.trimmed().isEmpty())
        payload.insert(QStringLiteral("invite"), invite.trimmed());
    authCall(server, QStringLiteral("/v1/auth/signup"), payload, callback);
}

void Api::checkInvite(const QString &server, const QString &code, const QJSValue &callback)
{
    const QString url = trimServer(server);
    const QString trimmed = code.trimmed();
    if (url.isEmpty() || trimmed.isEmpty()) {
        fail(callback, QStringLiteral("Informe o servidor e o convite"));
        return;
    }
    const QString path = QStringLiteral("/v1/invites/") + QString::fromUtf8(QUrl::toPercentEncoding(trimmed));
    QNetworkReply *reply = m_net.get(makeRequest(url, path, {}));
    respond(reply, callback);
}

void Api::setWorkspace(const QString &id)
{
    const QString next = id.trimmed();
    if (m_workspace == next)
        return;
    stopStream();
    m_workspace = next;
    if (!m_server.isEmpty())
        saveWorkspace(m_server, m_workspace);
    emit workspaceChanged();
    if (signedIn()) {
        stopLive();
        startLive();
    }
}

void Api::refreshMe()
{
    if (m_server.isEmpty() || m_token.isEmpty())
        return;
    QNetworkReply *reply = m_net.get(makeRequest(m_server, QStringLiteral("/v1/me"), m_token));
    connect(reply, &QNetworkReply::finished, this, [this, reply] {
        reply->deleteLater();
        const int status = reply->attribute(QNetworkRequest::HttpStatusCodeAttribute).toInt();
        if (status == 401) {
            signOut();
            return;
        }
        if (status == 403 && !m_workspace.isEmpty()) {
            // Saved workspace belongs to another account or was revoked; retry without it.
            setWorkspace({});
            refreshMe();
            return;
        }
        if (status < 200 || status >= 300)
            return;
        const QVariantMap data = QJsonDocument::fromJson(reply->readAll()).toVariant().toMap();
        const QString personId = data.value(QStringLiteral("person")).toMap().value(QStringLiteral("id")).toString();
        if (!personId.isEmpty())
            savePerson(m_server, personId);
        if (m_me != data) {
            m_me = data;
            emit meChanged();
        }
        const QVariantList list = data.value(QStringLiteral("workspaces")).toList();
        if (!list.isEmpty()) {
            bool found = false;
            QString firstActive;
            QString firstAny;
            for (const QVariant &v : list) {
                const QVariantMap ws = v.toMap();
                const QString id = ws.value(QStringLiteral("id")).toString();
                if (id.isEmpty())
                    continue;
                if (firstAny.isEmpty())
                    firstAny = id;
                if (firstActive.isEmpty() && ws.value(QStringLiteral("status")).toString() != QStringLiteral("pending"))
                    firstActive = id;
                if (id == m_workspace)
                    found = true;
            }
            if (!found) {
                const QString pick = !firstActive.isEmpty() ? firstActive : firstAny;
                if (!pick.isEmpty())
                    setWorkspace(pick);
            }
        }
    });
}

void Api::logout()
{
    stopStream();
    if (!m_server.isEmpty() && !m_token.isEmpty()) {
        QNetworkReply *r = m_net.post(makeRequest(m_server, QStringLiteral("/v1/auth/logout"), m_token), QByteArray{});
        connect(r, &QNetworkReply::finished, r, &QObject::deleteLater);
    }
    signOut();
}

void Api::signOut()
{
    if (m_token.isEmpty())
        return;
    m_token.clear();
    tokenstore::clear();
    if (!m_me.isEmpty()) {
        m_me.clear();
        emit meChanged();
    }
    emit signedInChanged();
}

void Api::get(const QString &path, const QJSValue &callback)
{
    call("GET", path, {}, callback);
}

QByteArray Api::jsonBody(const QJSValue &body)
{
    if (body.isUndefined() || body.isNull())
        return {};
    // A QML object arrives as a QJSValue; convert it to plain maps and lists
    // first, or QJsonDocument sees nothing and the server gets an empty body.
    const QVariant v = body.toVariant(QJSValue::ConvertJSObjects);
    return QJsonDocument::fromVariant(v).toJson(QJsonDocument::Compact);
}

void Api::post(const QString &path, const QJSValue &body, const QJSValue &callback)
{
    call("POST", path, jsonBody(body), callback);
}

void Api::put(const QString &path, const QJSValue &body, const QJSValue &callback)
{
    call("PUT", path, jsonBody(body), callback);
}

void Api::patch(const QString &path, const QJSValue &body, const QJSValue &callback)
{
    call("PATCH", path, jsonBody(body), callback);
}

void Api::del(const QString &path, const QJSValue &callback)
{
    call("DELETE", path, QByteArray(), callback);
}

void Api::fetchRaw(const QString &path, const std::function<void(const QByteArray &, const QString &)> &done)
{
    QNetworkRequest req = makeRequest(m_server, path, m_token);
    QNetworkReply *reply = m_net.get(req);
    connect(reply, &QNetworkReply::finished, this, [reply, done] {
        reply->deleteLater();
        const int status = reply->attribute(QNetworkRequest::HttpStatusCodeAttribute).toInt();
        const QByteArray body = reply->readAll();
        if (status == 200)
            done(body, {});
        else
            done({}, errorText(reply, body, status));
    });
}

void Api::uploadAvatar(const QString &agentId, const QUrl &file, const QJSValue &callback)
{
    const avatar::Compressed photo = avatar::compress(file.isLocalFile() ? file.toLocalFile() : file.toString());
    if (!photo.error.isEmpty()) {
        QJSValue cb = callback;
        if (cb.isCallable())
            cb.call({QJSValue(photo.error)});
        return;
    }
    QNetworkRequest req = makeRequest(m_server, "/v1/agents/" + agentId + "/avatar", m_token);
    req.setHeader(QNetworkRequest::ContentTypeHeader, photo.contentType);
    QNetworkReply *reply = m_net.put(req, photo.data);
    respond(reply, callback, [this](int status) {
        if (status == 401)
            signOut();
    });
}

void Api::sendMessage(const QString &conversationId, const QString &content, qint64 replyTo)
{
    if (m_stream)
        return;
    QNetworkRequest req = makeRequest(m_server, "/v1/conversations/" + conversationId + "/messages", m_token);
    req.setRawHeader("Accept", "text/event-stream");
    req.setTransferTimeout(0); // a long answer may take minutes; the user can stop it
    QJsonObject message{{"content", content}};
    if (replyTo > 0)
        message.insert("reply_to", replyTo);
    const QByteArray body = QJsonDocument(message).toJson(QJsonDocument::Compact);

    m_buffer.clear();
    m_stream = m_net.post(req, body);
    setStreaming(true);

    QNetworkReply *reply = m_stream;
    connect(reply, &QNetworkReply::readyRead, this, [this, reply] {
        const int status = reply->attribute(QNetworkRequest::HttpStatusCodeAttribute).toInt();
        if (status == 200)
            readStream();
    });
    connect(reply, &QNetworkReply::finished, this, [this, reply] {
        reply->deleteLater();
        const int status = reply->attribute(QNetworkRequest::HttpStatusCodeAttribute).toInt();
        if (reply->error() == QNetworkReply::OperationCanceledError) {
            // stopped by the user
        } else if (status != 200) {
            emit streamError(errorText(reply, reply->readAll(), status));
            if (status == 401)
                signOut();
        } else {
            readStream();
        }
        setStreaming(false);
    });
}

// readStream splits the SSE body into "event: x / data: {...}" blocks.
void Api::readStream()
{
    if (!m_stream)
        return;
    m_buffer += m_stream->readAll();
    qsizetype cut;
    while ((cut = m_buffer.indexOf("\n\n")) >= 0) {
        const QByteArray block = m_buffer.left(cut);
        m_buffer.remove(0, cut + 2);
        QByteArray event = "message";
        QByteArray data;
        for (const QByteArray &line : block.split('\n')) {
            if (line.startsWith("event: "))
                event = line.mid(7).trimmed();
            else if (line.startsWith("data: "))
                data += line.mid(6);
        }
        if (data.isEmpty())
            continue;
        const QVariantMap obj = QJsonDocument::fromJson(data).toVariant().toMap();
        if (event == "user")
            emit streamUser(obj);
        else if (event == "reasoning")
            emit streamReasoning(obj.value("content").toString());
        else if (event == "delta")
            emit streamDelta(obj.value("content").toString());
        else if (event == "step")
            emit streamStep(obj);
        else if (event == "done")
            emit streamDone(obj);
        else if (event == "error")
            emit streamError(obj.value("error").toString());
    }
}

void Api::stopStream()
{
    if (m_stream)
        m_stream->abort();
}

void Api::setStreaming(bool on)
{
    if (m_streaming == on)
        return;
    m_streaming = on;
    emit streamingChanged();
}

QString Api::fileBase64(const QUrl &file, qint64 maxBytes) const
{
    QFile f(file.isLocalFile() ? file.toLocalFile() : file.toString());
    if (f.size() <= 0 || f.size() > maxBytes || !f.open(QIODevice::ReadOnly))
        return {};
    return QString::fromLatin1(f.readAll().toBase64());
}

QString Api::clipboardText() const
{
    return QGuiApplication::clipboard()->text().trimmed();
}

void Api::copyText(const QString &text) const
{
    QGuiApplication::clipboard()->setText(text);
}

// ── Attachments ─────────────────────────────────────────────────

namespace {
constexpr qint64 maxUpload = 15 << 20;
constexpr qint64 shrinkOver = 4 << 20; // photos bigger than this become a JPEG

// shrunk turns a big photo into a JPEG of at most 2560 px, so the model can
// take it; small images and other files pass as they are.
QByteArray shrunk(const QImage &img)
{
    QImage out = img;
    if (out.width() > 2560 || out.height() > 2560)
        out = out.scaled(2560, 2560, Qt::KeepAspectRatio, Qt::SmoothTransformation);
    QByteArray bytes;
    QBuffer buf(&bytes);
    buf.open(QIODevice::WriteOnly);
    out.convertToFormat(QImage::Format_RGB888).save(&buf, "JPEG", 85);
    return bytes;
}
} // namespace

void Api::sendFile(const QString &conversationId, const QString &name, const QString &mime, const QByteArray &data, const QJSValue &callback)
{
    QUrlQuery q;
    q.addQueryItem("name", name);
    if (!conversationId.isEmpty())
        q.addQueryItem("conversation", conversationId);
    QNetworkRequest req = makeRequest(m_server, "/v1/files?" + q.toString(QUrl::FullyEncoded), m_token);
    req.setHeader(QNetworkRequest::ContentTypeHeader, mime);
    QNetworkReply *reply = m_net.post(req, data);
    respond(reply, callback, [this](int status) {
        if (status == 401)
            signOut();
    });
}

void Api::uploadFile(const QString &conversationId, const QUrl &file, const QJSValue &callback)
{
    const QString path = file.isLocalFile() ? file.toLocalFile() : file.toString();
    QFile f(path);
    if (!f.open(QIODevice::ReadOnly))
        return fail(callback, "Não consegui abrir " + QFileInfo(path).fileName());
    QString name = QFileInfo(path).fileName();
    QString mime = QMimeDatabase().mimeTypeForFile(path).name();
    QByteArray data;
    if (mime.startsWith("image/") && f.size() > shrinkOver) {
        QImage img(path);
        if (!img.isNull()) {
            data = shrunk(img);
            mime = "image/jpeg";
            name = QFileInfo(path).completeBaseName() + ".jpg";
        }
    }
    if (data.isEmpty()) {
        if (f.size() > maxUpload)
            return fail(callback, name + " passa de 15 MB");
        data = f.readAll();
    }
    if (data.isEmpty())
        return fail(callback, name + " está vazio");
    sendFile(conversationId, name, mime, data, callback);
}

bool Api::clipboardHasImage() const
{
    const QMimeData *md = QGuiApplication::clipboard()->mimeData();
    return md && md->hasImage();
}

void Api::uploadClipboardImage(const QString &conversationId, const QJSValue &callback)
{
    const QImage img = QGuiApplication::clipboard()->image();
    if (img.isNull())
        return fail(callback, "Não há imagem na área de transferência");
    QByteArray bytes;
    QBuffer buf(&bytes);
    buf.open(QIODevice::WriteOnly);
    img.save(&buf, "PNG");
    const QString stamp = QDateTime::currentDateTime().toString("yyyy-MM-dd_HH-mm-ss");
    if (bytes.size() > shrinkOver)
        return sendFile(conversationId, "imagem-" + stamp + ".jpg", "image/jpeg", shrunk(img), callback);
    sendFile(conversationId, "imagem-" + stamp + ".png", "image/png", bytes, callback);
}

void Api::saveFile(const QString &id, const QString &name, const QJSValue &callback)
{
    QJSValue cb = callback;
    fetchRaw("/v1/files/" + id, [this, cb, name](const QByteArray &data, const QString &err) mutable {
        if (!err.isEmpty()) {
            fail(cb, err);
            return;
        }
        const QString dir = QStandardPaths::writableLocation(QStandardPaths::DownloadLocation) + "/Avora";
        QDir().mkpath(dir);
        // The extension comes from the bytes: the name may have none.
        QString ext = ".bin";
        if (data.size() > 4 && uchar(data[0]) == 0x89 && data.mid(1, 3) == "PNG")
            ext = ".png";
        else if (data.size() > 2 && uchar(data[0]) == 0xFF && uchar(data[1]) == 0xD8)
            ext = ".jpg";
        else if (data.startsWith("GIF8"))
            ext = ".gif";
        else if (data.startsWith("RIFF") && data.mid(8, 4) == "WEBP")
            ext = ".webp";
        QString base = QFileInfo(name).completeBaseName();
        for (QChar &c : base)
            if (QStringLiteral("\\/:*?\"<>|").contains(c))
                c = QChar(' ');
        base = base.simplified();
        if (base.isEmpty())
            base = "imagem";
        QString path = dir + "/" + base + ext;
        for (int n = 2; QFileInfo::exists(path) && n < 1000; n++)
            path = dir + "/" + base + " (" + QString::number(n) + ")" + ext;
        QFile out(path);
        if (!out.open(QIODevice::WriteOnly) || out.write(data) != data.size()) {
            fail(cb, "Não consegui salvar em " + dir);
            return;
        }
        out.close();
        if (cb.isCallable())
            cb.call({QJSValue(QJSValue::NullValue), QJSValue(QDir::toNativeSeparators(path))});
    });
}

void Api::saveText(const QString &name, const QString &text, bool open, const QJSValue &callback)
{
    const QString dir = QStandardPaths::writableLocation(QStandardPaths::DownloadLocation) + "/Avora";
    QDir().mkpath(dir);
    const QFileInfo info(name);
    QString base = info.completeBaseName(), ext = info.suffix().isEmpty() ? QString(".md") : "." + info.suffix();
    for (QChar &c : base)
        if (QStringLiteral("\\/:*?\"<>|").contains(c))
            c = QChar(' ');
    base = base.simplified();
    if (base.isEmpty())
        base = "doc";
    if (base.size() > 120)
        base = base.left(120).trimmed();
    QString path = dir + "/" + base + ext;
    for (int n = 2; QFileInfo::exists(path) && n < 1000; n++)
        path = dir + "/" + base + " (" + QString::number(n) + ")" + ext;
    QFile out(path);
    const QByteArray data = text.toUtf8();
    if (!out.open(QIODevice::WriteOnly) || out.write(data) != data.size()) {
        fail(callback, "Não consegui salvar em " + dir);
        return;
    }
    out.close();
    if (open)
        QDesktopServices::openUrl(QUrl::fromLocalFile(path));
    QJSValue cb = callback;
    if (cb.isCallable())
        cb.call({QJSValue(QJSValue::NullValue), QJSValue(QDir::toNativeSeparators(path))});
}

void Api::showInFolder(const QString &path) const
{
    if (!QFileInfo::exists(path))
        return;
#ifdef Q_OS_WIN
    QProcess::startDetached("explorer.exe", {"/select,", QDir::toNativeSeparators(path)});
#else
    QDesktopServices::openUrl(QUrl::fromLocalFile(QFileInfo(path).absolutePath()));
#endif
}

void Api::openFile(const QString &id, const QString &name, const QJSValue &callback)
{
    QJSValue cb = callback;
    fetchRaw("/v1/files/" + id, [cb, name, id](const QByteArray &data, const QString &err) mutable {
        if (!err.isEmpty()) {
            fail(cb, err);
            return;
        }
        const QString dir = QStandardPaths::writableLocation(QStandardPaths::DownloadLocation) + "/Avora";
        QDir().mkpath(dir);
        QString safe = QFileInfo(name).fileName();
        if (safe.isEmpty())
            safe = id;
        QFile out(dir + "/" + safe);
        if (!out.open(QIODevice::WriteOnly) || out.write(data) != data.size()) {
            fail(cb, "Não consegui salvar em " + dir);
            return;
        }
        out.close();
        QDesktopServices::openUrl(QUrl::fromLocalFile(out.fileName()));
        if (cb.isCallable())
            cb.call({QJSValue()});
    });
}
