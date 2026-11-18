import QtQuick
import QtQuick.Controls.Basic

// Thin scrollbar: shows while the view scrolls or the pointer is on it,
// widens under the pointer so it is easy to grab, and fades out after.
ScrollBar {
    id: bar
    implicitWidth: 12
    padding: 3
    minimumSize: 0.08
    readonly property bool scrollable: size < 1
    readonly property bool shown: scrollable && (recent || hovered || pressed)
    property bool recent: false

    onPositionChanged: {
        recent = true;
        idle.restart();
    }
    Timer {
        id: idle
        interval: 1000
        onTriggered: bar.recent = false
    }

    contentItem: Item {
        implicitWidth: 6
        opacity: bar.shown ? 1 : 0
        Behavior on opacity { NumberAnimation { duration: bar.shown ? 90 : 300; easing.type: Easing.OutQuad } }
        Rectangle {
            anchors { right: parent.right; top: parent.top; bottom: parent.bottom }
            width: bar.hovered || bar.pressed ? 6 : 4
            radius: width / 2
            color: bar.pressed ? Qt.rgba(1, 1, 1, 0.42) : Qt.rgba(1, 1, 1, bar.hovered ? 0.30 : 0.18)
            Behavior on width { NumberAnimation { duration: 120; easing.type: Easing.OutQuad } }
        }
    }
    background: Rectangle {
        radius: Theme.radiusSm
        color: Qt.rgba(1, 1, 1, bar.scrollable && (bar.hovered || bar.pressed) ? 0.035 : 0)
    }
}
