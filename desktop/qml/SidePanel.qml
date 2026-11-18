import QtCore
import QtQuick
import QtQuick.Controls.Basic
import Avora
import "util.js" as U

// Per-conversation right side panel: Membros · Docs · Navegador · Tarefas.
// Remembers the active tab and width for each conversation, and creates one
// BrowserPanel per agent on demand.
Rectangle {
    id: panel
    property string convKey: ""
    property string convId: ""
    property string workspaceId: "pessoal"
    property var agents: []
    property var convAgents: []
    property var agentStatusMap: ({})
    property var citedDocIds: []
    property string openRunId: ""
    property int runsRev: 0
    property real maxAllowedWidth: 720

    property bool shown: false
    property string tab: "members"
    property real panelWidth: 420
    property string pickedBrowserAgent: ""

    signal openAgentRequested(string agentId)
    signal openRunRequested(string runId, string agentId)
    signal focusChatRequested(string agentId)

    readonly property real minPanelWidth: 320
    readonly property real maxPanelWidth: 720
    readonly property real effectiveWidth: shown ? Math.max(minPanelWidth, Math.min(maxPanelWidth, panelWidth, Math.max(minPanelWidth, maxAllowedWidth))) : 0

    readonly property string effectiveBrowserAgent: {
        if (pickedBrowserAgent !== "")
            return pickedBrowserAgent;
        return convAgents.length > 0 ? convAgents[0].id : "";
    }

    readonly property var browserChipAgents: {
        const list = convAgents.slice();
        if (effectiveBrowserAgent !== "" && !list.some(a => a.id === effectiveBrowserAgent)) {
            const extra = agents.find(a => a.id === effectiveBrowserAgent);
            if (extra)
                list.push(extra);
        }
        return list;
    }

    width: effectiveWidth
    visible: width > 0
    color: Theme.bg
    clip: true

    // ── Per-conversation preferences ────────────────────────────
    Settings {
        category: "sidePanel"
        property alias prefsJson: panel.prefsStore
    }
    property string prefsStore: "{}"
    property var prefs: ({})
    property bool restoring: false
    property bool prefsLoaded: false

    function clampWidth(w) {
        return Math.max(minPanelWidth, Math.min(maxPanelWidth, Math.round(w || 420)));
    }

    function ensurePrefsLoaded() {
        if (prefsLoaded)
            return;
        prefsLoaded = true;
        try {
            prefs = JSON.parse(prefsStore || "{}") || {};
        } catch (e) {
            prefs = {};
        }
    }

    function applyPrefs() {
        ensurePrefsLoaded();
        if (convKey === "")
            return;
        restoring = true;
        const p = prefs[convKey];
        if (p) {
            tab = ["members", "docs", "browser", "tasks"].indexOf(p.tab) >= 0 ? p.tab : "members";
            panelWidth = clampWidth(p.width);
            if (p.open !== undefined)
                shown = !!p.open;
        } else {
            tab = "members";
            panelWidth = clampWidth(panelWidth);
        }
        restoring = false;
    }

    function savePrefs() {
        if (restoring || convKey === "")
            return;
        ensurePrefsLoaded();
        const next = Object.assign({}, prefs);
        next[convKey] = {
            open: shown,
            tab: tab,
            width: clampWidth(panelWidth)
        };
        const keys = Object.keys(next);
        if (keys.length > 200)
            delete next[keys[0]];
        prefs = next;
        prefsStore = JSON.stringify(next);
    }

    function toggle() {
        shown = !shown;
        savePrefs();
    }

    function setTab(nextTab) {
        if (["members", "docs", "browser", "tasks"].indexOf(nextTab) < 0)
            return;
        tab = nextTab;
        savePrefs();
    }

    function openTab(nextTab, agentForBrowser) {
        if (agentForBrowser)
            pickBrowserAgent(agentForBrowser);
        if (["members", "docs", "browser", "tasks"].indexOf(nextTab) >= 0)
            tab = nextTab;
        shown = true;
        savePrefs();
    }

    function closePanel() {
        if (!shown)
            return;
        shown = false;
        savePrefs();
    }

    function pickBrowserAgent(id) {
        if (!id)
            return;
        pickedBrowserAgent = id;
        ensureBrowser(id);
    }

    onConvKeyChanged: {
        pickedBrowserAgent = "";
        applyPrefs();
        refreshActiveTab();
    }

    onShownChanged: {
        if (shown)
            refreshActiveTab();
    }

    onTabChanged: {
        if (shown)
            refreshActiveTab();
    }

    Component.onCompleted: {
        ensurePrefsLoaded();
        applyPrefs();
        if (Api.signedIn)
            pollWildcard();
    }

    function refreshActiveTab() {
        if (!shown || !Api.signedIn)
            return;
        if (tab === "members")
            loadMembers();
        else if (tab === "docs")
            loadConvDocs();
        else if (tab === "browser")
            ensureBrowser(effectiveBrowserAgent);
        else if (tab === "tasks")
            loadTasks();
    }

    // ── Members data ────────────────────────────────────────────
    property var people: [{ person_id: "self", name: "Você", online: true, presence: "auto", status_text: "" }]
    property bool loadingMembers: false

    function currentWorkspace() {
        if ("workspaceId" in Api && Api.workspaceId)
            return Api.workspaceId;
        if ("workspace" in Api && Api.workspace)
            return Api.workspace;
        return workspaceId !== "" ? workspaceId : "pessoal";
    }

    function loadMembers() {
        if (!Api.signedIn)
            return;
        const ws = currentWorkspace();
        loadingMembers = true;
        Api.get("/v1/workspaces/" + encodeURIComponent(ws) + "/members", (err, data, status) => {
            loadingMembers = false;
            if (err || status === 404 || !Array.isArray(data)) {
                panel.people = [{ person_id: "self", name: "Você", online: true, presence: "auto", status_text: "" }];
                return;
            }
            const active = data.filter(m => m.status !== "pending");
            panel.people = active.length > 0 ? active : [{ person_id: "self", name: "Você", online: true, presence: "auto", status_text: "" }];
        });
    }

    onWorkspaceIdChanged: if (shown && tab === "members") loadMembers()

    Timer {
        interval: 20000
        repeat: true
        running: panel.shown && panel.visible && panel.tab === "members" && Api.signedIn
        onTriggered: panel.loadMembers()
    }

    function personDotKind(m) {
        if (!m)
            return "";
        if (m.presence === "away")
            return "warn";
        if (m.online)
            return m.presence === "busy" ? "bad" : "ok";
        return "";
    }

    function personStatusText(m) {
        if (!m)
            return "";
        if (m.status_text)
            return m.status_text;
        if (m.presence === "away")
            return "Ausente";
        if (m.presence === "busy")
            return "Ocupado";
        return m.online ? "Online" : "Offline";
    }

    // ── Docs data ───────────────────────────────────────────────
    property var serverConvDocs: []
    property string docQuery: ""
    property var docHits: []
    property bool loadingDocs: false

    function loadConvDocs() {
        if (!Api.signedIn || convId === "") {
            serverConvDocs = [];
            return;
        }
        const asked = convId;
        loadingDocs = true;
        Api.get("/v1/docs?limit=200&conversation=" + encodeURIComponent(asked), (err, data) => {
            if (asked !== panel.convId)
                return;
            loadingDocs = false;
            if (!err && Array.isArray(data))
                panel.serverConvDocs = data;
        });
    }

    onConvIdChanged: {
        serverConvDocs = [];
        if (shown && tab === "docs")
            loadConvDocs();
    }

    Connections {
        target: DocBus
        function onChanged() {
            if (panel.shown && panel.tab === "docs") {
                panel.loadConvDocs();
                if (panel.docQuery.trim() !== "")
                    docSearchTimer.restart();
            }
        }
    }

    readonly property var convDocsList: {
        DocBus.rev;
        const out = [];
        const seen = {};
        const cid = panel.convId;
        const cited = panel.citedDocIds || [];
        const addIfMatch = d => {
            if (!d || !d.id || seen[d.id])
                return;
            if ((cid !== "" && d.conversation_id === cid) || cited.indexOf(d.id) >= 0) {
                seen[d.id] = true;
                out.push(d);
            }
        };
        for (const d of panel.serverConvDocs)
            addIfMatch(d);
        for (const d of DocBus.docs)
            addIfMatch(d);
        return out;
    }

    readonly property var otherDocsList: {
        DocBus.rev;
        const inConv = {};
        for (const d of panel.convDocsList)
            inConv[d.id] = true;
        const rest = DocBus.docs.filter(d => !inConv[d.id]);
        return rest.filter(d => d.pinned).concat(rest.filter(d => !d.pinned)).slice(0, 40);
    }

    Timer {
        id: docSearchTimer
        interval: 220
        onTriggered: {
            const q = panel.docQuery.trim();
            if (q === "" || !Api.signedIn) {
                panel.docHits = [];
                return;
            }
            Api.get("/v1/docs?limit=60&q=" + encodeURIComponent(q), (err, data) => {
                if (!err && q === panel.docQuery.trim())
                    panel.docHits = data || [];
            });
        }
    }
    onDocQueryChanged: docSearchTimer.restart()

    // ── On-demand BrowserPanels & wildcard app poller ───────────
    property var openedBrowserIds: []
    property bool wildcardPolling: false

    onEffectiveBrowserAgentChanged: {
        if (shown && tab === "browser" && effectiveBrowserAgent !== "")
            ensureBrowser(effectiveBrowserAgent);
    }

    function ensureBrowser(id) {
        if (!id || openedBrowserIds.indexOf(id) >= 0)
            return;
        openedBrowserIds = openedBrowserIds.concat([id]);
    }

    function browserItemFor(id) {
        for (let i = 0; i < browserRepeater.count; i++) {
            const it = browserRepeater.itemAt(i);
            if (it && it.agentId === id)
                return it;
        }
        return null;
    }

    function afterMs(ms, fn) {
        const t = Qt.createQmlObject("import QtQuick; Timer {}", panel);
        t.interval = ms;
        t.triggered.connect(() => {
            t.destroy();
            fn();
        });
        t.start();
    }

    // Keeps "app" ready on the server with zero WebEngineViews until an agent
    // actually asks to browse (or the person opens the Navegador tab).
    function pollWildcard() {
        if (!Api.signedIn) {
            wildcardPolling = false;
            return;
        }
        wildcardPolling = true;
        Api.post("/v1/browser/app/next", {}, (err, call) => {
            if (err || !call || !call.id) {
                panel.afterMs(err ? 4000 : 80, panel.pollWildcard);
                return;
            }
            const targetId = call.agent || panel.effectiveBrowserAgent || (panel.agents.length ? panel.agents[0].id : "default");
            panel.ensureBrowser(targetId);
            Qt.callLater(() => {
                const bp = panel.browserItemFor(targetId);
                if (bp) {
                    bp.executeCall(call);
                } else {
                    Api.post("/v1/browser/app/result", { id: call.id, agent: targetId, result: { ok: false, error: "navegador indisponível" } }, () => {});
                }
                panel.pollWildcard();
            });
        });
    }

    Connections {
        target: Api
        function onSignedInChanged() {
            if (Api.signedIn) {
                if (!panel.wildcardPolling)
                    panel.pollWildcard();
                panel.refreshActiveTab();
            } else {
                panel.openedBrowserIds = [];
            }
        }
    }

    Timer {
        interval: 6000
        repeat: true
        running: Api.signedIn
        onTriggered: {
            if (!panel.wildcardPolling)
                panel.pollWildcard();
        }
    }

    // ── Tasks (active runs) data ────────────────────────────────
    property var taskRuns: []
    property bool loadingTasks: false
    property int taskFetchSeq: 0

    function loadTasks() {
        if (!Api.signedIn) {
            taskRuns = [];
            return;
        }
        const ids = convAgents.map(a => a.id).filter(Boolean);
        if (ids.length === 0) {
            taskRuns = [];
            return;
        }
        const seq = ++taskFetchSeq;
        loadingTasks = true;
        let left = ids.length;
        const acc = [];
        const seen = {};
        for (const id of ids) {
            Api.get("/v1/runs?agent=" + encodeURIComponent(id) + "&active=1", (err, list) => {
                if (seq !== panel.taskFetchSeq)
                    return;
                if (!err && Array.isArray(list)) {
                    for (const r of list) {
                        if (r && r.id && !seen[r.id]) {
                            seen[r.id] = true;
                            acc.push(r);
                        }
                    }
                }
                left--;
                if (left === 0) {
                    panel.loadingTasks = false;
                    acc.sort((a, b) => String(b.created_at || "").localeCompare(String(a.created_at || "")));
                    if (!U.same(panel.taskRuns, acc))
                        panel.taskRuns = acc;
                }
            });
        }
    }

    onRunsRevChanged: if (shown && tab === "tasks") loadTasks()
    onConvAgentsChanged: {
        if (shown && tab === "tasks")
            loadTasks();
        if (shown && tab === "browser" && effectiveBrowserAgent !== "")
            ensureBrowser(effectiveBrowserAgent);
    }

    Timer {
        interval: 4000
        repeat: true
        running: panel.shown && panel.visible && panel.tab === "tasks" && Api.signedIn
        onTriggered: panel.loadTasks()
    }

    // ── Left border & resize handle ─────────────────────────────
    Rectangle {
        anchors { left: parent.left; top: parent.top; bottom: parent.bottom }
        width: 1
        color: resizeArea.pressed || resizeArea.containsMouse ? Theme.lineStrong : Theme.line
        Behavior on color { ColorAnimation { duration: Theme.fast } }
    }

    MouseArea {
        id: resizeArea
        enabled: panel.shown
        anchors { left: parent.left; top: parent.top; bottom: parent.bottom; leftMargin: -3 }
        width: 7
        z: 30
        hoverEnabled: true
        cursorShape: Qt.SizeHorCursor
        property real fromX: 0
        onPressed: mouse => {
            fromX = mapToItem(null, mouse.x, 0).x;
        }
        onPositionChanged: mouse => {
            if (!pressed)
                return;
            const x = mapToItem(null, mouse.x, 0).x;
            panel.panelWidth = panel.clampWidth(panel.panelWidth + fromX - x);
            fromX = x;
        }
        onReleased: panel.savePrefs()
    }

    // ── Top tab bar (below frameless window buttons) ────────────
    Item {
        id: topBar
        anchors { left: parent.left; right: parent.right; top: parent.top; topMargin: Theme.titleBar; leftMargin: 1 }
        height: 44

        Row {
            id: tabRow
            anchors { left: parent.left; leftMargin: Theme.space2; right: closeBtn.left; rightMargin: Theme.space1; top: parent.top; bottom: parent.bottom }
            spacing: 2

            Repeater {
                model: [
                    { key: "members", label: "Membros", icon: "users" },
                    { key: "docs", label: "Docs", icon: "docs" },
                    { key: "browser", label: "Navegador", icon: "globe" },
                    { key: "tasks", label: "Tarefas", icon: "tasks" }
                ]
                delegate: Item {
                    id: tb
                    required property var modelData
                    readonly property bool on: panel.tab === modelData.key
                    width: tbInner.implicitWidth + (panel.width >= 380 ? 2 * Theme.space2 + 4 : 2 * Theme.space1 + 6)
                    height: tabRow.height

                    Rectangle {
                        anchors { fill: parent; topMargin: 6; bottomMargin: 6 }
                        radius: Theme.radiusSm
                        color: tbArea.containsMouse && !tb.on ? Theme.hover : "transparent"
                        Behavior on color { ColorAnimation { duration: Theme.fast } }
                    }
                    Row {
                        id: tbInner
                        anchors.centerIn: parent
                        spacing: 5
                        Icon {
                            visible: panel.width >= 490
                            anchors.verticalCenter: parent.verticalCenter
                            name: tb.modelData.icon
                            size: 13
                            color: tb.on ? Theme.white : Theme.text3
                        }
                        Text {
                            anchors.verticalCenter: parent.verticalCenter
                            text: tb.modelData.label
                            color: tb.on ? Theme.white : tbArea.containsMouse ? Theme.text2 : Theme.text3
                            font.pixelSize: panel.width < 360 ? Theme.fontSmall : Theme.fontBody
                            font.weight: tb.on ? Font.DemiBold : Font.Medium
                            Behavior on color { ColorAnimation { duration: Theme.fast } }
                        }
                    }
                    Rectangle {
                        visible: tb.on
                        anchors { left: parent.left; right: parent.right; bottom: parent.bottom; leftMargin: Theme.space2; rightMargin: Theme.space2 }
                        height: 2
                        radius: 1
                        color: Theme.white
                    }
                    MouseArea {
                        id: tbArea
                        anchors.fill: parent
                        hoverEnabled: true
                        cursorShape: Qt.PointingHandCursor
                        onClicked: panel.setTab(tb.modelData.key)
                    }
                }
            }
        }

        RowTool {
            id: closeBtn
            anchors { right: parent.right; rightMargin: Theme.space2; verticalCenter: parent.verticalCenter }
            icon: "close"
            tip: "Fechar painel"
            onHit: panel.closePanel()
        }

        Rectangle {
            anchors { left: parent.left; right: parent.right; bottom: parent.bottom }
            height: 1
            color: Theme.line
        }
    }

    // ── Body container ──────────────────────────────────────────
    Item {
        id: contentArea
        anchors { left: parent.left; right: parent.right; top: topBar.bottom; bottom: parent.bottom; leftMargin: 1 }

        // ── 1. MEMBROS ──────────────────────────────────────────
        Flickable {
            id: membersFlick
            visible: panel.tab === "members"
            anchors.fill: parent
            contentHeight: membersCol.implicitHeight + 2 * Theme.space5
            clip: true
            boundsBehavior: Flickable.StopAtBounds
            ScrollBar.vertical: SlimScrollBar {}
            SmoothWheel {}

            Column {
                id: membersCol
                x: Theme.space3
                y: Theme.space4
                width: membersFlick.width - 2 * Theme.space3
                spacing: Theme.space4

                Column {
                    width: parent.width
                    spacing: Theme.space1
                    Overline {
                        leftPadding: Theme.space2
                        bottomPadding: Theme.space1
                        text: "PESSOAS  ·  " + panel.people.length
                    }
                    Repeater {
                        model: panel.people
                        delegate: Rectangle {
                            id: personRow
                            required property var modelData
                            width: membersCol.width
                            height: 52
                            radius: Theme.radiusMd
                            color: personHover.hovered ? Theme.hover : "transparent"
                            Behavior on color { ColorAnimation { duration: Theme.fast } }

                            Avatar {
                                id: personFace
                                x: Theme.space2
                                anchors.verticalCenter: parent.verticalCenter
                                size: 36
                                name: personRow.modelData.name || "Você"
                                photo: personRow.modelData.avatar || ""
                                tint: Theme.text2
                            }
                            Column {
                                anchors { left: personFace.right; leftMargin: Theme.space3; right: parent.right; rightMargin: Theme.space3; verticalCenter: parent.verticalCenter }
                                spacing: 2
                                Row {
                                    width: parent.width
                                    spacing: Theme.space2
                                    Text {
                                        text: personRow.modelData.name || "Você"
                                        color: Theme.text
                                        font.pixelSize: Theme.fontBody
                                        font.weight: Font.DemiBold
                                        elide: Text.ElideRight
                                        width: Math.min(implicitWidth, parent.width - 20)
                                    }
                                    Dot {
                                        anchors.verticalCenter: parent.verticalCenter
                                        kind: panel.personDotKind(personRow.modelData)
                                    }
                                }
                                Text {
                                    width: parent.width
                                    text: panel.personStatusText(personRow.modelData)
                                    color: Theme.text3
                                    font.pixelSize: Theme.fontSmall
                                    elide: Text.ElideRight
                                }
                            }
                            HoverHandler {
                                id: personHover
                            }
                        }
                    }
                }

                Column {
                    width: parent.width
                    spacing: Theme.space1
                    Overline {
                        leftPadding: Theme.space2
                        bottomPadding: Theme.space1
                        text: "AGENTES  ·  " + panel.convAgents.length
                    }
                    Repeater {
                        model: panel.convAgents
                        delegate: Rectangle {
                            id: agentRow
                            required property var modelData
                            readonly property var st: panel.agentStatusMap[modelData.id] || ({})
                            readonly property bool busyNow: !!st.working || !!modelData.working
                            readonly property string line: st.status || modelData.status_text || (busyNow ? "Trabalhando…" : (modelData.role || "Livre"))
                            width: membersCol.width
                            height: 56
                            radius: Theme.radiusMd
                            color: agentArea.pressed ? Theme.active : agentArea.containsMouse ? Theme.hover : "transparent"
                            Behavior on color { ColorAnimation { duration: Theme.fast } }

                            Avatar {
                                id: agentFace
                                x: Theme.space2
                                anchors.verticalCenter: parent.verticalCenter
                                size: 36
                                name: agentRow.modelData.name || "?"
                                photo: U.photo(agentRow.modelData)
                                tint: agentRow.modelData.color || Theme.text2
                                working: agentRow.busyNow
                            }
                            Column {
                                anchors { left: agentFace.right; leftMargin: Theme.space3; right: agentChev.left; rightMargin: Theme.space2; verticalCenter: parent.verticalCenter }
                                spacing: 2
                                Row {
                                    width: parent.width
                                    spacing: Theme.space2
                                    Text {
                                        text: agentRow.modelData.name || ""
                                        color: Theme.white
                                        font.pixelSize: Theme.fontBody
                                        font.weight: Font.DemiBold
                                        elide: Text.ElideRight
                                        width: Math.min(implicitWidth, parent.width - 20)
                                    }
                                    Dot {
                                        anchors.verticalCenter: parent.verticalCenter
                                        kind: agentRow.busyNow ? "live" : ""
                                    }
                                }
                                Text {
                                    width: parent.width
                                    text: agentRow.line
                                    color: agentRow.busyNow ? Theme.text2 : Theme.text3
                                    font.pixelSize: Theme.fontSmall
                                    elide: Text.ElideRight
                                }
                            }
                            Icon {
                                id: agentChev
                                anchors { right: parent.right; rightMargin: Theme.space2; verticalCenter: parent.verticalCenter }
                                name: "chevron"
                                size: 12
                                color: Theme.text4
                                opacity: agentArea.containsMouse ? 1 : 0
                                Behavior on opacity { NumberAnimation { duration: Theme.fast } }
                            }
                            MouseArea {
                                id: agentArea
                                anchors.fill: parent
                                hoverEnabled: true
                                cursorShape: Qt.PointingHandCursor
                                onClicked: panel.openAgentRequested(agentRow.modelData.id)
                            }
                        }
                    }

                    Text {
                        visible: panel.convAgents.length === 0
                        width: parent.width - 2 * Theme.space2
                        x: Theme.space2
                        topPadding: Theme.space2
                        text: "Nenhum agente participa desta conversa ainda."
                        color: Theme.text3
                        font.pixelSize: Theme.fontBody
                        wrapMode: Text.Wrap
                    }
                }
            }
        }

        // ── 2. DOCS ─────────────────────────────────────────────
        Item {
            id: docsArea
            visible: panel.tab === "docs"
            anchors.fill: parent

            // Open doc inside the panel using existing DocPanel.
            DocPanel {
                id: embeddedDoc
                visible: DocBus.openId !== ""
                embedded: true
                anchors.fill: parent
                onCloseRequested: DocBus.openId = ""
            }

            // Conversation docs + search when no single doc is open.
            Item {
                visible: DocBus.openId === ""
                anchors.fill: parent

                Field {
                    id: docSearch
                    anchors { left: parent.left; right: parent.right; top: parent.top; margins: Theme.space3 }
                    leftPadding: 36
                    placeholderText: "Buscar nos docs…"
                    text: panel.docQuery
                    onTextEdited: panel.docQuery = text
                    Keys.onEscapePressed: {
                        text = "";
                        panel.docQuery = "";
                    }
                    Icon {
                        x: Theme.space3
                        anchors.verticalCenter: parent.verticalCenter
                        name: "search"
                        size: 14
                        color: Theme.text4
                    }
                }

                Flickable {
                    id: docsFlick
                    anchors { left: parent.left; right: parent.right; top: docSearch.bottom; bottom: parent.bottom; topMargin: Theme.space1 }
                    contentHeight: docsCol.implicitHeight + 2 * Theme.space5
                    clip: true
                    boundsBehavior: Flickable.StopAtBounds
                    ScrollBar.vertical: SlimScrollBar {}
                    SmoothWheel {}

                    Column {
                        id: docsCol
                        x: Theme.space2
                        y: Theme.space2
                        width: docsFlick.width - 2 * Theme.space2
                        spacing: Theme.space4

                        // Search results.
                        Column {
                            visible: panel.docQuery.trim() !== ""
                            width: parent.width
                            spacing: 2
                            Overline {
                                leftPadding: Theme.space2
                                bottomPadding: Theme.space1
                                text: "RESULTADOS  ·  " + panel.docHits.length
                            }
                            Repeater {
                                model: panel.docQuery.trim() !== "" ? panel.docHits : []
                                delegate: DocRow {
                                    required property var modelData
                                    width: docsCol.width
                                    dense: true
                                    doc: modelData
                                    agents: panel.agents
                                    onOpened: DocBus.open(modelData.id)
                                }
                            }
                            Text {
                                visible: panel.docHits.length === 0
                                width: parent.width - 2 * Theme.space3
                                x: Theme.space3
                                topPadding: Theme.space3
                                text: "Nenhum doc encontrado para «" + panel.docQuery.trim() + "»."
                                color: Theme.text3
                                font.pixelSize: Theme.fontBody
                                wrapMode: Text.Wrap
                            }
                        }

                        // Docs created or cited in this conversation.
                        Column {
                            visible: panel.docQuery.trim() === ""
                            width: parent.width
                            spacing: 2
                            Overline {
                                leftPadding: Theme.space2
                                bottomPadding: Theme.space1
                                text: "DESTA CONVERSA" + (panel.convDocsList.length > 0 ? "  ·  " + panel.convDocsList.length : "")
                            }
                            Repeater {
                                model: panel.docQuery.trim() === "" ? panel.convDocsList : []
                                delegate: DocRow {
                                    required property var modelData
                                    width: docsCol.width
                                    dense: true
                                    doc: modelData
                                    agents: panel.agents
                                    onOpened: DocBus.open(modelData.id)
                                }
                            }
                            Rectangle {
                                visible: panel.convDocsList.length === 0
                                width: parent.width - 2 * Theme.space1
                                x: Theme.space1
                                height: emptyConvDocsText.implicitHeight + 2 * Theme.space3
                                radius: Theme.radiusMd
                                color: Theme.wash1
                                border.width: 1
                                border.color: Theme.line
                                Text {
                                    id: emptyConvDocsText
                                    anchors.centerIn: parent
                                    width: parent.width - 2 * Theme.space3
                                    horizontalAlignment: Text.AlignHCenter
                                    wrapMode: Text.Wrap
                                    text: panel.loadingDocs && !DocBus.loaded
                                          ? "Carregando docs…"
                                          : "Ainda não há docs desta conversa. Quando um agente criar um doc aqui ou você citar um, ele aparece nesta lista."
                                    color: Theme.text3
                                    font.pixelSize: Theme.fontSmall
                                }
                            }
                        }

                        // Other workspace docs, ready to open or drag into chat.
                        Column {
                            visible: panel.docQuery.trim() === "" && panel.otherDocsList.length > 0
                            width: parent.width
                            spacing: 2
                            Overline {
                                leftPadding: Theme.space2
                                bottomPadding: Theme.space1
                                text: "RECENTES E FIXADOS"
                            }
                            Repeater {
                                model: panel.docQuery.trim() === "" ? panel.otherDocsList : []
                                delegate: DocRow {
                                    required property var modelData
                                    width: docsCol.width
                                    dense: true
                                    doc: modelData
                                    agents: panel.agents
                                    onOpened: DocBus.open(modelData.id)
                                }
                            }
                        }
                    }
                }
            }
        }

        // ── 3. NAVEGADOR ────────────────────────────────────────
        // Kept alive in the scene even when another tab is open so open
        // BrowserPanels preserve their pages; only visible on the browser tab.
        Item {
            id: browserTab
            visible: panel.tab === "browser"
            anchors.fill: parent

            Item {
                id: agentPickerBar
                visible: panel.browserChipAgents.length > 1
                anchors { left: parent.left; right: parent.right; top: parent.top }
                height: visible ? agentChipsFlow.implicitHeight + 2 * Theme.space2 + 1 : 0

                Flow {
                    id: agentChipsFlow
                    x: Theme.space3
                    y: Theme.space2
                    width: parent.width - 2 * Theme.space3
                    spacing: 6
                    Repeater {
                        model: panel.browserChipAgents
                        delegate: Pill {
                            required property var modelData
                            text: modelData.name
                            on: panel.effectiveBrowserAgent === modelData.id
                            onHit: panel.pickBrowserAgent(modelData.id)
                        }
                    }
                }
                Rectangle {
                    anchors { left: parent.left; right: parent.right; bottom: parent.bottom }
                    height: 1
                    color: Theme.line
                }
            }

            Item {
                id: browserHost
                anchors { left: parent.left; right: parent.right; top: agentPickerBar.visible ? agentPickerBar.bottom : parent.top; bottom: parent.bottom }

                Repeater {
                    id: browserRepeater
                    model: panel.openedBrowserIds
                    delegate: BrowserPanel {
                        id: bpItem
                        required property string modelData
                        readonly property var ag: panel.agents.find(a => a.id === modelData) || null
                        anchors.fill: parent
                        embedded: true
                        agentId: modelData
                        agentName: ag ? ag.name : ""
                        visible: panel.tab === "browser" && modelData === panel.effectiveBrowserAgent
                        open: panel.shown && panel.visible && panel.tab === "browser" && modelData === panel.effectiveBrowserAgent
                        onOpenRequested: {
                            panel.focusChatRequested(modelData);
                            panel.openTab("browser", modelData);
                        }
                        onCloseRequested: panel.closePanel()
                    }
                }

                // Empty state when no agent exists in the conversation.
                Text {
                    visible: panel.convAgents.length === 0 && panel.openedBrowserIds.length === 0
                    anchors.centerIn: parent
                    width: parent.width - 2 * Theme.space5
                    horizontalAlignment: Text.AlignHCenter
                    wrapMode: Text.Wrap
                    text: "Selecione uma conversa com um agente para abrir o navegador dele."
                    color: Theme.text3
                    font.pixelSize: Theme.fontBody
                }
            }
        }

        // ── 4. TAREFAS ──────────────────────────────────────────
        Flickable {
            id: tasksFlick
            visible: panel.tab === "tasks"
            anchors.fill: parent
            contentHeight: tasksCol.implicitHeight + 2 * Theme.space5
            clip: true
            boundsBehavior: Flickable.StopAtBounds
            ScrollBar.vertical: SlimScrollBar {}
            SmoothWheel {}

            Column {
                id: tasksCol
                x: Theme.space3
                y: Theme.space4
                width: tasksFlick.width - 2 * Theme.space3
                spacing: Theme.space2

                Overline {
                    visible: panel.taskRuns.length > 0
                    leftPadding: Theme.space1
                    bottomPadding: Theme.space1
                    text: "EM EXECUÇÃO  ·  " + panel.taskRuns.length
                }

                Repeater {
                    model: panel.taskRuns
                    delegate: Rectangle {
                        id: taskCard
                        required property var modelData
                        readonly property var ownerAgent: panel.agents.find(a => a.id === modelData.agent_id) || null
                        readonly property bool selected: panel.openRunId !== "" && panel.openRunId === modelData.id
                        width: tasksCol.width
                        height: taskInner.implicitHeight + 2 * Theme.space3
                        radius: Theme.radiusMd
                        color: selected ? Theme.active : taskArea.containsMouse ? Theme.hover : Theme.wash1
                        border.width: 1
                        border.color: selected ? Theme.lineStrong : Theme.line
                        Behavior on color { ColorAnimation { duration: Theme.fast } }

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
                                    name: taskCard.modelData.engine === "codex" ? "openai" : taskCard.modelData.engine === "antigravity" ? "spark" : "claude"
                                    size: 15
                                    color: Theme.text
                                }
                                Text {
                                    width: parent.width - 24
                                    anchors.verticalCenter: parent.verticalCenter
                                    text: taskCard.modelData.title || "Execução"
                                    color: Theme.white
                                    font.pixelSize: Theme.fontBody
                                    font.weight: Font.DemiBold
                                    elide: Text.ElideRight
                                }
                            }

                            Item {
                                width: parent.width
                                height: statusPill.implicitHeight
                                Text {
                                    anchors { left: parent.left; right: statusPill.left; rightMargin: Theme.space2; verticalCenter: parent.verticalCenter }
                                    text: (taskCard.ownerAgent ? taskCard.ownerAgent.name + "  ·  " : "")
                                          + U.ago(taskCard.modelData.started_at || taskCard.modelData.created_at)
                                    color: Theme.text3
                                    font.pixelSize: Theme.fontSmall
                                    elide: Text.ElideRight
                                }
                                StatusPill {
                                    id: statusPill
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
                            onClicked: panel.openRunRequested(taskCard.modelData.id, taskCard.modelData.agent_id || "")
                        }
                    }
                }

                // Empty state for Tarefas.
                Column {
                    visible: panel.taskRuns.length === 0
                    width: parent.width
                    topPadding: Theme.space6
                    spacing: Theme.space3
                    Icon {
                        anchors.horizontalCenter: parent.horizontalCenter
                        name: "tasks"
                        size: 28
                        color: Theme.text3
                    }
                    Text {
                        width: parent.width - 2 * Theme.space3
                        anchors.horizontalCenter: parent.horizontalCenter
                        horizontalAlignment: Text.AlignHCenter
                        text: panel.loadingTasks ? "Procurando tarefas ativas…" : "Nenhuma tarefa ativa agora."
                        color: Theme.text
                        font.pixelSize: Theme.fontLarge
                        font.weight: Font.Medium
                        wrapMode: Text.Wrap
                    }
                    Text {
                        visible: !panel.loadingTasks
                        width: parent.width - 2 * Theme.space3
                        anchors.horizontalCenter: parent.horizontalCenter
                        horizontalAlignment: Text.AlignHCenter
                        wrapMode: Text.Wrap
                        text: "Quando um agente desta conversa delegar código ou pesquisa ao Claude Code, Codex ou Antigravity, o andamento aparece aqui."
                        color: Theme.text3
                        font.pixelSize: Theme.fontBody
                    }
                }
            }
        }
    }
}
