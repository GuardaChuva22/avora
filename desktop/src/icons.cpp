#include "icons.h"

#include <QGuiApplication>
#include <QJsonDocument>
#include <QJsonObject>
#include <QPainter>
#include <QtMath>
#include <QSvgRenderer>
#include <QUrl>

SvgIconProvider::SvgIconProvider()
    : QQuickImageProvider(QQuickImageProvider::Image)
{
}

QImage SvgIconProvider::requestImage(const QString &id, QSize *size, const QSize &requestedSize)
{
    const QByteArray svg = QUrl::fromPercentEncoding(id.toUtf8()).toUtf8();
    const QSize px = requestedSize.isValid() && !requestedSize.isEmpty() ? requestedSize : QSize(24, 24);
    QImage img(px, QImage::Format_ARGB32_Premultiplied);
    img.fill(Qt::transparent);
    QSvgRenderer r(svg);
    if (r.isValid()) {
        QPainter p(&img);
        p.setRenderHint(QPainter::Antialiasing);
        r.render(&p);
    }
    if (size)
        *size = px;
    return img;
}

ChipProvider::ChipProvider()
    : QQuickImageProvider(QQuickImageProvider::Image)
{
}

QImage ChipProvider::requestImage(const QString &id, QSize *size, const QSize &)
{
    const QJsonObject o = QJsonDocument::fromJson(
        QByteArray::fromBase64(id.toLatin1(), QByteArray::Base64UrlEncoding | QByteArray::OmitTrailingEquals)).object();
    const qreal w = qMax(20.0, o.value("w").toDouble()), h = qMax(12.0, o.value("h").toDouble());
    // Sharp on any screen: drawn at twice the points (or the screen's ratio).
    const qreal k = qMax<qreal>(2.0, qGuiApp->devicePixelRatio());
    QImage img(QSize(qCeil(w * k), qCeil(h * k)), QImage::Format_ARGB32_Premultiplied);
    img.fill(Qt::transparent);
    QPainter p(&img);
    p.setRenderHint(QPainter::Antialiasing);
    p.setRenderHint(QPainter::TextAntialiasing);
    p.scale(k, k);

    QColor tone(o.value("c").toString());
    if (!tone.isValid())
        tone = QColor("#9a9aa6");
    QColor fill = tone, line = tone;
    fill.setAlphaF(0.16);
    line.setAlphaF(0.38);
    p.setPen(QPen(line, 1));
    p.setBrush(fill);
    p.drawRoundedRect(QRectF(0.5, 0.5, w - 1, h - 1), 6, 6);

    const qreal icon = 12, left = 6;
    const QString path = o.value("p").toString();
    if (!path.isEmpty()) {
        const QString color = tone.name(QColor::HexRgb);
        const QString shape = o.value("m").toString() == "stroke"
            ? QString("<path fill=\"none\" stroke=\"%1\" stroke-width=\"1.9\" stroke-linecap=\"round\" stroke-linejoin=\"round\" d=\"%2\"/>")
            : QString("<path fill=\"%1\" d=\"%2\"/>");
        const QString svg = "<svg xmlns=\"http://www.w3.org/2000/svg\" viewBox=\"0 0 24 24\">" +
                            shape.arg(color, path.toHtmlEscaped()) + "</svg>";
        QSvgRenderer r(svg.toUtf8());
        if (r.isValid())
            r.render(&p, QRectF(left, (h - icon) / 2, icon, icon));
    }

    QFont font = QGuiApplication::font();
    font.setPixelSize(13);
    font.setWeight(QFont::Medium);
    p.setFont(font);
    QColor ink(o.value("t").toString());
    p.setPen(ink.isValid() ? ink : QColor(Qt::white));
    const qreal tx = left + icon + 5;
    p.drawText(QRectF(tx, 0, w - tx - 4, h), Qt::AlignLeft | Qt::AlignVCenter, o.value("n").toString());
    p.end();

    if (size)
        *size = img.size();
    return img;
}
