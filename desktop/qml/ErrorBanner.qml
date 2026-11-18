import QtQuick

// Inline error with an alert icon, on a faint red wash.
Rectangle {
    property alias text: label.text
    visible: label.text !== ""
    implicitHeight: label.implicitHeight + 22
    radius: Theme.radiusMd
    color: Theme.badWash
    border.width: 1
    border.color: Theme.badLine

    Icon {
        id: icon
        name: "alert"
        size: 16
        color: Theme.bad
        x: 12
        y: 11
    }
    Text {
        id: label
        anchors { left: icon.right; leftMargin: 9; right: parent.right; rightMargin: 12; verticalCenter: parent.verticalCenter }
        color: "#ffc4c4"
        font.pixelSize: Theme.fontBody
        wrapMode: Text.Wrap
    }
}
