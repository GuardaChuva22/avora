#include "avatar.h"

#include "api.h"

#include <QBuffer>
#include <QHash>
#include <QImageReader>
#include <QImageWriter>
#include <QMutex>
#include <QPainter>
#include <QPainterPath>
#include <QPointer>
#include <QQuickTextureFactory>

namespace {

constexpr int kSide = 512;   // stored photo, in pixels (sharp at the profile size)
constexpr int kQuality = 82; // WebP quality: small (about 50 KB) and still sharp at 512 px

QMutex cacheLock;
QHash<QString, QImage> cache; // "atlas?v=…" -> decoded photo

class AvatarResponse : public QQuickImageResponse {
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

int sideFor(const QSize &requested)
{
    const int side = qMax(requested.width(), requested.height());
    return side > 0 ? qMin(side, kSide) : 96;
}

} // namespace

namespace avatar {

QImage circle(const QImage &img, int px)
{
    const int side = qMin(img.width(), img.height());
    const QImage square = img.copy((img.width() - side) / 2, (img.height() - side) / 2, side, side)
                              .scaled(px, px, Qt::IgnoreAspectRatio, Qt::SmoothTransformation);
    QImage out(px, px, QImage::Format_ARGB32_Premultiplied);
    out.fill(Qt::transparent);
    QPainter p(&out);
    p.setRenderHint(QPainter::Antialiasing);
    p.setRenderHint(QPainter::SmoothPixmapTransform);
    QPainterPath clip;
    clip.addEllipse(QRectF(0, 0, px, px));
    p.setClipPath(clip);
    p.drawImage(0, 0, square);
    return out;
}

Compressed compress(const QString &file)
{
    QImageReader reader(file);
    reader.setAutoTransform(true); // phone photos keep their orientation
    QImage img = reader.read();
    if (img.isNull())
        return {{}, {}, QStringLiteral("Não consegui abrir a imagem (%1).").arg(reader.errorString())};

    const int side = qMin(img.width(), img.height());
    img = img.copy((img.width() - side) / 2, (img.height() - side) / 2, side, side)
              .scaled(kSide, kSide, Qt::IgnoreAspectRatio, Qt::SmoothTransformation)
              .convertToFormat(QImage::Format_RGB32); // no transparency: a photo on a disc

    for (const QByteArray format : {QByteArray("webp"), QByteArray("png")}) {
        QByteArray bytes;
        QBuffer buffer(&bytes);
        buffer.open(QIODevice::WriteOnly);
        QImageWriter writer(&buffer, format);
        if (format == "webp")
            writer.setQuality(kQuality);
        if (writer.write(img) && !bytes.isEmpty())
            return {bytes, "image/" + format, {}};
    }
    return {{}, {}, QStringLiteral("Não consegui comprimir a imagem.")};
}

} // namespace avatar

QQuickImageResponse *AvatarProvider::requestImageResponse(const QString &id, const QSize &requestedSize)
{
    auto *response = new AvatarResponse;
    const int px = sideFor(requestedSize);
    {
        QMutexLocker lock(&cacheLock);
        const auto it = cache.constFind(id);
        if (it != cache.constEnd()) {
            const QImage img = avatar::circle(*it, px);
            lock.unlock();
            QMetaObject::invokeMethod(response, [response, img] { response->finish(img, {}); }, Qt::QueuedConnection);
            return response;
        }
    }
    // "atlas?v=…" -> /v1/agents/atlas/avatar; the network lives on the app's thread.
    const QString agent = id.section('?', 0, 0);
    QPointer<AvatarResponse> guard(response);
    Api *api = Api::instance();
    if (!api) {
        QMetaObject::invokeMethod(response, [response] { response->finish({}, QStringLiteral("no api")); }, Qt::QueuedConnection);
        return response;
    }
    QMetaObject::invokeMethod(api, [api, agent, id, px, guard] {
        api->fetchRaw("/v1/agents/" + agent + "/avatar", [id, px, guard](const QByteArray &data, const QString &error) {
            QImage img;
            img.loadFromData(data);
            if (!img.isNull()) {
                QMutexLocker lock(&cacheLock);
                cache.insert(id, img);
            }
            if (guard)
                guard->finish(img.isNull() ? QImage() : avatar::circle(img, px), img.isNull() ? (error.isEmpty() ? QStringLiteral("bad image") : error) : QString());
        });
    }, Qt::QueuedConnection);
    return response;
}
