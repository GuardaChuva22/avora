import QtQuick
import QtQuick.Controls.Basic
import Avora
import "util.js" as U

// Rotinas: all workspace routines grouped by agent, with schedule, next run,
// on/off toggle, agent picker for creating a new routine, and execution history.
Item {
    id: page
    property var agents: []
    property var allRoutines: []
    property var allRuns: []
    property bool loading: true
    property string error: ""
    property var toggling: ({})
    property var triggering: ({})
    property int shownHistory: 15
    property real tick: Date.now()

    readonly property var agentMap: {
        const m = {};
        for (const a of agents)
            m[a.id] = a;
        return m;
    }

    readonly property var workspaceRoutines: allRoutines.filter(r => !!agentMap[r.agent_id])
    readonly property var workspaceRuns: allRuns.filter(r => !!agentMap[r.agent_id])

    readonly property int enabledCount: workspaceRoutines.filter(r => !!r.enabled).length
    readonly property bool anyActive: workspaceRoutines.some(r => r.last_status === "running")
        || workspaceRuns.some(r => !!r.active)

    readonly property var agentGroups: {
        const groups = [];
        for (const a of agents) {
            const list = workspaceRoutines.filter(r => r.agent_id === a.id);
            if (list.length === 0)
                continue;
            groups.push({
                agent: a,
                items: list,
                activeCount: list.filter(r => !!r.enabled).length
            });
        }
        return groups;
    }

    readonly property var historyItems: {
        void page.tick;
        const list = [];

        for (const r of workspaceRoutines) {
            if (!r.last_run)
                continue;
            const ag = agentMap[r.agent_id] || null;
            const cleanPrev = (r.last_preview || "").replace(/[*_`#>]/g, "").trim();
            const mappedStatus = r.last_status === "ok" ? "succeeded"
                : r.last_status === "running" ? "running"
                : r.last_status === "failed" ? "failed"
                : "canceled";
            list.push({
                id: "rt:" + r.id,
                kind: "routine",
                runId: "",
                routine: r,
                agent: ag,
                title: r.title || "Rotina",
                badge: "Rotina",
                status: mappedStatus,
                skipped: r.last_status === "skipped",
                detail: cleanPrev,
                at: r.last_run,
                ts: new Date(r.last_run).getTime() || 0
            });
        }

        for (const run of workspaceRuns) {
            const ag = agentMap[run.agent_id] || null;
            const stamp = run.completed_at || run.started_at || run.created_at;
            list.push({
                id: "run:" + run.id,
                kind: "run",
                runId: run.id,
                routine: null,
                agent: ag,
                title: run.title || engineLabel(run.engine),
                badge: engineLabel(run.engine),
                status: run.status || "queued",
                skipped: false,
                detail: runDurationText(run),
                at: stamp,
                ts: stamp ? (new Date(stamp).getTime() || 0) : 0
            });
        }

        list.sort((a, b) => b.ts - a.ts);
        return list;
    }

    readonly property var visibleHistory: historyItems.slice(0, shownHistory)

    function engineLabel(eng) {
        switch (eng) {
        case "claude": return "Claude Code";
        case "codex": return "Codex";
        case "antigravity": return "Antigravity";
        default: return eng || "Execução";
        }
    }

    function runDurationText(r) {
        if (r.active)
            return "em andamento";
        if (r.started_at && r.completed_at) {
            const ms = new Date(r.completed_at).getTime() - new Date(r.started_at).getTime();
            if (ms > 0)
                return "durou " + U.seconds(ms);
        }
        return "";
    }

    function subtitleText() {
        if (loading && agents.length === 0 && allRoutines.length === 0)
            return "Carregando as rotinas e o histórico deste espaço…";
        const total = workspaceRoutines.length;
        if (total === 0)
            return "Tarefas recorrentes que os agentes executam sozinhos em horários ou eventos.";
        const totalTxt = total === 1 ? "1 rotina configurada" : total + " rotinas configuradas";
        const onTxt = enabledCount === 0 ? "nenhuma ligada"
            : enabledCount === 1 ? "1 ligada"
            : enabledCount + " ligadas";
        return totalTxt + "  ·  " + onTxt + ".";
    }

    function statusColor(r, agColor) {
        if (!r.enabled)
            return Theme.text4;
        switch (r.last_status) {
        case "ok": return Theme.ok;
        case "failed": return Theme.bad;
        case "running": return agColor || Theme.live;
        default: return Theme.text3;
        }
    }

    function nextRunText(r) {
        if (!r.enabled)
            return "Desligada";
        if (r.next_run)
            return "Próxima: " + U.when(r.next_run);
        if (r.trigger)
            return "Por evento";
        return "Ligada";
    }

    function lastRunSummary(r) {
        const prev = (r.last_preview || "").replace(/[*_`#>]/g, "").trim();
        switch (r.last_status) {
        case "running":
            return "Rodando agora…";
        case "ok":
            return "Última vez " + U.ago(r.last_run) + (prev !== "" ? " · " + prev : "");
        case "failed":
            return "Falhou " + U.ago(r.last_run)
                + (r.failures > 1 ? " (" + r.failures + "× seguidas)" : "")
                + (prev !== "" ? " · " + prev : "");
        case "skipped":
            return "Pulou " + U.ago(r.last_run) + (prev !== "" ? " · " + prev : "");
        default:
            return "";
        }
    }

    function toggleRoutine(r) {
        if (!r || toggling[r.id])
            return;
        const next = Object.assign({}, toggling);
        next[r.id] = true;
        toggling = next;
        error = "";

        const body = {
            agent_id: r.agent_id,
            title: r.title,
            prompt: r.prompt,
            schedule: r.schedule || {},
            trigger: r.trigger || null,
            deliver: r.deliver || "time",
            responsibility_id: r.responsibility_id || "",
            skill: r.skill || "",
            enabled: !r.enabled
        };

        Api.put("/v1/routines/" + encodeURIComponent(r.id), body, err => {
            const done = Object.assign({}, page.toggling);
            delete done[r.id];
            page.toggling = done;
            if (err)
                page.error = err;
            page.load();
        });
    }

    function runRoutineNow(r) {
        if (!r || triggering[r.id])
            return;
        const next = Object.assign({}, triggering);
        next[r.id] = true;
        triggering = next;
        error = "";

        Api.post("/v1/routines/" + encodeURIComponent(r.id) + "/run", null, err => {
            const done = Object.assign({}, page.triggering);
            delete done[r.id];
            page.triggering = done;
            if (err)
                page.error = err;
            page.load();
        });
    }

    function startNewRoutine() {
        if (agents.length === 0) {
            if (typeof win !== "undefined" && win && win.newAgent)
                win.newAgent();
            return;
        }
        agentPickerSheet.shown = true;
    }

    function triggerOpenAgent(id) {
        if (!id || typeof win === "undefined" || !win)
            return;
        if (win.openAgent)
            win.openAgent(id);
        else if (win.openAgentConv)
            win.openAgentConv(id, "");
    }

    function openHistoryItem(item) {
        if (!item || typeof win === "undefined" || !win)
            return;
        if (item.kind === "run" && item.runId) {
            if (win.openFloat)
                win.openFloat("RunPane.qml", { runId: item.runId, agents: page.agents });
            else if (win.openRun)
                win.openRun(item.runId, item.agent ? item.agent.id : "");
            return;
        }
        if (item.kind === "routine" && item.routine) {
            if (item.routine.deliver === "time" && win.openChannel)
                win.openChannel("time");
            else if (item.agent && win.openAgent)
                win.openAgent(item.agent.id);
        }
    }

    function load() {
        Api.get("/v1/agents", (err, list) => {
            if (!err && !U.same(agents, list || []))
                agents = list || [];
        });
        Api.get("/v1/routines", (err, list) => {
            loading = false;
            if (err) {
                error = err;
                return;
            }
            error = "";
            if (!U.same(allRoutines, list || []))
                allRoutines = list || [];
        });
        Api.get("/v1/runs", (err, list) => {
            if (!err && !U.same(allRuns, list || []))
                allRuns = list || [];
        });
    }

    Component.onCompleted: load()
    onVisibleChanged: if (visible) load()

    Connections {
        target: Api
        function onWorkspaceChanged() {
            page.agents = [];
            page.allRoutines = [];
            page.allRuns = [];
            page.loading = true;
            page.shownHistory = 15;
            page.load();
        }
        function onLiveEvent(e) {
            if (!page.visible)
                return;
            const t = e.type || "";
            if (t.startsWith("routine.") || t.startsWith("job.") || t.startsWith("agent.") || t.startsWith("message."))
                reloadDebounce.restart();
        }
        function onLiveReset() {
            if (page.visible)
                page.load();
        }
    }

    Timer {
        id: reloadDebounce
        interval: 250
        onTriggered: {
            page.tick = Date.now();
            page.load();
        }
    }

    Timer {
        interval: page.anyActive ? 5000 : (Api.live ? 20000 : 6000)
        running: page.visible
        repeat: true
        onTriggered: {
            page.tick = Date.now();
            page.load();
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
            readonly property real colWidth: Math.min(scroller.width - 2 * Theme.gutter, 860)
            x: Math.max(Theme.gutter, (scroller.width - colWidth) / 2)
            y: 44
            width: colWidth
            spacing: Theme.space5

            // Header
            Item {
                width: parent.width
                height: Math.max(headBlock.implicitHeight, newBtn.implicitHeight)

                PageHead {
                    id: headBlock
                    width: Math.max(240, parent.width - newBtn.width - Theme.space3)
                    eyebrow: "Autonomia"
                    title: "Rotinas"
                    sub: page.subtitleText()
                }

                PrimaryButton {
                    id: newBtn
                    anchors { right: parent.right; bottom: headBlock.bottom }
                    icon: "plus"
                    text: page.agents.length > 0 ? "Nova rotina" : "Novo agente"
                    onClicked: page.startNewRoutine()
                }
            }

            ErrorBanner {
                width: parent.width
                text: page.error
            }

            // Loading state
            Card {
                visible: page.loading && page.workspaceRoutines.length === 0
                width: parent.width
                height: 88
                padding: Theme.space5

                Text {
                    anchors.verticalCenter: parent.verticalCenter
                    text: "Carregando as rotinas deste espaço…"
                    color: Theme.text3
                    font.pixelSize: Theme.fontBody
                }
            }

            // Empty routines state
            Card {
                visible: !page.loading && page.workspaceRoutines.length === 0
                width: parent.width
                height: emptyCol.implicitHeight + 2 * padding
                padding: Theme.space5

                Column {
                    id: emptyCol
                    width: parent.width
                    spacing: Theme.space3

                    Text {
                        text: page.agents.length === 0
                            ? "Crie um agente antes de agendar rotinas"
                            : "Nenhuma rotina configurada ainda"
                        color: Theme.white
                        font.family: Theme.display
                        font.pixelSize: Theme.fontHeading
                        font.weight: Font.DemiBold
                    }

                    Text {
                        width: parent.width
                        wrapMode: Text.WordWrap
                        text: page.agents.length === 0
                            ? "Cada rotina pertence a um agente do espaço. Crie o primeiro agente para agendar resumos, acompanhamentos ou alertas."
                            : "Rotinas fazem um agente trabalhar sozinho em horários fixos, intervalos ou quando algo acontecer, entregando o resultado no canal ou na conversa."
                        color: Theme.text3
                        font.pixelSize: Theme.fontBody
                    }

                    Row {
                        spacing: Theme.space2
                        PrimaryButton {
                            icon: "plus"
                            text: page.agents.length === 0 ? "Novo agente" : "Nova rotina"
                            onClicked: page.startNewRoutine()
                        }
                    }
                }
            }

            // ── Routines grouped by agent ──────────────────────────
            Repeater {
                model: page.agentGroups
                delegate: Card {
                    id: grpCard
                    required property var modelData

                    readonly property var ag: modelData.agent
                    readonly property color agColor: ag && ag.color ? ag.color : Theme.text2

                    width: body.width
                    height: grpCol.implicitHeight + 2 * padding
                    padding: Theme.space4

                    Column {
                        id: grpCol
                        width: parent.width
                        spacing: Theme.space3

                        // Agent header row
                        Item {
                            width: parent.width
                            height: 42

                            Row {
                                id: agHdrLeft
                                anchors { left: parent.left; verticalCenter: parent.verticalCenter }
                                spacing: Theme.space3

                                Avatar {
                                    size: 38
                                    name: grpCard.ag.name
                                    photo: U.photo(grpCard.ag)
                                    tint: grpCard.agColor
                                    working: !!grpCard.ag.working
                                    anchors.verticalCenter: parent.verticalCenter
                                }

                                Column {
                                    anchors.verticalCenter: parent.verticalCenter
                                    spacing: Theme.space1

                                    Text {
                                        text: grpCard.ag.name
                                        color: agLinkArea.containsMouse ? Theme.white : Theme.text
                                        font.pixelSize: Theme.fontLarge
                                        font.weight: Font.DemiBold
                                        font.underline: agLinkArea.containsMouse

                                        MouseArea {
                                            id: agLinkArea
                                            anchors.fill: parent
                                            hoverEnabled: true
                                            cursorShape: Qt.PointingHandCursor
                                            onClicked: page.triggerOpenAgent(grpCard.ag.id)
                                        }
                                    }

                                    Text {
                                        text: {
                                            const parts = [];
                                            if (grpCard.ag.role)
                                                parts.push(grpCard.ag.role);
                                            const n = grpCard.modelData.items.length;
                                            const on = grpCard.modelData.activeCount;
                                            parts.push((n === 1 ? "1 rotina" : n + " rotinas")
                                                       + " · " + (on === 0 ? "nenhuma ligada" : on + (on === 1 ? " ligada" : " ligadas")));
                                            return parts.join("  ·  ");
                                        }
                                        color: Theme.text3
                                        font.pixelSize: Theme.fontSmall
                                    }
                                }
                            }

                            GhostButton {
                                anchors { right: parent.right; verticalCenter: parent.verticalCenter }
                                small: true
                                icon: "plus"
                                text: "Nova rotina"
                                onClicked: routineEditor.open(grpCard.ag, null, null)
                            }
                        }

                        // Agent's routine items
                        Column {
                            width: parent.width
                            spacing: Theme.space2

                            Repeater {
                                model: grpCard.modelData.items
                                delegate: Rectangle {
                                    id: rtRow
                                    required property var modelData
                                    readonly property var r: modelData
                                    readonly property string lastText: {
                                        void page.tick;
                                        return page.lastRunSummary(r);
                                    }
                                    readonly property bool busyRun: !!page.triggering[r.id] || r.last_status === "running"

                                    width: grpCol.width
                                    height: rtTextCol.implicitHeight + 2 * Theme.space3
                                    radius: Theme.radiusMd
                                    color: rtClickArea.containsMouse ? Theme.hover : Theme.wash1
                                    border.width: 1
                                    border.color: rtClickArea.containsMouse ? Theme.lineStrong : Theme.line
                                    opacity: r.enabled ? 1 : 0.62
                                    Behavior on color { ColorAnimation { duration: Theme.fast } }

                                    MouseArea {
                                        id: rtClickArea
                                        anchors.fill: parent
                                        hoverEnabled: true
                                        cursorShape: Qt.PointingHandCursor
                                        onClicked: routineEditor.open(grpCard.ag, rtRow.r, null)
                                    }

                                    Column {
                                        id: rtTextCol
                                        x: Theme.space3
                                        y: Theme.space3
                                        width: parent.width - rtControls.width - 3 * Theme.space3
                                        spacing: Theme.space1

                                        Row {
                                            width: parent.width
                                            spacing: Theme.space2

                                            Rectangle {
                                                width: 8
                                                height: 8
                                                radius: width / 2
                                                anchors.verticalCenter: parent.verticalCenter
                                                color: page.statusColor(rtRow.r, grpCard.agColor)

                                                SequentialAnimation on opacity {
                                                    running: rtRow.r.last_status === "running" && rtRow.visible
                                                    loops: Animation.Infinite
                                                    NumberAnimation { to: 0.3; duration: 600 }
                                                    NumberAnimation { to: 1.0; duration: 600 }
                                                }
                                            }

                                            Text {
                                                width: parent.width - 16
                                                text: rtRow.r.title
                                                color: Theme.white
                                                font.pixelSize: Theme.fontBody
                                                font.weight: Font.DemiBold
                                                elide: Text.ElideRight
                                                anchors.verticalCenter: parent.verticalCenter
                                            }
                                        }

                                        // When it runs + next run time
                                        Flow {
                                            width: parent.width
                                            spacing: Theme.space2

                                            Text {
                                                text: U.whenText(rtRow.r)
                                                    + (rtRow.r.deliver === "time" ? "  ·  #time" : "  ·  conversa própria")
                                                color: Theme.text2
                                                font.pixelSize: Theme.fontSmall
                                            }

                                            Text {
                                                text: "·  " + page.nextRunText(rtRow.r)
                                                color: !rtRow.r.enabled ? Theme.text4
                                                    : rtRow.r.next_run ? Theme.ok
                                                    : Theme.text3
                                                font.family: Theme.mono
                                                font.pixelSize: Theme.fontCaption
                                            }
                                        }

                                        // Last run outcome
                                        Text {
                                            visible: rtRow.lastText !== ""
                                            width: parent.width
                                            text: rtRow.lastText
                                            color: rtRow.r.last_status === "failed" ? Theme.bad : Theme.text3
                                            font.pixelSize: Theme.fontSmall
                                            maximumLineCount: 2
                                            elide: Text.ElideRight
                                            wrapMode: Text.WordWrap
                                        }
                                    }

                                    // Run now + On/off toggle
                                    Row {
                                        id: rtControls
                                        anchors {
                                            right: parent.right
                                            rightMargin: Theme.space3
                                            verticalCenter: parent.verticalCenter
                                        }
                                        spacing: Theme.space3

                                        GhostButton {
                                            small: true
                                            icon: "bolt"
                                            enabled: !rtRow.busyRun
                                            text: rtRow.r.last_status === "running" ? "Rodando" : "Rodar"
                                            anchors.verticalCenter: parent.verticalCenter
                                            onClicked: page.runRoutineNow(rtRow.r)
                                        }

                                        Rectangle {
                                            width: 36
                                            height: 20
                                            radius: height / 2
                                            anchors.verticalCenter: parent.verticalCenter
                                            color: rtRow.r.enabled ? Theme.white : Theme.wash4
                                            border.width: 1
                                            border.color: rtRow.r.enabled ? Theme.white : Theme.line
                                            Behavior on color { ColorAnimation { duration: Theme.fast } }

                                            Rectangle {
                                                width: 14
                                                height: 14
                                                radius: width / 2
                                                y: 3
                                                x: rtRow.r.enabled ? parent.width - width - 3 : 3
                                                color: rtRow.r.enabled ? Theme.ink : Theme.text3
                                                Behavior on x { NumberAnimation { duration: Theme.fast; easing.type: Theme.ease } }
                                            }

                                            MouseArea {
                                                anchors.fill: parent
                                                anchors.margins: -Theme.space1
                                                cursorShape: Qt.PointingHandCursor
                                                onClicked: page.toggleRoutine(rtRow.r)
                                            }
                                        }
                                    }
                                }
                            }
                        }
                    }
                }
            }

            // ── Histórico das últimas execuções ────────────────────
            Column {
                id: historySec
                width: parent.width
                spacing: Theme.space3

                Overline {
                    text: "HISTÓRICO DAS ÚLTIMAS EXECUÇÕES"
                        + (page.historyItems.length > 0 ? "  ·  " + page.historyItems.length : "")
                }

                Card {
                    visible: !page.loading && page.historyItems.length === 0
                    width: parent.width
                    height: emptyHistTxt.implicitHeight + 2 * padding
                    padding: Theme.space5

                    Text {
                        id: emptyHistTxt
                        width: parent.width
                        wrapMode: Text.WordWrap
                        text: "Ainda não houve nenhuma execução neste espaço. Quando uma rotina rodar no horário ou você clicar em Rodar, o histórico aparece aqui."
                        color: Theme.text3
                        font.pixelSize: Theme.fontBody
                    }
                }

                Card {
                    visible: page.visibleHistory.length > 0
                    width: parent.width
                    height: histCol.implicitHeight + 2 * padding
                    padding: Theme.space2

                    Column {
                        id: histCol
                        width: parent.width

                        Repeater {
                            model: page.visibleHistory
                            delegate: Rectangle {
                                id: hRow
                                required property var modelData
                                required property int index

                                readonly property var ag: modelData.agent

                                width: histCol.width
                                height: 58
                                radius: Theme.radiusMd
                                color: hArea.containsMouse ? Theme.hover : "transparent"
                                Behavior on color { ColorAnimation { duration: Theme.fast } }

                                Rectangle {
                                    visible: hRow.index > 0 && !hArea.containsMouse
                                    anchors {
                                        top: parent.top
                                        left: parent.left
                                        right: parent.right
                                        leftMargin: Theme.space3
                                        rightMargin: Theme.space3
                                    }
                                    height: 1
                                    color: Theme.line
                                }

                                Avatar {
                                    id: hAv
                                    x: Theme.space3
                                    size: 34
                                    name: hRow.ag ? hRow.ag.name : "?"
                                    photo: hRow.ag ? U.photo(hRow.ag) : ""
                                    tint: hRow.ag && hRow.ag.color ? hRow.ag.color : Theme.text2
                                    working: hRow.modelData.status === "running"
                                    anchors.verticalCenter: parent.verticalCenter
                                }

                                Column {
                                    anchors {
                                        left: hAv.right
                                        leftMargin: Theme.space3
                                        right: hRight.left
                                        rightMargin: Theme.space3
                                        verticalCenter: parent.verticalCenter
                                    }
                                    spacing: Theme.space1

                                    Row {
                                        width: parent.width
                                        spacing: Theme.space2

                                        Rectangle {
                                            height: 18
                                            width: hKindLbl.implicitWidth + 2 * Theme.space2
                                            radius: height / 2
                                            color: Theme.wash3
                                            anchors.verticalCenter: parent.verticalCenter

                                            Text {
                                                id: hKindLbl
                                                anchors.centerIn: parent
                                                text: hRow.modelData.badge
                                                color: Theme.text2
                                                font.pixelSize: Theme.fontTiny
                                                font.weight: Font.DemiBold
                                            }
                                        }

                                        Text {
                                            width: Math.max(80, parent.width - 110)
                                            text: (hRow.ag ? hRow.ag.name + " · " : "") + hRow.modelData.title
                                            color: Theme.white
                                            font.pixelSize: Theme.fontBody
                                            font.weight: Font.Medium
                                            elide: Text.ElideRight
                                            anchors.verticalCenter: parent.verticalCenter
                                        }
                                    }

                                    Text {
                                        visible: hRow.modelData.detail !== ""
                                        width: parent.width
                                        text: hRow.modelData.detail
                                        color: hRow.modelData.status === "failed" ? Theme.bad : Theme.text3
                                        font.pixelSize: Theme.fontSmall
                                        elide: Text.ElideRight
                                    }
                                }

                                Row {
                                    id: hRight
                                    anchors {
                                        right: parent.right
                                        rightMargin: Theme.space3
                                        verticalCenter: parent.verticalCenter
                                    }
                                    spacing: Theme.space3

                                    StatusPill {
                                        visible: !hRow.modelData.skipped
                                        status: hRow.modelData.status
                                        anchors.verticalCenter: parent.verticalCenter
                                    }

                                    Rectangle {
                                        visible: hRow.modelData.skipped
                                        height: 24
                                        width: skipLbl.implicitWidth + 2 * Theme.space3
                                        radius: height / 2
                                        color: Theme.hover
                                        border.width: 1
                                        border.color: Theme.line
                                        anchors.verticalCenter: parent.verticalCenter

                                        Text {
                                            id: skipLbl
                                            anchors.centerIn: parent
                                            text: "Pulou"
                                            color: Theme.text3
                                            font.pixelSize: Theme.fontSmall
                                            font.weight: Font.Medium
                                        }
                                    }

                                    Text {
                                        anchors.verticalCenter: parent.verticalCenter
                                        text: {
                                            void page.tick;
                                            return U.ago(hRow.modelData.at);
                                        }
                                        color: Theme.text4
                                        font.family: Theme.mono
                                        font.pixelSize: Theme.fontCaption
                                    }
                                }

                                MouseArea {
                                    id: hArea
                                    anchors.fill: parent
                                    hoverEnabled: true
                                    cursorShape: Qt.PointingHandCursor
                                    onClicked: page.openHistoryItem(hRow.modelData)
                                }
                            }
                        }
                    }
                }

                GhostButton {
                    visible: page.historyItems.length > page.shownHistory
                    small: true
                    text: "Mostrar mais execuções"
                    onClicked: page.shownHistory += 15
                }
            }
        }
    }

    // Agent picker sheet for "Nova rotina"
    Sheet {
        id: agentPickerSheet
        anchors.fill: parent
        sheetWidth: 460

        Column {
            width: parent.width
            spacing: Theme.space1

            Text {
                text: "Nova rotina"
                color: Theme.white
                font.family: Theme.display
                font.pixelSize: Theme.fontHeading
                font.weight: Font.DemiBold
            }

            Text {
                width: parent.width
                wrapMode: Text.WordWrap
                text: "Escolha qual agente vai rodar esta rotina e entregar o resultado."
                color: Theme.text3
                font.pixelSize: Theme.fontBody
            }
        }

        Column {
            width: parent.width
            spacing: Theme.space2

            Overline { text: "AGENTES DO ESPAÇO" }

            Repeater {
                model: page.agents
                delegate: Rectangle {
                    id: pickRow
                    required property var modelData
                    width: agentPickerSheet.form.width
                    height: 54
                    radius: Theme.radiusMd
                    color: pickArea.pressed ? Theme.active : pickArea.containsMouse ? Theme.hover : Theme.wash1
                    border.width: 1
                    border.color: pickArea.containsMouse ? Theme.focusLine : Theme.line
                    Behavior on color { ColorAnimation { duration: Theme.fast } }

                    Avatar {
                        id: pickAv
                        x: Theme.space3
                        size: 36
                        name: pickRow.modelData.name
                        photo: U.photo(pickRow.modelData)
                        tint: pickRow.modelData.color || Theme.text2
                        anchors.verticalCenter: parent.verticalCenter
                    }

                    Column {
                        anchors {
                            left: pickAv.right
                            leftMargin: Theme.space3
                            right: pickChevron.left
                            rightMargin: Theme.space3
                            verticalCenter: parent.verticalCenter
                        }
                        spacing: Theme.space1

                        Text {
                            width: parent.width
                            text: pickRow.modelData.name
                            color: Theme.white
                            font.pixelSize: Theme.fontLarge
                            font.weight: Font.DemiBold
                            elide: Text.ElideRight
                        }

                        Text {
                            visible: !!pickRow.modelData.role
                            width: parent.width
                            text: pickRow.modelData.role || ""
                            color: Theme.text3
                            font.pixelSize: Theme.fontSmall
                            elide: Text.ElideRight
                        }
                    }

                    Icon {
                        id: pickChevron
                        anchors {
                            right: parent.right
                            rightMargin: Theme.space3
                            verticalCenter: parent.verticalCenter
                        }
                        name: "chevron"
                        size: 15
                        color: pickArea.containsMouse ? Theme.white : Theme.text3
                    }

                    MouseArea {
                        id: pickArea
                        anchors.fill: parent
                        hoverEnabled: true
                        cursorShape: Qt.PointingHandCursor
                        onClicked: {
                            const chosen = pickRow.modelData;
                            agentPickerSheet.shown = false;
                            routineEditor.open(chosen, null, null);
                        }
                    }
                }
            }
        }

        Row {
            spacing: Theme.space2
            GhostButton {
                text: "Cancelar"
                onClicked: agentPickerSheet.shown = false
            }
        }
    }

    RoutineEditor {
        id: routineEditor
        anchors.fill: parent
        onSaved: page.load()
    }
}
