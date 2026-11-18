import QtCore
import QtQuick
import QtQuick.Controls.Basic
import QtQuick.Effects
import Avora
import "util.js" as U

// Single unified sidebar: workspace card + Ctrl+K at top, tools, channels,
// agents grouped by sector, direct messages, and account + notifications footer.
// Only the middle section scrolls; in compact mode it shrinks to an icon/face rail.
Rectangle {
    id: sidebar

    readonly property real full: 260
    readonly property real slim: 72
    property bool compact: false

    property bool workspaceMenuOpen: false
    property bool accountMenuOpen: false

    readonly property Item workspaceButton: compact ? wsSlimBtn : wsCard
    readonly property Item accountButton: compact ? meSlimBtn : meBtn
    readonly property real workspaceMenuY: topBox.y + (compact ? wsSlimBtn.y + wsSlimBtn.height : wsHeaderRow.y + wsHeaderRow.height) + Theme.space1
    readonly property real accountMenuY: footerBox.y

    property var agents: []
    property var channels: []
    property var sectors: []
    property var members: []
    property var homeData: ({ waiting: [], working: [], next_routines: [] })
    property int goalsCount: 0
    property int routinesCount: 0
    property int pluginsCount: 0
    property int skillsCount: 0

    property string seenStore: "{}"
    property string collapsedStore: "{}"
    property var seen: ({})
    property var collapsed: ({})
    property var unreadCounts: ({})

    // Shared tooltip outside the clipped Flickable so compact-mode tips float over pages.
    property string tipText: ""
    property real tipY: 0
    property bool tipShown: false

    signal workspaceClicked
    signal accountClicked

    Settings {
        category: "sidebar"
        property alias seenJson: sidebar.seenStore
        property alias collapsedJson: sidebar.collapsedStore
    }

    readonly property var mePerson: (Api.me && Api.me.person && Api.me.person.id) ? Api.me.person : null
    readonly property string myPersonId: mePerson ? (mePerson.id || "") : ""
    readonly property var workspaces: (Api.me && Api.me.workspaces) ? Api.me.workspaces : []
    readonly property string currentWsId: Api.workspace !== "" ? Api.workspace
        : (workspaces.length > 0 ? workspaces[0].id : "pessoal")
    readonly property var currentWorkspace: workspaces.find(w => w.id === currentWsId)
        || (workspaces.length > 0 ? workspaces[0] : null)
    readonly property string wsName: currentWorkspace && currentWorkspace.name ? currentWorkspace.name : "Pessoal"
    readonly property string wsIcon: currentWorkspace && currentWorkspace.icon ? currentWorkspace.icon : ""

    readonly property var activeMembers: members.filter(m => m.status !== "pending" && !!m.person_id)
    readonly property var otherMembers: myPersonId !== ""
        ? activeMembers.filter(m => m.person_id !== myPersonId)
        : (activeMembers.length > 1 ? activeMembers : [])

    // Personal mode: solo workspace without other people.
    readonly property bool isPersonal: (currentWorkspace ? currentWorkspace.mode === "personal" : currentWsId === "pessoal")
        && otherMembers.length === 0

    readonly property int waitingCount: ((homeData && homeData.waiting) || []).length
    readonly property int workingCount: ((homeData && homeData.working) || []).length
    readonly property int docsCount: {
        DocBus.rev;
        return DocBus.docs.length;
    }

    readonly property var directTools: {
        const out = [{ key: "home", label: "Início", icon: "spark", count: 0, warn: false }];
        if (waitingCount > 0)
            out.push({ key: "waiting", label: "Esperando você", icon: "alert", count: waitingCount, warn: true });
        if (!isPersonal) {
            out.push({ key: "docs", label: "Docs", icon: "docs", count: docsCount, warn: false });
            out.push({ key: "goals", label: "Objetivos", icon: "target", count: goalsCount, warn: false });
            out.push({ key: "team", label: "Equipe", icon: "users", count: agents.length, warn: false });
        }
        return out;
    }

    readonly property var moreTools: {
        const out = [];
        if (isPersonal) {
            out.push({ key: "docs", label: "Docs", icon: "docs", count: docsCount });
            out.push({ key: "goals", label: "Objetivos", icon: "target", count: goalsCount });
            out.push({ key: "team", label: "Equipe", icon: "users", count: agents.length });
        }
        out.push({ key: "routines", label: "Rotinas", icon: "clock", count: routinesCount });
        out.push({ key: "tasks", label: "Gerenciador de tarefas", icon: "pulse", count: workingCount });
        out.push({ key: "teammemory", label: "Memória do time", icon: "bookmark", count: 0 });
        out.push({ key: "plugins", label: "Plugins", icon: "plug", count: pluginsCount });
        out.push({ key: "skills", label: "Skills", icon: "book", count: skillsCount });
        return out;
    }

    readonly property bool moreActive: moreTools.some(t => t.key === win.page)

    readonly property var channelIds: channels.map(c => c.id)
    readonly property var sectorIds: sectors.map(s => s.id)
    readonly property var pinnedAgents: agents.filter(a => !!a.pinned).slice(0, 5)
    readonly property var unsectoredAgents: agents.filter(a => !a.pinned && !sectorIds.includes(a.sector || ""))
    readonly property var visibleSectors: isPersonal
        ? sectors.filter(s => agents.some(a => !a.pinned && (a.sector || "") === s.id))
        : sectors
    readonly property var orderedAllAgents: pinnedAgents.concat(agents.filter(a => !a.pinned))

    readonly property string meDisplayName: mePerson ? (mePerson.name || mePerson.email || "Você") : "Operador"
    readonly property string mePhoto: mePerson ? personPhoto(mePerson.avatar) : ""
    readonly property string mePresenceDot: mePerson ? selfPresenceDot(mePerson.presence) : "live"
    readonly property string meStatusLine: mePerson
        ? (mePerson.status_text ? mePerson.status_text : presenceLabel(mePerson.presence))
        : "Conta e configurações"

    width: compact ? slim : full
    Behavior on width { NumberAnimation { duration: Theme.move; easing.type: Theme.ease } }
    color: Theme.panel

    function showTip(item, label) {
        if (!item || !label)
            return;
        const pt = item.mapToItem(sidebar, 0, item.height / 2);
        tipY = pt.y;
        tipText = label;
        tipShown = true;
    }

    function hideTip(label) {
        if (tipText === label)
            tipShown = false;
    }

    onCompactChanged: tipShown = false

    function isFolded(key) {
        return collapsed[key] === true;
    }

    function toggleFold(key) {
        const next = Object.assign({}, collapsed);
        if (next[key])
            delete next[key];
        else
            next[key] = true;
        collapsed = next;
        collapsedStore = JSON.stringify(next);
    }

    function presenceLabel(p) {
        return p === "away" ? "Ausente" : p === "busy" ? "Ocupado" : p === "invisible" ? "Invisível" : "Automático";
    }

    function selfPresenceDot(p) {
        return p === "away" ? "warn" : p === "busy" ? "bad" : p === "invisible" ? "" : "live";
    }

    function memberPresenceDot(m) {
        if (!m || !m.online || m.presence === "invisible")
            return "";
        if (m.presence === "busy")
            return "bad";
        if (m.presence === "away")
            return "warn";
        return "live";
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

    function bellSvg(col) {
        const h = v => ("0" + Math.round(v * 255).toString(16)).slice(-2);
        const hex = "#" + h(col.r) + h(col.g) + h(col.b);
        const a = col.a.toFixed(3);
        const body = '<path d="M6 8a6 6 0 0 1 12 0c0 7 3 9 3 9H3s3-2 3-9M10.3 21a1.94 1.94 0 0 0 3.4 0" fill="none" stroke="'
            + hex + '" stroke-opacity="' + a + '" stroke-width="2.1" stroke-linecap="round" stroke-linejoin="round"/>';
        return "image://svg/" + encodeURIComponent('<svg xmlns="http://www.w3.org/2000/svg" viewBox="0 0 24 24" width="24" height="24">' + body + "</svg>");
    }

    function isChannelActive(id) {
        return win.page === "chat" && !!win.chatState && win.chatState.kind === "channel" && win.chatState.id === id;
    }

    function isAgentActive(id) {
        return win.page === "chat" && !!win.chatState && win.chatState.kind === "agent" && win.chatState.id === id;
    }

    function isPersonActive(id) {
        return win.page === "chat" && !!win.chatState && win.chatState.kind === "person" && win.chatState.id === id;
    }

    function markSeenKey(key, stamp) {
        if (!key)
            return;
        const ts = stamp || new Date().toISOString();
        if (seen[key] !== ts) {
            const s = Object.assign({}, seen);
            s[key] = ts;
            seen = s;
            seenStore = JSON.stringify(s);
        }
        if (unreadCounts[key]) {
            const u = Object.assign({}, unreadCounts);
            delete u[key];
            unreadCounts = u;
        }
    }

    function syncActiveSeen() {
        if (win.page !== "chat" || !win.chatState)
            return;
        if (win.chatState.kind === "channel" && win.chatState.id) {
            const ch = channels.find(c => c.id === win.chatState.id);
            markSeenKey("#" + win.chatState.id, ch && ch.last_at ? ch.last_at : new Date().toISOString());
        } else if (win.chatState.kind === "agent" && win.chatState.id) {
            const ag = agents.find(a => a.id === win.chatState.id);
            markSeenKey(win.chatState.id, ag && ag.last_at ? ag.last_at : new Date().toISOString());
        }
    }

    function channelUnread(c) {
        if (!c || isChannelActive(c.id))
            return 0;
        const key = "#" + c.id;
        if (unreadCounts[key] > 0)
            return unreadCounts[key];
        if (c.last_at && c.preview !== "" && (!seen[key] || seen[key] < c.last_at))
            return 1;
        return 0;
    }

    function agentUnread(a) {
        if (!a || isAgentActive(a.id))
            return 0;
        if (unreadCounts[a.id] > 0)
            return unreadCounts[a.id];
        if (a.last_at && a.last_role === "assistant" && (!seen[a.id] || seen[a.id] < a.last_at))
            return 1;
        return 0;
    }

    function needsYou(agentId) {
        const list = (homeData && homeData.waiting) ? homeData.waiting : [];
        return list.some(w => w.agent_id === agentId);
    }

    function isAgentWorking(a) {
        if (!a)
            return false;
        if (a.status === "working" || !!a.working)
            return true;
        const list = (homeData && homeData.working) ? homeData.working : [];
        return list.some(w => w.agent_id === a.id);
    }

    function agentDotKind(a) {
        if (!a)
            return "";
        if (a.status === "waiting" || needsYou(a.id))
            return "warn";
        if (isAgentWorking(a))
            return "live";
        return "";
    }

    function agentTipText(a) {
        if (!a)
            return "";
        const dot = agentDotKind(a);
        const state = dot === "warn" ? "  ·  esperando você" : dot === "live" ? "  ·  trabalhando" : "";
        return (a.name || a.id) + state;
    }

    function loadHome() {
        if (!Api.signedIn)
            return;
        Api.get("/v1/home?peek=1", (err, data) => {
            if (!err && data && !U.same(sidebar.homeData, data))
                sidebar.homeData = data;
        });
    }

    function loadChannels() {
        if (!Api.signedIn)
            return;
        Api.get("/v1/channels", (err, list) => {
            if (err)
                return;
            const clean = list || [];
            if (!U.same(sidebar.channels, clean))
                sidebar.channels = clean;
            sidebar.syncActiveSeen();
        });
    }

    function loadAgents() {
        if (!Api.signedIn)
            return;
        Api.get("/v1/agents", (err, list) => {
            if (err)
                return;
            const clean = list || [];
            if (!U.same(sidebar.agents, clean))
                sidebar.agents = clean;
            sidebar.syncActiveSeen();
        });
    }

    function loadSectors() {
        if (!Api.signedIn)
            return;
        Api.get("/v1/sectors", (err, list) => {
            if (!err && !U.same(sidebar.sectors, list || []))
                sidebar.sectors = list || [];
        });
    }

    function loadMembers() {
        if (!Api.signedIn)
            return;
        const ws = currentWsId;
        if (ws === "")
            return;
        Api.get("/v1/workspaces/" + encodeURIComponent(ws) + "/members", (err, list) => {
            if (err || sidebar.currentWsId !== ws)
                return;
            if (!U.same(sidebar.members, list || []))
                sidebar.members = list || [];
        });
    }

    function loadCounts() {
        if (!Api.signedIn)
            return;
        Api.get("/v1/goals", (err, list) => {
            if (!err && list)
                sidebar.goalsCount = list.filter(g => !g.status || g.status === "ativo").length;
        });
        Api.get("/v1/routines", (err, list) => {
            if (!err && list)
                sidebar.routinesCount = list.filter(r => !!r.enabled).length;
        });
        Api.get("/v1/connectors", (err, list) => {
            if (!err && list)
                sidebar.pluginsCount = list.filter(c => (c.connections || []).length > 0).length;
        });
        Api.get("/v1/skills", (err, list) => {
            if (!err && list)
                sidebar.skillsCount = list.length;
        });
    }

    function reload() {
        if (!Api.signedIn)
            return;
        loadHome();
        loadChannels();
        loadSectors();
        loadAgents();
        loadMembers();
        loadCounts();
    }

    function togglePin(a) {
        if (!a)
            return;
        if (!a.pinned && pinnedAgents.length >= 5)
            return;
        const next = !a.pinned;
        agents = agents.map(x => x.id === a.id ? Object.assign({}, x, { pinned: next }) : x);
        Api.patch("/v1/agents/" + encodeURIComponent(a.id), { pinned: next }, () => loadAgents());
    }

    function moveAgentToSector(a, sectorId) {
        if (!a || (a.sector || "") === sectorId)
            return;
        agents = agents.map(x => x.id === a.id ? Object.assign({}, x, { sector: sectorId }) : x);
        if (sectorId !== "" && isFolded("sector:" + sectorId))
            toggleFold("sector:" + sectorId);
        Api.patch("/v1/agents/" + encodeURIComponent(a.id), { sector: sectorId }, () => loadAgents());
    }

    Component.onCompleted: {
        try {
            seen = JSON.parse(seenStore || "{}");
            collapsed = JSON.parse(collapsedStore || "{}");
        } catch (e) {
            seen = {};
            collapsed = {};
        }
        if (Api.signedIn)
            reload();
    }

    Connections {
        target: win
        function onPageChanged() { sidebar.syncActiveSeen(); }
        function onChatStateChanged() { sidebar.syncActiveSeen(); }
    }

    Connections {
        target: Api
        function onSignedInChanged() {
            if (Api.signedIn) {
                sidebar.reload();
            } else {
                sidebar.agents = [];
                sidebar.channels = [];
                sidebar.sectors = [];
                sidebar.members = [];
                sidebar.homeData = { waiting: [], working: [], next_routines: [] };
            }
        }
        function onWorkspaceChanged() {
            sidebar.unreadCounts = ({});
            sidebar.sectors = [];
            sidebar.members = [];
            if (Api.signedIn)
                sidebar.reload();
        }
        function onMeChanged() {
            if (Api.signedIn)
                sidebar.loadMembers();
        }
        function onLiveReset() {
            if (Api.signedIn)
                sidebar.reload();
        }
        function onLiveEvent(e) {
            if (!e || !Api.signedIn)
                return;
            const t = e.type || "";
            const p = e.payload || {};
            if (t === "message.created") {
                const conv = p.conversation_id || "";
                if (sidebar.channelIds.includes(conv)) {
                    if (!sidebar.isChannelActive(conv)) {
                        const u = Object.assign({}, sidebar.unreadCounts);
                        u["#" + conv] = (u["#" + conv] || 0) + 1;
                        sidebar.unreadCounts = u;
                    }
                    chanDebounce.restart();
                } else if (p.role === "assistant" && p.agent_id && !sidebar.isAgentActive(p.agent_id)) {
                    const u = Object.assign({}, sidebar.unreadCounts);
                    u[p.agent_id] = (u[p.agent_id] || 0) + 1;
                    sidebar.unreadCounts = u;
                }
                agentDebounce.restart();
                homeDebounce.restart();
            } else if (t === "channel.saved") {
                chanDebounce.restart();
            } else if (t === "sector.saved" || t === "sector.deleted") {
                sidebar.loadSectors();
            } else if (t.startsWith("approval.") || t.startsWith("mission.") || t.startsWith("job.")) {
                homeDebounce.restart();
                agentDebounce.restart();
            } else if (t.startsWith("agent.") || t.startsWith("conversation.")) {
                agentDebounce.restart();
            } else if (t.startsWith("routine.") || t.startsWith("goal.") || t.startsWith("connection.") || t.startsWith("skill.")) {
                homeDebounce.restart();
                countsDebounce.restart();
            } else if (t.startsWith("workspace.") || t.startsWith("member.") || t.startsWith("person.")) {
                memberDebounce.restart();
            }
        }
    }

    Timer { id: homeDebounce; interval: 220; onTriggered: sidebar.loadHome() }
    Timer { id: chanDebounce; interval: 300; onTriggered: sidebar.loadChannels() }
    Timer { id: agentDebounce; interval: 220; onTriggered: sidebar.loadAgents() }
    Timer { id: memberDebounce; interval: 300; onTriggered: sidebar.loadMembers() }
    Timer { id: countsDebounce; interval: 400; onTriggered: sidebar.loadCounts() }

    // Poll /v1/home (waiting.length) and team lists every 30 s.
    Timer {
        interval: 30000
        repeat: true
        running: sidebar.visible && Api.signedIn
        onTriggered: {
            sidebar.loadHome();
            sidebar.loadAgents();
            sidebar.loadChannels();
            sidebar.loadMembers();
        }
    }

    // Right border separating sidebar from the active page.
    Rectangle {
        anchors { top: parent.top; bottom: parent.bottom; right: parent.right }
        width: 1
        color: Theme.line
    }

    // Frameless window drag strip across the top of the sidebar.
    Item {
        width: parent.width
        height: Theme.titleBar
        DragHandler {
            target: null
            onActiveChanged: if (active && Window.window) Window.window.startSystemMove()
        }
    }

    // ── Reusable row components ─────────────────────────────────
    component ActiveBar: Rectangle {
        property bool on: false
        x: -Theme.space2
        width: 3
        height: on ? 18 : 0
        radius: width / 2
        anchors.verticalCenter: parent.verticalCenter
        color: Theme.white
        Behavior on height { NumberAnimation { duration: Theme.medium; easing.type: Theme.ease } }
    }

    component CountPill: Rectangle {
        id: cp
        property int count: 0
        property bool warn: false
        visible: count > 0
        width: Math.max(20, cpText.implicitWidth + Theme.space2 * 1.5)
        height: 18
        radius: height / 2
        color: warn ? Qt.rgba(Theme.warn.r, Theme.warn.g, Theme.warn.b, 0.18) : Theme.wash4
        border.width: warn ? 1 : 0
        border.color: Qt.rgba(Theme.warn.r, Theme.warn.g, Theme.warn.b, 0.40)

        Text {
            id: cpText
            anchors.centerIn: parent
            text: cp.count > 99 ? "99+" : String(cp.count)
            color: cp.warn ? Theme.warn : Theme.text2
            font.family: Theme.mono
            font.pixelSize: Theme.fontTiny
            font.weight: Font.DemiBold
        }
    }

    component ToolRow: Item {
        id: tr
        property string iconName: ""
        property string label: ""
        property int count: 0
        property bool warn: false
        property bool active: false
        signal clicked

        width: parent ? parent.width : 0
        height: 36

        Rectangle {
            anchors.fill: parent
            radius: Theme.radiusMd
            color: tr.active || trArea.pressed ? Theme.active : trArea.containsMouse ? Theme.hover : "transparent"
            border.width: tr.active ? 1 : 0
            border.color: Theme.lineStrong
            Behavior on color { ColorAnimation { duration: Theme.fast } }
        }

        ActiveBar { on: tr.active }

        Icon {
            id: trIcon
            x: sidebar.compact ? (tr.width - width) / 2 : Theme.space3
            anchors.verticalCenter: parent.verticalCenter
            name: tr.iconName
            size: 16
            color: tr.warn ? Theme.warn : tr.active ? Theme.white : trArea.containsMouse ? Theme.text : Theme.text3
            Behavior on x { NumberAnimation { duration: Theme.move; easing.type: Theme.ease } }
        }

        // Small corner dot when slim and counter > 0.
        Rectangle {
            visible: sidebar.compact && tr.count > 0
            width: 7
            height: 7
            radius: height / 2
            color: tr.warn ? Theme.warn : Theme.white
            anchors { right: trIcon.right; top: trIcon.top; margins: -2 }
        }

        Text {
            visible: !sidebar.compact
            anchors {
                left: trIcon.right
                leftMargin: Theme.space3
                right: trBadge.visible ? trBadge.left : parent.right
                rightMargin: Theme.space2
                verticalCenter: parent.verticalCenter
            }
            text: tr.label
            color: tr.warn ? Theme.warn : tr.active ? Theme.white : trArea.containsMouse ? Theme.text : Theme.text2
            font.pixelSize: Theme.fontBody
            font.weight: tr.active || tr.warn ? Font.DemiBold : Font.Medium
            elide: Text.ElideRight
        }

        CountPill {
            id: trBadge
            visible: !sidebar.compact && tr.count > 0
            anchors { right: parent.right; rightMargin: Theme.space2; verticalCenter: parent.verticalCenter }
            count: tr.count
            warn: tr.warn
        }

        MouseArea {
            id: trArea
            anchors.fill: parent
            hoverEnabled: true
            cursorShape: Qt.PointingHandCursor
            readonly property string tipStr: tr.label + (tr.count > 0 ? "  ·  " + tr.count : "")
            onContainsMouseChanged: {
                if (!sidebar.compact)
                    return;
                if (containsMouse)
                    sidebar.showTip(tr, tipStr);
                else
                    sidebar.hideTip(tipStr);
            }
            onClicked: tr.clicked()
        }
    }

    component AgentRow: Item {
        id: ar
        property var agentData: null
        readonly property var a: agentData || ({})
        readonly property bool active: sidebar.isAgentActive(a.id || "")
        readonly property string dotKind: sidebar.agentDotKind(a)
        readonly property int unread: sidebar.agentUnread(a)

        width: parent ? parent.width : 0
        height: 38

        Rectangle {
            anchors.fill: parent
            radius: Theme.radiusMd
            color: ar.active || arArea.pressed ? Theme.active : arArea.containsMouse ? Theme.hover : "transparent"
            border.width: ar.active ? 1 : 0
            border.color: Theme.lineStrong
            Behavior on color { ColorAnimation { duration: Theme.fast } }
        }

        ActiveBar { on: ar.active }

        Avatar {
            id: arFace
            x: Theme.space2
            anchors.verticalCenter: parent.verticalCenter
            size: 28
            name: ar.a.name || "?"
            photo: U.photo(ar.a)
            tint: ar.a.color || Theme.text2
            working: sidebar.isAgentWorking(ar.a)
        }

        Row {
            anchors {
                left: arFace.right
                leftMargin: Theme.space2
                right: arRight.left
                rightMargin: Theme.space2
                verticalCenter: parent.verticalCenter
            }
            spacing: Theme.space1

            Text {
                width: Math.min(implicitWidth, parent.width - (arPin.visible ? 16 : 0))
                text: ar.a.name || ""
                color: ar.active || ar.unread > 0 ? Theme.white : arArea.containsMouse ? Theme.text : Theme.text2
                font.pixelSize: Theme.fontBody
                font.weight: ar.active || ar.unread > 0 ? Font.DemiBold : Font.Medium
                elide: Text.ElideRight
                anchors.verticalCenter: parent.verticalCenter
            }

            Icon {
                id: arPin
                visible: !!ar.a.pinned
                name: "pin"
                size: 11
                color: Theme.text4
                anchors.verticalCenter: parent.verticalCenter
            }
        }

        Row {
            id: arRight
            anchors { right: parent.right; rightMargin: Theme.space2; verticalCenter: parent.verticalCenter }
            spacing: Theme.space2

            Dot {
                visible: ar.dotKind !== ""
                kind: ar.dotKind
                anchors.verticalCenter: parent.verticalCenter
            }

            CountPill {
                count: ar.unread
                warn: false
                anchors.verticalCenter: parent.verticalCenter
            }
        }

        MouseArea {
            id: arArea
            anchors.fill: parent
            hoverEnabled: true
            acceptedButtons: Qt.LeftButton | Qt.RightButton
            cursorShape: Qt.PointingHandCursor
            onClicked: mouse => {
                if (mouse.button === Qt.RightButton) {
                    agentMenu.agent = ar.a;
                    const pt = ar.mapToItem(sidebar, mouse.x, mouse.y);
                    agentMenu.x = Math.min(pt.x, sidebar.width - 20);
                    agentMenu.y = pt.y;
                    agentMenu.open();
                } else if (ar.a.id) {
                    sidebar.markSeenKey(ar.a.id, ar.a.last_at || new Date().toISOString());
                    win.openAgent(ar.a.id);
                }
            }
        }
    }

    // ── Fixed Top: Workspace card + Collapse + Ctrl+K ───────────
    Item {
        id: topBox
        anchors { top: parent.top; topMargin: Theme.space3; left: parent.left; right: parent.right; leftMargin: Theme.space3; rightMargin: Theme.space3 }
        height: sidebar.compact ? slimTopCol.implicitHeight : expandedTopCol.implicitHeight

        // Expanded header
        Column {
            id: expandedTopCol
            visible: !sidebar.compact
            width: parent.width
            spacing: Theme.space2

            Item {
                id: wsHeaderRow
                width: parent.width
                height: 38

                Rectangle {
                    id: wsCard
                    anchors { left: parent.left; right: toggleBtn.left; rightMargin: Theme.space1; top: parent.top; bottom: parent.bottom }
                    radius: Theme.radiusMd
                    color: sidebar.workspaceMenuOpen || wsArea.pressed ? Theme.active : wsArea.containsMouse ? Theme.hover : "transparent"
                    border.width: sidebar.workspaceMenuOpen ? 1 : 0
                    border.color: Theme.focusLine
                    Behavior on color { ColorAnimation { duration: Theme.fast } }

                    Rectangle {
                        id: wsBadge
                        x: Theme.space2
                        width: 26
                        height: 26
                        radius: Theme.radiusSm
                        anchors.verticalCenter: parent.verticalCenter
                        color: Theme.wash3
                        border.width: 1
                        border.color: Theme.lineStrong

                        Text {
                            anchors.centerIn: parent
                            text: sidebar.wsIcon !== "" ? sidebar.wsIcon : (sidebar.wsName || "A").charAt(0).toUpperCase()
                            color: Theme.white
                            font.family: Theme.display
                            font.pixelSize: Theme.fontSmall
                            font.weight: Font.DemiBold
                        }
                    }

                    Text {
                        id: wsLabel
                        anchors {
                            left: wsBadge.right
                            leftMargin: Theme.space2
                            right: wsChev.left
                            rightMargin: Theme.space1
                            verticalCenter: parent.verticalCenter
                        }
                        text: sidebar.wsName
                        color: Theme.white
                        font.pixelSize: Theme.fontLarge
                        font.weight: Font.DemiBold
                        elide: Text.ElideRight
                    }

                    Icon {
                        id: wsChev
                        anchors { right: parent.right; rightMargin: Theme.space2; verticalCenter: parent.verticalCenter }
                        name: "chevron"
                        size: 12
                        rotation: 90
                        color: Theme.text3
                    }

                    MouseArea {
                        id: wsArea
                        anchors.fill: parent
                        hoverEnabled: true
                        cursorShape: Qt.PointingHandCursor
                        onClicked: sidebar.workspaceClicked()
                    }
                }

                Rectangle {
                    id: toggleBtn
                    anchors { right: parent.right; verticalCenter: parent.verticalCenter }
                    width: 30
                    height: 30
                    radius: Theme.radiusSm
                    color: toggleArea.pressed ? Theme.active : toggleArea.containsMouse ? Theme.hover : "transparent"
                    border.width: toggleArea.containsMouse ? 1 : 0
                    border.color: Theme.line

                    Icon {
                        anchors.centerIn: parent
                        name: "sidebar"
                        size: 16
                        color: toggleArea.containsMouse ? Theme.white : Theme.text3
                    }
                    MouseArea {
                        id: toggleArea
                        anchors.fill: parent
                        hoverEnabled: true
                        cursorShape: Qt.PointingHandCursor
                        onClicked: win.compact = !win.compact
                    }
                    Tip {
                        text: "Recolher  ·  Ctrl+B"
                        show: toggleArea.containsMouse
                    }
                }
            }

            // "Ir para…  Ctrl+K" command button
            Rectangle {
                id: cmdBtn
                width: parent.width
                height: 32
                radius: Theme.radiusMd
                color: cmdArea.pressed ? Theme.active : cmdArea.containsMouse ? Theme.hover : Theme.wash1
                border.width: 1
                border.color: cmdArea.containsMouse ? Theme.lineStrong : Theme.line
                Behavior on color { ColorAnimation { duration: Theme.fast } }

                Icon {
                    id: cmdIcon
                    x: Theme.space3
                    anchors.verticalCenter: parent.verticalCenter
                    name: "search"
                    size: 14
                    color: cmdArea.containsMouse ? Theme.text : Theme.text3
                }

                Text {
                    anchors { left: cmdIcon.right; leftMargin: Theme.space2; right: cmdShortcut.left; rightMargin: Theme.space2; verticalCenter: parent.verticalCenter }
                    text: "Ir para…"
                    color: cmdArea.containsMouse ? Theme.text : Theme.text3
                    font.pixelSize: Theme.fontSmall
                    elide: Text.ElideRight
                }

                Rectangle {
                    id: cmdShortcut
                    anchors { right: parent.right; rightMargin: Theme.space2; verticalCenter: parent.verticalCenter }
                    width: cmdKeyText.implicitWidth + Theme.space2 * 1.5
                    height: 18
                    radius: Theme.radiusXs
                    color: Theme.wash2
                    border.width: 1
                    border.color: Theme.line

                    Text {
                        id: cmdKeyText
                        anchors.centerIn: parent
                        text: "Ctrl+K"
                        color: Theme.text3
                        font.family: Theme.mono
                        font.pixelSize: Theme.fontTiny
                    }
                }

                MouseArea {
                    id: cmdArea
                    anchors.fill: parent
                    hoverEnabled: true
                    cursorShape: Qt.PointingHandCursor
                    onClicked: win.openCommand()
                }
            }
        }

        // Slim header: workspace mark, expand toggle, command search icon
        Column {
            id: slimTopCol
            visible: sidebar.compact
            width: parent.width
            spacing: Theme.space2

            Rectangle {
                id: wsSlimBtn
                width: 38
                height: 38
                anchors.horizontalCenter: parent.horizontalCenter
                radius: Theme.radiusMd
                color: sidebar.workspaceMenuOpen || wsSlimArea.pressed ? Theme.active : wsSlimArea.containsMouse ? Theme.hover : Theme.wash2
                border.width: 1
                border.color: sidebar.workspaceMenuOpen ? Theme.focusLine : Theme.lineStrong

                Text {
                    anchors.centerIn: parent
                    text: sidebar.wsIcon !== "" ? sidebar.wsIcon : (sidebar.wsName || "A").charAt(0).toUpperCase()
                    color: Theme.white
                    font.family: Theme.display
                    font.pixelSize: Theme.fontTitle
                    font.weight: Font.DemiBold
                }

                MouseArea {
                    id: wsSlimArea
                    anchors.fill: parent
                    hoverEnabled: true
                    cursorShape: Qt.PointingHandCursor
                    onClicked: sidebar.workspaceClicked()
                }
                Tip {
                    text: sidebar.wsName
                    show: wsSlimArea.containsMouse && !sidebar.workspaceMenuOpen
                }
            }

            Rectangle {
                width: 34
                height: 32
                anchors.horizontalCenter: parent.horizontalCenter
                radius: Theme.radiusSm
                color: slimToggleArea.pressed ? Theme.active : slimToggleArea.containsMouse ? Theme.hover : "transparent"

                Icon {
                    anchors.centerIn: parent
                    name: "sidebar"
                    size: 16
                    color: slimToggleArea.containsMouse ? Theme.white : Theme.text3
                }
                MouseArea {
                    id: slimToggleArea
                    anchors.fill: parent
                    hoverEnabled: true
                    cursorShape: Qt.PointingHandCursor
                    onClicked: win.compact = false
                }
                Tip {
                    text: "Expandir  ·  Ctrl+B"
                    show: slimToggleArea.containsMouse
                }
            }

            Rectangle {
                width: 34
                height: 32
                anchors.horizontalCenter: parent.horizontalCenter
                radius: Theme.radiusSm
                color: slimCmdArea.pressed ? Theme.active : slimCmdArea.containsMouse ? Theme.hover : Theme.wash1
                border.width: 1
                border.color: Theme.line

                Icon {
                    anchors.centerIn: parent
                    name: "search"
                    size: 15
                    color: slimCmdArea.containsMouse ? Theme.white : Theme.text3
                }
                MouseArea {
                    id: slimCmdArea
                    anchors.fill: parent
                    hoverEnabled: true
                    cursorShape: Qt.PointingHandCursor
                    onClicked: win.openCommand()
                }
                Tip {
                    text: "Ir para…  ·  Ctrl+K"
                    show: slimCmdArea.containsMouse
                }
            }
        }
    }

    // ── Scrollable Middle ───────────────────────────────────────
    Flickable {
        id: scroller
        anchors {
            top: topBox.bottom
            topMargin: Theme.space3
            bottom: footerBox.top
            bottomMargin: Theme.space2
            left: parent.left
            right: parent.right
        }
        contentHeight: sidebar.compact ? slimBodyCol.implicitHeight + Theme.space3 : fullBodyCol.implicitHeight + Theme.space4
        clip: true
        boundsBehavior: Flickable.StopAtBounds
        ScrollBar.vertical: SlimScrollBar {}
        SmoothWheel {}

        // ── Expanded middle list ────────────────────────────────
        Column {
            id: fullBodyCol
            visible: !sidebar.compact
            x: Theme.space3
            width: scroller.width - 2 * Theme.space3
            spacing: Theme.space3

            // 1. Tools + "Mais"
            Column {
                width: parent.width
                spacing: 2

                Repeater {
                    model: sidebar.directTools
                    delegate: ToolRow {
                        required property var modelData
                        iconName: modelData.icon
                        label: modelData.label
                        count: modelData.count
                        warn: !!modelData.warn
                        active: win.page === modelData.key
                        onClicked: win.go(modelData.key)
                    }
                }

                ToolRow {
                    id: moreRowFull
                    iconName: "more"
                    label: "Mais"
                    count: sidebar.workingCount
                    active: morePopup.opened || sidebar.moreActive
                    onClicked: {
                        if (morePopup.opened) {
                            morePopup.close();
                        } else {
                        const popH = morePopup.implicitHeight > 0 ? morePopup.implicitHeight : 320;
                        const pt = moreRowFull.mapToItem(sidebar, 0, moreRowFull.height + Theme.space1);
                        morePopup.x = Theme.space3;
                        morePopup.y = Math.max(Theme.space3, Math.min(pt.y, sidebar.height - popH - Theme.space4));
                        morePopup.open();
                        }
                    }
                }
            }

            // 2. CANAIS (hidden in personal mode)
            Column {
                visible: !sidebar.isPersonal
                width: parent.width
                spacing: 2

                Item {
                    width: parent.width
                    height: 28

                    Rectangle {
                        anchors.fill: parent
                        radius: Theme.radiusSm
                        color: chanHeadArea.containsMouse ? Theme.hover : "transparent"
                    }

                    Icon {
                        id: chanChev
                        x: Theme.space1
                        anchors.verticalCenter: parent.verticalCenter
                        name: "chevron"
                        size: 12
                        color: Theme.text4
                        rotation: sidebar.isFolded("channels:") ? 0 : 90
                        Behavior on rotation { NumberAnimation { duration: Theme.fast } }
                    }

                    Overline {
                        anchors { left: chanChev.right; leftMargin: Theme.space2; verticalCenter: parent.verticalCenter }
                        text: "CANAIS"
                    }

                    MouseArea {
                        id: chanHeadArea
                        anchors { left: parent.left; top: parent.top; bottom: parent.bottom; right: addChanBtn.left }
                        hoverEnabled: true
                        cursorShape: Qt.PointingHandCursor
                        onClicked: sidebar.toggleFold("channels:")
                    }

                    RowTool {
                        id: addChanBtn
                        anchors { right: parent.right; rightMargin: Theme.space1; verticalCenter: parent.verticalCenter }
                        icon: "plus"
                        tip: "Novo canal"
                        onHit: {
                            if (typeof win.newChannel === "function")
                                win.newChannel();
                            else
                                win.go("chat");
                        }
                    }
                }

                Repeater {
                    model: sidebar.isFolded("channels:") ? [] : sidebar.channels
                    delegate: Item {
                        id: chanRow
                        required property var modelData
                        readonly property bool active: sidebar.isChannelActive(modelData.id)
                        readonly property int unread: sidebar.channelUnread(modelData)

                        width: fullBodyCol.width
                        height: 34

                        Rectangle {
                            anchors.fill: parent
                            radius: Theme.radiusMd
                            color: chanRow.active || chanRowArea.pressed ? Theme.active : chanRowArea.containsMouse ? Theme.hover : "transparent"
                            border.width: chanRow.active ? 1 : 0
                            border.color: Theme.lineStrong
                            Behavior on color { ColorAnimation { duration: Theme.fast } }
                        }

                        ActiveBar { on: chanRow.active }

                        Icon {
                            id: chanHash
                            x: Theme.space3
                            anchors.verticalCenter: parent.verticalCenter
                            name: "hash"
                            size: 15
                            color: chanRow.active || chanRow.unread > 0 ? Theme.white : Theme.text3
                        }

                        Text {
                            anchors {
                                left: chanHash.right
                                leftMargin: Theme.space2
                                right: chanCount.visible ? chanCount.left : parent.right
                                rightMargin: Theme.space2
                                verticalCenter: parent.verticalCenter
                            }
                            text: chanRow.modelData.name || chanRow.modelData.id
                            color: chanRow.active || chanRow.unread > 0 ? Theme.white : chanRowArea.containsMouse ? Theme.text : Theme.text2
                            font.pixelSize: Theme.fontBody
                            font.weight: chanRow.active || chanRow.unread > 0 ? Font.DemiBold : Font.Normal
                            elide: Text.ElideRight
                        }

                        CountPill {
                            id: chanCount
                            anchors { right: parent.right; rightMargin: Theme.space2; verticalCenter: parent.verticalCenter }
                            count: chanRow.unread
                        }

                        MouseArea {
                            id: chanRowArea
                            anchors.fill: parent
                            hoverEnabled: true
                            cursorShape: Qt.PointingHandCursor
                            onClicked: {
                                sidebar.markSeenKey("#" + chanRow.modelData.id, chanRow.modelData.last_at || new Date().toISOString());
                                win.openChannel(chanRow.modelData.id);
                            }
                        }
                    }
                }

                Text {
                    visible: !sidebar.isFolded("channels:") && sidebar.channels.length === 0
                    leftPadding: Theme.space3
                    topPadding: Theme.space1
                    bottomPadding: Theme.space1
                    text: "Nenhum canal criado ainda."
                    color: Theme.text4
                    font.pixelSize: Theme.fontSmall
                }
            }

            // Divider before agents in personal mode (since headers are omitted).
            Rectangle {
                visible: sidebar.isPersonal
                width: parent.width - 2 * Theme.space2
                height: 1
                anchors.horizontalCenter: parent.horizontalCenter
                color: Theme.line
            }

            // 3. AGENTES (pinned at top, unsectored first, then collapsible sectors, + Novo agente)
            Column {
                width: parent.width
                spacing: 2

                Item {
                    visible: !sidebar.isPersonal
                    width: parent.width
                    height: 28

                    Rectangle {
                        anchors.fill: parent
                        radius: Theme.radiusSm
                        color: agentsHeadArea.containsMouse ? Theme.hover : "transparent"
                    }

                    Icon {
                        id: agentsChev
                        x: Theme.space1
                        anchors.verticalCenter: parent.verticalCenter
                        name: "chevron"
                        size: 12
                        color: Theme.text4
                        rotation: sidebar.isFolded("agents:") ? 0 : 90
                        Behavior on rotation { NumberAnimation { duration: Theme.fast } }
                    }

                    Overline {
                        anchors { left: agentsChev.right; leftMargin: Theme.space2; verticalCenter: parent.verticalCenter }
                        text: "AGENTES"
                    }

                    MouseArea {
                        id: agentsHeadArea
                        anchors.fill: parent
                        hoverEnabled: true
                        cursorShape: Qt.PointingHandCursor
                        onClicked: sidebar.toggleFold("agents:")
                    }
                }

                Column {
                    visible: sidebar.isPersonal || !sidebar.isFolded("agents:")
                    width: parent.width
                    spacing: 2

                    // Pinned agents at top
                    Repeater {
                        model: sidebar.pinnedAgents
                        delegate: AgentRow {
                            required property var modelData
                            agentData: modelData
                        }
                    }

                    // Agents without sector first
                    Repeater {
                        model: sidebar.unsectoredAgents
                        delegate: AgentRow {
                            required property var modelData
                            agentData: modelData
                        }
                    }

                    // Collapsible sector groups
                    Repeater {
                        model: sidebar.visibleSectors
                        delegate: Column {
                            id: secGroup
                            required property var modelData
                            readonly property string foldKey: "sector:" + modelData.id
                            readonly property bool folded: sidebar.isFolded(foldKey)
                            readonly property var secAgents: sidebar.agents.filter(a => !a.pinned && (a.sector || "") === modelData.id)

                            width: fullBodyCol.width
                            spacing: 2

                            Item {
                                width: parent.width
                                height: 30

                                Rectangle {
                                    anchors.fill: parent
                                    radius: Theme.radiusSm
                                    color: secHeadArea.containsMouse ? Theme.hover : "transparent"
                                }

                                Icon {
                                    id: secChev
                                    x: Theme.space1
                                    anchors.verticalCenter: parent.verticalCenter
                                    name: "chevron"
                                    size: 12
                                    color: Theme.text4
                                    rotation: secGroup.folded ? 0 : 90
                                    Behavior on rotation { NumberAnimation { duration: Theme.fast } }
                                }

                                Icon {
                                    id: secIcon
                                    anchors { left: secChev.right; leftMargin: Theme.space2; verticalCenter: parent.verticalCenter }
                                    name: secGroup.modelData.icon || "users"
                                    size: 14
                                    color: secGroup.modelData.color || Theme.text2
                                }

                                Text {
                                    anchors {
                                        left: secIcon.right
                                        leftMargin: Theme.space2
                                        right: secCount.left
                                        rightMargin: Theme.space2
                                        verticalCenter: parent.verticalCenter
                                    }
                                    text: secGroup.modelData.name || "Setor"
                                    color: Theme.text2
                                    font.pixelSize: Theme.fontSmall
                                    font.weight: Font.DemiBold
                                    elide: Text.ElideRight
                                }

                                Text {
                                    id: secCount
                                    anchors { right: parent.right; rightMargin: Theme.space2; verticalCenter: parent.verticalCenter }
                                    text: String(secGroup.secAgents.length)
                                    color: Theme.text4
                                    font.family: Theme.mono
                                    font.pixelSize: Theme.fontTiny
                                }

                                MouseArea {
                                    id: secHeadArea
                                    anchors.fill: parent
                                    hoverEnabled: true
                                    cursorShape: Qt.PointingHandCursor
                                    onClicked: sidebar.toggleFold(secGroup.foldKey)
                                }
                            }

                            Repeater {
                                model: secGroup.folded ? [] : secGroup.secAgents
                                delegate: AgentRow {
                                    required property var modelData
                                    agentData: modelData
                                }
                            }

                            Text {
                                visible: !secGroup.folded && secGroup.secAgents.length === 0
                                leftPadding: Theme.space5
                                topPadding: 2
                                bottomPadding: Theme.space1
                                text: "Sem agentes neste setor"
                                color: Theme.text4
                                font.pixelSize: Theme.fontSmall
                            }
                        }
                    }

                    Text {
                        visible: sidebar.agents.length === 0
                        leftPadding: Theme.space3
                        topPadding: Theme.space1
                        bottomPadding: Theme.space1
                        text: "Ainda não há agentes neste espaço."
                        color: Theme.text4
                        font.pixelSize: Theme.fontSmall
                    }

                    // "+ Novo agente"
                    Rectangle {
                        id: newAgentRow
                        width: parent.width
                        height: 36
                        radius: Theme.radiusMd
                        color: newAgentArea.pressed ? Theme.active : newAgentArea.containsMouse ? Theme.hover : "transparent"
                        Behavior on color { ColorAnimation { duration: Theme.fast } }

                        Icon {
                            id: newAgentIcon
                            x: Theme.space3
                            anchors.verticalCenter: parent.verticalCenter
                            name: "plus"
                            size: 15
                            color: newAgentArea.containsMouse ? Theme.white : Theme.text3
                        }

                        Text {
                            anchors { left: newAgentIcon.right; leftMargin: Theme.space3; right: parent.right; rightMargin: Theme.space2; verticalCenter: parent.verticalCenter }
                            text: "Novo agente"
                            color: newAgentArea.containsMouse ? Theme.white : Theme.text3
                            font.pixelSize: Theme.fontBody
                            font.weight: Font.Medium
                            elide: Text.ElideRight
                        }

                        MouseArea {
                            id: newAgentArea
                            anchors.fill: parent
                            hoverEnabled: true
                            cursorShape: Qt.PointingHandCursor
                            onClicked: win.newAgent()
                        }
                    }
                }
            }

            // 4. MENSAGENS DIRETAS (hidden when there are no other people)
            Column {
                visible: sidebar.otherMembers.length > 0
                width: parent.width
                spacing: 2

                Item {
                    width: parent.width
                    height: 28

                    Rectangle {
                        anchors.fill: parent
                        radius: Theme.radiusSm
                        color: dmHeadArea.containsMouse ? Theme.hover : "transparent"
                    }

                    Icon {
                        id: dmChev
                        x: Theme.space1
                        anchors.verticalCenter: parent.verticalCenter
                        name: "chevron"
                        size: 12
                        color: Theme.text4
                        rotation: sidebar.isFolded("dms:") ? 0 : 90
                        Behavior on rotation { NumberAnimation { duration: Theme.fast } }
                    }

                    Overline {
                        anchors { left: dmChev.right; leftMargin: Theme.space2; verticalCenter: parent.verticalCenter }
                        text: "MENSAGENS DIRETAS"
                    }

                    MouseArea {
                        id: dmHeadArea
                        anchors.fill: parent
                        hoverEnabled: true
                        cursorShape: Qt.PointingHandCursor
                        onClicked: sidebar.toggleFold("dms:")
                    }
                }

                Repeater {
                    model: sidebar.isFolded("dms:") ? [] : sidebar.otherMembers
                    delegate: Item {
                        id: dmRow
                        required property var modelData
                        readonly property bool active: sidebar.isPersonActive(modelData.person_id)
                        readonly property string pDot: sidebar.memberPresenceDot(modelData)

                        width: fullBodyCol.width
                        height: 38

                        Rectangle {
                            anchors.fill: parent
                            radius: Theme.radiusMd
                            color: dmRow.active || dmArea.pressed ? Theme.active : dmArea.containsMouse ? Theme.hover : "transparent"
                            border.width: dmRow.active ? 1 : 0
                            border.color: Theme.lineStrong
                            Behavior on color { ColorAnimation { duration: Theme.fast } }
                        }

                        ActiveBar { on: dmRow.active }

                        Item {
                            id: dmAv
                            x: Theme.space2
                            width: 28
                            height: 28
                            anchors.verticalCenter: parent.verticalCenter

                            Avatar {
                                anchors.fill: parent
                                size: 28
                                name: dmRow.modelData.name || dmRow.modelData.email || "?"
                                photo: sidebar.personPhoto(dmRow.modelData.avatar)
                                tint: sidebar.tintFor(dmRow.modelData.person_id || dmRow.modelData.email)
                            }

                            Rectangle {
                                visible: dmRow.pDot !== ""
                                width: 9
                                height: 9
                                radius: height / 2
                                color: Theme.card
                                anchors { right: parent.right; bottom: parent.bottom }

                                Dot {
                                    anchors.centerIn: parent
                                    kind: dmRow.pDot
                                }
                            }
                        }

                        Text {
                            anchors {
                                left: dmAv.right
                                leftMargin: Theme.space2
                                right: parent.right
                                rightMargin: Theme.space2
                                verticalCenter: parent.verticalCenter
                            }
                            text: dmRow.modelData.name || dmRow.modelData.email || "Pessoa"
                            color: dmRow.active ? Theme.white : dmArea.containsMouse ? Theme.text : Theme.text2
                            font.pixelSize: Theme.fontBody
                            font.weight: dmRow.active ? Font.DemiBold : Font.Medium
                            elide: Text.ElideRight
                        }

                        MouseArea {
                            id: dmArea
                            anchors.fill: parent
                            hoverEnabled: true
                            cursorShape: Qt.PointingHandCursor
                            onClicked: win.openPerson(dmRow.modelData.person_id)
                        }
                    }
                }
            }
        }

        // ── Slim middle rail: icons + agent faces ───────────────
        Column {
            id: slimBodyCol
            visible: sidebar.compact
            x: Theme.space3
            width: scroller.width - 2 * Theme.space3
            spacing: Theme.space2

            Column {
                width: parent.width
                spacing: 2

                Repeater {
                    model: sidebar.directTools
                    delegate: ToolRow {
                        required property var modelData
                        iconName: modelData.icon
                        label: modelData.label
                        count: modelData.count
                        warn: !!modelData.warn
                        active: win.page === modelData.key
                        onClicked: win.go(modelData.key)
                    }
                }

                ToolRow {
                    id: moreRowSlim
                    iconName: "more"
                    label: "Mais"
                    count: sidebar.workingCount
                    active: morePopup.opened || sidebar.moreActive
                    onClicked: {
                        if (morePopup.opened) {
                            morePopup.close();
                        } else {
                        const popH = morePopup.implicitHeight > 0 ? morePopup.implicitHeight : 320;
                        const pt = moreRowSlim.mapToItem(sidebar, sidebar.width, 0);
                        morePopup.x = sidebar.width + Theme.space2;
                        morePopup.y = Math.max(Theme.space3, Math.min(pt.y, sidebar.height - popH - Theme.space4));
                        morePopup.open();
                        }
                    }
                }
            }

            Rectangle {
                width: 28
                height: 1
                anchors.horizontalCenter: parent.horizontalCenter
                color: Theme.line
            }

            // Agent faces in slim mode
            Column {
                width: parent.width
                spacing: Theme.space1

                Repeater {
                    model: sidebar.orderedAllAgents
                    delegate: Item {
                        id: slimAg
                        required property var modelData
                        readonly property bool active: sidebar.isAgentActive(modelData.id)
                        readonly property string dotKind: sidebar.agentDotKind(modelData)
                        readonly property int unread: sidebar.agentUnread(modelData)
                        readonly property string tipStr: sidebar.agentTipText(modelData)

                        width: slimBodyCol.width
                        height: 40

                        Rectangle {
                            anchors.centerIn: parent
                            width: 38
                            height: 38
                            radius: Theme.radiusMd
                            color: slimAg.active || slimAgArea.pressed ? Theme.active : slimAgArea.containsMouse ? Theme.hover : "transparent"
                            border.width: slimAg.active ? 1 : 0
                            border.color: Theme.lineStrong
                        }

                        ActiveBar { on: slimAg.active }

                        Item {
                            width: 32
                            height: 32
                            anchors.centerIn: parent

                            Avatar {
                                anchors.fill: parent
                                size: 32
                                name: slimAg.modelData.name || "?"
                                photo: U.photo(slimAg.modelData)
                                tint: slimAg.modelData.color || Theme.text2
                                working: sidebar.isAgentWorking(slimAg.modelData)
                            }

                            Rectangle {
                                visible: slimAg.dotKind !== "" || slimAg.unread > 0
                                width: 9
                                height: 9
                                radius: height / 2
                                color: Theme.card
                                anchors { right: parent.right; bottom: parent.bottom }

                                Dot {
                                    visible: slimAg.dotKind !== ""
                                    anchors.centerIn: parent
                                    kind: slimAg.dotKind
                                }

                                Rectangle {
                                    visible: slimAg.dotKind === "" && slimAg.unread > 0
                                    anchors.centerIn: parent
                                    width: 6
                                    height: 6
                                    radius: height / 2
                                    color: Theme.white
                                }
                            }
                        }

                        MouseArea {
                            id: slimAgArea
                            anchors.fill: parent
                            hoverEnabled: true
                            cursorShape: Qt.PointingHandCursor
                            onContainsMouseChanged: {
                                if (containsMouse)
                                    sidebar.showTip(slimAg, slimAg.tipStr);
                                else
                                    sidebar.hideTip(slimAg.tipStr);
                            }
                            onClicked: {
                                sidebar.markSeenKey(slimAg.modelData.id, slimAg.modelData.last_at || new Date().toISOString());
                                win.openAgent(slimAg.modelData.id);
                            }
                        }
                    }
                }

                // "+" new agent in slim rail
                Item {
                    id: slimNewAg
                    width: parent.width
                    height: 36

                    Rectangle {
                        width: 34
                        height: 34
                        anchors.centerIn: parent
                        radius: Theme.radiusMd
                        color: slimNewArea.pressed ? Theme.active : slimNewArea.containsMouse ? Theme.hover : "transparent"
                        border.width: 1
                        border.color: slimNewArea.containsMouse ? Theme.lineStrong : Theme.line

                        Icon {
                            anchors.centerIn: parent
                            name: "plus"
                            size: 16
                            color: slimNewArea.containsMouse ? Theme.white : Theme.text3
                        }
                    }

                    MouseArea {
                        id: slimNewArea
                        anchors.fill: parent
                        hoverEnabled: true
                        cursorShape: Qt.PointingHandCursor
                        onContainsMouseChanged: {
                            if (containsMouse)
                                sidebar.showTip(slimNewAg, "Novo agente");
                            else
                                sidebar.hideTip("Novo agente");
                        }
                        onClicked: win.newAgent()
                    }
                }
            }

            // People faces in slim mode when there are other members
            Rectangle {
                visible: sidebar.otherMembers.length > 0
                width: 28
                height: 1
                anchors.horizontalCenter: parent.horizontalCenter
                color: Theme.line
            }

            Column {
                visible: sidebar.otherMembers.length > 0
                width: parent.width
                spacing: Theme.space1

                Repeater {
                    model: sidebar.otherMembers
                    delegate: Item {
                        id: slimDm
                        required property var modelData
                        readonly property bool active: sidebar.isPersonActive(modelData.person_id)
                        readonly property string pDot: sidebar.memberPresenceDot(modelData)
                        readonly property string labelStr: modelData.name || modelData.email || "Pessoa"

                        width: slimBodyCol.width
                        height: 40

                        Rectangle {
                            anchors.centerIn: parent
                            width: 38
                            height: 38
                            radius: Theme.radiusMd
                            color: slimDm.active || slimDmArea.pressed ? Theme.active : slimDmArea.containsMouse ? Theme.hover : "transparent"
                            border.width: slimDm.active ? 1 : 0
                            border.color: Theme.lineStrong
                        }

                        ActiveBar { on: slimDm.active }

                        Item {
                            width: 32
                            height: 32
                            anchors.centerIn: parent

                            Avatar {
                                anchors.fill: parent
                                size: 32
                                name: slimDm.labelStr
                                photo: sidebar.personPhoto(slimDm.modelData.avatar)
                                tint: sidebar.tintFor(slimDm.modelData.person_id || slimDm.modelData.email)
                            }

                            Rectangle {
                                visible: slimDm.pDot !== ""
                                width: 9
                                height: 9
                                radius: height / 2
                                color: Theme.card
                                anchors { right: parent.right; bottom: parent.bottom }

                                Dot {
                                    anchors.centerIn: parent
                                    kind: slimDm.pDot
                                }
                            }
                        }

                        MouseArea {
                            id: slimDmArea
                            anchors.fill: parent
                            hoverEnabled: true
                            cursorShape: Qt.PointingHandCursor
                            onContainsMouseChanged: {
                                if (containsMouse)
                                    sidebar.showTip(slimDm, slimDm.labelStr);
                                else
                                    sidebar.hideTip(slimDm.labelStr);
                            }
                            onClicked: win.openPerson(slimDm.modelData.person_id)
                        }
                    }
                }
            }
        }
    }

    // ── Fixed Footer: You (AccountMenu) + Notification Bell ─────
    Item {
        id: footerBox
        anchors { bottom: parent.bottom; bottomMargin: Theme.space3; left: parent.left; right: parent.right; leftMargin: Theme.space3; rightMargin: Theme.space3 }
        height: sidebar.compact ? slimFooterCol.implicitHeight + Theme.space2 : 52

        Rectangle {
            anchors { top: parent.top; left: parent.left; right: parent.right }
            height: 1
            color: Theme.line
        }

        // Expanded footer
        Item {
            visible: !sidebar.compact
            anchors { top: parent.top; topMargin: Theme.space2; left: parent.left; right: parent.right; bottom: parent.bottom }

            Rectangle {
                id: meBtn
                anchors { left: parent.left; right: bellBtn.left; rightMargin: Theme.space2; top: parent.top; bottom: parent.bottom }
                radius: Theme.radiusMd
                color: sidebar.accountMenuOpen || meArea.pressed ? Theme.active : meArea.containsMouse ? Theme.hover : "transparent"
                border.width: sidebar.accountMenuOpen ? 1 : 0
                border.color: Theme.focusLine
                Behavior on color { ColorAnimation { duration: Theme.fast } }

                Item {
                    id: meAvBox
                    x: Theme.space2
                    width: 30
                    height: 30
                    anchors.verticalCenter: parent.verticalCenter

                    Avatar {
                        anchors.fill: parent
                        size: 30
                        name: sidebar.meDisplayName
                        photo: sidebar.mePhoto
                        tint: Theme.palette[0]
                    }

                    Rectangle {
                        visible: sidebar.mePresenceDot !== ""
                        width: 10
                        height: 10
                        radius: height / 2
                        color: Theme.card
                        anchors { right: parent.right; bottom: parent.bottom }

                        Dot {
                            anchors.centerIn: parent
                            kind: sidebar.mePresenceDot
                        }
                    }
                }

                Column {
                    anchors {
                        left: meAvBox.right
                        leftMargin: Theme.space2
                        right: parent.right
                        rightMargin: Theme.space2
                        verticalCenter: parent.verticalCenter
                    }
                    spacing: 0

                    Text {
                        width: parent.width
                        text: sidebar.meDisplayName
                        color: Theme.white
                        font.pixelSize: Theme.fontBody
                        font.weight: Font.DemiBold
                        elide: Text.ElideRight
                    }

                    Text {
                        width: parent.width
                        text: sidebar.meStatusLine
                        color: Theme.text3
                        font.pixelSize: Theme.fontCaption
                        elide: Text.ElideRight
                    }
                }

                MouseArea {
                    id: meArea
                    anchors.fill: parent
                    hoverEnabled: true
                    cursorShape: Qt.PointingHandCursor
                    onClicked: sidebar.accountClicked()
                }
            }

            Rectangle {
                id: bellBtn
                anchors { right: parent.right; verticalCenter: parent.verticalCenter }
                width: 38
                height: 38
                radius: Theme.radiusMd
                color: win.page === "waiting" || bellArea.pressed ? Theme.active : bellArea.containsMouse ? Theme.hover : "transparent"
                border.width: win.page === "waiting" ? 1 : 0
                border.color: Theme.lineStrong
                Behavior on color { ColorAnimation { duration: Theme.fast } }

                Image {
                    anchors.centerIn: parent
                    width: 17
                    height: 17
                    source: sidebar.bellSvg(win.page === "waiting" || sidebar.waitingCount > 0 || bellArea.containsMouse ? Theme.white : Theme.text3)
                    sourceSize: Qt.size(Math.ceil(17 * Screen.devicePixelRatio), Math.ceil(17 * Screen.devicePixelRatio))
                    smooth: true
                    cache: true
                }

                Rectangle {
                    visible: sidebar.waitingCount > 0
                    anchors { right: parent.right; top: parent.top; margins: 3 }
                    width: Math.max(15, bellCountText.implicitWidth + 6)
                    height: 15
                    radius: height / 2
                    color: Theme.warn

                    Text {
                        id: bellCountText
                        anchors.centerIn: parent
                        text: sidebar.waitingCount > 99 ? "99" : String(sidebar.waitingCount)
                        color: Theme.ink
                        font.family: Theme.mono
                        font.pixelSize: Theme.fontTiny
                        font.weight: Font.Bold
                    }
                }

                MouseArea {
                    id: bellArea
                    anchors.fill: parent
                    hoverEnabled: true
                    cursorShape: Qt.PointingHandCursor
                    onClicked: win.go("waiting")
                }

                Tip {
                    text: sidebar.waitingCount > 0 ? "Esperando você (" + sidebar.waitingCount + ")" : "Notificações"
                    show: bellArea.containsMouse
                }
            }
        }

        // Slim footer: bell + your face
        Column {
            id: slimFooterCol
            visible: sidebar.compact
            anchors { top: parent.top; topMargin: Theme.space2; left: parent.left; right: parent.right }
            spacing: Theme.space1

            Rectangle {
                id: bellSlimBtn
                width: 36
                height: 36
                anchors.horizontalCenter: parent.horizontalCenter
                radius: Theme.radiusMd
                color: win.page === "waiting" || bellSlimArea.pressed ? Theme.active : bellSlimArea.containsMouse ? Theme.hover : "transparent"

                Image {
                    anchors.centerIn: parent
                    width: 17
                    height: 17
                    source: sidebar.bellSvg(win.page === "waiting" || sidebar.waitingCount > 0 || bellSlimArea.containsMouse ? Theme.white : Theme.text3)
                    sourceSize: Qt.size(Math.ceil(17 * Screen.devicePixelRatio), Math.ceil(17 * Screen.devicePixelRatio))
                    smooth: true
                    cache: true
                }

                Rectangle {
                    visible: sidebar.waitingCount > 0
                    anchors { right: parent.right; top: parent.top; margins: 3 }
                    width: Math.max(14, bellSlimCount.implicitWidth + 6)
                    height: 14
                    radius: height / 2
                    color: Theme.warn

                    Text {
                        id: bellSlimCount
                        anchors.centerIn: parent
                        text: sidebar.waitingCount > 9 ? "9+" : String(sidebar.waitingCount)
                        color: Theme.ink
                        font.family: Theme.mono
                        font.pixelSize: Theme.fontTiny
                        font.weight: Font.Bold
                    }
                }

                MouseArea {
                    id: bellSlimArea
                    anchors.fill: parent
                    hoverEnabled: true
                    cursorShape: Qt.PointingHandCursor
                    onClicked: win.go("waiting")
                }

                Tip {
                    text: sidebar.waitingCount > 0 ? "Esperando você (" + sidebar.waitingCount + ")" : "Notificações"
                    show: bellSlimArea.containsMouse
                }
            }

            Rectangle {
                id: meSlimBtn
                width: 38
                height: 38
                anchors.horizontalCenter: parent.horizontalCenter
                radius: Theme.radiusMd
                color: sidebar.accountMenuOpen || meSlimArea.pressed ? Theme.active : meSlimArea.containsMouse ? Theme.hover : "transparent"
                border.width: sidebar.accountMenuOpen ? 1 : 0
                border.color: Theme.focusLine

                Item {
                    width: 32
                    height: 32
                    anchors.centerIn: parent

                    Avatar {
                        anchors.fill: parent
                        size: 32
                        name: sidebar.meDisplayName
                        photo: sidebar.mePhoto
                        tint: Theme.palette[0]
                    }

                    Rectangle {
                        visible: sidebar.mePresenceDot !== ""
                        width: 9
                        height: 9
                        radius: height / 2
                        color: Theme.card
                        anchors { right: parent.right; bottom: parent.bottom }

                        Dot {
                            anchors.centerIn: parent
                            kind: sidebar.mePresenceDot
                        }
                    }
                }

                MouseArea {
                    id: meSlimArea
                    anchors.fill: parent
                    hoverEnabled: true
                    cursorShape: Qt.PointingHandCursor
                    onClicked: sidebar.accountClicked()
                }

                Tip {
                    text: sidebar.meDisplayName + "  ·  " + sidebar.meStatusLine
                    show: meSlimArea.containsMouse && !sidebar.accountMenuOpen
                }
            }
        }
    }

    // ── Unclipped tooltip anchor for scrollable items in slim mode ──
    Item {
        x: 0
        y: sidebar.tipY - height / 2
        width: sidebar.width
        height: 32
        z: 100
        Tip {
            text: sidebar.tipText
            show: sidebar.tipShown && sidebar.compact
            gap: Theme.space2
        }
    }

    // ── "Mais" tools menu ───────────────────────────────────────
    Popup {
        id: morePopup
        width: 244
        padding: Theme.space2
        closePolicy: Popup.CloseOnEscape | Popup.CloseOnPressOutside
        enter: Transition {
            ParallelAnimation {
                NumberAnimation { property: "opacity"; from: 0; to: 1; duration: Theme.medium; easing.type: Theme.ease }
                NumberAnimation { property: "scale"; from: 0.97; to: 1; duration: Theme.medium; easing.type: Theme.ease }
            }
        }
        exit: Transition {
            NumberAnimation { property: "opacity"; from: 1; to: 0; duration: Theme.fast }
        }
        background: Rectangle {
            radius: Theme.radiusLg
            color: Theme.card
            border.width: 1
            border.color: Theme.lineStrong
        }
        contentItem: Column {
            spacing: 2

            Repeater {
                model: sidebar.moreTools
                delegate: Rectangle {
                    id: mItem
                    required property var modelData
                    readonly property bool on: win.page === modelData.key
                    width: 244 - 2 * Theme.space2
                    height: 36
                    radius: Theme.radiusSm
                    color: on ? Theme.active : mArea.containsMouse ? Theme.hover : "transparent"
                    Behavior on color { ColorAnimation { duration: Theme.fast } }

                    Icon {
                        id: mIcon
                        x: Theme.space2
                        anchors.verticalCenter: parent.verticalCenter
                        name: mItem.modelData.icon
                        size: 16
                        color: mItem.on ? Theme.white : mArea.containsMouse ? Theme.text : Theme.text3
                    }

                    Text {
                        anchors {
                            left: mIcon.right
                            leftMargin: Theme.space3
                            right: mCount.visible ? mCount.left : parent.right
                            rightMargin: Theme.space2
                            verticalCenter: parent.verticalCenter
                        }
                        text: mItem.modelData.label
                        color: mItem.on ? Theme.white : mArea.containsMouse ? Theme.text : Theme.text2
                        font.pixelSize: Theme.fontBody
                        font.weight: mItem.on ? Font.DemiBold : Font.Medium
                        elide: Text.ElideRight
                    }

                    CountPill {
                        id: mCount
                        anchors { right: parent.right; rightMargin: Theme.space2; verticalCenter: parent.verticalCenter }
                        count: mItem.modelData.count || 0
                    }

                    MouseArea {
                        id: mArea
                        anchors.fill: parent
                        hoverEnabled: true
                        cursorShape: Qt.PointingHandCursor
                        onClicked: {
                            morePopup.close();
                            win.go(mItem.modelData.key);
                        }
                    }
                }
            }
        }
    }

    // ── Right-click menu on an agent: pin / move sector ─────────
    Popup {
        id: agentMenu
        property var agent: null
        width: 224
        padding: Theme.space2
        closePolicy: Popup.CloseOnEscape | Popup.CloseOnPressOutside
        enter: Transition {
            NumberAnimation { property: "opacity"; from: 0; to: 1; duration: Theme.fast }
        }
        exit: Transition {
            NumberAnimation { property: "opacity"; from: 1; to: 0; duration: Theme.fast }
        }
        background: Rectangle {
            radius: Theme.radiusMd
            color: Theme.card
            border.width: 1
            border.color: Theme.lineStrong
        }
        contentItem: Column {
            spacing: 2

            Rectangle {
                id: pinMenuRow
                readonly property bool isPinned: !!agentMenu.agent && !!agentMenu.agent.pinned
                readonly property bool pinFull: !isPinned && sidebar.pinnedAgents.length >= 5
                width: 224 - 2 * Theme.space2
                height: 32
                radius: Theme.radiusSm
                color: pinMenuArea.containsMouse && !pinFull ? Theme.hover : "transparent"

                Icon {
                    id: pinMenuIcon
                    x: Theme.space2
                    anchors.verticalCenter: parent.verticalCenter
                    name: "pin"
                    size: 14
                    color: pinMenuRow.pinFull ? Theme.text4 : Theme.text2
                }

                Text {
                    anchors { left: pinMenuIcon.right; leftMargin: Theme.space2; right: parent.right; rightMargin: Theme.space2; verticalCenter: parent.verticalCenter }
                    text: pinMenuRow.isPinned ? "Desafixar do topo" : pinMenuRow.pinFull ? "Topo cheio (5/5)" : "Fixar no topo"
                    color: pinMenuRow.pinFull ? Theme.text4 : Theme.text
                    font.pixelSize: Theme.fontBody
                    elide: Text.ElideRight
                }

                MouseArea {
                    id: pinMenuArea
                    anchors.fill: parent
                    hoverEnabled: true
                    cursorShape: pinMenuRow.pinFull ? Qt.ArrowCursor : Qt.PointingHandCursor
                    onClicked: {
                        if (!pinMenuRow.pinFull && agentMenu.agent)
                            sidebar.togglePin(agentMenu.agent);
                        agentMenu.close();
                    }
                }
            }

            Rectangle {
                visible: sidebar.sectors.length > 0
                width: 224 - 2 * Theme.space2
                height: 1
                color: Theme.line
            }

            Overline {
                visible: sidebar.sectors.length > 0
                leftPadding: Theme.space2
                topPadding: Theme.space1
                text: "SETOR"
            }

            Repeater {
                model: sidebar.sectors.length > 0 ? [{ id: "", name: "Sem setor", color: "" }].concat(sidebar.sectors) : []
                delegate: Rectangle {
                    id: secChoice
                    required property var modelData
                    readonly property bool here: !!agentMenu.agent && (agentMenu.agent.sector || "") === modelData.id
                    width: 224 - 2 * Theme.space2
                    height: 30
                    radius: Theme.radiusSm
                    color: secChoiceArea.containsMouse && !here ? Theme.hover : "transparent"

                    Rectangle {
                        id: secChoiceDot
                        x: Theme.space2
                        width: 8
                        height: 8
                        radius: Theme.radiusXs
                        anchors.verticalCenter: parent.verticalCenter
                        color: secChoice.modelData.color || "transparent"
                        border.width: secChoice.modelData.color ? 0 : 1
                        border.color: Theme.text4
                    }

                    Text {
                        anchors { left: secChoiceDot.right; leftMargin: Theme.space2; right: parent.right; rightMargin: Theme.space2; verticalCenter: parent.verticalCenter }
                        text: secChoice.modelData.name
                        color: secChoice.here ? Theme.text4 : Theme.text
                        font.pixelSize: Theme.fontBody
                        elide: Text.ElideRight
                    }

                    MouseArea {
                        id: secChoiceArea
                        anchors.fill: parent
                        hoverEnabled: true
                        cursorShape: secChoice.here ? Qt.ArrowCursor : Qt.PointingHandCursor
                        onClicked: {
                            if (!secChoice.here && agentMenu.agent)
                                sidebar.moveAgentToSector(agentMenu.agent, secChoice.modelData.id);
                            agentMenu.close();
                        }
                    }
                }
            }
        }
    }
}
