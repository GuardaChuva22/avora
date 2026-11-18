#pragma once

#include <QQuickAsyncImageProvider>

// Images from the Avora server for QML, fetched with the app's token (an
// Image cannot send it): Image { source: "image://server/v1/files/<id>" }.
// The browser's live view adds ?t=<time> to load the latest screen.
class ServerImageProvider : public QQuickAsyncImageProvider {
public:
    QQuickImageResponse *requestImageResponse(const QString &id, const QSize &requestedSize) override;
};
