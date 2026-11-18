#pragma once

#include <QJSValue>
#include <QJsonObject>
#include <QNetworkAccessManager>
#include <QObject>
#include <QPointer>
#include <QTimer>
#include <QVariantMap>
#include <QtQml/qqmlregistration.h>

#include <functional>

class QNetworkReply;
class QTcpServer;

// Api is the app's only door to avora-server. QML calls get/post with a
// callback (error, data, status); chat answers arrive through the stream
// signals. A 401 anywhere signs the app out.
class Api : public QObject {
    Q_OBJECT
    QML_ELEMENT
    QML_SINGLETON
    Q_PROPERTY(QString server READ server NOTIFY serverChanged)
    Q_PROPERTY(bool signedIn READ signedIn NOTIFY signedInChanged)
    Q_PROPERTY(QString workspace READ workspace WRITE setWorkspace NOTIFY workspaceChanged)
    Q_PROPERTY(QVariantMap me READ me NOTIFY meChanged)
    Q_PROPERTY(bool streaming READ streaming NOTIFY streamingChanged)
    // live: the stream of everything that happens on the server is open
    // (D47). While it is, screens react to liveEvent instead of polling.
    Q_PROPERTY(bool live READ live NOTIFY liveChanged)

public:
    explicit Api(QObject *parent = nullptr);

    // instance is the app's Api, for the image provider.
    static Api *instance();

    QString server() const { return m_server; }
    bool signedIn() const { return !m_token.isEmpty(); }
    QString workspace() const { return m_workspace; }
    QVariantMap me() const { return m_me; }
    bool streaming() const { return m_streaming; }
    bool live() const { return m_live; }

    Q_INVOKABLE void login(const QString &server, const QString &token, const QJSValue &callback);
    Q_INVOKABLE void signIn(const QString &server, const QString &email, const QString &password, const QJSValue &callback);
    Q_INVOKABLE void signUp(const QString &server, const QString &email, const QString &name, const QString &password, const QString &invite, const QJSValue &callback);
    Q_INVOKABLE void checkInvite(const QString &server, const QString &code, const QJSValue &callback);
    Q_INVOKABLE void setWorkspace(const QString &id);
    Q_INVOKABLE void refreshMe();
    Q_INVOKABLE void logout();
    Q_INVOKABLE void get(const QString &path, const QJSValue &callback);
    Q_INVOKABLE void post(const QString &path, const QJSValue &body, const QJSValue &callback);
    Q_INVOKABLE void put(const QString &path, const QJSValue &body, const QJSValue &callback);
    Q_INVOKABLE void patch(const QString &path, const QJSValue &body, const QJSValue &callback);
    Q_INVOKABLE void del(const QString &path, const QJSValue &callback);

    // jsonBody turns a QML value (object, array, null) into a request body.
    static QByteArray jsonBody(const QJSValue &body);
    Q_INVOKABLE void sendMessage(const QString &conversationId, const QString &content, qint64 replyTo = 0);
    // uploadAvatar compresses a picked image to a 256 px WebP and stores it
    // as the agent's photo; callback(error).
    Q_INVOKABLE void uploadAvatar(const QString &agentId, const QUrl &file, const QJSValue &callback);
    // fetchRaw GETs bytes (an image) with the token.
    void fetchRaw(const QString &path, const std::function<void(const QByteArray &, const QString &)> &done);
    // connectOAuth connects a Plugin that logs in through the browser: it
    // listens on a 127.0.0.1 port, opens the consent page and hands the code
    // it gets back to the server; callback(error, connector). connection is
    // the account being reconnected; empty adds an account. port is the
    // fixed port a provider registered (0: any free one).
    Q_INVOKABLE void connectOAuth(const QString &kind, const QString &connection, int port, const QJSValue &callback);
    Q_INVOKABLE void cancelOAuth();
    Q_INVOKABLE void stopStream();
    // Project folders on this PC. localPath turns a picked folder URL into
    // C:\path. allowFolder adds it to the list the Avora runtime of this
    // PC accepts (%APPDATA%\Avoraolders.json). readAgentsFile and
    // writeAgentsFile keep the folder's AGENTS.md, only in allowed folders.
    Q_INVOKABLE QString localPath(const QUrl &url) const;
    Q_INVOKABLE bool allowFolder(const QString &folder) const;
    Q_INVOKABLE bool folderExists(const QString &folder) const;
    Q_INVOKABLE QString readAgentsFile(const QString &folder) const;
    Q_INVOKABLE bool writeAgentsFile(const QString &folder, const QString &text) const;
    Q_INVOKABLE QString clipboardText() const;
    // fileBase64 reads a small picked file (a certificate for a Plugin
    // form) as base64; empty when it is missing or bigger than maxBytes.
    Q_INVOKABLE QString fileBase64(const QUrl &file, qint64 maxBytes) const;
    // Attachments. uploadFile stores a picked or dropped file on the server;
    // uploadClipboardImage the image on the clipboard; callback(error, file)
    // with file {id, name, mime, size}. Big photos are shrunk to a JPEG.
    // openFile saves an attachment to Downloads\Avora and opens it.
    Q_INVOKABLE void uploadFile(const QString &conversationId, const QUrl &file, const QJSValue &callback);
    Q_INVOKABLE bool clipboardHasImage() const;
    Q_INVOKABLE void uploadClipboardImage(const QString &conversationId, const QJSValue &callback);
    Q_INVOKABLE void openFile(const QString &id, const QString &name, const QJSValue &callback);
    // saveFile keeps a file (an image in the chat) in Downloads\Avora under a
    // name of its own, never over another; callback(err, path).
    Q_INVOKABLE void saveFile(const QString &id, const QString &name, const QJSValue &callback);
    // saveText keeps a doc's text (a .md, an .html page) in Downloads\Avora
    // under a name of its own; open also opens it in its app (the browser
    // for a page). callback(err, path).
    Q_INVOKABLE void saveText(const QString &name, const QString &text, bool open, const QJSValue &callback);
    // showInFolder opens the folder of a saved file with it selected.
    Q_INVOKABLE void showInFolder(const QString &path) const;
    Q_INVOKABLE void copyText(const QString &text) const;

signals:
    void serverChanged();
    void signedInChanged();
    void workspaceChanged();
    void meChanged();
    void streamingChanged();
    void streamUser(const QVariantMap &message);
    void streamReasoning(const QString &text);
    void streamDelta(const QString &text);
    void streamStep(const QVariantMap &step);
    void streamDone(const QVariantMap &message);
    void streamError(const QString &error);
    void liveChanged();
    // liveEvent is one event of the server's log: {id, type, job_id, payload}.
    void liveEvent(const QVariantMap &event);
    // liveReset: events were missed (a long drop); read everything again.
    void liveReset();

private:
    QNetworkRequest makeRequest(const QString &server, const QString &path, const QString &token) const;
    void call(const QByteArray &verb, const QString &path, const QByteArray &body, const QJSValue &callback);
    void respond(QNetworkReply *reply, QJSValue callback, const std::function<void(int)> &after = {});
    void authCall(const QString &server, const QString &path, const QJsonObject &payload, const QJSValue &callback);
    void completeSignIn(const QString &url, const QString &tok, const QVariantMap &person = {});
    void readStream();
    void sendFile(const QString &conversationId, const QString &name, const QString &mime, const QByteArray &data, const QJSValue &callback);
    void setStreaming(bool on);
    void signOut();
    void startLive();
    void stopLive();
    void readLive();
    void liveLost();
    void setLive(bool on);

    QNetworkAccessManager m_net;
    QString m_server;
    QString m_token;
    QString m_workspace;
    QVariantMap m_me;
    QPointer<QNetworkReply> m_stream;
    QPointer<QTcpServer> m_oauth;
    QByteArray m_buffer;
    bool m_streaming = false;
    QPointer<QNetworkReply> m_liveReply;
    QByteArray m_liveBuffer;
    qint64 m_liveAfter = 0;
    int m_liveBackoff = 0;
    bool m_live = false;
    QTimer m_liveRetry;
    // m_liveWatch: no byte for this long (the server sends a keepalive
    // every 15 s) means the connection died quietly.
    QTimer m_liveWatch;
};
