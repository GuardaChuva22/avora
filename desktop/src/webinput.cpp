#include "webinput.h"

#include <QBuffer>
#include <QCoreApplication>
#include <QGuiApplication>
#include <QImage>
#include <QInputMethodEvent>
#include <QKeyEvent>
#include <QMouseEvent>
#include <QQuickItem>
#include <QQuickWindow>
#include <QWheelEvent>

namespace {

QQuickWindow *windowOf(QQuickItem *view)
{
    return view && view->window() && view->isVisible() ? view->window() : nullptr;
}

} // namespace

bool WebInput::click(QQuickItem *view, qreal x, qreal y)
{
    QQuickWindow *w = windowOf(view);
    if (!w || x < 0 || y < 0 || x > view->width() || y > view->height())
        return false;
    view->forceActiveFocus();
    const QPointF scene = view->mapToScene(QPointF(x, y));
    const QPointF global = w->mapToGlobal(scene);
    QMouseEvent move(QEvent::MouseMove, scene, scene, global, Qt::NoButton, Qt::NoButton, Qt::NoModifier);
    QCoreApplication::sendEvent(w, &move);
    QMouseEvent press(QEvent::MouseButtonPress, scene, scene, global, Qt::LeftButton, Qt::LeftButton, Qt::NoModifier);
    QCoreApplication::sendEvent(w, &press);
    QMouseEvent release(QEvent::MouseButtonRelease, scene, scene, global, Qt::LeftButton, Qt::NoButton, Qt::NoModifier);
    QCoreApplication::sendEvent(w, &release);
    return true;
}

bool WebInput::wheel(QQuickItem *view, int dy)
{
    QQuickWindow *w = windowOf(view);
    if (!w)
        return false;
    const QPointF scene = view->mapToScene(QPointF(view->width() / 2, view->height() / 2));
    const QPointF global = w->mapToGlobal(scene);
    // dy pixels down (positive) or up, in notches of 120 (about 100 px each).
    const int notches = qMax(1, qAbs(dy) / 100);
    for (int i = 0; i < notches; ++i) {
        QWheelEvent ev(scene, global, QPoint(), QPoint(0, dy > 0 ? -120 : 120), Qt::NoButton, Qt::NoModifier, Qt::NoScrollPhase, false);
        QCoreApplication::sendEvent(w, &ev);
    }
    return true;
}

bool WebInput::key(QQuickItem *view, const QString &name)
{
    QQuickWindow *w = windowOf(view);
    if (!w)
        return false;
    struct K {
        Qt::Key key;
        QString text;
        Qt::KeyboardModifiers mods;
    };
    const QString n = name.trimmed().toLower();
    K k;
    if (n == "enter")
        k = {Qt::Key_Return, QStringLiteral("\r"), Qt::NoModifier};
    else if (n == "esc" || n == "escape")
        k = {Qt::Key_Escape, QString(QChar(27)), Qt::NoModifier};
    else if (n == "tab")
        k = {Qt::Key_Tab, QStringLiteral("\t"), Qt::NoModifier};
    else if (n == "pagedown")
        k = {Qt::Key_PageDown, {}, Qt::NoModifier};
    else if (n == "pageup")
        k = {Qt::Key_PageUp, {}, Qt::NoModifier};
    else if (n == "baixo" || n == "down")
        k = {Qt::Key_Down, {}, Qt::NoModifier};
    else if (n == "cima" || n == "up")
        k = {Qt::Key_Up, {}, Qt::NoModifier};
    else if (n == "end")
        k = {Qt::Key_End, {}, Qt::NoModifier};
    else if (n == "home")
        k = {Qt::Key_Home, {}, Qt::NoModifier};
    else if (n == "backspace")
        k = {Qt::Key_Backspace, {}, Qt::NoModifier};
    else if (n == "selectall")
        k = {Qt::Key_A, QStringLiteral("a"), Qt::ControlModifier};
    else
        return false;
    QObject *target = w->focusObject() ? w->focusObject() : static_cast<QObject *>(w);
    QKeyEvent press(QEvent::KeyPress, k.key, k.mods, k.text);
    QCoreApplication::sendEvent(target, &press);
    QKeyEvent release(QEvent::KeyRelease, k.key, k.mods, k.text);
    QCoreApplication::sendEvent(target, &release);
    return true;
}

bool WebInput::insertText(QQuickItem *view, const QString &text)
{
    QQuickWindow *w = windowOf(view);
    if (!w || !w->focusObject())
        return false;
    QInputMethodEvent ev;
    ev.setCommitString(text);
    QCoreApplication::sendEvent(w->focusObject(), &ev);
    return true;
}

QString WebInput::grab(QQuickItem *view, int maxWidth, int quality)
{
    QQuickWindow *w = windowOf(view);
    if (!w || view->width() < 10 || view->height() < 10)
        return {};
    const QImage all = w->grabWindow();
    if (all.isNull())
        return {};
    const qreal dpr = all.devicePixelRatio() > 0 ? all.devicePixelRatio() : w->devicePixelRatio();
    const QRectF r = view->mapRectToScene(QRectF(0, 0, view->width(), view->height()));
    QImage img = all.copy(QRect(qRound(r.x() * dpr), qRound(r.y() * dpr), qRound(r.width() * dpr), qRound(r.height() * dpr)));
    if (img.width() > maxWidth)
        img = img.scaledToWidth(maxWidth, Qt::SmoothTransformation);
    QByteArray bytes;
    QBuffer buf(&bytes);
    buf.open(QIODevice::WriteOnly);
    img.convertToFormat(QImage::Format_RGB32).save(&buf, "JPG", quality);
    return QString::fromLatin1(bytes.toBase64());
}
