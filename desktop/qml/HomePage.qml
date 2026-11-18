import QtCore
import QtQuick
import QtQuick.Controls.Basic
import Avora
import "util.js" as U

// Início (Phase 6): greeting + live summary, quick composer with the last
// agent first, and up to 5 rows of each non-empty work section.
Item {
    id: page
    property var home: ({ greeting_name: "", waiting: [], working: [], delivered: [], next_routines: [], last_agent: "" })
    property var agents: []
    property var channels: []
    property bool loading: true
    property string error: ""
    property string sessionSince: ""
    property string pickedAgent: ""
    property string openPref: "home"
    property real nowMs: Date.now()

    signal goPage(string name)
    signal askAgent(string agentId, string message)
    signal openAgentConv(string agentId, string convId)
    signal openChannel(string channelId)
    signal openRun(string runId, string agentId)
    signal newAgentRequested

    Settings {
        category: "ui"
        property alias openMode: page.openPref
    }

    readonly property var orderedAgents: {
        const last = (home && home.last_agent) ? home.last_agent : "";
        if (!last)
            return agents;
        const first = agents.filter(a => a.id === last);
        const rest = agents.filter(a => a.id !== last);
        return first.concat(rest);
    }

    readonly property var selectedAgent: {
        const found = orderedAgents.find(a => a.id === pickedAgent);
        if (found)
            return found;
        return orderedAgents.length > 0 ? orderedAgents[0] : null;
    }

    readonly property var waitingRows: ((home && home.waiting) || []).slice(0, 5)
    readonly property var workingRows: ((home && home.working) || []).slice(0, 5)
    readonly property var deliveredRows: ((home && home.delivered) || []).slice(0, 5)
    readonly property var routineRows: ((home && home.next_routines) || []).slice(0, 5)

    readonly property int waitingTotal: ((home && home.waiting) || []).length
    readonly property int workingTotal: ((home && home.working) || []).length
    readonly property int workingAgentCount: {
        const seen = {};
        let n = 0;
        for (const w of ((home && home.working) || [])) {
            if (w.agent_id && !seen[w.agent_id]) {
                seen[w.agent_id] = true;
                n++;
            }
        }
        return n;
    }

    function greetingText() {
        const h = new Date().getHours();
        const sal = h < 12 ? "Bom dia" : h < 18 ? "Boa tarde" : "Boa noite";
        const who = (home && home.greeting_name) ? home.greeting_name : "";
        return who !== "" ? sal + ", " + who : sal;
    }

    function summaryText() {
        const parts = [];
        if (waitingTotal > 0)
            parts.push(waitingTotal === 1 ? "1 coisa espera você" : waitingTotal + " coisas esperam você");
        if (workingAgentCount > 0)
            parts.push(workingAgentCount === 1 ? "1 agente trabalhando" : workingAgentCount + " agentes trabalhando");
        else if (workingTotal > 0)
            parts.push(workingTotal === 1 ? "1 execução ativa" : workingTotal + " execuções ativas");
        if (parts.length > 0)
            return parts.join(", ");
        if (loading && agents.length === 0)
            return "Carregando o resumo do seu espaço…";
        if (agents.length === 0)
            return "Ainda não há agentes neste espaço. Crie o primeiro para começar.";
        return "Tudo em dia por aqui — ninguém esperando e nenhuma tarefa travada.";
    }

    function elapsedLabel(startedIso, fallbackSec) {
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

    function waitingKindLabel(k) {
        if (k === "approval")
            return "Permissão";
        if (k === "question")
            return "Pergunta";
        return "Decisão";
    }

    function load(visit) {
        let url = "/v1/home";
        if (!visit) {
            url += "?peek=1";
            if (sessionSince !== "")
                url += "&since=" + encodeURIComponent(sessionSince);
        }
        Api.get(url, (err, data) => {
            loading = false;
            if (err) {
                error = err;
                return;
            }
            error = "";
            if (visit && data && data.since)
                sessionSince = data.since;
            if (!U.same(home, data))
                home = data;
            if (pickedAgent === "" && data && data.last_agent)
                pickedAgent = data.last_agent;
        });
        Api.get("/v1/agents", (err, data) => {
            if (!err && !U.same(agents, data || []))
                agents = data || [];
        });
        Api.get("/v1/channels", (err, data) => {
            if (!err && !U.same(channels, data || []))
                channels = data || [];
        });
    }

    function submitAsk() {
        const text = askField.text.trim();
        const target = selectedAgent;
        if (text === "" || !target)
            return;
        askField.text = "";
        pickedAgent = target.id;
        askAgent(target.id, text);
    }

    Component.onCompleted: load(true)
    onVisibleChanged: {
        if (visible) {
            sessionSince = "";
            load(true);
        }
    }

    Connections {
        target: Api
        function onLiveEvent(e) {
            if (!page.visible)
                return;
            const t = e.type || "";
            if (t.startsWith("job.") || t.startsWith("approval.") || t.startsWith("mission.") ||
                t.startsWith("routine.") || t.startsWith("message.") || t.startsWith("agent."))
                refreshDebounce.restart();
        }
    }

    Timer {
        id: refreshDebounce
        interval: 250
        onTriggered: page.load(false)
    }

    Timer {
        interval: Api.live ? 20000 : 5000
        running: page.visible
        repeat: true
        onTriggered: {
            page.nowMs = Date.now();
            page.load(false);
        }
    }

    Flickable {
        id: scroller
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

            // Greeting & quick navigation
            Item {
                width: parent.width
                height: Math.max(headBlock.implicitHeight, headActions.implicitHeight)

                PageHead {
                    id: headBlock
                    width: parent.width - headActions.width - Theme.space3
                    eyebrow: "Início"
                    title: page.greetingText()
                    sub: page.summaryText()
                }

                Row {
                    id: headActions
                    anchors { right: parent.right; bottom: headBlock.bottom }
                    spacing: Theme.space2

                    GhostButton {
                        visible: page.waitingTotal > 0
                        icon: "alert"
                        text: "Esperando você (" + page.waitingTotal + ")"
                        onClicked: page.goPage("waiting")
                    }
                    GhostButton {
                        icon: "pulse"
                        text: page.workingTotal > 0 ? "Rodando (" + page.workingTotal + ")" : "Gerenciador"
                        onClicked: page.goPage("tasks")
                    }
                }
            }

            ErrorBanner {
                width: parent.width
                text: page.error
            }

            // Empty team callout
            Card {
                visible: !page.loading && page.agents.length === 0
                width: parent.width
                height: emptyTeamCol.implicitHeight + 2 * padding
                padding: Theme.space5

                Column {
                    id: emptyTeamCol
                    width: parent.width
                    spacing: Theme.space3

                    Text {
                        text: "Monte seu time de agentes"
                        color: Theme.white
                        font.family: Theme.display
                        font.pixelSize: Theme.fontHeading
                        font.weight: Font.DemiBold
                    }
                    Text {
                        width: parent.width
                        wrapMode: Text.WordWrap
                        text: "Escolha modelos prontos (PM, Dev, Pesquisa, Ops ou Assistente pessoal) e comece a pedir tarefas por aqui."
                        color: Theme.text3
                        font.pixelSize: Theme.fontBody
                    }
                    Row {
                        spacing: Theme.space2
                        PrimaryButton {
                            icon: "plus"
                            text: "Novo agente"
                            onClicked: page.newAgentRequested()
                        }
                    }
                }
            }

            // "Peça a um agente"
            Card {
                visible: page.orderedAgents.length > 0
                width: parent.width
                height: askCol.implicitHeight + 2 * padding
                padding: Theme.space4

                Column {
                    id: askCol
                    width: parent.width
                    spacing: Theme.space3

                    Overline { text: "PEÇA A UM AGENTE" }

                    Flow {
                        width: parent.width
                        spacing: Theme.space2

                        Repeater {
                            model: page.orderedAgents
                            delegate: Rectangle {
                                id: agChip
                                required property var modelData
                                required property int index
                                readonly property bool on: page.selectedAgent && page.selectedAgent.id === modelData.id
                                height: 32
                                width: chipRow.implicitWidth + 20
                                radius: height / 2
                                color: on ? Theme.wash4 : agChipArea.containsMouse ? Theme.hover : "transparent"
                                border.width: 1
                                border.color: on ? Theme.focusLine : Theme.line
                                Behavior on color { ColorAnimation { duration: Theme.fast } }

                                Row {
                                    id: chipRow
                                    anchors.centerIn: parent
                                    spacing: Theme.space2

                                    Avatar {
                                        size: 20
                                        name: agChip.modelData.name
                                        photo: U.photo(agChip.modelData)
                                        tint: agChip.modelData.color || Theme.text2
                                        working: !!agChip.modelData.working
                                        anchors.verticalCenter: parent.verticalCenter
                                    }
                                    Text {
                                        text: agChip.modelData.name
                                        color: agChip.on ? Theme.white : Theme.text2
                                        font.pixelSize: Theme.fontBody
                                        font.weight: agChip.on ? Font.DemiBold : Font.Normal
                                        anchors.verticalCenter: parent.verticalCenter
                                    }
                                }

                                MouseArea {
                                    id: agChipArea
                                    anchors.fill: parent
                                    hoverEnabled: true
                                    cursorShape: Qt.PointingHandCursor
                                    onClicked: {
                                        page.pickedAgent = agChip.modelData.id;
                                        askField.area.forceActiveFocus();
                                    }
                                }
                            }
                        }
                    }

                    Row {
                        width: parent.width
                        spacing: Theme.space2

                        AreaField {
                            id: askField
                            width: parent.width - sendAskBtn.width - Theme.space2
                            minHeight: 46
                            maxHeight: 140
                            enterSubmits: true
                            placeholderText: page.selectedAgent
                                ? "Escreva um pedido para " + page.selectedAgent.name + "…"
                                : "Escreva um pedido…"
                            onSubmitted: page.submitAsk()
                        }

                        PrimaryButton {
                            id: sendAskBtn
                            height: 46
                            icon: "send"
                            text: "Enviar"
                            enabled: askField.text.trim() !== "" && page.selectedAgent !== null
                            anchors.bottom: askField.bottom
                            onClicked: page.submitAsk()
                        }
                    }
                }
            }

            // Section 1: Esperando você (only when non-empty, up to 5 rows)
            Card {
                visible: page.waitingRows.length > 0
                width: parent.width
                height: waitSec.implicitHeight + 2 * padding
                padding: Theme.space4

                Column {
                    id: waitSec
                    width: parent.width
                    spacing: Theme.space3

                    Item {
                        width: parent.width
                        height: 20
                        Overline {
                            anchors.verticalCenter: parent.verticalCenter
                            text: "ESPERANDO VOCÊ"
                        }
                        Text {
                            anchors { right: parent.right; verticalCenter: parent.verticalCenter }
                            text: page.waitingTotal > 5 ? "Ver todas (" + page.waitingTotal + ")" : "Responder tudo"
                            color: waitAllArea.containsMouse ? Theme.white : Theme.text3
                            font.pixelSize: Theme.fontSmall
                            font.weight: Font.Medium
                            MouseArea {
                                id: waitAllArea
                                anchors.fill: parent
                                hoverEnabled: true
                                cursorShape: Qt.PointingHandCursor
                                onClicked: page.goPage("waiting")
                            }
                        }
                    }

                    Column {
                        width: parent.width
                        spacing: Theme.space1

                        Repeater {
                            model: page.waitingRows
                            delegate: Rectangle {
                                id: wRow
                                required property var modelData
                                width: waitSec.width
                                height: 52
                                radius: Theme.radiusMd
                                color: wRowArea.containsMouse ? Theme.hover : Theme.wash1
                                border.width: 1
                                border.color: wRowArea.containsMouse ? Theme.lineStrong : Theme.line
                                Behavior on color { ColorAnimation { duration: Theme.fast } }

                                Avatar {
                                    id: wAv
                                    x: Theme.space3
                                    anchors.verticalCenter: parent.verticalCenter
                                    size: 32
                                    name: wRow.modelData.agent_name || wRow.modelData.agent_id
                                    tint: wRow.modelData.agent_color || Theme.warn
                                }

                                Column {
                                    anchors {
                                        left: wAv.right
                                        leftMargin: Theme.space3
                                        right: wRight.left
                                        rightMargin: Theme.space3
                                        verticalCenter: parent.verticalCenter
                                    }
                                    spacing: 2

                                    Row {
                                        width: parent.width
                                        spacing: Theme.space2
                                        Rectangle {
                                            height: 18
                                            width: wBadgeText.implicitWidth + 12
                                            radius: height / 2
                                            color: Qt.rgba(Theme.warn.r, Theme.warn.g, Theme.warn.b, 0.14)
                                            anchors.verticalCenter: parent.verticalCenter
                                            Text {
                                                id: wBadgeText
                                                anchors.centerIn: parent
                                                text: page.waitingKindLabel(wRow.modelData.kind)
                                                color: Theme.warn
                                                font.pixelSize: Theme.fontTiny
                                                font.weight: Font.DemiBold
                                            }
                                        }
                                        Text {
                                            width: parent.width - 84
                                            text: wRow.modelData.title || ""
                                            color: Theme.text
                                            font.pixelSize: Theme.fontBody
                                            font.weight: Font.DemiBold
                                            elide: Text.ElideRight
                                            anchors.verticalCenter: parent.verticalCenter
                                        }
                                    }

                                    Text {
                                        width: parent.width
                                        text: wRow.modelData.summary || ""
                                        color: Theme.text3
                                        font.pixelSize: Theme.fontSmall
                                        elide: Text.ElideRight
                                    }
                                }

                                Row {
                                    id: wRight
                                    anchors { right: parent.right; rightMargin: Theme.space3; verticalCenter: parent.verticalCenter }
                                    spacing: Theme.space2
                                    Text {
                                        text: U.ago(wRow.modelData.created_at)
                                        color: Theme.text4
                                        font.pixelSize: Theme.fontCaption
                                        anchors.verticalCenter: parent.verticalCenter
                                    }
                                    Icon {
                                        name: "chevron"
                                        size: 14
                                        color: Theme.text3
                                        anchors.verticalCenter: parent.verticalCenter
                                    }
                                }

                                MouseArea {
                                    id: wRowArea
                                    anchors.fill: parent
                                    hoverEnabled: true
                                    cursorShape: Qt.PointingHandCursor
                                    onClicked: page.goPage("waiting")
                                }
                            }
                        }
                    }
                }
            }

            // Section 2: Trabalhando agora (only when non-empty, up to 5 rows)
            Card {
                visible: page.workingRows.length > 0
                width: parent.width
                height: workSec.implicitHeight + 2 * padding
                padding: Theme.space4

                Column {
                    id: workSec
                    width: parent.width
                    spacing: Theme.space3

                    Item {
                        width: parent.width
                        height: 20
                        Overline {
                            anchors.verticalCenter: parent.verticalCenter
                            text: "TRABALHANDO AGORA"
                        }
                        Text {
                            anchors { right: parent.right; verticalCenter: parent.verticalCenter }
                            text: page.workingTotal > 5 ? "Gerenciar todas (" + page.workingTotal + ")" : "Gerenciar tarefas"
                            color: workAllArea.containsMouse ? Theme.white : Theme.text3
                            font.pixelSize: Theme.fontSmall
                            font.weight: Font.Medium
                            MouseArea {
                                id: workAllArea
                                anchors.fill: parent
                                hoverEnabled: true
                                cursorShape: Qt.PointingHandCursor
                                onClicked: page.goPage("tasks")
                            }
                        }
                    }

                    Column {
                        width: parent.width
                        spacing: Theme.space1

                        Repeater {
                            model: page.workingRows
                            delegate: Rectangle {
                                id: wkRow
                                required property var modelData
                                width: workSec.width
                                height: 50
                                radius: Theme.radiusMd
                                color: wkArea.containsMouse ? Theme.hover : Theme.wash1
                                border.width: 1
                                border.color: wkArea.containsMouse ? Theme.lineStrong : Theme.line
                                Behavior on color { ColorAnimation { duration: Theme.fast } }

                                Avatar {
                                    id: wkAv
                                    x: Theme.space3
                                    anchors.verticalCenter: parent.verticalCenter
                                    size: 32
                                    name: wkRow.modelData.agent_name || wkRow.modelData.agent_id
                                    tint: wkRow.modelData.agent_color || Theme.text2
                                    working: true
                                }

                                Column {
                                    anchors {
                                        left: wkAv.right
                                        leftMargin: Theme.space3
                                        right: wkTime.left
                                        rightMargin: Theme.space3
                                        verticalCenter: parent.verticalCenter
                                    }
                                    spacing: 2

                                    Text {
                                        width: parent.width
                                        text: (wkRow.modelData.agent_name || wkRow.modelData.agent_id) +
                                              (wkRow.modelData.channel ? " · " + wkRow.modelData.channel : "")
                                        color: Theme.white
                                        font.pixelSize: Theme.fontBody
                                        font.weight: Font.DemiBold
                                        elide: Text.ElideRight
                                    }
                                    Text {
                                        width: parent.width
                                        text: wkRow.modelData.title || ""
                                        color: Theme.text3
                                        font.pixelSize: Theme.fontSmall
                                        elide: Text.ElideRight
                                    }
                                }

                                Text {
                                    id: wkTime
                                    anchors { right: parent.right; rightMargin: Theme.space3; verticalCenter: parent.verticalCenter }
                                    text: page.elapsedLabel(wkRow.modelData.started_at, wkRow.modelData.elapsed_seconds)
                                    color: Theme.ok
                                    font.family: Theme.mono
                                    font.pixelSize: Theme.fontCaption
                                }

                                MouseArea {
                                    id: wkArea
                                    anchors.fill: parent
                                    hoverEnabled: true
                                    cursorShape: Qt.PointingHandCursor
                                    onClicked: {
                                        const m = wkRow.modelData;
                                        if (m.run_id)
                                            page.openRun(m.run_id, m.agent_id);
                                        else if (m.kind === "channel" && m.conversation_id)
                                            page.openChannel(m.conversation_id);
                                        else if (m.conversation_id)
                                            page.openAgentConv(m.agent_id, m.conversation_id);
                                        else
                                            page.goPage("tasks");
                                    }
                                }
                            }
                        }
                    }
                }
            }

            // Section 3: Desde a sua última visita (only when non-empty, up to 5 rows)
            Card {
                visible: page.deliveredRows.length > 0
                width: parent.width
                height: delivSec.implicitHeight + 2 * padding
                padding: Theme.space4

                Column {
                    id: delivSec
                    width: parent.width
                    spacing: Theme.space3

                    Overline { text: "DESDE A SUA ÚLTIMA VISITA" }

                    Column {
                        width: parent.width
                        spacing: Theme.space1

                        Repeater {
                            model: page.deliveredRows
                            delegate: Rectangle {
                                id: dRow
                                required property var modelData
                                width: delivSec.width
                                height: 52
                                radius: Theme.radiusMd
                                color: dArea.containsMouse ? Theme.hover : Theme.wash1
                                border.width: 1
                                border.color: dArea.containsMouse ? Theme.lineStrong : Theme.line
                                Behavior on color { ColorAnimation { duration: Theme.fast } }

                                Avatar {
                                    id: dAv
                                    x: Theme.space3
                                    anchors.verticalCenter: parent.verticalCenter
                                    size: 32
                                    name: dRow.modelData.agent_name || dRow.modelData.agent_id
                                    tint: dRow.modelData.agent_color || Theme.ok
                                }

                                Column {
                                    anchors {
                                        left: dAv.right
                                        leftMargin: Theme.space3
                                        right: dWhen.left
                                        rightMargin: Theme.space3
                                        verticalCenter: parent.verticalCenter
                                    }
                                    spacing: 2

                                    Row {
                                        width: parent.width
                                        spacing: Theme.space2
                                        Dot {
                                            kind: "ok"
                                            anchors.verticalCenter: parent.verticalCenter
                                        }
                                        Text {
                                            width: parent.width - 16
                                            text: (dRow.modelData.agent_name || dRow.modelData.agent_id) + " · " + (dRow.modelData.title || "")
                                            color: Theme.text
                                            font.pixelSize: Theme.fontBody
                                            font.weight: Font.DemiBold
                                            elide: Text.ElideRight
                                            anchors.verticalCenter: parent.verticalCenter
                                        }
                                    }

                                    Text {
                                        visible: !!(dRow.modelData.summary)
                                        width: parent.width
                                        text: dRow.modelData.summary || ""
                                        color: Theme.text3
                                        font.pixelSize: Theme.fontSmall
                                        elide: Text.ElideRight
                                    }
                                }

                                Text {
                                    id: dWhen
                                    anchors { right: parent.right; rightMargin: Theme.space3; verticalCenter: parent.verticalCenter }
                                    text: U.ago(dRow.modelData.completed_at)
                                    color: Theme.text4
                                    font.pixelSize: Theme.fontCaption
                                }

                                MouseArea {
                                    id: dArea
                                    anchors.fill: parent
                                    hoverEnabled: true
                                    cursorShape: Qt.PointingHandCursor
                                    onClicked: {
                                        const m = dRow.modelData;
                                        if (m.run_id)
                                            page.openRun(m.run_id, m.agent_id);
                                        else if (m.conversation_id === "time")
                                            page.openChannel("time");
                                        else if (m.conversation_id)
                                            page.openAgentConv(m.agent_id, m.conversation_id);
                                    }
                                }
                            }
                        }
                    }
                }
            }

            // Section 4: Próximas rotinas (only when non-empty, up to 3)
            Card {
                visible: page.routineRows.length > 0
                width: parent.width
                height: rtSec.implicitHeight + 2 * padding
                padding: Theme.space4

                Column {
                    id: rtSec
                    width: parent.width
                    spacing: Theme.space3

                    Overline { text: "PRÓXIMAS ROTINAS" }

                    Column {
                        width: parent.width
                        spacing: Theme.space1

                        Repeater {
                            model: page.routineRows
                            delegate: Rectangle {
                                id: rRow
                                required property var modelData
                                width: rtSec.width
                                height: 46
                                radius: Theme.radiusMd
                                color: Theme.wash1
                                border.width: 1
                                border.color: Theme.line

                                Icon {
                                    id: rIcon
                                    x: Theme.space3
                                    anchors.verticalCenter: parent.verticalCenter
                                    name: "clock"
                                    size: 16
                                    color: rRow.modelData.agent_color || Theme.text3
                                }

                                Text {
                                    anchors {
                                        left: rIcon.right
                                        leftMargin: Theme.space3
                                        right: rNext.left
                                        rightMargin: Theme.space3
                                        verticalCenter: parent.verticalCenter
                                    }
                                    text: (rRow.modelData.title || "") + "  ·  " + (rRow.modelData.agent_name || rRow.modelData.agent_id)
                                    color: Theme.text
                                    font.pixelSize: Theme.fontBody
                                    elide: Text.ElideRight
                                }

                                Text {
                                    id: rNext
                                    anchors { right: parent.right; rightMargin: Theme.space3; verticalCenter: parent.verticalCenter }
                                    text: rRow.modelData.next_run ? U.when(rRow.modelData.next_run) : (rRow.modelData.when || "")
                                    color: Theme.text3
                                    font.family: Theme.mono
                                    font.pixelSize: Theme.fontCaption
                                }
                            }
                        }
                    }
                }
            }

            // Preference: "Ao abrir o app"
            Card {
                width: parent.width
                height: prefCol.implicitHeight + 2 * padding
                padding: Theme.space4

                Column {
                    id: prefCol
                    width: parent.width
                    spacing: Theme.space3

                    Overline { text: "AO ABRIR O APP" }

                    Text {
                        width: parent.width
                        wrapMode: Text.WordWrap
                        text: "Escolha por onde o Avora começa quando você abre o aplicativo."
                        color: Theme.text3
                        font.pixelSize: Theme.fontSmall
                    }

                    Flow {
                        width: parent.width
                        spacing: Theme.space2

                        Pill {
                            text: "Início"
                            on: page.openPref === "home"
                            onHit: page.openPref = "home"
                        }
                        Pill {
                            text: "Última conversa"
                            on: page.openPref === "chat"
                            onHit: page.openPref = "chat"
                        }
                        Repeater {
                            model: page.channels
                            delegate: Pill {
                                required property var modelData
                                text: "#" + modelData.name
                                on: page.openPref === "channel:" + modelData.id
                                onHit: page.openPref = "channel:" + modelData.id
                            }
                        }
                    }
                }
            }
        }
    }
}
