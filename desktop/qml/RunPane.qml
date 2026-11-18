import QtQuick
import QtQuick.Controls.Basic
import Avora
import "util.js" as U

// One Claude Code or Codex run an agent delegated, as a conversation of its
// own: what it was asked, what it said and did, and its answer. Live while
// it runs.
Rectangle {
    id: run
    property string runId: ""
    property var agents: []
    // Room to leave at the top, when the pane reaches the window's title bar
    // (which would take the clicks on Voltar).
    property int topInset: 0
    signal closed

    property var info: null
    property bool showPrompt: false
    property real now: Date.now()
    readonly property var agent: info ? agents.find(a => a.id === info.agent_id) || null : null
    readonly property string engineName: info && info.engine === "codex" ? "Codex" : "Claude Code"
    readonly property real column: Math.min(width - 2 * Theme.gutter, 760)

    color: Theme.bg

    function load() {
        const id = runId;
        if (id === "")
            return;
        Api.get("/v1/runs/" + id, (err, r) => {
            if (err || run.runId !== id)
                return;
            const atEnd = flick.contentY >= flick.contentHeight - flick.height - 40;
            run.info = r;
            if (r.active && atEnd)
                Qt.callLater(() => flick.contentY = Math.max(0, flick.contentHeight - flick.height));
        });
    }
    onRunIdChanged: {
        info = null;
        showPrompt = false;
        load();
    }
    Timer {
        interval: 1500
        repeat: true
        running: run.visible && run.info !== null && run.info.active
        onTriggered: {
            run.now = Date.now();
            run.load();
        }
    }

    function elapsed() {
        if (!info)
            return "";
        const from = new Date(info.started_at || info.created_at).getTime();
        const to = info.completed_at ? new Date(info.completed_at).getTime() : now;
        const m = Math.max(0, Math.round((to - from) / 60000));
        return m < 1 ? "menos de 1 min" : m + " min";
    }
    function statusText() {
        if (!info)
            return "";
        if (info.active)
            return info.status === "queued" ? "na fila" : "rodando há " + elapsed();
        return { succeeded: "terminou em ", failed: "falhou depois de ", canceled: "parado depois de ", timed_out: "parou de responder depois de " }[info.status] + elapsed();
    }

    // Header
    Item {
        id: head
        y: run.topInset
        width: parent.width
        height: 64
        Row {
            x: (parent.width - run.column) / 2
            anchors.verticalCenter: parent.verticalCenter
            spacing: 12
            GhostButton {
                small: true
                icon: "back"
                text: "Voltar"
                anchors.verticalCenter: parent.verticalCenter
                onClicked: run.closed()
            }
            Icon {
                name: run.info && run.info.engine === "codex" ? "openai" : "claude"
                size: 18
                color: Theme.text
                anchors.verticalCenter: parent.verticalCenter
            }
            Column {
                anchors.verticalCenter: parent.verticalCenter
                spacing: 2
                Text {
                    width: Math.min(implicitWidth, run.column - 220)
                    textFormat: Text.StyledText
                    text: "<b>" + run.engineName + "</b>  <font color='" + Theme.text3 + "'>" + U.esc(run.info ? run.info.title : "") + "</font>"
                    color: Theme.text
                    font.pixelSize: Theme.fontLarge
                    elide: Text.ElideRight
                }
                Row {
                    spacing: 7
                    Rectangle {
                        width: 6
                        height: 6
                        radius: 3
                        anchors.verticalCenter: parent.verticalCenter
                        color: !run.info ? Theme.text4 : run.info.active ? Theme.live : run.info.status === "succeeded" ? Theme.ok : Theme.bad
                    }
                    Text {
                        text: (run.agent ? "a pedido de " + run.agent.name + " · " : "") + run.statusText()
                        color: Theme.text3
                        font.pixelSize: Theme.fontSmall
                    }
                }
            }
        }
        Rectangle {
            anchors.bottom: parent.bottom
            width: parent.width
            height: 1
            color: Theme.line
        }
    }

    Flickable {
        id: flick
        anchors { top: head.bottom; bottom: parent.bottom; left: parent.left; right: parent.right }
        contentHeight: body.implicitHeight + 60
        clip: true
        boundsBehavior: Flickable.StopAtBounds
        ScrollBar.vertical: SlimScrollBar {}
        SmoothWheel {}

        Column {
            id: body
            x: (flick.width - run.column) / 2
            y: 24
            width: run.column
            spacing: 14

            // What it was asked (folded).
            Rectangle {
                visible: !!run.info && !!run.info.prompt
                width: parent.width
                height: promptText.height + 24
                radius: Theme.radiusLg
                color: Theme.hover
                border.width: 1
                border.color: Theme.line
                Text {
                    id: promptText
                    x: 14
                    y: 12
                    width: parent.width - 28
                    textFormat: Text.StyledText
                    text: "<font color='" + Theme.text4 + "'>PEDIDO " + (run.showPrompt ? "▴" : "▾") + "</font><br>" +
                          U.esc(run.info ? run.info.prompt : "").replace(/\n/g, "<br>")
                    color: Theme.text2
                    font.pixelSize: Theme.fontBody
                    lineHeight: 1.2
                    wrapMode: Text.Wrap
                    maximumLineCount: run.showPrompt ? 400 : 5
                    elide: Text.ElideRight
                }
                MouseArea {
                    anchors.fill: parent
                    cursorShape: Qt.PointingHandCursor
                    onClicked: run.showPrompt = !run.showPrompt
                }
            }

            // What it said and did, in order.
            Repeater {
                model: run.info && run.info.steps ? run.info.steps : []
                delegate: Item {
                    id: st
                    required property var modelData
                    readonly property bool said: modelData.tool === "texto"
                    width: body.width
                    height: said ? said1.implicitHeight + 6 : tool1.implicitHeight
                    TextEdit {
                        id: said1
                        visible: st.said
                        width: parent.width
                        text: st.said ? st.modelData.summary : ""
                        textFormat: TextEdit.MarkdownText
                        color: Theme.text
                        font.pixelSize: Theme.fontLarge
                        wrapMode: TextEdit.Wrap
                        readOnly: true
                        selectByMouse: true
                        selectionColor: Theme.wash6
                    }
                    Row {
                        id: tool1
                        visible: !st.said
                        spacing: 8
                        Rectangle {
                            width: 5
                            height: 5
                            radius: 2.5
                            color: Theme.text4
                            anchors.verticalCenter: parent.verticalCenter
                        }
                        Text {
                            text: U.stepLabel(st.modelData.tool)
                            color: Theme.text2
                            font.family: Theme.mono
                            font.pixelSize: Theme.fontSmall
                        }
                        Text {
                            width: body.width - 140
                            text: st.modelData.summary || ""
                            color: Theme.text4
                            font.family: Theme.mono
                            font.pixelSize: Theme.fontSmall
                            elide: Text.ElideRight
                        }
                    }
                }
            }

            TypingIndicator {
                visible: !!run.info && run.info.active
            }

            // The answer.
            Rectangle {
                visible: !!run.info && !run.info.active && (!!run.info.output || !!run.info.error)
                width: parent.width
                height: answerCol.implicitHeight + 28
                radius: Theme.radiusLg
                color: Theme.hover
                border.width: 1
                border.color: run.info && run.info.error ? Qt.rgba(1, 0.54, 0.54, 0.25) : Theme.lineStrong
                Column {
                    id: answerCol
                    x: 16
                    y: 14
                    width: parent.width - 32
                    spacing: 8
                    Text {
                        text: run.info && run.info.error ? "NÃO DEU CERTO" : "RESPOSTA"
                        color: run.info && run.info.error ? Theme.bad : Theme.text4
                        font.pixelSize: Theme.fontCaption
                        font.letterSpacing: 0.6
                    }
                    Text {
                        visible: !!run.info && !!run.info.error
                        width: parent.width
                        text: run.info ? run.info.error || "" : ""
                        color: Theme.bad
                        font.pixelSize: Theme.fontBody
                        wrapMode: Text.Wrap
                    }
                    TextEdit {
                        visible: !!run.info && !!run.info.output
                        width: parent.width
                        text: run.info ? run.info.output || "" : ""
                        textFormat: TextEdit.MarkdownText
                        color: Theme.text
                        font.pixelSize: Theme.fontLarge
                        wrapMode: TextEdit.Wrap
                        readOnly: true
                        selectByMouse: true
                        selectionColor: Theme.wash6
                    }
                }
            }
        }
    }
}
