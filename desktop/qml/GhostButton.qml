import QtQuick

// Quiet button: hairline outline, lifts on hover. "danger" tints it red,
// "round" makes it an icon-only circle.
Rectangle {
    id: btn
    property string text
    property string icon
    property bool danger: false
    property bool round: false
    property bool small: false
    readonly property bool hovered: area.containsMouse
    signal clicked

    readonly property color tone: danger ? Theme.bad : Theme.text2
    implicitHeight: small ? 30 : 36
    implicitWidth: round ? implicitHeight : row.implicitWidth + (small ? 22 : 28)
    radius: height / 2
    opacity: enabled ? 1 : 0.4
    scale: area.pressed ? 0.97 : 1
    Behavior on scale { NumberAnimation { duration: Theme.fast; easing.type: Theme.ease } }
    color: area.pressed ? Theme.active : area.containsMouse ? Theme.hover : "transparent"
    border.width: 1
    border.color: danger ? Qt.rgba(1, 0.54, 0.54, area.containsMouse ? 0.45 : 0.25) : btn.activeFocus ? Theme.focusLine : area.containsMouse ? Theme.lineStrong : Theme.line
    Behavior on color { ColorAnimation { duration: Theme.fast } }
    Behavior on border.color { ColorAnimation { duration: Theme.fast } }

    Row {
        id: row
        anchors.centerIn: parent
        spacing: 7
        Icon {
            visible: btn.icon !== ""
            name: btn.icon
            color: area.containsMouse && !btn.danger ? Theme.white : btn.tone
            size: btn.small ? 14 : 16
            anchors.verticalCenter: parent.verticalCenter
        }
        Text {
            visible: btn.text !== ""
            text: btn.text
            color: area.containsMouse && !btn.danger ? Theme.white : btn.tone
            font.pixelSize: btn.small ? Theme.fontSmall : Theme.fontBody
            font.weight: Font.Medium
            anchors.verticalCenter: parent.verticalCenter
        }
    }
    MouseArea {
        id: area
        anchors.fill: parent
        hoverEnabled: true
        cursorShape: Qt.PointingHandCursor
        onClicked: btn.clicked()
    }
}
