#pragma once

#include <QObject>
#include <QString>

class QQuickItem;

// WebInput drives the embedded browser like a person would: real mouse,
// wheel and keyboard events delivered to the window at the page's
// coordinates (sites see trusted input, not script clicks), and a JPEG of
// the view for the agents' prints and the server's live view.
class WebInput : public QObject {
    Q_OBJECT
public:
    using QObject::QObject;

    // x, y are CSS pixels in the view (zoom 1).
    Q_INVOKABLE bool click(QQuickItem *view, qreal x, qreal y);
    Q_INVOKABLE bool wheel(QQuickItem *view, int dy);
    // key: enter, esc, tab, pagedown, pageup, down, up, end, home, selectall.
    Q_INVOKABLE bool key(QQuickItem *view, const QString &key);
    // insertText types text where the focus is (a committed input method
    // string: accents and emoji go in as they are).
    Q_INVOKABLE bool insertText(QQuickItem *view, const QString &text);
    // grab returns the view as a base64 JPEG ("" when it is not on screen).
    Q_INVOKABLE QString grab(QQuickItem *view, int maxWidth = 1280, int quality = 55);
};
