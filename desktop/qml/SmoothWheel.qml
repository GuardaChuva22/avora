import QtQuick

// Wheel scrolling for a Flickable, like a browser: each notch adds to a goal
// and the view eases toward it every frame (~100 ms), so fast notches chain
// into one continuous move instead of restarting a tween. A touchpad moves
// pixel for pixel. Put it inside the Flickable.
//
// A middle click starts browser-style autoscroll (AutoScroll.qml).
//
// It also turns off dragging the content with the mouse (a phone gesture
// that fought text selection and clicks on desktop); set mouseDrag to keep it.
WheelHandler {
    id: wh
    // Declared inside a Flickable, the handler lands on its contentItem.
    property Item flickable: parent ? parent.parent : null
    property real notch: 120 // pixels per wheel notch
    property bool mouseDrag: false

    target: null
    acceptedDevices: PointerDevice.Mouse | PointerDevice.TouchPad

    property real goal: 0
    property bool gliding: false
    // True while the owner is scrolling (and a moment after), so views can
    // tell a user scroll from content growing under them.
    readonly property bool driving: gliding || settle.running || (auto !== null && auto.on)
    property var auto: null // the middle-click autoscroll made for this view
    readonly property Timer settle: Timer { interval: 200 }

    Component.onCompleted: {
        if (!flickable)
            return;
        if (!mouseDrag)
            flickable.interactive = false;
        // Parented to the Flickable itself (not its content), so it stays put.
        const c = Qt.createComponent("AutoScroll.qml");
        if (c.status === Component.Ready)
            auto = c.createObject(flickable, { flickable: flickable });
        else
            console.warn("AutoScroll:", c.errorString());
    }

    function bounds() {
        const f = flickable;
        const min = f.originY - f.topMargin;
        return [min, Math.max(min, f.originY + f.contentHeight + f.bottomMargin - f.height)];
    }
    function clamp(y) {
        const [min, max] = bounds();
        return Math.max(min, Math.min(max, y));
    }

    onWheel: event => {
        settle.restart();
        const f = flickable;
        if (event.pixelDelta.y !== 0) {
            gliding = false;
            f.contentY = clamp(f.contentY - event.pixelDelta.y);
        } else if (event.angleDelta.y !== 0) {
            const from = gliding ? goal : f.contentY;
            goal = clamp(from - event.angleDelta.y / 120 * notch);
            placed = f.contentY;
            gliding = goal !== f.contentY;
        }
        event.accepted = true;
    }

    // Where the glide last put the view. Anything else that moves it (the
    // scrollbar, autoscroll, a jump to the newest message) ends the glide,
    // or the two would pull against each other and the view would shake.
    property real placed: 0
    function stop() {
        gliding = false;
    }

    // Runs only while gliding; idle it costs nothing.
    readonly property FrameAnimation frames: FrameAnimation {
        running: wh.gliding
        onTriggered: {
            const f = wh.flickable;
            if (Math.abs(f.contentY - wh.placed) > 1) {
                wh.gliding = false;
                return;
            }
            const goal = wh.clamp(wh.goal); // content may have grown or shrunk
            const d = goal - f.contentY;
            if (Math.abs(d) < 0.5) {
                f.contentY = goal;
                wh.gliding = false;
                return;
            }
            f.contentY += d * (1 - Math.exp(-Math.min(frameTime, 0.05) * 20));
            wh.placed = f.contentY;
        }
    }
}
