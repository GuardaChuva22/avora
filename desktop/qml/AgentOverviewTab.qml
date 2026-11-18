import QtQuick
import QtQuick.Controls.Basic
import Avora
import "util.js" as U

// Agent overview: live status, 30-day usage, summary sheet, recent runs and shortcuts.
Column {
    id: tab
    required property var page
    spacing: Theme.space5

    property var activeRuns: []
    property var recentRuns: []
    property var routines: []
    property var memories: []
    property bool loadingActive: true
    property bool loadingRuns: true
    property bool loadingRoutines: true
    property bool loadingMemories: true
    property string error: ""
    property real tick: Date.now()
    property int draftRev: 0
    property int hoveredDay: -1
    property bool ready: false

    readonly property string agentId: page && page.agent ? page.agent.id : ""
    readonly property color agentColor: (page && page.draft && page.draft.color) ? page.draft.color
                                      : (page && page.agent && page.agent.color) ? page.agent.color
                                      : Theme.white
    readonly property var usage: page ? page.usage : null
    readonly property bool agentWorking: !!(page && page.agent && page.agent.working)

    readonly property var days14: {
        const u = tab.usage;
        const byDay = {};
        if (u && u.by_day) {
            for (const d of u.by_day)
                byDay[d.day] = (d.prompt_tokens || 0) + (d.completion_tokens || 0);
        }
        const now = new Date();
        const out = [];
        for (let i = 13; i >= 0; i--) {
            const dt = new Date(now.getFullYear(), now.getMonth(), now.getDate() - i);
            const key = dt.getFullYear() + "-" + U.pad(dt.getMonth() + 1) + "-" + U.pad(dt.getDate());
            out.push({
                day: key,
                label: U.pad(dt.getDate()) + "/" + U.pad(dt.getMonth() + 1),
                tokens: byDay[key] || 0
            });
        }
        return out;
    }

    readonly property int maxDayTokens: {
        let m = 0;
        for (const d of tab.days14) {
            if (d.tokens > m)
                m = d.tokens;
        }
        return m > 0 ? m : 1;
    }

    readonly property string brainText: {
        void tab.draftRev;
        const src = (tab.page && tab.page.draft) ? tab.page.draft : (tab.page ? tab.page.agent : null);
        const b = src && src.brain ? src.brain : {};
        const prov = b.provider || "";
        const brains = (tab.page && tab.page.brains) ? tab.page.brains : [];
        const opt = prov !== "" ? (brains.find(x => x.provider === prov) || null) : null;
        const parts = [prov === "" ? "Automático" : (opt ? opt.name : prov)];
        if (b.model) {
            const mName = opt && opt.model_names && opt.model_names[b.model] ? opt.model_names[b.model] : b.model;
            parts.push(mName);
        }
        if (b.effort) {
            const eff = ({ low: "baixo", medium: "médio", high: "alto", max: "máximo" })[b.effort] || b.effort;
            parts.push("esforço " + eff);
        }
        return parts.join(" · ");
    }

    readonly property string approvalsText: {
        void tab.draftRev;
        const src = (tab.page && tab.page.draft) ? tab.page.draft : (tab.page ? tab.page.agent : null);
        const v = src && src.approvals ? src.approvals : "";
        if (v === "always")
            return "Sempre pergunta";
        if (v === "never")
            return "Age sozinho";
        return "Só o arriscado";
    }

    readonly property string projectText: tab.page && tab.page.project ? tab.page.project.name : "Universal"

    readonly property string routinesText: {
        if (tab.loadingRoutines)
            return "…";
        const total = tab.routines.length;
        if (total === 0)
            return "Nenhuma";
        const on = tab.routines.filter(r => r.enabled).length;
        return total + (total === 1 ? " rotina · " : " rotinas · ")
               + (on === 0 ? "nenhuma ativa" : on + (on === 1 ? " ativa" : " ativas"));
    }

    readonly property string memoriesText: {
        if (tab.loadingMemories)
            return "…";
        const n = tab.memories.length;
        if (n === 0)
            return "Nenhuma";
        return n === 1 ? "1 memória" : n + " memórias";
    }

    readonly property string versionText: {
        void tab.tick;
        const a = tab.page ? tab.page.agent : null;
        if (!a)
            return "—";
        return "v" + a.version + " · mudou " + U.ago(a.updated_at);
    }

    readonly property var fichaRows: [
        { key: "brain", icon: "spark", label: "Cérebro", target: "settings" },
        { key: "approvals", icon: "lock", label: "Aprovações", target: "tools" },
        { key: "project", icon: "folder", label: "Projeto", target: "settings" },
        { key: "routines", icon: "clock", label: "Rotinas", target: "instructions" },
        { key: "memories", icon: "bookmark", label: "Memórias", target: "memory" },
        { key: "version", icon: "history", label: "Versão", target: "instructions" }
    ]

    Component.onCompleted: {
        ready = true;
        load();
    }

    onAgentIdChanged: {
        if (!ready)
            return;
        activeRuns = [];
        recentRuns = [];
        routines = [];
        memories = [];
        loadingActive = true;
        loadingRuns = true;
        loadingRoutines = true;
        loadingMemories = true;
        error = "";
        load();
    }

    function load() {
        if (agentId === "")
            return;
        loadRuns();
        loadRoutines();
        loadMemories();
    }

    function loadRuns() {
        const id = agentId;
        if (id === "")
            return;
        Api.get("/v1/runs?agent=" + id + "&active=1", (err, list) => {
            if (tab.agentId !== id)
                return;
            tab.loadingActive = false;
            if (err) {
                tab.error = err;
                return;
            }
            const next = list || [];
            if (!U.same(tab.activeRuns, next))
                tab.activeRuns = next;
        });
        Api.get("/v1/runs?agent=" + id, (err, list) => {
            if (tab.agentId !== id)
                return;
            tab.loadingRuns = false;
            if (err) {
                tab.error = err;
                return;
            }
            tab.error = "";
            const next = list || [];
            if (!U.same(tab.recentRuns, next))
                tab.recentRuns = next;
        });
    }

    function loadRoutines() {
        const id = agentId;
        if (id === "")
            return;
        Api.get("/v1/routines?agent=" + id, (err, list) => {
            if (tab.agentId !== id)
                return;
            tab.loadingRoutines = false;
            if (!err)
                tab.routines = list || [];
        });
    }

    function loadMemories() {
        const id = agentId;
        if (id === "")
            return;
        Api.get("/v1/agents/" + id + "/memories", (err, list) => {
            if (tab.agentId !== id)
                return;
            tab.loadingMemories = false;
            if (!err)
                tab.memories = list || [];
        });
    }

    Connections {
        target: tab.page
        ignoreUnknownSignals: true
        function onDraftReset() {
            tab.draftRev++;
        }
    }

    Connections {
        target: Api
        function onLiveEvent(e) {
            const t = e && e.type ? e.type : "";
            if (t.startsWith("job."))
                liveRunsTimer.restart();
            else if (t.startsWith("routine."))
                liveMetaTimer.restart();
            else if (t.startsWith("agent.memory_"))
                liveMetaTimer.restart();
        }
    }

    Timer {
        id: liveRunsTimer
        interval: 400
        onTriggered: {
            tab.tick = Date.now();
            tab.loadRuns();
        }
    }

    Timer {
        id: liveMetaTimer
        interval: 400
        onTriggered: {
            tab.loadRoutines();
            tab.loadMemories();
        }
    }

    Timer {
        interval: 5000
        repeat: true
        running: tab.visible && (tab.activeRuns.length > 0 || tab.agentWorking)
        onTriggered: {
            tab.tick = Date.now();
            tab.loadRuns();
        }
    }

    Timer {
        interval: 30000
        repeat: true
        running: tab.visible
        onTriggered: tab.tick = Date.now()
    }

    ErrorBanner {
        width: parent.width
        text: tab.error
    }

    // ── Agora ───────────────────────────────────────────────────
    Column {
        width: parent.width
        spacing: Theme.space2

        Overline { text: "AGORA" }

        Rectangle {
            width: parent.width
            height: nowBody.implicitHeight + Theme.space3 * 2
            radius: Theme.radiusMd
            color: Theme.wash1
            border.width: 1
            border.color: Theme.line

            Column {
                id: nowBody
                x: Theme.space3
                y: Theme.space3
                width: parent.width - Theme.space3 * 2
                spacing: Theme.space1

                Repeater {
                    model: tab.activeRuns
                    delegate: Rectangle {
                        id: actRow
                        required property var modelData
                        width: nowBody.width
                        height: 38
                        radius: Theme.radiusSm
                        color: actArea.containsMouse ? Theme.hover : "transparent"
                        Behavior on color { ColorAnimation { duration: Theme.fast } }

                        Rectangle {
                            id: actDot
                            x: Theme.space2
                            width: 8
                            height: 8
                            radius: width / 2
                            anchors.verticalCenter: parent.verticalCenter
                            color: tab.agentColor
                        }

                        Text {
                            anchors {
                                left: actDot.right
                                leftMargin: Theme.space3
                                right: actRight.left
                                rightMargin: Theme.space3
                                verticalCenter: parent.verticalCenter
                            }
                            text: actRow.modelData.title || ""
                            color: Theme.text
                            font.pixelSize: Theme.fontBody
                            font.weight: Font.Medium
                            elide: Text.ElideRight
                        }

                        Row {
                            id: actRight
                            anchors {
                                right: parent.right
                                rightMargin: Theme.space2
                                verticalCenter: parent.verticalCenter
                            }
                            spacing: Theme.space2

                            Text {
                                anchors.verticalCenter: parent.verticalCenter
                                text: {
                                    tab.tick; // re-read every tick
                                    return U.ago(actRow.modelData.started_at || actRow.modelData.created_at);
                                }
                                color: Theme.text3
                                font.pixelSize: Theme.fontSmall
                            }

                            Icon {
                                anchors.verticalCenter: parent.verticalCenter
                                name: "chevron"
                                size: 14
                                color: Theme.text3
                                opacity: actArea.containsMouse ? 1 : 0
                                Behavior on opacity { NumberAnimation { duration: Theme.fast } }
                            }
                        }

                        MouseArea {
                            id: actArea
                            anchors.fill: parent
                            hoverEnabled: true
                            cursorShape: Qt.PointingHandCursor
                            onClicked: tab.page.openRun(actRow.modelData.id)
                        }
                    }
                }

                Row {
                    visible: tab.activeRuns.length === 0 && tab.agentWorking
                    width: parent.width
                    spacing: Theme.space3
                    leftPadding: Theme.space1
                    topPadding: Theme.space1
                    bottomPadding: Theme.space1

                    Rectangle {
                        width: 8
                        height: 8
                        radius: width / 2
                        anchors.verticalCenter: parent.verticalCenter
                        color: tab.agentColor
                    }

                    Text {
                        anchors.verticalCenter: parent.verticalCenter
                        text: "Respondendo uma conversa agora."
                        color: Theme.text
                        font.pixelSize: Theme.fontBody
                    }
                }

                Text {
                    visible: tab.activeRuns.length === 0 && !tab.agentWorking
                    width: parent.width
                    leftPadding: Theme.space1
                    topPadding: Theme.space1
                    bottomPadding: Theme.space1
                    text: tab.loadingActive ? "Verificando atividade…" : "Livre. Quando estiver trabalhando, aparece aqui."
                    color: tab.loadingActive ? Theme.text4 : Theme.text3
                    font.pixelSize: Theme.fontBody
                    wrapMode: Text.Wrap
                }
            }
        }
    }

    // ── Este mês ────────────────────────────────────────────────
    Column {
        width: parent.width
        spacing: Theme.space3

        Overline { text: "ÚLTIMOS 30 DIAS" }

        Row {
            id: statsRow
            width: parent.width
            spacing: Theme.space3
            readonly property real cardWidth: (width - Theme.space3 * 3) / 4
            readonly property bool hasFailed: !!(tab.usage && tab.usage.runs_failed > 0)
            readonly property real cardHeight: hasFailed ? 98 : 82

            // Respostas
            Rectangle {
                width: statsRow.cardWidth
                height: statsRow.cardHeight
                radius: Theme.radiusMd
                color: Theme.wash1
                border.width: 1
                border.color: Theme.line

                Column {
                    x: Theme.space4
                    y: Theme.space3
                    width: parent.width - Theme.space4 * 2
                    spacing: Theme.space1

                    Text {
                        width: parent.width
                        height: Theme.fontDisplay + Theme.space2
                        verticalAlignment: Text.AlignVCenter
                        text: tab.usage ? String((tab.usage.totals && tab.usage.totals.replies) || 0) : "…"
                        color: Theme.white
                        font.family: Theme.display
                        font.pixelSize: Theme.fontDisplay
                        font.weight: Font.DemiBold
                        elide: Text.ElideRight
                    }

                    Text {
                        width: parent.width
                        text: "Respostas"
                        color: Theme.text3
                        font.pixelSize: Theme.fontSmall
                        elide: Text.ElideRight
                    }
                }
            }

            // Execuções
            Rectangle {
                width: statsRow.cardWidth
                height: statsRow.cardHeight
                radius: Theme.radiusMd
                color: Theme.wash1
                border.width: 1
                border.color: Theme.line

                Column {
                    x: Theme.space4
                    y: Theme.space3
                    width: parent.width - Theme.space4 * 2
                    spacing: Theme.space1

                    Text {
                        width: parent.width
                        height: Theme.fontDisplay + Theme.space2
                        verticalAlignment: Text.AlignVCenter
                        text: tab.usage ? String(tab.usage.runs || 0) : "…"
                        color: Theme.white
                        font.family: Theme.display
                        font.pixelSize: Theme.fontDisplay
                        font.weight: Font.DemiBold
                        elide: Text.ElideRight
                    }

                    Text {
                        width: parent.width
                        text: "Execuções"
                        color: Theme.text3
                        font.pixelSize: Theme.fontSmall
                        elide: Text.ElideRight
                    }

                    Text {
                        visible: statsRow.hasFailed
                        width: parent.width
                        text: tab.usage && tab.usage.runs_failed > 0
                              ? (tab.usage.runs_failed === 1 ? "1 falhou" : tab.usage.runs_failed + " falharam")
                              : ""
                        color: Theme.bad
                        font.pixelSize: Theme.fontSmall
                        elide: Text.ElideRight
                    }
                }
            }

            // Tokens
            Rectangle {
                width: statsRow.cardWidth
                height: statsRow.cardHeight
                radius: Theme.radiusMd
                color: Theme.wash1
                border.width: 1
                border.color: Theme.line

                Column {
                    x: Theme.space4
                    y: Theme.space3
                    width: parent.width - Theme.space4 * 2
                    spacing: Theme.space1

                    Text {
                        width: parent.width
                        height: Theme.fontDisplay + Theme.space2
                        verticalAlignment: Text.AlignVCenter
                        text: tab.usage
                              ? U.tokens(((tab.usage.totals && tab.usage.totals.prompt_tokens) || 0)
                                         + ((tab.usage.totals && tab.usage.totals.completion_tokens) || 0))
                              : "…"
                        color: Theme.white
                        font.family: Theme.display
                        font.pixelSize: Theme.fontDisplay
                        font.weight: Font.DemiBold
                        elide: Text.ElideRight
                    }

                    Text {
                        width: parent.width
                        text: "Tokens"
                        color: Theme.text3
                        font.pixelSize: Theme.fontSmall
                        elide: Text.ElideRight
                    }
                }
            }

            // Modelo mais usado
            Rectangle {
                width: statsRow.cardWidth
                height: statsRow.cardHeight
                radius: Theme.radiusMd
                color: Theme.wash1
                border.width: 1
                border.color: Theme.line

                Column {
                    x: Theme.space4
                    y: Theme.space3
                    width: parent.width - Theme.space4 * 2
                    spacing: Theme.space1

                    Text {
                        width: parent.width
                        height: Theme.fontDisplay + Theme.space2
                        verticalAlignment: Text.AlignVCenter
                        text: tab.usage
                              ? ((tab.usage.by_model && tab.usage.by_model.length > 0 && tab.usage.by_model[0].model)
                                 ? tab.usage.by_model[0].model : "—")
                              : "…"
                        color: Theme.white
                        font.family: Theme.display
                        font.pixelSize: Theme.fontDisplay
                        minimumPixelSize: Theme.fontBody
                        fontSizeMode: Text.HorizontalFit
                        font.weight: Font.DemiBold
                        maximumLineCount: 1
                        elide: Text.ElideRight
                    }

                    Text {
                        width: parent.width
                        text: "Modelo mais usado"
                        color: Theme.text3
                        font.pixelSize: Theme.fontSmall
                        elide: Text.ElideRight
                    }
                }
            }
        }

        // 14-day token mini chart and link to the full Usage tab.
        Rectangle {
            width: parent.width
            height: chartCol.implicitHeight + Theme.space3 * 2
            radius: Theme.radiusMd
            color: Theme.wash1
            border.width: 1
            border.color: Theme.line

            Column {
                id: chartCol
                x: Theme.space4
                y: Theme.space3
                width: parent.width - Theme.space4 * 2
                spacing: Theme.space2

                Row {
                    id: barsRow
                    width: parent.width
                    height: 48
                    spacing: Theme.space1
                    readonly property real slotWidth: Math.max(4, (width - Theme.space1 * 13) / 14)

                    Repeater {
                        model: tab.days14
                        delegate: Item {
                            id: daySlot
                            required property var modelData
                            required property int index
                            readonly property bool hasTokens: tab.usage !== null && modelData.tokens > 0
                            width: barsRow.slotWidth
                            height: 48

                            // Empty bar track for days with zero usage; colored bar otherwise.
                            Rectangle {
                                anchors.left: parent.left
                                anchors.right: parent.right
                                anchors.bottom: parent.bottom
                                height: daySlot.hasTokens
                                        ? Math.max(4, Math.round((daySlot.modelData.tokens / tab.maxDayTokens) * parent.height))
                                        : 3
                                radius: Theme.radiusXs
                                color: daySlot.hasTokens
                                       ? Qt.rgba(tab.agentColor.r, tab.agentColor.g, tab.agentColor.b,
                                                 dayMouse.containsMouse ? 0.9 : 0.55)
                                       : (dayMouse.containsMouse ? Theme.wash4 : Theme.wash2)
                                Behavior on height { NumberAnimation { duration: Theme.medium; easing.type: Theme.ease } }
                                Behavior on color { ColorAnimation { duration: Theme.fast } }
                            }

                            MouseArea {
                                id: dayMouse
                                anchors.fill: parent
                                hoverEnabled: true
                                onEntered: tab.hoveredDay = daySlot.index
                                onExited: {
                                    if (tab.hoveredDay === daySlot.index)
                                        tab.hoveredDay = -1;
                                }
                            }
                        }
                    }
                }

                Item {
                    width: parent.width
                    height: 18

                    Text {
                        anchors.verticalCenter: parent.verticalCenter
                        text: {
                            if (tab.hoveredDay >= 0 && tab.hoveredDay < tab.days14.length) {
                                const d = tab.days14[tab.hoveredDay];
                                return d.label + " · " + (d.tokens > 0 ? U.tokens(d.tokens) + " tokens" : "sem uso");
                            }
                            return "Tokens por dia nos últimos 14 dias";
                        }
                        color: Theme.text4
                        font.pixelSize: Theme.fontSmall
                    }

                    Text {
                        anchors {
                            right: parent.right
                            verticalCenter: parent.verticalCenter
                        }
                        text: "Ver uso"
                        color: usageLinkArea.containsMouse ? Theme.white : Theme.text3
                        font.pixelSize: Theme.fontSmall
                        font.underline: usageLinkArea.containsMouse

                        MouseArea {
                            id: usageLinkArea
                            anchors.fill: parent
                            anchors.margins: -Theme.space1
                            hoverEnabled: true
                            cursorShape: Qt.PointingHandCursor
                            onClicked: tab.page.openTab("usage")
                        }
                    }
                }
            }
        }
    }

    // ── Ficha ───────────────────────────────────────────────────
    Column {
        width: parent.width
        spacing: Theme.space2

        Overline { text: "FICHA" }

        Grid {
            id: fichaGrid
            width: parent.width
            columns: 2
            columnSpacing: Theme.space3
            rowSpacing: Theme.space2
            readonly property real cellWidth: (width - Theme.space3) / 2

            Repeater {
                model: tab.fichaRows
                delegate: Rectangle {
                    id: cell
                    required property var modelData
                    width: fichaGrid.cellWidth
                    height: 56
                    radius: Theme.radiusMd
                    color: cellArea.containsMouse ? Theme.hover : Theme.wash1
                    border.width: 1
                    border.color: cellArea.containsMouse ? Theme.lineStrong : Theme.line
                    Behavior on color { ColorAnimation { duration: Theme.fast } }
                    Behavior on border.color { ColorAnimation { duration: Theme.fast } }

                    Icon {
                        id: cellIcon
                        x: Theme.space3
                        anchors.verticalCenter: parent.verticalCenter
                        name: cell.modelData.icon
                        size: 16
                        color: Theme.text3
                    }

                    Column {
                        anchors {
                            left: cellIcon.right
                            leftMargin: Theme.space3
                            right: cellArrow.left
                            rightMargin: Theme.space2
                            verticalCenter: parent.verticalCenter
                        }
                        spacing: 2

                        Text {
                            width: parent.width
                            text: cell.modelData.label
                            color: Theme.text3
                            font.pixelSize: Theme.fontSmall
                            elide: Text.ElideRight
                        }

                        Text {
                            width: parent.width
                            text: cell.modelData.key === "brain" ? tab.brainText
                                : cell.modelData.key === "approvals" ? tab.approvalsText
                                : cell.modelData.key === "project" ? tab.projectText
                                : cell.modelData.key === "routines" ? tab.routinesText
                                : cell.modelData.key === "memories" ? tab.memoriesText
                                : tab.versionText
                            color: Theme.text
                            font.pixelSize: Theme.fontBody
                            font.weight: Font.Medium
                            elide: Text.ElideRight
                        }
                    }

                    Icon {
                        id: cellArrow
                        anchors {
                            right: parent.right
                            rightMargin: Theme.space3
                            verticalCenter: parent.verticalCenter
                        }
                        name: "chevron"
                        size: 14
                        color: Theme.text3
                        opacity: cellArea.containsMouse ? 1 : 0
                        Behavior on opacity { NumberAnimation { duration: Theme.fast } }
                    }

                    MouseArea {
                        id: cellArea
                        anchors.fill: parent
                        hoverEnabled: true
                        cursorShape: Qt.PointingHandCursor
                        onClicked: tab.page.openTab(cell.modelData.target)
                    }
                }
            }
        }
    }

    // ── Últimas execuções ───────────────────────────────────────
    Column {
        width: parent.width
        spacing: Theme.space2

        Item {
            width: parent.width
            height: 20

            Overline {
                anchors.verticalCenter: parent.verticalCenter
                text: "ÚLTIMAS EXECUÇÕES"
            }

            Text {
                anchors {
                    right: parent.right
                    verticalCenter: parent.verticalCenter
                }
                text: "Ver todas"
                color: allRunsArea.containsMouse ? Theme.white : Theme.text3
                font.pixelSize: Theme.fontSmall
                font.underline: allRunsArea.containsMouse

                MouseArea {
                    id: allRunsArea
                    anchors.fill: parent
                    anchors.margins: -Theme.space1
                    hoverEnabled: true
                    cursorShape: Qt.PointingHandCursor
                    onClicked: tab.page.openTab("activity")
                }
            }
        }

        Rectangle {
            width: parent.width
            height: recentRunsCol.implicitHeight + Theme.space2 * 2
            radius: Theme.radiusMd
            color: Theme.wash1
            border.width: 1
            border.color: Theme.line

            Column {
                id: recentRunsCol
                x: Theme.space2
                y: Theme.space2
                width: parent.width - Theme.space2 * 2
                spacing: 0

                Text {
                    visible: tab.recentRuns.length === 0
                    width: parent.width - Theme.space2 * 2
                    leftPadding: Theme.space2
                    topPadding: Theme.space2
                    bottomPadding: Theme.space2
                    text: tab.loadingRuns
                          ? "Carregando execuções…"
                          : "Ainda não fez nenhuma execução. Quando trabalhar sozinho, aparece aqui."
                    color: Theme.text4
                    font.pixelSize: Theme.fontSmall
                    wrapMode: Text.Wrap
                }

                Repeater {
                    model: tab.recentRuns.slice(0, 5)
                    delegate: Rectangle {
                        id: runRow
                        required property var modelData
                        required property int index
                        width: recentRunsCol.width
                        height: 42
                        radius: Theme.radiusSm
                        color: runRowArea.containsMouse ? Theme.hover : "transparent"
                        Behavior on color { ColorAnimation { duration: Theme.fast } }

                        Rectangle {
                            visible: runRow.index > 0 && !runRowArea.containsMouse
                            anchors {
                                top: parent.top
                                left: parent.left
                                right: parent.right
                                leftMargin: Theme.space2
                                rightMargin: Theme.space2
                            }
                            height: 1
                            color: Theme.line
                        }

                        StatusPill {
                            id: runPill
                            x: Theme.space2
                            anchors.verticalCenter: parent.verticalCenter
                            status: runRow.modelData.status
                        }

                        Text {
                            anchors {
                                left: runPill.right
                                leftMargin: Theme.space3
                                right: runAgo.left
                                rightMargin: Theme.space3
                                verticalCenter: parent.verticalCenter
                            }
                            text: runRow.modelData.title || ""
                            color: Theme.text
                            font.pixelSize: Theme.fontBody
                            elide: Text.ElideRight
                        }

                        Text {
                            id: runAgo
                            anchors {
                                right: parent.right
                                rightMargin: Theme.space2
                                verticalCenter: parent.verticalCenter
                            }
                            text: {
                                tab.tick; // re-read every tick
                                return U.ago(runRow.modelData.created_at);
                            }
                            color: Theme.text3
                            font.pixelSize: Theme.fontSmall
                        }

                        MouseArea {
                            id: runRowArea
                            anchors.fill: parent
                            hoverEnabled: true
                            cursorShape: Qt.PointingHandCursor
                            onClicked: tab.page.openRun(runRow.modelData.id)
                        }
                    }
                }
            }
        }
    }

    // ── Atalhos ─────────────────────────────────────────────────
    Column {
        width: parent.width
        spacing: Theme.space2

        Overline { text: "ATALHOS" }

        Flow {
            width: parent.width
            spacing: Theme.space2

            GhostButton {
                small: true
                icon: "plus"
                text: "Nova rotina"
                onClicked: tab.page.newRoutine(null)
            }

            GhostButton {
                small: true
                icon: "bookmark"
                text: "Ensinar algo"
                onClicked: tab.page.openTab("memory")
            }

            GhostButton {
                small: true
                icon: "spark"
                text: "Mudar o cérebro"
                onClicked: tab.page.openTab("settings")
            }
        }
    }
}
