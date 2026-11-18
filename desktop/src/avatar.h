#pragma once

#include <QByteArray>
#include <QImage>
#include <QQuickAsyncImageProvider>
#include <QString>

// Agent photos for QML: Image { source: "image://avatar/atlas?v=<time>" }.
// The photo comes from the server with the app's token (an Image cannot send
// it), is kept in memory, and is drawn as an antialiased circle at the size
// asked for. A new ?v= after an upload fetches it again.
class AvatarProvider : public QQuickAsyncImageProvider {
public:
    QQuickImageResponse *requestImageResponse(const QString &id, const QSize &requestedSize) override;
};

namespace avatar {

// compress turns a picked image into the photo sent to the server: the
// centered square, 256 px, WebP (PNG if this Qt has no WebP writer).
// Returns the bytes and their content type, or an error in words.
struct Compressed {
    QByteArray data;
    QByteArray contentType;
    QString error;
};
Compressed compress(const QString &file);

// circle draws img as a circle of px pixels, transparent outside.
QImage circle(const QImage &img, int px);

} // namespace avatar
