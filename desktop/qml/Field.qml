import QtQuick
import QtQuick.Controls.Basic
import QtQuick.Effects

// Single-line input on a dark well; focus lights the edge.
TextField {
    id: field
    property bool mono: false

    implicitHeight: 42
    leftPadding: 14
    rightPadding: 14
    color: Theme.text
    placeholderTextColor: Theme.text4
    selectionColor: Theme.wash6
    selectedTextColor: Theme.white
    font.family: mono ? Theme.mono : Theme.sans
    font.pixelSize: mono ? Theme.fontBody : Theme.fontLarge
    verticalAlignment: TextInput.AlignVCenter

    background: Item {
        RectangularShadow {
            anchors.fill: well
            radius: well.radius
            blur: 14
            visible: color.a > 0
            color: Qt.rgba(1, 1, 1, field.activeFocus ? 0.06 : 0)
            Behavior on color { ColorAnimation { duration: Theme.medium } }
        }
        Rectangle {
            id: well
            anchors.fill: parent
            radius: Theme.radiusMd
            color: Theme.well
            border.width: 1
            border.color: field.activeFocus ? Theme.focusLine : field.hovered ? Theme.lineStrong : Theme.line
            Behavior on border.color { ColorAnimation { duration: Theme.fast } }
        }
    }
}
