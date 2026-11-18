import QtQuick
import QtQuick.Controls.Basic
import Avora
import "util.js" as U

// Equipe: workspace agents grouped by sector (or as an indented org chart)
// with live status, sector leaders and reporting lines, plus workspace people.
Item {
    id: page
    property var agents: []
    property var sectors: []
    property var tasks: []
    property var waiting: []
    property var members: []
    property bool loading: true
    property string error: ""
    property string view: "sectors" // "sectors" | "org"

    readonly property string wsId: Api.workspace !== "" ? Api.workspace
        : (Api.me && Api.me.workspaces && Api.me.workspaces.length > 0 ? Api.me.workspaces[0].id : "pessoal")
    readonly property string myId: (Api.me && Api.me.person && Api.me.person.id) ? Api.me.person.id : ""

    readonly property var agentMap: {
        const m = {};
        for (const a of agents)
            m[a.id] = a;
        return m;
    }

    readonly property var sectorMap: {
        const m = {};
        for (const s of sectors)
            m[s.id] = s;
        return m;
    }

    readonly property var waitingByAgent: {
        const m = {};
        for (const w of waiting) {
            if (w.agent_id && !m[w.agent_id])
                m[w.agent_id] = w;
        }
        return m;
    }

    readonly property var workingByAgent: {
        const m = {};
        for (const t of tasks) {
            if (t.agent_id && !m[t.agent_id])
                m[t.agent_id] = t;
        }
        return m;
    }

    readonly property var sectorGroups: {
        const groups = [];
        const known = {};
        for (const s of sectors)
            known[s.id] = true;

        for (const s of sectors) {
            const list = agents.filter(a => a.sector === s.id);
            groups.push({
                id: s.id,
                name: s.name || s.id,
                icon: s.icon || "users",
                color: s.color || Theme.text2,
                leadId: s.lead_agent_id || "",
                leadName: s.lead_agent_id ? agentName(s.lead_agent_id) : "",
                sectorObj: s,
                items: list
            });
        }

        const unassigned = agents.filter(a => !a.sector || !known[a.sector]);
        if (unassigned.length > 0) {
            groups.push({
                id: "",
                name: sectors.length > 0 ? "Sem setor" : "Agentes",
                icon: "users",
                color: Theme.text3,
                leadId: "",
                leadName: "",
                sectorObj: null,
                items: unassigned
            });
        }
        return groups;
    }

    readonly property bool hasAnyReporting: agents.some(a => !!a.reports_to && !!agentMap[a.reports_to])

    readonly property var orgRows: {
        const byMgr = {};
        const roots = [];
        for (const a of agents) {
            const mgr = a.reports_to || "";
            if (mgr !== "" && mgr !== a.id && agentMap[mgr]) {
                if (!byMgr[mgr])
                    byMgr[mgr] = [];
                byMgr[mgr].push(a);
            } else {
                roots.push(a);
            }
        }

        const out = [];
        const visited = {};
        function walk(a, depth) {
            if (!a || visited[a.id])
                return;
            visited[a.id] = true;
            const sec = a.sector ? (sectorMap[a.sector] || null) : null;
            const kids = byMgr[a.id] || [];
            out.push({
                agent: a,
                depth: depth,
                managerName: (a.reports_to && agentMap[a.reports_to]) ? agentMap[a.reports_to].name : "",
                sectorName: sec ? (sec.name || "") : "",
                sectorColor: sec && sec.color ? sec.color : Theme.text3,
                isLead: !!(sec && sec.lead_agent_id === a.id),
                directCount: kids.length
            });
            for (const child of kids)
                walk(child, depth + 1);
        }

        for (const r of roots)
            walk(r, 0);
        for (const a of agents) {
            if (!visited[a.id])
                walk(a, 0);
        }
        return out;
    }

    readonly property var activeMembers: members.filter(m => m.status !== "pending")
    readonly property var pendingMembers: members.filter(m => m.status === "pending")

    function agentName(id) {
        if (!id)
            return "";
        const a = agentMap[id];
        return a ? a.name : id;
    }

    function statusInfo(a) {
        if (!a)
            return { kind: "free", dot: "", working: false, text: "Livre" };
        const w = waitingByAgent[a.id];
        if (w || a.status === "waiting") {
            const detail = w ? (w.title || w.summary || "") : "";
            return {
                kind: "waiting",
                dot: "warn",
                working: false,
                text: detail !== "" ? "Esperando · " + detail : "Esperando você"
            };
        }
        const t = workingByAgent[a.id];
        if (t || a.status === "working" || a.working) {
            const detail = t ? (t.title || t.channel || "") : (a.status_channel ? "em " + a.status_channel : "");
            return {
                kind: "working",
                dot: "live",
                working: true,
                text: detail !== "" ? "Trabalhando · " + detail : "Trabalhando agora"
            };
        }
        return { kind: "free", dot: "", working: false, text: "Livre" };
    }

    function hierarchyLine(a, grp) {
        const parts = [];
        const isLead = !!(grp && grp.leadId !== "" && grp.leadId === a.id);
        if (isLead && grp.name)
            parts.push("Lidera " + grp.name);
        else if (grp && grp.leadName !== "" && grp.leadId !== a.reports_to)
            parts.push("Líder: " + grp.leadName);

        if (a.reports_to && agentMap[a.reports_to])
            parts.push("Reporta a " + agentMap[a.reports_to].name);
        return parts.join("  ·  ");
    }

    function subtitleText() {
        if (loading && agents.length === 0 && members.length === 0)
            return "Carregando os agentes e pessoas deste espaço…";
        const agLabel = agents.length === 1 ? "1 agente" : agents.length + " agentes";
        const pCount = activeMembers.length;
        const pLabel = pCount === 1 ? "1 pessoa" : pCount + " pessoas";
        if (pCount > 0)
            return agLabel + " e " + pLabel + " trabalhando juntos neste espaço.";
        return agLabel + " neste espaço — organizados por área e linha de reporte.";
    }

    function roleLabel(r) {
        return r === "owner" ? "Dono" : r === "admin" ? "Administrador" : "Membro";
    }

    function personPhoto(av) {
        if (!av)
            return "";
        if (av.startsWith("avora:file/"))
            return "image://server/v1/files/" + av.slice(11);
        if (av.startsWith("/v1/files/"))
            return "image://server" + av;
        return av;
    }

    function tintFor(seed) {
        const s = String(seed || "?");
        let h = 0;
        for (let i = 0; i < s.length; i++)
            h = (h * 31 + s.charCodeAt(i)) >>> 0;
        return Theme.palette[h % Theme.palette.length];
    }

    function presenceDot(m) {
        if (!m || !m.online || m.presence === "invisible")
            return "";
        if (m.presence === "busy")
            return "bad";
        if (m.presence === "away")
            return "warn";
        return "live";
    }

    function triggerNewAgent() {
        if (typeof win === "undefined" || !win)
            return;
        if (win.newAgent)
            win.newAgent();
        else if (win.openNewAgent)
            win.openNewAgent();
    }

    function triggerOpenAgent(id) {
        if (!id || typeof win === "undefined" || !win)
            return;
        if (win.openAgent)
            win.openAgent(id);
        else if (win.openAgentConv)
            win.openAgentConv(id, "");
    }

    function triggerInvite() {
        if (typeof win !== "undefined" && win && win.invite)
            win.invite();
    }

    function triggerOpenPerson(personId) {
        if (!personId || personId === myId)
            return;
        if (typeof win !== "undefined" && win && win.openPerson)
            win.openPerson(personId);
    }

    function load() {
        const curWs = wsId;
        Api.get("/v1/agents", (err, list) => {
            loading = false;
            if (err) {
                error = err;
                return;
            }
            error = "";
            if (!U.same(agents, list || []))
                agents = list || [];
        });
        Api.get("/v1/sectors", (err, list) => {
            if (!err && !U.same(sectors, list || []))
                sectors = list || [];
        });
        Api.get("/v1/tasks", (err, list) => {
            if (!err && !U.same(tasks, list || []))
                tasks = list || [];
        });
        Api.get("/v1/waiting", (err, list) => {
            if (!err && !U.same(waiting, list || []))
                waiting = list || [];
        });
        if (curWs !== "") {
            Api.get("/v1/workspaces/" + encodeURIComponent(curWs) + "/members", (err, list) => {
                if (!err && !U.same(members, list || []))
                    members = list || [];
            });
        }
    }

    Component.onCompleted: load()
    onVisibleChanged: if (visible) load()

    Connections {
        target: Api
        function onWorkspaceChanged() {
            page.agents = [];
            page.sectors = [];
            page.tasks = [];
            page.waiting = [];
            page.members = [];
            page.loading = true;
            page.load();
        }
        function onLiveEvent(e) {
            if (!page.visible)
                return;
            const t = e.type || "";
            if (t.startsWith("agent.") || t.startsWith("sector.") || t.startsWith("job.") ||
                t.startsWith("mission.") || t.startsWith("approval.") || t.startsWith("message.") ||
                t.startsWith("member.") || t.startsWith("workspace.") || t.startsWith("person."))
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
        onTriggered: page.load()
    }

    Timer {
        interval: Api.live ? 20000 : 5000
        running: page.visible
        repeat: true
        onTriggered: page.load()
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
            readonly property real colWidth: Math.min(scroller.width - 2 * Theme.gutter, 920)
            x: Math.max(Theme.gutter, (scroller.width - colWidth) / 2)
            y: 44
            width: colWidth
            spacing: Theme.space5

            // Header with actions
            Item {
                width: parent.width
                height: Math.max(headBlock.implicitHeight, headActions.implicitHeight)

                PageHead {
                    id: headBlock
                    width: Math.max(240, parent.width - headActions.width - Theme.space3)
                    eyebrow: "Espaço"
                    title: "Equipe"
                    sub: page.subtitleText()
                }

                Row {
                    id: headActions
                    anchors { right: parent.right; bottom: headBlock.bottom }
                    spacing: Theme.space2

                    GhostButton {
                        icon: "folder"
                        text: "Organizar setores"
                        onClicked: sectorsEditor.open(null)
                    }

                    PrimaryButton {
                        icon: "plus"
                        text: "Novo agente"
                        onClicked: page.triggerNewAgent()
                    }
                }
            }

            ErrorBanner {
                width: parent.width
                text: page.error
            }

            // View switcher: Por setor vs Organograma
            Item {
                visible: page.agents.length > 0
                width: parent.width
                height: viewSeg.implicitHeight

                Segmented {
                    id: viewSeg
                    value: page.view
                    options: [
                        { value: "sectors", label: "Por setor", icon: "users" },
                        { value: "org", label: "Organograma", icon: "sidebar" }
                    ]
                    onActivated: v => page.view = v
                }
            }

            // Loading state
            Card {
                visible: page.loading && page.agents.length === 0
                width: parent.width
                height: 88
                padding: Theme.space5

                Text {
                    anchors.verticalCenter: parent.verticalCenter
                    text: "Carregando os agentes do espaço…"
                    color: Theme.text3
                    font.pixelSize: Theme.fontBody
                }
            }

            // Empty agents state
            Card {
                visible: !page.loading && page.agents.length === 0
                width: parent.width
                height: emptyAgentsCol.implicitHeight + 2 * padding
                padding: Theme.space5

                Column {
                    id: emptyAgentsCol
                    width: parent.width
                    spacing: Theme.space3

                    Text {
                        text: "Ainda não há agentes neste espaço"
                        color: Theme.white
                        font.family: Theme.display
                        font.pixelSize: Theme.fontHeading
                        font.weight: Font.DemiBold
                    }

                    Text {
                        width: parent.width
                        wrapMode: Text.WordWrap
                        text: "Crie o primeiro agente para cuidar de tarefas, responder nos canais e organizar o trabalho por setores."
                        color: Theme.text3
                        font.pixelSize: Theme.fontBody
                    }

                    Row {
                        spacing: Theme.space2
                        PrimaryButton {
                            icon: "plus"
                            text: "Novo agente"
                            onClicked: page.triggerNewAgent()
                        }
                    }
                }
            }

            // ── View 1: Agentes por setor ──────────────────────────
            Column {
                id: sectorsView
                visible: page.view === "sectors" && page.agents.length > 0
                width: parent.width
                spacing: Theme.space5

                Repeater {
                    model: page.sectorGroups
                    delegate: Column {
                        id: secGrp
                        required property var modelData
                        width: sectorsView.width
                        spacing: Theme.space3

                        // Sector header bar
                        Item {
                            width: parent.width
                            height: 28

                            Row {
                                anchors { left: parent.left; verticalCenter: parent.verticalCenter }
                                spacing: Theme.space2

                                Icon {
                                    name: secGrp.modelData.icon
                                    size: 15
                                    color: secGrp.modelData.color
                                    anchors.verticalCenter: parent.verticalCenter
                                }

                                Overline {
                                    text: secGrp.modelData.name + "  ·  " + secGrp.modelData.items.length
                                    anchors.verticalCenter: parent.verticalCenter
                                }

                                Rectangle {
                                    visible: secGrp.modelData.leadName !== ""
                                    height: 22
                                    width: leadHdrTxt.implicitWidth + 2 * Theme.space2
                                    radius: height / 2
                                    color: Theme.wash2
                                    border.width: 1
                                    border.color: Theme.line
                                    anchors.verticalCenter: parent.verticalCenter

                                    Text {
                                        id: leadHdrTxt
                                        anchors.centerIn: parent
                                        text: "Líder: " + secGrp.modelData.leadName
                                        color: Theme.text2
                                        font.pixelSize: Theme.fontCaption
                                        font.weight: Font.Medium
                                    }
                                }
                            }

                            RowTool {
                                visible: secGrp.modelData.sectorObj !== null
                                anchors { right: parent.right; verticalCenter: parent.verticalCenter }
                                icon: "pen"
                                tip: "Editar setor"
                                onHit: sectorsEditor.open(secGrp.modelData.sectorObj)
                            }
                        }

                        // Empty sector note
                        Card {
                            visible: secGrp.modelData.items.length === 0
                            width: parent.width
                            height: 64
                            padding: Theme.space4

                            Text {
                                anchors.verticalCenter: parent.verticalCenter
                                text: "Nenhum agente atribuído a este setor ainda."
                                color: Theme.text4
                                font.pixelSize: Theme.fontBody
                            }
                        }

                        // Agent cards grid (2 columns when wide enough)
                        Flow {
                            id: secFlow
                            visible: secGrp.modelData.items.length > 0
                            width: parent.width
                            spacing: Theme.space3
                            readonly property int cols: width >= 620 ? 2 : 1
                            readonly property real cardWidth: cols === 2 ? Math.floor((width - Theme.space3) / 2) : width

                            Repeater {
                                model: secGrp.modelData.items
                                delegate: Card {
                                    id: agCard
                                    required property var modelData

                                    readonly property var st: page.statusInfo(modelData)
                                    readonly property bool isLead: secGrp.modelData.leadId !== "" && secGrp.modelData.leadId === modelData.id
                                    readonly property string metaLine: page.hierarchyLine(modelData, secGrp.modelData)

                                    width: secFlow.cardWidth
                                    height: agCol.implicitHeight + 2 * padding
                                    padding: Theme.space4
                                    radius: Theme.radiusLg
                                    glow: agArea.containsMouse
                                    topColor: agArea.containsMouse ? Theme.cardHover : Theme.card

                                    Column {
                                        id: agCol
                                        width: parent.width
                                        spacing: Theme.space3

                                        Row {
                                            width: parent.width
                                            spacing: Theme.space3

                                            Avatar {
                                                id: agFace
                                                size: 44
                                                name: agCard.modelData.name
                                                photo: U.photo(agCard.modelData)
                                                tint: agCard.modelData.color || Theme.text2
                                                working: agCard.st.working
                                                anchors.verticalCenter: parent.verticalCenter
                                            }

                                            Column {
                                                width: parent.width - agFace.width - Theme.space3
                                                anchors.verticalCenter: parent.verticalCenter
                                                spacing: Theme.space1

                                                Row {
                                                    width: parent.width
                                                    spacing: Theme.space2

                                                    Text {
                                                        width: Math.min(implicitWidth, parent.width - (leadBadge.visible ? leadBadge.width + Theme.space2 : 0))
                                                        text: agCard.modelData.name
                                                        color: Theme.white
                                                        font.pixelSize: Theme.fontLarge
                                                        font.weight: Font.DemiBold
                                                        elide: Text.ElideRight
                                                        anchors.verticalCenter: parent.verticalCenter
                                                    }

                                                    Rectangle {
                                                        id: leadBadge
                                                        visible: agCard.isLead
                                                        height: 20
                                                        width: leadBadgeTxt.implicitWidth + 2 * Theme.space2
                                                        radius: height / 2
                                                        color: Theme.wash4
                                                        border.width: 1
                                                        border.color: Theme.focusLine
                                                        anchors.verticalCenter: parent.verticalCenter

                                                        Text {
                                                            id: leadBadgeTxt
                                                            anchors.centerIn: parent
                                                            text: "Líder"
                                                            color: Theme.white
                                                            font.pixelSize: Theme.fontTiny
                                                            font.weight: Font.DemiBold
                                                        }
                                                    }
                                                }

                                                Text {
                                                    width: parent.width
                                                    text: agCard.modelData.role || "Sem função definida"
                                                    color: Theme.text3
                                                    font.pixelSize: Theme.fontSmall
                                                    elide: Text.ElideRight
                                                }
                                            }
                                        }

                                        // Live status bar (working on what / waiting / free)
                                        Rectangle {
                                            width: parent.width
                                            height: 32
                                            radius: Theme.radiusSm
                                            color: agCard.st.kind === "waiting"
                                                ? Qt.rgba(Theme.warn.r, Theme.warn.g, Theme.warn.b, 0.10)
                                                : Theme.wash1
                                            border.width: 1
                                            border.color: agCard.st.kind === "waiting"
                                                ? Qt.rgba(Theme.warn.r, Theme.warn.g, Theme.warn.b, 0.28)
                                                : Theme.line

                                            Row {
                                                anchors {
                                                    left: parent.left
                                                    right: parent.right
                                                    margins: Theme.space2
                                                    verticalCenter: parent.verticalCenter
                                                }
                                                spacing: Theme.space2

                                                Dot {
                                                    kind: agCard.st.dot
                                                    anchors.verticalCenter: parent.verticalCenter
                                                }

                                                Text {
                                                    width: parent.width - 16
                                                    text: agCard.st.text
                                                    color: agCard.st.kind === "waiting" ? Theme.warn
                                                        : agCard.st.kind === "working" ? Theme.text
                                                        : Theme.text3
                                                    font.pixelSize: Theme.fontSmall
                                                    font.weight: agCard.st.kind !== "free" ? Font.Medium : Font.Normal
                                                    elide: Text.ElideRight
                                                    anchors.verticalCenter: parent.verticalCenter
                                                }
                                            }
                                        }

                                        // Leader / Reports-to metadata line
                                        Row {
                                            visible: agCard.metaLine !== ""
                                            width: parent.width
                                            spacing: Theme.space2

                                            Icon {
                                                name: "reply"
                                                size: 13
                                                color: Theme.text4
                                                anchors.verticalCenter: parent.verticalCenter
                                            }

                                            Text {
                                                width: parent.width - 21
                                                text: agCard.metaLine
                                                color: Theme.text3
                                                font.pixelSize: Theme.fontCaption
                                                elide: Text.ElideRight
                                                anchors.verticalCenter: parent.verticalCenter
                                            }
                                        }
                                    }

                                    MouseArea {
                                        id: agArea
                                        anchors.fill: parent
                                        anchors.margins: -agCard.padding
                                        hoverEnabled: true
                                        cursorShape: Qt.PointingHandCursor
                                        onClicked: page.triggerOpenAgent(agCard.modelData.id)
                                    }
                                }
                            }
                        }
                    }
                }
            }

            // ── View 2: Organograma (indented reporting tree) ──────
            Column {
                id: orgView
                visible: page.view === "org" && page.agents.length > 0
                width: parent.width
                spacing: Theme.space3

                Overline { text: "ORGANOGRAMA  ·  QUEM REPORTA A QUEM" }

                Rectangle {
                    visible: page.agents.length > 1 && !page.hasAnyReporting
                    width: parent.width
                    height: orgHintTxt.implicitHeight + 2 * Theme.space3
                    radius: Theme.radiusMd
                    color: Theme.wash1
                    border.width: 1
                    border.color: Theme.line

                    Text {
                        id: orgHintTxt
                        anchors {
                            left: parent.left
                            right: parent.right
                            margins: Theme.space3
                            verticalCenter: parent.verticalCenter
                        }
                        wrapMode: Text.WordWrap
                        text: "Nenhum vínculo de «reporta a» foi definido ainda. Configure na ficha de cada agente a quem ele responde para formar os níveis da árvore."
                        color: Theme.text3
                        font.pixelSize: Theme.fontSmall
                    }
                }

                Card {
                    width: parent.width
                    height: orgCol.implicitHeight + 2 * padding
                    padding: Theme.space3

                    Column {
                        id: orgCol
                        width: parent.width
                        spacing: Theme.space1

                        Repeater {
                            model: page.orgRows
                            delegate: Item {
                                id: orgItem
                                required property var modelData
                                required property int index

                                readonly property var ag: modelData.agent
                                readonly property var st: page.statusInfo(ag)
                                readonly property real indentPx: Math.min(modelData.depth, 6) * Theme.space6

                                width: orgCol.width
                                height: 58

                                // Indentation tree connector (L-line) for non-root nodes
                                Item {
                                    visible: orgItem.modelData.depth > 0
                                    x: orgItem.indentPx - Theme.space5
                                    width: Theme.space4
                                    height: parent.height

                                    Rectangle {
                                        x: 0
                                        y: 0
                                        width: 1
                                        height: parent.height / 2 + 1
                                        color: Theme.lineStrong
                                    }
                                    Rectangle {
                                        x: 0
                                        y: parent.height / 2
                                        width: parent.width
                                        height: 1
                                        color: Theme.lineStrong
                                    }
                                }

                                Rectangle {
                                    id: orgCardRow
                                    x: orgItem.indentPx
                                    width: parent.width - orgItem.indentPx
                                    height: 54
                                    anchors.verticalCenter: parent.verticalCenter
                                    radius: Theme.radiusMd
                                    color: orgRowArea.containsMouse ? Theme.hover : Theme.wash1
                                    border.width: 1
                                    border.color: orgRowArea.containsMouse ? Theme.lineStrong : Theme.line
                                    Behavior on color { ColorAnimation { duration: Theme.fast } }

                                    Avatar {
                                        id: orgAv
                                        x: Theme.space3
                                        size: 36
                                        name: orgItem.ag.name
                                        photo: U.photo(orgItem.ag)
                                        tint: orgItem.ag.color || Theme.text2
                                        working: orgItem.st.working
                                        anchors.verticalCenter: parent.verticalCenter
                                    }

                                    Column {
                                        anchors {
                                            left: orgAv.right
                                            leftMargin: Theme.space3
                                            right: orgStatusRow.left
                                            rightMargin: Theme.space3
                                            verticalCenter: parent.verticalCenter
                                        }
                                        spacing: Theme.space1

                                        Row {
                                            width: parent.width
                                            spacing: Theme.space2

                                            Text {
                                                text: orgItem.ag.name
                                                color: Theme.white
                                                font.pixelSize: Theme.fontBody
                                                font.weight: Font.DemiBold
                                                elide: Text.ElideRight
                                                anchors.verticalCenter: parent.verticalCenter
                                            }

                                            Rectangle {
                                                visible: orgItem.modelData.isLead
                                                height: 18
                                                width: orgLeadTxt.implicitWidth + 2 * Theme.space2
                                                radius: height / 2
                                                color: Theme.wash4
                                                anchors.verticalCenter: parent.verticalCenter

                                                Text {
                                                    id: orgLeadTxt
                                                    anchors.centerIn: parent
                                                    text: "Líder"
                                                    color: Theme.white
                                                    font.pixelSize: Theme.fontTiny
                                                    font.weight: Font.DemiBold
                                                }
                                            }

                                            Rectangle {
                                                visible: orgItem.modelData.sectorName !== ""
                                                height: 18
                                                width: orgSecRow.implicitWidth + 2 * Theme.space2
                                                radius: height / 2
                                                color: Theme.wash2
                                                border.width: 1
                                                border.color: Theme.line
                                                anchors.verticalCenter: parent.verticalCenter

                                                Row {
                                                    id: orgSecRow
                                                    anchors.centerIn: parent
                                                    spacing: Theme.space1

                                                    Rectangle {
                                                        width: 6
                                                        height: 6
                                                        radius: width / 2
                                                        color: orgItem.modelData.sectorColor
                                                        anchors.verticalCenter: parent.verticalCenter
                                                    }
                                                    Text {
                                                        text: orgItem.modelData.sectorName
                                                        color: Theme.text2
                                                        font.pixelSize: Theme.fontTiny
                                                        anchors.verticalCenter: parent.verticalCenter
                                                    }
                                                }
                                            }
                                        }

                                        Text {
                                            width: parent.width
                                            elide: Text.ElideRight
                                            text: {
                                                const p = [orgItem.ag.role || "Sem função definida"];
                                                if (orgItem.modelData.managerName !== "")
                                                    p.push("Reporta a " + orgItem.modelData.managerName);
                                                if (orgItem.modelData.directCount > 0) {
                                                    const n = orgItem.modelData.directCount;
                                                    p.push(n === 1 ? "1 liderado direto" : n + " liderados diretos");
                                                }
                                                return p.join("  ·  ");
                                            }
                                            color: Theme.text3
                                            font.pixelSize: Theme.fontSmall
                                        }
                                    }

                                    Row {
                                        id: orgStatusRow
                                        anchors {
                                            right: parent.right
                                            rightMargin: Theme.space3
                                            verticalCenter: parent.verticalCenter
                                        }
                                        spacing: Theme.space2

                                        Dot {
                                            kind: orgItem.st.dot
                                            anchors.verticalCenter: parent.verticalCenter
                                        }

                                        Text {
                                            width: Math.min(implicitWidth, 180)
                                            text: orgItem.st.text
                                            color: orgItem.st.kind === "waiting" ? Theme.warn
                                                : orgItem.st.kind === "working" ? Theme.ok
                                                : Theme.text4
                                            font.pixelSize: Theme.fontSmall
                                            elide: Text.ElideRight
                                            anchors.verticalCenter: parent.verticalCenter
                                        }
                                    }

                                    MouseArea {
                                        id: orgRowArea
                                        anchors.fill: parent
                                        hoverEnabled: true
                                        cursorShape: Qt.PointingHandCursor
                                        onClicked: page.triggerOpenAgent(orgItem.ag.id)
                                    }
                                }
                            }
                        }
                    }
                }
            }

            // ── Section: Pessoas do espaço ─────────────────────────
            Column {
                id: peopleSec
                width: parent.width
                spacing: Theme.space3

                Item {
                    width: parent.width
                    height: 30

                    Overline {
                        anchors.verticalCenter: parent.verticalCenter
                        text: "PESSOAS" + (page.members.length > 0 ? "  ·  " + page.activeMembers.length : "")
                    }

                    GhostButton {
                        anchors { right: parent.right; verticalCenter: parent.verticalCenter }
                        small: true
                        icon: "plus"
                        text: "Convidar"
                        onClicked: page.triggerInvite()
                    }
                }

                Card {
                    visible: page.members.length === 0
                    width: parent.width
                    height: emptyPeopleTxt.implicitHeight + 2 * padding
                    padding: Theme.space5

                    Text {
                        id: emptyPeopleTxt
                        width: parent.width
                        wrapMode: Text.WordWrap
                        text: "Nenhuma pessoa listada neste espaço ainda. Use Convidar para gerar um link e chamar colegas para trabalhar com os agentes."
                        color: Theme.text3
                        font.pixelSize: Theme.fontBody
                    }
                }

                Card {
                    visible: page.members.length > 0
                    width: parent.width
                    height: peopleCol.implicitHeight + 2 * padding
                    padding: Theme.space2

                    Column {
                        id: peopleCol
                        width: parent.width

                        Repeater {
                            model: page.members
                            delegate: Rectangle {
                                id: pRow
                                required property var modelData
                                required property int index

                                readonly property bool isMe: page.myId !== "" && modelData.person_id === page.myId
                                readonly property bool isPending: modelData.status === "pending"
                                readonly property bool canChat: !isMe && !isPending && !!modelData.person_id

                                width: peopleCol.width
                                height: 60
                                radius: Theme.radiusMd
                                color: canChat && pArea.containsMouse ? Theme.hover : "transparent"
                                opacity: isPending ? 0.65 : 1
                                Behavior on color { ColorAnimation { duration: Theme.fast } }

                                Rectangle {
                                    visible: pRow.index > 0 && !(pRow.canChat && pArea.containsMouse)
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

                                Item {
                                    id: pAvBox
                                    x: Theme.space3
                                    width: 38
                                    height: 38
                                    anchors.verticalCenter: parent.verticalCenter

                                    Avatar {
                                        anchors.fill: parent
                                        size: 38
                                        name: pRow.modelData.name || pRow.modelData.email || "?"
                                        photo: page.personPhoto(pRow.modelData.avatar)
                                        tint: page.tintFor(pRow.modelData.person_id || pRow.modelData.email)
                                    }

                                    Rectangle {
                                        visible: !pRow.isPending
                                        width: 11
                                        height: 11
                                        radius: height / 2
                                        color: Theme.card
                                        anchors { right: parent.right; bottom: parent.bottom }

                                        Dot {
                                            anchors.centerIn: parent
                                            kind: page.presenceDot(pRow.modelData)
                                        }
                                    }
                                }

                                Column {
                                    anchors {
                                        left: pAvBox.right
                                        leftMargin: Theme.space3
                                        right: pRight.left
                                        rightMargin: Theme.space3
                                        verticalCenter: parent.verticalCenter
                                    }
                                    spacing: Theme.space1

                                    Row {
                                        width: parent.width
                                        spacing: Theme.space2

                                        Text {
                                            text: pRow.modelData.name || pRow.modelData.email
                                            color: Theme.white
                                            font.pixelSize: Theme.fontLarge
                                            font.weight: Font.DemiBold
                                            elide: Text.ElideRight
                                            anchors.verticalCenter: parent.verticalCenter
                                        }

                                        Text {
                                            visible: pRow.isMe
                                            text: "você"
                                            color: Theme.text4
                                            font.pixelSize: Theme.fontCaption
                                            font.weight: Font.DemiBold
                                            anchors.verticalCenter: parent.verticalCenter
                                        }

                                        Text {
                                            visible: pRow.isPending
                                            text: "aguardando aprovação"
                                            color: Theme.warn
                                            font.pixelSize: Theme.fontCaption
                                            font.weight: Font.Medium
                                            anchors.verticalCenter: parent.verticalCenter
                                        }
                                    }

                                    Text {
                                        width: parent.width
                                        elide: Text.ElideRight
                                        text: {
                                            const bits = [];
                                            if (pRow.modelData.status_text)
                                                bits.push(pRow.modelData.status_text);
                                            if (pRow.modelData.email)
                                                bits.push(pRow.modelData.email);
                                            return bits.join("  ·  ");
                                        }
                                        color: Theme.text3
                                        font.pixelSize: Theme.fontSmall
                                    }
                                }

                                Rectangle {
                                    id: pRight
                                    anchors {
                                        right: parent.right
                                        rightMargin: Theme.space3
                                        verticalCenter: parent.verticalCenter
                                    }
                                    height: 26
                                    width: pRoleTxt.implicitWidth + 2 * Theme.space3
                                    radius: height / 2
                                    color: pRow.modelData.role === "owner" ? Theme.wash3 : Theme.wash1
                                    border.width: 1
                                    border.color: pRow.modelData.role === "owner" ? Theme.lineStrong : Theme.line

                                    Text {
                                        id: pRoleTxt
                                        anchors.centerIn: parent
                                        text: page.roleLabel(pRow.modelData.role)
                                        color: pRow.modelData.role === "owner" ? Theme.white : Theme.text2
                                        font.pixelSize: Theme.fontSmall
                                        font.weight: Font.Medium
                                    }
                                }

                                MouseArea {
                                    id: pArea
                                    anchors.fill: parent
                                    enabled: pRow.canChat
                                    hoverEnabled: true
                                    cursorShape: pRow.canChat ? Qt.PointingHandCursor : Qt.ArrowCursor
                                    onClicked: page.triggerOpenPerson(pRow.modelData.person_id)
                                }
                            }
                        }
                    }
                }
            }
        }
    }

    SectorsEditor {
        id: sectorsEditor
        anchors.fill: parent
        onChanged: page.load()
    }
}
