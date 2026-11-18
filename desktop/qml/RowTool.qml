import QtQuick
import Avora

// A small icon button laid over a list row on hover (archive, delete).
Rectangle {
    id: tool
    property string icon
    property string tip
    property bool danger: false
    readonly property bool hovered: area.containsMouse
    signal hit

    width: 24
    height: 24
    radius: Theme.radiusSm
    color: danger ? Qt.rgba(1, 0.54, 0.54, 0.14) : area.containsMouse ? Theme.active : "transparent"
    Behavior on color { ColorAnimation { duration: Theme.fast } }

    Icon {
        anchors.centerIn: parent
        name: tool.icon
        size: 14
        weight: 1.5
        color: tool.danger ? Theme.bad : area.containsMouse ? Theme.text : Theme.text3
    }
    MouseArea {
        id: area
        anchors.fill: parent
        hoverEnabled: true
        cursorShape: Qt.PointingHandCursor
        onClicked: tool.hit()
    }
}
