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
#include <QTextStream>
#include <cstdlib>

// QA: with AVORA_QA_LOG set, every Qt/QML message is appended to that file
// (a GUI app on Windows has no console to print to).
static void qaLog(QtMsgType, const QMessageLogContext &, const QString &msg)
{
    static QFile file(QString::fromLocal8Bit(qgetenv("AVORA_QA_LOG")));
    if (!file.isOpen() && !file.open(QIODevice::Append | QIODevice::Text))
        return;
    QTextStream(&file) << msg << Qt::endl;
    file.flush();
}

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
    if (qEnvironmentVariableIsSet("AVORA_QA_LOG"))
        qInstallMessageHandler(qaLog);
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
        {"click", "Click x,y;x,y;... inside the window after a moment, 1.5 s apart, before --grab (QA).", "points"},
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

    // Visual QA for navigation: synthetic clicks (the real mouse is never
    // touched), 1.5 s apart, starting at 3.5 s; --grab-delay should cover them.
    if (cli.isSet("click")) {
        const QStringList points = cli.value("click").split(';', Qt::SkipEmptyParts);
        for (int i = 0; i < points.size(); ++i) {
            const QStringList xy = points[i].split(',');
            if (xy.size() != 2)
                continue;
            const QPointF pos(xy[0].toDouble(), xy[1].toDouble());
            QTimer::singleShot(3500 + i * 1500, window, [window, pos] {
                const QPointF global = window->mapToGlobal(pos);
                QMouseEvent move(QEvent::MouseMove, pos, global, Qt::NoButton, Qt::NoButton, Qt::NoModifier);
                QCoreApplication::sendEvent(window, &move);
                QMouseEvent press(QEvent::MouseButtonPress, pos, global, Qt::LeftButton, Qt::LeftButton, Qt::NoModifier);
                QCoreApplication::sendEvent(window, &press);
                // Who took the press: the item (or handler) and its parents.
                if (!press.points().isEmpty()) {
                    QObject *g = press.exclusiveGrabber(press.points().first());
                    QString chain;
                    for (QObject *o = g; o; o = o->parent())
                        chain += QString(" < %1(%2)").arg(o->metaObject()->className(), o->objectName());
                    qWarning().noquote() << "grabber" << pos << (g ? chain : QString("none"));
                }
                // Popups that are open, even when nothing of them shows.
                for (QObject *o : window->findChildren<QObject *>()) {
                    if (o->inherits("QQuickPopup") && (o->property("opened").toBool() || o->property("visible").toBool())) {
                        QString chain;
                        for (QObject *p = o; p; p = p->parent())
                            chain += QString(" < %1(%2)").arg(p->metaObject()->className(), p->objectName());
                        qWarning().noquote() << "open popup" << chain << "opened" << o->property("opened").toBool() << "visible" << o->property("visible").toBool() << "modal" << o->property("modal").toBool() << "dim" << o->property("dim").toBool()
                                             << "w" << o->property("width").toDouble() << "h" << o->property("height").toDouble();
                    }
                }
                QMouseEvent release(QEvent::MouseButtonRelease, pos, global, Qt::LeftButton, Qt::NoButton, Qt::NoModifier);
                QCoreApplication::sendEvent(window, &release);
                qWarning().noquote() << "clicked" << pos;
            });
        }
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
