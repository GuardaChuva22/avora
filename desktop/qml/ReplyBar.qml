import QtQuick
import Avora

// "Respondendo a Atlas: …" above the composer, with an x to cancel.
Rectangle {
    id: bar
    property var target: null // { id, who, content }
    signal cancel

    visible: target !== null
    height: visible ? 34 : 0
    radius: Theme.radiusMd
    color: Theme.hover
    border.width: 1
    border.color: Theme.line

    Rectangle {
        x: 0
        y: 7
        width: 3
        height: parent.height - 14
        radius: 1.5
        color: Theme.text3
    }
    Icon {
        id: arrow
        x: 12
        anchors.verticalCenter: parent.verticalCenter
        name: "reply"
        size: 14
        color: Theme.text3
    }
    Text {
        anchors { left: arrow.right; leftMargin: 8; right: close.left; rightMargin: 8; verticalCenter: parent.verticalCenter }
        textFormat: Text.StyledText
        text: bar.target ? "Respondendo a <b>" + bar.target.who + "</b>  <font color='" + Theme.text4 + "'>" +
                           bar.target.content.replace(/[*_`#>]/g, "").replace(/\s+/g, " ").replace(/&/g, "&amp;").replace(/</g, "&lt;").slice(0, 160) + "</font>"
                         : ""
        color: Theme.text2
        font.pixelSize: Theme.fontSmall
        elide: Text.ElideRight
    }
    Rectangle {
        id: close
        anchors { right: parent.right; rightMargin: 6; verticalCenter: parent.verticalCenter }
        width: 24
        height: 24
        radius: Theme.radiusSm
        color: closeArea.containsMouse ? Theme.active : "transparent"
        Icon {
            anchors.centerIn: parent
            name: "close"
            size: 13
            color: Theme.text3
        }
        MouseArea {
            id: closeArea
            anchors.fill: parent
            hoverEnabled: true
            cursorShape: Qt.PointingHandCursor
            onClicked: bar.cancel()
        }
    }
}
