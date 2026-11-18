import QtQuick

// A pill that is on or off (a choice among a few, a filter, a member).
// "dim" fades it while off, for the less likely options.
Rectangle {
    id: pill
    property string text
    property bool on: false
    property bool dim: false
    signal hit

    height: 32
    width: pillText.implicitWidth + 26
    radius: height / 2
    color: on ? Theme.wash4 : pillArea.containsMouse ? Theme.hover : "transparent"
    border.width: 1
    border.color: on ? Theme.focusLine : Theme.line
    opacity: dim && !on ? 0.55 : 1
    Behavior on color { ColorAnimation { duration: Theme.fast } }

    Text {
        id: pillText
        anchors.centerIn: parent
        text: pill.text
        color: pill.on ? Theme.white : Theme.text2
        font.pixelSize: Theme.fontBody
        font.weight: pill.on ? Font.DemiBold : Font.Normal
    }
    MouseArea {
        id: pillArea
        anchors.fill: parent
        hoverEnabled: true
        cursorShape: Qt.PointingHandCursor
        onClicked: pill.hit()
    }
}
