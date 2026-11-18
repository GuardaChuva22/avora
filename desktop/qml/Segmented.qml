import QtQuick
import QtQuick.Effects

// Pill selector with a glowing white thumb that slides to the choice.
Rectangle {
    id: seg
    property var options: [] // [{ value, label, icon? }]
    property string value
    signal activated(string value)

    readonly property int index: {
        for (var i = 0; i < options.length; i++)
            if (options[i].value === value)
                return i;
        return -1;
    }
    readonly property Item current: index >= 0 && repeater.count > index ? repeater.itemAt(index) : null

    implicitHeight: 38
    implicitWidth: row.implicitWidth + 8
    radius: height / 2
    color: Theme.well
    border.width: 1
    border.color: Theme.line

    RectangularShadow {
        visible: thumb.visible
        anchors.fill: thumb
        radius: thumb.radius
        blur: 12
        color: Qt.rgba(1, 1, 1, Theme.glow)
    }
    Rectangle {
        id: thumb
        visible: seg.current !== null
        x: seg.current ? row.x + seg.current.x : 0
        y: 4
        width: seg.current ? seg.current.width : 0
        height: seg.height - 8
        radius: height / 2
        color: "#ececf0"
        Behavior on x { NumberAnimation { duration: Theme.medium; easing.type: Theme.ease } }
        Behavior on width { NumberAnimation { duration: Theme.medium; easing.type: Theme.ease } }
    }
    Row {
        id: row
        x: 4
        anchors.verticalCenter: parent.verticalCenter
        Repeater {
            id: repeater
            model: seg.options
            delegate: Item {
                id: opt
                required property var modelData
                required property int index
                readonly property bool on: index === seg.index
                implicitWidth: content.implicitWidth + 28
                implicitHeight: seg.height - 8
                Row {
                    id: content
                    anchors.centerIn: parent
                    spacing: 6
                    Icon {
                        visible: !!opt.modelData.icon
                        name: opt.modelData.icon || ""
                        size: 14
                        color: label.color
                        anchors.verticalCenter: parent.verticalCenter
                    }
                    Text {
                        id: label
                        text: opt.modelData.label
                        font.pixelSize: Theme.fontBody
                        font.weight: opt.on ? Font.DemiBold : Font.Medium
                        color: opt.on ? Theme.ink : hover.containsMouse ? Theme.text : Theme.text3
                        Behavior on color { ColorAnimation { duration: Theme.fast } }
                        anchors.verticalCenter: parent.verticalCenter
                    }
                }
                MouseArea {
                    id: hover
                    anchors.fill: parent
                    hoverEnabled: true
                    cursorShape: Qt.PointingHandCursor
                    onClicked: seg.activated(opt.modelData.value)
                }
            }
        }
    }
}
