import QtQuick
import Avora

// Small actions under a message, shown on hover: copy, reply, details.
Row {
    id: bar
    property bool shown: false
    property bool canReply: true
    property bool copied: false
    signal copy
    signal reply
    signal details

    spacing: 2
    opacity: shown || copied ? 1 : 0
    visible: opacity > 0
    Behavior on opacity { NumberAnimation { duration: Theme.fast } }

    Timer {
        id: copiedTimer
        interval: 1400
        onTriggered: bar.copied = false
    }

    component Act: Rectangle {
        id: act
        property string icon
        property string tip
        signal hit
        width: 26
        height: 24
        radius: Theme.radiusSm
        color: area.containsMouse ? Theme.active : "transparent"
        Behavior on color { ColorAnimation { duration: Theme.fast } }
        Icon {
            anchors.centerIn: parent
            name: act.icon
            size: 15
            weight: 1.5
            color: area.containsMouse ? Theme.text : Theme.text3
        }
        MouseArea {
            id: area
            anchors.fill: parent
            hoverEnabled: true
            cursorShape: Qt.PointingHandCursor
            onClicked: act.hit()
        }
        // Label above, after a short hover.
        Rectangle {
            visible: area.containsMouse
            anchors { bottom: parent.top; bottomMargin: 6; horizontalCenter: parent.horizontalCenter }
            width: tipText.implicitWidth + 14
            height: 22
            radius: Theme.radiusSm
            color: "#16161a"
            border.width: 1
            border.color: Theme.lineStrong
            z: 10
            Text {
                id: tipText
                anchors.centerIn: parent
                text: act.tip
                color: Theme.text
                font.pixelSize: Theme.fontCaption
            }
        }
    }

    Act {
        icon: bar.copied ? "check" : "copy"
        tip: bar.copied ? "Copiado" : "Copiar"
        onHit: {
            bar.copy();
            bar.copied = true;
            copiedTimer.restart();
        }
    }
    Act {
        visible: bar.canReply
        icon: "reply"
        tip: "Responder"
        onHit: bar.reply()
    }
    Act {
        icon: "more"
        tip: "Detalhes"
        onHit: bar.details()
    }
}
