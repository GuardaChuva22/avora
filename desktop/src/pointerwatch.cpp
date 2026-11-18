#include "pointerwatch.h"

#include <QGuiApplication>
#include <QMouseEvent>
#include <QQuickItem>
#include <QQuickWindow>

PointerWatch::PointerWatch(QObject *parent)
    : QObject(parent)
{
    qApp->installEventFilter(this);
}

void PointerWatch::setTracking(bool on)
{
    if (on == m_tracking)
        return;
    m_tracking = on;
    if (on)
        QGuiApplication::setOverrideCursor(Qt::BlankCursor);
    else
        QGuiApplication::restoreOverrideCursor();
    emit trackingChanged();
}

void PointerWatch::watch(QQuickItem *view)
{
    if (view && !m_views.contains(view))
        m_views.append(view);
}

void PointerWatch::unwatch(QQuickItem *view)
{
    m_views.removeAll(view);
}

// shown tells if an item is drawn: visible and not faded out, up the chain,
// and inside every clipping ancestor at the point.
static bool shown(QQuickItem *item, QPointF scenePos)
{
    if (!item->isVisible())
        return false;
    for (QQuickItem *it = item; it; it = it->parentItem()) {
        if (it->opacity() <= 0)
            return false;
        if ((it == item || it->clip()) && !it->contains(it->mapFromScene(scenePos)))
            return false;
    }
    return true;
}

static int depth(QQuickItem *item)
{
    int n = 0;
    for (; item; item = item->parentItem())
        n++;
    return n;
}

QQuickItem *PointerWatch::viewAt(QObject *window, QPointF scenePos) const
{
    QQuickItem *best = nullptr;
    int bestDepth = -1;
    for (const auto &view : m_views) {
        if (!view || view->window() != window || !shown(view, scenePos))
            continue;
        // A view inside another (deeper in the tree) is the one under the pointer.
        const int d = depth(view);
        if (d > bestDepth) {
            best = view;
            bestDepth = d;
        }
    }
    return best;
}

bool PointerWatch::eventFilter(QObject *watched, QEvent *event)
{
    const QEvent::Type type = event->type();
    if (type != QEvent::MouseButtonPress && type != QEvent::MouseButtonRelease && type != QEvent::MouseMove
        && type != QEvent::MouseButtonDblClick)
        return false;
    if (!qobject_cast<QQuickWindow *>(watched) || !event->spontaneous())
        return false;
    auto *me = static_cast<QMouseEvent *>(event);
    const QPointF pos = me->scenePosition();

    switch (type) {
    case QEvent::MouseButtonPress:
    case QEvent::MouseButtonDblClick:
        m_claimed = false;
        emit pressed(pos, int(me->button()), viewAt(watched, pos));
        if (m_claimed)
            m_swallowRelease = true;
        return m_claimed;
    case QEvent::MouseButtonRelease: {
        if (m_tracking)
            emit released(pos, int(me->button()));
        const bool swallow = m_swallowRelease;
        if (me->buttons() == Qt::NoButton)
            m_swallowRelease = false;
        return swallow;
    }
    default:
        if (m_tracking)
            emit moved(pos);
        return false;
    }
}
