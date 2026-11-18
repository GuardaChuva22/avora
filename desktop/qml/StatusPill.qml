import QtQuick
import "util.js" as U

// Job status as a small pill with its light.
Rectangle {
    id: pill
    property string status
    readonly property string kind: status === "running" ? "live" : status === "succeeded" ? "ok" : (status === "failed" || status === "timed_out") ? "bad" : ""

    implicitHeight: 24
    implicitWidth: row.implicitWidth + 20
    radius: height / 2
    color: status === "running" ? Theme.wash4 : Theme.hover
    border.width: 1
    border.color: status === "running" ? Theme.wash5 : Theme.line

    Row {
        id: row
        anchors.centerIn: parent
        spacing: 7
        Dot {
            kind: pill.kind
            anchors.verticalCenter: parent.verticalCenter
        }
        Text {
            text: U.statusLabel(pill.status)
            font.pixelSize: Theme.fontSmall
            font.weight: Font.Medium
            color: pill.status === "running" ? Theme.white : pill.kind === "ok" ? Theme.ok : pill.kind === "bad" ? Theme.bad : Theme.text3
            anchors.verticalCenter: parent.verticalCenter
        }
    }
}
