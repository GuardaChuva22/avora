import QtQuick
import QtQuick.Controls.Basic
import QtQuick.Effects
import QtCore
import QtQuick.Dialogs
import Avora
import "util.js" as U
import "logos.js" as Logos

// Full-area conversation view: header + messages + composer + context-sensitive
// right column (ChannelMembers for a channel, AgentSide for an agent).
// Navigation lives in the main window's Sidebar; detail views open in FloatingPanel.
Item {
    id: chat
    property var agents: []
    signal goPage(string name)
    signal opened(string kind, string id)

    // "@Name" in an answer becomes a chip in that agent's color.
    readonly property var teamChips: agents.map(a => ({ name: a.name, color: a.color || "" }))
    property string agentId: ""
    property bool channel: false
    property string channelId: "time"
    property bool personMode: false
    property string personId: ""
    property bool explicitTarget: false

    property var channels: [] // GET /v1/channels
    readonly property var channelNow: channels.find(c => c.id === channelId) || null
    readonly property var channelIds: channels.map(c => c.id)
    property string convId: ""
    property string error: ""

    readonly property string currentKind: personMode ? "person" : channel ? "channel" : (agentId !== "" ? "agent" : "")

    function emitOpened(kind, id) {
        if (typeof win !== "undefined" && win && "chatState" in win)
            win.chatState = { kind: kind, id: id };
        opened(kind, id);
    }

    // ── Floating panel helpers ──────────────────────────────────
    function openAgentPage(targetId, tabName) {
        const id = targetId || agentId;
        if (!id)
            return;
        if (typeof win !== "undefined" && win && typeof win.openFloat === "function") {
            win.openFloat("AgentPage.qml", {
                agentId: id,
                agents: chat.agents,
                projects: chat.projects,
                shown: true,
                tab: tabName || "overview"
            });
        }
    }

    function openDocFloat(docId) {
        DocBus.openId = docId || "";
        if (typeof win !== "undefined" && win && typeof win.openFloat === "function")
            win.openFloat("DocPanel.qml", { agents: chat.agents });
    }

    function openBrowserFloat(targetId, targetName) {
        const id = targetId || agentId;
        if (!id)
            return;
        const ag = agents.find(a => a.id === id) || null;
        const name = targetName || (ag ? ag.name : "");
        if (typeof win !== "undefined" && win && typeof win.openFloat === "function")
            win.openFloat("BrowserPanel.qml", { agentId: id, agentName: name, open: true });
    }

    // ── Attachments & links ─────────────────────────────────────
    // Files waiting to go with the next message: {id, name, mime, size}.
    property var attachments: []
    property int uploading: 0
    function attachFiles(urls) {
        for (const u of urls) {
            uploading++;
            Api.uploadFile(convId, u, (err, f) => {
                uploading--;
                if (err)
                    error = err;
                else
                    attachments = attachments.concat([f]);
            });
        }
        composer.area.forceActiveFocus();
    }
    // pasteImage uploads the clipboard's image; false lets the text paste.
    function pasteImage() {
        if (!Api.clipboardHasImage())
            return false;
        uploading++;
        Api.uploadClipboardImage(convId, (err, f) => {
            uploading--;
            if (err)
                error = err;
            else
                attachments = attachments.concat([f]);
        });
        return true;
    }
    function attachmentText() {
        return attachments.map(f => {
            const name = f.name.replace(/[\[\]()]/g, "");
            return f.mime.startsWith("image/") ? "![" + name + "](avora:file/" + f.id + ")"
                                               : "[📎 " + name + "](avora:file/" + f.id + "#" + encodeURIComponent(f.name) + ")";
        }).join("\n");
    }
    function openLink(link) {
        if (link.startsWith("avora:doc/")) {
            openDocFloat(link.slice(10).split(/[#?]/)[0]);
            return;
        }
        if (link.startsWith("avora:file/")) {
            const rest = link.slice(11), hash = rest.indexOf("#");
            const id = hash < 0 ? rest : rest.slice(0, hash);
            const name = hash < 0 ? id : decodeURIComponent(rest.slice(hash + 1));
            Api.openFile(id, name, err => { if (err) error = err; });
            return;
        }
        if (link.startsWith("avora:setup/")) {
            const parts = link.slice(12).split("/");
            const kind = parts[0] || "";
            const targetId = parts[1] || chat.agentId;
            if (kind === "connect") {
                if (typeof win !== "undefined" && win && typeof win.go === "function")
                    win.go("modelos:connect");
                else
                    chat.goPage("modelos:connect");
            } else if (kind === "brain" && targetId !== "") {
                openAgentPage(targetId, "settings");
            } else if (kind === "tools" && targetId !== "") {
                openAgentPage(targetId, "tools");
            } else if (kind === "channels") {
                const targetAg = chat.agents.find(x => x.id === targetId) || chat.agent;
                chanEditor.open(null, targetAg ? (targetAg.project_id || "") : "");
            } else if (kind === "routine") {
                const targetAg = chat.agents.find(x => x.id === targetId) || chat.agent;
                if (targetAg)
                    routineEditor.open(targetAg, null, null);
            }
            return;
        }
        Qt.openUrlExternally(link);
    }

    property bool stick: true
    property string streamConv: "" // native stream in flight
    // The work line above the composer: when the open conversation's
    // answer started and its latest step.
    property real workSince: 0
    property string workStep: ""
    property real workNow: Date.now()
    readonly property bool workingHere: convId !== "" && (!!live[convId] || (Api.streaming && streamConv === convId))
    onWorkingHereChanged: {
        workNow = Date.now();
        if (workingHere && workSince === 0)
            workSince = live[convId] && live[convId].since ? live[convId].since : Date.now();
        if (!workingHere) {
            workSince = 0;
            workStep = "";
        }
    }
    onConvIdChanged: {
        workSince = live[convId] && live[convId].since ? live[convId].since : (workingHere ? Date.now() : 0);
        const st = live[convId] ? live[convId].steps : [];
        workStep = st && st.length ? st[st.length - 1].tool : "";
        applyRightPrefs();
    }
    Timer {
        interval: 1000
        repeat: true
        running: chat.workingHere && Qt.application.state === Qt.ApplicationActive
        onTriggered: chat.workNow = Date.now()
    }

    property var live: ({}) // convId -> { job, after, steps: [] }
    property var seen: ({}) // agentId -> ISO time last read
    property var projects: []
    property var sectors: []
    property var collapsed: ({}) // section key -> true when folded

    readonly property var agent: agents.find(a => a.id === agentId) || null
    readonly property bool isNative: true
    readonly property bool nativeBusy: Api.streaming
    readonly property bool working: convId !== "" && live[convId] !== undefined
    readonly property bool busy: (nativeBusy && streamConv === convId) || working
    readonly property bool empty: msgs.count === 0
    property bool loadingConv: false

    // ── Per-conversation right column state ─────────────────────
    readonly property real minRightWidth: 280
    readonly property real maxRightWidth: 420
    property bool rightOpen: width >= 920
    property real rightWidth: 320
    property string rightPrefsStore: "{}"
    property var rightPrefs: ({})
    property bool rightPrefsLoaded: false
    property bool restoringRightPrefs: false

    readonly property string rightConvKey: {
        if (personMode)
            return "";
        if (channel)
            return "#" + channelId;
        if (convId !== "")
            return convId;
        return agentId !== "" ? ("agent:" + agentId) : "";
    }
    readonly property string rightFallbackKey: !personMode && !channel && agentId !== "" ? ("agent:" + agentId) : ""
    readonly property real effectiveRightWidth: {
        if (personMode || !rightOpen)
            return 0;
        const maxAllowed = Math.max(minRightWidth, Math.min(maxRightWidth, chat.width - 360));
        return Math.max(minRightWidth, Math.min(maxAllowed, rightWidth));
    }

    function clampRightWidth(w) {
        return Math.max(minRightWidth, Math.min(maxRightWidth, Math.round(w || 320)));
    }

    function ensureRightPrefsLoaded() {
        if (rightPrefsLoaded)
            return;
        rightPrefsLoaded = true;
        try {
            rightPrefs = JSON.parse(rightPrefsStore || "{}") || {};
        } catch (e) {
            rightPrefs = {};
        }
    }

    function applyRightPrefs() {
        ensureRightPrefsLoaded();
        if (rightConvKey === "")
            return;
        restoringRightPrefs = true;
        const p = rightPrefs[rightConvKey] || (rightFallbackKey !== "" ? rightPrefs[rightFallbackKey] : null);
        if (p) {
            if (p.width !== undefined)
                rightWidth = clampRightWidth(p.width);
            if (p.open !== undefined)
                rightOpen = !!p.open;
        } else {
            rightWidth = clampRightWidth(rightWidth);
        }
        restoringRightPrefs = false;
    }

    function saveRightPrefs() {
        if (restoringRightPrefs || rightConvKey === "")
            return;
        ensureRightPrefsLoaded();
        const entry = {
            open: rightOpen,
            width: clampRightWidth(rightWidth)
        };
        const next = Object.assign({}, rightPrefs);
        next[rightConvKey] = entry;
        if (rightFallbackKey !== "")
            next[rightFallbackKey] = entry;
        const keys = Object.keys(next);
        if (keys.length > 240)
            delete next[keys[0]];
        rightPrefs = next;
        rightPrefsStore = JSON.stringify(next);
    }

    function toggleRightColumn() {
        rightOpen = !rightOpen;
        saveRightPrefs();
    }

    onRightConvKeyChanged: applyRightPrefs()

    Settings {
        category: "chat"
        property alias lastAgent: chat.agentId
        property alias lastChannel: chat.channel
        property alias lastChannelId: chat.channelId
        property alias seenJson: chat.seenStore
        property alias collapsedJson: chat.collapsedStore
        property alias rightPrefsJson: chat.rightPrefsStore
    }
    property string seenStore: "{}"
    property string collapsedStore: "{}"

    Component.onCompleted: {
        if (DocBus.pendingCite)
            Qt.callLater(() => chat.citeDoc(DocBus.pendingCite));
        try {
            seen = JSON.parse(seenStore);
            collapsed = JSON.parse(collapsedStore);
        } catch (e) {
            seen = {};
        }
        ensureRightPrefsLoaded();
        applyRightPrefs();
        loadProjects();
        loadSectors();
        loadChannels();
        loadAgents(true);
    }

    // ── People (for openPerson DM empty state) ──────────────────
    property var workspaceMembers: []

    function currentWorkspaceId() {
        if (Api.workspace !== "")
            return Api.workspace;
        if (Api.me && Api.me.workspaces && Api.me.workspaces.length > 0)
            return Api.me.workspaces[0].id;
        return "pessoal";
    }

    function loadWorkspaceMembers() {
        if (!Api.signedIn)
            return;
        const ws = currentWorkspaceId();
        Api.get("/v1/workspaces/" + encodeURIComponent(ws) + "/members", (err, list) => {
            if (!err && Array.isArray(list))
                chat.workspaceMembers = list;
        });
    }

    readonly property var currentPerson: {
        const pid = chat.personId;
        if (!chat.personMode || pid === "")
            return null;
        if (Api.me && Api.me.person && (pid === "self" || Api.me.person.id === pid)) {
            const me = Api.me.person;
            return {
                person_id: me.id,
                name: me.name || me.email || "Você",
                avatar: me.avatar || "",
                online: me.presence !== "invisible",
                presence: me.presence || "auto",
                status_text: me.status_text || ""
            };
        }
        const found = (chat.workspaceMembers || []).find(m => (m.person_id || m.id) === pid);
        if (found)
            return found;
        if (pid === "self")
            return { person_id: "self", name: "Você", avatar: "", online: true, presence: "auto", status_text: "" };
        return { person_id: pid, name: pid, avatar: "", online: false, presence: "auto", status_text: "" };
    }
    readonly property string currentPersonName: currentPerson && currentPerson.name ? currentPerson.name : (personId || "esta pessoa")

    function personPhoto(av) {
        if (!av)
            return "";
        if (av.startsWith("avora:file/"))
            return "image://server/v1/files/" + av.slice(11);
        if (av.startsWith("/v1/files/"))
            return "image://server" + av;
        return av;
    }

    // ── Channels (D54) ──────────────────────────────────────────
    function loadChannels() {
        Api.get("/v1/channels", (err, data) => {
            if (err)
                return;
            if (!U.same(channels, data))
                channels = data;
            if (!data.some(c => c.id === channelId))
                channelId = "time";
        });
    }
    function channelUnreadOf(c) {
        return !!c && !(channel && channelId === c.id) && !!c.last_at && c.preview !== "" &&
               (!seen["#" + c.id] || seen["#" + c.id] < c.last_at);
    }
    function channelsOf(pid) {
        const known = projects.map(p => p.id);
        return channels.filter(c => pid === "" ? !known.includes(c.project_id || "") : c.project_id === pid);
    }
    Connections {
        target: Api
        function onWorkspaceChanged() {
            chat.sectors = [];
            chat.workspaceMembers = [];
            chat.loadSectors();
            chat.loadProjects();
            chat.loadChannels();
            chat.loadAgents(true);
        }
        function onLiveEvent(e) {
            if (!e)
                return;
            if (e.type === "channel.saved" || e.type === "message.created" && chat.channelIds.includes((e.payload || {}).conversation_id))
                chanReload.restart();
            if (e.type === "sector.saved" || e.type === "sector.deleted")
                chat.loadSectors();
        }
    }
    Timer {
        id: chanReload
        interval: 400
        onTriggered: chat.loadChannels()
    }
    Timer {
        interval: Api.live ? 30000 : 8000
        repeat: true
        running: chat.visible
        onTriggered: chat.loadChannels()
    }

    // ── Projects & sectors (functions kept for callers) ─────────
    property string sheet: ""
    onProjectsChanged: openSheet()
    onSheetChanged: if (sheet !== "") openSheet()
    onAgentsChanged: if (sheet !== "") openSheet()
    onChannelsChanged: if (sheet.startsWith("canal-") || sheet.startsWith("ver-")) openSheet()
    function openSheet() {
        if (sheet === "conversas")
            convOpen.start();
        else if (sheet === "time")
            openChannel("time");
        else if (sheet === "canal")
            chanEditor.open(null, "");
        else if (sheet.startsWith("ver-")) {
            if (!channels.some(x => x.id === sheet.slice(4)))
                return;
            openChannel(sheet.slice(4));
        } else if (sheet.startsWith("canal-editar-")) {
            const c = channels.find(x => x.id === sheet.slice(13));
            if (!c)
                return;
            openChannel(c.id);
            chanEditor.open(c, "");
        } else if (sheet.startsWith("editar-")) {
            const a = agents.find(x => x.id === sheet.slice(7));
            if (!a)
                return;
            openAgentPage(a.id, "settings");
        } else if (sheet.startsWith("agente-")) {
            const parts = sheet.slice(7).split(".");
            if (!agents.find(x => x.id === parts[0]))
                return;
            openAgent(parts[0]);
            openAgentPage(parts[0], parts[1] || "overview");
        } else {
            return;
        }
        sheet = "";
    }
    Timer {
        id: convOpen
        interval: 250
        onTriggered: chatHeader.openConversations()
    }
    readonly property var project: agent && agent.project_id ? projects.find(p => p.id === agent.project_id) || null : null

    function loadProjects() {
        Api.get("/v1/projects", (err, data) => {
            if (!err)
                projects = data;
        });
    }

    function loadSectors() {
        Api.get("/v1/sectors", (err, data) => {
            if (!err && data)
                chat.sectors = data;
        });
    }

    function togglePin(a) {
        if (!a)
            return;
        const pinnedCount = chat.agents.filter(x => !!x.pinned).length;
        if (!a.pinned && pinnedCount >= 5) {
            chat.error = "Você pode fixar até 5 funcionários no topo.";
            return;
        }
        const next = !a.pinned;
        chat.agents = chat.agents.map(x => x.id === a.id ? Object.assign({}, x, { pinned: next }) : x);
        Api.patch("/v1/agents/" + encodeURIComponent(a.id), { pinned: next }, err => {
            if (err)
                chat.error = err;
            chat.loadAgents(false);
        });
    }

    function moveAgentToSector(a, sectorId) {
        if (!a || (a.sector || "") === sectorId)
            return;
        chat.agents = chat.agents.map(x => x.id === a.id ? Object.assign({}, x, { sector: sectorId }) : x);
        if (sectorId !== "" && chat.collapsed["sector:" + sectorId])
            chat.toggleProject("sector:" + sectorId);
        Api.patch("/v1/agents/" + encodeURIComponent(a.id), { sector: sectorId }, err => {
            if (err)
                chat.error = err;
            chat.loadAgents(false);
        });
    }

    function leaderName(leadId) {
        if (!leadId)
            return "";
        const found = chat.agents.find(x => x.id === leadId);
        return found ? found.name : "";
    }

    function toggleProject(id) {
        const c = Object.assign({}, collapsed);
        if (c[id])
            delete c[id];
        else
            c[id] = true;
        collapsed = c;
        collapsedStore = JSON.stringify(c);
    }

    function moveAgent(a, projectId) {
        if (!a || (a.project_id || "") === projectId)
            return;
        const id = a.id;
        agents = agents.map(x => x.id === id ? Object.assign({}, x, { project_id: projectId }) : x);
        if (projectId !== "" && collapsed[projectId])
            toggleProject(projectId);
        Api.put("/v1/agents/" + id + "/project", { project_id: projectId }, err => {
            if (err)
                error = err;
            loadAgents(false);
        });
    }

    ListModel { id: convs }
    property var convItems: []
    ListModel { id: msgs }

    // ── Data & contract navigation ──────────────────────────────
    function engineLabel(a) {
        return "nenhuma conversa ainda";
    }

    function unread(a) {
        return !!a.last_at && a.last_role === "assistant" && (channel || personMode || a.id !== agentId) && (!seen[a.id] || seen[a.id] < a.last_at);
    }

    function markSeen(id) {
        const a = agents.find(x => x.id === id);
        if (!a || !a.last_at)
            return;
        const s = Object.assign({}, seen);
        s[id] = a.last_at;
        seen = s;
        seenStore = JSON.stringify(s);
    }

    property var archivedAgents: []
    function loadArchived() {
        Api.get("/v1/agents/archived", (err, data) => {
            if (!err && !U.same(chat.archivedAgents, data || []))
                chat.archivedAgents = data || [];
        });
    }

    function loadAgents(first) {
        if (first)
            loadArchived();
        Api.get("/v1/agents", (err, data) => {
            if (err) {
                error = err;
                return;
            }
            if (first || !U.same(agents, data))
                agents = data;
            if (first) {
                if (pendingAskAgent !== "" && agents.some(a => a.id === pendingAskAgent)) {
                    const askId = pendingAskAgent, askMsg = pendingAskText;
                    pendingAskAgent = "";
                    pendingAskText = "";
                    Qt.callLater(() => chat.askAgent(askId, askMsg));
                    return;
                }
                if (personMode) {
                    emitOpened("person", personId);
                    return;
                }
                if (channel) {
                    seeChannel();
                    emitOpened("channel", channelId);
                    return;
                }
                if (explicitTarget && agentId !== "" && (loadingConv || convItems.length > 0)) {
                    markSeen(agentId);
                    emitOpened("agent", agentId);
                    return;
                }
                selectAgent(agents.find(a => a.id === agentId) ? agentId : (agents.length ? agents[0].id : ""));
            } else if (pendingAskAgent !== "" && agents.some(a => a.id === pendingAskAgent)) {
                const askId = pendingAskAgent, askMsg = pendingAskText;
                pendingAskAgent = "";
                pendingAskText = "";
                Qt.callLater(() => chat.askAgent(askId, askMsg));
            } else if (!channel && !personMode) {
                markSeen(agentId);
            }
        });
    }

    readonly property var channelConv: room.status ? room.status.conversation : null

    function seeChannel() {
        if (!channelConv)
            return;
        const s = Object.assign({}, seen);
        s["#" + channelId] = channelConv.updated_at;
        seen = s;
        seenStore = JSON.stringify(s);
    }

    // Contract: openAgent(id), openChannel(id), openPerson(personId)
    function openAgent(id) {
        explicitTarget = true;
        personMode = false;
        personId = "";
        selectAgent(id || "");
    }

    function openChannel(id) {
        if (!id || id === "") {
            chanEditor.open(null, "");
            return;
        }
        explicitTarget = true;
        personMode = false;
        personId = "";
        channelId = id;
        channel = true;
        seeChannel();
        applyRightPrefs();
        emitOpened("channel", id);
    }

    function openPerson(id) {
        if (!id)
            return;
        explicitTarget = true;
        channel = false;
        personMode = true;
        personId = id;
        loadWorkspaceMembers();
        emitOpened("person", id);
    }

    function selectChannel(id) {
        openChannel(id || "time");
    }

    // Opens one conversation of an agent (a note's card opens the
    // colleague's conversation).
    property string wantConv: ""
    property string pendingAskAgent: ""
    property string pendingAskText: ""
    function openAgentConv(id, conv) {
        explicitTarget = true;
        personMode = false;
        personId = "";
        if (id === agentId && !channel) {
            open(conv);
            emitOpened("agent", id);
            return;
        }
        wantConv = conv;
        selectAgent(id);
    }
    function openNewAgent(project) {
        if (!project && typeof win !== "undefined" && win && typeof win.newAgent === "function") {
            win.newAgent();
            return;
        }
        newAgentSheet.open(project || "");
    }
    function openCreatedAgent(a, conv) {
        if (!a || !a.id)
            return;
        runId = "";
        if (!agents.some(x => x.id === a.id))
            agents = agents.concat([a]);
        wantConv = conv || "";
        openAgent(a.id);
        loadAgents(false);
    }
    function askAgent(id, text) {
        const clean = (text || "").trim();
        if (!id || clean === "")
            return;
        explicitTarget = true;
        personMode = false;
        personId = "";
        channel = false;
        runId = "";
        if (!agents.some(x => x.id === id)) {
            pendingAskAgent = id;
            pendingAskText = clean;
            loadAgents(false);
            return;
        }
        pendingAskAgent = "";
        pendingAskText = "";
        agentId = id;
        markSeen(id);
        open("");
        loadingConv = false;
        emitOpened("agent", id);
        Api.get(convQuery(id), (err, data) => {
            if (!err && agentId === id) {
                const rows = data.map(convRow);
                convItems = rows;
                U.syncModel(convs, rows);
            }
            newConversation(clean, () => streamNative(clean, 0));
        });
    }

    function selectAgent(id) {
        personMode = false;
        personId = "";
        channel = false;
        if (id !== runOwner)
            runId = "";
        agentId = id;
        markSeen(id);
        convs.clear();
        convItems = [];
        open("");
        applyRightPrefs();
        emitOpened(id !== "" ? "agent" : "", id);
        if (id === "")
            return;
        loadingConv = true;
        showArchived = false;
        Api.get(convQuery(id), (err, data) => {
            if (agentId !== id)
                return;
            if (err) {
                loadingConv = false;
                return;
            }
            const rows = data.map(convRow);
            convItems = rows;
            U.syncModel(convs, rows);
            for (const c of data)
                if (c.job_id && !live[c.id])
                    follow(c.id, c.job_id);
            // The newest conversation the owner had, not the heartbeat's own.
            const first = data.find(c => c.id === wantConv) || data.find(c => !(c.title || "").startsWith("💓")) || null;
            wantConv = "";
            if (first)
                open(first.id);
            else
                loadingConv = false;
        });
    }

    function normMsg(m) {
        return {
            id: m.id || 0,
            role: m.role || "assistant",
            content: m.content || "",
            reasoning: "",
            mdl: m.model || "",
            prompt_tokens: m.prompt_tokens || 0,
            completion_tokens: m.completion_tokens || 0,
            duration_ms: m.duration_ms || 0,
            err: m.error || "",
            steps: JSON.stringify(m.steps || []),
            streaming: false,
            author: m.agent_id || "",
            reply_to: m.reply_to || 0,
            at: m.created_at || ""
        };
    }

    // ── Replies ─────────────────────────────────────────────────
    property var replyTarget: null // { id, who, content }

    function messageById(id) {
        for (let i = 0; i < msgs.count; i++)
            if (msgs.get(i).id === id)
                return msgs.get(i);
        return null;
    }

    function authorOf(m) {
        return m.role === "user" ? "você" : (agent ? agent.name : "agente");
    }

    function replyTo(m) {
        replyTarget = { id: m.id, who: authorOf(m), content: m.content };
        composer.area.forceActiveFocus();
    }

    function showDetails(m) {
        let steps = [];
        try {
            steps = JSON.parse(m.steps || "[]");
        } catch (e) {}
        details.open({ who: m.role === "user" ? "Você" : (agent ? agent.name : ""), at: m.at, mdl: m.mdl, prompt_tokens: m.prompt_tokens,
                       completion_tokens: m.completion_tokens, duration_ms: m.duration_ms, steps: steps.length, id: m.id, err: m.err });
    }

    function open(id) {
        if (id !== convId)
            replyTarget = null;
        convId = id;
        error = "";
        msgs.clear();
        missionsHere = [];
        loadMissions();
        loadingConv = id !== "";
        if (id === "")
            return;
        Api.get("/v1/conversations/" + id + "/messages", (err, data) => {
            if (convId !== id)
                return;
            loadingConv = false;
            if (err) {
                error = err;
                return;
            }
            msgs.clear();
            const rows = data.filter(m => m.role !== "system").map(normMsg);
            if (rows.length)
                msgs.append(rows); // one insert: one layout pass, not one per message
            if (live[id])
                msgs.append(Object.assign(normMsg({ id: -2 }), { streaming: true, steps: JSON.stringify(live[id].steps) }));
            msgsRev++;
            stick = true;
            thread.positionViewAtEnd();
            threadIntro.restart();
        });
    }

    function newConversation(first, then) {
        Api.post("/v1/conversations", { agent_id: agentId, title: first.slice(0, 60) }, (err, c) => {
            if (err) {
                error = err;
                setComposer(first);
                return;
            }
            const row = { id: c.id, title: c.title || first.slice(0, 60), preview: "", updated_at: c.created_at, job_id: "" };
            convs.insert(0, row);
            convItems = [row].concat(convItems);
            convId = c.id;
            then();
        });
    }

    // ── @ mentions (D35): an app, a skill or a tool for this message ──
    property var mentionables: []
    property var mentionList: []
    property int mentionPick: 0
    property int mentionAt: -1
    function loadMentions() {
        Api.get("/v1/mentions?agent=" + encodeURIComponent(chat.agentId || ""), (err, data) => {
            if (!err)
                chat.mentionables = data;
        });
    }
    onAgentIdChanged: loadMentions()
    FontMetrics {
        id: chipMetrics
        font.pixelSize: Theme.fontBody
        font.weight: Font.Medium
    }
    Icon {
        id: iconBank
        visible: false
    }
    function foldText(t) {
        return t.toLowerCase().normalize("NFD").replace(/[\u0300-\u036f]/g, "");
    }
    function updateMentions() {
        const area = composer.area;
        const before = area.getText(0, area.cursorPosition);
        const m = before.match(/(^|\s)@([^@\n\u2029\uFFFC]{0,30})$/);
        if (!m || (chat.mentionables.length === 0 && DocBus.docs.length === 0)) {
            mentionList = [];
            return;
        }
        const q = foldText(m[2]);
        const apps = chat.mentionables.filter(x => foldText(x.name).startsWith(q) || (q.length > 1 && foldText(x.name).indexOf(q) >= 0));
        // The Docs too (D60): marked, the doc's text goes with the message.
        const docs = DocBus.match(m[2], q === "" ? 3 : 5).map(docMention);
        const hits = apps.slice(0, docs.length ? 8 - Math.min(docs.length, 3) : 8).concat(docs).slice(0, 8);
        if (hits.length === 0 && /\s/.test(m[2])) {
            mentionList = [];
            return;
        }
        mentionAt = area.cursorPosition - m[2].length - 1;
        mentionList = hits;
        mentionPick = 0;
    }
    function completeMention(i) {
        const s = mentionList[i];
        if (!s)
            return;
        const area = composer.area;
        const at = mentionAt;
        area.remove(at, area.cursorPosition);
        // The chip is one picture in the text: Backspace takes it whole. The
        // space after it keeps what comes next as plain text.
        area.insert(at, chipHtml(s) + "&nbsp;");
        area.cursorPosition = at + 2;
        mentionList = [];
    }
    // A doc as an @ choice and a chip: its title, and its id to cite it by.
    function docMention(d) {
        const t = String(d.title || "Doc");
        return { kind: "doc", id: d.id, doc: d, name: t.length > 48 ? t.slice(0, 47) + "…" : t,
                 hint: DocBus.kindLabel(d) + (d.folder ? "  ·  " + d.folder : "") + (d.summary ? "  ·  " + d.summary : "") };
    }
    // citeDoc puts a doc's chip where the cursor is (a drop, "Citar").
    function citeDoc(d) {
        if (!d)
            return;
        const area = composer.area;
        area.forceActiveFocus();
        area.insert(area.cursorPosition, chipHtml(docMention(d)) + "&nbsp;");
        DocBus.pendingCite = null;
    }
    Connections {
        target: DocBus
        function onOpenRequested(id) {
            if (chat.visible)
                chat.openDocFloat(id);
        }
        function onListRequested() {
            if (chat.visible)
                chat.openDocFloat("");
        }
        function onCiteRequested(doc) {
            Qt.callLater(() => chat.citeDoc(doc));
        }
    }
    // ── chips inside the text: one picture each, drawn by image://chip ──
    function chipTone(m) {
        if (m.kind === "doc")
            return DocBus.tone(m.doc || DocBus.byId(m.id));
        const c = m.icon ? Logos.color(m.icon) : "";
        return c !== "" ? c : m.kind === "skill" ? Theme.palette[0] : m.kind === "tool" ? Theme.palette[1] : String(Theme.text2);
    }
    function chipShape(m) {
        if (m.kind === "doc") {
            const di = iconBank.icons[DocBus.icon(m.doc || DocBus.byId(m.id))] || iconBank.icons["doc"];
            return { m: "stroke", p: di.s || di.b };
        }
        const logo = m.icon ? Logos.of(m.icon) : "";
        if (logo !== "")
            return { m: "fill", p: logo };
        const name = m.kind === "skill" ? "book" : m.kind === "tool" ? "globe" : U.connectorIcon(m.icon || "") || "plug";
        const ic = iconBank.icons[name] || iconBank.icons["plug"];
        return ic.s || ic.b ? { m: "stroke", p: ic.s || ic.b } : { m: "fill", p: ic.f || "" };
    }
    function chipHtml(m) {
        const w = Math.ceil(chipMetrics.advanceWidth(m.name)) + 30;
        const shape = chipShape(m);
        const info = { w: w, h: 20, c: chipTone(m), t: String(Theme.white), m: shape.m, p: shape.p, n: m.name };
        if (m.kind === "doc")
            info.d = m.id;
        const data = JSON.stringify(info);
        const id = Qt.btoa(data).replace(/\+/g, "-").replace(/\//g, "_").replace(/=+$/, "");
        return '<img src="image://chip/' + id + '" width="' + w + '" height="20" align="middle">';
    }
    function chipInfo(id) {
        try {
            let b = id.replace(/-/g, "+").replace(/_/g, "/");
            while (b.length % 4)
                b += "=";
            return JSON.parse(Qt.atob(b));
        } catch (e) {
            return {};
        }
    }
    function chipName(id) {
        return chipInfo(id).n || "";
    }
    // composerPlain is the message as the agent gets it: each chip back to
    // "@Name" among the words.
    function composerPlain() {
        const area = composer.area;
        const names = [];
        const re = /src="image:\/\/chip\/([^"]+)"/g;
        let m;
        while ((m = re.exec(area.text)) !== null) {
            // A doc's chip goes as its link, so the agent gets the doc's text.
            const c = chipInfo(m[1]);
            names.push(c.d ? "[📄 " + String(c.n || "Doc").replace(/[\[\]]/g, "") + "](avora:doc/" + c.d + ")" : c.n ? "@" + c.n : "");
        }
        const parts = area.getText(0, area.length).split("\uFFFC");
        let out = parts[0];
        for (let i = 1; i < parts.length; i++)
            out += (names[i - 1] || "") + parts[i];
        return out.replace(/\u00A0/g, " ").replace(/[\u2028\u2029]/g, "\n");
    }
    // markHtml turns a plain message into rich text with its @ marks as
    // chips: the sent bubble, and text put back in the box.
    function markHtml(content, forBox) {
        let html = U.esc(content || "");
        // Cited docs: a chip, a link to open it in the sent bubble.
        if (html.indexOf("(avora:doc/") >= 0)
            html = html.replace(/\[📄 ?([^\]]*)\]\(avora:doc\/([0-9a-f]{8,32})\)/g, (all, title, id) => {
                const d = DocBus.byId(id);
                const chip = chipHtml(docMention(d || { id: id, title: title.replace(/&lt;/g, "<").replace(/&gt;/g, ">").replace(/&amp;/g, "&") }));
                return forBox ? chip : '<a href="avora:doc/' + id + '">' + chip + "</a>";
            });
        if (html.indexOf("@") >= 0) {
            const byLength = mentionables.slice().sort((a, b) => b.name.length - a.name.length);
            for (const m of byLength) {
                const name = U.esc(m.name).replace(/[.*+?^${}()|[\]\\]/g, "\\$&");
                const re = new RegExp("(^|\\s)@" + name + "(?=$|[\\s.,;:!?])", "gi");
                html = html.replace(re, (all, pre) => pre + chipHtml(m));
            }
        }
        return html.replace(/\n/g, "<br>").replace(/  /g, " &nbsp;");
    }
    function setComposer(t) {
        composer.text = markHtml(t, true);
        composer.area.cursorPosition = composer.area.length;
    }
    // A plain paste: what the clipboard has, never its colors and fonts.
    function pastePlain() {
        const t = Api.clipboardText();
        if (t === "")
            return false;
        composer.area.insert(composer.area.cursorPosition, U.esc(t).replace(/\n/g, "<br>"));
        return true;
    }
    function send(text) {
        let content = (text === undefined ? composerPlain() : text).trim();
        if (text === undefined && attachments.length > 0)
            content = (content + "\n\n" + attachmentText()).trim();
        if (content === "" || busy || !agent || uploading > 0)
            return;
        if (isNative && nativeBusy)
            return; // one word-by-word stream at a time
        composer.text = "";
        if (text === undefined)
            attachments = [];
        error = "";
        stick = true;
        const reply = replyTarget ? replyTarget.id : 0;
        replyTarget = null;
        const go = () => isNative ? streamNative(content, reply) : runAgent(content);
        if (convId === "")
            newConversation(content, go);
        else
            go();
    }

    function streamNative(content, reply) {
        streamConv = convId;
        msgs.append(normMsg({ id: -1, role: "user", content: content, reply_to: reply }));
        msgs.append(Object.assign(normMsg({ id: -2 }), { streaming: true }));
        Api.sendMessage(convId, content, reply || 0);
    }

    function runAgent(content) {
        const id = convId;
        msgs.append(normMsg({ id: -1, role: "user", content: content }));
        msgs.append(Object.assign(normMsg({ id: -2 }), { streaming: true }));
        Api.post("/v1/conversations/" + id + "/messages", { content: content }, (err, r) => {
            if (err) {
                if (convId === id) {
                    error = err;
                    open(id);
                }
                return;
            }
            follow(id, r.job_id);
            loadAgents(false);
        });
    }

    function follow(conv, job) {
        if (live[conv] && live[conv].job === job)
            return;
        const l = Object.assign({}, live);
        l[conv] = { job: job, after: 0, steps: [], since: Date.now() };
        live = l;
    }

    // The answer runs on the server: stopping it is asking the server.
    function stop() {
        if (convId !== "")
            Api.post("/v1/conversations/" + convId + "/stop", null, () => {});
    }

    function draftIndex() {
        const i = msgs.count - 1;
        return i >= 0 && msgs.get(i).streaming ? i : -1;
    }

    // Follows the running agent jobs: steps of the open one, the end of all.
    function poll() {
        for (const conv in live) {
            const l = live[conv];
            if (conv === convId)
                Api.get("/v1/events?job=" + l.job + "&after=" + l.after, (err, evs) => {
                    if (err || !live[conv])
                        return;
                    for (const e of evs) {
                        l.after = Math.max(l.after, e.id);
                        if (e.type === "agent.step")
                            l.steps.push(e.payload);
                    }
                    const i = draftIndex();
                    if (i >= 0 && convId === conv) {
                        msgs.setProperty(i, "steps", JSON.stringify(l.steps));
                        chat.msgsRev++;
                    }
                });
            Api.get("/v1/conversations/" + conv, (err, c) => {
                if (err || !live[conv] || c.job_id === l.job)
                    return;
                // The server stores the answer as the job ends; give it a beat.
                finishTimer.pending.push(conv);
                finishTimer.restart();
            });
        }
    }

    Timer {
        interval: Api.live ? 15000 : 1500
        repeat: true
        running: Object.keys(chat.live).length > 0
        onTriggered: chat.poll()
    }
    Timer {
        id: finishTimer
        property var pending: []
        interval: 400
        onTriggered: {
            const l = Object.assign({}, chat.live);
            for (const conv of pending)
                delete l[conv];
            chat.live = l;
            if (pending.indexOf(chat.convId) >= 0)
                chat.open(chat.convId);
            pending = [];
            chat.loadAgents(false);
            chat.refreshConvs();
            chat.loadMissions();
        }
    }

    // Locked actions waiting for the owner, for every agent.
    property var pending: []
    readonly property var pendingHere: pending.filter(a => !channelIds.includes(a.conversation_id) &&
                                                     (a.conversation_id === convId || a.agent_id === agentId))
    readonly property var pendingTeam: pending.filter(a => a.conversation_id === channelId)
    function needsYou(id) {
        return pending.some(a => a.agent_id === id);
    }

    property var runs: []
    property int runsRev: 0
    property int msgsRev: 0
    property string runId: ""
    property string runOwner: ""
    function loadRuns() {
        Api.get("/v1/runs?active=1", (err, list) => {
            if (!err && !U.same(chat.runs, list || [])) {
                chat.runs = list || [];
                chat.runsRev++;
            }
        });
    }
    function openRun(id, ownerAgentId) {
        if (!id)
            return;
        runOwner = ownerAgentId !== undefined ? ownerAgentId : chat.agentId;
        runId = id;
        if (typeof win !== "undefined" && win && typeof win.openFloat === "function")
            win.openFloat("RunPane.qml", { runId: id, agents: chat.agents });
    }
    function openDocsTab() {
        openDocFloat(DocBus.openId);
    }
    function closeSidePanel() {
        if (typeof win !== "undefined" && win && typeof win.closeFloat === "function")
            win.closeFloat();
    }

    function browserSiteOf(stepList) {
        let site = "";
        for (const st of stepList || []) {
            const base = (st.tool || "").split(" · ").pop();
            if (base !== "navegador" && !base.startsWith("browser."))
                continue;
            if (st.detail) {
                site = st.detail;
                continue;
            }
            const sum = st.summary || "";
            const m = sum.match(/(?:abriu|abrindo|navegando em| em )\s*([a-z0-9.-]+\.[a-z]{2,})/i);
            if (m && m[1])
                site = m[1];
            else if (site === "")
                site = "uma página";
        }
        return site;
    }

    readonly property string liveBrowserSite: {
        msgsRev;
        if (channel || personMode)
            return "";
        const d = draftIndex();
        if (d < 0)
            return "";
        try {
            return browserSiteOf(JSON.parse(msgs.get(d).steps || "[]"));
        } catch (e) {
            return "";
        }
    }

    readonly property var citedDocsHere: {
        msgsRev;
        const ids = [];
        const seenIds = {};
        const addId = id => {
            if (id && !seenIds[id]) {
                seenIds[id] = true;
                ids.push(id);
            }
        };
        const scanText = text => {
            const re = /avora:doc\/([0-9a-f]{8,32})/g;
            let m;
            while ((m = re.exec(text || "")) !== null)
                addId(m[1]);
        };
        const scanSteps = raw => {
            try {
                for (const st of JSON.parse(raw || "[]")) {
                    const tl = (st.tool || "").split(" · ").pop();
                    if ((tl === "docs_criar" || tl === "docs_editar" || tl === "docs_ler") && st.detail)
                        addId(st.detail);
                }
            } catch (e) {}
        };
        if (!channel && !personMode) {
            for (let i = 0; i < msgs.count; i++) {
                const row = msgs.get(i);
                scanText(row.content);
                scanSteps(row.steps);
            }
        }
        return ids;
    }

    readonly property var channelPinnedDocs: {
        DocBus.rev;
        const cid = chat.channelId;
        return DocBus.docs.filter(d => !!d.pinned && d.conversation_id === cid);
    }

    Timer {
        interval: Api.live ? 30000 : chat.runs.length > 0 ? 2500 : 6000
        repeat: true
        running: chat.visible
        triggeredOnStart: true
        onTriggered: chat.loadRuns()
    }

    // Missions in progress in the open conversation (MissionCards).
    property var missionsHere: []
    function loadMissions() {
        const conv = convId;
        if (conv === "" || chat.channelIds.includes(conv)) {
            missionsHere = [];
            return;
        }
        Api.get("/v1/missions?active=1&conversation=" + conv, (err, list) => {
            if (!err && chat.convId === conv)
                chat.missionsHere = list || [];
        });
    }
    Timer {
        interval: Api.live ? 20000 : 4000
        repeat: true
        running: chat.visible && chat.missionsHere.length > 0
        onTriggered: {
            const conv = chat.convId;
            chat.loadMissions();
            Api.get("/v1/conversations/" + conv, (err, c) => {
                if (!err && c.job_id && !chat.live[conv] && !(Api.streaming && chat.streamConv === conv))
                    chat.follow(conv, c.job_id);
            });
        }
    }
    function loadPending() {
        Api.get("/v1/approvals?status=pending&limit=50", (err, data) => {
            if (!err && !U.same(chat.pending, data))
                chat.pending = data;
        });
    }
    Timer {
        interval: Api.live ? 30000 : 2500
        repeat: true
        running: chat.visible
        triggeredOnStart: true
        onTriggered: chat.loadPending()
    }

    Timer {
        interval: Api.live ? 30000 : 6000
        repeat: true
        running: chat.visible
        onTriggered: chat.loadAgents(false)
    }

    // ── Archive / delete conversations ──────────────────────────
    property bool showArchived: false
    property string confirmDelete: "" // conversation id armed for delete

    function convRow(c) {
        return { id: c.id, title: c.title || c.preview || "Conversa", preview: c.preview || "",
                 updated_at: c.updated_at || c.created_at, job_id: c.job_id || "" };
    }

    function convQuery(id) {
        return "/v1/conversations?limit=60&agent=" + id + (showArchived ? "&archived=1" : "");
    }

    function toggleArchived() {
        showArchived = !showArchived;
        convs.clear();
        convItems = [];
        refreshConvs();
    }

    function dropped(id) {
        if (id === convId)
            open("");
        refreshConvs();
    }

    function archiveConv(id, archived) {
        Api.patch("/v1/conversations/" + id, { archived: archived }, err => {
            if (err) {
                error = err;
                return;
            }
            dropped(id);
        });
    }

    function deleteConv(id) {
        if (confirmDelete !== id) {
            confirmDelete = id;
            confirmTimer.restart();
            return;
        }
        confirmDelete = "";
        Api.del("/v1/conversations/" + id, err => {
            if (err) {
                error = err.indexOf("409") >= 0 || err.toLowerCase().indexOf("busy") >= 0 || err.indexOf("trabalh") >= 0
                        ? "Essa conversa ainda está trabalhando. Pare ela antes de apagar." : err;
                return;
            }
            dropped(id);
        });
    }

    Timer {
        id: confirmTimer
        interval: 3000
        onTriggered: chat.confirmDelete = ""
    }

    function refreshConvs() {
        const id = agentId;
        if (!id)
            return;
        Api.get(convQuery(id), (err, data) => {
            if (err || agentId !== id)
                return;
            const rows = data.map(convRow);
            convItems = rows;
            U.syncModel(convs, rows);
            for (const c of data)
                if (c.job_id && !live[c.id])
                    follow(c.id, c.job_id);
        });
    }

    // ── Live (D47) ──────────────────────────────────────────────
    Timer { id: liveAgents; interval: 120; onTriggered: chat.loadAgents(false) }
    Timer { id: liveConvs; interval: 120; onTriggered: chat.refreshConvs() }
    Timer { id: livePending; interval: 60; onTriggered: chat.loadPending() }
    Timer { id: liveRuns; interval: 120; onTriggered: chat.loadRuns() }
    Timer { id: liveMissions; interval: 120; onTriggered: chat.loadMissions() }
    Timer {
        id: liveMsgs
        property string conv: ""
        interval: 40
        onTriggered: chat.catchUp(conv)
    }

    function ownStream(conv) {
        return Api.streaming && streamConv === conv;
    }

    function convOfJob(job) {
        for (const c in live)
            if (live[c].job === job)
                return c;
        return "";
    }

    function endLive(conv) {
        if (!live[conv])
            return;
        const l = Object.assign({}, live);
        delete l[conv];
        live = l;
    }

    function catchUp(conv) {
        if (conv === "" || conv !== convId)
            return;
        Api.get("/v1/conversations/" + conv + "/messages", (err, data) => {
            if (err || conv !== convId)
                return;
            let known = 0;
            for (let i = 0; i < msgs.count; i++)
                known = Math.max(known, msgs.get(i).id);
            const fresh = data.filter(m => m.role !== "system" && m.id > known).map(normMsg);
            for (const m of fresh) {
                if (m.role === "user") {
                    let tmp = -1;
                    for (let i = 0; i < msgs.count; i++)
                        if (msgs.get(i).id === -1) {
                            tmp = i;
                            break;
                        }
                    const d = draftIndex();
                    if (tmp >= 0)
                        msgs.set(tmp, m);
                    else if (d >= 0)
                        msgs.insert(d, m);
                    else
                        msgs.append(m);
                } else if (m.author !== "") {
                    const d = draftIndex();
                    if (d >= 0)
                        msgs.insert(d, m);
                    else
                        msgs.append(m);
                } else {
                    if (ownStream(conv))
                        continue;
                    const d = draftIndex();
                    if (d >= 0)
                        msgs.set(d, m);
                    else
                        msgs.append(m);
                }
            }
            if (live[conv] && draftIndex() < 0 && !ownStream(conv))
                msgs.append(Object.assign(normMsg({ id: -2 }), { streaming: true, steps: JSON.stringify(live[conv].steps) }));
            if (fresh.length)
                chat.msgsRev++;
        });
    }

    function onLive(e) {
        const p = e.payload || {};
        const t = e.type;
        if (t === "message.created") {
            const conv = p.conversation_id || "";
            if (p.role === "user" && e.job_id && !ownStream(conv) && (!live[conv] || live[conv].job !== e.job_id))
                follow(conv, e.job_id);
            else if (p.role === "assistant")
                endLive(conv);
            if (conv === convId) {
                liveMsgs.conv = conv;
                liveMsgs.restart();
            }
            liveConvs.restart();
            liveAgents.restart();
        } else if (t === "agent.step") {
            const conv = convOfJob(e.job_id);
            if (conv === "" || ownStream(conv))
                return;
            live[conv].steps.push(p);
            if (conv === convId)
                workStep = p.tool || "";
            const i = draftIndex();
            if (i >= 0 && conv === convId) {
                msgs.setProperty(i, "steps", JSON.stringify(live[conv].steps));
                chat.msgsRev++;
            }
        } else if (t.startsWith("approval.")) {
            livePending.restart();
        } else if (t.startsWith("mission.")) {
            liveMissions.restart();
        } else if (t.startsWith("job.")) {
            liveRuns.restart();
            liveAgents.restart();
        } else if (t.startsWith("conversation.")) {
            liveConvs.restart();
            liveAgents.restart();
        } else if (t.startsWith("routine.")) {
            liveAgents.restart();
        } else if (t.startsWith("agent.") || t.startsWith("project.") || t.startsWith("goal.")) {
            liveAgents.restart();
        }
    }

    Connections {
        target: Api
        function onLiveEvent(e) {
            chat.onLive(e);
        }
        function onLiveReset() {
            liveAgents.restart();
            liveConvs.restart();
            livePending.restart();
            liveRuns.restart();
            liveMissions.restart();
            if (chat.convId !== "") {
                liveMsgs.conv = chat.convId;
                liveMsgs.restart();
            }
        }
    }

    Connections {
        target: Api
        function onStreamDelta(text) {
            const i = chat.draftIndex();
            if (i >= 0 && chat.convId === chat.streamConv)
                msgs.setProperty(i, "content", msgs.get(i).content + text);
        }
        function onStreamReasoning(text) {
            const i = chat.draftIndex();
            if (i >= 0 && chat.convId === chat.streamConv)
                msgs.setProperty(i, "reasoning", msgs.get(i).reasoning + text);
        }
        function onStreamDone(m) {
            const i = chat.draftIndex();
            if (i >= 0 && chat.convId === chat.streamConv) {
                msgs.set(i, chat.normMsg(m));
                chat.msgsRev++;
            }
            chat.loadAgents(false);
            chat.refreshConvs();
        }
        function onStreamStep(st) {
            if (chat.convId === chat.streamConv)
                chat.workStep = st.tool || "";
            const i = chat.draftIndex();
            if (i < 0 || chat.convId !== chat.streamConv)
                return;
            const list = JSON.parse(msgs.get(i).steps || "[]");
            list.push(st);
            msgs.setProperty(i, "steps", JSON.stringify(list));
            chat.msgsRev++;
        }
        function onStreamError(e) {
            chat.error = e;
        }
        function onStreamingChanged() {
            if (Api.streaming || chat.convId !== chat.streamConv)
                return;
            const i = chat.draftIndex();
            if (i < 0)
                return;
            const conv = chat.streamConv;
            Api.get("/v1/conversations/" + conv, (err, c) => {
                if (!err && c.job_id && !chat.live[conv])
                    chat.follow(conv, c.job_id);
            });
            if (msgs.get(i).content === "")
                msgs.remove(i);
            else
                msgs.setProperty(i, "streaming", false);
        }
    }

    // What each agent is good at, to start a conversation.
    readonly property var starters: ({
        vega: ["Organize minha semana", "O que o time deve priorizar hoje?", "Transforme esta ideia em um plano: ", "Quem do time cuida disso? "],
        atlas: ["Crie um script que ", "Monte a estrutura de um projeto novo para ", "Explique este erro: ", "Revise o código da sua pasta"],
        iris: ["Pesquise e compare ", "O que há de novo sobre ", "Verifique se isto é verdade: ", "Encontre fontes confiáveis sobre "],
        cora: ["Monte um orçamento mensal para mim", "Vale a pena comprar ", "Quanto guardar por mês para ", "Analise estes gastos: "],
        nico: ["3 ideias de renda com o que eu sei fazer", "Plano de 30 dias para crescer no ", "Onde vender meu serviço de ", "Como precificar meu trabalho de "]
    })

    // ── Header ──────────────────────────────────────────────────
    ChatHeader {
        id: chatHeader
        z: 5
        anchors {
            top: parent.top
            left: parent.left
            right: rightCol.left
        }
        kind: chat.currentKind
        agent: chat.agent
        agents: chat.agents
        projects: chat.projects
        convId: chat.convId
        msgsCount: msgs.count
        working: chat.busy
        workStep: chat.workStep
        pendingCount: chat.pendingHere.length
        conversations: chat.convItems
        liveMap: chat.live
        showArchived: chat.showArchived
        confirmDelete: chat.confirmDelete

        channelId: chat.channelId
        channelName: room.chName
        channelPurpose: room.purpose
        channelMemberCount: room.members.length
        channelEveryone: !!(room.info && room.info.everyone)
        channelBusy: room.busy
        channelRouting: room.routing
        channelWorkingText: room.busy ? (room.names(room.workingIds) + (room.workingIds.length > 1 ? " estão trabalhando…" : " está trabalhando…")) : ""
        channelStreak: room.streak
        channelLimit: room.limit
        channelLocked: room.locked
        channelPinnedDocs: chat.channelPinnedDocs

        person: chat.currentPerson
        rightOpen: chat.rightOpen && !chat.personMode

        onPickModelRequested: {
            if (chat.agent)
                modelPicker.open(chat.agent);
        }
        onSelectConversationRequested: id => {
            chat.confirmDelete = "";
            chat.open(id);
        }
        onNewConversationRequested: {
            if (chat.showArchived)
                chat.toggleArchived();
            chat.open("");
            composer.area.forceActiveFocus();
        }
        onToggleArchivedRequested: chat.toggleArchived()
        onArchiveConversationRequested: (id, archived) => chat.archiveConv(id, archived)
        onDeleteConversationRequested: id => chat.deleteConv(id)
        onToggleRightRequested: chat.toggleRightColumn()
        onChannelSettingsRequested: chanEditor.open(chat.channelNow, "")
        onStopChannelRequested: room.stop()
        onContinueChannelRequested: room.send("continuem")
        onOpenDocRequested: docId => chat.openDocFloat(docId)
    }

    // ── Agent conversation ──────────────────────────────────────
    Item {
        id: pane
        visible: !chat.channel && !chat.personMode
        anchors {
            top: chatHeader.bottom
            bottom: parent.bottom
            left: parent.left
            right: rightCol.left
        }
        readonly property real sideMargin: Math.min(96, Math.max(20, (width - 520) * 0.35))
        readonly property real column: Math.max(260, width - 2 * sideMargin)
        readonly property color tint: chat.agent && chat.agent.color ? chat.agent.color : Theme.text2

        // Welcome: who this agent is and what to ask.
        Column {
            visible: chat.empty && chat.agent !== null && !chat.loadingConv
            anchors { horizontalCenter: parent.horizontalCenter; verticalCenter: parent.verticalCenter; verticalCenterOffset: -30 }
            width: pane.column
            spacing: 0
            opacity: visible ? 1 : 0
            Behavior on opacity { NumberAnimation { duration: Theme.slow } }

            Avatar {
                size: 92
                name: chat.agent ? chat.agent.name : ""
                photo: U.photo(chat.agent)
                tint: pane.tint
                anchors.horizontalCenter: parent.horizontalCenter
            }
            Item { width: 1; height: 18 }
            Text {
                text: chat.agent ? "Fale com " + chat.agent.name : ""
                color: Theme.white
                font.family: Theme.display
                font.pixelSize: Theme.fontHero
                font.weight: Font.DemiBold
                font.letterSpacing: -1
                anchors.horizontalCenter: parent.horizontalCenter
            }
            Item { width: 1; height: 8 }
            Text {
                width: Math.min(pane.column, 560)
                anchors.horizontalCenter: parent.horizontalCenter
                horizontalAlignment: Text.AlignHCenter
                text: chat.agent ? (chat.agent.persona || "").split("\n")[0] : ""
                color: Theme.text3
                font.pixelSize: Theme.fontLarge
                wrapMode: Text.Wrap
            }
            Item { width: 1; height: 30 }
            Grid {
                id: sugg
                columns: pane.column >= 480 ? 2 : 1
                spacing: 12
                anchors.horizontalCenter: parent.horizontalCenter
                readonly property real cell: columns === 2 ? (Math.min(pane.column, 640) - spacing) / 2 : Math.min(pane.column, 440)
                Repeater {
                    model: chat.agent ? (chat.starters[chat.agent.id] || ["Me ajude com ", "Explique ", "Planeje ", "Revise "]) : []
                    delegate: Card {
                        id: s
                        required property string modelData
                        width: sugg.cell
                        height: 58
                        padding: 16
                        radius: Theme.radiusLg
                        glow: hov.containsMouse
                        glowStrength: 0.06
                        topColor: hov.containsMouse ? Theme.cardHover : Theme.card
                        Text {
                            anchors.verticalCenter: parent.verticalCenter
                            width: parent.width
                            text: s.modelData.trim() + (s.modelData.endsWith(" ") ? "…" : "")
                            color: Theme.text2
                            font.pixelSize: Theme.fontLarge
                            elide: Text.ElideRight
                        }
                        MouseArea {
                            id: hov
                            anchors.fill: parent
                            hoverEnabled: true
                            cursorShape: Qt.PointingHandCursor
                            onClicked: {
                                if (s.modelData.endsWith(" ")) {
                                    chat.setComposer(s.modelData);
                                    composer.area.forceActiveFocus();
                                } else {
                                    chat.send(s.modelData);
                                }
                            }
                        }
                    }
                }
            }
        }

        // Empty state when workspace has no agents yet.
        Column {
            visible: chat.agent === null && !chat.loadingConv
            anchors.centerIn: parent
            width: Math.min(pane.column, 420)
            spacing: Theme.space3

            Text {
                width: parent.width
                horizontalAlignment: Text.AlignHCenter
                text: "Escolha um agente ou canal"
                color: Theme.white
                font.family: Theme.display
                font.pixelSize: Theme.fontDisplay
                font.weight: Font.DemiBold
                wrapMode: Text.Wrap
            }

            Text {
                width: parent.width
                horizontalAlignment: Text.AlignHCenter
                text: "Selecione alguém na barra lateral para começar uma conversa."
                color: Theme.text3
                font.pixelSize: Theme.fontBody
                wrapMode: Text.Wrap
            }
        }

        // A conversation on its way.
        ThreadSkeleton {
            anchors { top: parent.top; bottom: composerBox.top; left: parent.left; right: parent.right; bottomMargin: 8 }
            active: chat.loadingConv && chat.empty
            column: pane.column
            tint: pane.tint
        }

        // Messages
        ListView {
            id: thread
            visible: !chat.empty
            transform: Translate { id: threadRise }
            ParallelAnimation {
                id: threadIntro
                NumberAnimation { target: thread; property: "opacity"; from: 0; to: 1; duration: Theme.medium; easing.type: Easing.OutCubic }
                NumberAnimation { target: threadRise; property: "y"; from: 10; to: 0; duration: Theme.move; easing.type: Easing.OutCubic }
            }
            anchors { top: parent.top; bottom: composerBox.top; left: parent.left; right: parent.right; bottomMargin: 8 }
            topMargin: 26
            bottomMargin: 16
            clip: true
            spacing: 0
            model: msgs
            boundsBehavior: Flickable.StopAtBounds
            ScrollBar.vertical: SlimScrollBar { id: threadBar }
            SmoothWheel { id: threadWheel }
            cacheBuffer: count <= 150 ? 100000 : 8000
            property bool pinning: false
            onContentHeightChanged: if (chat.stick) {
                pinning = true;
                positionViewAtEnd();
                pinning = false;
            }
            onContentYChanged: if (!pinning && (threadWheel.driving || threadBar.pressed)) chat.stick = atYEnd
            delegate: Item {
                id: msg
                required property int index
                required property string role
                required property string content
                required property string reasoning
                required property string mdl
                required property int prompt_tokens
                required property int completion_tokens
                required property int duration_ms
                required property string err
                required property string steps
                required property bool streaming
                required property int id
                required property int reply_to
                required property string at
                required property string author
                readonly property var who: author !== "" && author !== chat.agentId ? chat.agents.find(a => a.id === author) || null : null
                readonly property color tint: who && who.color ? who.color : pane.tint
                readonly property bool mine: role === "user"
                readonly property string markup: mine && !report ? chat.markHtml(content) : ""
                readonly property bool rich: markup.indexOf("image://chip/") >= 0
                readonly property bool report: mine && (content.startsWith("🎯 Missão") || content.startsWith("💓 Heartbeat") || content.startsWith("📨 Recado de"))
                readonly property var quoted: reply_to > 0 ? chat.messageById(reply_to) : null
                readonly property var stepList: JSON.parse(steps || "[]")
                readonly property var innerSteps: stepList.filter(s => U.action(s) === null && s.tool !== "colega_respondeu" && s.tool !== "perguntar")
                readonly property var asks: stepList.filter(s => s.tool === "perguntar" && s.detail)
                readonly property string nextMine: {
                    const n = msgs.count;
                    if (asks.length === 0 || index + 1 >= n)
                        return "";
                    const m = msgs.get(index + 1);
                    return m && m.role === "user" ? m.content : "";
                }
                readonly property var replies: stepList.filter(s => s.tool === "colega_respondeu" && s.detail)
                readonly property bool drawing: streaming && stepList.some(s => (s.tool || "").endsWith("gerar_imagem"))
                readonly property string browserSite: streaming ? chat.browserSiteOf(stepList) : ""
                property bool expanded: false

                width: thread.width
                height: (mine ? bubble.y + bubble.height : answer.height) + 30

                HoverHandler { id: msgHover }

                MessageActions {
                    shown: msgHover.hovered && !msg.streaming
                    canReply: msg.id > 0
                    y: parent.height - 28
                    x: msg.mine ? bubble.x + bubble.width - width : answer.x + 54
                    onCopy: Api.copyText(msg.content)
                    onReply: chat.replyTo(msg)
                    onDetails: chat.showDetails(msg)
                }

                Text {
                    id: quoteLine
                    visible: msg.mine && msg.quoted !== null
                    x: bubble.x + bubble.width - width
                    width: Math.min(implicitWidth, pane.column * 0.7)
                    textFormat: Text.StyledText
                    text: msg.quoted ? "↪ " + U.esc(chat.authorOf(msg.quoted)) + ": <font color='" + Theme.text4 + "'>" +
                                       U.esc(msg.quoted.content.replace(/[*_`#>|]/g, "").replace(/\s+/g, " ").slice(0, 120)) + "</font>" : ""
                    color: Theme.text3
                    font.pixelSize: Theme.fontSmall
                    elide: Text.ElideRight
                }

                Rectangle {
                    id: bubble
                    visible: msg.mine
                    y: quoteLine.visible ? quoteLine.height + 6 : 0
                    x: (thread.width + pane.column) / 2 - width
                    width: Math.min(userText.implicitWidth + 36, pane.column * 0.78, 560)
                    height: userText.implicitHeight + 24
                    radius: Theme.radius
                    color: msg.report ? Theme.wash1 : Theme.active
                    border.width: 1
                    border.color: Theme.wash4
                    TextEdit {
                        id: userText
                        x: 18
                        y: 12
                        textFormat: msg.rich ? TextEdit.RichText : TextEdit.PlainText
                        width: Math.min(implicitWidth, pane.column * 0.78 - 36, 560 - 36)
                        text: msg.report && !msg.expanded ? msg.content.split("\n")[0] + "  ▾" : msg.rich ? msg.markup : msg.content
                        color: msg.report ? Theme.text2 : Theme.text
                        font.pixelSize: msg.report ? Theme.fontBody : Theme.fontLarge
                        wrapMode: TextEdit.Wrap
                        readOnly: true
                        selectByMouse: !msg.report || msg.expanded
                        selectionColor: Theme.wash6
                        onLinkActivated: link => chat.openLink(link)
                        HoverHandler {
                            enabled: !msg.report
                            cursorShape: userText.hoveredLink !== "" ? Qt.PointingHandCursor : Qt.IBeamCursor
                        }
                    }
                    TapHandler {
                        enabled: msg.report
                        onTapped: msg.expanded = !msg.expanded
                    }
                    HoverHandler {
                        enabled: msg.report
                        cursorShape: Qt.PointingHandCursor
                    }
                }

                Item {
                    id: answer
                    visible: !msg.mine
                    x: (thread.width - pane.column) / 2
                    width: pane.column
                    height: Math.max(34, bubbleA.height)

                    Avatar {
                        size: 42
                        name: msg.who ? msg.who.name : chat.agent ? chat.agent.name : ""
                        photo: U.photo(msg.who || chat.agent)
                        tint: msg.tint
                        working: msg.streaming
                    }
                    AgentBubble {
                        id: bubbleA
                        x: 54
                        maxWidth: Math.min(answer.width - 54, 780)
                        tint: msg.tint
                        name: msg.who ? msg.who.name : chat.agent ? chat.agent.name : ""
                        sub: (msg.who ? "colega, a pedido de " + (chat.agent ? chat.agent.name : "") + "  ·  " : "") + (msg.at ? U.clock(msg.at).slice(0, 5) : "")
                        natural: {
                            if (msg.replies.length || msg.asks.length || msg.drawing || msg.err !== "" || (msg.streaming && msg.stepList.length)
                                || msg.stepList.some(s => s.tool === "pedir_conexao" || U.action(s) !== null || /docs_(criar|editar)$/.test(s.tool || ""))
                                || U.imagesOf(msg.content).length || msg.content.indexOf("(avora:doc/") >= 0)
                                return Infinity;
                            if (msg.content === "")
                                return status.implicitWidth;
                            return Math.max(body.naturalWidth, !msg.streaming && msg.innerSteps.length ? stepsChip.width : 0);
                        }
                        Column {
                            id: col
                            width: parent.width
                            spacing: 10

                            Text {
                                id: status
                                visible: msg.streaming && msg.content === ""
                                text: chat.pendingHere.length ? "Esperando sua permissão…"
                                      : msg.stepList.length ? "Trabalhando · " + U.stepLabel(msg.stepList[msg.stepList.length - 1].tool.split(" · ")[0])
                                      : msg.reasoning !== "" ? "Pensando…" : "Escrevendo…"
                                color: Theme.text3
                                font.pixelSize: Theme.fontLarge
                                SequentialAnimation on opacity {
                                    running: status.visible && Qt.application.state === Qt.ApplicationActive
                                    loops: Animation.Infinite
                                    NumberAnimation { to: 0.4; duration: 700; easing.type: Easing.InOutSine }
                                    NumberAnimation { to: 1; duration: 700; easing.type: Easing.InOutSine }
                                }
                            }

                            Column {
                                visible: msg.streaming && msg.stepList.length > 0
                                width: parent.width
                                spacing: 4
                                Repeater {
                                    model: msg.streaming ? msg.stepList.filter(s => s.tool !== "colega_respondeu").slice(-5) : []
                                    delegate: Row {
                                        id: liveStep
                                        required property var modelData
                                        required property int index
                                        readonly property bool latest: index === Math.min(4, msg.stepList.length - 1)
                                        spacing: 8
                                        opacity: latest ? 1 : 0.45
                                        Rectangle {
                                            width: 5
                                            height: 5
                                            radius: 2.5
                                            color: liveStep.latest ? pane.tint : Theme.text4
                                            anchors.verticalCenter: parent.verticalCenter
                                        }
                                        Text {
                                            text: U.stepLabel(liveStep.modelData.tool)
                                            color: Theme.text2
                                            font.family: Theme.mono
                                            font.pixelSize: Theme.fontSmall
                                        }
                                        Text {
                                            width: col.width - 120
                                            text: liveStep.modelData.summary || ""
                                            color: Theme.text4
                                            font.family: Theme.mono
                                            font.pixelSize: Theme.fontSmall
                                            elide: Text.ElideMiddle
                                        }
                                    }
                                }
                            }

                            ImageSkeleton {
                                visible: msg.drawing
                                tint: msg.tint
                                onVisibleChanged: if (visible) since = Date.now()
                            }

                            // Live browser card: opens this agent's BrowserPanel in FloatingPanel.
                            Rectangle {
                                visible: msg.browserSite !== ""
                                width: Math.min(col.width, browserLiveRow.implicitWidth + 2 * Theme.space3)
                                height: 36
                                radius: Theme.radiusMd
                                color: browserLiveArea.containsMouse ? Theme.active : Theme.wash1
                                border.width: 1
                                border.color: browserLiveArea.containsMouse ? Theme.lineStrong : Theme.line
                                Behavior on color { ColorAnimation { duration: Theme.fast } }
                                Row {
                                    id: browserLiveRow
                                    x: Theme.space3
                                    anchors.verticalCenter: parent.verticalCenter
                                    spacing: Theme.space2
                                    Dot {
                                        anchors.verticalCenter: parent.verticalCenter
                                        kind: "live"
                                    }
                                    Icon {
                                        anchors.verticalCenter: parent.verticalCenter
                                        name: "globe"
                                        size: 14
                                        color: msg.tint
                                    }
                                    Text {
                                        anchors.verticalCenter: parent.verticalCenter
                                        text: "Navegando em " + msg.browserSite
                                        color: Theme.text
                                        font.pixelSize: Theme.fontBody
                                        font.weight: Font.Medium
                                        width: Math.min(implicitWidth, col.width - 110)
                                        elide: Text.ElideRight
                                    }
                                    Text {
                                        anchors.verticalCenter: parent.verticalCenter
                                        text: "Assistir ›"
                                        color: browserLiveArea.containsMouse ? Theme.white : Theme.text3
                                        font.pixelSize: Theme.fontSmall
                                    }
                                }
                                MouseArea {
                                    id: browserLiveArea
                                    anchors.fill: parent
                                    hoverEnabled: true
                                    cursorShape: Qt.PointingHandCursor
                                    onClicked: {
                                        const target = msg.who || chat.agent;
                                        if (target)
                                            chat.openBrowserFloat(target.id, target.name || "");
                                    }
                                }
                            }

                            // A colleague consulted mid-answer: its reply, as its message.
                            Repeater {
                                model: msg.replies
                                delegate: Rectangle {
                                    id: reply
                                    required property var modelData
                                    readonly property var mate: chat.agents.find(a => a.id === modelData.summary) || null
                                    readonly property color mateTint: mate && mate.color ? mate.color : Theme.text2
                                    width: col.width
                                    height: replyCol.implicitHeight + 22
                                    radius: Theme.radiusMd
                                    color: Theme.hover
                                    border.width: 1
                                    border.color: Theme.active
                                    Rectangle {
                                        x: 0
                                        y: 10
                                        width: 2
                                        height: parent.height - 20
                                        radius: 1
                                        color: reply.mateTint
                                        opacity: 0.8
                                    }
                                    Column {
                                        id: replyCol
                                        x: 14
                                        y: 11
                                        width: parent.width - 28
                                        spacing: 6
                                        Row {
                                            spacing: 8
                                            Avatar {
                                                size: 20
                                                name: reply.mate ? reply.mate.name : "?"
                                                photo: U.photo(reply.mate)
                                                tint: reply.mateTint
                                            }
                                            Text {
                                                anchors.verticalCenter: parent.verticalCenter
                                                text: (reply.mate ? reply.mate.name : "Colega") + "  <font color='" + Theme.text4 + "'>respondeu a " + U.esc(chat.agent ? chat.agent.name : "") + "</font>"
                                                textFormat: Text.StyledText
                                                color: reply.mateTint
                                                font.pixelSize: Theme.fontSmall
                                                font.weight: Font.DemiBold
                                            }
                                        }
                                        MarkdownView {
                                            width: parent.width
                                            text: reply.modelData.detail
                                            tint: reply.mateTint
                                            color: Theme.text2
                                            fontSize: Theme.fontLarge
                                            onLinkActivated: link => chat.openLink(link)
                                        }
                                    }
                                }
                            }

                            MarkdownView {
                                id: body
                                width: parent.width
                                visible: msg.content !== ""
                                text: msg.content
                                tint: msg.tint
                                mentions: chat.teamChips
                                onLinkActivated: link => chat.openLink(link)
                            }

                            MessageImages {
                                width: parent.width
                                images: msg.streaming ? [] : U.imagesOf(msg.content)
                            }

                            DocCards {
                                width: parent.width
                                visible: !msg.streaming && items.length > 0
                                text: msg.content
                                steps: msg.stepList
                            }

                            Repeater {
                                model: msg.asks
                                delegate: QuestionCard {
                                    required property var modelData
                                    required property int index
                                    readonly property var d: { try { return JSON.parse(modelData.detail); } catch (e) { return {}; } }
                                    width: col.width
                                    question: modelData.summary || ""
                                    options: d.opcoes || []
                                    multiple: !!d.multipla
                                    other: !!d.outra
                                    tint: msg.tint
                                    live: !msg.streaming && !chat.busy && msg.nextMine === "" && msg.index === msgs.count - 1 && index === msg.asks.length - 1
                                    answer: msg.nextMine
                                    onPicked: text => chat.send(text)
                                    onOtherAnswer: composer.area.forceActiveFocus()
                                }
                            }

                            ConnectCards {
                                width: parent.width
                                kinds: msg.streaming ? [] : msg.stepList.filter(s => s.tool === "pedir_conexao").map(s => s.summary)
                                    .filter((k, i, all) => k && all.indexOf(k) === i)
                                onConnected: name => chat.send("Conectei o " + name + ". Pode continuar.")
                            }

                            ActionCards {
                                width: parent.width
                                visible: !msg.streaming && actions.length > 0
                                steps: msg.stepList
                                tint: pane.tint
                                onOpenRun: id => chat.openRun(id)
                                onOpenConv: (agent, conv) => chat.openAgentConv(agent, conv)
                            }

                            Rectangle {
                                visible: msg.err !== ""
                                width: Math.min(parent.width, errText.implicitWidth + 50)
                                height: errText.implicitHeight + 18
                                radius: Theme.radiusMd
                                color: Theme.badWash
                                border.width: 1
                                border.color: Theme.badLine
                                Icon {
                                    x: 12
                                    y: 10
                                    name: "alert"
                                    size: 15
                                    color: Theme.bad
                                }
                                Text {
                                    id: errText
                                    x: 36
                                    y: 9
                                    width: Math.min(implicitWidth, col.width - 50)
                                    text: msg.err
                                    color: Theme.bad
                                    font.pixelSize: Theme.fontBody
                                    wrapMode: Text.Wrap
                                }
                            }

                            // Steps of a finished run, folded.
                            Column {
                                visible: !msg.streaming && msg.innerSteps.length > 0
                                width: parent.width
                                spacing: 6
                                Rectangle {
                                    id: stepsChip
                                    width: stepsLabel.implicitWidth + 40
                                    height: 28
                                    radius: Theme.radiusLg
                                    color: stepsArea.containsMouse ? Theme.hover : Theme.wash1
                                    border.width: 1
                                    border.color: Theme.line
                                    Icon {
                                        x: 10
                                        anchors.verticalCenter: parent.verticalCenter
                                        name: "chevron"
                                        size: 12
                                        color: Theme.text3
                                        rotation: msg.expanded ? 90 : 0
                                        Behavior on rotation { NumberAnimation { duration: Theme.fast } }
                                    }
                                    Text {
                                        id: stepsLabel
                                        x: 28
                                        anchors.verticalCenter: parent.verticalCenter
                                        text: msg.innerSteps.length + (msg.innerSteps.length === 1 ? " passo" : " passos") + "  ·  " +
                                              [...new Set(msg.innerSteps.map(s => U.stepLabel(s.tool.split(" · ")[0])))].slice(0, 3).join(", ")
                                        color: Theme.text3
                                        font.pixelSize: Theme.fontSmall
                                    }
                                    MouseArea {
                                        id: stepsArea
                                        anchors.fill: parent
                                        hoverEnabled: true
                                        cursorShape: Qt.PointingHandCursor
                                        onClicked: msg.expanded = !msg.expanded
                                    }
                                }
                                Column {
                                    visible: msg.expanded
                                    width: parent.width
                                    spacing: 3
                                    leftPadding: 12
                                    Repeater {
                                        model: msg.expanded ? msg.innerSteps : []
                                        delegate: Text {
                                            required property var modelData
                                            width: col.width - 12
                                            text: U.stepLabel(modelData.tool) + "  " + (modelData.summary || "")
                                            color: Theme.text3
                                            font.family: Theme.mono
                                            font.pixelSize: Theme.fontSmall
                                            elide: Text.ElideRight
                                        }
                                    }
                                }
                            }
                        }
                    }
                }
            }
        }

        // Back to the newest message, when you have scrolled away from it.
        GhostButton {
            id: jumpEnd
            round: true
            icon: "send"
            z: 3
            readonly property bool wanted: thread.visible && !chat.stick
            anchors { horizontalCenter: thread.horizontalCenter; bottom: thread.bottom; bottomMargin: 12 }
            color: jumpEnd.hovered ? Theme.cardHover : Theme.card
            opacity: wanted ? 1 : 0
            visible: opacity > 0
            Behavior on opacity { NumberAnimation { duration: Theme.medium; easing.type: Theme.ease } }
            transform: Rotation { origin.x: jumpEnd.width / 2; origin.y: jumpEnd.height / 2; angle: 180 }
            onClicked: {
                chat.stick = true;
                thread.positionViewAtEnd();
            }
        }

        // Composer
        Column {
            id: composerBox
            anchors { bottom: parent.bottom; bottomMargin: 18; horizontalCenter: parent.horizontalCenter }
            width: pane.column
            spacing: 10

            ApprovalCards {
                width: parent.width
                items: chat.pendingHere
                agents: chat.agents
                onDecided: chat.loadPending()
            }
            MissionCards {
                width: parent.width
                items: chat.missionsHere
                agents: chat.agents
                onChanged: chat.loadMissions()
            }
            ErrorBanner {
                width: parent.width
                text: chat.error
            }
            Row {
                id: workLine
                visible: chat.workingHere
                spacing: 8
                height: 20
                leftPadding: 4
                opacity: visible ? 1 : 0
                Behavior on opacity { NumberAnimation { duration: Theme.medium; easing.type: Theme.ease } }
                Rectangle {
                    width: 7
                    height: 7
                    radius: 3.5
                    anchors.verticalCenter: parent.verticalCenter
                    color: chat.pendingHere.length ? Theme.warn : pane.tint
                    SequentialAnimation on opacity {
                        running: workLine.visible && Qt.application.state === Qt.ApplicationActive
                        loops: Animation.Infinite
                        NumberAnimation { to: 0.35; duration: 650; easing.type: Easing.InOutSine }
                        NumberAnimation { to: 1; duration: 650; easing.type: Easing.InOutSine }
                    }
                }
                Text {
                    anchors.verticalCenter: parent.verticalCenter
                    readonly property int secs: chat.workSince > 0 ? Math.max(0, Math.floor((chat.workNow - chat.workSince) / 1000)) : 0
                    text: (chat.agent ? chat.agent.name : "O agente") + " "
                          + (chat.pendingHere.length ? "espera sua permissão"
                             : chat.workStep !== "" ? "· " + U.stepLabel(chat.workStep.split(" · ")[0])
                             : "está pensando")
                          + "  ·  " + (secs < 60 ? secs + " s" : Math.floor(secs / 60) + " min " + (secs % 60) + " s")
                    color: Theme.text3
                    font.pixelSize: Theme.fontBody
                    font.features: { "tnum": 1 }
                }
            }
            ReplyBar {
                width: parent.width
                target: chat.replyTarget
                onCancel: chat.replyTarget = null
            }
            // Files that go with the next message.
            Flow {
                width: parent.width
                spacing: 6
                visible: chat.attachments.length > 0 || chat.uploading > 0
                Repeater {
                    model: chat.attachments
                    delegate: Rectangle {
                        required property var modelData
                        required property int index
                        height: 30
                        width: chipRow.implicitWidth + 16
                        radius: Theme.radiusSm
                        color: Theme.card
                        border.width: 1
                        border.color: Theme.line
                        Row {
                            id: chipRow
                            x: 8
                            anchors.verticalCenter: parent.verticalCenter
                            spacing: 6
                            Icon {
                                anchors.verticalCenter: parent.verticalCenter
                                name: modelData.mime.startsWith("image/") ? "image" : "file"
                                size: 13
                                color: Theme.text3
                            }
                            Text {
                                anchors.verticalCenter: parent.verticalCenter
                                text: modelData.name
                                width: Math.min(implicitWidth, 220)
                                elide: Text.ElideMiddle
                                color: Theme.text2
                                font.pixelSize: Theme.fontSmall
                            }
                            Icon {
                                anchors.verticalCenter: parent.verticalCenter
                                name: "close"
                                size: 12
                                color: dropX.containsMouse ? Theme.text : Theme.text4
                                MouseArea {
                                    id: dropX
                                    anchors.fill: parent
                                    anchors.margins: -5
                                    hoverEnabled: true
                                    cursorShape: Qt.PointingHandCursor
                                    onClicked: {
                                        const a = chat.attachments.slice();
                                        a.splice(index, 1);
                                        chat.attachments = a;
                                    }
                                }
                            }
                        }
                    }
                }
                Text {
                    visible: chat.uploading > 0
                    height: 30
                    verticalAlignment: Text.AlignVCenter
                    text: "Enviando arquivo…"
                    color: Theme.text4
                    font.pixelSize: Theme.fontSmall
                }
            }
            Item {
                width: parent.width
                height: composer.height
                FileDialog {
                    id: attachDialog
                    title: "Anexar arquivos"
                    fileMode: FileDialog.OpenFiles
                    onAccepted: chat.attachFiles(selectedFiles)
                }
                AreaField {
                    id: composer
                    width: parent.width
                    minHeight: 54
                    rightInset: 122
                    rich: true
                    pasteFilter: () => chat.pasteImage() || chat.pastePlain()
                    placeholderText: chat.agent ? "Mensagem para " + chat.agent.name + "…  ·  @ marca app, skill ou doc" : "Mensagem…"
                    onSubmitted: chat.send()
                    Component.onCompleted: {
                        area.forceActiveFocus();
                        chat.loadMentions();
                    }
                    keyFilter: event => {
                        if (chat.mentionList.length === 0)
                            return false;
                        if (event.key === Qt.Key_Up)
                            chat.mentionPick = (chat.mentionPick + chat.mentionList.length - 1) % chat.mentionList.length;
                        else if (event.key === Qt.Key_Down)
                            chat.mentionPick = (chat.mentionPick + 1) % chat.mentionList.length;
                        else if (event.key === Qt.Key_Escape)
                            chat.mentionList = [];
                        else
                            chat.completeMention(chat.mentionPick);
                        event.accepted = true;
                        return true;
                    }
                    Connections {
                        target: composer.area
                        function onTextChanged() { chat.updateMentions(); }
                        function onCursorPositionChanged() { chat.updateMentions(); }
                    }
                }
                // @ suggestions, above the field.
                Card {
                    visible: chat.mentionList.length > 0
                    anchors { bottom: composer.top; bottomMargin: 8; left: composer.left }
                    width: 340
                    height: mentionCol.implicitHeight + 12
                    padding: 6
                    radius: Theme.radiusLg
                    z: 5
                    Column {
                        id: mentionCol
                        width: parent.width
                        Repeater {
                            model: chat.mentionList
                            delegate: Rectangle {
                                id: mrow
                                required property var modelData
                                required property int index
                                readonly property string logo: modelData.icon ? Logos.of(modelData.icon) : ""
                                width: mentionCol.width
                                height: 44
                                radius: Theme.radiusSm
                                color: index === chat.mentionPick ? Theme.active : mArea.containsMouse ? Theme.hover : "transparent"
                                Item {
                                    id: mIcon
                                    x: 10
                                    width: 24
                                    height: 24
                                    anchors.verticalCenter: parent.verticalCenter
                                    Image {
                                        visible: source != ""
                                        anchors.centerIn: parent
                                        source: mrow.modelData.icon ? Logos.image(mrow.modelData.icon, mrow.modelData.name) : ""
                                        width: 20
                                        height: 20
                                        sourceSize: Qt.size(40, 40)
                                        fillMode: Image.PreserveAspectFit
                                        smooth: true
                                        mipmap: true
                                    }
                                    Icon {
                                        visible: !mrow.modelData.icon || Logos.image(mrow.modelData.icon) === ""
                                        anchors.centerIn: parent
                                        path: mrow.logo
                                        name: mrow.logo !== "" ? "" : mrow.modelData.kind === "doc" ? DocBus.icon(mrow.modelData.doc) : mrow.modelData.kind === "skill" ? "book" : mrow.modelData.kind === "tool" ? "globe" : U.connectorIcon(mrow.modelData.icon || "") || "plug"
                                        size: 18
                                        color: mrow.modelData.kind === "doc" ? DocBus.tone(mrow.modelData.doc) : mrow.modelData.icon && Logos.color(mrow.modelData.icon) !== "" ? Logos.color(mrow.modelData.icon) : Theme.text2
                                    }
                                }
                                Column {
                                    anchors { left: mIcon.right; leftMargin: 10; right: mKind.left; rightMargin: 8; verticalCenter: parent.verticalCenter }
                                    spacing: 1
                                    Text { text: mrow.modelData.name; color: Theme.text; font.pixelSize: Theme.fontBody; font.weight: Font.Medium; elide: Text.ElideRight; width: parent.width }
                                    Text { text: mrow.modelData.hint || ""; color: Theme.text4; font.pixelSize: Theme.fontCaption; elide: Text.ElideRight; width: parent.width; visible: text !== "" }
                                }
                                Text {
                                    id: mKind
                                    anchors { right: parent.right; rightMargin: 10; verticalCenter: parent.verticalCenter }
                                    text: mrow.modelData.kind === "doc" ? "DOC" : mrow.modelData.kind === "skill" ? "SKILL" : mrow.modelData.kind === "tool" ? "FERRAMENTA" : "APP"
                                    color: Theme.text4
                                    font.pixelSize: Theme.fontTiny
                                    font.weight: Font.DemiBold
                                    font.letterSpacing: Theme.trackCaps
                                }
                                MouseArea {
                                    id: mArea
                                    anchors.fill: parent
                                    hoverEnabled: true
                                    cursorShape: Qt.PointingHandCursor
                                    onClicked: {
                                        chat.completeMention(mrow.index);
                                        composer.area.forceActiveFocus();
                                    }
                                }
                            }
                        }
                    }
                }
                RowTool {
                    anchors { right: attachTool.left; rightMargin: 4; verticalCenter: sendBtn.verticalCenter }
                    icon: "docs"
                    tip: "Docs: abrir ao lado para citar (ou @ e o nome do doc)"
                    onHit: chat.openDocFloat("")
                }
                RowTool {
                    id: attachTool
                    anchors { right: sendBtn.left; rightMargin: 6; verticalCenter: sendBtn.verticalCenter }
                    icon: "file"
                    tip: "Anexar arquivos (ou cole uma imagem, ou arraste para cá)"
                    onHit: attachDialog.open()
                }
                Item {
                    id: sendBtn
                    readonly property bool ready: (composer.area.length > 0 || chat.attachments.length > 0) && chat.uploading === 0 || chat.busy
                    width: 38
                    height: 38
                    anchors { right: parent.right; rightMargin: 8; bottom: parent.bottom; bottomMargin: 8 }
                    RectangularShadow {
                        anchors.fill: knob
                        radius: knob.radius
                        blur: 18
                        color: Qt.rgba(1, 1, 1, sendBtn.ready ? 0.14 : 0)
                        Behavior on color { ColorAnimation { duration: Theme.medium } }
                    }
                    Rectangle {
                        id: knob
                        anchors.fill: parent
                        radius: width / 2
                        color: sendBtn.ready ? Theme.white : Theme.wash4
                        scale: sendArea.pressed ? 0.92 : 1
                        Behavior on color { ColorAnimation { duration: Theme.medium } }
                        Behavior on scale { NumberAnimation { duration: Theme.fast } }
                        Icon {
                            anchors.centerIn: parent
                            name: chat.busy ? "stop" : "send"
                            size: 17
                            color: sendBtn.ready ? Theme.ink : Theme.text3
                        }
                    }
                    MouseArea {
                        id: sendArea
                        anchors.fill: parent
                        cursorShape: Qt.PointingHandCursor
                        onClicked: chat.busy ? chat.stop() : chat.send()
                    }
                }
            }
            Text {
                anchors.horizontalCenter: parent.horizontalCenter
                width: Math.min(implicitWidth, parent.width)
                horizontalAlignment: Text.AlignHCenter
                elide: Text.ElideRight
                text: "Peça qualquer coisa: o agente age sozinho e mostra o que fez  ·  Enter envia, Shift+Enter quebra a linha"
                color: Theme.text4
                font.pixelSize: Theme.fontSmall
            }
        }
    }

    // ── Channel conversation ────────────────────────────────────
    ChannelView {
        id: room
        visible: chat.channel && !chat.personMode
        active: chat.channel && !chat.personMode && chat.visible
        channelId: chat.channelId
        onSettings: chanEditor.open(chat.channelNow, "")
        agents: chat.agents
        approvals: chat.pendingTeam
        onApprovalDecided: chat.loadPending()
        anchors {
            top: parent.top
            bottom: parent.bottom
            left: parent.left
            right: rightCol.left
        }
        onMoved: if (chat.channel) chat.seeChannel()
        onOpenRun: (id, ownerAgentId) => chat.openRun(id, ownerAgentId)
    }

    // ── Person DM empty state (server has no human-to-human DM yet) ──
    Item {
        id: personPane
        visible: chat.personMode
        anchors {
            top: chatHeader.bottom
            bottom: parent.bottom
            left: parent.left
            right: parent.right
        }

        Column {
            anchors.centerIn: parent
            width: Math.min(parent.width - 2 * Theme.space6, 440)
            spacing: Theme.space3

            Avatar {
                anchors.horizontalCenter: parent.horizontalCenter
                size: 84
                name: chat.currentPersonName
                photo: chat.currentPerson ? chat.personPhoto(chat.currentPerson.avatar) : ""
                tint: Theme.text2
            }

            Text {
                width: parent.width
                horizontalAlignment: Text.AlignHCenter
                wrapMode: Text.Wrap
                text: "Conversa com " + chat.currentPersonName + " em breve"
                color: Theme.white
                font.family: Theme.display
                font.pixelSize: Theme.fontDisplay
                font.weight: Font.DemiBold
            }

            Text {
                width: parent.width
                horizontalAlignment: Text.AlignHCenter
                wrapMode: Text.Wrap
                text: "Mensagens diretas entre pessoas do espaço ainda estão a caminho. Por enquanto, conversem juntos nos canais com o time."
                color: Theme.text3
                font.pixelSize: Theme.fontBody
            }

            GhostButton {
                anchors.horizontalCenter: parent.horizontalCenter
                icon: "hash"
                text: "Ir para #" + (chat.channelNow ? chat.channelNow.name : "time")
                onClicked: chat.openChannel(chat.channelId || "time")
            }
        }
    }

    // ── Per-conversation right column (280–420 px) ──────────────
    Rectangle {
        id: rightCol
        anchors { top: parent.top; bottom: parent.bottom; right: parent.right }
        width: chat.effectiveRightWidth
        visible: width > 0
        color: Theme.bg
        clip: true

        Rectangle {
            anchors { left: parent.left; top: parent.top; bottom: parent.bottom }
            width: 1
            color: rightResize.pressed || rightResize.containsMouse ? Theme.lineStrong : Theme.line
            Behavior on color { ColorAnimation { duration: Theme.fast } }
        }

        MouseArea {
            id: rightResize
            enabled: rightCol.visible
            anchors { left: parent.left; top: parent.top; bottom: parent.bottom; leftMargin: -3 }
            width: 7
            z: 20
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
                chat.rightWidth = chat.clampRightWidth(chat.rightWidth + fromX - x);
                fromX = x;
            }
            onReleased: chat.saveRightPrefs()
        }

        Item {
            anchors {
                top: parent.top
                topMargin: Theme.titleBar
                bottom: parent.bottom
                left: parent.left
                leftMargin: 1
                right: parent.right
            }

            ChannelMembers {
                anchors.fill: parent
                visible: chat.channel && !chat.personMode
                channelAgents: room.members
                channelWorking: room.working
                channelSteps: room.stepOf
                channelNextIds: room.nextIds
                approvals: chat.pending
                onOpenAgentRequested: id => chat.openAgent(id)
                onOpenPersonRequested: id => chat.openPerson(id)
            }

            AgentSide {
                anchors.fill: parent
                visible: !chat.channel && !chat.personMode && chat.agent !== null
                agent: chat.agent
                agents: chat.agents
                projects: chat.projects
                sectors: chat.sectors
                convId: chat.convId
                citedDocIds: chat.citedDocsHere
                working: chat.busy
                workStep: chat.workStep
                pendingCount: chat.pendingHere.length
                runsRev: chat.runsRev
                liveBrowserSite: chat.liveBrowserSite
                onChangeBrainRequested: {
                    if (chat.agent)
                        modelPicker.open(chat.agent);
                }
                onOpenBrowserRequested: (id, name) => chat.openBrowserFloat(id, name)
                onOpenRunRequested: (id, ownerAgentId) => chat.openRun(id, ownerAgentId)
                onOpenDocRequested: docId => chat.openDocFloat(docId)
                onNewRoutineRequested: {
                    if (chat.agent)
                        routineEditor.open(chat.agent, null, null);
                }
                onEditRoutineRequested: routine => {
                    if (chat.agent)
                        routineEditor.open(chat.agent, routine, null);
                }
            }
        }
    }

    // ── Wildcard app browser poller (keeps "app" ready on server) ──
    property bool wildcardPolling: false

    function findBrowserPanel(item, targetId, depth) {
        if (!item || depth > 10)
            return null;
        if (item.agentId === targetId && typeof item.executeCall === "function")
            return item;
        const kids = item.children || [];
        for (let i = 0; i < kids.length; i++) {
            const found = findBrowserPanel(kids[i], targetId, depth + 1);
            if (found)
                return found;
        }
        return null;
    }

    function afterMs(ms, fn) {
        const t = Qt.createQmlObject("import QtQuick; Timer {}", chat);
        t.interval = ms;
        t.triggered.connect(() => {
            t.destroy();
            fn();
        });
        t.start();
    }

    function pollWildcard() {
        if (!Api.signedIn) {
            wildcardPolling = false;
            return;
        }
        wildcardPolling = true;
        Api.post("/v1/browser/app/next", {}, (err, call) => {
            if (err || !call || !call.id) {
                chat.afterMs(err ? 4000 : 80, chat.pollWildcard);
                return;
            }
            const targetId = call.agent || chat.agentId || (chat.agents.length ? chat.agents[0].id : "default");
            const rootItem = (typeof win !== "undefined" && win && win.contentItem) ? win.contentItem : null;
            let bp = chat.findBrowserPanel(rootItem, targetId, 0);
            if (!bp)
                chat.openBrowserFloat(targetId, call.agent_name || "");
            chat.afterMs(100, () => {
                bp = chat.findBrowserPanel(rootItem, targetId, 0);
                if (bp) {
                    bp.executeCall(call);
                } else {
                    Api.post("/v1/browser/app/result", { id: call.id, agent: targetId, result: { ok: false, error: "navegador indisponível" } }, () => {});
                }
                chat.pollWildcard();
            });
        });
    }

    Connections {
        target: Api
        function onSignedInChanged() {
            if (Api.signedIn && !chat.wildcardPolling)
                chat.pollWildcard();
        }
    }

    Timer {
        interval: 6000
        repeat: true
        running: Api.signedIn
        triggeredOnStart: true
        onTriggered: {
            if (!chat.wildcardPolling)
                chat.pollWildcard();
        }
    }

    // ── Sheets & dialogs ────────────────────────────────────────
    MessageDetails {
        id: details
        anchors.fill: parent
    }

    ChannelEditor {
        id: chanEditor
        anchors.fill: parent
        agents: chat.agents
        projects: chat.projects
        onSaved: c => {
            chat.loadChannels();
            chat.openChannel(c.id);
        }
        onArchived: id => {
            chat.loadChannels();
            if (chat.channelId === id)
                chat.openChannel("time");
        }
    }

    RoutineEditor {
        id: routineEditor
        anchors.fill: parent
    }

    NewAgentSheet {
        id: newAgentSheet
        anchors.fill: parent
        onCreated: (a, newConvId) => chat.openCreatedAgent(a, newConvId)
    }

    ModelPicker {
        id: modelPicker
        anchors.fill: parent
        onSaved: chat.loadAgents(false)
    }

    // Drop files anywhere on the page to attach them; drop a doc to cite it.
    DropArea {
        id: dropZone
        property bool doc: false
        anchors.fill: parent
        keys: ["text/uri-list", "text/x-avora-doc"]
        onEntered: drag => doc = drag.formats.indexOf("text/x-avora-doc") >= 0
        onDropped: drop => {
            if (drop.formats.indexOf("text/x-avora-doc") >= 0) {
                const d = DocBus.byId(drop.getDataAsString("text/x-avora-doc"));
                if (d)
                    chat.citeDoc(d);
                drop.acceptProposedAction();
                return;
            }
            if (drop.hasUrls && chat.agent && !chat.channel && !chat.personMode)
                chat.attachFiles(drop.urls);
        }
        Rectangle {
            anchors.fill: parent
            anchors.margins: 12
            visible: dropZone.containsDrag
            radius: Theme.radiusLg
            color: Theme.hover
            border.width: 2
            border.color: Theme.lineStrong
            Text {
                anchors.centerIn: parent
                text: dropZone.doc ? "Solte para citar o doc na mensagem" : "Solte para anexar"
                color: Theme.text
                font.pixelSize: Theme.fontTitle
            }
        }
    }
}
