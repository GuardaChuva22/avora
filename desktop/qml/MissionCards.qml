import QtQuick
import Avora
import "util.js" as U

// Missions in progress in this conversation, above the composer: the title,
// where it stands, the checklist (click to open) and Parar. The work runs on
// a node; the agent is woken here when each round ends.
Column {
    id: box
    property var items: []   // GET /v1/missions?active=1&conversation=
    property var agents: []
    signal changed

    visible: items.length > 0
    spacing: 8

    property var open: ({})      // mission id -> checklist shown
    property string arming: ""   // mission id armed for Parar
    property string error: ""

    function statusText(m) {
        switch (m.status) {
        case "running": return "rodando · rodada " + m.runs;
        case "review": return "rodada " + m.runs + " terminou · decidindo";
        case "waiting": return "esperando você";
        }
        return m.status;
    }
    function doneCount(m) {
        return (m.plan || []).filter(s => s.status === "feito" || s.status === "pulado").length;
    }
    function cancel(m) {
        if (arming !== m.id) {
            arming = m.id;
            disarm.restart();
            return;
        }
        arming = "";
        error = "";
        Api.post("/v1/missions/" + m.id + "/cancel", null, err => {
            if (err)
                box.error = err;
            box.changed();
        });
    }
    Timer {
        id: disarm
        interval: 3000
        onTriggered: box.arming = ""
    }

    Repeater {
        model: box.items
        delegate: Rectangle {
            id: card
            required property var modelData
            readonly property bool shown: !!box.open[modelData.id]
            readonly property bool running: modelData.status === "running"
            width: box.width
            height: col.implicitHeight + 22
            radius: Theme.radiusLg
            color: Theme.hover
            border.width: 1
            border.color: modelData.status === "waiting" ? Qt.rgba(0.95, 0.8, 0.51, 0.3) : Theme.line

            Column {
                id: col
                x: 14
                y: 11
                width: parent.width - 28
                spacing: 8
                Item {
                    width: col.width
                    height: 22
                    Row {
                        spacing: 9
                        anchors.verticalCenter: parent.verticalCenter
                        width: parent.width - stopBtn.width - 8
                        Rectangle {
                            width: 8
                            height: 8
                            radius: 4
                            anchors.verticalCenter: parent.verticalCenter
                            color: card.running ? Theme.live : card.modelData.status === "waiting" ? Theme.warn : Theme.text3
                            SequentialAnimation on opacity {
                                running: card.running && parent.visible
                                loops: Animation.Infinite
                                NumberAnimation { to: 0.35; duration: 900 }
                                NumberAnimation { to: 1; duration: 900 }
                            }
                        }
                        Text {
                            anchors.verticalCenter: parent.verticalCenter
                            width: Math.min(implicitWidth, parent.width - 18)
                            textFormat: Text.StyledText
                            text: "<b>" + U.esc(card.modelData.title) + "</b>  <font color='" + Theme.text4 + "'>" +
                                  box.statusText(card.modelData) + " · " + box.doneCount(card.modelData) + "/" +
                                  (card.modelData.plan || []).length + " passos " + (card.shown ? "▴" : "▾") + "</font>"
                            color: Theme.text
                            font.pixelSize: Theme.fontBody
                            elide: Text.ElideRight
                        }
                    }
                    MouseArea {
                        anchors.fill: parent
                        anchors.rightMargin: stopBtn.width + 8
                        cursorShape: Qt.PointingHandCursor
                        onClicked: {
                            const o = Object.assign({}, box.open);
                            o[card.modelData.id] = !o[card.modelData.id];
                            box.open = o;
                        }
                    }
                    GhostButton {
                        id: stopBtn
                        anchors.right: parent.right
                        anchors.verticalCenter: parent.verticalCenter
                        small: true
                        danger: box.arming === card.modelData.id
                        icon: "close"
                        text: box.arming === card.modelData.id ? "Confirmar" : "Parar"
                        onClicked: box.cancel(card.modelData)
                    }
                }
                Column {
                    visible: card.shown
                    width: col.width
                    spacing: 5
                    Repeater {
                        model: card.shown ? card.modelData.plan : []
                        delegate: Text {
                            required property var modelData
                            required property int index
                            width: col.width
                            readonly property string mark: ({ "feito": "✓", "fazendo": "●", "pulado": "–" })[modelData.status] || "○"
                            text: mark + "  " + modelData.text
                            color: modelData.status === "feito" ? Theme.text3 : modelData.status === "fazendo" ? Theme.text : Theme.text2
                            font.pixelSize: Theme.fontBody
                            font.strikeout: modelData.status === "pulado"
                            wrapMode: Text.Wrap
                        }
                    }
                    Text {
                        visible: card.modelData.status === "waiting" && !!card.modelData.summary
                        width: col.width
                        topPadding: 2
                        text: "Pergunta: " + (card.modelData.summary || "")
                        color: Theme.warn
                        font.pixelSize: Theme.fontBody
                        wrapMode: Text.Wrap
                    }
                }
            }
        }
    }
    ErrorBanner {
        width: box.width
        text: box.error
    }
}
