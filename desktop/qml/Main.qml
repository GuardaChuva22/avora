import QtCore
import QtQuick
import QtQuick.Controls.Basic
import QtQuick.Effects
import Avora
import "util.js" as U

// Frameless main window: atmosphere, sidebar, pages, our own title bar.
Window {
    id: win
    property string startPage: ""
    property string autoServer: ""
    property string autoToken: ""

    // People, Activity, Nodes and Models live inside Settings ("config");
    // --page pessoas|atividade|nos|modelos opens that section.
    readonly property var sections: ["pessoas", "modelos", "atividade", "nos"]
    readonly property var directPages: ["home", "waiting", "tasks", "chat", "plugins", "skills", "docs", "goals", "config"]
    // "modelos:pick" also opens a sheet of that section.
    readonly property string startBase: startPage.split(":")[0]
    readonly property string startSheet: startPage.split(":")[1] || ""
    property string openPref: "home"
    property string onboardingDoneJson: "{}"
    property string page: directPages.indexOf(startBase) >= 0 ? startBase : sections.indexOf(startBase) >= 0 ? "config" : "home"
    property string section: sections.indexOf(startBase) >= 0 ? startBase : "modelos"
    property var nodes: []
    property var quotas: []
    property bool compact: false

    // Remembered between launches.
    Settings {
        category: "ui"
        property alias sidebarCompact: win.compact
        property alias openMode: win.openPref
        property alias onboardingDone: win.onboardingDoneJson
    }
    property bool pendingDocsTab: false
    onPageChanged: {
        if (page === "docs") {
            const l = pageLoaders.itemAt(0);
            if (l && l.item)
                l.item.closeSidePanel();
        }
    }
    Connections {
        target: DocBus
        function onOpenRequested(id) {
            DocBus.openId = id;
            if (win.page === "chat") {
                const l = pageLoaders.itemAt(0);
                if (l && l.item)
                    l.item.openDocsTab();
                else
                    win.pendingDocsTab = true;
            } else if (win.page !== "docs") {
                win.go("docs");
            }
        }
        function onListRequested() {
            if (win.page === "chat") {
                DocBus.openId = "";
                const l = pageLoaders.itemAt(0);
                if (l && l.item)
                    l.item.openDocsTab();
                else
                    win.pendingDocsTab = true;
            } else {
                win.go("docs");
            }
        }
        function onCiteRequested(doc) {
            if (win.page !== "chat")
                win.go("chat");
        }
    }
    Shortcut {
        sequence: "Ctrl+B"
        onActivated: win.compact = !win.compact
    }

    width: 1320
    height: 860
    minimumWidth: 980
    minimumHeight: 640
    visible: true
    color: Theme.bg
    title: "Avora"
    flags: Qt.Window | Qt.FramelessWindowHint

    // go opens a page; "modelos:connect" opens that settings section and
    // one of its sheets.
    function go(p) {
        const base = p.split(":")[0];
        const sub = p.split(":")[1] || "";
        if (sections.indexOf(base) < 0) {
            page = base;
            if (base === "chat" && sub !== "") {
                const l = pageLoaders.itemAt(0);
                if (l && l.item)
                    l.item.sheet = sub;
                else
                    pendingChatSheet = sub;
            }
            return;
        }
        section = base;
        configSheet = sub;
        page = "config";
        for (let i = 0; i < pageLoaders.count; i++) {
            const l = pageLoaders.itemAt(i);
            if (l && l.item && l.item.deliver !== undefined) {
                l.item.section = base;
                l.item.sheet = configSheet;
                configSheet = "";
            }
        }
    }
    property string configSheet: startSheet
    property string pendingChatSheet: startBase === "chat" ? startSheet : ""
    // openConversation shows a conversation in the chat page (from other pages).
    property string pendingConversation: ""
    property string pendingAgent: ""
    property string pendingChannel: ""
    property string pendingAskAgent: ""
    property string pendingAskText: ""
    property string pendingRunId: ""
    property string pendingRunAgent: ""
    property bool pendingNewAgent: false
    function openConversation(id) {
        go("chat");
        const l = pageLoaders.itemAt(0);
        if (l && l.item)
            l.item.open(id);
        else
            pendingConversation = id;
    }
    function openAgentConv(agentId, convId) {
        go("chat");
        const l = pageLoaders.itemAt(0);
        if (l && l.item)
            l.item.openAgentConv(agentId, convId);
        else {
            pendingAgent = agentId;
            pendingConversation = convId;
        }
    }
    function openChannel(channelId) {
        go("chat");
        const l = pageLoaders.itemAt(0);
        if (l && l.item)
            l.item.selectChannel(channelId || "time");
        else
            pendingChannel = channelId || "time";
    }
    function askAgent(agentId, text) {
        go("chat");
        const l = pageLoaders.itemAt(0);
        if (l && l.item)
            l.item.askAgent(agentId, text);
        else {
            pendingAskAgent = agentId;
            pendingAskText = text;
        }
    }
    function openRun(runId, agentId) {
        go("chat");
        const l = pageLoaders.itemAt(0);
        if (l && l.item)
            l.item.openRun(runId, agentId);
        else {
            pendingRunId = runId;
            pendingRunAgent = agentId;
        }
    }
    function openNewAgent() {
        go("chat");
        const l = pageLoaders.itemAt(0);
        if (l && l.item)
            l.item.openNewAgent("");
        else
            pendingNewAgent = true;
    }
    function applyOpenPreference() {
        if (startPage !== "")
            return;
        if (openPref === "chat") {
            go("chat");
        } else if (openPref.startsWith("channel:")) {
            openChannel(openPref.slice(8));
        } else {
            go("home");
        }
    }

    // "21:40" today, "qui 22h" later in the week.
    // poolAgy shows every Antigravity account as one ring (D61): the
    // average left in each window, out only when every account is out, and
    // the soonest reset among the ones that are out.
    function poolAgy(list) {
        const agy = list.filter(q => q.engine === "antigravity");
        if (agy.length < 2)
            return list;
        const ok = agy.filter(q => !q.error);
        const avg = key => {
            const xs = ok.filter(q => q[key]);
            if (!xs.length)
                return null;
            var used = 0, reset = 0;
            xs.forEach(q => {
                used += q[key].used_percent;
                if (q[key].resets_at && (!reset || q[key].resets_at < reset))
                    reset = q[key].resets_at;
            });
            return { used_percent: used / xs.length, resets_at: reset };
        };
        const free = ok.filter(q => !q.limited).length;
        const pool = {
            engine: "antigravity",
            account: "antigravity",
            name: "Antigravity  ·  " + free + "/" + agy.length,
            five_hour: avg("five_hour"),
            weekly: avg("weekly"),
            limited: ok.length > 0 && free === 0,
            error: ok.length ? "" : (agy[0].error || "sem leitura")
        };
        const out = [];
        var placed = false;
        list.forEach(q => {
            if (q.engine !== "antigravity")
                out.push(q);
            else if (!placed) {
                out.push(pool);
                placed = true;
            }
        });
        return out;
    }
    function resetLabel(unix) {
        if (!unix)
            return "em breve";
        const d = new Date(unix * 1000), now = new Date();
        const pad = n => (n < 10 ? "0" : "") + n;
        if (d.toDateString() === now.toDateString())
            return pad(d.getHours()) + ":" + pad(d.getMinutes());
        return ["dom", "seg", "ter", "qua", "qui", "sex", "sáb"][d.getDay()] + " " + d.getHours() + "h";
    }

    Component.onCompleted: {
        applyOpenPreference();
        if (!Api.signedIn && autoToken !== "")
            Api.login(autoServer, autoToken, err => {
                if (err)
                    console.warn("test sign-in failed:", err);
            });
    }

    Backdrop {
        anchors.fill: parent
    }

    // ── Signed out ──────────────────────────────────────────────
    LoginPage {
        anchors.fill: parent
        visible: !Api.signedIn
    }

    // ── Signed in ───────────────────────────────────────────────
    Item {
        id: shell
        anchors.fill: parent
        visible: Api.signedIn

        property bool presenceOpen: false
        readonly property var mePerson: (Api.me && Api.me.person && Api.me.person.id) ? Api.me.person : null
        readonly property string currentWsId: Api.workspace !== "" ? Api.workspace
            : (Api.me && Api.me.workspaces && Api.me.workspaces.length > 0 ? Api.me.workspaces[0].id : "pessoal")
        readonly property string onboardingKey: (mePerson ? mePerson.id + ":" : "") + currentWsId
        property bool wsChecked: false
        property bool wsHasAgents: true
        property bool onboardingOpen: false
        property bool onboardingPaused: false

        readonly property bool isOnboardingDone: {
            try {
                const map = JSON.parse(win.onboardingDoneJson || "{}");
                return !!map[onboardingKey] || !!map[currentWsId];
            } catch (e) {
                return false;
            }
        }
        readonly property bool canResumeOnboarding: Api.signedIn && wsChecked && !isOnboardingDone && onboardingPaused && (!wsHasAgents || onboardingView.step > 2)

        function markOnboardingDone() {
            let map = {};
            try {
                map = JSON.parse(win.onboardingDoneJson || "{}");
            } catch (e) {
                map = {};
            }
            map[onboardingKey] = true;
            map[currentWsId] = true;
            win.onboardingDoneJson = JSON.stringify(map);
            onboardingOpen = false;
            onboardingPaused = false;
        }
        function checkOnboarding() {
            if (!Api.signedIn)
                return;
            const ws = currentWsId;
            Api.get("/v1/agents", (err, active) => {
                if (err || shell.currentWsId !== ws)
                    return;
                if ((active || []).length > 0) {
                    shell.wsHasAgents = true;
                    shell.wsChecked = true;
                    return;
                }
                Api.get("/v1/agents/archived", (aErr, arch) => {
                    if (shell.currentWsId !== ws)
                        return;
                    const hasAny = !aErr && (arch || []).length > 0;
                    shell.wsHasAgents = hasAny;
                    shell.wsChecked = true;
                    if (!hasAny && !shell.isOnboardingDone && !shell.onboardingPaused && win.startPage === "")
                        shell.onboardingOpen = true;
                });
            });
        }

        function presenceLabel(p) {
            return p === "away" ? "Ausente" : p === "busy" ? "Ocupado" : p === "invisible" ? "Invisível" : "Automático";
        }
        function presenceDot(p) {
            return p === "away" ? "warn" : p === "busy" ? "bad" : p === "invisible" ? "" : "live";
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
        function saveStatusText() {
            const text = statusInput.text.trim();
            Api.patch("/v1/me", { status_text: text }, err => {
                if (!err)
                    Api.refreshMe();
                shell.presenceOpen = false;
            });
        }
        function refreshQuotas() {
            Api.get("/v1/quotas", (err, data) => {
                if (err)
                    return;
                data = win.poolAgy(data);
                if (!U.same(win.quotas, data))
                    win.quotas = data;
            });
        }
        function refreshNodes() {
            Api.get("/v1/nodes", (err, data) => {
                if (err)
                    return;
                const live = data.filter(n => !n.revoked_at);
                if (!U.same(win.nodes, live))
                    win.nodes = live;
            });
        }
        function reloadWorkspace() {
            shell.presenceOpen = false;
            shell.wsChecked = false;
            shell.onboardingOpen = false;
            shell.onboardingPaused = false;
            onboardingView.step = 1;
            onboardingView.prefilled = false;
            DocBus.openId = "";
            DocBus.load();
            refreshQuotas();
            refreshNodes();
            checkOnboarding();
            for (let i = 0; i < pageLoaders.count; i++) {
                const l = pageLoaders.itemAt(i);
                if (!l)
                    continue;
                l.opened = false;
                if (l.current)
                    l.opened = true;
            }
        }

        Connections {
            target: Api
            function onSignedInChanged() {
                if (Api.signedIn) {
                    shell.onboardingPaused = false;
                    shell.checkOnboarding();
                } else {
                    shell.wsChecked = false;
                    shell.onboardingOpen = false;
                }
            }
            function onWorkspaceChanged() {
                if (Api.signedIn)
                    shell.reloadWorkspace();
            }
            function onLiveEvent(e) {
                const t = e.type || "";
                if (t.startsWith("workspace.") || t.startsWith("member.") || t.startsWith("person."))
                    Api.refreshMe();
                if (t === "agent.saved")
                    shell.checkOnboarding();
            }
        }
        Component.onCompleted: if (Api.signedIn) shell.checkOnboarding()

        Timer {
            interval: 30000
            running: shell.visible
            repeat: true
            onTriggered: Api.refreshMe()
        }

        WorkspaceRail {
            id: rail
            z: 3
            anchors { top: parent.top; bottom: parent.bottom; left: parent.left }
            onAddRequested: workspaceSheet.open("create")
        }

        Rectangle {
            id: sidebar
            readonly property real full: 248
            readonly property real slim: 72
            width: win.compact ? slim : full
            Behavior on width { NumberAnimation { duration: Theme.move; easing.type: Theme.ease } }
            z: 2 // tooltips float over the pages
            anchors { top: parent.top; bottom: parent.bottom; left: rail.visible ? rail.right : parent.left }
            color: Theme.panel
            Rectangle {
                anchors { top: parent.top; bottom: parent.bottom; right: parent.right }
                width: 1
                gradient: Gradient {
                    GradientStop { position: 0; color: Theme.wash1 }
                    GradientStop { position: 0.4; color: Theme.wash4 }
                    GradientStop { position: 1; color: Theme.wash1 }
                }
            }

            Row {
                id: brand
                x: 24
                y: 22
                spacing: 12
                BrandMark {
                    size: 24
                    anchors.verticalCenter: parent.verticalCenter
                }
                Text {
                    text: "AVORA"
                    opacity: win.compact ? 0 : 1
                    visible: opacity > 0
                    Behavior on opacity { NumberAnimation { duration: win.compact ? 90 : Theme.medium } }
                    color: Theme.white
                    font.family: Theme.display
                    font.pixelSize: Theme.fontLarge
                    font.weight: Font.DemiBold
                    font.letterSpacing: 5
                    anchors.verticalCenter: parent.verticalCenter
                }
            }

            // The sidebar's top strip moves the window too (the toggle sits above it).
            Item {
                width: parent.width
                height: Theme.titleBar + 16
                DragHandler {
                    target: null
                    onActiveChanged: if (active) win.startSystemMove()
                }
            }

            // Collapse / expand. Top right when open, under the mark when slim.
            Rectangle {
                id: toggle
                width: 30
                height: 30
                radius: Theme.radiusSm
                x: win.compact ? (sidebar.slim - width) / 2 : sidebar.full - width - 16
                y: win.compact ? 66 : 19
                Behavior on x { NumberAnimation { duration: Theme.move; easing.type: Theme.ease } }
                Behavior on y { NumberAnimation { duration: Theme.move; easing.type: Theme.ease } }
                color: toggleArea.pressed ? Theme.active : toggleArea.containsMouse ? Theme.hover : "transparent"
                border.width: toggleArea.containsMouse ? 1 : 0
                border.color: Theme.line
                Icon {
                    anchors.centerIn: parent
                    name: "sidebar"
                    size: 17
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
                    text: win.compact ? "Expandir  ·  Ctrl+B" : "Recolher  ·  Ctrl+B"
                    show: toggleArea.containsMouse
                }
            }

            Column {
                id: navs
                anchors { top: brand.bottom; topMargin: 38; left: parent.left; right: parent.right; leftMargin: 14; rightMargin: 14 }
                spacing: 3
                Text {
                    text: "ESPAÇO"
                    opacity: win.compact ? 0 : 1
                    Behavior on opacity { NumberAnimation { duration: win.compact ? 90 : Theme.medium } }
                    leftPadding: 12
                    bottomPadding: 8
                    color: Theme.text4
                    font.pixelSize: Theme.fontCaption
                    font.weight: Font.DemiBold
                    font.letterSpacing: Theme.trackCaps
                }
                Repeater {
                    model: [
                        { key: "home", label: "Início", icon: "spark" },
                        { key: "chat", label: "Chat", icon: "chat" },
                        { key: "plugins", label: "Plugins", icon: "plug" },
                        { key: "skills", label: "Skills", icon: "book" },
                        { key: "docs", label: "Docs", icon: "docs" },
                        { key: "goals", label: "Objetivos", icon: "target" }
                    ]
                    delegate: NavItem {
                        required property var modelData
                        width: navs.width
                        icon: modelData.icon
                        label: modelData.label
                        active: win.page === modelData.key || (modelData.key === "home" && (win.page === "waiting" || win.page === "tasks"))
                        compact: win.compact
                        onClicked: win.go(modelData.key)
                    }
                }
            }

            Column {
                anchors { bottom: parent.bottom; bottomMargin: 16; left: parent.left; right: parent.right; leftMargin: 14; rightMargin: 14 }
                spacing: 10

                // Subscription limits: outer ring weekly, inner ring 5 h.
                Card {
                    id: quotaCard
                    width: parent.width
                    height: quotaCol.implicitHeight + 8
                    padding: 0
                    radius: Theme.radiusMd
                    visible: win.quotas.length > 0
                    Column {
                        id: quotaCol
                        y: 4
                        width: parent.width
                        Repeater {
                            model: win.quotas
                            delegate: Item {
                                id: quotaRow
                                required property var modelData
                                readonly property var q: modelData
                                readonly property real leftWeek: q.weekly ? 100 - q.weekly.used_percent : -1
                                readonly property real leftFive: q.five_hour ? 100 - q.five_hour.used_percent : -1
                                // An extra account shows the name given in Settings.
                                readonly property string name: q.name ? q.name : q.engine === "codex" ? "Codex" : q.engine === "antigravity" ? "Antigravity" : "Claude"
                                readonly property string summary: {
                                    if (leftWeek < 0 && leftFive < 0)
                                        return q.error ? "sem leitura" : "aguardando";
                                    if (q.limited || leftWeek <= 0 || leftFive <= 0) {
                                        const w = leftWeek <= 0 && q.weekly ? q.weekly : q.five_hour;
                                        return "esgotado · volta " + win.resetLabel(w ? w.resets_at : 0);
                                    }
                                    return "5h " + Math.round(leftFive) + "%  ·  sem " + Math.round(leftWeek) + "%";
                                }
                                readonly property bool out: q.limited || leftWeek === 0 || leftFive === 0
                                width: quotaCol.width
                                height: 50

                                MouseArea {
                                    id: quotaArea
                                    anchors.fill: parent
                                    hoverEnabled: true
                                }
                                Tip {
                                    gap: 26
                                    text: quotaRow.name + "  ·  " + quotaRow.summary
                                    show: win.compact && quotaArea.containsMouse
                                }
                                QuotaRing {
                                    id: qring
                                    size: 36
                                    engine: quotaRow.q.engine
                                    weekly: quotaRow.leftWeek
                                    fiveHour: quotaRow.leftFive
                                    limited: quotaRow.q.limited
                                    x: win.compact ? (quotaRow.width - size) / 2 : 9
                                    anchors.verticalCenter: parent.verticalCenter
                                    Behavior on x { NumberAnimation { duration: Theme.move; easing.type: Theme.ease } }
                                }
                                Column {
                                    anchors { left: parent.left; leftMargin: 57; right: parent.right; rightMargin: 10; verticalCenter: parent.verticalCenter }
                                    spacing: 3
                                    opacity: win.compact ? 0 : 1
                                    visible: opacity > 0
                                    Behavior on opacity { NumberAnimation { duration: win.compact ? 90 : Theme.medium } }
                                    Row {
                                        spacing: 7
                                        Text {
                                            text: quotaRow.name
                                            color: Theme.text
                                            font.pixelSize: Theme.fontBody
                                            font.weight: Font.DemiBold
                                        }
                                        Text {
                                            visible: !!quotaRow.q.plan
                                            text: (quotaRow.q.plan || "").toUpperCase()
                                            color: Theme.text4
                                            font.pixelSize: Theme.fontTiny
                                            font.weight: Font.DemiBold
                                            font.letterSpacing: Theme.trackCaps
                                            topPadding: 4
                                        }
                                    }
                                    Text {
                                        width: parent.width
                                        text: quotaRow.summary
                                        color: quotaRow.out ? Theme.bad : Theme.text3
                                        font.family: Theme.mono
                                        font.pixelSize: Theme.fontCaption
                                        elide: Text.ElideRight
                                    }
                                }
                            }
                        }
                    }
                }

                // Node presence. Fixed padding; in slim mode only the dots
                // slide to the middle, so nothing moves vertically.
                Card {
                    width: parent.width
                    height: presence.implicitHeight + 24
                    padding: 12
                    radius: Theme.radiusMd
                    visible: win.nodes.length > 0
                    MouseArea {
                        id: presenceArea
                        anchors.fill: parent
                        anchors.margins: -12
                        hoverEnabled: true
                    }
                    Tip {
                        gap: 24
                        text: win.nodes.map(n => n.name + " · " + (n.online ? "online" : "offline")).join("   ")
                        show: win.compact && presenceArea.containsMouse
                    }
                    Column {
                        id: presence
                        width: parent.width
                        spacing: 8
                        Repeater {
                            model: win.nodes
                            delegate: Item {
                                id: nodeRow
                                required property var modelData
                                width: presence.width
                                height: 18
                                Dot {
                                    id: nodeDot
                                    kind: nodeRow.modelData.online ? "live" : ""
                                    x: win.compact ? (nodeRow.width - width) / 2 : 0
                                    anchors.verticalCenter: parent.verticalCenter
                                    Behavior on x { NumberAnimation { duration: Theme.move; easing.type: Theme.ease } }
                                }
                                Text {
                                    id: nodeState
                                    anchors { right: parent.right; verticalCenter: parent.verticalCenter }
                                    text: nodeRow.modelData.online ? "online" : "offline"
                                    color: nodeRow.modelData.online ? Theme.text2 : Theme.text4
                                    font.family: Theme.mono
                                    font.pixelSize: Theme.fontCaption
                                    opacity: win.compact ? 0 : 1
                                    visible: opacity > 0
                                    Behavior on opacity { NumberAnimation { duration: win.compact ? 90 : Theme.medium } }
                                }
                                Text {
                                    anchors { left: parent.left; leftMargin: 16; right: nodeState.left; rightMargin: 8; verticalCenter: parent.verticalCenter }
                                    text: nodeRow.modelData.name
                                    color: Theme.text2
                                    font.pixelSize: Theme.fontBody
                                    elide: Text.ElideRight
                                    opacity: win.compact ? 0 : 1
                                    visible: opacity > 0
                                    Behavior on opacity { NumberAnimation { duration: win.compact ? 90 : Theme.medium } }
                                }
                            }
                        }
                    }
                }
                NavItem {
                    width: parent.width
                    icon: "gear"
                    label: "Configurações"
                    active: win.page === "config"
                    compact: win.compact
                    onClicked: win.go("config")
                }
                // Person profile & presence bar (or Sair in operator mode).
                Rectangle {
                    id: meFooter
                    visible: shell.mePerson !== null
                    width: parent.width
                    height: 46
                    radius: Theme.radiusMd
                    color: shell.presenceOpen || meArea.pressed ? Theme.active : meArea.containsMouse ? Theme.hover : Theme.wash1
                    border.width: 1
                    border.color: shell.presenceOpen ? Theme.focusLine : Theme.line
                    Behavior on color { ColorAnimation { duration: Theme.fast } }

                    MouseArea {
                        id: meArea
                        anchors.fill: parent
                        hoverEnabled: true
                        cursorShape: Qt.PointingHandCursor
                        onClicked: {
                            statusInput.text = (shell.mePerson && shell.mePerson.status_text) ? shell.mePerson.status_text : "";
                            shell.presenceOpen = !shell.presenceOpen;
                        }
                    }

                    Item {
                        width: 32
                        height: 32
                        x: win.compact ? (meFooter.width - width) / 2 : Theme.space2
                        anchors.verticalCenter: parent.verticalCenter
                        Behavior on x { NumberAnimation { duration: Theme.move; easing.type: Theme.ease } }

                        Avatar {
                            anchors.fill: parent
                            size: 32
                            name: shell.mePerson ? (shell.mePerson.name || shell.mePerson.email || "?") : "?"
                            photo: shell.mePerson ? shell.personPhoto(shell.mePerson.avatar) : ""
                            tint: Theme.palette[0]
                        }
                        Rectangle {
                            width: 10
                            height: 10
                            radius: height / 2
                            color: Theme.card
                            anchors { right: parent.right; bottom: parent.bottom }
                            Dot {
                                anchors.centerIn: parent
                                kind: shell.mePerson ? shell.presenceDot(shell.mePerson.presence) : "live"
                            }
                        }
                    }

                    Column {
                        anchors { left: parent.left; leftMargin: 46; right: quickLogout.left; rightMargin: Theme.space1; verticalCenter: parent.verticalCenter }
                        spacing: 0
                        opacity: win.compact ? 0 : 1
                        visible: opacity > 0
                        Behavior on opacity { NumberAnimation { duration: win.compact ? 90 : Theme.medium } }
                        Text {
                            width: parent.width
                            elide: Text.ElideRight
                            text: shell.mePerson ? (shell.mePerson.name || shell.mePerson.email) : ""
                            color: Theme.white
                            font.pixelSize: Theme.fontBody
                            font.weight: Font.DemiBold
                        }
                        Text {
                            width: parent.width
                            elide: Text.ElideRight
                            text: shell.mePerson ? (shell.mePerson.status_text ? shell.mePerson.status_text : shell.presenceLabel(shell.mePerson.presence)) : ""
                            color: Theme.text3
                            font.pixelSize: Theme.fontCaption
                        }
                    }

                    Rectangle {
                        id: quickLogout
                        visible: !win.compact
                        width: 28
                        height: 28
                        radius: Theme.radiusSm
                        anchors { right: parent.right; rightMargin: Theme.space2; verticalCenter: parent.verticalCenter }
                        color: quickLogoutArea.containsMouse ? Theme.hover : "transparent"
                        Icon {
                            anchors.centerIn: parent
                            name: "logout"
                            size: 15
                            color: quickLogoutArea.containsMouse ? Theme.white : Theme.text3
                        }
                        MouseArea {
                            id: quickLogoutArea
                            anchors.fill: parent
                            hoverEnabled: true
                            cursorShape: Qt.PointingHandCursor
                            onClicked: Api.logout()
                        }
                        Tip {
                            text: "Sair"
                            show: quickLogoutArea.containsMouse
                        }
                    }

                    Tip {
                        text: shell.mePerson ? (shell.mePerson.name || shell.mePerson.email) + "  ·  " + (shell.mePerson.status_text || shell.presenceLabel(shell.mePerson.presence)) : ""
                        show: win.compact && meArea.containsMouse && !shell.presenceOpen
                    }
                }

                NavItem {
                    visible: shell.mePerson === null
                    width: parent.width
                    icon: "logout"
                    label: "Sair"
                    compact: win.compact
                    onClicked: Api.logout()
                }
            }

            Timer {
                interval: 30000
                running: shell.visible
                repeat: true
                triggeredOnStart: true
                onTriggered: shell.refreshQuotas()
            }
            Timer {
                interval: 10000
                running: shell.visible
                repeat: true
                triggeredOnStart: true
                onTriggered: shell.refreshNodes()
            }
        }

        // Presence & status popover menu.
        MouseArea {
            visible: shell.presenceOpen
            z: 24
            anchors.fill: parent
            onClicked: shell.presenceOpen = false
        }
        Shortcut {
            sequence: "Esc"
            enabled: shell.presenceOpen
            onActivated: shell.presenceOpen = false
        }
        Card {
            id: presencePopup
            z: 25
            x: (rail.visible ? rail.width : 0) + Theme.space3
            anchors { bottom: parent.bottom; bottomMargin: 72 }
            width: 272
            height: presenceMenuCol.implicitHeight + 2 * padding
            padding: Theme.space4
            radius: Theme.radiusLg
            visible: opacity > 0
            opacity: shell.presenceOpen ? 1 : 0
            Behavior on opacity { NumberAnimation { duration: Theme.fast } }
            MouseArea { anchors.fill: parent }

            Column {
                id: presenceMenuCol
                width: parent.width
                spacing: Theme.space3

                Overline { text: "PRESENÇA" }

                Column {
                    width: parent.width
                    spacing: Theme.space1
                    Repeater {
                        model: [
                            { key: "auto", label: "Automático", dot: "live" },
                            { key: "away", label: "Ausente", dot: "warn" },
                            { key: "busy", label: "Ocupado", dot: "bad" },
                            { key: "invisible", label: "Invisível", dot: "" }
                        ]
                        delegate: Rectangle {
                            id: presOpt
                            required property var modelData
                            readonly property string cur: (shell.mePerson && shell.mePerson.presence) ? shell.mePerson.presence : "auto"
                            readonly property bool on: cur === modelData.key
                            width: parent.width
                            height: 32
                            radius: Theme.radiusSm
                            color: on ? Theme.active : presOptArea.containsMouse ? Theme.hover : "transparent"

                            Row {
                                x: Theme.space2
                                anchors.verticalCenter: parent.verticalCenter
                                spacing: Theme.space2
                                Dot {
                                    kind: presOpt.modelData.dot
                                    anchors.verticalCenter: parent.verticalCenter
                                }
                                Text {
                                    text: presOpt.modelData.label
                                    color: presOpt.on ? Theme.white : Theme.text2
                                    font.pixelSize: Theme.fontBody
                                    font.weight: presOpt.on ? Font.DemiBold : Font.Normal
                                    anchors.verticalCenter: parent.verticalCenter
                                }
                            }
                            Icon {
                                visible: presOpt.on
                                anchors { right: parent.right; rightMargin: Theme.space2; verticalCenter: parent.verticalCenter }
                                name: "check"
                                size: 14
                                color: Theme.white
                            }
                            MouseArea {
                                id: presOptArea
                                anchors.fill: parent
                                hoverEnabled: true
                                cursorShape: Qt.PointingHandCursor
                                onClicked: {
                                    Api.patch("/v1/me", { presence: presOpt.modelData.key }, err => {
                                        if (!err)
                                            Api.refreshMe();
                                    });
                                }
                            }
                        }
                    }
                }

                Rectangle { width: parent.width; height: 1; color: Theme.line }

                Column {
                    width: parent.width
                    spacing: Theme.space2
                    Overline { text: "STATUS" }
                    Row {
                        width: parent.width
                        spacing: Theme.space2
                        Field {
                            id: statusInput
                            width: parent.width - saveStatusBtn.width - Theme.space2
                            placeholderText: "O que você está fazendo…"
                            maximumLength: 80
                            onAccepted: shell.saveStatusText()
                        }
                        GhostButton {
                            id: saveStatusBtn
                            height: 42
                            round: true
                            icon: "check"
                            onClicked: shell.saveStatusText()
                        }
                    }
                    Text {
                        visible: !!(shell.mePerson && shell.mePerson.status_text)
                        text: "Limpar status"
                        color: clearStatusArea.containsMouse ? Theme.white : Theme.text3
                        font.pixelSize: Theme.fontSmall
                        MouseArea {
                            id: clearStatusArea
                            anchors.fill: parent
                            hoverEnabled: true
                            cursorShape: Qt.PointingHandCursor
                            onClicked: {
                                statusInput.text = "";
                                shell.saveStatusText();
                            }
                        }
                    }
                }

                Rectangle { width: parent.width; height: 1; color: Theme.line }

                Column {
                    width: parent.width
                    spacing: Theme.space1
                    Rectangle {
                        width: parent.width
                        height: 32
                        radius: Theme.radiusSm
                        color: wsMenuArea.containsMouse ? Theme.hover : "transparent"
                        Row {
                            x: Theme.space2
                            anchors.verticalCenter: parent.verticalCenter
                            spacing: Theme.space2
                            Icon { name: "plus"; size: 15; color: Theme.text2; anchors.verticalCenter: parent.verticalCenter }
                            Text { text: "Criar ou entrar em espaço…"; color: Theme.text2; font.pixelSize: Theme.fontBody; anchors.verticalCenter: parent.verticalCenter }
                        }
                        MouseArea {
                            id: wsMenuArea
                            anchors.fill: parent
                            hoverEnabled: true
                            cursorShape: Qt.PointingHandCursor
                            onClicked: {
                                shell.presenceOpen = false;
                                workspaceSheet.open("create");
                            }
                        }
                    }
                    Rectangle {
                        width: parent.width
                        height: 32
                        radius: Theme.radiusSm
                        color: logoutMenuArea.containsMouse ? Theme.hover : "transparent"
                        Row {
                            x: Theme.space2
                            anchors.verticalCenter: parent.verticalCenter
                            spacing: Theme.space2
                            Icon { name: "logout"; size: 15; color: Theme.bad; anchors.verticalCenter: parent.verticalCenter }
                            Text { text: "Sair da conta"; color: Theme.bad; font.pixelSize: Theme.fontBody; anchors.verticalCenter: parent.verticalCenter }
                        }
                        MouseArea {
                            id: logoutMenuArea
                            anchors.fill: parent
                            hoverEnabled: true
                            cursorShape: Qt.PointingHandCursor
                            onClicked: {
                                shell.presenceOpen = false;
                                Api.logout();
                            }
                        }
                    }
                }
            }
        }

        WorkspaceSheet {
            id: workspaceSheet
            anchors.fill: parent
        }

        // Pages are laid out at the sidebar's final width at once, and this
        // clip only reveals or covers the strip while the sidebar animates.
        // Resizing the pages every frame re-wrapped every chat message.
        Item {
            id: pagesClip
            anchors { top: parent.top; bottom: parent.bottom; left: sidebar.right; right: parent.right }
            clip: true
        // Pages stay alive once opened, so a running chat keeps streaming.
        Item {
            id: pages
            readonly property real railWidth: rail.visible ? rail.width : 0
            readonly property real edge: win.compact ? sidebar.slim : sidebar.full
            x: edge - sidebar.width
            width: win.width - railWidth - edge
            height: parent.height


            Repeater {
                id: pageLoaders
                model: [
                    { key: "chat", url: "ChatPage.qml" },
                    { key: "plugins", url: "PluginsPage.qml" },
                    { key: "skills", url: "SkillsPage.qml" },
                    { key: "docs", url: "DocsPage.qml" },
                    { key: "goals", url: "GoalsPage.qml" },
                    { key: "config", url: "SettingsPage.qml" },
                    { key: "home", url: "HomePage.qml" },
                    { key: "waiting", url: "WaitingPage.qml" },
                    { key: "tasks", url: "TaskManager.qml" }
                ]
                delegate: Loader {
                    id: loader
                    required property var modelData
                    readonly property bool current: win.page === modelData.key
                    property bool opened: false
                    onCurrentChanged: if (current) opened = true
                    Component.onCompleted: if (current) opened = true
                    anchors.fill: parent
                    active: opened && Api.signedIn // no requests before sign-in; logout drops page state
                    visible: current
                    source: modelData.url
                    onLoaded: {
                        if (modelData.key === "goals")
                            item.openConversation.connect(win.openConversation);
                        if (modelData.key === "home") {
                            item.goPage.connect(win.go);
                            item.askAgent.connect(win.askAgent);
                            item.openAgentConv.connect(win.openAgentConv);
                            item.openChannel.connect(win.openChannel);
                            item.openRun.connect(win.openRun);
                            item.newAgentRequested.connect(win.openNewAgent);
                        }
                        if (modelData.key === "waiting") {
                            item.goPage.connect(win.go);
                            item.openAgentConv.connect(win.openAgentConv);
                        }
                        if (modelData.key === "tasks") {
                            item.goPage.connect(win.go);
                            item.openAgentConv.connect(win.openAgentConv);
                            item.openChannel.connect(win.openChannel);
                        }
                        if (modelData.key === "chat")
                            item.goPage.connect(win.go);
                        if (modelData.key === "chat" && win.pendingAskAgent !== "") {
                            const ag = win.pendingAskAgent, msg = win.pendingAskText;
                            win.pendingAskAgent = "";
                            win.pendingAskText = "";
                            Qt.callLater(() => item.askAgent(ag, msg));
                        } else if (modelData.key === "chat" && win.pendingChannel !== "") {
                            const ch = win.pendingChannel;
                            win.pendingChannel = "";
                            Qt.callLater(() => item.selectChannel(ch));
                        } else if (modelData.key === "chat" && win.pendingAgent !== "" && win.pendingConversation !== "") {
                            const ag = win.pendingAgent, cid = win.pendingConversation;
                            win.pendingAgent = "";
                            win.pendingConversation = "";
                            Qt.callLater(() => item.openAgentConv(ag, cid));
                        } else if (modelData.key === "chat" && win.pendingConversation !== "") {
                            const id = win.pendingConversation;
                            win.pendingConversation = "";
                            Qt.callLater(() => item.open(id));
                        }
                        if (modelData.key === "chat" && win.pendingRunId !== "") {
                            const rid = win.pendingRunId, rag = win.pendingRunAgent;
                            win.pendingRunId = "";
                            win.pendingRunAgent = "";
                            Qt.callLater(() => item.openRun(rid, rag));
                        }
                        if (modelData.key === "chat" && win.pendingNewAgent) {
                            win.pendingNewAgent = false;
                            Qt.callLater(() => item.openNewAgent(""));
                        }
                        if (modelData.key === "chat" && win.pendingDocsTab) {
                            win.pendingDocsTab = false;
                            Qt.callLater(() => item.openDocsTab());
                        }
                        if (modelData.key === "config") {
                            item.sheet = win.configSheet;
                            win.configSheet = "";
                            item.section = win.section;
                        } else if (modelData.key === "chat" && win.pendingChatSheet !== "") {
                            const sh = win.pendingChatSheet;
                            win.pendingChatSheet = "";
                            item.sheet = sh;
                        } else if (modelData.key === "skills" && win.startSheet !== "") {
                            item.sheet = win.startSheet; // "skills:nova", "skills:colar" or a skill id
                        } else if (modelData.key === "docs" && win.startSheet !== "") {
                            const doc = win.startSheet; // "docs:<id>" opens that doc
                            Qt.callLater(() => DocBus.open(doc));
                        } else if (modelData.key === "plugins" && win.startSheet !== "") {
                            item.query = win.startSheet; // "plugins:pluggy" opens its form
                            item.expanded = win.startSheet;
                        }
                    }
                    // Quick arrival: the old page is gone at once, the new one
                    // settles in 160 ms, so a click feels answered immediately.
                    opacity: current ? 1 : 0
                    Behavior on opacity { NumberAnimation { duration: 160; easing.type: Easing.OutQuad } }
                    transform: Translate {
                        y: loader.current ? 0 : 4
                        Behavior on y { NumberAnimation { duration: 160; easing.type: Easing.OutQuad } }
                    }
                }
            }
        }
        }

        // Resume banner when guided onboarding was skipped or paused to connect an AI.
        Rectangle {
            visible: shell.canResumeOnboarding && !shell.onboardingOpen
            z: 28
            anchors { top: parent.top; topMargin: Theme.space3; horizontalCenter: pagesClip.horizontalCenter }
            height: 36
            width: resumeRow.implicitWidth + 24
            radius: height / 2
            color: Theme.card
            border.width: 1
            border.color: Theme.focusLine

            Row {
                id: resumeRow
                anchors.centerIn: parent
                spacing: Theme.space2

                Dot {
                    kind: "live"
                    anchors.verticalCenter: parent.verticalCenter
                }
                Text {
                    text: "Abertura guiada em andamento"
                    color: Theme.text2
                    font.pixelSize: Theme.fontSmall
                    anchors.verticalCenter: parent.verticalCenter
                }
                Text {
                    text: "Retomar →"
                    color: Theme.white
                    font.pixelSize: Theme.fontSmall
                    font.weight: Font.DemiBold
                    anchors.verticalCenter: parent.verticalCenter
                }
            }

            MouseArea {
                anchors.fill: parent
                cursorShape: Qt.PointingHandCursor
                onClicked: {
                    shell.onboardingPaused = false;
                    shell.onboardingOpen = true;
                }
            }
        }

        OnboardingPage {
            id: onboardingView
            visible: shell.onboardingOpen
            z: 30
            anchors { top: parent.top; bottom: parent.bottom; left: rail.visible ? rail.right : parent.left; right: parent.right }
            workspaceId: shell.currentWsId
            onSkipped: {
                shell.onboardingOpen = false;
                shell.onboardingPaused = true;
            }
            onConnectAIRequested: {
                shell.onboardingOpen = false;
                shell.onboardingPaused = true;
                win.go("modelos:connect");
            }
            onCompleted: chId => {
                shell.markOnboardingDone();
                shell.checkOnboarding();
                const l = pageLoaders.itemAt(0);
                if (l && l.item) {
                    l.item.loadChannels();
                    l.item.loadAgents(false);
                }
                win.openChannel(chId || "time");
            }
        }
    }

    // ── Title bar: drag to move, double-click to maximize ───────
    Item {
        id: titleBar
        anchors { top: parent.top; left: parent.left; right: parent.right; leftMargin: Api.signedIn ? (rail.visible ? rail.width : 0) + sidebar.width : 0 }
        height: Theme.titleBar
        DragHandler {
            target: null
            onActiveChanged: if (active) win.startSystemMove()
        }
        TapHandler {
            onDoubleTapped: win.visibility === Window.Maximized ? win.showNormal() : win.showMaximized()
        }
    }
    WindowButtons {
        id: winButtons
        window: win
        anchors { top: parent.top; right: parent.right }
    }
    // ── Resize edges ────────────────────────────────────────────
    Repeater {
        model: [
            { e: Qt.LeftEdge, c: Qt.SizeHorCursor },
            { e: Qt.RightEdge, c: Qt.SizeHorCursor },
            { e: Qt.TopEdge, c: Qt.SizeVerCursor },
            { e: Qt.BottomEdge, c: Qt.SizeVerCursor },
            { e: Qt.LeftEdge | Qt.TopEdge, c: Qt.SizeFDiagCursor },
            { e: Qt.RightEdge | Qt.BottomEdge, c: Qt.SizeFDiagCursor },
            { e: Qt.RightEdge | Qt.TopEdge, c: Qt.SizeBDiagCursor },
            { e: Qt.LeftEdge | Qt.BottomEdge, c: Qt.SizeBDiagCursor }
        ]
        delegate: MouseArea {
            required property var modelData
            readonly property int edge: modelData.e
            readonly property bool corner: (edge & (Qt.LeftEdge | Qt.RightEdge)) && (edge & (Qt.TopEdge | Qt.BottomEdge))
            readonly property int grip: 6
            enabled: win.visibility !== Window.Maximized
            cursorShape: modelData.c
            x: (edge & Qt.RightEdge) ? win.width - grip * (corner ? 2 : 1) : 0
            y: (edge & Qt.BottomEdge) ? win.height - grip * (corner ? 2 : 1) : 0
            width: (edge & (Qt.LeftEdge | Qt.RightEdge)) ? grip * (corner ? 2 : 1) : win.width
            height: (edge & (Qt.TopEdge | Qt.BottomEdge)) ? grip * (corner ? 2 : 1) : win.height
            onPressed: win.startSystemResize(edge)
        }
    }
}
