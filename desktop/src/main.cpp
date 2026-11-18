#include "api.h"
#include "avatar.h"
#include "serverimage.h"
#include "webinput.h"
#include "pointerwatch.h"

#include <QtWebEngineQuick/qtwebenginequickglobal.h>
#include "chrome.h"
#include "icons.h"

#include <QCommandLineParser>
#include <QQmlContext>
#include <QCursor>
#include <QMouseEvent>
#include <QDir>
#include <QFile>
#include <QFont>
#include <QGuiApplication>
#include <QIcon>
#include <QImage>
#include <QPainter>
#include <QQmlApplicationEngine>
#include <QQuickStyle>
#include <QQuickWindow>
#include <QSvgRenderer>
#include <QTimer>

namespace {

constexpr auto kIcon = ":/qt/qml/Avora/resources/icon.svg";

// renderIcon writes the multi-size .ico used for the .exe resource.
int renderIcon(const QString &out)
{
    QSvgRenderer svg{QString(kIcon)};
    QImage img(256, 256, QImage::Format_ARGB32_Premultiplied);
    img.fill(Qt::transparent);
    QPainter p(&img);
    p.setRenderHint(QPainter::Antialiasing);
    svg.render(&p);
    p.end();
    return img.save(out, "ICO") ? 0 : 1;
}

} // namespace

int main(int argc, char *argv[])
{
    // The embedded browser shares GL contexts with the scene; set up first.
    QtWebEngineQuick::initialize();
    QGuiApplication app(argc, argv);
    QGuiApplication::setOrganizationName("Avora");
    QGuiApplication::setApplicationName("Desktop");
    QGuiApplication::setApplicationDisplayName("Avora");
    QGuiApplication::setWindowIcon(QIcon(kIcon));

    QCommandLineParser cli;
    cli.addOptions({
        {"profile", "Isolated settings and token (for testing).", "name"},
        {"grab", "Render the window to a PNG after a moment and quit (visual QA).", "file"},
        {"page", "Page to open: chat, atividade, nos.", "name"},
        {"grab-delay", "Milliseconds before --grab (default 3500).", "ms"},
        {"size", "Window size WxH.", "size"},
        {"render-icon", "Write the app icon as .ico and quit.", "file"},
        {"server", "Server URL to sign in to (test profiles only).", "url"},
        {"token-file", "File with a token to sign in with (test profiles only).", "file"},
        {"probe-cursor", "Hover x,y;x,y;... inside the window after a moment, print the cursor shape at each and quit (QA).", "points"},
    });
    cli.process(app);

    if (cli.isSet("render-icon"))
        return renderIcon(cli.value("render-icon"));
    // A test profile never touches the real settings or the real token.
    if (cli.isSet("profile"))
        QGuiApplication::setApplicationName("Desktop-" + cli.value("profile"));

    QFont font("Segoe UI Variable Text");
    font.setPixelSize(14);
    // Grey antialiasing (no ClearType fringes) with the system's own glyph
    // rasterizer: crisp at 96 DPI, bold included.
    font.setStyleStrategy(QFont::NoSubpixelAntialias);
    QGuiApplication::setFont(font);
    QQuickStyle::setStyle("Basic");
    // Native rendering: the system rasterizes each glyph once into a cache.
    // Curve rendering (used before) drew small bold text blocky at 96 DPI
    // and built geometry for every glyph, which made long answers with
    // tables slow. AVORA_TEXT_RENDER (curve, qt) switches it for QA.
    const QByteArray textRender = qgetenv("AVORA_TEXT_RENDER");
    QQuickWindow::setTextRenderType(textRender == "qt"      ? QQuickWindow::QtTextRendering
                                    : textRender == "curve" ? QQuickWindow::CurveTextRendering
                                                            : QQuickWindow::NativeTextRendering);

    QQmlApplicationEngine engine;
    engine.addImageProvider("avatar", new AvatarProvider);
    engine.addImageProvider("svg", new SvgIconProvider);
    engine.addImageProvider("chip", new ChipProvider);
    engine.addImageProvider("server", new ServerImageProvider);
    engine.rootContext()->setContextProperty("WebInput", new WebInput(&app));
    engine.rootContext()->setContextProperty("PointerWatch", new PointerWatch(&app));
    // Automatic sign-in exists only for isolated test profiles, so the real
    // admin token is only ever typed or pasted by its owner.
    QString autoServer, autoToken;
    if (cli.isSet("profile") && cli.isSet("token-file")) {
        QFile f(cli.value("token-file"));
        if (f.open(QIODevice::ReadOnly))
            autoToken = QString::fromUtf8(f.readAll()).trimmed();
        autoServer = cli.value("server");
    }
    engine.setInitialProperties({
        {"startPage", cli.value("page")},
        {"autoServer", autoServer},
        {"autoToken", autoToken},
    });
    QObject::connect(&engine, &QQmlApplicationEngine::objectCreationFailed, &app, [] { QCoreApplication::exit(1); }, Qt::QueuedConnection);
    engine.loadFromModule("Avora", "Main");

    auto *window = engine.rootObjects().isEmpty() ? nullptr : qobject_cast<QQuickWindow *>(engine.rootObjects().first());
    if (!window)
        return 1;

    if (cli.isSet("size")) {
        const QStringList wh = cli.value("size").split('x');
        if (wh.size() == 2)
            window->resize(wh[0].toInt(), wh[1].toInt());
    }
    applyChrome(window);

    // Visual QA for cursors: a synthetic hover inside the window (the real
    // mouse is never touched), then the shape Qt Quick picked.
    if (cli.isSet("probe-cursor")) {
        const QString points = cli.value("probe-cursor");
        QTimer::singleShot(3500, window, [window, points] {
            for (const QString &p : points.split(';', Qt::SkipEmptyParts)) {
                const QStringList xy = p.split(',');
                if (xy.size() != 2)
                    continue;
                const QPointF pos(xy[0].toDouble(), xy[1].toDouble());
                QMouseEvent move(QEvent::MouseMove, pos, window->mapToGlobal(pos), Qt::NoButton, Qt::NoButton, Qt::NoModifier);
                QCoreApplication::sendEvent(window, &move);
                QCoreApplication::processEvents();
                qWarning().noquote() << "cursor" << p << "=" << int(window->cursor().shape());
            }
            QCoreApplication::quit();
        });
    }

    if (cli.isSet("grab")) {
        const QString out = cli.value("grab");
        const int delay = cli.isSet("grab-delay") ? cli.value("grab-delay").toInt() : 3500;
        QTimer::singleShot(delay, window, [window, out] {
            window->grabWindow().save(out);
            QCoreApplication::quit();
        });
    }
    return app.exec();
}
