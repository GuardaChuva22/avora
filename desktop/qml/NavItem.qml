import QtQuick
import QtQuick.Effects

// Sidebar entry. The active one gets a lit bar on its left edge. In compact
// mode only the icon shows, and the label comes back as a tooltip.
Item {
    id: nav
    property string icon
    property string label
    property bool active: false
    property bool compact: false
    signal clicked

    implicitHeight: 40
    implicitWidth: 200

    Rectangle {
        anchors.fill: parent
        radius: Theme.radiusMd
        color: nav.active || area.pressed ? Theme.active : area.containsMouse ? Theme.hover : "transparent"
        border.width: nav.active ? 1 : 0
        border.color: Theme.active
        Behavior on color { ColorAnimation { duration: Theme.fast } }
    }
    RectangularShadow {
        anchors.fill: bar
        radius: 1
        blur: 8
        visible: color.a > 0
        color: Qt.rgba(1, 1, 1, nav.active ? 0.55 : 0)
        Behavior on color { ColorAnimation { duration: Theme.medium } }
    }
    Rectangle {
        id: bar
        x: -12
        width: 2
        height: nav.active ? 18 : 0
        anchors.verticalCenter: parent.verticalCenter
        radius: 1
        color: Theme.white
        Behavior on height { NumberAnimation { duration: Theme.medium; easing.type: Theme.ease } }
    }
    Row {
        x: nav.compact ? (nav.width - 17) / 2 : 12
        Behavior on x { NumberAnimation { duration: Theme.medium; easing.type: Theme.ease } }
        anchors.verticalCenter: parent.verticalCenter
        spacing: 11
        Icon {
            name: nav.icon
            size: 17
            color: nav.active ? Theme.white : area.containsMouse ? Theme.text : Theme.text3
            anchors.verticalCenter: parent.verticalCenter
        }
        Text {
            text: nav.label
            opacity: nav.compact ? 0 : 1
            visible: opacity > 0
            Behavior on opacity { NumberAnimation { duration: nav.compact ? 90 : Theme.medium } }
            font.pixelSize: Theme.fontLarge
            font.weight: nav.active ? Font.DemiBold : Font.Medium
            color: nav.active ? Theme.white : area.containsMouse ? Theme.text : Theme.text2
            anchors.verticalCenter: parent.verticalCenter
        }
    }
    MouseArea {
        id: area
        anchors.fill: parent
        hoverEnabled: true
        cursorShape: Qt.PointingHandCursor
        onClicked: nav.clicked()
    }
    Tip {
        text: nav.label
        show: nav.compact && area.containsMouse
    }
}
