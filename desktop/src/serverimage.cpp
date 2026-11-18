#include "serverimage.h"

#include "api.h"

#include <QImage>
#include <QPointer>
#include <QQuickTextureFactory>

namespace {

class ServerImageResponse : public QQuickImageResponse {
public:
    QQuickTextureFactory *textureFactory() const override
    {
        return QQuickTextureFactory::textureFactoryForImage(m_image);
    }
    QString errorString() const override { return m_error; }

    void finish(const QImage &image, const QString &error)
    {
        m_image = image;
        m_error = error;
        emit finished();
    }

private:
    QImage m_image;
    QString m_error;
};

} // namespace

QQuickImageResponse *ServerImageProvider::requestImageResponse(const QString &id, const QSize &requestedSize)
{
    auto *response = new ServerImageResponse;
    Api *api = Api::instance();
    // Only the server's own API paths (files, the browser's screen).
    const QString path = "/" + id;
    if (!api || !(path.startsWith("/v1/files/") || path.startsWith("/v1/browser/") || path.startsWith("/v1/whatsapp/qr.png"))) {
        QMetaObject::invokeMethod(response, [response] { response->finish({}, QStringLiteral("not allowed")); }, Qt::QueuedConnection);
        return response;
    }
    QPointer<ServerImageResponse> guard(response);
    QMetaObject::invokeMethod(api, [api, path, requestedSize, guard] {
        api->fetchRaw(path, [requestedSize, guard](const QByteArray &data, const QString &error) {
            QImage img;
            img.loadFromData(data);
            if (!img.isNull() && requestedSize.isValid() && requestedSize.width() > 0 && requestedSize.height() > 0
                && (img.width() > requestedSize.width() || img.height() > requestedSize.height()))
                img = img.scaled(requestedSize, Qt::KeepAspectRatio, Qt::SmoothTransformation);
            if (guard)
                guard->finish(img, img.isNull() ? (error.isEmpty() ? QStringLiteral("bad image") : error) : QString());
        });
    }, Qt::QueuedConnection);
    return response;
}
