import QtQuick
import QtQuick.Effects

// The one bright thing on a screen: a flat white pill. It glows only
// under the pointer or with keyboard focus.
Item {
    id: btn
    property string text
    property string icon
    property bool busy: false
    signal clicked

    implicitHeight: 42
    implicitWidth: row.implicitWidth + 44
    opacity: enabled ? 1 : 0.32
    activeFocusOnTab: true
    Behavior on opacity { NumberAnimation { duration: Theme.medium } }


    RectangularShadow {
        anchors.fill: face
        radius: face.radius
        blur: 16
        visible: color.a > 0
        color: Qt.rgba(1, 1, 1, btn.enabled && (area.containsMouse || btn.activeFocus) ? 0.16 : 0)
        Behavior on blur { NumberAnimation { duration: Theme.medium } }
        Behavior on color { ColorAnimation { duration: Theme.medium } }
    }
    Rectangle {
        id: face
        anchors.fill: parent
        radius: height / 2
        scale: area.pressed ? 0.975 : 1
        Behavior on scale { NumberAnimation { duration: Theme.fast; easing.type: Theme.ease } }
        color: area.pressed ? "#dcdce2" : area.containsMouse ? "#ffffff" : "#ececf0"
        Behavior on color { ColorAnimation { duration: Theme.fast } }
    }
    Row {
        id: row
        anchors.centerIn: face
        spacing: 8
        scale: face.scale
        Icon {
            visible: btn.icon !== ""
            name: btn.busy ? "refresh" : btn.icon
            color: Theme.ink
            size: 16
            weight: 1.9
            anchors.verticalCenter: parent.verticalCenter
            RotationAnimation on rotation {
                running: btn.busy
                from: 0; to: 360; duration: 900; loops: Animation.Infinite
            }
        }
        Text {
            text: btn.text
            color: Theme.ink
            font.pixelSize: Theme.fontLarge
            font.weight: Font.DemiBold
            anchors.verticalCenter: parent.verticalCenter
        }
    }
    MouseArea {
        id: area
        anchors.fill: parent
        hoverEnabled: true
        cursorShape: btn.enabled ? Qt.PointingHandCursor : Qt.ArrowCursor
        onClicked: btn.clicked()
    }
    Keys.onReturnPressed: btn.clicked()
    Keys.onSpacePressed: btn.clicked()
}
