import QtQuick
import QtQuick.Controls.Basic
import Avora
import "util.js" as U

// Single-scroll right column for an agent conversation:
// Quem é · Computador · Rotinas · Como pensa · Tarefas recentes · Docs desta conversa.
Item {
    id: side
    property var agent: null
    property var agents: []
    property var projects: []
    property var sectors: []
    property string convId: ""
    property var citedDocIds: []
    property bool working: false
    property string workStep: ""
    property int pendingCount: 0
    property int runsRev: 0
    property string liveBrowserSite: ""

    signal changeBrainRequested
    signal openBrowserRequested(string agentId, string agentName)
    signal openRunRequested(string runId, string agentId)
    signal openDocRequested(string docId)
    signal newRoutineRequested
    signal editRoutineRequested(var routine)

    readonly property string agentId: agent ? agent.id : ""
    readonly property color tint: agent && agent.color ? agent.color : Theme.text2
    readonly property var sectorObj: agent && agent.sector
        ? (sectors || []).find(s => s.id === agent.sector) || null
        : null
    readonly property var managerObj: agent && agent.reports_to
        ? (agents || []).find(a => a.id === agent.reports_to) || null
        : null

    // ── Nodes & live browser ────────────────────────────────────
    property var nodes: []
    property bool loadingNodes: false
    property var browserViews: []

    readonly property var activeNodes: (nodes || []).filter(n => !n.revoked_at)
    readonly property string homeNodeId: agent && agent.home_node_id ? agent.home_node_id : ""
    readonly property var pinnedNode: homeNodeId !== ""
        ? (activeNodes.find(n => n.id === homeNodeId) || null)
        : null
    readonly property var autoNode: homeNodeId === ""
        ? (activeNodes.find(n => n.online) || (activeNodes.length > 0 ? activeNodes[0] : null))
        : null
    readonly property string folderPath: agent && agent.home_dir
        ? agent.home_dir
        : ("agents/" + (agentId !== "" ? agentId : "agente"))

    readonly property var browserView: {
        const list = side.browserViews || [];
        return list.find(v => v.active)
            || list.find(v => !!v.shot_at)
            || list.find(v => v.url && v.url !== "about:blank")
            || (list.length > 0 ? list[0] : null);
    }
    readonly property bool browserActive: side.liveBrowserSite !== "" || !!(browserView && browserView.active)
    readonly property bool browserOwner: !!(browserView && browserView.owner)
    readonly property string browserHost: {
        if (side.liveBrowserSite !== "")
            return side.liveBrowserSite;
        const u = browserView && browserView.url ? String(browserView.url) : "";
        if (u === "" || u === "about:blank")
            return "";
        return u.replace(/^https?:\/\/(www\.)?/, "").split(/[/?#]/)[0];
    }
    readonly property string browserSubline: {
        if (browserOwner)
            return "Você está no controle";
        const log = browserView && Array.isArray(browserView.log) ? browserView.log : [];
        if (log.length > 0 && log[log.length - 1].text)
            return log[log.length - 1].text;
        if (browserView && browserView.title)
            return browserView.title;
        return browserHost;
    }
    readonly property string shotSource: {
        const v = side.browserView;
        if (!v || !v.shot_at || !v.node_id || side.agentId === "")
            return "";
        return "image://server/v1/browser/" + encodeURIComponent(v.node_id)
            + "/screen?agent=" + encodeURIComponent(side.agentId)
            + "&t=" + encodeURIComponent(String(v.shot_at));
    }

    function loadNodes() {
        if (!Api.signedIn)
            return;
        loadingNodes = true;
        Api.get("/v1/nodes", (err, list) => {
            side.loadingNodes = false;
            if (!err && Array.isArray(list) && !U.same(side.nodes, list))
                side.nodes = list;
        });
    }

    function loadBrowser() {
        if (!Api.signedIn || agentId === "") {
            browserViews = [];
            return;
        }
        const asked = agentId;
        Api.get("/v1/browser?agent=" + encodeURIComponent(asked), (err, list) => {
            if (asked !== side.agentId)
                return;
            if (!err && Array.isArray(list) && !U.same(side.browserViews, list))
                side.browserViews = list;
        });
    }

    // ── Routines ────────────────────────────────────────────────
    property var routines: []
    property bool loadingRoutines: false

    readonly property var sortedRoutines: {
        const list = (side.routines || []).slice();
        list.sort((a, b) => {
            if (!!a.enabled !== !!b.enabled)
                return a.enabled ? -1 : 1;
            const na = a.next_run || "";
            const nb = b.next_run || "";
            if (na !== "" && nb !== "")
                return na.localeCompare(nb);
            if (na !== "" || nb !== "")
                return na !== "" ? -1 : 1;
            return String(a.title || "").localeCompare(String(b.title || ""), "pt-BR");
        });
        return list;
    }

    function loadRoutines() {
        if (!Api.signedIn || agentId === "") {
            routines = [];
            return;
        }
        const asked = agentId;
        loadingRoutines = true;
        Api.get("/v1/routines?agent=" + encodeURIComponent(asked), (err, list) => {
            if (asked !== side.agentId)
                return;
            side.loadingRoutines = false;
            if (!err && Array.isArray(list) && !U.same(side.routines, list))
                side.routines = list;
        });
    }

    function toggleRoutine(r) {
        if (!r || !r.id)
            return;
        const nextEnabled = !r.enabled;
        side.routines = side.routines.map(x => x.id === r.id ? Object.assign({}, x, { enabled: nextEnabled }) : x);
        const body = {
            agent_id: r.agent_id,
            title: r.title,
            prompt: r.prompt,
            schedule: r.schedule || {},
            deliver: r.deliver || "time",
            responsibility_id: r.responsibility_id || "",
            enabled: nextEnabled
        };
        if (r.trigger)
            body.trigger = r.trigger;
        if (r.skill !== undefined)
            body.skill = r.skill;
        Api.put("/v1/routines/" + encodeURIComponent(r.id), body, () => side.loadRoutines());
    }

    function routineStatusColor(r) {
        if (!r || !r.enabled)
            return Theme.text4;
        if (r.last_status === "ok")
            return Theme.ok;
        if (r.last_status === "failed")
            return Theme.bad;
        if (r.last_status === "running")
            return side.tint;
        return Theme.text3;
    }

    // ── Brains ──────────────────────────────────────────────────
    property var brains: []

    function loadBrains() {
        if (!Api.signedIn)
            return;
        Api.get("/v1/brains", (err, list) => {
            if (!err && Array.isArray(list) && !U.same(side.brains, list))
                side.brains = list;
        });
    }

    readonly property var brainCfg: agent && agent.brain ? agent.brain : ({})
    readonly property var currentBrain: brainCfg.provider
        ? ((brains || []).find(b => b.provider === brainCfg.provider) || null)
        : null
    readonly property string brainHeadline: {
        const prov = brainCfg.provider || "";
        if (prov === "")
            return "Automático";
        const b = side.currentBrain;
        const m = brainCfg.model || "";
        let label = m !== ""
            ? (b && b.model_names && b.model_names[m] ? b.model_names[m] : m)
            : ("Padrão · " + (b ? b.name : prov));
        if (brainCfg.effort) {
            const eff = ({ low: "baixo", medium: "médio", high: "alto", max: "máximo" })[brainCfg.effort] || brainCfg.effort;
            label += "  ·  esforço " + eff;
        }
        return label;
    }
    readonly property string brainAccountLine: {
        const prov = brainCfg.provider || "";
        if (prov === "")
            return "Conta automática (escolhe a melhor pronta)";
        const b = side.currentBrain;
        const provName = b ? b.name : prov;
        const accs = b && Array.isArray(b.accounts) ? b.accounts : [];
        if (brainCfg.account) {
            const found = accs.find(a => a.id === brainCfg.account);
            const accLabel = found ? found.name : brainCfg.account;
            return provName + "  ·  " + accLabel + (brainCfg.rotate === false ? " (fixa)" : "");
        }
        if (accs.length > 0)
            return provName + "  ·  qualquer conta pronta";
        return provName;
    }

    // ── Recent tasks (runs) ─────────────────────────────────────
    property var recentRuns: []
    property bool loadingRuns: false
    property real tick: Date.now()

    readonly property bool hasActiveRun: (recentRuns || []).some(r => !U.terminal(r.status || "running"))

    function loadRuns() {
        if (!Api.signedIn || agentId === "") {
            recentRuns = [];
            return;
        }
        const asked = agentId;
        loadingRuns = true;
        Api.get("/v1/runs?agent=" + encodeURIComponent(asked), (err, list) => {
            if (asked !== side.agentId)
                return;
            side.loadingRuns = false;
            if (!err && Array.isArray(list) && !U.same(side.recentRuns, list))
                side.recentRuns = list;
        });
    }

    // ── Conversation docs ───────────────────────────────────────
    property var serverConvDocs: []
    property bool loadingDocs: false

    function loadConvDocs() {
        if (!Api.signedIn || convId === "") {
            serverConvDocs = [];
            return;
        }
        const asked = convId;
        loadingDocs = true;
        Api.get("/v1/docs?limit=200&conversation=" + encodeURIComponent(asked), (err, list) => {
            if (asked !== side.convId)
                return;
            side.loadingDocs = false;
            if (!err && Array.isArray(list))
                side.serverConvDocs = list;
        });
    }

    readonly property var convDocsList: {
        DocBus.rev;
        const out = [];
        const seen = {};
        const cid = side.convId;
        const cited = side.citedDocIds || [];
        const addIfMatch = d => {
            if (!d || !d.id || seen[d.id])
                return;
            if ((cid !== "" && d.conversation_id === cid) || cited.indexOf(d.id) >= 0) {
                seen[d.id] = true;
                out.push(d);
            }
        };
        for (const d of side.serverConvDocs)
            addIfMatch(d);
        for (const d of DocBus.docs)
            addIfMatch(d);
        return out;
    }

    // ── Float helpers ───────────────────────────────────────────
    function openAgentFloat(tabName) {
        if (!agent || !agent.id)
            return;
        if (typeof win !== "undefined" && win && typeof win.openFloat === "function") {
            win.openFloat("AgentPage.qml", {
                agentId: agent.id,
                agents: side.agents,
                projects: side.projects,
                shown: true,
                tab: tabName || "overview"
            });
        }
    }

    function openBrowser() {
        if (!agent || !agent.id)
            return;
        side.openBrowserRequested(agent.id, agent.name || "");
    }

    function openRun(runId) {
        if (!runId)
            return;
        if (typeof win !== "undefined" && win && typeof win.openFloat === "function")
            win.openFloat("RunPane.qml", { runId: runId, agents: side.agents });
        else
            side.openRunRequested(runId, side.agentId);
    }

    function openDoc(docId) {
        side.openDocRequested(docId || "");
    }

    // ── Lifecycle & refreshes ───────────────────────────────────
    function reloadAll() {
        if (!visible || !Api.signedIn)
            return;
        loadNodes();
        loadBrains();
        if (agentId !== "") {
            loadBrowser();
            loadRoutines();
            loadRuns();
        }
        loadConvDocs();
    }

    Component.onCompleted: reloadAll()

    onVisibleChanged: {
        if (visible)
            reloadAll();
    }

    onAgentIdChanged: {
        browserViews = [];
        routines = [];
        recentRuns = [];
        if (visible && Api.signedIn && agentId !== "") {
            loadBrowser();
            loadRoutines();
            loadRuns();
        }
    }

    onConvIdChanged: {
        serverConvDocs = [];
        if (visible && Api.signedIn)
            loadConvDocs();
    }

    onRunsRevChanged: {
        if (visible && Api.signedIn && agentId !== "")
            loadRuns();
    }

    onLiveBrowserSiteChanged: {
        if (visible && Api.signedIn && agentId !== "")
            loadBrowser();
    }

    Connections {
        target: DocBus
        function onChanged() {
            if (side.visible && Api.signedIn)
                side.loadConvDocs();
        }
    }

    Connections {
        target: Api
        function onSignedInChanged() {
            if (Api.signedIn && side.visible)
                side.reloadAll();
        }
        function onLiveEvent(e) {
            if (!side.visible || !e || !e.type)
                return;
            const t = e.type;
            if (t === "node.paired" || t === "node.revoked" || t === "node.online")
                side.loadNodes();
            else if (t.startsWith("routine."))
                side.loadRoutines();
            else if (t.startsWith("job."))
                side.loadRuns();
            else if (t === "brain.switched" || t.startsWith("connection.") || t === "agent.saved")
                side.loadBrains();
            else if (t === "agent.step" && e.payload && String(e.payload.tool || "").indexOf("navegador") >= 0)
                side.loadBrowser();
        }
    }

    Timer {
        interval: side.browserActive || side.working ? 3000 : 8000
        repeat: true
        running: side.visible && Api.signedIn && side.agentId !== ""
        onTriggered: {
            side.tick = Date.now();
            side.loadBrowser();
            if (side.hasActiveRun)
                side.loadRuns();
        }
    }

    Timer {
        interval: 20000
        repeat: true
        running: side.visible && Api.signedIn
        onTriggered: side.loadNodes()
    }

    // ── Single-scroll body ──────────────────────────────────────
    Flickable {
        id: flick
        anchors.fill: parent
        contentHeight: bodyCol.implicitHeight + 2 * Theme.space5
        clip: true
        boundsBehavior: Flickable.StopAtBounds
        ScrollBar.vertical: SlimScrollBar {}
        SmoothWheel {}

        Column {
            id: bodyCol
            x: Theme.space3
            y: Theme.space3
            width: flick.width - 2 * Theme.space3
            spacing: Theme.space5

            // ── 1. QUEM É ───────────────────────────────────────
            Column {
                width: parent.width
                spacing: Theme.space2

                Item {
                    width: parent.width
                    height: 18

                    Overline {
                        anchors.verticalCenter: parent.verticalCenter
                        text: "QUEM É"
                    }

                    Text {
                        anchors { right: parent.right; verticalCenter: parent.verticalCenter }
                        text: "Página completa ›"
                        color: fullPageArea.containsMouse ? Theme.white : Theme.text3
                        font.pixelSize: Theme.fontSmall
                        MouseArea {
                            id: fullPageArea
                            anchors.fill: parent
                            anchors.margins: -Theme.space1
                            hoverEnabled: true
                            cursorShape: Qt.PointingHandCursor
                            onClicked: side.openAgentFloat("overview")
                        }
                    }
                }

                Rectangle {
                    width: parent.width
                    height: whoCol.implicitHeight + 2 * Theme.space3
                    radius: Theme.radiusMd
                    color: Theme.wash1
                    border.width: 1
                    border.color: Theme.line

                    Column {
                        id: whoCol
                        x: Theme.space3
                        y: Theme.space3
                        width: parent.width - 2 * Theme.space3
                        spacing: Theme.space3

                        Item {
                            width: parent.width
                            height: Math.max(46, whoTextCol.implicitHeight)

                            Avatar {
                                id: whoFace
                                anchors.verticalCenter: parent.verticalCenter
                                size: 46
                                name: side.agent ? side.agent.name : ""
                                photo: U.photo(side.agent)
                                tint: side.tint
                                working: side.working || !!(side.agent && side.agent.working)
                            }

                            Column {
                                id: whoTextCol
                                anchors {
                                    left: whoFace.right
                                    leftMargin: Theme.space3
                                    right: parent.right
                                    verticalCenter: parent.verticalCenter
                                }
                                spacing: 2

                                Text {
                                    width: parent.width
                                    text: side.agent ? side.agent.name : ""
                                    color: Theme.white
                                    font.pixelSize: Theme.fontTitle
                                    font.weight: Font.DemiBold
                                    elide: Text.ElideRight
                                }

                                Text {
                                    width: parent.width
                                    text: side.agent && side.agent.role ? side.agent.role : "Sem função definida"
                                    color: side.agent && side.agent.role ? Theme.text2 : Theme.text4
                                    font.pixelSize: Theme.fontBody
                                    wrapMode: Text.Wrap
                                    maximumLineCount: 2
                                    elide: Text.ElideRight
                                }
                            }

                            MouseArea {
                                anchors.fill: parent
                                cursorShape: Qt.PointingHandCursor
                                onClicked: side.openAgentFloat("overview")
                            }
                        }

                        Rectangle {
                            width: parent.width
                            height: 1
                            color: Theme.line
                        }

                        Column {
                            width: parent.width
                            spacing: Theme.space2

                            // Setor
                            Item {
                                width: parent.width
                                height: 20

                                Text {
                                    id: secKey
                                    anchors.verticalCenter: parent.verticalCenter
                                    text: "Setor"
                                    color: Theme.text4
                                    font.pixelSize: Theme.fontSmall
                                }

                                Row {
                                    anchors { right: parent.right; verticalCenter: parent.verticalCenter }
                                    spacing: 6

                                    Rectangle {
                                        visible: side.sectorObj !== null
                                        anchors.verticalCenter: parent.verticalCenter
                                        width: 8
                                        height: 8
                                        radius: Theme.radiusXs
                                        color: side.sectorObj && side.sectorObj.color ? side.sectorObj.color : Theme.text3
                                    }

                                    Text {
                                        anchors.verticalCenter: parent.verticalCenter
                                        text: side.sectorObj ? side.sectorObj.name : "Sem setor"
                                        color: side.sectorObj ? Theme.text2 : Theme.text4
                                        font.pixelSize: Theme.fontSmall
                                        font.weight: side.sectorObj ? Font.Medium : Font.Normal
                                        elide: Text.ElideRight
                                        width: Math.min(implicitWidth, whoCol.width - secKey.implicitWidth - 24)
                                    }
                                }
                            }

                            // Reporta a
                            Item {
                                width: parent.width
                                height: 22

                                Text {
                                    id: repKey
                                    anchors.verticalCenter: parent.verticalCenter
                                    text: "Reporta a"
                                    color: Theme.text4
                                    font.pixelSize: Theme.fontSmall
                                }

                                Row {
                                    id: repVal
                                    anchors { right: parent.right; verticalCenter: parent.verticalCenter }
                                    spacing: 6

                                    Avatar {
                                        visible: side.managerObj !== null
                                        anchors.verticalCenter: parent.verticalCenter
                                        size: 18
                                        name: side.managerObj ? side.managerObj.name : ""
                                        photo: U.photo(side.managerObj)
                                        tint: side.managerObj && side.managerObj.color ? side.managerObj.color : Theme.text2
                                    }

                                    Text {
                                        anchors.verticalCenter: parent.verticalCenter
                                        text: side.managerObj ? side.managerObj.name : "Você"
                                        color: side.managerObj && repArea.containsMouse ? Theme.white : Theme.text2
                                        font.pixelSize: Theme.fontSmall
                                        font.weight: Font.Medium
                                        font.underline: side.managerObj !== null && repArea.containsMouse
                                        elide: Text.ElideRight
                                        width: Math.min(implicitWidth, whoCol.width - repKey.implicitWidth - 34)
                                    }
                                }

                                MouseArea {
                                    id: repArea
                                    enabled: side.managerObj !== null
                                    anchors.fill: repVal
                                    anchors.margins: -Theme.space1
                                    hoverEnabled: true
                                    cursorShape: side.managerObj !== null ? Qt.PointingHandCursor : Qt.ArrowCursor
                                    onClicked: {
                                        if (side.managerObj && typeof win !== "undefined" && win && typeof win.openAgent === "function")
                                            win.openAgent(side.managerObj.id);
                                    }
                                }
                            }
                        }
                    }
                }
            }

            // ── 2. COMPUTADOR ───────────────────────────────────
            Column {
                width: parent.width
                spacing: Theme.space2

                Overline { text: "COMPUTADOR" }

                Rectangle {
                    width: parent.width
                    height: compCol.implicitHeight + 2 * Theme.space3
                    radius: Theme.radiusMd
                    color: Theme.wash1
                    border.width: 1
                    border.color: Theme.line

                    Column {
                        id: compCol
                        x: Theme.space3
                        y: Theme.space3
                        width: parent.width - 2 * Theme.space3
                        spacing: Theme.space3

                        // Home computer status
                        Item {
                            width: parent.width
                            height: 22

                            Row {
                                anchors { left: parent.left; right: nodeStateRow.left; rightMargin: Theme.space2; verticalCenter: parent.verticalCenter }
                                spacing: Theme.space2

                                Icon {
                                    anchors.verticalCenter: parent.verticalCenter
                                    name: "monitor"
                                    size: 15
                                    color: Theme.text3
                                }

                                Text {
                                    anchors.verticalCenter: parent.verticalCenter
                                    text: side.homeNodeId !== ""
                                          ? (side.pinnedNode ? side.pinnedNode.name : side.homeNodeId)
                                          : side.autoNode
                                            ? ("Automático · " + side.autoNode.name)
                                            : "Este computador"
                                    color: Theme.text
                                    font.pixelSize: Theme.fontBody
                                    font.weight: Font.Medium
                                    elide: Text.ElideRight
                                    width: Math.min(implicitWidth, parent.width - 24)
                                }
                            }

                            Row {
                                id: nodeStateRow
                                anchors { right: parent.right; verticalCenter: parent.verticalCenter }
                                spacing: 6

                                readonly property bool isOnline: side.homeNodeId !== ""
                                    ? !!(side.pinnedNode && side.pinnedNode.online)
                                    : (side.autoNode ? !!side.autoNode.online : true)

                                Dot {
                                    anchors.verticalCenter: parent.verticalCenter
                                    kind: nodeStateRow.isOnline ? "live" : (side.homeNodeId !== "" ? "bad" : "")
                                }

                                Text {
                                    anchors.verticalCenter: parent.verticalCenter
                                    text: nodeStateRow.isOnline ? "online" : "offline"
                                    color: nodeStateRow.isOnline ? Theme.text2 : (side.homeNodeId !== "" ? Theme.bad : Theme.text4)
                                    font.family: Theme.mono
                                    font.pixelSize: Theme.fontCaption
                                }
                            }
                        }

                        // Working folder
                        Row {
                            width: parent.width
                            spacing: Theme.space2

                            Icon {
                                anchors.verticalCenter: parent.verticalCenter
                                name: "folder"
                                size: 14
                                color: Theme.text4
                            }

                            Text {
                                width: parent.width - 22
                                anchors.verticalCenter: parent.verticalCenter
                                text: side.folderPath
                                color: Theme.text3
                                font.family: Theme.mono
                                font.pixelSize: Theme.fontSmall
                                elide: Text.ElideMiddle
                            }
                        }

                        Text {
                            visible: side.homeNodeId !== "" && (!side.pinnedNode || !side.pinnedNode.online) && !side.loadingNodes
                            width: parent.width
                            wrapMode: Text.Wrap
                            text: "Este computador está desligado agora; as tarefas ficam na fila até ele voltar."
                            color: Theme.warn
                            font.pixelSize: Theme.fontSmall
                        }

                        Rectangle {
                            width: parent.width
                            height: 1
                            color: Theme.line
                        }

                        // Mini live browser preview + "Assistir"
                        Column {
                            width: parent.width
                            spacing: Theme.space2

                            Item {
                                width: parent.width
                                height: 30

                                Row {
                                    anchors { left: parent.left; right: watchBtn.left; rightMargin: Theme.space2; verticalCenter: parent.verticalCenter }
                                    spacing: 6

                                    Dot {
                                        visible: side.browserActive || side.browserOwner
                                        anchors.verticalCenter: parent.verticalCenter
                                        kind: side.browserOwner ? "warn" : "live"
                                    }

                                    Icon {
                                        anchors.verticalCenter: parent.verticalCenter
                                        name: "globe"
                                        size: 14
                                        color: side.browserActive ? side.tint : Theme.text3
                                    }

                                    Text {
                                        anchors.verticalCenter: parent.verticalCenter
                                        text: side.browserHost !== "" ? side.browserHost : "Navegador"
                                        color: side.browserActive ? Theme.white : Theme.text2
                                        font.pixelSize: Theme.fontBody
                                        font.weight: side.browserActive ? Font.DemiBold : Font.Medium
                                        elide: Text.ElideRight
                                        width: Math.min(implicitWidth, parent.width - (side.browserActive || side.browserOwner ? 36 : 22))
                                    }
                                }

                                GhostButton {
                                    id: watchBtn
                                    anchors { right: parent.right; verticalCenter: parent.verticalCenter }
                                    small: true
                                    icon: "eye"
                                    text: "Assistir"
                                    onClicked: side.openBrowser()
                                }
                            }

                            Rectangle {
                                id: miniViewport
                                width: parent.width
                                height: 124
                                radius: Theme.radiusSm
                                color: Theme.well
                                border.width: 1
                                border.color: side.browserActive ? Theme.focusLine : (miniBrowserArea.containsMouse ? Theme.lineStrong : Theme.line)
                                clip: true
                                Behavior on border.color { ColorAnimation { duration: Theme.fast } }

                                Image {
                                    id: shotImg
                                    anchors.fill: parent
                                    anchors.margins: 1
                                    visible: side.shotSource !== "" && shotImg.status === Image.Ready
                                    source: side.shotSource
                                    fillMode: Image.PreserveAspectCrop
                                    asynchronous: true
                                    cache: false
                                    smooth: true
                                }

                                // Subtitle strip when a screenshot is shown
                                Rectangle {
                                    visible: shotImg.visible && side.browserSubline !== ""
                                    anchors { left: parent.left; right: parent.right; bottom: parent.bottom }
                                    height: 26
                                    color: Theme.scrim

                                    Text {
                                        anchors { left: parent.left; right: parent.right; leftMargin: Theme.space2; rightMargin: Theme.space2; verticalCenter: parent.verticalCenter }
                                        text: side.browserSubline
                                        color: Theme.white
                                        font.pixelSize: Theme.fontCaption
                                        elide: Text.ElideRight
                                    }
                                }

                                // Empty / idle state inside the mini browser box
                                Column {
                                    visible: !shotImg.visible
                                    anchors.centerIn: parent
                                    width: parent.width - 2 * Theme.space3
                                    spacing: Theme.space2

                                    Icon {
                                        anchors.horizontalCenter: parent.horizontalCenter
                                        name: "globe"
                                        size: 20
                                        color: side.browserActive ? side.tint : Theme.text4
                                    }

                                    Text {
                                        width: parent.width
                                        horizontalAlignment: Text.AlignHCenter
                                        wrapMode: Text.Wrap
                                        text: side.browserActive && side.browserSubline !== ""
                                              ? side.browserSubline
                                              : side.browserHost !== ""
                                                ? ("Última página: " + side.browserHost + " · clique para assistir")
                                                : "Nenhuma página aberta agora. Clique em Assistir para ver ou entrar em contas."
                                        color: Theme.text3
                                        font.pixelSize: Theme.fontSmall
                                    }
                                }

                                MouseArea {
                                    id: miniBrowserArea
                                    anchors.fill: parent
                                    hoverEnabled: true
                                    cursorShape: Qt.PointingHandCursor
                                    onClicked: side.openBrowser()
                                }
                            }
                        }
                    }
                }
            }

            // ── 3. ROTINAS ──────────────────────────────────────
            Column {
                width: parent.width
                spacing: Theme.space2

                Item {
                    width: parent.width
                    height: 20

                    Overline {
                        anchors.verticalCenter: parent.verticalCenter
                        text: "ROTINAS" + (side.routines.length > 0 ? "  ·  " + side.routines.length : "")
                    }

                    Text {
                        anchors { right: parent.right; verticalCenter: parent.verticalCenter }
                        text: "+ Nova"
                        color: addRoutineArea.containsMouse ? Theme.white : Theme.text3
                        font.pixelSize: Theme.fontSmall
                        font.weight: Font.Medium
                        MouseArea {
                            id: addRoutineArea
                            anchors.fill: parent
                            anchors.margins: -Theme.space1
                            hoverEnabled: true
                            cursorShape: Qt.PointingHandCursor
                            onClicked: side.newRoutineRequested()
                        }
                    }
                }

                Repeater {
                    model: side.sortedRoutines
                    delegate: Rectangle {
                        id: rtRow
                        required property var modelData
                        readonly property var r: modelData
                        width: bodyCol.width
                        height: rtCol.implicitHeight + 2 * Theme.space3
                        radius: Theme.radiusMd
                        color: rtArea.containsMouse ? Theme.hover : Theme.wash1
                        border.width: 1
                        border.color: Theme.line
                        opacity: r.enabled ? 1 : 0.65
                        Behavior on color { ColorAnimation { duration: Theme.fast } }

                        MouseArea {
                            id: rtArea
                            anchors.fill: parent
                            hoverEnabled: true
                            cursorShape: Qt.PointingHandCursor
                            onClicked: side.editRoutineRequested(rtRow.r)
                        }

                        Column {
                            id: rtCol
                            x: Theme.space3
                            y: Theme.space3
                            width: parent.width - 2 * Theme.space3 - 44
                            spacing: 3

                            Row {
                                width: parent.width
                                spacing: 7

                                Rectangle {
                                    width: 7
                                    height: 7
                                    radius: height / 2
                                    anchors.verticalCenter: parent.verticalCenter
                                    color: side.routineStatusColor(rtRow.r)
                                }

                                Text {
                                    width: parent.width - 14
                                    text: rtRow.r.title || "Rotina"
                                    color: Theme.text
                                    font.pixelSize: Theme.fontBody
                                    font.weight: Font.Medium
                                    elide: Text.ElideRight
                                }
                            }

                            Text {
                                width: parent.width
                                text: rtRow.r.enabled && rtRow.r.next_run
                                      ? ("Próxima: " + U.when(rtRow.r.next_run) + "  ·  " + U.whenText(rtRow.r))
                                      : rtRow.r.enabled
                                        ? U.whenText(rtRow.r)
                                        : ("Desligada  ·  " + U.whenText(rtRow.r))
                                color: Theme.text3
                                font.pixelSize: Theme.fontSmall
                                wrapMode: Text.Wrap
                                maximumLineCount: 2
                                elide: Text.ElideRight
                            }
                        }

                        // On/off switch
                        Rectangle {
                            anchors { right: parent.right; rightMargin: Theme.space3; verticalCenter: parent.verticalCenter }
                            width: 34
                            height: 20
                            radius: height / 2
                            color: rtRow.r.enabled ? Theme.white : Theme.wash4
                            border.width: 1
                            border.color: rtRow.r.enabled ? Theme.white : Theme.line
                            Behavior on color { ColorAnimation { duration: Theme.fast } }

                            Rectangle {
                                width: 14
                                height: 14
                                radius: height / 2
                                y: 2
                                x: rtRow.r.enabled ? parent.width - width - 3 : 3
                                color: rtRow.r.enabled ? Theme.ink : Theme.text3
                                Behavior on x { NumberAnimation { duration: Theme.fast } }
                            }

                            MouseArea {
                                anchors.fill: parent
                                anchors.margins: -Theme.space1
                                cursorShape: Qt.PointingHandCursor
                                onClicked: side.toggleRoutine(rtRow.r)
                            }
                        }
                    }
                }

                Rectangle {
                    visible: side.routines.length === 0
                    width: parent.width
                    height: emptyRoutinesText.implicitHeight + 2 * Theme.space3
                    radius: Theme.radiusMd
                    color: Theme.wash1
                    border.width: 1
                    border.color: Theme.line

                    Text {
                        id: emptyRoutinesText
                        anchors.centerIn: parent
                        width: parent.width - 2 * Theme.space3
                        wrapMode: Text.Wrap
                        text: side.loadingRoutines
                              ? "Carregando rotinas…"
                              : "Nenhuma rotina ainda. Peça na conversa ou clique em + Nova para agendar."
                        color: Theme.text3
                        font.pixelSize: Theme.fontSmall
                    }
                }
            }

            // ── 4. COMO PENSA ───────────────────────────────────
            Column {
                width: parent.width
                spacing: Theme.space2

                Overline { text: "COMO PENSA" }

                Rectangle {
                    width: parent.width
                    height: brainCol.implicitHeight + 2 * Theme.space3
                    radius: Theme.radiusMd
                    color: Theme.wash1
                    border.width: 1
                    border.color: Theme.line

                    Column {
                        id: brainCol
                        x: Theme.space3
                        y: Theme.space3
                        width: parent.width - 2 * Theme.space3
                        spacing: Theme.space2

                        Item {
                            width: parent.width
                            height: Math.max(30, brainTitleCol.implicitHeight)

                            Column {
                                id: brainTitleCol
                                anchors { left: parent.left; right: changeBrainBtn.left; rightMargin: Theme.space2; verticalCenter: parent.verticalCenter }
                                spacing: 2

                                Row {
                                    width: parent.width
                                    spacing: 6
                                    Icon {
                                        anchors.verticalCenter: parent.verticalCenter
                                        name: "spark"
                                        size: 14
                                        color: side.tint
                                    }
                                    Text {
                                        width: parent.width - 20
                                        anchors.verticalCenter: parent.verticalCenter
                                        text: side.brainHeadline
                                        color: Theme.white
                                        font.pixelSize: Theme.fontBody
                                        font.weight: Font.DemiBold
                                        elide: Text.ElideRight
                                    }
                                }

                                Text {
                                    width: parent.width
                                    text: side.brainAccountLine
                                    color: Theme.text3
                                    font.pixelSize: Theme.fontSmall
                                    elide: Text.ElideRight
                                }
                            }

                            GhostButton {
                                id: changeBrainBtn
                                anchors { right: parent.right; verticalCenter: parent.verticalCenter }
                                small: true
                                text: "Mudar"
                                onClicked: side.changeBrainRequested()
                            }
                        }

                        Text {
                            text: "Conta, esforço e contexto ›"
                            color: brainMoreArea.containsMouse ? Theme.white : Theme.text4
                            font.pixelSize: Theme.fontCaption
                            MouseArea {
                                id: brainMoreArea
                                anchors.fill: parent
                                anchors.margins: -Theme.space1
                                hoverEnabled: true
                                cursorShape: Qt.PointingHandCursor
                                onClicked: side.openAgentFloat("settings")
                            }
                        }
                    }
                }
            }

            // ── 5. TAREFAS RECENTES ─────────────────────────────
            Column {
                width: parent.width
                spacing: Theme.space2

                Overline {
                    text: "TAREFAS RECENTES" + (side.recentRuns.length > 0 ? "  ·  " + side.recentRuns.length : "")
                }

                Repeater {
                    model: side.recentRuns.slice(0, 6)
                    delegate: Rectangle {
                        id: taskCard
                        required property var modelData
                        width: bodyCol.width
                        height: taskInner.implicitHeight + 2 * Theme.space3
                        radius: Theme.radiusMd
                        color: taskArea.pressed ? Theme.active : taskArea.containsMouse ? Theme.hover : Theme.wash1
                        border.width: 1
                        border.color: taskArea.containsMouse ? Theme.lineStrong : Theme.line
                        Behavior on color { ColorAnimation { duration: Theme.fast } }
                        Behavior on border.color { ColorAnimation { duration: Theme.fast } }

                        Column {
                            id: taskInner
                            x: Theme.space3
                            y: Theme.space3
                            width: parent.width - 2 * Theme.space3
                            spacing: Theme.space2

                            Row {
                                width: parent.width
                                spacing: Theme.space2

                                Icon {
                                    anchors.verticalCenter: parent.verticalCenter
                                    name: taskCard.modelData.engine === "codex"
                                          ? "openai"
                                          : taskCard.modelData.engine === "antigravity" ? "spark" : "claude"
                                    size: 14
                                    color: Theme.text2
                                }

                                Text {
                                    width: parent.width - 22
                                    anchors.verticalCenter: parent.verticalCenter
                                    text: taskCard.modelData.title || "Execução"
                                    color: Theme.white
                                    font.pixelSize: Theme.fontBody
                                    font.weight: Font.Medium
                                    elide: Text.ElideRight
                                }
                            }

                            Item {
                                width: parent.width
                                height: taskStatusPill.implicitHeight

                                Text {
                                    anchors { left: parent.left; right: taskStatusPill.left; rightMargin: Theme.space2; verticalCenter: parent.verticalCenter }
                                    text: {
                                        side.tick;
                                        return U.ago(taskCard.modelData.started_at || taskCard.modelData.created_at);
                                    }
                                    color: Theme.text3
                                    font.pixelSize: Theme.fontSmall
                                    elide: Text.ElideRight
                                }

                                StatusPill {
                                    id: taskStatusPill
                                    anchors { right: parent.right; verticalCenter: parent.verticalCenter }
                                    status: taskCard.modelData.status || "running"
                                }
                            }
                        }

                        MouseArea {
                            id: taskArea
                            anchors.fill: parent
                            hoverEnabled: true
                            cursorShape: Qt.PointingHandCursor
                            onClicked: side.openRun(taskCard.modelData.id)
                        }
                    }
                }

                Rectangle {
                    visible: side.recentRuns.length === 0
                    width: parent.width
                    height: emptyRunsText.implicitHeight + 2 * Theme.space3
                    radius: Theme.radiusMd
                    color: Theme.wash1
                    border.width: 1
                    border.color: Theme.line

                    Text {
                        id: emptyRunsText
                        anchors.centerIn: parent
                        width: parent.width - 2 * Theme.space3
                        wrapMode: Text.Wrap
                        text: side.loadingRuns
                              ? "Carregando execuções…"
                              : "Ainda não fez nenhuma execução. Quando trabalhar sozinho, aparece aqui."
                        color: Theme.text3
                        font.pixelSize: Theme.fontSmall
                    }
                }
            }

            // ── 6. DOCS DESTA CONVERSA ──────────────────────────
            Column {
                width: parent.width
                spacing: Theme.space2

                Item {
                    width: parent.width
                    height: 20

                    Overline {
                        anchors.verticalCenter: parent.verticalCenter
                        text: "DOCS DESTA CONVERSA" + (side.convDocsList.length > 0 ? "  ·  " + side.convDocsList.length : "")
                    }

                    Text {
                        anchors { right: parent.right; verticalCenter: parent.verticalCenter }
                        text: "Todos ›"
                        color: allDocsArea.containsMouse ? Theme.white : Theme.text3
                        font.pixelSize: Theme.fontSmall
                        MouseArea {
                            id: allDocsArea
                            anchors.fill: parent
                            anchors.margins: -Theme.space1
                            hoverEnabled: true
                            cursorShape: Qt.PointingHandCursor
                            onClicked: side.openDoc("")
                        }
                    }
                }

                Repeater {
                    model: side.convDocsList
                    delegate: DocRow {
                        required property var modelData
                        width: bodyCol.width
                        dense: true
                        doc: modelData
                        agents: side.agents
                        onOpened: side.openDoc(modelData.id)
                    }
                }

                Rectangle {
                    visible: side.convDocsList.length === 0
                    width: parent.width
                    height: emptyDocsText.implicitHeight + 2 * Theme.space3
                    radius: Theme.radiusMd
                    color: Theme.wash1
                    border.width: 1
                    border.color: Theme.line

                    Text {
                        id: emptyDocsText
                        anchors.centerIn: parent
                        width: parent.width - 2 * Theme.space3
                        wrapMode: Text.Wrap
                        text: side.loadingDocs && !DocBus.loaded
                              ? "Carregando docs…"
                              : "Ainda não há docs desta conversa. Quando o agente criar um doc aqui ou você citar um com @, ele aparece nesta lista."
                        color: Theme.text3
                        font.pixelSize: Theme.fontSmall
                    }
                }
            }
        }
    }
}
