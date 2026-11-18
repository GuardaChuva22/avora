import QtQuick
import QtQuick.Controls.Basic
import QtQuick.Effects
import QtCore
import QtQuick.Dialogs
import Avora
import "util.js" as U
import "logos.js" as Logos

// Conversas com o time. Left: the funcionários, each with its own
// conversations. Middle: the open conversation, or the agent's page.
//
// Every agent answers word by word and acts through tools: the page shows
// what it did as cards (a routine created, a task for Claude Code) and
// follows a long answer live, even after the app reopens.
Item {
    id: chat
    property var agents: []
    signal goPage(string name) // Plugins, Skills: pages of the window
    // "@Name" in an answer becomes a chip in that agent's color.
    readonly property var teamChips: agents.map(a => ({ name: a.name, color: a.color || "" }))
    property string agentId: ""
    property bool channel: false // a channel is open
    property string channelId: "time" // the channel open (or last open)
    property var channels: [] // GET /v1/channels
    readonly property var channelNow: channels.find(c => c.id === channelId) || null
    readonly property var channelIds: channels.map(c => c.id)
    property string convId: ""
    property string error: ""
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
            DocBus.open(link.slice(10).split(/[#?]/)[0]);
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
            if (kind === "connect")
                chat.goPage("modelos:connect");
            else if (kind === "brain" && targetId !== "")
                agentPage.open(targetId, "settings");
            else if (kind === "tools" && targetId !== "")
                agentPage.open(targetId, "tools");
            else if (kind === "channels") {
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
    }
    Timer {
        interval: 1000
        repeat: true
        running: chat.workingHere && Qt.application.state === Qt.ApplicationActive
        onTriggered: chat.workNow = Date.now()
    }
    property bool convsOpen: width > 1000 // the conversation list on the right
    property var live: ({}) // convId -> { job, after, steps: [] }
    property var seen: ({}) // agentId -> ISO time last read
    property var projects: []
    property var collapsed: ({}) // projectId -> true when folded
    property var dragAgent: null // the agent being dragged to a section
    property var dropTo: undefined // the section it was dropped on, until the release ends
    // endDrag lets go of the drag first and moves the agent afterwards:
    // moving rebuilds the sections, which destroys the row whose mouse
    // handler is still running, and anything after that in the handler
    // (clearing the drag) would never run.
    function endDrag(drop) {
        const a = dragAgent;
        dropTo = undefined;
        if (drop && a !== null)
            ghost.Drag.drop();
        const target = dropTo;
        dropTo = undefined;
        dragAgent = null;
        if (a !== null && target !== undefined)
            Qt.callLater(() => moveAgent(a, target));
    }

    readonly property var agent: agents.find(a => a.id === agentId) || null
    readonly property bool isNative: true // every agent; Claude Code and Codex are its tools
    readonly property bool nativeBusy: Api.streaming
    readonly property bool working: convId !== "" && live[convId] !== undefined
    readonly property bool busy: (nativeBusy && streamConv === convId) || working
    readonly property bool empty: msgs.count === 0
    // A conversation is on its way: the welcome waits, a skeleton stands in.
    property bool loadingConv: false

    Settings {
        category: "chat"
        property alias lastAgent: chat.agentId
        property alias lastChannel: chat.channel
        property alias lastChannelId: chat.channelId
        property alias seenJson: chat.seenStore
        property alias collapsedJson: chat.collapsedStore
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
        loadProjects();
        loadChannels();
        loadAgents(true);
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
        function onLiveEvent(e) {
            if (e.type === "channel.saved" || e.type === "message.created" && chat.channelIds.includes((e.payload || {}).conversation_id))
                chanReload.restart();
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

    // ── Projects ────────────────────────────────────────────────
    // sheet opens an editor at start (QA and deep links): "projeto" a new
    // project, "projeto-<id>" an existing one, "conversas" the list,
    // "agente-<id>[.<tab>]" the agent's page.
    property string sheet: ""
    onProjectsChanged: openSheet()
    onSheetChanged: if (sheet !== "") openSheet()
    onAgentsChanged: if (sheet !== "") openSheet()
    onChannelsChanged: if (sheet.startsWith("canal-") || sheet.startsWith("ver-")) openSheet()
    function openSheet() {
        if (sheet === "projeto")
            projectEditor.open(null);
        else if (sheet.startsWith("projeto-")) {
            const p = projects.find(x => x.id === sheet.slice(8));
            if (!p)
                return;
            projectEditor.open(p);
        } else if (sheet === "conversas")
            convOpen.start();
        else if (sheet === "time")
            selectChannel("time");
        else if (sheet === "canal")
            chanEditor.open(null, "");
        else if (sheet.startsWith("ver-")) {
            if (!channels.some(x => x.id === sheet.slice(4)))
                return;
            selectChannel(sheet.slice(4));
        }
        else if (sheet.startsWith("canal-editar-")) {
            const c = channels.find(x => x.id === sheet.slice(13));
            if (!c)
                return;
            selectChannel(c.id);
            chanEditor.open(c, "");
        }
        else if (sheet.startsWith("editar-")) {
            const a = agents.find(x => x.id === sheet.slice(7));
            if (!a)
                return;
            agentPage.open(a.id, "settings");
        } else if (sheet.startsWith("agente-")) { // "agente-atlas" or "agente-atlas.usage"
            const parts = sheet.slice(7).split(".");
            if (!agents.find(x => x.id === parts[0]))
                return;
            agentId = parts[0]; // the first load selects it instead of the first agent
            agentPage.open(parts[0], parts[1] || "");
        } else
            return;
        sheet = "";
    }
    Timer {
        id: convOpen
        interval: 250
        onTriggered: chat.convsOpen = true
    }
    readonly property var project: agent && agent.project_id ? projects.find(p => p.id === agent.project_id) || null : null

    function loadProjects() {
        Api.get("/v1/projects", (err, data) => {
            if (!err)
                projects = data;
        });
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

    // moveAgent puts an agent in a project ("" = universal), at once in the
    // list and then on the server.
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
    ListModel { id: msgs }

    // ── Data ────────────────────────────────────────────────────
    function engineLabel(a) {
        return "nenhuma conversa ainda";
    }

    function unread(a) {
        return !!a.last_at && a.last_role === "assistant" && (channel || a.id !== agentId) && (!seen[a.id] || seen[a.id] < a.last_at);
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

    property var archivedAgents: [] // GET /v1/agents/archived
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
                const inChannel = channel; // reopen #time if it was open
                selectAgent(agents.find(a => a.id === agentId) ? agentId : (agents.length ? agents[0].id : ""));
                channel = inChannel;
            } else if (pendingAskAgent !== "" && agents.some(a => a.id === pendingAskAgent)) {
                const askId = pendingAskAgent, askMsg = pendingAskText;
                pendingAskAgent = "";
                pendingAskText = "";
                Qt.callLater(() => chat.askAgent(askId, askMsg));
            } else
                markSeen(agentId);
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

    function selectChannel(id) {
        channelId = id || "time";
        channel = true;
        seeChannel();
    }

    // Opens one conversation of an agent (a note's card opens the
    // colleague's conversation).
    property string wantConv: ""
    property string pendingAskAgent: ""
    property string pendingAskText: ""
    function openAgentConv(id, conv) {
        if (id === agentId && !channel) {
            open(conv);
            return;
        }
        wantConv = conv;
        selectAgent(id);
    }
    function openNewAgent(project) {
        newAgentSheet.open(project || "");
    }
    function openCreatedAgent(a, conv) {
        if (!a || !a.id)
            return;
        if (agentPage.shown)
            agentPage.close();
        runId = "";
        if (!agents.some(x => x.id === a.id))
            agents = agents.concat([a]);
        wantConv = conv || "";
        selectAgent(a.id);
        loadAgents(false);
    }
    function askAgent(id, text) {
        const clean = (text || "").trim();
        if (!id || clean === "")
            return;
        if (agentPage.shown)
            agentPage.close();
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
        Api.get(convQuery(id), (err, data) => {
            if (!err && agentId === id)
                U.syncModel(convs, data.map(convRow));
            newConversation(clean, () => streamNative(clean, 0));
        });
    }

    function selectAgent(id) {
        if (agentPage.shown && id !== "" && id !== agentPage.agentId)
            agentPage.open(id, "");
        channel = false;
        if (id !== runOwner)
            runId = "";
        agentId = id;
        markSeen(id);
        convs.clear();
        open("");
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
            U.syncModel(convs, data.map(convRow));
            for (const c of data)
                if (c.job_id && !live[c.id])
                    follow(c.id, c.job_id);
            // The newest conversation the owner had, not the heartbeat's
            // own (it runs every 30 min and would always come first).
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
            convs.insert(0, { id: c.id, title: c.title || first.slice(0, 60), preview: "", updated_at: c.created_at, job_id: "" });
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
        return t.toLowerCase().normalize("NFD").replace(/[̀-ͯ]/g, "");
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
        function onCiteRequested(doc) {
            Qt.callLater(() => chat.citeDoc(doc));
        }
    }
    // ── chips inside the text: one picture each, drawn by image://chip ──
    function chipTone(m) {
        if (m.kind === "doc")
            return DocBus.tone(m.doc || DocBus.byId(m.id));
        const c = m.icon ? Logos.color(m.icon) : "";
        return c !== "" ? c : m.kind === "skill" ? "#A78BFA" : m.kind === "tool" ? "#60A5FA" : String(Theme.text2);
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
            return; // already followed (the live stream may tell first)
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
                    if (i >= 0 && convId === conv)
                        msgs.setProperty(i, "steps", JSON.stringify(l.steps));
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
    // Claude Code / Codex runs the agents delegated: the running ones show
    // as threads under their agent; runId is the one open in RunPane.
    property var runs: []
    property int runsRev: 0
    property int msgsRev: 0
    property string runId: ""
    property string runOwner: "" // the agent of the open run
    function loadRuns() {
        Api.get("/v1/runs?active=1", (err, list) => {
            if (!err && !U.same(chat.runs, list || [])) {
                chat.runs = list || [];
                chat.runsRev++;
            }
        });
    }
    function openRun(id, agentId) {
        runOwner = agentId || agentId === "" ? agentId : chat.agentId;
        runId = id;
    }
    function openDocsTab() {
        if (agentPage.shown)
            agentPage.close();
        sidePanel.openTab("docs", "");
    }
    function closeSidePanel() {
        sidePanel.closePanel();
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
    readonly property string activeConvKey: channel ? ("#" + channelId) : (convId !== "" ? convId : (agentId !== "" ? "agent:" + agentId : ""))
    readonly property string activeConvId: channel ? channelId : convId
    readonly property var sideAgents: channel ? room.members : (agent ? [agent] : [])
    readonly property var sideAgentStatus: {
        const out = {};
        for (const a of sideAgents) {
            if (!a)
                continue;
            if (channel) {
                const w = room.working ? room.working[a.id] : undefined;
                const st = w && w.job_id && room.stepOf ? room.stepOf[w.job_id] : undefined;
                const inQueue = (room.nextIds || []).indexOf(a.id) >= 0;
                const busyNow = w !== undefined || !!a.working;
                let line = "";
                if (needsYou(a.id))
                    line = "Precisa da sua aprovação";
                else if (st && st.tool)
                    line = U.stepLabel(st.tool.split(" · ")[0]) + (st.summary ? " · " + st.summary : "");
                else if (w !== undefined)
                    line = "Trabalhando neste canal…";
                else if (inQueue)
                    line = "Na fila para responder…";
                else if (a.status_text)
                    line = a.status_text;
                else if (a.working)
                    line = "Trabalhando…";
                else
                    line = a.role || "Livre";
                out[a.id] = { working: busyNow, status: line };
            } else {
                const here = a.id === agentId && busy;
                const busyNow = here || !!a.working;
                let line = "";
                if (a.id === agentId && pendingHere.length > 0)
                    line = "Esperando sua permissão";
                else if (needsYou(a.id))
                    line = "Precisa da sua aprovação";
                else if (a.id === agentId && workStep !== "")
                    line = U.stepLabel(workStep.split(" · ")[0]);
                else if (here)
                    line = "Trabalhando nesta conversa…";
                else if (a.status_text)
                    line = a.status_text;
                else if (a.working)
                    line = "Trabalhando…";
                else
                    line = a.role || "Livre";
                out[a.id] = { working: busyNow, status: line };
            }
        }
        return out;
    }
    readonly property var citedDocsHere: {
        msgsRev;
        const ids = [];
        const seen = {};
        const addId = id => {
            if (id && !seen[id]) {
                seen[id] = true;
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
        if (!channel) {
            for (let i = 0; i < msgs.count; i++) {
                const row = msgs.get(i);
                scanText(row.content);
                scanSteps(row.steps);
            }
        }
        return ids;
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
    // While a mission runs, its agent is woken here when a round ends: follow
    // that answer as if it had been asked from the app.
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

    // Keeps the team list fresh (who is working, last messages).
    Timer {
        interval: Api.live ? 30000 : 6000
        repeat: true
        running: chat.visible
        onTriggered: {
            chat.loadAgents(false);
        }
    }

    // ── Archive / delete ────────────────────────────────────────
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
        refreshConvs();
    }

    // Leaves the open conversation when it drops out of the list.
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
        Api.get(convQuery(id), (err, data) => {
            if (err || agentId !== id)
                return;
            U.syncModel(convs, data.map(convRow));
            // An answer started elsewhere (a note, a routine): follow it.
            for (const c of data)
                if (c.job_id && !live[c.id])
                    follow(c.id, c.job_id);
        });
    }

    // ── Live (D47) ──────────────────────────────────────────────
    // The server pushes every event as it happens (Api.liveEvent); each
    // one reads only what it changed, and a burst of events reads once.
    // The timers above stay as a slow safety net while the stream is up.
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

    // ownStream: this app is streaming that conversation's answer itself.
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

    // catchUp adds the messages of the open conversation the screen lacks,
    // in place of the temporary ones (the sent text, the answer being
    // written), without reloading the list: nothing on screen jumps.
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
                    // A colleague's answer to a note: its own row, before
                    // any answer still being written.
                    const d = draftIndex();
                    if (d >= 0)
                        msgs.insert(d, m);
                    else
                        msgs.append(m);
                } else {
                    if (ownStream(conv))
                        continue; // the stream puts its own answer in place
                    const d = draftIndex();
                    if (d >= 0)
                        msgs.set(d, m);
                    else
                        msgs.append(m);
                }
            }
            // An answer started elsewhere (a mission, a routine): show it
            // being written.
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
            if (i >= 0 && conv === convId)
                msgs.setProperty(i, "steps", JSON.stringify(live[conv].steps));
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
        // Back after a long drop (or first connect): read it all once.
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
            // The stream ended before the answer (connection dropped): the
            // server goes on, so follow the answer there.
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

    // ── Team ────────────────────────────────────────────────────
    // The left column: #time, then each project with its agents, then the
    // universal agents. Conversations live in the header's picker, so the
    // list stays one row per agent. Agents are dragged between sections.
    // A channel in the sidebar: its name, what is happening, unread.
    component ChanRow: Rectangle {
        id: cr
        property var c: null
        readonly property bool on: chat.channel && chat.channelId === (c ? c.id : "")
        readonly property bool busy: on && room.busy
        readonly property bool unread: chat.channelUnreadOf(c)
        height: 40
        radius: Theme.radiusMd
        color: on ? Theme.active : crArea.containsMouse ? Theme.hover : "transparent"
        border.width: on ? 1 : 0
        border.color: Theme.active
        Behavior on color { ColorAnimation { duration: Theme.fast } }
        Icon {
            id: crHash
            x: 14
            anchors.verticalCenter: parent.verticalCenter
            name: "hash"
            size: 14
            color: cr.on || cr.unread ? Theme.white : Theme.text3
        }
        Text {
            id: crName
            anchors { left: crHash.right; leftMargin: 9; verticalCenter: parent.verticalCenter }
            width: Math.min(implicitWidth, cr.width * 0.55)
            text: cr.c ? cr.c.name : ""
            color: cr.on ? Theme.white : cr.unread ? Theme.text : Theme.text2
            font.pixelSize: Theme.fontLarge
            font.weight: cr.on || cr.unread ? Font.DemiBold : Font.Normal
            elide: Text.ElideRight
        }
        Text {
            anchors { left: crName.right; leftMargin: 8; right: crDot.visible ? crDot.left : parent.right; rightMargin: 12; verticalCenter: parent.verticalCenter }
            text: cr.busy ? room.names(room.workingIds) + " trabalhando…"
                  : cr.c && cr.c.preview ? cr.c.preview : ""
            color: cr.busy ? Theme.text2 : Theme.text4
            font.pixelSize: Theme.fontSmall
            elide: Text.ElideRight
        }
        Rectangle {
            id: crDot
            visible: cr.unread
            anchors { right: parent.right; rightMargin: 14; verticalCenter: parent.verticalCenter }
            width: 7
            height: 7
            radius: 3.5
            color: Theme.white
        }
        MouseArea {
            id: crArea
            anchors.fill: parent
            hoverEnabled: true
            cursorShape: Qt.PointingHandCursor
            onClicked: chat.selectChannel(cr.c.id)
        }
    }

    component IconBtn: Rectangle {
        id: ib
        property string icon
        property string tip
        signal hit
        width: 30
        height: 30
        radius: Theme.radiusSm
        color: ibArea.pressed ? Theme.active : ibArea.containsMouse ? Theme.hover : "transparent"
        Behavior on color { ColorAnimation { duration: Theme.fast } }
        Icon {
            anchors.centerIn: parent
            name: ib.icon
            size: 16
            weight: 1.5
            color: ibArea.containsMouse ? Theme.text : Theme.text3
        }
        MouseArea {
            id: ibArea
            anchors.fill: parent
            hoverEnabled: true
            cursorShape: Qt.PointingHandCursor
            onClicked: ib.hit()
        }
        Tip {
            text: ib.tip
            show: ibArea.containsMouse && ib.tip !== ""
        }
    }

    Item {
        id: team
        width: 290
        anchors { top: parent.top; bottom: parent.bottom; left: parent.left }

        property var sectors: []
        property var hoverSector: null
        readonly property var sectorIds: sectors.map(s => s.id)
        readonly property var pinnedAgents: chat.agents.filter(a => !!a.pinned).slice(0, 5)

        function loadSectors() {
            Api.get("/v1/sectors", (err, data) => {
                if (!err && data)
                    team.sectors = data;
            });
        }
        function togglePin(a) {
            if (!a)
                return;
            if (!a.pinned && pinnedAgents.length >= 5) {
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

        Component.onCompleted: loadSectors()

        Connections {
            target: Api
            function onWorkspaceChanged() {
                team.sectors = [];
                team.loadSectors();
            }
            function onLiveEvent(e) {
                if (!e)
                    return;
                if (e.type === "sector.saved" || e.type === "sector.deleted")
                    team.loadSectors();
            }
        }

        component TeamAgentRow: Column {
            id: rowBox
            property var agentData: null
            readonly property var myRuns: agentData ? chat.runs.filter(r => r.agent_id === agentData.id) : []

            Rectangle {
                id: row
                readonly property var a: rowBox.agentData || ({})
                readonly property bool on: !chat.channel && a.id === chat.agentId
                readonly property bool lifted: chat.dragAgent !== null && chat.dragAgent.id === a.id
                width: rowBox.width
                height: 64
                radius: Theme.radiusLg
                color: on ? Theme.active : rowArea.containsMouse && chat.dragAgent === null ? Theme.hover : "transparent"
                border.width: on ? 1 : 0
                border.color: Theme.active
                opacity: lifted ? 0.35 : 1
                Behavior on color { ColorAnimation { duration: Theme.fast } }

                Avatar {
                    id: face
                    x: 9
                    anchors.verticalCenter: parent.verticalCenter
                    size: 44
                    name: row.a.name || ""
                    photo: U.photo(row.a)
                    tint: row.a.color || Theme.text2
                    working: !!row.a.working
                }
                Column {
                    anchors { left: face.right; leftMargin: Theme.space3; right: parent.right; rightMargin: Theme.space3; verticalCenter: parent.verticalCenter }
                    spacing: 3
                    Item {
                        width: parent.width
                        height: nameText.height
                        Text {
                            id: nameText
                            text: row.a.name || ""
                            color: row.on ? Theme.white : Theme.text
                            font.pixelSize: Theme.fontLarge
                            font.weight: Font.DemiBold
                        }
                        Icon {
                            id: pinMark
                            visible: !!row.a.pinned
                            anchors { left: nameText.right; leftMargin: Theme.space1; verticalCenter: nameText.verticalCenter }
                            name: "pin"
                            size: 11
                            color: Theme.text3
                        }
                        Dot {
                            id: statusDot
                            anchors { left: pinMark.visible ? pinMark.right : nameText.right; leftMargin: 6; verticalCenter: nameText.verticalCenter }
                            kind: (row.a.status === "waiting" || chat.needsYou(row.a.id)) ? "warn" : (row.a.status === "working" || row.a.working) ? "live" : ""
                        }
                        Text {
                            anchors {
                                left: statusDot.right
                                leftMargin: 6
                                right: when.left
                                rightMargin: 6
                                baseline: nameText.baseline
                            }
                            text: row.a.status === "working" && row.a.status_channel ? row.a.status_channel : (row.a.role || "")
                            color: Theme.text4
                            font.pixelSize: Theme.fontSmall
                            elide: Text.ElideRight
                        }
                        Text {
                            id: when
                            anchors { right: parent.right; baseline: nameText.baseline }
                            text: row.a.last_at ? U.ago(row.a.last_at).replace("há ", "") : ""
                            color: Theme.text4
                            font.pixelSize: Theme.fontCaption
                        }
                    }
                    Item {
                        width: parent.width
                        height: 16
                        Text {
                            anchors { left: parent.left; right: badge.visible ? badge.left : parent.right; rightMargin: 6; verticalCenter: parent.verticalCenter }
                            text: chat.needsYou(row.a.id) ? "precisa de você"
                                  : row.a.working ? "trabalhando…"
                                  : row.a.preview ? (row.a.last_role === "user" ? "Você: " : "") + row.a.preview.replace(/[*_`#>]/g, "")
                                  : "nenhuma conversa ainda"
                            color: chat.needsYou(row.a.id) ? Theme.warn
                                   : row.a.working ? Qt.lighter(row.a.color || Theme.text2, 1.1) : chat.unread(row.a) ? Theme.text : Theme.text3
                            font.pixelSize: Theme.fontSmall
                            font.weight: chat.unread(row.a) ? Font.Medium : Font.Normal
                            elide: Text.ElideRight
                        }
                        Rectangle {
                            id: badge
                            visible: chat.unread(row.a)
                            anchors { right: parent.right; verticalCenter: parent.verticalCenter }
                            width: 7
                            height: 7
                            radius: height / 2
                            color: row.a.color || Theme.white
                        }
                    }
                }
                MouseArea {
                    id: rowArea
                    anchors.fill: parent
                    hoverEnabled: true
                    preventStealing: true
                    acceptedButtons: Qt.LeftButton | Qt.RightButton
                    cursorShape: chat.dragAgent !== null ? Qt.ClosedHandCursor : Qt.PointingHandCursor
                    property point start
                    onPressed: m => start = Qt.point(m.x, m.y)
                    onPositionChanged: m => {
                        if (!pressed || m.buttons !== Qt.LeftButton)
                            return;
                        const p = mapToItem(team, m.x, m.y);
                        if (chat.dragAgent === null && Math.abs(m.x - start.x) + Math.abs(m.y - start.y) > 8)
                            chat.dragAgent = row.a;
                        if (chat.dragAgent !== null) {
                            ghost.x = p.x - 20;
                            ghost.y = p.y - ghost.height / 2;
                        }
                    }
                    onReleased: {
                        if (chat.dragAgent !== null && team.hoverSector !== null) {
                            const targetSector = team.hoverSector;
                            const dragged = chat.dragAgent;
                            team.hoverSector = null;
                            chat.endDrag(false);
                            team.moveAgentToSector(dragged, targetSector);
                        } else {
                            team.hoverSector = null;
                            chat.endDrag(true);
                        }
                    }
                    onCanceled: {
                        team.hoverSector = null;
                        chat.endDrag(false);
                    }
                    onClicked: m => {
                        if (m.button === Qt.RightButton) {
                            movePopup.agent = row.a;
                            const p = mapToItem(chat, m.x, m.y);
                            movePopup.x = p.x;
                            movePopup.y = p.y;
                            movePopup.open();
                        } else if (!row.on) {
                            chat.selectAgent(row.a.id);
                        } else {
                            chat.runId = "";
                        }
                    }
                }
            }
            Repeater {
                model: rowBox.myRuns
                delegate: Item {
                    id: runRow
                    required property var modelData
                    readonly property bool on: chat.runId === modelData.id
                    width: rowBox.width
                    height: 34
                    Rectangle {
                        x: 31
                        width: 1
                        height: 17
                        color: Theme.lineStrong
                    }
                    Rectangle {
                        x: 31
                        y: 17
                        width: 11
                        height: 1
                        color: Theme.lineStrong
                    }
                    Rectangle {
                        x: 46
                        width: parent.width - 46
                        height: parent.height
                        radius: Theme.radiusMd
                        color: runRow.on ? Theme.active : runArea.containsMouse ? Theme.hover : "transparent"
                        Icon {
                            x: 10
                            anchors.verticalCenter: parent.verticalCenter
                            name: runRow.modelData.engine === "codex" ? "openai" : "claude"
                            size: 14
                            color: Theme.text2
                        }
                        Text {
                            x: 32
                            width: parent.width - 54
                            anchors.verticalCenter: parent.verticalCenter
                            text: runRow.modelData.title
                            color: runRow.on ? Theme.text : Theme.text2
                            font.pixelSize: Theme.fontSmall
                            elide: Text.ElideRight
                        }
                        Rectangle {
                            anchors.right: parent.right
                            anchors.rightMargin: Theme.space3
                            anchors.verticalCenter: parent.verticalCenter
                            width: 6
                            height: 6
                            radius: height / 2
                            color: Theme.live
                        }
                        MouseArea {
                            id: runArea
                            anchors.fill: parent
                            hoverEnabled: true
                            cursorShape: Qt.PointingHandCursor
                            onClicked: {
                                if (chat.channel || chat.agentId !== runRow.modelData.agent_id)
                                    chat.selectAgent(runRow.modelData.agent_id);
                                chat.openRun(runRow.modelData.id, runRow.modelData.agent_id);
                            }
                        }
                    }
                }
            }
        }

        Rectangle {
            anchors { top: parent.top; bottom: parent.bottom; right: parent.right }
            width: 1
            color: Theme.line
        }
        Item {
            id: teamHead
            anchors { top: parent.top; topMargin: 46; left: parent.left; right: parent.right; leftMargin: 20; rightMargin: 12 }
            height: 36
            Column {
                anchors.verticalCenter: parent.verticalCenter
                spacing: 1
                Text { text: "Time"; color: Theme.text; font.pixelSize: Theme.fontLarge; font.weight: Font.DemiBold }
                Text {
                    readonly property int busyCount: chat.agents.filter(a => a.working).length
                    text: chat.agents.length + " funcionários" + (busyCount ? "  ·  " + busyCount + " trabalhando" : "")
                    color: Theme.text4
                    font.pixelSize: Theme.fontSmall
                }
            }
            Row {
                anchors { right: parent.right; verticalCenter: parent.verticalCenter }
                spacing: 2
                IconBtn {
                    visible: chat.archivedAgents.length > 0
                    icon: "archive"
                    tip: "Arquivados (" + chat.archivedAgents.length + ")"
                    onHit: archivedSheet.open()
                }
                IconBtn {
                    icon: "users"
                    tip: "Setores"
                    onHit: sectorsEditor.open(null)
                }
                IconBtn {
                    icon: "folder"
                    tip: "Novo projeto"
                    onHit: projectEditor.open(null)
                }
                IconBtn {
                    icon: "plus"
                    tip: "Contratar funcionário"
                    onHit: newAgentSheet.open("")
                }
            }
        }

        Flickable {
            id: teamList
            anchors { top: teamHead.bottom; topMargin: 14; bottom: parent.bottom; left: parent.left; right: parent.right }
            contentHeight: teamCol.implicitHeight + 10
            clip: true
            boundsBehavior: Flickable.StopAtBounds
            ScrollBar.vertical: SlimScrollBar {}
            SmoothWheel {}

            Column {
                id: teamCol
                x: 10
                width: teamList.width - 20
                spacing: 4

                // Channels with no project; each project lists its own.
                Item {
                    width: parent.width
                    height: 30
                    Text {
                        x: 12
                        anchors.verticalCenter: parent.verticalCenter
                        text: "CANAIS"
                        color: Theme.text4
                        font.pixelSize: Theme.fontCaption
                        font.weight: Font.DemiBold
                        font.letterSpacing: Theme.trackCaps
                    }
                    RowTool {
                        anchors { right: parent.right; rightMargin: 5; verticalCenter: parent.verticalCenter }
                        icon: "plus"
                        tip: "Novo canal"
                        onHit: chanEditor.open(null, "")
                    }
                }
                Repeater {
                    model: chat.channelsOf("")
                    delegate: ChanRow {
                        required property var modelData
                        c: modelData
                        width: teamCol.width
                    }
                }
                Item { width: 1; height: Theme.space2 }

                // Pinned agents at top (up to 5).
                Column {
                    id: pinnedBox
                    visible: team.pinnedAgents.length > 0
                    width: teamCol.width
                    spacing: 2
                    readonly property bool folded: chat.collapsed["pinned:"] === true

                    Item {
                        width: parent.width
                        height: 32
                        Rectangle {
                            anchors.fill: parent
                            radius: Theme.radiusSm
                            color: pinHeadArea.containsMouse ? Theme.hover : "transparent"
                        }
                        Icon {
                            id: pinChev
                            x: 8
                            anchors.verticalCenter: parent.verticalCenter
                            name: "chevron"
                            size: 12
                            color: Theme.text4
                            rotation: pinnedBox.folded ? 0 : 90
                            Behavior on rotation { NumberAnimation { duration: Theme.fast } }
                        }
                        Icon {
                            id: pinHeadIcon
                            anchors { left: pinChev.right; leftMargin: Theme.space2; verticalCenter: parent.verticalCenter }
                            name: "pin"
                            size: 12
                            color: Theme.text3
                        }
                        Text {
                            id: pinHeadLabel
                            anchors { left: pinHeadIcon.right; leftMargin: Theme.space2; verticalCenter: parent.verticalCenter }
                            text: "FIXADOS"
                            color: Theme.text3
                            font.pixelSize: Theme.fontCaption
                            font.weight: Font.DemiBold
                            font.letterSpacing: Theme.trackCaps
                        }
                        Text {
                            anchors { left: pinHeadLabel.right; leftMargin: Theme.space2; verticalCenter: parent.verticalCenter }
                            text: team.pinnedAgents.length + "/5"
                            color: Theme.text4
                            font.pixelSize: Theme.fontCaption
                        }
                        MouseArea {
                            id: pinHeadArea
                            anchors.fill: parent
                            hoverEnabled: true
                            cursorShape: Qt.PointingHandCursor
                            onClicked: chat.toggleProject("pinned:")
                        }
                    }

                    Repeater {
                        model: pinnedBox.folded ? [] : team.pinnedAgents
                        delegate: TeamAgentRow {
                            required property var modelData
                            agentData: modelData
                            width: pinnedBox.width
                        }
                    }
                    Item { width: 1; height: 6 }
                }

                // Sectors with collapsible headers.
                Repeater {
                    model: team.sectors
                    delegate: Item {
                        id: sectorSec
                        required property var modelData
                        readonly property string sid: modelData.id
                        readonly property string foldKey: "sector:" + sid
                        readonly property bool folded: chat.collapsed[foldKey] === true
                        readonly property var members: chat.agents.filter(a => !a.pinned && (a.sector || "") === sid)
                        readonly property string lead: team.leaderName(modelData.lead_agent_id)
                        readonly property bool target: sectorDrop.containsDrag && chat.dragAgent !== null && (chat.dragAgent.sector || "") !== sid
                        width: teamCol.width
                        height: sectorCol.implicitHeight

                        DropArea {
                            id: sectorDrop
                            anchors.fill: parent
                            onEntered: team.hoverSector = sectorSec.sid
                            onExited: if (team.hoverSector === sectorSec.sid) team.hoverSector = null
                            Rectangle {
                                anchors.fill: parent
                                anchors.margins: -2
                                radius: Theme.radiusLg
                                color: sectorSec.target ? Theme.hover : "transparent"
                                border.width: sectorSec.target ? 1 : 0
                                border.color: sectorSec.modelData.color || Theme.lineStrong
                            }
                        }

                        Column {
                            id: sectorCol
                            width: parent.width
                            spacing: 2

                            Item {
                                width: sectorSec.width
                                height: 34
                                Rectangle {
                                    anchors.fill: parent
                                    radius: Theme.radiusSm
                                    color: sectorHeadArea.containsMouse ? Theme.hover : "transparent"
                                }
                                Icon {
                                    id: sectorChev
                                    x: 8
                                    anchors.verticalCenter: parent.verticalCenter
                                    name: "chevron"
                                    size: 12
                                    color: Theme.text4
                                    rotation: sectorSec.folded ? 0 : 90
                                    Behavior on rotation { NumberAnimation { duration: Theme.fast } }
                                }
                                Icon {
                                    id: sectorIcon
                                    anchors { left: sectorChev.right; leftMargin: Theme.space2; verticalCenter: parent.verticalCenter }
                                    name: sectorSec.modelData.icon || "users"
                                    size: 14
                                    color: sectorSec.modelData.color || Theme.text2
                                }
                                Text {
                                    id: sectorTitle
                                    anchors { left: sectorIcon.right; leftMargin: Theme.space2; verticalCenter: parent.verticalCenter }
                                    text: sectorSec.modelData.name
                                    color: Theme.text2
                                    font.pixelSize: Theme.fontBody
                                    font.weight: Font.DemiBold
                                }
                                Text {
                                    id: sectorSub
                                    anchors {
                                        left: sectorTitle.right
                                        leftMargin: Theme.space2
                                        right: sectorEditTool.visible ? sectorEditTool.left : parent.right
                                        rightMargin: Theme.space2
                                        verticalCenter: parent.verticalCenter
                                    }
                                    text: (sectorSec.lead ? "· " + sectorSec.lead + "  " : "") + sectorSec.members.length
                                    color: Theme.text4
                                    font.pixelSize: Theme.fontCaption
                                    elide: Text.ElideRight
                                }
                                MouseArea {
                                    id: sectorHeadArea
                                    anchors.fill: parent
                                    hoverEnabled: true
                                    cursorShape: Qt.PointingHandCursor
                                    onClicked: chat.toggleProject(sectorSec.foldKey)
                                }
                                RowTool {
                                    id: sectorEditTool
                                    visible: sectorHeadArea.containsMouse || hovered
                                    anchors { right: parent.right; rightMargin: 5; verticalCenter: parent.verticalCenter }
                                    icon: "gear"
                                    tip: "Editar setor"
                                    onHit: sectorsEditor.open(sectorSec.modelData)
                                }
                            }

                            Repeater {
                                model: sectorSec.folded ? [] : sectorSec.members
                                delegate: TeamAgentRow {
                                    required property var modelData
                                    agentData: modelData
                                    width: sectorSec.width
                                }
                            }

                            Rectangle {
                                visible: !sectorSec.folded && sectorSec.members.length === 0
                                width: sectorSec.width
                                height: 38
                                radius: Theme.radiusMd
                                color: "transparent"
                                border.width: 1
                                border.color: Theme.line
                                Text {
                                    anchors.centerIn: parent
                                    text: "Arraste funcionários para este setor"
                                    color: Theme.text4
                                    font.pixelSize: Theme.fontSmall
                                }
                            }
                            Item { width: 1; height: 6 }
                        }
                    }
                }

                // Projects and remaining (unpinned, unsectored) universal agents.
                Repeater {
                    model: chat.projects.concat([{ id: "", universal: true }])
                    delegate: Item {
                        id: sec
                        required property var modelData
                        readonly property var project: modelData.universal ? null : modelData
                        readonly property string pid: project ? project.id : ""
                        readonly property string foldKey: project ? pid : "unsectored:"
                        readonly property var members: chat.agents.filter(a => !a.pinned && !team.sectorIds.includes(a.sector || "") && (a.project_id || "") === pid)
                        readonly property bool showHead: !!project || chat.projects.length > 0 || (team.sectors.length > 0 && members.length > 0)
                        readonly property bool canFold: !!project || team.sectors.length > 0
                        readonly property bool folded: canFold && chat.collapsed[foldKey] === true
                        readonly property bool target: drop.containsDrag && team.hoverSector === null && chat.dragAgent !== null && (chat.dragAgent.project_id || "") !== pid
                        visible: !!project || members.length > 0 || chat.projects.length > 0
                        width: teamCol.width
                        height: visible ? secCol.implicitHeight : 0

                        DropArea {
                            id: drop
                            anchors.fill: parent
                            onEntered: if (!sec.project && team.sectors.length > 0) team.hoverSector = ""
                            onExited: if (team.hoverSector === "") team.hoverSector = null
                            onDropped: chat.dropTo = sec.pid
                            Rectangle {
                                anchors.fill: parent
                                anchors.margins: -2
                                radius: Theme.radiusLg
                                color: sec.target ? Theme.hover : "transparent"
                                border.width: sec.target ? 1 : 0
                                border.color: sec.project && sec.project.color ? sec.project.color : Theme.lineStrong
                            }
                        }

                        Column {
                            id: secCol
                            width: parent.width
                            spacing: 2

                            Item {
                                visible: sec.showHead
                                width: sec.width
                                height: 34
                                Rectangle {
                                    anchors.fill: parent
                                    radius: Theme.radiusSm
                                    color: sec.canFold && headArea.containsMouse ? Theme.hover : "transparent"
                                }
                                Icon {
                                    id: chev
                                    visible: sec.canFold
                                    x: 8
                                    anchors.verticalCenter: parent.verticalCenter
                                    name: "chevron"
                                    size: 12
                                    color: Theme.text4
                                    rotation: sec.folded ? 0 : 90
                                    Behavior on rotation { NumberAnimation { duration: Theme.fast } }
                                }
                                Rectangle {
                                    id: swatch
                                    visible: !!sec.project
                                    anchors { left: chev.right; leftMargin: Theme.space2; verticalCenter: parent.verticalCenter }
                                    width: 9
                                    height: 9
                                    radius: Theme.radiusXs
                                    color: sec.project && sec.project.color ? sec.project.color : Theme.text3
                                }
                                Text {
                                    id: secName
                                    anchors {
                                        left: sec.project ? swatch.right : sec.canFold ? chev.right : parent.left
                                        leftMargin: sec.project ? 9 : sec.canFold ? Theme.space2 : Theme.space3
                                        verticalCenter: parent.verticalCenter
                                    }
                                    text: sec.project ? sec.project.name : (team.sectors.length > 0 && chat.projects.length === 0 ? "SEM SETOR" : "UNIVERSAIS")
                                    color: sec.project ? Theme.text2 : Theme.text4
                                    font.pixelSize: sec.project ? Theme.fontBody : Theme.fontCaption
                                    font.weight: Font.DemiBold
                                    font.letterSpacing: sec.project ? 0 : Theme.trackCaps
                                }
                                Text {
                                    anchors { left: secName.right; leftMargin: Theme.space2; verticalCenter: parent.verticalCenter }
                                    visible: sec.folded || !sec.project
                                    text: sec.members.length
                                    color: Theme.text4
                                    font.pixelSize: Theme.fontCaption
                                }
                                MouseArea {
                                    id: headArea
                                    anchors.fill: parent
                                    hoverEnabled: true
                                    cursorShape: sec.canFold ? Qt.PointingHandCursor : Qt.ArrowCursor
                                    onClicked: if (sec.canFold) chat.toggleProject(sec.foldKey)
                                }
                                Row {
                                    visible: !!sec.project && (headArea.containsMouse || hire.hovered || edit.hovered || newChan.hovered)
                                    anchors { right: parent.right; rightMargin: 5; verticalCenter: parent.verticalCenter }
                                    spacing: 1
                                    RowTool {
                                        id: newChan
                                        icon: "hash"
                                        tip: "Novo canal neste projeto"
                                        onHit: chanEditor.open(null, sec.pid)
                                    }
                                    RowTool {
                                        id: hire
                                        icon: "plus"
                                        tip: "Contratar neste projeto"
                                        onHit: newAgentSheet.open(sec.pid)
                                    }
                                    RowTool {
                                        id: edit
                                        icon: "gear"
                                        tip: "Editar projeto"
                                        onHit: projectEditor.open(sec.project)
                                    }
                                }
                            }

                            Repeater {
                                model: sec.folded || !sec.project ? [] : chat.channelsOf(sec.pid)
                                delegate: ChanRow {
                                    required property var modelData
                                    c: modelData
                                    width: sec.width
                                }
                            }
                            Repeater {
                                model: sec.folded ? [] : sec.members
                                delegate: TeamAgentRow {
                                    required property var modelData
                                    agentData: modelData
                                    width: sec.width
                                }
                            }
                            Rectangle {
                                visible: !!sec.project && !sec.folded && sec.members.length === 0
                                width: sec.width
                                height: 42
                                radius: Theme.radiusMd
                                color: "transparent"
                                border.width: 1
                                border.color: Theme.line
                                Text {
                                    anchors.centerIn: parent
                                    text: "Arraste funcionários para cá"
                                    color: Theme.text4
                                    font.pixelSize: Theme.fontSmall
                                }
                            }
                            Item { width: 1; height: 6 }
                        }
                    }
                }
            }
        }

        // What follows the mouse while an agent is dragged.
        Rectangle {
            id: ghost
            visible: chat.dragAgent !== null
            width: ghostRow.implicitWidth + 24
            height: 38
            radius: Theme.radius
            color: Theme.cardTop
            border.width: 1
            border.color: Theme.lineStrong
            z: 100
            Drag.active: chat.dragAgent !== null
            Drag.hotSpot.x: 20
            Drag.hotSpot.y: height / 2
            Row {
                id: ghostRow
                anchors.centerIn: parent
                spacing: Theme.space2
                Avatar {
                    size: 28
                    anchors.verticalCenter: parent.verticalCenter
                    name: chat.dragAgent ? chat.dragAgent.name : ""
                    photo: chat.dragAgent ? U.photo(chat.dragAgent) : ""
                    tint: chat.dragAgent && chat.dragAgent.color ? chat.dragAgent.color : Theme.text2
                }
                Text {
                    anchors.verticalCenter: parent.verticalCenter
                    text: chat.dragAgent ? chat.dragAgent.name : ""
                    color: Theme.white
                    font.pixelSize: Theme.fontBody
                    font.weight: Font.DemiBold
                }
            }
        }
    }

    // Right click on an agent: pin at top or move between sectors and projects.
    Popup {
        id: movePopup
        property var agent: null
        width: 242
        padding: 6
        closePolicy: Popup.CloseOnEscape | Popup.CloseOnPressOutside
        enter: Transition { ParallelAnimation {
            NumberAnimation { property: "opacity"; from: 0; to: 1; duration: Theme.medium; easing.type: Theme.ease }
            NumberAnimation { property: "scale"; from: 0.97; to: 1; duration: Theme.medium; easing.type: Theme.ease }
        } }
        exit: Transition { NumberAnimation { property: "opacity"; from: 1; to: 0; duration: Theme.fast; easing.type: Easing.InQuad } }
        background: Rectangle {
            radius: Theme.radiusMd
            color: Theme.cardTop
            border.width: 1
            border.color: Theme.lineStrong
        }
        contentItem: Column {
            spacing: 2

            // Pin / unpin action.
            Rectangle {
                id: pinItem
                readonly property bool isPinned: !!movePopup.agent && !!movePopup.agent.pinned
                readonly property bool pinFull: !isPinned && team.pinnedAgents.length >= 5
                width: 230
                height: 32
                radius: Theme.radiusSm
                color: pinItemArea.containsMouse && !pinFull ? Theme.hover : "transparent"
                Icon {
                    id: pinItemIcon
                    x: 10
                    anchors.verticalCenter: parent.verticalCenter
                    name: "pin"
                    size: 14
                    color: pinItem.pinFull ? Theme.text4 : Theme.text2
                }
                Text {
                    anchors { left: pinItemIcon.right; leftMargin: 10; right: parent.right; rightMargin: 10; verticalCenter: parent.verticalCenter }
                    text: pinItem.isPinned ? "Desafixar do topo"
                          : pinItem.pinFull ? "Topo cheio (5/5)"
                          : "Fixar no topo"
                    color: pinItem.pinFull ? Theme.text4 : Theme.text
                    font.pixelSize: Theme.fontBody
                    elide: Text.ElideRight
                }
                MouseArea {
                    id: pinItemArea
                    anchors.fill: parent
                    hoverEnabled: true
                    cursorShape: pinItem.pinFull ? Qt.ArrowCursor : Qt.PointingHandCursor
                    onClicked: {
                        if (!pinItem.pinFull && movePopup.agent)
                            team.togglePin(movePopup.agent);
                        movePopup.close();
                    }
                }
            }

            // Sector choices.
            Rectangle {
                visible: team.sectors.length > 0
                width: 230
                height: 1
                color: Theme.line
            }
            Text {
                visible: team.sectors.length > 0
                leftPadding: 10
                topPadding: 4
                bottomPadding: 2
                text: "Setor"
                color: Theme.text4
                font.pixelSize: Theme.fontCaption
                font.weight: Font.DemiBold
            }
            Repeater {
                model: team.sectors.length > 0 ? [{ id: "", name: "Sem setor", color: "" }].concat(team.sectors) : []
                delegate: Rectangle {
                    id: mvSec
                    required property var modelData
                    readonly property bool here: !!movePopup.agent && (movePopup.agent.sector || "") === modelData.id
                    width: 230
                    height: 30
                    radius: Theme.radiusSm
                    color: mvSecArea.containsMouse && !here ? Theme.hover : "transparent"
                    Rectangle {
                        id: mvSecDot
                        x: 10
                        anchors.verticalCenter: parent.verticalCenter
                        width: 8
                        height: 8
                        radius: Theme.radiusXs
                        color: mvSec.modelData.color || "transparent"
                        border.width: mvSec.modelData.color ? 0 : 1
                        border.color: Theme.text4
                    }
                    Text {
                        anchors { left: mvSecDot.right; leftMargin: 10; right: parent.right; rightMargin: 10; verticalCenter: parent.verticalCenter }
                        text: mvSec.modelData.name
                        color: mvSec.here ? Theme.text4 : Theme.text
                        font.pixelSize: Theme.fontBody
                        elide: Text.ElideRight
                    }
                    MouseArea {
                        id: mvSecArea
                        anchors.fill: parent
                        hoverEnabled: true
                        cursorShape: mvSec.here ? Qt.ArrowCursor : Qt.PointingHandCursor
                        onClicked: {
                            if (!mvSec.here)
                                team.moveAgentToSector(movePopup.agent, mvSec.modelData.id);
                            movePopup.close();
                        }
                    }
                }
            }

            // Project choices.
            Rectangle {
                width: 230
                height: 1
                color: Theme.line
            }
            Text {
                leftPadding: 10
                topPadding: 4
                bottomPadding: 2
                text: movePopup.agent ? "Mover " + movePopup.agent.name + " para projeto" : ""
                color: Theme.text4
                font.pixelSize: Theme.fontCaption
                font.weight: Font.DemiBold
            }
            Repeater {
                model: [{ id: "", name: "Universal (sem projeto)", color: "" }].concat(chat.projects)
                delegate: Rectangle {
                    id: mv
                    required property var modelData
                    readonly property bool here: !!movePopup.agent && (movePopup.agent.project_id || "") === modelData.id
                    width: 230
                    height: 30
                    radius: Theme.radiusSm
                    color: mvArea.containsMouse && !here ? Theme.hover : "transparent"
                    Rectangle {
                        id: mvDot
                        x: 10
                        anchors.verticalCenter: parent.verticalCenter
                        width: 8
                        height: 8
                        radius: Theme.radiusXs
                        color: mv.modelData.color || "transparent"
                        border.width: mv.modelData.color ? 0 : 1
                        border.color: Theme.text4
                    }
                    Text {
                        anchors { left: mvDot.right; leftMargin: 10; right: parent.right; rightMargin: 10; verticalCenter: parent.verticalCenter }
                        text: mv.modelData.name
                        color: mv.here ? Theme.text4 : Theme.text
                        font.pixelSize: Theme.fontBody
                        elide: Text.ElideRight
                    }
                    MouseArea {
                        id: mvArea
                        anchors.fill: parent
                        hoverEnabled: true
                        cursorShape: mv.here ? Qt.ArrowCursor : Qt.PointingHandCursor
                        onClicked: {
                            if (!mv.here)
                                chat.moveAgent(movePopup.agent, mv.modelData.id);
                            movePopup.close();
                        }
                    }
                }
            }
        }
    }

    SectorsEditor {
        id: sectorsEditor
        anchors.fill: parent
        agents: chat.agents
        onChanged: {
            team.loadSectors();
            chat.loadAgents(false);
        }
    }

    // ── Profile (data and modal) ────────────────────────────────
    // ── Conversations (right) ───────────────────────────────────
    Item {
        id: convSide
        width: chat.convsOpen && !chat.channel && (chat.width - team.width - sidePanel.width >= 680) ? 284 : 0
        clip: true
        anchors { top: parent.top; bottom: parent.bottom; right: sidePanel.left }
        Behavior on width { NumberAnimation { duration: Theme.move; easing.type: Theme.ease } }

        Rectangle {
            anchors { top: parent.top; bottom: parent.bottom; left: parent.left }
            width: 1
            color: Theme.line
        }
        Item {
            id: sideHead
            x: 1
            width: 283
            anchors.top: parent.top
            anchors.topMargin: 52
            height: 32
            Text {
                x: 20
                anchors.verticalCenter: parent.verticalCenter
                text: chat.showArchived ? "ARQUIVADAS" : "CONVERSAS"
                color: Theme.text4
                font.pixelSize: Theme.fontCaption
                font.weight: Font.DemiBold
                font.letterSpacing: Theme.trackCaps
            }
            Row {
                anchors { right: parent.right; rightMargin: 14; verticalCenter: parent.verticalCenter }
                spacing: 2
                RowTool {
                    icon: "archive"
                    tip: chat.showArchived ? "Voltar às conversas" : "Ver arquivadas"
                    onHit: chat.toggleArchived()
                }
                RowTool {
                    icon: "pen"
                    tip: "Nova conversa"
                    onHit: {
                        if (chat.showArchived)
                            chat.toggleArchived();
                        chat.open("");
                        composer.area.forceActiveFocus();
                    }
                }
            }
        }
        ListView {
            id: convList
            x: 10
            width: 264
            anchors { top: sideHead.bottom; topMargin: 6; bottom: parent.bottom; bottomMargin: 12 }
            clip: true
            spacing: 2
            model: convs
            boundsBehavior: Flickable.StopAtBounds
            ScrollBar.vertical: SlimScrollBar {}
            SmoothWheel {}
            delegate: Rectangle {
                id: thr
                required property var model
                readonly property bool here: model.id === chat.convId
                readonly property bool hot: thrArea.containsMouse || archBtn.hovered || delBtn.hovered || chat.confirmDelete === model.id
                readonly property bool beat: (model.title || "").startsWith("💓")
                width: convList.width
                height: 50
                radius: Theme.radiusMd
                color: here ? Theme.active : hot ? Theme.hover : "transparent"
                Behavior on color { ColorAnimation { duration: Theme.fast } }
                Rectangle {
                    x: 0
                    anchors.verticalCenter: parent.verticalCenter
                    width: 2
                    height: 18
                    radius: 1
                    visible: thr.here
                    color: pane.tint
                }
                Column {
                    anchors { left: parent.left; leftMargin: 14; right: thr.hot ? thrTools.left : parent.right; rightMargin: 12; verticalCenter: parent.verticalCenter }
                    spacing: 3
                    Text {
                        width: parent.width
                        text: chat.confirmDelete === thr.model.id ? "Clique de novo para apagar" : thr.model.title
                        color: chat.confirmDelete === thr.model.id ? Theme.bad : thr.here ? Theme.white : thr.beat ? Theme.text3 : Theme.text2
                        font.pixelSize: Theme.fontBody
                        elide: Text.ElideRight
                    }
                    Row {
                        spacing: 6
                        Rectangle {
                            visible: !!chat.live[thr.model.id]
                            width: 6
                            height: 6
                            radius: 3
                            anchors.verticalCenter: parent.verticalCenter
                            color: pane.tint
                        }
                        Text {
                            text: chat.live[thr.model.id] ? "trabalhando agora" : U.ago(thr.model.updated_at)
                            color: chat.live[thr.model.id] ? Qt.lighter(pane.tint, 1.1) : Theme.text4
                            font.pixelSize: Theme.fontCaption
                        }
                    }
                }
                MouseArea {
                    id: thrArea
                    anchors.fill: parent
                    hoverEnabled: true
                    cursorShape: Qt.PointingHandCursor
                    onClicked: {
                        chat.confirmDelete = "";
                        chat.open(thr.model.id);
                    }
                }
                Row {
                    id: thrTools
                    visible: thr.hot
                    anchors { right: parent.right; rightMargin: 6; verticalCenter: parent.verticalCenter }
                    spacing: 1
                    RowTool {
                        id: archBtn
                        icon: "archive"
                        tip: chat.showArchived ? "Desarquivar" : "Arquivar"
                        onHit: chat.archiveConv(thr.model.id, !chat.showArchived)
                    }
                    RowTool {
                        id: delBtn
                        icon: "trash"
                        tip: "Apagar"
                        danger: chat.confirmDelete === thr.model.id
                        onHit: chat.deleteConv(thr.model.id)
                    }
                }
            }
        }
        Text {
            visible: convs.count === 0
            anchors { top: sideHead.bottom; topMargin: 14; left: parent.left; leftMargin: 24 }
            text: chat.showArchived ? "Nada arquivado." : "Nenhuma conversa ainda."
            color: Theme.text4
            font.pixelSize: Theme.fontSmall
        }
    }

    // ── Conversation ────────────────────────────────────────────
    Item {
        id: pane
        visible: !chat.channel
        anchors { top: parent.top; bottom: parent.bottom; left: team.right; right: convSide.left }
        // The conversation runs the whole width, with a slim margin each side.
        readonly property real column: width - 2 * 96
        readonly property color tint: chat.agent && chat.agent.color ? chat.agent.color : Theme.text2

        // Agent header
        Item {
            id: head
            anchors { top: parent.top; topMargin: 44; left: parent.left; right: parent.right; leftMargin: 28; rightMargin: 20 }
            height: 48
            visible: chat.agent !== null
            Avatar {
                id: headFace
                anchors.verticalCenter: parent.verticalCenter
                size: 40
                name: chat.agent ? chat.agent.name : ""
                photo: U.photo(chat.agent)
                tint: pane.tint
                working: chat.working || (chat.nativeBusy && chat.streamConv === chat.convId)
                // The face opens the profile.
                MouseArea {
                    anchors.fill: parent
                    cursorShape: Qt.PointingHandCursor
                    onClicked: agentPage.open(chat.agentId, "")
                }
            }
            Column {
                anchors { left: headFace.right; leftMargin: 12; verticalCenter: parent.verticalCenter }
                spacing: 2
                Row {
                    spacing: 8
                    Text {
                        text: chat.agent ? chat.agent.name : ""
                        color: Theme.white
                        font.pixelSize: Theme.fontTitle
                        font.weight: Font.DemiBold
                    }
                    Text {
                        topPadding: 3
                        text: chat.agent ? chat.agent.role : ""
                        color: Theme.text3
                        font.pixelSize: Theme.fontBody
                    }
                }
                Text {
                    visible: chat.busy
                    text: "trabalhando nesta conversa…"
                    color: chat.busy ? Qt.lighter(pane.tint, 1.1) : Theme.text4
                    font.pixelSize: Theme.fontSmall
                }
                Row {
                    visible: !chat.busy
                    spacing: 6
                    Rectangle {
                        anchors.verticalCenter: parent.verticalCenter
                        width: 7
                        height: 7
                        radius: 2.5
                        color: chat.project ? (chat.project.color || Theme.text3) : "transparent"
                        border.width: chat.project ? 0 : 1
                        border.color: Theme.text4
                    }
                    Text {
                        text: chat.project ? chat.project.name : "Universal"
                        color: Theme.text4
                        font.pixelSize: Theme.fontSmall
                    }
                }
            }
            Row {
                anchors { right: parent.right; verticalCenter: parent.verticalCenter }
                spacing: 6
                // How full this conversation's context is.
                ContextMeter {
                    anchors.verticalCenter: parent.verticalCenter
                    convId: chat.convId
                    refreshKey: msgs.count
                }
                GhostButton {
                    visible: chat.agent !== null
                    small: true
                    icon: "spark"
                    text: chat.agent && chat.agent.brain && chat.agent.brain.provider
                        ? (chat.agent.brain.model || chat.agent.brain.provider)
                        : "Automático"
                    onClicked: modelPicker.open(chat.agent)
                }
                GhostButton {
                    visible: chat.convId !== ""
                    small: true
                    icon: "pen"
                    text: "Nova"
                    onClicked: {
                        if (chat.showArchived)
                            chat.toggleArchived();
                        chat.open("");
                        composer.area.forceActiveFocus();
                    }
                }
                GhostButton {
                    small: true
                    round: true
                    icon: "history"
                    onClicked: chat.convsOpen = !chat.convsOpen
                }
                GhostButton {
                    small: true
                    round: true
                    icon: "user"
                    onClicked: agentPage.open(chat.agentId, "")
                }
                GhostButton {
                    small: true
                    round: true
                    icon: "sidebar"
                    color: sidePanel.shown ? Theme.active : (hovered ? Theme.hover : "transparent")
                    onClicked: sidePanel.toggle()
                }
            }
            Rectangle {
                anchors { top: parent.bottom; topMargin: 14; left: parent.left; right: parent.right; leftMargin: -28; rightMargin: -20 }
                height: 1
                color: Theme.line
            }
        }

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
                columns: 2
                spacing: 12
                anchors.horizontalCenter: parent.horizontalCenter
                readonly property real cell: (Math.min(pane.column, 640) - spacing) / 2
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

        // A conversation on its way.
        ThreadSkeleton {
            anchors { top: head.bottom; topMargin: 15; bottom: composerBox.top; left: parent.left; right: parent.right; bottomMargin: 8 }
            active: chat.loadingConv && chat.empty
            column: pane.column
            tint: pane.tint
        }

        // Messages
        ListView {
            id: thread
            visible: !chat.empty
            // A loaded conversation settles in: a fade and a short rise.
            transform: Translate { id: threadRise }
            ParallelAnimation {
                id: threadIntro
                NumberAnimation { target: thread; property: "opacity"; from: 0; to: 1; duration: Theme.medium; easing.type: Easing.OutCubic }
                NumberAnimation { target: threadRise; property: "y"; from: 10; to: 0; duration: Theme.move; easing.type: Easing.OutCubic }
            }
            anchors { top: head.bottom; topMargin: 15; bottom: composerBox.top; left: parent.left; right: parent.right; bottomMargin: 8 }
            topMargin: 26
            bottomMargin: 16
            clip: true
            spacing: 0
            model: msgs
            boundsBehavior: Flickable.StopAtBounds
            ScrollBar.vertical: SlimScrollBar { id: threadBar }
            SmoothWheel { id: threadWheel }
            // Short threads are built up front, so the height is exact and dragging
            // the scrollbar does not jump. Long ones keep ~7 screens around the
            // view, built ahead in the background so scrolling meets ready rows.
            cacheBuffer: count <= 150 ? 100000 : 8000
            property bool pinning: false
            onContentHeightChanged: if (chat.stick) {
                pinning = true;
                positionViewAtEnd();
                pinning = false;
            }
            // Following the newest message until the owner scrolls away from it.
            // Only your own scrolling lets go of the end (or grabs it again);
            // rows being measured off screen move contentY too.
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
                // A colleague's answer in this conversation (a note it got).
                readonly property var who: author !== "" && author !== chat.agentId ? chat.agents.find(a => a.id === author) || null : null
                readonly property color tint: who && who.color ? who.color : pane.tint
                readonly property bool mine: role === "user"
                readonly property string markup: mine && !report ? chat.markHtml(content) : ""
                readonly property bool rich: markup.indexOf("image://chip/") >= 0
                // A mission round's report or the heartbeat's check (written
                // by the server, not the owner): shown folded, first line only.
                readonly property bool report: mine && (content.startsWith("🎯 Missão") || content.startsWith("💓 Heartbeat") || content.startsWith("📨 Recado de"))
                readonly property var quoted: reply_to > 0 ? chat.messageById(reply_to) : null
                readonly property var stepList: JSON.parse(steps || "[]")
                // What the cards do not show: readings, Claude Code's inner steps.
                readonly property var innerSteps: stepList.filter(s => U.action(s) === null && s.tool !== "colega_respondeu" && s.tool !== "perguntar")
                // Questions put to the owner with answers to click.
                readonly property var asks: stepList.filter(s => s.tool === "perguntar" && s.detail)
                // The owner's message right after this one: the answer to its question.
                readonly property string nextMine: {
                    const n = msgs.count;
                    if (asks.length === 0 || index + 1 >= n)
                        return "";
                    const m = msgs.get(index + 1);
                    return m && m.role === "user" ? m.content : "";
                }
                // What colleagues answered when consulted: shown as their messages.
                readonly property var replies: stepList.filter(s => s.tool === "colega_respondeu" && s.detail)
                // Codex is drawing an image: its frame shows until the answer.
                readonly property bool drawing: streaming && stepList.some(s => (s.tool || "").endsWith("gerar_imagem"))
                // Live browser card while the agent is browsing in this turn.
                readonly property string browserSite: streaming ? chat.browserSiteOf(stepList) : ""
                property bool expanded: false

                width: thread.width
                // The actions sit in the space under the message.
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

                // What this message answers, above the bubble.
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
                        maxWidth: Math.min(answer.width - 54, 780) // full-width thread, readable lines
                        tint: msg.tint
                        name: msg.who ? msg.who.name : chat.agent ? chat.agent.name : ""
                        sub: (msg.who ? "colega, a pedido de " + (chat.agent ? chat.agent.name : "") + "  ·  " : "") + (msg.at ? U.clock(msg.at).slice(0, 5) : "")
                        // Only text: a bubble its size. Cards, images, questions: the full width.
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

                        // Live steps while working: the last few, newest bright.
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

                        // Live browser card: click to watch in the side panel's Navegador tab.
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
                                    text: "Ver ›"
                                    color: browserLiveArea.containsMouse ? Theme.white : Theme.text3
                                    font.pixelSize: Theme.fontSmall
                                }
                            }
                            MouseArea {
                                id: browserLiveArea
                                anchors.fill: parent
                                hoverEnabled: true
                                cursorShape: Qt.PointingHandCursor
                                onClicked: sidePanel.openTab("browser", msg.who ? msg.who.id : chat.agentId)
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
                                        fontSize: 14
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

                        // The docs this answer wrote or cites, like artifacts.
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
                                // Only the last question of the newest answer takes a click.
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

                        // Failure, said plainly.
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

        // A Claude Code / Codex run, opened from its thread or step.
        RunPane {
            anchors.fill: parent
            z: 20
            visible: chat.runId !== ""
            runId: chat.runId
            agents: chat.agents
            onClosed: chat.runId = ""
        }

        // Back to the newest message, when you have scrolled away from it.
        GhostButton {
            id: jumpEnd
            round: true
            icon: "send"
            z: 3
            readonly property bool wanted: thread.visible && !chat.stick
            anchors { horizontalCenter: thread.horizontalCenter; bottom: thread.bottom; bottomMargin: 12 }
            color: hovered ? Theme.cardHover : Theme.card
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
                onDecided: {
                    chat.loadPending();
                }
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
            // Who works here, on what, for how long: one line that changes
            // in place, never a new row in the conversation.
            Row {
                id: workLine
                visible: chat.workingHere
                spacing: 8
                height: 20
                leftPadding: 4
                opacity: visible ? 1 : 0
                Behavior on opacity { NumberAnimation { duration: 180; easing.type: Easing.OutCubic } }
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
                            chat.completeMention(chat.mentionPick); // Enter or Tab
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
                    onHit: DocBus.showList()
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

    ChannelView {
        id: room
        visible: chat.channel
        active: chat.channel && chat.visible
        channelId: chat.channelId
        onSettings: chanEditor.open(chat.channelNow, "")
        agents: chat.agents
        approvals: chat.pendingTeam
        onApprovalDecided: chat.loadPending()
        anchors { top: parent.top; bottom: parent.bottom; left: team.right; right: sidePanel.left }
        onMoved: if (chat.channel) chat.seeChannel()
        onOpenRun: (id, agentId) => chat.openRun(id, agentId)
    }
    GhostButton {
        visible: chat.channel && !sidePanel.shown
        z: 10
        small: true
        round: true
        icon: "sidebar"
        anchors { top: room.top; topMargin: 114; right: room.right; rightMargin: 20 }
        onClicked: sidePanel.toggle()
    }
    SidePanel {
        id: sidePanel
        anchors { top: parent.top; bottom: parent.bottom; right: parent.right }
        maxAllowedWidth: Math.min(720, Math.max(320, chat.width - team.width - 360))
        convKey: chat.activeConvKey
        convId: chat.activeConvId
        agents: chat.agents
        convAgents: chat.sideAgents
        agentStatusMap: chat.sideAgentStatus
        citedDocIds: chat.citedDocsHere
        openRunId: chat.runId
        runsRev: chat.runsRev
        onOpenAgentRequested: id => agentPage.open(id, "")
        onOpenRunRequested: (id, agentId) => chat.openRun(id, agentId)
        onFocusChatRequested: id => {
            if (agentPage.shown)
                agentPage.close();
            chat.goPage("chat");
        }
    }
    // A run opened from a channel message (a research, a Claude Code task).
    RunPane {
        anchors.fill: room
        z: 20
        topInset: Theme.titleBar
        visible: chat.channel && chat.runId !== ""
        runId: chat.channel ? chat.runId : ""
        agents: chat.agents
        onClosed: chat.runId = ""
    }

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
            chat.selectChannel(c.id);
        }
        onArchived: id => {
            chat.loadChannels();
            if (chat.channelId === id)
                chat.selectChannel("time");
        }
    }

    // ── Agent page ──────────────────────────────────────────────
    // Opens from the agent's face or name; covers the conversation and
    // keeps the team list beside it, so another agent opens in place.
    AgentPage {
        id: agentPage
        anchors { top: parent.top; bottom: parent.bottom; left: team.right; right: parent.right }
        z: 45
        agents: chat.agents
        projects: chat.projects
        onChanged: chat.loadAgents(false)
        onArchived: id => {
            chat.loadAgents(false);
            chat.loadArchived();
            if (chat.agentId === id) {
                const next = chat.agents.find(a => a.id !== id);
                chat.selectAgent(next ? next.id : "");
            }
        }
        onRunRequested: (id, agentId) => {
            agentPage.close();
            chat.openRun(id, agentId);
        }
        onDocRequested: id => {
            agentPage.close();
            DocBus.open(id);
        }
        onRoutineRequested: (routine, preset) => routineEditor.open(agentPage.agent, routine, preset)
        onPageRequested: name => chat.goPage(name)
    }
    Shortcut {
        sequence: "Esc"
        enabled: agentPage.shown && !agentPage.leaving && !routineEditor.shown && !editor.shown
        onActivated: agentPage.close()
    }

    ArchivedAgents {
        id: archivedSheet
        anchors.fill: parent
        agents: chat.archivedAgents
        onRestored: id => {
            chat.loadArchived();
            chat.loadAgents(false);
        }
    }
    RoutineEditor {
        id: routineEditor
        anchors.fill: parent
    }

    ProjectEditor {
        id: projectEditor
        anchors.fill: parent
        onSaved: p => chat.loadProjects()
        onDeleted: id => {
            chat.loadProjects();
            chat.loadAgents(false);
        }
    }

    NewAgentSheet {
        id: newAgentSheet
        anchors.fill: parent
        onCreated: (a, convId) => chat.openCreatedAgent(a, convId)
    }

    AgentEditor {
        id: editor
        anchors.fill: parent
        onSaved: a => {
            if (editor.agent === null) {
                chat.agentId = a.id;
                chat.loadAgents(true);
            } else {
                chat.loadAgents(false);
            }
        }
    }

    ModelPicker {
        id: modelPicker
        anchors.fill: parent
        onSaved: chat.loadAgents(false)
    }

    // Drop files anywhere on the page to attach them; drop a doc (from the
    // Docs panel, a card, the Docs page) to cite it.
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
            if (drop.hasUrls && chat.agent)
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
