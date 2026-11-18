import QtQuick
import QtQuick.Controls.Basic

// A modal sheet: dims the page and centers one card whose content scrolls.
// A click outside or Esc closes it. Every editor (funcionário, canal,
// projeto, rotina, skill) is a Sheet, so they all open, look and close the
// same way. Children go into a Column (body) with the sheet's spacing.
Item {
    id: root
    default property alias content: body.data
    property bool shown: false
    readonly property alias form: body
    property int sheetWidth: 620

    visible: opacity > 0
    opacity: shown ? 1 : 0
    Behavior on opacity { NumberAnimation { duration: Theme.medium } }
    z: 50

    // Dim the page; a click outside closes.
    Rectangle {
        anchors.fill: parent
        color: Theme.scrim
        MouseArea {
            anchors.fill: parent
            onClicked: root.shown = false
        }
    }
    Shortcut {
        sequence: "Esc"
        enabled: root.shown
        onActivated: root.shown = false
    }

    Card {
        width: Math.min(root.sheetWidth, parent.width - 2 * Theme.space5)
        height: Math.min(body.implicitHeight + 2 * padding, parent.height - 2 * Theme.space5)
        anchors.centerIn: parent
        padding: 28
        radius: Theme.radius
        transform: Translate {
            y: root.shown ? 0 : 8
            Behavior on y { NumberAnimation { duration: Theme.slow; easing.type: Theme.ease } }
        }
        MouseArea { anchors.fill: parent } // keep clicks inside

        Flickable {
            anchors.fill: parent
            contentHeight: body.implicitHeight
            clip: true
            boundsBehavior: Flickable.StopAtBounds
            ScrollBar.vertical: SlimScrollBar {}
            SmoothWheel {}

            Column {
                id: body
                width: parent.width
                spacing: 20
            }
        }
    }
}
