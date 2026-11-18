import QtQuick
import QtQuick.Controls.Basic
import QtQuick.Effects
import Avora
import "util.js" as U

// A channel (D54): the owner and some agents in one group conversation. @Nome
// calls someone; agents call each other the same way, up to the loop lock.
Item {
    id: ch
    property var agents: []
    property string channelId: "time"
    readonly property var info: status && status.channel && status.channel.id === channelId ? status.channel : null
    readonly property string chName: info ? info.name : (channelId === "time" ? "time" : "")
    readonly property string purpose: info ? info.purpose || "" : ""
    // The agents in the channel (every agent when it has everyone).
    readonly property var members: !info || info.everyone ? agents
        : agents.filter(a => (info.members || []).some(m => m.agent_id === a.id))
    signal settings // the gear: edit the channel
    signal openRun(string id, string agentId) // a run's card: see what it did
    // "@Name" in an answer becomes a chip in that agent's color.
    readonly property var teamChips: agents.map(a => ({ name: a.name, color: a.color || "" }))
    property bool active: false
    property var status: null // GET /v1/channels/{id}/status
    property string error: ""
    property bool stick: true
    property string loadedAt: ""
    // The channel whose messages are on screen; until it is this one, a
    // skeleton stands in and the empty-channel guide waits.
    property string loadedFor: ""
    readonly property bool loading: loadedFor !== channelId
    property var stepOf: ({}) // job id -> { after, tool, summary, count }

    readonly property var working: status ? status.working || {} : {}
    readonly property var workingIds: Object.keys(working)
    readonly property bool busy: workingIds.length > 0
    readonly property int streak: status ? status.streak : 0
    readonly property int limit: status ? status.limit : 8
    readonly property bool locked: status ? !!status.locked : false
    // A message without @ waits a moment while the router picks who answers.
    readonly property bool routing: status ? !!status.routing : false
    // Agents lined up to answer after the one answering now (they join in turn).
    readonly property var nextIds: status ? status.next || [] : []

    property var approvals: [] // locked actions in #time waiting for the owner
    signal moved // the channel's last message moved (for the team list)
    signal approvalDecided

    ListModel { id: msgs }

    function agentOf(id) {
        return agents.find(a => a.id === id) || null;
    }

    function names(ids) {
        const n = ids.map(id => (agentOf(id) || { name: id }).name);
        return n.length <= 1 ? n.join("") : n.slice(0, -1).join(", ") + " e " + n[n.length - 1];
    }

    // Mentions stand out: @Atlas -> **@Atlas**.
    function marked(text) {
        let out = text;
        for (const a of agents)
            out = out.replace(new RegExp("(^|[^\\w*])@(" + a.name + ")(?![\\w])", "gi"), "$1**@$2**");
        return out;
    }

    function norm(m, prev) {
        const head = !prev || prev.role !== m.role || (m.agent_id || "") !== (prev.agent_id || "") || prev.role === "system";
        return {
            id: m.id,
            role: m.role,
            author: m.agent_id || "",
            content: m.role === "assistant" ? marked(m.content || "") : (m.content || ""),
            raw: m.content || "",
            reply_to: m.reply_to || 0,
            prompt_tokens: m.prompt_tokens || 0,
            completion_tokens: m.completion_tokens || 0,
            err: m.error || "",
            steps: JSON.stringify(m.steps || []),
            mdl: m.model || "",
            duration_ms: m.duration_ms || 0,
            at: m.created_at || "",
            reactions: JSON.stringify(m.reactions || []),
            head: head
        };
    }

    function load() {
        const asked = channelId;
        Api.get("/v1/conversations/" + channelId + "/messages", (err, data) => {
            if (asked !== channelId)
                return;
            const first = loadedFor !== asked;
            loadedFor = asked;
            if (err) {
                error = err;
                return;
            }
            error = "";
            const items = [];
            for (let i = 0; i < data.length; i++)
                items.push(norm(data[i], i ? data[i - 1] : null));
            U.syncModel(msgs, items);
            if (stick)
                thread.positionViewAtEnd();
            if (first)
                threadIntro.restart();
        });
    }

    function poll() {
        const asked = channelId;
        Api.get("/v1/channels/" + channelId + "/status", (err, st) => {
            if (err || asked !== channelId)
                return;
            const moved = !status || st.conversation.updated_at !== status.conversation.updated_at;
            status = st;
            if (moved)
                ch.moved();
            if (active && (moved || loadedAt === ""))
                load();
            loadedAt = st.conversation.updated_at;
            for (const id in st.working) {
                const job = st.working[id].job_id;
                if (job && active)
                    followSteps(job);
            }
        });
    }

    function followSteps(job) {
        const cur = stepOf[job] || { after: 0, tool: "", summary: "", count: 0 };
        Api.get("/v1/events?job=" + job + "&after=" + cur.after, (err, evs) => {
            if (err)
                return;
            const s = Object.assign({}, cur);
            for (const e of evs) {
                s.after = Math.max(s.after, e.id);
                if (e.type === "agent.step") {
                    s.tool = e.payload.tool;
                    s.summary = e.payload.summary || "";
                    s.count++;
                }
            }
            const m = Object.assign({}, stepOf);
            m[job] = s;
            stepOf = m;
        });
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
        if (m.role === "user")
            return "você";
        const a = agentOf(m.author);
        return a ? a.name : "Ex-funcionário";
    }

    function replyTo(m) {
        replyTarget = { id: m.id, who: authorOf(m), content: m.raw };
        composer.area.forceActiveFocus();
    }

    function showDetails(m) {
        let steps = [];
        try {
            steps = JSON.parse(m.steps || "[]");
        } catch (e) {}
        details.open({ who: m.role === "user" ? "Você" : authorOf(m), at: m.at, mdl: m.mdl, prompt_tokens: m.prompt_tokens,
                       completion_tokens: m.completion_tokens, duration_ms: m.duration_ms, steps: steps.length, id: m.id, err: m.err });
    }

    function send(text) {
        const content = (text || composer.text).trim();
        if (content === "")
            return;
        if (!text)
            composer.text = "";
        error = "";
        stick = true;
        const reply = replyTarget ? replyTarget.id : 0;
        replyTarget = null;
        msgs.append(norm({ id: -1, role: "user", content: content, reply_to: reply }, msgs.count ? msgs.get(msgs.count - 1) : null));
        thread.positionViewAtEnd();
        Api.post("/v1/conversations/" + channelId + "/messages", { content: content, reply_to: reply }, err => {
            if (err)
                error = err;
            poll();
        });
    }

    function stop() {
        Api.post("/v1/conversations/" + channelId + "/stop", null, () => poll());
    }

    onActiveChanged: if (active) {
        stick = true;
        load();
        poll();
        composer.area.forceActiveFocus();
    }
    Component.onCompleted: poll()
    // Another channel: start clean.
    onChannelIdChanged: {
        status = null;
        msgs.clear();
        stepOf = ({});
        loadedAt = "";
        replyTarget = null;
        error = "";
        stick = true;
        poll();
        if (active)
            load();
    }

    Timer {
        interval: Api.live ? 20000 : ch.active ? (ch.busy ? 1500 : 3000) : 8000
        repeat: true
        running: true
        onTriggered: ch.poll()
    }

    // Live (D47): #time moves the moment a message or a step happens.
    Timer {
        id: livePoll
        interval: 60
        onTriggered: ch.poll()
    }
    function workingJob(job) {
        if (!status || !status.working)
            return false;
        for (const id in status.working)
            if (status.working[id].job_id === job)
                return true;
        return false;
    }
    Connections {
        target: Api
        function onLiveEvent(e) {
            const p = e.payload || {};
            if (((e.type === "message.created" || e.type === "message.reacted") && p.conversation_id === ch.channelId) || e.type.startsWith("channel.") && (!p.channel_id || p.channel_id === ch.channelId)) {
                livePoll.restart();
            } else if (e.type === "agent.step" && ch.workingJob(e.job_id)) {
                const cur = ch.stepOf[e.job_id] || { after: 0, tool: "", summary: "", count: 0 };
                const m = Object.assign({}, ch.stepOf);
                m[e.job_id] = { after: Math.max(cur.after, e.id), tool: p.tool, summary: p.summary || "", count: cur.count + 1 };
                ch.stepOf = m;
            }
        }
        function onLiveReset() {
            livePoll.restart();
        }
    }

    // ── @ suggestions ───────────────────────────────────────────
    property var suggestions: []
    property int pick: 0
    property int mentionAt: -1

    function fold(t) {
        return t.toLowerCase().normalize("NFD").replace(/[̀-ͯ]/g, "");
    }

    function updateSuggestions() {
        const area = composer.area;
        const before = area.text.slice(0, area.cursorPosition);
        const m = before.match(/(^|\s)@([^\s@]*)$/);
        if (!m) {
            suggestions = [];
            return;
        }
        const q = fold(m[2]);
        mentionAt = area.cursorPosition - m[2].length - 1;
        const list = agents.filter(a => fold(a.name).startsWith(q) || fold(a.role || "").startsWith(q))
                           .map(a => ({ id: a.id, name: a.name, role: a.role, color: a.color }));
        if ("todos".startsWith(q))
            list.push({ id: "", name: "todos", role: "chama o canal inteiro", color: "" });
        suggestions = list.slice(0, 6);
        pick = 0;
    }

    function complete(i) {
        const s = suggestions[i];
        if (!s)
            return;
        const area = composer.area;
        const end = area.cursorPosition;
        const text = area.text;
        area.text = text.slice(0, mentionAt) + "@" + s.name + " " + text.slice(end);
        area.cursorPosition = mentionAt + s.name.length + 2;
        suggestions = [];
    }

    // ── Header ──────────────────────────────────────────────────
    Item {
        id: head
        anchors { top: parent.top; topMargin: 44; left: parent.left; right: parent.right; leftMargin: 28; rightMargin: 20 }
        height: 48

        Rectangle {
            id: hashDisc
            width: 40
            height: 40
            radius: 20
            anchors.verticalCenter: parent.verticalCenter
            color: Theme.active
            border.width: 1
            border.color: Theme.lineStrong
            Icon {
                anchors.centerIn: parent
                name: "hash"
                size: 19
                color: Theme.white
            }
        }
        Column {
            anchors { left: hashDisc.right; leftMargin: 12; verticalCenter: parent.verticalCenter }
            spacing: 2
            Text {
                text: ch.chName
                color: Theme.white
                font.pixelSize: Theme.fontTitle
                font.weight: Font.DemiBold
            }
            Text {
                text: ch.busy ? ch.names(ch.workingIds) + (ch.workingIds.length > 1 ? " estão trabalhando…" : " está trabalhando…")
                      : ch.routing ? "Vendo quem responde…"
                      : ch.purpose !== "" ? ch.purpose
                      : ch.info && ch.info.everyone ? "Você e o time inteiro  ·  @Nome chama alguém"
                      : "Você e " + ch.members.length + (ch.members.length === 1 ? " agente" : " agentes") + "  ·  @Nome chama alguém"
                color: ch.busy || ch.routing ? Theme.text2 : Theme.text4
                font.pixelSize: Theme.fontSmall
            }
        }
        Row {
            anchors { right: parent.right; verticalCenter: parent.verticalCenter }
            spacing: 12

            // Who is in the channel.
            Row {
                anchors.verticalCenter: parent.verticalCenter
                spacing: -8
                Repeater {
                    model: ch.members
                    delegate: Avatar {
                        required property var modelData
                        size: 28
                        name: modelData.name
                        photo: U.photo(modelData)
                        tint: modelData.color || Theme.text2
                        working: ch.working[modelData.id] !== undefined
                    }
                }
            }
            // How close the loop lock is, once the team talks among itself.
            Rectangle {
                visible: ch.streak > 0 || ch.locked
                anchors.verticalCenter: parent.verticalCenter
                height: 28
                width: lockRow.implicitWidth + 22
                radius: Theme.radiusLg
                color: ch.locked ? Qt.rgba(1, 0.75, 0.4, 0.1) : Theme.wash1
                border.width: 1
                border.color: ch.locked ? Qt.rgba(1, 0.75, 0.4, 0.35) : Theme.line
                Row {
                    id: lockRow
                    anchors.centerIn: parent
                    spacing: 6
                    Icon {
                        name: "lock"
                        size: 13
                        color: ch.locked ? Theme.warn : Theme.text3
                        anchors.verticalCenter: parent.verticalCenter
                    }
                    Text {
                        text: ch.locked ? "travado" : ch.streak + "/" + ch.limit
                        color: ch.locked ? Theme.warn : Theme.text3
                        font.family: Theme.mono
                        font.pixelSize: Theme.fontSmall
                        anchors.verticalCenter: parent.verticalCenter
                    }
                }
            }
            // The team stopped at its limit: one click lets it go on.
            GhostButton {
                visible: ch.locked && !ch.busy
                small: true
                icon: "send"
                text: "Continuar"
                onClicked: ch.send("continuem")
            }
            GhostButton {
                visible: ch.busy
                small: true
                danger: true
                icon: "stop"
                text: "Parar todos"
                onClicked: ch.stop()
            }
            GhostButton {
                small: true
                icon: "gear"
                text: "Canal"
                onClicked: ch.settings()
            }
        }
        Rectangle {
            anchors { top: parent.bottom; topMargin: 14; left: parent.left; right: parent.right; leftMargin: -28; rightMargin: -20 }
            height: 1
            color: Theme.line
        }
    }

    // The conversation runs the whole width, with a slim margin each side.
    readonly property real column: width - 2 * 96

    // Empty channel: how it works.
    Column {
        visible: msgs.count === 0 && !ch.loading
        opacity: visible ? 1 : 0
        Behavior on opacity { NumberAnimation { duration: Theme.slow } }
        anchors { horizontalCenter: parent.horizontalCenter; verticalCenter: parent.verticalCenter; verticalCenterOffset: -30 }
        width: ch.column
        spacing: 0
        Row {
            anchors.horizontalCenter: parent.horizontalCenter
            spacing: -14
            Repeater {
                model: ch.members
                delegate: Avatar {
                    required property var modelData
                    size: 64
                    name: modelData.name
                    photo: U.photo(modelData)
                    tint: modelData.color || Theme.text2
                }
            }
        }
        Item { width: 1; height: 22 }
        Text {
            text: "#" + ch.chName
            color: Theme.white
            font.family: Theme.display
            font.pixelSize: Theme.fontHero
            font.weight: Font.DemiBold
            font.letterSpacing: -1
            anchors.horizontalCenter: parent.horizontalCenter
        }
        Item { width: 1; height: 8 }
        Text {
            width: Math.min(ch.column, 560)
            anchors.horizontalCenter: parent.horizontalCenter
            horizontalAlignment: Text.AlignHCenter
            text: (ch.purpose !== "" ? ch.purpose + ".\n\n" : "") +
                  "Sem @, quem tem a ver com a mensagem responde. Com @Nome, você chama quem quiser, e eles se chamam entre si até " + ch.limit + " respostas seguidas."
            color: Theme.text3
            font.pixelSize: Theme.fontLarge
            lineHeight: 1.2
            wrapMode: Text.Wrap
        }
    }

    // ── Messages ────────────────────────────────────────────────
    // The channel on its way.
    ThreadSkeleton {
        anchors { top: head.bottom; topMargin: 15; bottom: composerBox.top; left: parent.left; right: parent.right; bottomMargin: 8 }
        active: ch.loading && msgs.count === 0
        column: ch.column
    }

    ListView {
        id: thread
        visible: msgs.count > 0
        // A loaded channel settles in: a fade and a short rise.
        transform: Translate { id: threadRise }
        ParallelAnimation {
            id: threadIntro
            NumberAnimation { target: thread; property: "opacity"; from: 0; to: 1; duration: Theme.medium; easing.type: Easing.OutCubic }
            NumberAnimation { target: threadRise; property: "y"; from: 10; to: 0; duration: Theme.move; easing.type: Easing.OutCubic }
        }
        anchors { top: head.bottom; topMargin: 15; bottom: composerBox.top; left: parent.left; right: parent.right; bottomMargin: 8 }
        topMargin: 22
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
        onContentHeightChanged: if (ch.stick) {
            pinning = true;
            positionViewAtEnd();
            pinning = false;
        }
        // Following the newest message until the owner scrolls away from it.
        // Only your own scrolling lets go of the end (or grabs it again);
        // rows being measured off screen move contentY too.
        onContentYChanged: if (!pinning && (threadWheel.driving || threadBar.pressed)) ch.stick = atYEnd

        delegate: Item {
            id: msg
            required property string role
            required property string author
            required property string content
            required property string err
            required property string steps
            required property string mdl
            required property int duration_ms
            required property string at
            required property bool head
            required property int id
            required property string raw
            required property int reply_to
            required property int prompt_tokens
            required property int completion_tokens
            required property string reactions
            // Emoji the agents put on it, grouped: [{emoji, ids}].
            readonly property var reactGroups: {
                const out = [];
                for (const r of JSON.parse(reactions || "[]")) {
                    const g = out.find(x => x.emoji === r.emoji);
                    if (g)
                        g.ids.push(r.agent_id);
                    else
                        out.push({ emoji: r.emoji, ids: [r.agent_id] });
                }
                return out;
            }
            readonly property bool mine: role === "user"
            readonly property var quoted: reply_to > 0 ? ch.messageById(reply_to) : null
            readonly property bool sys: role === "system"
            readonly property bool routineNote: sys && content.startsWith("⏰")
            readonly property var who: ch.agentOf(author)
            readonly property color tint: who && who.color ? who.color : Theme.text2
            readonly property var stepList: JSON.parse(steps || "[]")
            readonly property var innerSteps: stepList.filter(s => U.action(s) === null)
            property bool expanded: false

            width: thread.width
            // Room for the hover actions under each message.
            readonly property real bodyBottom: sys ? notice.y + notice.height + 8 : mine ? bubble.y + bubble.height : answer.y + answer.height
            height: bodyBottom + (sys ? 0 : 28) + (reactGroups.length ? 24 : 0)

            HoverHandler { id: msgHover }

            // Reactions, hanging off the bottom of the bubble like in a messenger.
            Row {
                id: reactRow
                visible: !msg.sys && msg.reactGroups.length > 0
                z: 2
                y: msg.bodyBottom - 6
                x: msg.mine ? bubble.x + bubble.width - width - 10 : answer.x + 54 + 12
                spacing: 4
                Repeater {
                    model: msg.reactGroups
                    delegate: Rectangle {
                        id: pill
                        required property var modelData
                        width: pillRow.implicitWidth + 14
                        height: 26
                        radius: Theme.radiusMd
                        color: Theme.card
                        border.width: 1
                        border.color: Theme.wash5
                        scale: 0.6
                        Component.onCompleted: scale = 1
                        Behavior on scale { NumberAnimation { duration: 260; easing.type: Easing.OutBack } }
                        Row {
                            id: pillRow
                            anchors.centerIn: parent
                            spacing: 5
                            Text {
                                anchors.verticalCenter: parent.verticalCenter
                                text: pill.modelData.emoji
                                font.pixelSize: Theme.fontLarge
                            }
                            Row {
                                anchors.verticalCenter: parent.verticalCenter
                                spacing: -5
                                Repeater {
                                    model: pill.modelData.ids
                                    delegate: Avatar {
                                        required property string modelData
                                        readonly property var a: ch.agentOf(modelData)
                                        size: 18
                                        name: a ? a.name : "?"
                                        photo: U.photo(a)
                                        tint: a && a.color ? a.color : Theme.text2
                                    }
                                }
                            }
                        }
                        HoverHandler { id: pillHover }
                        Tip {
                            show: pillHover.hovered
                            text: pill.modelData.ids.map(id => (ch.agentOf(id) || { name: id }).name).join(", ") + " reagiu"
                        }
                    }
                }
            }

            MessageActions {
                visible: !msg.sys && opacity > 0
                shown: msgHover.hovered
                canReply: msg.id > 0
                y: parent.height - 27
                x: msg.mine ? bubble.x + bubble.width - width : answer.x + 54
                onCopy: Api.copyText(msg.raw)
                onReply: ch.replyTo(msg)
                onDetails: ch.showDetails(msg)
            }

            // What this message answers, above the bubble.
            Text {
                id: quoteLine
                visible: msg.mine && msg.quoted !== null
                y: msg.head ? 14 : 0
                x: bubble.x + bubble.width - width
                width: Math.min(implicitWidth, ch.column * 0.7)
                textFormat: Text.StyledText
                text: msg.quoted ? "↪ " + U.esc(ch.authorOf(msg.quoted)) + ": <font color='" + Theme.text4 + "'>" +
                                   U.esc(msg.quoted.raw.replace(/[*_`#>|]/g, "").replace(/\s+/g, " ").slice(0, 120)) + "</font>" : ""
                color: Theme.text3
                font.pixelSize: Theme.fontSmall
                elide: Text.ElideRight
            }

            // Loop lock notice.
            Rectangle {
                id: notice
                visible: msg.sys
                y: msg.head ? 14 : 0
                anchors.horizontalCenter: parent.horizontalCenter
                width: Math.min(noticeText.implicitWidth + 52, ch.column)
                height: noticeText.implicitHeight + 18
                radius: msg.routineNote ? 14 : height / 2
                color: msg.routineNote ? Theme.wash1 : Qt.rgba(1, 0.75, 0.4, 0.07)
                border.width: 1
                border.color: msg.routineNote ? Theme.line : Qt.rgba(1, 0.75, 0.4, 0.25)
                Icon {
                    x: 14
                    y: msg.routineNote ? 11 : (parent.height - height) / 2
                    name: msg.routineNote ? "history" : "lock"
                    size: 14
                    color: msg.routineNote ? Theme.text3 : Theme.warn
                }
                Text {
                    id: noticeText
                    x: 36
                    anchors.verticalCenter: parent.verticalCenter
                    width: Math.min(implicitWidth, ch.column - 52)
                    // A routine notice: its title, then the prompt, quieter.
                    text: msg.routineNote ? "<b>" + U.esc(msg.content.split("\n")[0].replace("⏰ ", "")) + "</b><br><font color='" + Theme.text4 + "'>" +
                                            U.esc(msg.content.split("\n").slice(1).join(" ")) + "</font>" : msg.content
                    textFormat: msg.routineNote ? Text.StyledText : Text.PlainText
                    color: msg.routineNote ? Theme.text3 : Theme.warn
                    font.pixelSize: Theme.fontBody
                    lineHeight: 1.15
                    wrapMode: Text.Wrap
                    maximumLineCount: 4
                    elide: Text.ElideRight
                }
            }

            Rectangle {
                id: bubble
                visible: msg.mine
                y: (msg.head ? 14 : 0) + (quoteLine.visible ? quoteLine.height + 6 : 0)
                x: (thread.width + ch.column) / 2 - width
                width: Math.min(userText.implicitWidth + 36, ch.column * 0.78, 560)
                height: userText.implicitHeight + 22
                radius: Theme.radius
                color: Theme.active
                border.width: 1
                border.color: Theme.wash4
                TextEdit {
                    id: userText
                    x: 18
                    y: 11
                    width: Math.min(implicitWidth, ch.column * 0.78 - 36, 560 - 36)
                    text: msg.content
                    color: Theme.text
                    font.pixelSize: Theme.fontLarge
                    wrapMode: TextEdit.Wrap
                    readOnly: true
                    selectByMouse: true
                    selectionColor: Theme.wash6
                }
            }

            Item {
                id: answer
                visible: !msg.mine && !msg.sys
                y: msg.head ? 14 : 0
                x: (thread.width - ch.column) / 2
                width: ch.column
                height: bubbleA.height

                Avatar {
                    visible: msg.head
                    size: 42
                    name: msg.who ? msg.who.name : "?"
                    photo: U.photo(msg.who)
                    tint: msg.tint
                }
                AgentBubble {
                    id: bubbleA
                    x: 54
                    maxWidth: Math.min(answer.width - 54, 780) // full-width thread, readable lines
                    head: msg.head
                    tint: msg.tint
                    name: msg.who ? msg.who.name : "Ex-funcionário"
                    sub: (msg.who ? msg.who.role : "") + (msg.at ? "  ·  " + U.clock(msg.at).slice(0, 5) : "")
                    // Only text: a bubble its size; cards and tables take the full width.
                    natural: msg.err !== "" || msg.stepList.some(s => U.action(s) !== null || /docs_(criar|editar)$/.test(s.tool || "")) || msg.content.indexOf("(avora:doc/") >= 0 ? Infinity
                             : Math.max(chBody.naturalWidth, msg.innerSteps.length ? stepsChip.width : 0)
                Column {
                    id: col
                    width: parent.width
                    spacing: 8

                    MarkdownView {
                        id: chBody
                        width: parent.width
                        visible: msg.content !== ""
                        text: msg.content
                        tint: msg.tint
                        mentions: ch.teamChips
                        onLinkActivated: link => link.startsWith("avora:doc/") ? DocBus.open(link.slice(10)) : Qt.openUrlExternally(link)
                    }
                    DocCards {
                        width: parent.width
                        text: msg.content
                        steps: msg.stepList
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
                    ActionCards {
                        width: parent.width
                        steps: msg.stepList
                        tint: msg.who && msg.who.color ? msg.who.color : Theme.text2
                        onOpenRun: id => ch.openRun(id, msg.who ? msg.who.id : "")
                    }
                    // Everything the agent did, folded.
                    Column {
                        visible: msg.innerSteps.length > 0
                        width: parent.width
                        spacing: 6
                        Rectangle {
                            id: stepsChip
                            width: stepsLabel.implicitWidth + 40
                            height: 26
                            radius: Theme.radiusMd
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
        readonly property bool wanted: thread.visible && !ch.stick
        anchors { horizontalCenter: thread.horizontalCenter; bottom: thread.bottom; bottomMargin: 12 }
        color: hovered ? Theme.cardHover : Theme.card
        opacity: wanted ? 1 : 0
        visible: opacity > 0
        Behavior on opacity { NumberAnimation { duration: Theme.medium; easing.type: Theme.ease } }
        transform: Rotation { origin.x: jumpEnd.width / 2; origin.y: jumpEnd.height / 2; angle: 180 }
        onClicked: {
            ch.stick = true;
            thread.positionViewAtEnd();
        }
    }

    // ── Composer ────────────────────────────────────────────────
    Column {
        id: composerBox
        anchors { bottom: parent.bottom; bottomMargin: 18; horizontalCenter: parent.horizontalCenter }
        width: ch.column
        spacing: 10

        ApprovalCards {
            width: parent.width
            items: ch.approvals
            agents: ch.agents
            onDecided: ch.approvalDecided()
        }
        ErrorBanner {
            width: parent.width
            text: ch.error
        }
        ReplyBar {
            width: parent.width
            target: ch.replyTarget
            onCancel: ch.replyTarget = null
        }
        Item {
            width: parent.width
            height: composer.height

            // @ suggestions, above the field.
            Card {
                id: pop
                visible: ch.suggestions.length > 0
                anchors { bottom: parent.top; bottomMargin: 8; left: parent.left }
                width: 300
                height: popCol.implicitHeight + 12
                padding: 6
                radius: Theme.radiusLg
                z: 5
                Column {
                    id: popCol
                    width: parent.width
                    Repeater {
                        model: ch.suggestions
                        delegate: Rectangle {
                            id: sg
                            required property var modelData
                            required property int index
                            width: popCol.width
                            height: 42
                            radius: Theme.radiusSm
                            color: index === ch.pick ? Theme.active : sgArea.containsMouse ? Theme.hover : "transparent"
                            Avatar {
                                id: sgFace
                                x: 8
                                anchors.verticalCenter: parent.verticalCenter
                                size: 28
                                name: sg.modelData.id ? sg.modelData.name : "@"
                                photo: U.photo(sg.modelData)
                                tint: sg.modelData.color || Theme.text2
                            }
                            Text {
                                anchors { left: sgFace.right; leftMargin: 10; verticalCenter: parent.verticalCenter }
                                text: sg.modelData.name + "  <font color='" + Theme.text4 + "'>" + (sg.modelData.role || "") + "</font>"
                                textFormat: Text.StyledText
                                color: Theme.text
                                font.pixelSize: Theme.fontBody
                            }
                            MouseArea {
                                id: sgArea
                                anchors.fill: parent
                                hoverEnabled: true
                                cursorShape: Qt.PointingHandCursor
                                onClicked: {
                                    ch.complete(sg.index);
                                    composer.area.forceActiveFocus();
                                }
                            }
                        }
                    }
                }
            }

            AreaField {
                id: composer
                width: parent.width
                minHeight: 54
                rightInset: 54
                placeholderText: "Mensagem para #time  ·  @ para chamar alguém"
                onSubmitted: ch.send()
                keyFilter: event => {
                    if (ch.suggestions.length === 0)
                        return false;
                    if (event.key === Qt.Key_Up)
                        ch.pick = (ch.pick + ch.suggestions.length - 1) % ch.suggestions.length;
                    else if (event.key === Qt.Key_Down)
                        ch.pick = (ch.pick + 1) % ch.suggestions.length;
                    else if (event.key === Qt.Key_Escape)
                        ch.suggestions = [];
                    else
                        ch.complete(ch.pick); // Enter or Tab
                    event.accepted = true;
                    return true;
                }
                Connections {
                    target: composer.area
                    function onTextChanged() { ch.updateSuggestions(); }
                    function onCursorPositionChanged() { ch.updateSuggestions(); }
                }
            }
            Item {
                id: sendBtn
                readonly property bool ready: composer.text.trim() !== ""
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
                        name: "send"
                        size: 17
                        color: sendBtn.ready ? Theme.ink : Theme.text3
                    }
                }
                MouseArea {
                    id: sendArea
                    anchors.fill: parent
                    cursorShape: Qt.PointingHandCursor
                    onClicked: ch.send()
                }
            }
        }
        // Who is answering, like "digitando…" in a messenger; the hint otherwise.
        Item {
            width: parent.width
            height: 18
            TypingIndicator {
                anchors { left: parent.left; leftMargin: 4; verticalCenter: parent.verticalCenter }
                people: ch.workingIds.concat(ch.nextIds.filter(id => ch.working[id] === undefined)).map(id => {
                    const who = ch.agentOf(id) || { name: id };
                    const run = ch.working[id] || {};
                    const st = run.job_id ? ch.stepOf[run.job_id] : undefined;
                    return { id: id, name: who.name, color: who.color, photo: U.photo(who), step: st && st.tool ? st.tool.split(" · ")[0] : "", summary: st ? st.summary : "" };
                })
            }
            Text {
                id: routingLine
                visible: !ch.busy && ch.nextIds.length === 0 && ch.routing
                anchors { left: parent.left; leftMargin: 4; verticalCenter: parent.verticalCenter }
                text: "Vendo quem do time responde…"
                color: Theme.text3
                font.pixelSize: Theme.fontSmall
                SequentialAnimation on opacity {
                    running: routingLine.visible && Qt.application.state === Qt.ApplicationActive
                    loops: Animation.Infinite
                    NumberAnimation { to: 0.4; duration: 650; easing.type: Easing.InOutSine }
                    NumberAnimation { to: 1; duration: 650; easing.type: Easing.InOutSine }
                }
            }
            Text {
                visible: !ch.busy && ch.nextIds.length === 0 && !ch.routing
                anchors.centerIn: parent
                text: ch.status && ch.status.discussing ? "O time está conversando entre si  ·  " + ch.streak + " de " + ch.limit + " falas  ·  escreva para encerrar"
                      : "Sem @ responde quem tem a ver  ·  @todos chama o canal  ·  peça \"conversem entre si\" para uma discussão  ·  trava em " + ch.limit + " respostas seguidas"
                color: Theme.text4
                font.pixelSize: Theme.fontSmall
            }
        }
    }

    MessageDetails {
        id: details
        anchors.fill: parent
    }
}
