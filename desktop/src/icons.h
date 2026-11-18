#pragma once

#include <QQuickImageProvider>

// Line icons and brand logos for QML, rasterized once per name, color and
// size: Image { source: "image://svg/<percent-encoded SVG document>" }.
// Shapes with the curve renderer were triangulated on the CPU for every
// instance; a cached texture is drawn for free and shared by every copy.
class SvgIconProvider : public QQuickImageProvider {
public:
    SvgIconProvider();
    QImage requestImage(const QString &id, QSize *size, const QSize &requestedSize) override;
};

// A mark from the chat's @ (D35) drawn as one picture, so it can sit inside
// the text like a letter and Backspace takes it whole:
// <img src="image://chip/<base64url JSON>">, where the JSON carries the size
// (w, h, in points), the tone (c), the text color (t), the name (n) and the
// logo or icon path (p) with its mode (m: "fill" or "stroke").
class ChipProvider : public QQuickImageProvider {
public:
    ChipProvider();
    QImage requestImage(const QString &id, QSize *size, const QSize &requestedSize) override;
};
