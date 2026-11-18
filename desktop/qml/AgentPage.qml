import QtQuick
import QtQuick.Controls.Basic
import QtQuick.Dialogs
import QtCore
import Avora
import "util.js" as U

// An agent's page (D62): who it is, what it does and what it spent, in
// tabs. Edits from every tab collect in one draft; the SaveBar lists them
// and saves them together as the agent's next version.
Item {
    id: page
    property var agents: []
    property var projects: []
    property string agentId: ""
    readonly property var agent: agents.find(a => a.id === agentId) || null
    readonly property var project: agent && agent.project_id ? projects.find(p => p.id === agent.project_id) || null : null
    property bool shown: false
    property string tab: "overview"
    property var brains: []
    property var usage: null

    // The draft is what the tabs edit; base is the saved agent it started
    // from. changes is the difference, for the SaveBar.
    property var base: ({})
    property var draft: ({})
    property int baseVersion: 0
    readonly property var changes: shown ? diff(base, draft) : []
    property bool saving: false
    property string saveError: ""
    property bool leaving: false // asking whether to leave without saving
    property string nextAgent: "" // where to go after leaving ("" = close)

    property bool uploading: false
    property string photoError: ""

    signal changed // something about the agent changed: reload the team
    signal archived(string id)
    signal draftReset
    signal runRequested(string id, string agentId)
    signal docRequested(string id)
    signal routineRequested(var routine, var preset)
    signal pageRequested(string name)

    readonly property var tabs: [
        { key: "overview", label: "Visão geral", icon: "user", url: "AgentOverviewTab.qml" },
        { key: "instructions", label: "Instruções", icon: "book", url: "AgentInstructionsTab.qml" },
        { key: "tools", label: "Ferramentas e acesso", icon: "plug", url: "AgentToolsTab.qml" },
        { key: "memory", label: "Memória", icon: "bookmark", url: "AgentMemoryTab.qml" },
        { key: "files", label: "Arquivos", icon: "docs", url: "AgentFilesTab.qml" },
        { key: "usage", label: "Uso", icon: "pulse", url: "AgentUsageTab.qml" },
        { key: "activity", label: "Atividade", icon: "history", url: "AgentActivityTab.qml" },
        { key: "settings", label: "Configurações", icon: "gear", url: "AgentSettingsTab.qml" }
    ]

    // ── Opening and leaving ─────────────────────────────────────
    function open(id, tabName) {
        if (shown && id !== agentId && changes.length > 0) {
            nextAgent = id;
            leaving = true;
            return;
        }
        const switching = id !== agentId || !shown;
        agentId = id;
        if (switching) {
            usage = null;
            reset();
            loadUsage();
            Api.get("/v1/brains", (err, list) => {
                if (!err)
                    page.brains = list || [];
            });
        }
        shown = true;
        openTab(tabName || (switching ? "overview" : tab));
    }
    function close() {
        if (changes.length > 0) {
            nextAgent = "";
            leaving = true;
            return;
        }
        shown = false;
    }
    // After the leave prompt: go where the owner was heading.
    function leave() {
        leaving = false;
        const next = nextAgent;
        nextAgent = "";
        draft = copy(base);
        if (next !== "")
            open(next, tab);
        else
            shown = false;
    }

    function openTab(name) {
        const t = tabs.find(x => x.key === name) || tabs[0];
        tab = t.key;
        tabLoader.setSource(t.url, { page: page });
        body.contentY = 0;
    }

    // ── The draft ───────────────────────────────────────────────
    function fresh(a) {
        if (!a)
            return {};
        const st = a.style || {}, b = a.brain || {};
        return {
            name: a.name || "", role: a.role || "", persona: a.persona || "", color: a.color || "",
            approvals: a.approvals || "",
            style: { voice: st.voice || "", talk: st.talk || "", emoji: st.emoji || "", length: st.length || "" },
            brain: {
                provider: b.provider || "",
                model: b.model || "",
                effort: b.effort || "",
                fallback: b.fallback === "none" ? "none" : "",
                rotate: b.rotate !== false,
                account: b.account || "",
                context: b.context || 0,
                speed: b.speed || ""
            },
            responsibilities: (a.responsibilities || []).map(r => ({ id: r.id || "", goal: r.goal || "", metric: r.metric || "", cadence: r.cadence || "" }))
        };
    }
    function copy(o) {
        return JSON.parse(JSON.stringify(o));
    }
    function reset() {
        base = fresh(agent);
        baseVersion = agent ? agent.version : 0;
        draft = copy(base);
        saveError = "";
        draftReset();
    }
    // set changes one field: "name" or "style.voice".
    function set(key, value) {
        if (draft.style === undefined) // the agent has not arrived yet
            return;
        const d = copy(draft);
        const parts = key.split(".");
        if (parts.length === 2)
            d[parts[0]][parts[1]] = value;
        else
            d[key] = value;
        draft = d;
    }
    function discard() {
        draft = copy(base);
        saveError = "";
        draftReset();
    }

    // A saved version from elsewhere (a restore, another window) replaces
    // the draft, unless the owner is in the middle of editing.
    onAgentChanged: {
        if (!shown || !agent)
            return;
        if (agent.version !== baseVersion && changes.length === 0)
            reset();
    }

    readonly property var labels: ({
        name: "Nome", role: "Cargo", persona: "Persona e funções", color: "Cor", approvals: "Aprovações",
        "style.voice": "Jeito de falar", "style.talk": "No #time", "style.emoji": "Emoji", "style.length": "Respostas",
        "brain.provider": "Cérebro", "brain.model": "Modelo", "brain.effort": "Esforço",
        "brain.fallback": "Se a conta falhar", "brain.rotate": "Rotação de contas", "brain.account": "Conta",
        "brain.context": "Janela de contexto", "brain.speed": "Velocidade",
        responsibilities: "Responsabilidades"
    })
    function show(key, v) {
        switch (key) {
        case "approvals": return ({ always: "Sempre pergunta", "": "Só o arriscado", never: "Age sozinho" })[v] || v;
        case "style.talk": return ({ pouco: "Fala pouco", "": "Normal", muito: "Participa muito" })[v] || v;
        case "style.emoji": return ({ nunca: "Nunca", "": "Às vezes", bastante: "Bastante" })[v] || v;
        case "style.length": return ({ curta: "Curtas", "": "Normais", longa: "Detalhadas" })[v] || v;
        case "brain.effort": return ({ "": "Padrão", low: "Baixo", medium: "Médio", high: "Alto", max: "Máximo" })[v] || v;
        case "brain.fallback": return v === "none" ? "Parar e avisar" : "Cair no Automático";
        case "brain.rotate": return v === false ? "Só esta conta" : "Girar entre contas";
        case "brain.account": return v === "" ? "Automática" : v;
        case "brain.context": return v === 1000000 ? "1M" : v === 400000 ? "400K" : v === 200000 ? "200K" : "Padrão";
        case "brain.speed": return v === "fast" ? "Rápida" : "Padrão";
        case "brain.provider": {
            if (v === "")
                return "Automático";
            const b = brains.find(x => x.provider === v);
            return b ? b.name : v;
        }
        case "brain.model": return v === "" ? "Padrão" : v;
        case "responsibilities": {
            const n = v.filter(r => r.goal.trim() !== "").length;
            return n === 1 ? "1 meta" : n + " metas";
        }
        case "persona": {
            const t = v.replace(/\s+/g, " ").trim();
            return t.length > 80 ? t.slice(0, 80) + "…" : t;
        }
        }
        return v;
    }
    function diff(a, b) {
        const out = [];
        if (!a || !b || a.style === undefined || b.style === undefined)
            return out;
        const plain = r => (r || []).filter(x => x.goal.trim() !== "").map(x => [x.id, x.goal.trim(), x.metric.trim(), x.cadence.trim()]);
        for (const key of Object.keys(labels)) {
            const parts = key.split(".");
            let from = parts.length === 2 ? a[parts[0]][parts[1]] : a[key];
            let to = parts.length === 2 ? b[parts[0]][parts[1]] : b[key];
            const same = key === "responsibilities" ? JSON.stringify(plain(from)) === JSON.stringify(plain(to))
                       : typeof from === "string" ? from.trim() === to.trim() : from === to;
            if (!same)
                out.push({ key: key, label: labels[key], from: show(key, from), to: show(key, to) });
        }
        return out;
    }

    function save(then) {
        if (saving || !agent)
            return;
        const d = draft;
        if (d.name.trim() === "") {
            saveError = "Dê um nome ao funcionário.";
            return;
        }
        const body = {
            name: d.name.trim(), role: d.role.trim(), persona: d.persona.trim(), color: d.color,
            style: { voice: d.style.voice.trim(), talk: d.style.talk, emoji: d.style.emoji, length: d.style.length },
            approvals: d.approvals,
            brain: {
                provider: d.brain.provider,
                model: d.brain.provider === "" ? "" : d.brain.model,
                effort: d.brain.effort,
                fallback: d.brain.fallback || "",
                rotate: d.brain.rotate !== false,
                account: d.brain.provider === "" ? "" : (d.brain.account || ""),
                context: d.brain.context || 0,
                speed: d.brain.speed || ""
            },
            responsibilities: d.responsibilities.filter(r => r.goal.trim() !== "")
                .map(r => ({ id: r.id, goal: r.goal.trim(), metric: r.metric.trim(), cadence: r.cadence.trim() }))
        };
        saving = true;
        saveError = "";
        Api.put("/v1/agents/" + agent.id, body, (err, saved) => {
            saving = false;
            if (err) {
                saveError = err;
                return;
            }
            base = fresh(saved);
            baseVersion = saved.version;
            draft = copy(base);
            draftReset();
            changed();
            if (then)
                then();
        });
    }

    // ── What the tabs call ──────────────────────────────────────
    function reload() {
        changed();
        loadUsage();
    }
    function loadUsage() {
        const id = agentId;
        if (id === "")
            return;
        Api.get("/v1/agents/" + id + "/usage?days=30", (err, u) => {
            if (!err && page.agentId === id)
                page.usage = u;
        });
    }
    function openRun(id) {
        runRequested(id, agentId);
    }
    function openDoc(id) {
        docRequested(id);
    }
    function newRoutine(preset) {
        routineRequested(null, preset || null);
    }
    function editRoutine(r) {
        routineRequested(r, null);
    }
    function openPlugins() {
        pageRequested("plugins");
    }
    function openSkills() {
        pageRequested("skills");
    }
    function pickPhoto() {
        photoDialog.open();
    }
    function archive() {
        if (!agent)
            return;
        const id = agent.id;
        Api.post("/v1/agents/" + id + "/archive", { archived: true }, err => {
            if (err) {
                saveError = err;
                return;
            }
            draft = copy(base);
            shown = false;
            archived(id);
        });
    }

    // The photo is cut to a centered square and sent as a 512 px WebP.
    FileDialog {
        id: photoDialog
        title: "Escolha uma foto"
        nameFilters: ["Imagens (*.png *.jpg *.jpeg *.webp *.bmp *.gif)"]
        currentFolder: StandardPaths.writableLocation(StandardPaths.PicturesLocation)
        onAccepted: {
            const id = page.agentId;
            if (id === "")
                return;
            page.uploading = true;
            page.photoError = "";
            Api.uploadAvatar(id, selectedFile, err => {
                page.uploading = false;
                page.photoError = err || "";
                page.changed();
            });
        }
    }

    // ── The page ────────────────────────────────────────────────
    visible: opacity > 0
    opacity: shown ? 1 : 0
    Behavior on opacity { NumberAnimation { duration: Theme.medium } }

    Rectangle {
        anchors.fill: parent
        color: Theme.bg
        MouseArea { anchors.fill: parent } // keep clicks off the chat below
    }

    readonly property real column: Math.min(980, width - 2 * Theme.space6)
    readonly property real side: (width - column) / 2
    readonly property color tint: agent && agent.color ? agent.color : Theme.text2

    // Header: back, face, name, role and where it stands.
    Item {
        id: head
        x: page.side
        y: Theme.titleBar + Theme.space3
        width: page.column
        height: 84

        Avatar {
            id: face
            size: 68
            name: page.draft.name || (page.agent ? page.agent.name : "")
            photo: U.photo(page.agent)
            tint: page.draft.color || page.tint
            working: page.agent ? !!page.agent.working : false
            anchors.verticalCenter: parent.verticalCenter
            opacity: page.uploading ? 0.5 : 1
            MouseArea {
                anchors.fill: parent
                cursorShape: Qt.PointingHandCursor
                onClicked: page.openTab("settings")
            }
        }
        Column {
            anchors { left: face.right; leftMargin: Theme.space4; right: headActions.left; rightMargin: Theme.space4; verticalCenter: parent.verticalCenter }
            spacing: Theme.space1
            Text {
                width: parent.width
                text: page.agent ? page.agent.name : ""
                color: Theme.white
                font.family: Theme.display
                font.pixelSize: Theme.fontDisplay
                font.weight: Font.DemiBold
                elide: Text.ElideRight
            }
            Text {
                width: parent.width
                visible: text !== ""
                text: page.agent ? page.agent.role : ""
                color: Theme.text2
                font.pixelSize: Theme.fontLarge
                elide: Text.ElideRight
            }
            Row {
                spacing: Theme.space2
                Dot {
                    anchors.verticalCenter: parent.verticalCenter
                    kind: page.agent && page.agent.working ? "live" : ""
                }
                Text {
                    text: (page.agent && page.agent.working ? "Trabalhando" : "Livre") + "  ·  "
                          + (page.project ? page.project.name : "Universal") + "  ·  v" + (page.agent ? page.agent.version : "")
                    color: Theme.text3
                    font.pixelSize: Theme.fontSmall
                }
            }
        }
        Row {
            id: headActions
            anchors { right: parent.right; verticalCenter: parent.verticalCenter }
            spacing: Theme.space2
            PrimaryButton {
                text: "Conversar"
                icon: "chat"
                onClicked: page.close()
            }
            GhostButton {
                round: true
                icon: "close"
                onClicked: page.close()
            }
        }
    }

    // Tabs.
    Item {
        id: tabBar
        x: page.side
        anchors.top: head.bottom
        anchors.topMargin: Theme.space4
        width: page.column
        height: 40
        Rectangle {
            anchors { left: parent.left; right: parent.right; bottom: parent.bottom }
            height: 1
            color: Theme.line
        }
        Flickable {
            anchors.fill: parent
            contentWidth: tabRow.implicitWidth
            flickableDirection: Flickable.HorizontalFlick
            boundsBehavior: Flickable.StopAtBounds
            clip: true
            Row {
                id: tabRow
                height: parent.height
                spacing: Theme.space1
                Repeater {
                    model: page.tabs
                    delegate: Item {
                        id: tb
                        required property var modelData
                        readonly property bool on: page.tab === modelData.key
                        width: tbRow.implicitWidth + 2 * Theme.space2 + 4
                        height: tabRow.height
                        Rectangle {
                            anchors { fill: parent; bottomMargin: 4 }
                            radius: Theme.radiusSm
                            color: tbArea.containsMouse && !tb.on ? Theme.hover : "transparent"
                        }
                        Row {
                            id: tbRow
                            anchors.centerIn: parent
                            anchors.verticalCenterOffset: -2
                            spacing: 6
                            Icon {
                                visible: page.column >= 940 // labels first when it is tight
                                anchors.verticalCenter: parent.verticalCenter
                                name: tb.modelData.icon
                                size: 14
                                color: tb.on ? Theme.white : Theme.text3
                            }
                            Text {
                                anchors.verticalCenter: parent.verticalCenter
                                text: tb.modelData.label
                                color: tb.on ? Theme.white : Theme.text3
                                font.pixelSize: Theme.fontBody
                                font.weight: tb.on ? Font.DemiBold : Font.Normal
                            }
                        }
                        Rectangle {
                            anchors { left: parent.left; right: parent.right; bottom: parent.bottom; leftMargin: Theme.space2; rightMargin: Theme.space2 }
                            height: 2
                            radius: 1
                            color: page.tint
                            visible: tb.on
                        }
                        MouseArea {
                            id: tbArea
                            anchors.fill: parent
                            hoverEnabled: true
                            cursorShape: Qt.PointingHandCursor
                            onClicked: page.openTab(tb.modelData.key)
                        }
                    }
                }
            }
        }
    }

    // The open tab; the page scrolls, not the tab.
    Flickable {
        id: body
        anchors { top: tabBar.bottom; bottom: parent.bottom; left: parent.left; right: parent.right }
        contentHeight: tabLoader.y + tabLoader.height + 140 // room for the SaveBar
        clip: true
        boundsBehavior: Flickable.StopAtBounds
        ScrollBar.vertical: SlimScrollBar {}
        SmoothWheel {}
        Loader {
            id: tabLoader
            x: page.side
            y: Theme.space5
            width: page.column
            onLoaded: item.width = Qt.binding(() => tabLoader.width)
        }
    }

    SaveBar {
        id: saveBar
        anchors { horizontalCenter: parent.horizontalCenter; bottom: parent.bottom; bottomMargin: Theme.space5 }
        width: Math.min(760, parent.width - 2 * Theme.space5)
        changes: page.changes
        saving: page.saving
        error: page.saveError
        onSave: page.save(null)
        onDiscard: page.discard()
    }

    Shortcut {
        sequence: "Esc"
        enabled: page.shown && page.leaving
        onActivated: page.leaving = false
    }

    // Leaving with unsaved changes.
    Item {
        anchors.fill: parent
        visible: page.leaving
        z: 10
        Rectangle {
            anchors.fill: parent
            color: Theme.scrim
            MouseArea {
                anchors.fill: parent
                onClicked: page.leaving = false
            }
        }
        Card {
            anchors.centerIn: parent
            width: Math.min(440, parent.width - 2 * Theme.space5)
            height: leaveCol.implicitHeight + 2 * padding
            padding: Theme.space5
            Column {
                id: leaveCol
                width: parent.width
                spacing: Theme.space3
                Text {
                    text: "Sair sem salvar?"
                    color: Theme.white
                    font.family: Theme.display
                    font.pixelSize: Theme.fontHeading
                    font.weight: Font.DemiBold
                }
                Text {
                    width: parent.width
                    wrapMode: Text.Wrap
                    text: "Você mudou " + (page.changes.length === 1 ? "1 coisa" : page.changes.length + " coisas") + " em "
                          + (page.agent ? page.agent.name : "") + ": " + page.changes.map(c => c.label.toLowerCase()).join(", ") + "."
                    color: Theme.text2
                    font.pixelSize: Theme.fontBody
                }
                Row {
                    spacing: Theme.space2
                    topPadding: Theme.space2
                    PrimaryButton {
                        text: "Salvar e sair"
                        icon: "check"
                        busy: page.saving
                        onClicked: page.save(() => page.leave())
                    }
                    GhostButton {
                        text: "Descartar"
                        danger: true
                        onClicked: page.leave()
                    }
                    GhostButton {
                        text: "Continuar editando"
                        onClicked: page.leaving = false
                    }
                }
            }
        }
    }
}
