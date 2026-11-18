import QtQuick

// Middle-click autoscroll, like a browser: click the wheel to drop an anchor,
// then the farther the pointer goes above or below it, the faster the view
// runs. Click again (any button) or Esc to stop; press, drag and release
// scrolls only while held. SmoothWheel adds one to every Flickable it serves.
//
// The mouse is heard at the window (PointerWatch, in C++), before any item:
// a message's selectable text or buttons took the middle press for
// themselves, so a handler on the view missed it. At rest nothing covers the
// view; this item only draws the anchor and the pointer marks while running,
// and the system cursor hides meanwhile.
Item {
    id: auto
    property Flickable flickable
    property bool on: false
    property point origin
    property point pointer
    property bool dragged: false
    property bool held: false // the middle button is still down since the start

    anchors.fill: parent
    z: 1000
    visible: on

    function bounds() {
        const f = flickable;
        const min = f.originY - f.topMargin;
        return [min, Math.max(min, f.originY + f.contentHeight + f.bottomMargin - f.height)];
    }
    function stop() {
        on = false;
        held = false;
    }

    onOnChanged: {
        PointerWatch.tracking = on;
        if (on)
            keys.forceActiveFocus();
    }
    Component.onCompleted: PointerWatch.watch(flickable)
    Component.onDestruction: {
        PointerWatch.unwatch(flickable);
        if (on)
            PointerWatch.tracking = false;
    }

    Connections {
        target: PointerWatch
        function onPressed(scenePos, button, view) {
            if (auto.on) {
                // Any press while running stops it, and goes no further.
                auto.stop();
                PointerWatch.claimed = true;
                return;
            }
            if (button !== Qt.MiddleButton || PointerWatch.claimed || view !== auto.flickable)
                return;
            const [min, max] = auto.bounds();
            if (max <= min)
                return;
            PointerWatch.claimed = true;
            auto.origin = auto.mapFromItem(null, scenePos.x, scenePos.y);
            auto.pointer = auto.origin;
            auto.dragged = false;
            auto.held = true;
            auto.on = true;
        }
        function onMoved(scenePos) {
            if (!auto.on)
                return;
            auto.pointer = auto.mapFromItem(null, scenePos.x, scenePos.y);
            if (auto.held && Math.abs(auto.pointer.y - auto.origin.y) > 12)
                auto.dragged = true;
        }
        function onReleased(scenePos, button) {
            if (!auto.on || button !== Qt.MiddleButton || !auto.held)
                return;
            auto.held = false;
            // Press, drag, release: scrolled only while held.
            if (auto.dragged)
                auto.stop();
        }
    }

    Item {
        id: keys
        Keys.onEscapePressed: auto.stop()
    }
    Connections {
        target: auto.flickable
        function onVisibleChanged() {
            if (!auto.flickable.visible)
                auto.stop();
        }
    }

    FrameAnimation {
        running: auto.on
        onTriggered: {
            const dy = auto.pointer.y - auto.origin.y;
            const dead = 12;
            if (Math.abs(dy) <= dead)
                return;
            // px/s grows a bit faster than the distance: fine near, quick far.
            const speed = Math.sign(dy) * Math.pow(Math.abs(dy) - dead, 1.25) * 4;
            const [min, max] = auto.bounds();
            const f = auto.flickable;
            f.contentY = Math.max(min, Math.min(max, f.contentY + speed * Math.min(frameTime, 0.05)));
        }
    }

    // The anchor: a small disc with up and down marks where you clicked.
    Rectangle {
        x: auto.origin.x - width / 2
        y: auto.origin.y - height / 2
        width: 30
        height: 30
        radius: Theme.radiusLg
        color: Qt.rgba(0.05, 0.05, 0.06, 0.92)
        border.width: 1
        border.color: Theme.lineStrong
        Icon {
            anchors { horizontalCenter: parent.horizontalCenter; top: parent.top; topMargin: 2 }
            name: "chevron"
            size: 12
            rotation: -90
            color: Theme.text2
        }
        Rectangle {
            anchors.centerIn: parent
            width: 4
            height: 4
            radius: 2
            color: Theme.text2
        }
        Icon {
            anchors { horizontalCenter: parent.horizontalCenter; bottom: parent.bottom; bottomMargin: 2 }
            name: "chevron"
            size: 12
            rotation: 90
            color: Theme.text2
        }
    }

    // The pointer, while the system cursor is hidden: a dot in the still
    // zone, an arrow toward where the view runs outside it.
    Item {
        readonly property real dy: auto.pointer.y - auto.origin.y
        readonly property bool still: Math.abs(dy) <= 12
        x: auto.pointer.x - width / 2
        y: auto.pointer.y - height / 2
        width: 18
        height: 18
        visible: !still || Math.abs(auto.pointer.x - auto.origin.x) > 12
        Rectangle {
            visible: parent.still
            anchors.centerIn: parent
            width: 6
            height: 6
            radius: 3
            color: Theme.text
            border.width: 1
            border.color: Theme.scrim
        }
        Icon {
            visible: !parent.still
            anchors.centerIn: parent
            name: "chevron"
            size: 16
            rotation: parent.dy < 0 ? -90 : 90
            color: Theme.text
        }
    }
}
