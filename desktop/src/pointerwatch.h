#pragma once

#include <QList>
#include <QObject>
#include <QPointF>
#include <QPointer>

class QQuickItem;

// PointerWatch hears the mouse at the window, before any item does, for
// browser-style middle-click autoscroll (AutoScroll.qml). Items inside a
// view (selectable text, buttons) take presses for themselves, so a handler
// on the view never heard a middle click over a message.
//
// Each scrolling view registers itself with watch(). On each real press it
// emits pressed() with the view under the pointer (the innermost visible
// one, or null); a listener that takes the press sets claimed, and the press
// and its release go no further. While tracking, it also reports moves and
// releases, and hides the system cursor (the autoscroll marks stand in for
// it).
class PointerWatch : public QObject {
    Q_OBJECT
    Q_PROPERTY(bool tracking READ tracking WRITE setTracking NOTIFY trackingChanged)
    Q_PROPERTY(bool claimed MEMBER m_claimed)
public:
    explicit PointerWatch(QObject *parent = nullptr);

    bool tracking() const { return m_tracking; }
    void setTracking(bool on);

    Q_INVOKABLE void watch(QQuickItem *view);
    Q_INVOKABLE void unwatch(QQuickItem *view);

signals:
    void pressed(QPointF scenePos, int button, QQuickItem *view);
    void moved(QPointF scenePos);
    void released(QPointF scenePos, int button);
    void trackingChanged();

protected:
    bool eventFilter(QObject *watched, QEvent *event) override;

private:
    QQuickItem *viewAt(QObject *window, QPointF scenePos) const;

    QList<QPointer<QQuickItem>> m_views;
    bool m_tracking = false;
    bool m_claimed = false;
    bool m_swallowRelease = false;
};
