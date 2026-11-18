import QtQuick
import QtQuick.Controls.Basic
import Avora
import "util.js" as U

// Gerenciador de tarefas: everything each agent is running right now in the
// active workspace, with live elapsed time, inspection and stop controls.
Item {
    id: page
    property var tasks: []
    property var agents: []
    property bool loading: true
    property string error: ""
    property var stopping: ({})
    property string openRunId: ""
    property real nowMs: Date.now()

    signal goPage(string name)
    signal openAgentConv(string agentId, string convId)
    signal openChannel(string channelId)

    readonly property var agentGroups: {
        const groups = [];
        const byAgent = {};
        for (const t of tasks) {
            const id = t.agent_id || "avora";
            if (!byAgent[id]) {
                const full = agents.find(a => a.id === id) || null;
                byAgent[id] = {
                    agent_id: id,
                    name: t.agent_name || (full ? full.name : id),
                    role: full ? (full.role || "") : "",
                    color: t.agent_color || (full ? full.color : ""),
                    agent: full,
                    items: []
                };
                groups.push(byAgent[id]);
            }
            byAgent[id].items.push(t);
        }
        return groups;
    }

    function subtitleText() {
        if (loading && tasks.length === 0)
            return "Verificando as execuções ativas deste espaço…";
        if (tasks.length === 0)
            return "Nenhuma execução em andamento neste momento.";
        if (tasks.length === 1)
            return "1 execução rodando agora.";
        return tasks.length + " execuções rodando agora.";
    }

    function kindLabel(t) {
        if (t.kind === "run") {
            if (t.engine === "codex")
                return "Codex";
            if (t.engine === "antigravity")
                return "Antigravity";
            return "Claude Code";
        }
        if (t.kind === "mission")
            return "Missão";
        if (t.kind === "channel")
            return t.channel || "Canal";
        return "Conversa";
    }

    function elapsedText(startedIso, fallbackSec) {
        let sec = fallbackSec || 0;
        if (startedIso) {
            const diff = Math.round((nowMs - new Date(startedIso).getTime()) / 1000);
            if (diff >= 0)
                sec = diff;
        }
        if (sec < 60)
            return "há " + Math.max(1, sec) + " s";
        if (sec < 3600)
            return "há " + Math.round(sec / 60) + " min";
        return "há " + Math.round(sec / 3600) + " h";
    }

    function inspectTask(m) {
        if (!m)
            return;
        if (m.run_id) {
            if (typeof win !== "undefined" && win && win.openFloat) {
                win.openFloat("RunPane.qml", { runId: m.run_id, agents: page.agents });
                return;
            }
            openRunId = m.run_id;
            return;
        }
        if (m.kind === "channel" && m.conversation_id) {
            if (typeof win !== "undefined" && win && win.openChannel) {
                win.openChannel(m.conversation_id);
                return;
            }
            openChannel(m.conversation_id);
            return;
        }
        if (m.agent_id) {
            if (typeof win !== "undefined" && win) {
                if (win.openAgent) {
                    win.openAgent(m.agent_id);
                    return;
                }
                if (win.openAgentConv) {
                    win.openAgentConv(m.agent_id, m.conversation_id || "");
                    return;
                }
            }
            openAgentConv(m.agent_id, m.conversation_id || "");
        }
    }

    function triggerOpenAgent(agentId) {
        if (!agentId || typeof win === "undefined" || !win)
            return;
        if (win.openAgent)
            win.openAgent(agentId);
        else if (win.openAgentConv)
            win.openAgentConv(agentId, "");
    }

    function load() {
        Api.get("/v1/tasks", (err, list) => {
            loading = false;
            if (err) {
                error = err;
                return;
            }
            error = "";
            if (!U.same(tasks, list || []))
                tasks = list || [];
        });
        Api.get("/v1/agents", (err, list) => {
            if (!err && !U.same(agents, list || []))
                agents = list || [];
        });
    }

    function stopTask(t) {
        if (!t || stopping[t.id])
            return;
        const next = Object.assign({}, stopping);
        next[t.id] = true;
        stopping = next;
        error = "";

        let path = "";
        if (t.kind === "run" && t.run_id)
            path = "/v1/runs/" + encodeURIComponent(t.run_id) + "/stop";
        else if (t.kind === "mission" && t.mission_id)
            path = "/v1/missions/" + encodeURIComponent(t.mission_id) + "/cancel";
        else if (t.conversation_id)
            path = "/v1/conversations/" + encodeURIComponent(t.conversation_id) + "/stop";

        if (path === "")
            return;

        Api.post(path, null, err => {
            const done = Object.assign({}, page.stopping);
            delete done[t.id];
            page.stopping = done;
            if (err)
                page.error = err;
            page.load();
        });
    }

    Component.onCompleted: load()
    onVisibleChanged: if (visible) load()

    Connections {
        target: Api
        function onWorkspaceChanged() {
            page.tasks = [];
            page.agents = [];
            page.openRunId = "";
            page.loading = true;
            page.load();
        }
        function onLiveEvent(e) {
            if (!page.visible)
                return;
            const t = e.type || "";
            if (t.startsWith("job.") || t.startsWith("mission.") || t.startsWith("channel.") || t.startsWith("message."))
                reloadTimer.restart();
        }
        function onLiveReset() {
            if (page.visible)
                page.load();
        }
    }

    Timer {
        id: reloadTimer
        interval: 200
        onTriggered: page.load()
    }

    Timer {
        interval: 1500
        running: page.visible
        repeat: true
        onTriggered: {
            page.nowMs = Date.now();
            if (page.tasks.length > 0 || !Api.live)
                page.load();
        }
    }

    Flickable {
        id: scroller
        visible: page.openRunId === ""
        anchors.fill: parent
        contentHeight: body.implicitHeight + 96
        clip: true
        boundsBehavior: Flickable.StopAtBounds
        ScrollBar.vertical: SlimScrollBar {}
        SmoothWheel {}

        Column {
            id: body
            readonly property real colWidth: Math.min(scroller.width - 2 * Theme.gutter, 820)
            x: Math.max(Theme.gutter, (scroller.width - colWidth) / 2)
            y: 44
            width: colWidth
            spacing: Theme.space5

            PageHead {
                width: parent.width
                eyebrow: "Trabalho visível"
                title: "Gerenciador de tarefas"
                sub: page.subtitleText()
            }

            ErrorBanner {
                width: parent.width
                text: page.error
            }

            // Loading state
            Card {
                visible: page.loading && page.tasks.length === 0
                width: parent.width
                height: 88
                padding: Theme.space5

                Text {
                    anchors.verticalCenter: parent.verticalCenter
                    text: "Verificando o que os agentes estão fazendo agora…"
                    color: Theme.text3
                    font.pixelSize: Theme.fontBody
                }
            }

            // Empty state
            Card {
                visible: !page.loading && page.tasks.length === 0
                width: parent.width
                height: emptyCol.implicitHeight + 2 * padding
                padding: Theme.space5

                Column {
                    id: emptyCol
                    width: parent.width
                    spacing: Theme.space2

                    Text {
                        text: "Nenhum agente está trabalhando agora"
                        color: Theme.white
                        font.family: Theme.display
                        font.pixelSize: Theme.fontHeading
                        font.weight: Font.DemiBold
                    }
                    Text {
                        width: parent.width
                        wrapMode: Text.WordWrap
                        text: "Quando um agente estiver escrevendo código, pesquisando, rodando uma missão ou respondendo num canal, você acompanha e pode parar tudo por aqui."
                        color: Theme.text3
                        font.pixelSize: Theme.fontBody
                    }
                }
            }

            // Grouped by agent
            Repeater {
                model: page.agentGroups
                delegate: Card {
                    id: grpCard
                    required property var modelData
                    width: body.width
                    height: grpCol.implicitHeight + 2 * padding
                    padding: Theme.space4

                    Column {
                        id: grpCol
                        width: parent.width
                        spacing: Theme.space3

                        Row {
                            width: parent.width
                            spacing: Theme.space3

                            Avatar {
                                size: 38
                                name: grpCard.modelData.name
                                photo: grpCard.modelData.agent ? U.photo(grpCard.modelData.agent) : ""
                                tint: grpCard.modelData.color || Theme.text2
                                working: true
                                anchors.verticalCenter: parent.verticalCenter
                            }

                            Column {
                                anchors.verticalCenter: parent.verticalCenter
                                spacing: Theme.space1

                                Text {
                                    text: grpCard.modelData.name
                                    color: agHdrArea.containsMouse ? Theme.white : Theme.text
                                    font.pixelSize: Theme.fontLarge
                                    font.weight: Font.DemiBold
                                    font.underline: agHdrArea.containsMouse

                                    MouseArea {
                                        id: agHdrArea
                                        anchors.fill: parent
                                        hoverEnabled: true
                                        cursorShape: Qt.PointingHandCursor
                                        onClicked: page.triggerOpenAgent(grpCard.modelData.agent_id)
                                    }
                                }
                                Text {
                                    visible: grpCard.modelData.role !== ""
                                    text: grpCard.modelData.role
                                    color: Theme.text3
                                    font.pixelSize: Theme.fontSmall
                                }
                            }
                        }

                        Column {
                            width: parent.width
                            spacing: Theme.space2

                            Repeater {
                                model: grpCard.modelData.items
                                delegate: Rectangle {
                                    id: tRow
                                    required property var modelData
                                    readonly property bool busyStop: !!page.stopping[modelData.id]
                                    width: grpCol.width
                                    height: 58
                                    radius: Theme.radiusMd
                                    color: Theme.wash1
                                    border.width: 1
                                    border.color: Theme.line

                                    Column {
                                        anchors {
                                            left: parent.left
                                            leftMargin: Theme.space3
                                            right: tActions.left
                                            rightMargin: Theme.space3
                                            verticalCenter: parent.verticalCenter
                                        }
                                        spacing: Theme.space1

                                        Row {
                                            width: parent.width
                                            spacing: Theme.space2

                                            Rectangle {
                                                height: 18
                                                width: kLbl.implicitWidth + 2 * Theme.space2
                                                radius: height / 2
                                                color: Theme.wash4
                                                anchors.verticalCenter: parent.verticalCenter
                                                Text {
                                                    id: kLbl
                                                    anchors.centerIn: parent
                                                    text: page.kindLabel(tRow.modelData)
                                                    color: Theme.text2
                                                    font.pixelSize: Theme.fontTiny
                                                    font.weight: Font.DemiBold
                                                }
                                            }

                                            Text {
                                                text: page.elapsedText(tRow.modelData.started_at, tRow.modelData.elapsed_seconds)
                                                color: Theme.ok
                                                font.family: Theme.mono
                                                font.pixelSize: Theme.fontCaption
                                                anchors.verticalCenter: parent.verticalCenter
                                            }
                                        }

                                        Text {
                                            width: parent.width
                                            text: tRow.modelData.title || ""
                                            color: Theme.text
                                            font.pixelSize: Theme.fontBody
                                            font.weight: Font.Medium
                                            elide: Text.ElideRight
                                        }
                                    }

                                    Row {
                                        id: tActions
                                        anchors { right: parent.right; rightMargin: Theme.space3; verticalCenter: parent.verticalCenter }
                                        spacing: Theme.space2

                                        GhostButton {
                                            visible: !!(tRow.modelData.run_id || tRow.modelData.conversation_id || tRow.modelData.agent_id)
                                            small: true
                                            icon: tRow.modelData.run_id ? "terminal" : "chat"
                                            text: "Ver"
                                            onClicked: page.inspectTask(tRow.modelData)
                                        }

                                        GhostButton {
                                            small: true
                                            danger: true
                                            enabled: !tRow.busyStop
                                            icon: "stop"
                                            text: tRow.busyStop ? "Parando…" : "Parar"
                                            onClicked: page.stopTask(tRow.modelData)
                                        }
                                    }
                                }
                            }
                        }
                    }
                }
            }
        }
    }

    RunPane {
        visible: page.openRunId !== ""
        anchors.fill: parent
        topInset: Theme.titleBar
        runId: page.openRunId
        agents: page.agents
        onClosed: page.openRunId = ""
    }
}
