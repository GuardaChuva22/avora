import QtQuick
import Avora
import "util.js" as U

// Who is answering, in one line like a messenger: overlapping faces, three
// bouncing dots and "Atlas e Cora estão respondendo…". With one agent it
// also says what it is doing ("usando Claude Code"). Hover lists everyone's
// current step.
Item {
    id: ti
    property var people: [] // [{ id, name, color, step }]
    readonly property int n: people.length

    implicitHeight: 22
    visible: n > 0

    function sentence() {
        const names = people.map(p => p.name);
        if (n === 1) {
            const st = people[0].step;
            return "<b>" + U.esc(names[0]) + "</b> " + (st ? "está trabalhando  ·  " + U.esc(U.stepLabel(st)) : "está respondendo…");
        }
        if (n <= 3)
            return "<b>" + names.slice(0, -1).map(U.esc).join("</b>, <b>") + "</b> e <b>" + U.esc(names[n - 1]) + "</b> estão respondendo…";
        return "<b>" + n + " pessoas do time</b> estão respondendo…";
    }

    Row {
        id: row
        anchors.verticalCenter: parent.verticalCenter
        spacing: 9

        // Faces, overlapping.
        Item {
            width: faces.count ? 20 + (Math.min(ti.n, 4) - 1) * 13 : 0
            height: 20
            anchors.verticalCenter: parent.verticalCenter
            Repeater {
                id: faces
                model: ti.people.slice(0, 4)
                delegate: Rectangle {
                    required property var modelData
                    required property int index
                    x: index * 13
                    z: 10 - index
                    width: 20
                    height: 20
                    radius: Theme.radiusMd
                    color: Theme.bg
                    Avatar {
                        anchors.centerIn: parent
                        size: 20
                        name: modelData.name
                        photo: modelData.photo || ""
                        tint: modelData.color || Theme.text2
                    }
                }
            }
        }

        // Three dots, bouncing one after the other.
        Row {
            anchors.verticalCenter: parent.verticalCenter
            spacing: 3
            Repeater {
                model: 3
                delegate: Rectangle {
                    id: dot
                    required property int index
                    width: 5
                    height: 5
                    radius: 2.5
                    color: Theme.text2
                    opacity: 0.5
                    SequentialAnimation {
                        running: ti.visible
                        loops: Animation.Infinite
                        PauseAnimation { duration: dot.index * 140 }
                        ParallelAnimation {
                            NumberAnimation { target: dot; property: "y"; to: -3; duration: 240; easing.type: Easing.OutQuad }
                            NumberAnimation { target: dot; property: "opacity"; to: 1; duration: 240 }
                        }
                        ParallelAnimation {
                            NumberAnimation { target: dot; property: "y"; to: 0; duration: 240; easing.type: Easing.InQuad }
                            NumberAnimation { target: dot; property: "opacity"; to: 0.5; duration: 240 }
                        }
                        PauseAnimation { duration: (2 - dot.index) * 140 + 380 }
                    }
                }
            }
        }

        Text {
            anchors.verticalCenter: parent.verticalCenter
            textFormat: Text.StyledText
            text: ti.sentence()
            color: Theme.text3
            font.pixelSize: Theme.fontSmall
        }
    }

    // Everyone's step, on hover.
    MouseArea {
        id: hover
        anchors.fill: row
        hoverEnabled: true
    }
    Rectangle {
        visible: hover.containsMouse
        anchors { bottom: row.top; bottomMargin: 8; left: row.left }
        z: 100
        width: Math.min(460, steps.implicitWidth + 24)
        height: steps.implicitHeight + 16
        radius: Theme.radiusMd
        color: "#16161a"
        border.width: 1
        border.color: Theme.lineStrong
        Column {
            id: steps
            x: 12
            y: 8
            spacing: 4
            Repeater {
                model: ti.people
                delegate: Text {
                    required property var modelData
                    width: Math.min(implicitWidth, 436)
                    textFormat: Text.StyledText
                    text: "<b>" + U.esc(modelData.name) + "</b>  <font color='" + Theme.text3 + "'>" +
                          U.esc(modelData.step ? U.stepLabel(modelData.step) + (modelData.summary ? " · " + modelData.summary : "") : "pensando…") + "</font>"
                    color: Theme.text
                    font.pixelSize: Theme.fontSmall
                    elide: Text.ElideRight
                }
            }
        }
    }
}
