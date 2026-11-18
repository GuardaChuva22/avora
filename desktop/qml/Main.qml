import QtCore
import QtQuick
import QtQuick.Controls.Basic
import QtQuick.Effects
import Avora
import "util.js" as U

// Frameless main window: atmosphere, single unified sidebar, notice bar,
// pages, floating panel, command menu (Ctrl+K), and window chrome.
Window {
    id: win
    property string startPage: ""
    property string autoServer: ""
    property string autoToken: ""

    // Settings sections opened via go("pessoas"), go("modelos:connect"), or go("config:pessoas").
    readonly property var sections: ["pessoas", "modelos", "heartbeat", "noite", "atividade", "nos"]
    readonly property var directPages: [
        "home", "waiting", "docs", "goals", "team",
        "routines", "tasks", "teammemory", "plugins", "skills",
        "config", "chat"
    ]
    readonly property string startBase: startPage.split(":")[0]
    readonly property string startSheet: startPage.split(":").slice(1).join(":")

    property string openPref: "home"
    property string onboardingDoneJson: "{}"
    property string page: directPages.indexOf(startBase) >= 0 ? startBase : sections.indexOf(startBase) >= 0 ? "config" : "home"
    property string section: sections.indexOf(startBase) >= 0 ? startBase : "modelos"
    property var chatState: ({ kind: "", id: "" })
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

    onPageChanged: {
        if (page === "docs" && floater.shown)
            closeFloat();
    }

    Connections {
        target: DocBus
        function onOpenRequested(id) {
            DocBus.openId = id;
            if (win.page !== "docs")
                win.openFloat("DocPanel.qml", { docId: id, embedded: true });
        }
        function onListRequested() {
            DocBus.openId = "";
            if (win.page === "chat")
                win.openFloat("DocPanel.qml", { docId: "", embedded: true });
            else
                win.go("docs");
        }
        function onCiteRequested(doc) {
            win.closeFloat();
            if (win.page !== "chat")
                win.go("chat");
        }
    }

    Shortcut {
        sequence: "Ctrl+B"
        onActivated: win.compact = !win.compact
    }

    Shortcut {
        sequence: "Ctrl+K"
        enabled: Api.signedIn
        onActivated: win.openCommand()
    }

    width: 1320
    height: 860
    minimumWidth: 980
    minimumHeight: 640
    visible: true
    color: Theme.bg
    title: "Avora"
    flags: Qt.Window | Qt.FramelessWindowHint

    property string configSheet: sections.indexOf(startBase) >= 0 ? startSheet : (startBase === "config" ? startSheet : "")
    property string pendingChatSheet: startBase === "chat" ? startSheet : ""
    property var pendingChatAction: null

    function pageItem(key) {
        for (let i = 0; i < pageLoaders.count; i++) {
            const l = pageLoaders.itemAt(i);
            if (l && l.modelData && l.modelData.key === key)
                return l.item || null;
        }
        return null;
    }

    function deliverConfig(sec, sh) {
        const cfg = pageItem("config");
        if (cfg && cfg.deliver !== undefined) {
            if (sec !== "")
                cfg.section = sec;
            if (sh !== "") {
                cfg.sheet = sh;
                configSheet = "";
            }
        }
    }

    // go opens a page; "modelos:connect" or "config:pessoas" also selects that settings section/sheet.
    function go(p) {
        if (!p)
            return;
        const parts = String(p).split(":");
        const base = parts[0];
        const sub = parts[1] || "";
        const extra = parts.slice(2).join(":");

        if (base === "config" && sub !== "" && sections.indexOf(sub) >= 0) {
            section = sub;
            configSheet = extra;
            page = "config";
            deliverConfig(sub, extra);
            return;
        }

        if (sections.indexOf(base) >= 0) {
            section = base;
            configSheet = sub;
            page = "config";
            deliverConfig(base, sub);
            return;
        }

        page = base;
        if (base === "chat" && sub !== "") {
            const chatItem = pageItem("chat");
            if (chatItem && chatItem.sheet !== undefined)
                chatItem.sheet = sub;
            else
                pendingChatSheet = sub;
        }
    }

    function applyChatAction(item, act) {
        if (!item || !act)
            return;
        if (act.kind === "agent") {
            if (typeof item.openAgent === "function")
                item.openAgent(act.id);
            else if (typeof item.selectAgent === "function")
                item.selectAgent(act.id);
        } else if (act.kind === "channel") {
            if (typeof item.openChannel === "function")
                item.openChannel(act.id);
            else if (typeof item.selectChannel === "function")
                item.selectChannel(act.id);
        } else if (act.kind === "person") {
            if (typeof item.openPerson === "function")
                item.openPerson(act.id);
        } else if (act.kind === "agentConv") {
            if (act.convId && typeof item.openAgentConv === "function")
                item.openAgentConv(act.id, act.convId);
            else if (typeof item.openAgent === "function")
                item.openAgent(act.id);
            else if (typeof item.selectAgent === "function")
                item.selectAgent(act.id);
        } else if (act.kind === "conv") {
            if (typeof item.open === "function")
                item.open(act.id);
        } else if (act.kind === "ask") {
            if (typeof item.askAgent === "function")
                item.askAgent(act.id, act.text);
            else if (typeof item.openAgent === "function")
                item.openAgent(act.id);
        }
    }

    function openAgent(id) {
        if (!id)
            return;
        chatState = { kind: "agent", id: id };
        go("chat");
        const item = pageItem("chat");
        const act = { kind: "agent", id: id };
        if (item)
            applyChatAction(item, act);
        else
            pendingChatAction = act;
    }

    function openChannel(id) {
        const ch = id || "time";
        chatState = { kind: "channel", id: ch };
        go("chat");
        const item = pageItem("chat");
        const act = { kind: "channel", id: ch };
        if (item)
            applyChatAction(item, act);
        else
            pendingChatAction = act;
    }

    function openPerson(personId) {
        if (!personId)
            return;
        chatState = { kind: "person", id: personId };
        go("chat");
        const item = pageItem("chat");
        const act = { kind: "person", id: personId };
        if (item)
            applyChatAction(item, act);
        else
            pendingChatAction = act;
    }

    function openFloat(url, props) {
        if (!url)
            return;
        floater.show(url, props || {});
    }

    function closeFloat() {
        floater.hide();
    }

    function openCommand() {
        command.open();
    }

    function newAgent(project) {
        newAgentSheet.open(project || "");
    }

    function openNewAgent(project) {
        newAgent(project);
    }

    function invite() {
        inviteSheet.open(shell.currentWsId, shell.currentWsName);
    }

    function openWorkspaceSheet(initialTab) {
        workspaceSheet.open(initialTab || "create");
    }

    function newChannel() {
        go("chat:canal");
        const item = pageItem("chat");
        if (item && typeof item.newChannel === "function")
            item.newChannel();
    }

    function openConversation(id) {
        if (!id)
            return;
        go("chat");
        const item = pageItem("chat");
        const act = { kind: "conv", id: id };
        if (item)
            applyChatAction(item, act);
        else
            pendingChatAction = act;
    }

    function openAgentConv(agentId, convId) {
        if (!agentId)
            return;
        chatState = { kind: "agent", id: agentId };
        go("chat");
        const item = pageItem("chat");
        const act = { kind: "agentConv", id: agentId, convId: convId || "" };
        if (item)
            applyChatAction(item, act);
        else
            pendingChatAction = act;
    }

    function askAgent(agentId, text) {
        if (!agentId)
            return;
        chatState = { kind: "agent", id: agentId };
        go("chat");
        const item = pageItem("chat");
        const act = { kind: "ask", id: agentId, text: text || "" };
        if (item)
            applyChatAction(item, act);
        else
            pendingChatAction = act;
    }

    function openRun(runId, agentId) {
        if (!runId)
            return;
        openFloat("RunPane.qml", { runId: runId, agents: sidebar.agents });
    }

    function applyOpenPreference() {
        if (startPage !== "")
            return;
        if (openPref === "chat")
            go("chat");
        else if (openPref.startsWith("channel:"))
            openChannel(openPref.slice(8));
        else if (openPref.startsWith("agent:"))
            openAgent(openPref.slice(6));
        else
            go("home");
    }

    // poolAgy combines multiple Antigravity accounts into one summary ring (D61).
    function poolAgy(list) {
        const agy = list.filter(q => q.engine === "antigravity");
        if (agy.length < 2)
            return list;
        const ok = agy.filter(q => !q.error);
        const avg = key => {
            const xs = ok.filter(q => q[key]);
            if (!xs.length)
                return null;
            let used = 0, reset = 0;
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
        let placed = false;
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

        readonly property var mePerson: (Api.me && Api.me.person && Api.me.person.id) ? Api.me.person : null
        readonly property var wsList: (Api.me && Api.me.workspaces) ? Api.me.workspaces : []
        readonly property string currentWsId: Api.workspace !== "" ? Api.workspace
            : (wsList.length > 0 ? wsList[0].id : "pessoal")
        readonly property var currentWs: wsList.find(w => w.id === currentWsId) || (wsList.length > 0 ? wsList[0] : null)
        readonly property string currentWsName: currentWs && currentWs.name ? currentWs.name : "Pessoal"

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

        function menuIsOpen(m) {
            if (!m)
                return false;
            if (m.opened !== undefined)
                return !!m.opened;
            if (m.shown !== undefined)
                return !!m.shown;
            return !!m.visible;
        }

        function closeMenu(m) {
            if (!m)
                return;
            if (typeof m.close === "function")
                m.close();
            else if (m.shown !== undefined)
                m.shown = false;
            else
                m.visible = false;
        }

        function toggleMenu(m, otherMenu, anchorBtn) {
            if (menuIsOpen(otherMenu))
                closeMenu(otherMenu);
            if (menuIsOpen(m)) {
                closeMenu(m);
                return;
            }
            if (m.opened !== undefined)
                m.open();
            else if (typeof m.open === "function")
                m.open(anchorBtn);
            else if (m.shown !== undefined)
                m.shown = true;
            else
                m.visible = true;
        }

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
            closeMenu(workspaceMenu);
            closeMenu(accountMenu);
            win.closeFloat();
            win.chatState = { kind: "", id: "" };
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
                    win.closeFloat();
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

        Component.onCompleted: {
            if (Api.signedIn)
                shell.checkOnboarding();
            if (workspaceMenu.createWorkspaceRequested)
                workspaceMenu.createWorkspaceRequested.connect(() => workspaceSheet.open("create"));
            if (workspaceMenu.joinWorkspaceRequested)
                workspaceMenu.joinWorkspaceRequested.connect(() => workspaceSheet.open("join"));
            if (workspaceMenu.workspaceSheetRequested)
                workspaceMenu.workspaceSheetRequested.connect(tab => workspaceSheet.open(tab || "create"));
        }

        Timer {
            interval: 30000
            running: shell.visible
            repeat: true
            onTriggered: Api.refreshMe()
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

        Sidebar {
            id: sidebar
            z: 2 // compact tooltips float over the pages
            anchors { top: parent.top; bottom: parent.bottom; left: parent.left }
            compact: win.compact
            workspaceMenuOpen: shell.menuIsOpen(workspaceMenu)
            accountMenuOpen: shell.menuIsOpen(accountMenu)
            onWorkspaceClicked: shell.toggleMenu(workspaceMenu, accountMenu, sidebar.workspaceButton)
            onAccountClicked: shell.toggleMenu(accountMenu, workspaceMenu, sidebar.accountButton)
        }

        // Pages are laid out at the sidebar's final width at once, and this
        // clip reveals or covers the strip while the sidebar animates.
        Item {
            id: pagesClip
            z: 1
            anchors { top: parent.top; bottom: parent.bottom; left: sidebar.right; right: parent.right }
            clip: true

            Item {
                id: pages
                readonly property real edge: win.compact ? sidebar.slim : sidebar.full
                x: edge - sidebar.width
                width: win.width - edge
                height: parent.height

                NoticeBar {
                    id: noticeBar
                    z: 5
                    anchors { top: parent.top; left: parent.left; right: parent.right }
                }

                // Pages stay alive once opened, so a running chat keeps streaming.
                Item {
                    id: pageHost
                    readonly property real topOffset: noticeBar.visible ? noticeBar.height : 0
                    y: topOffset
                    width: parent.width
                    height: Math.max(0, parent.height - topOffset)

                    Repeater {
                        id: pageLoaders
                        model: [
                            { key: "chat", url: "ChatPage.qml" },
                            { key: "home", url: "HomePage.qml" },
                            { key: "waiting", url: "WaitingPage.qml" },
                            { key: "docs", url: "DocsPage.qml" },
                            { key: "goals", url: "GoalsPage.qml" },
                            { key: "team", url: "TeamPage.qml" },
                            { key: "routines", url: "RoutinesPage.qml" },
                            { key: "tasks", url: "TaskManager.qml" },
                            { key: "teammemory", url: "TeamMemoryPage.qml" },
                            { key: "plugins", url: "PluginsPage.qml" },
                            { key: "skills", url: "SkillsPage.qml" },
                            { key: "config", url: "SettingsPage.qml" }
                        ]
                        delegate: Loader {
                            id: loader
                            required property var modelData
                            readonly property bool current: win.page === modelData.key
                            property bool opened: false
                            onCurrentChanged: if (current) opened = true
                            Component.onCompleted: if (current) opened = true
                            anchors.fill: parent
                            active: opened && Api.signedIn
                            visible: current
                            source: modelData.url
                            onLoaded: {
                                if (item.goPage)
                                    item.goPage.connect(win.go);
                                if (modelData.key === "goals" && item.openConversation)
                                    item.openConversation.connect(win.openConversation);
                                if (modelData.key === "home") {
                                    if (item.askAgent)
                                        item.askAgent.connect(win.askAgent);
                                    if (item.openAgentConv)
                                        item.openAgentConv.connect(win.openAgentConv);
                                    if (item.openChannel)
                                        item.openChannel.connect(win.openChannel);
                                    if (item.openRun)
                                        item.openRun.connect(win.openRun);
                                    if (item.newAgentRequested)
                                        item.newAgentRequested.connect(() => win.newAgent(""));
                                }
                                if (modelData.key === "waiting" && item.openAgentConv)
                                    item.openAgentConv.connect(win.openAgentConv);
                                if (modelData.key === "tasks") {
                                    if (item.openAgentConv)
                                        item.openAgentConv.connect(win.openAgentConv);
                                    if (item.openChannel)
                                        item.openChannel.connect(win.openChannel);
                                }
                                if (modelData.key === "chat") {
                                    if (item.opened)
                                        item.opened.connect((kind, id) => {
                                            win.chatState = { kind: kind || "", id: id || "" };
                                        });
                                    if (win.pendingChatAction !== null) {
                                        const act = win.pendingChatAction;
                                        win.pendingChatAction = null;
                                        Qt.callLater(() => win.applyChatAction(item, act));
                                    }
                                    if (win.pendingChatSheet !== "" && item.sheet !== undefined) {
                                        const sh = win.pendingChatSheet;
                                        win.pendingChatSheet = "";
                                        item.sheet = sh;
                                    }
                                }
                                if (modelData.key === "config") {
                                    item.section = win.section;
                                    if (win.configSheet !== "") {
                                        item.sheet = win.configSheet;
                                        win.configSheet = "";
                                    }
                                } else if (modelData.key === "skills" && win.startSheet !== "" && item.sheet !== undefined) {
                                    item.sheet = win.startSheet;
                                } else if (modelData.key === "docs" && win.startSheet !== "") {
                                    const doc = win.startSheet;
                                    Qt.callLater(() => DocBus.open(doc));
                                } else if (modelData.key === "plugins" && win.startSheet !== "") {
                                    if (item.query !== undefined)
                                        item.query = win.startSheet;
                                    if (item.expanded !== undefined)
                                        item.expanded = win.startSheet;
                                }
                            }
                            opacity: current ? 1 : 0
                            Behavior on opacity { NumberAnimation { duration: Theme.medium; easing.type: Theme.ease } }
                            transform: Translate {
                                y: loader.current ? 0 : Theme.space1
                                Behavior on y { NumberAnimation { duration: Theme.medium; easing.type: Theme.ease } }
                            }
                        }
                    }
                }
            }
        }

        // Menus from T4 anchored to the sidebar's workspace card and account footer.
        WorkspaceMenu {
            id: workspaceMenu
            z: 35
            x: Theme.space3
            y: sidebar.workspaceMenuY
        }

        AccountMenu {
            id: accountMenu
            z: 35
            x: Theme.space3
            y: Math.max(Theme.space3, sidebar.accountMenuY - (height > 0 ? height : implicitHeight) - Theme.space2)
        }

        // Resume banner when guided onboarding was skipped or paused to connect an AI.
        Rectangle {
            visible: shell.canResumeOnboarding && !shell.onboardingOpen
            z: 28
            anchors {
                top: parent.top
                topMargin: (noticeBar.visible ? noticeBar.height : 0) + Theme.space3
                horizontalCenter: pagesClip.horizontalCenter
            }
            height: 36
            width: resumeRow.implicitWidth + Theme.space5
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
            anchors.fill: parent
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
                sidebar.reload();
                const chatItem = win.pageItem("chat");
                if (chatItem) {
                    if (typeof chatItem.loadChannels === "function")
                        chatItem.loadChannels();
                    if (typeof chatItem.loadAgents === "function")
                        chatItem.loadAgents(false);
                }
                win.openChannel(chId || "time");
            }
        }

        // Single window-wide floating panel (T3) for AgentPage, DocPanel, RunPane, threads.
        FloatingPanel {
            id: floater
            z: 40
            anchors.fill: parent
        }

        // Workspace, NewAgent and Invite modal sheets (one instance each in Main).
        WorkspaceSheet {
            id: workspaceSheet
            anchors.fill: parent
        }

        NewAgentSheet {
            id: newAgentSheet
            anchors.fill: parent
            onCreated: (agent, convId) => {
                sidebar.reload();
                shell.checkOnboarding();
                if (agent && agent.id) {
                    if (convId)
                        win.openAgentConv(agent.id, convId);
                    else
                        win.openAgent(agent.id);
                }
            }
        }

        InviteSheet {
            id: inviteSheet
            anchors.fill: parent
        }

        // Global Ctrl+K command palette (T3).
        CommandMenu {
            id: command
            z: 60
            anchors.fill: parent
        }
    }

    // ── Title bar: drag to move, double-click to maximize ───────
    Item {
        id: titleBar
        anchors { top: parent.top; left: parent.left; right: parent.right; leftMargin: Api.signedIn ? sidebar.width : 0 }
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
