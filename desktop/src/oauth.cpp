// The browser login for Plugins (Google): a one-shot listener on a
// 127.0.0.1 port catches the consent redirect and hands the code to the
// server, which keeps the credential. The app never sees a token.

#include "api.h"

#include <QDesktopServices>
#include <QHostAddress>
#include <QJSEngine>
#include <QJsonDocument>
#include <QJsonObject>
#include <QNetworkReply>
#include <QTcpServer>
#include <QTcpSocket>
#include <QTimer>
#include <QUrl>
#include <QUrlQuery>

#include <memory>

namespace {

constexpr int kLoginMs = 5 * 60 * 1000;

QByteArray loginPage(int code, const QString &title, const QString &text)
{
    const QByteArray body = QStringLiteral(
        "<!doctype html><html lang=pt-BR><meta charset=utf-8><title>Avora</title>"
        "<body style=\"margin:0;height:100vh;display:grid;place-items:center;background:#0b0c10;color:#e8e9ee;"
        "font:16px system-ui,sans-serif\"><div style=\"text-align:center;max-width:420px;padding:24px\">"
        "<div style=\"font-size:22px;font-weight:600;margin-bottom:10px\">%1</div>"
        "<div style=\"color:#9a9caa\">%2</div></div></body></html>")
                                .arg(title.toHtmlEscaped(), text.toHtmlEscaped())
                                .toUtf8();
    const QByteArray status = code == 200 ? "200 OK" : code == 404 ? "404 Not Found" : "400 Bad Request";
    return "HTTP/1.1 " + status + "\r\nContent-Type: text/html; charset=utf-8\r\nCache-Control: no-store\r\n"
           "Connection: close\r\nContent-Length: " + QByteArray::number(body.size()) + "\r\n\r\n" + body;
}

void answer(QTcpSocket *sock, const QByteArray &response)
{
    sock->write(response);
    sock->disconnectFromHost();
}

QString replyError(QNetworkReply *reply, const QByteArray &body, int status)
{
    const QString text = QString::fromUtf8(body).trimmed();
    if (!text.isEmpty() && status != 401)
        return text;
    if (status == 401)
        return QStringLiteral("Token recusado");
    if (status == 0)
        return QStringLiteral("Sem conexão com o servidor: %1").arg(reply->errorString());
    return QStringLiteral("Erro %1").arg(status);
}

} // namespace

void Api::cancelOAuth()
{
    if (m_oauth) {
        m_oauth->close();
        m_oauth->deleteLater();
    }
}

void Api::connectOAuth(const QString &kind, const QString &connection, int port, const QJSValue &callback)
{
    cancelOAuth();
    QPointer<QTcpServer> server = new QTcpServer(this);
    if (port < 0 || port > 65535 || !server->listen(QHostAddress::LocalHost, quint16(port))) {
        delete server;
        if (callback.isCallable())
            QJSValue(callback).call({QJSValue(port > 0
                ? QStringLiteral("A porta %1 do login está ocupada por outro programa; feche-o e tente de novo").arg(port)
                : QStringLiteral("Não consegui abrir a porta local do login"))});
        return;
    }
    m_oauth = server;
    const QString redirect = QStringLiteral("http://127.0.0.1:%1/callback").arg(server->serverPort());
    const QString base = QStringLiteral("/v1/connectors/") + QString::fromUtf8(QUrl::toPercentEncoding(kind));
    auto state = std::make_shared<QString>();
    auto done = std::make_shared<bool>(false);
    QJSValue cb = callback;

    // finish ends the login once, whatever ends it first.
    using Finish = std::function<void(const QString &, const QByteArray &)>;
    auto finish = std::make_shared<Finish>([this, server, cb, done](const QString &err, const QByteArray &json) mutable {
        if (*done)
            return;
        *done = true;
        if (server) {
            server->close();
            server->deleteLater();
        }
        QJSEngine *engine = qjsEngine(this);
        if (!cb.isCallable() || !engine)
            return;
        if (!err.isEmpty()) {
            cb.call({QJSValue(err)});
            return;
        }
        cb.call({QJSValue(), engine->toScriptValue(QJsonDocument::fromJson(json).toVariant())});
    });

    // send POSTs to the server and passes a 2xx body to ok.
    auto send = std::make_shared<std::function<void(const QString &, const QJsonObject &, std::function<void(const QByteArray &)>)>>(
        [this, finish](const QString &path, const QJsonObject &body, std::function<void(const QByteArray &)> ok) {
            QNetworkRequest req = makeRequest(m_server, path, m_token);
            req.setTransferTimeout(60'000);
            QNetworkReply *r = m_net.post(req, QJsonDocument(body).toJson(QJsonDocument::Compact));
            connect(r, &QNetworkReply::finished, this, [this, r, finish, ok]() {
                r->deleteLater();
                const int status = r->attribute(QNetworkRequest::HttpStatusCodeAttribute).toInt();
                const QByteArray body = r->readAll();
                if (status >= 200 && status < 300) {
                    ok(body);
                    return;
                }
                (*finish)(replyError(r, body, status), {});
                if (status == 401)
                    signOut();
            });
        });

    QTimer::singleShot(kLoginMs, server, [finish]() {
        (*finish)(QStringLiteral("O login não terminou em 5 minutos; tente de novo"), {});
    });

    connect(server, &QTcpServer::newConnection, this, [this, server, state, finish, base, send]() {
        while (server && server->hasPendingConnections()) {
            QTcpSocket *sock = server->nextPendingConnection();
            sock->setParent(nullptr); // outlives the listener until it has answered
            connect(sock, &QTcpSocket::disconnected, sock, &QObject::deleteLater);
            auto buf = std::make_shared<QByteArray>();
            connect(sock, &QTcpSocket::readyRead, sock, [sock, buf, server, state, finish, base, send]() {
                buf->append(sock->readAll());
                if (buf->size() > 16 * 1024) {
                    sock->abort();
                    return;
                }
                const qsizetype end = buf->indexOf("\r\n");
                if (end < 0)
                    return;
                const QList<QByteArray> line = buf->left(end).split(' ');
                buf->clear();
                const QUrl url(QStringLiteral("http://127.0.0.1") + QString::fromLatin1(line.value(1)));
                if (line.value(0) != "GET" || url.path() != QStringLiteral("/callback")) {
                    answer(sock, loginPage(404, QStringLiteral("Avora"), QStringLiteral("Nada aqui.")));
                    return;
                }
                const QUrlQuery q(url);
                const QString got = q.queryItemValue(QStringLiteral("state"), QUrl::FullyDecoded);
                const QString code = q.queryItemValue(QStringLiteral("code"), QUrl::FullyDecoded);
                if (state->isEmpty() || got != *state) {
                    answer(sock, loginPage(400, QStringLiteral("Login não reconhecido"),
                                           QStringLiteral("Volte ao Avora e clique em Conectar de novo.")));
                    return;
                }
                if (code.isEmpty()) {
                    answer(sock, loginPage(400, QStringLiteral("Login cancelado"),
                                           QStringLiteral("Nada foi conectado. Pode fechar esta aba.")));
                    (*finish)(QStringLiteral("O login foi cancelado no navegador"), {});
                    return;
                }
                answer(sock, loginPage(200, QStringLiteral("Pronto"), QStringLiteral("Pode fechar esta aba e voltar ao Avora.")));
                if (server)
                    server->close(); // one code per login
                (*send)(base + QStringLiteral("/oauth/finish"), {{"state", got}, {"code", code}},
                        [finish](const QByteArray &body) { (*finish)({}, body); });
            });
        }
    });

    QJsonObject start{{"redirect_uri", redirect}};
    if (!connection.isEmpty())
        start.insert("connection", connection);
    (*send)(base + QStringLiteral("/oauth/start"), start, [state, finish, server](const QByteArray &body) {
        if (!server)
            return;
        const QJsonObject o = QJsonDocument::fromJson(body).object();
        *state = o.value("state").toString();
        const QUrl url(o.value("url").toString());
        if (state->isEmpty() || !url.isValid() || url.scheme() != QStringLiteral("https")) {
            (*finish)(QStringLiteral("O servidor não devolveu um endereço de login válido"), {});
            return;
        }
        if (!QDesktopServices::openUrl(url))
            (*finish)(QStringLiteral("Não consegui abrir o navegador"), {});
    });
}
