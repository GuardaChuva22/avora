import QtCore
import QtQuick
import QtQuick.Controls.Basic
import Avora
import "util.js" as U

// Quick switcher and action launcher (Ctrl+K): searches agents, channels,
// people, pages and actions without accents or case sensitivity.
Item {
    id: root
    property bool shown: false

    property var agents: []
    property var channels: []
    property var members: []
    property var projects: []
    property bool creatingDoc: false

    property string recentsStore: "[]"
    property var recents: []
    property int selectedIndex: 0
    property real lastMouseX: -1
    property real lastMouseY: -1
    property Item previousFocus: null
    property bool suppressFocusRestore: false

    Settings {
        category: "commandMenu"
        property alias recentsJson: root.recentsStore
    }

    readonly property var pageCatalog: [
        { key: "home", name: "Início", sub: "Resumo do espaço e entregas recentes", icon: "spark", terms: "inicio home painel" },
        { key: "waiting", name: "Esperando você", sub: "Aprovações, perguntas e decisões pendentes", icon: "clock", terms: "esperando aprovacoes perguntas" },
        { key: "docs", name: "Docs", sub: "Documentos, páginas e arquivos do time", icon: "docs", terms: "docs documentos drive arquivos wiki" },
        { key: "goals", name: "Objetivos", sub: "Metas em andamento com mandato", icon: "target", terms: "objetivos metas funil" },
        { key: "team", name: "Equipe", sub: "Agentes e setores do espaço", icon: "users", terms: "equipe funcionarios agentes setores time" },
        { key: "routines", name: "Rotinas", sub: "Tarefas agendadas e gatilhos automáticos", icon: "calendar", terms: "rotinas horarios agendamentos lembretes" },
        { key: "tasks", name: "Gerenciador de tarefas", sub: "Execuções em andamento agora", icon: "tasks", terms: "gerenciador tarefas execucoes rodando" },
        { key: "teammemory", name: "Memória do time", sub: "Fatos, regras e aprendizados compartilhados", icon: "bookmark", terms: "memoria fatos regras decisoes" },
        { key: "plugins", name: "Plugins", sub: "Apps e integrações conectadas", icon: "plug", terms: "plugins apps conectores integracoes" },
        { key: "skills", name: "Skills", sub: "Habilidades e passo a passo dos agentes", icon: "book", terms: "skills habilidades fluxos" },
        { key: "config", name: "Configurações", sub: "Contas de IA, pessoas e computadores", icon: "gear", terms: "configuracoes ajustes modelos nos pessoas contas" },
        { key: "chat", name: "Conversas", sub: "Abrir a tela de conversa atual", icon: "chat", terms: "chat conversas mensagens" }
    ]

    readonly property var actionCatalog: [
        { id: "new_agent", name: "Novo agente", sub: "Criar um funcionário para o espaço", icon: "plus", terms: "novo criar agente funcionario" },
        { id: "invite", name: "Convidar pessoas", sub: "Gerar link de convite para o espaço", icon: "users", terms: "convidar pessoas membro link" },
        { id: "connect_ai", name: "Conectar uma IA", sub: "Adicionar assinatura ou chave de API", icon: "spark", terms: "conectar ia modelo conta chave api claude codex gemini openai" },
        { id: "new_channel", name: "Novo canal", sub: "Criar um canal de conversa em grupo", icon: "hash", terms: "novo criar canal grupo" },
        { id: "new_doc", name: "Novo doc", sub: "Criar um documento nos Docs", icon: "doc", terms: "novo criar doc documento pagina" }
    ]

    Component.onCompleted: loadRecents()

    function fold(t) {
        return String(t || "").toLowerCase().normalize("NFD").replace(/[\u0300-\u036f]/g, "");
    }

    function currentWorkspaceId() {
        if (Api.workspace && Api.workspace !== "")
            return Api.workspace;
        if (Api.me && Api.me.workspaces && Api.me.workspaces.length > 0)
            return Api.me.workspaces[0].id || "pessoal";
        return "pessoal";
    }

    function myPersonId() {
        return Api.me && Api.me.person && Api.me.person.id ? Api.me.person.id : "";
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

    function roleLabel(r) {
        return r === "owner" ? "Dono" : r === "admin" ? "Administrador" : "Membro";
    }

    function loadRecents() {
        try {
            const parsed = JSON.parse(recentsStore || "[]");
            recents = Array.isArray(parsed) ? parsed.slice(0, 6) : [];
        } catch (e) {
            recents = [];
        }
    }

    function rememberItem(it) {
        if (!it || it.kind === "action")
            return;
        const entry = {
            key: it.key,
            kind: it.kind,
            targetId: it.targetId,
            name: it.name,
            sub: it.sub || "",
            icon: it.icon || "",
            tint: it.tint ? String(it.tint) : ""
        };
        const next = [entry].concat(recents.filter(r => r && r.key !== entry.key)).slice(0, 6);
        recents = next;
        recentsStore = JSON.stringify(next);
    }

    function refreshData() {
        if (!Api.signedIn)
            return;
        Api.get("/v1/agents", (err, list) => {
            if (!err && Array.isArray(list))
                root.agents = list;
        });
        Api.get("/v1/channels", (err, list) => {
            if (!err && Array.isArray(list))
                root.channels = list.filter(c => !c.archived_at);
        });
        const wsId = currentWorkspaceId();
        if (wsId !== "") {
            Api.get("/v1/workspaces/" + encodeURIComponent(wsId) + "/members", (err, list) => {
                if (!err && Array.isArray(list))
                    root.members = list.filter(m => m.status !== "pending");
                else if (err)
                    root.members = [];
            });
        }
        Api.get("/v1/projects", (err, list) => {
            if (!err && Array.isArray(list))
                root.projects = list;
        });
    }

    function open() {
        if (!shown && Window.window && Window.window.activeFocusItem)
            previousFocus = Window.window.activeFocusItem;
        suppressFocusRestore = false;
        loadRecents();
        searchField.text = "";
        selectedIndex = 0;
        listFlick.contentY = 0;
        refreshData();
        shown = true;
        Qt.callLater(() => searchField.forceActiveFocus());
    }

    function close() {
        if (!shown)
            return;
        shown = false;
        if (!suppressFocusRestore) {
            const prev = previousFocus;
            previousFocus = null;
            if (prev && prev.visible && prev.forceActiveFocus)
                Qt.callLater(() => { if (prev && prev.visible) prev.forceActiveFocus(); });
        } else {
            previousFocus = null;
        }
    }

    function agentEntry(a) {
        const stateNote = a.working ? "trabalhando" : a.status === "waiting" ? "esperando você" : "";
        const baseRole = a.role || "Agente";
        return {
            key: "agent:" + a.id,
            kind: "agent",
            targetId: a.id,
            name: a.name || a.id,
            sub: stateNote !== "" && a.role ? a.role + " · " + stateNote : (a.role || (stateNote !== "" ? stateNote : "Conversa direta")),
            icon: "user",
            photo: U.photo(a),
            tint: a.color || Theme.text2,
            working: !!a.working,
            terms: (a.name || "") + " " + (a.role || "") + " " + (a.sector || "")
        };
    }

    function channelEntry(c) {
        return {
            key: "channel:" + c.id,
            kind: "channel",
            targetId: c.id,
            name: "#" + (c.name || c.id),
            sub: c.purpose ? c.purpose : (c.everyone ? "Canal de todo o time" : "Canal"),
            icon: "hash",
            photo: "",
            tint: Theme.text2,
            working: false,
            terms: "#" + (c.name || "") + " " + (c.name || "") + " " + (c.purpose || "")
        };
    }

    function personEntry(m) {
        const label = m.name || m.email || m.person_id;
        const isMe = myPersonId() !== "" && m.person_id === myPersonId();
        return {
            key: "person:" + m.person_id,
            kind: "person",
            targetId: m.person_id,
            name: label,
            sub: m.status_text ? m.status_text : (isMe ? "Você · " + roleLabel(m.role) : roleLabel(m.role)),
            icon: "user",
            photo: personPhoto(m.avatar),
            tint: tintFor(label),
            working: false,
            terms: (m.name || "") + " " + (m.email || "") + " " + (m.status_text || "")
        };
    }

    function pageEntry(p) {
        return {
            key: "page:" + p.key,
            kind: "page",
            targetId: p.key,
            name: p.name,
            sub: p.sub,
            icon: p.icon,
            photo: "",
            tint: Theme.text2,
            working: false,
            terms: p.name + " " + p.sub + " " + (p.terms || "")
        };
    }

    function actionEntry(act) {
        return {
            key: "action:" + act.id,
            kind: "action",
            targetId: act.id,
            name: act.name,
            sub: act.sub,
            icon: act.icon,
            photo: "",
            tint: Theme.text2,
            working: false,
            terms: act.name + " " + act.sub + " " + (act.terms || "")
        };
    }

    function resolveRecent(r) {
        if (!r || !r.kind || !r.targetId)
            return null;
        if (r.kind === "agent") {
            const a = agents.find(x => x.id === r.targetId);
            if (a)
                return agentEntry(a);
            if (agents.length > 0)
                return null;
        } else if (r.kind === "channel") {
            const c = channels.find(x => x.id === r.targetId);
            if (c)
                return channelEntry(c);
            if (channels.length > 0)
                return null;
        } else if (r.kind === "person") {
            const m = members.find(x => x.person_id === r.targetId);
            if (m)
                return personEntry(m);
            if (members.length > 0)
                return null;
        } else if (r.kind === "page") {
            const p = pageCatalog.find(x => x.key === r.targetId);
            return p ? pageEntry(p) : null;
        }
        return {
            key: r.key || (r.kind + ":" + r.targetId),
            kind: r.kind,
            targetId: r.targetId,
            name: r.name || r.targetId,
            sub: r.sub || "",
            icon: r.icon || "spark",
            photo: "",
            tint: r.tint || Theme.text2,
            working: false,
            terms: ""
        };
    }

    function matchGroup(items, tokens, maxCount) {
        const scored = [];
        for (let i = 0; i < items.length; i++) {
            const it = items[i];
            const nameF = fold(it.name);
            const hay = nameF + " " + fold(it.sub) + " " + fold(it.terms);
            let ok = true;
            for (let t = 0; t < tokens.length; t++) {
                if (hay.indexOf(tokens[t]) < 0) {
                    ok = false;
                    break;
                }
            }
            if (!ok)
                continue;
            const first = tokens[0];
            const rank = nameF === first ? 0
                : nameF.startsWith(first) || nameF.startsWith("#" + first) ? 1
                : nameF.indexOf(first) >= 0 ? 2 : 3;
            scored.push({ it: it, rank: rank, ord: i });
        }
        scored.sort((a, b) => a.rank - b.rank || a.ord - b.ord);
        return scored.slice(0, maxCount).map(x => x.it);
    }

    readonly property string trimmedQuery: fold(searchField.text).trim()

    readonly property var builtModel: {
        // Touch reactive dependencies explicitly.
        const agList = root.agents;
        const chList = root.channels;
        const memList = root.members;
        const recList = root.recents;
        const q = root.trimmedQuery;

        const rows = [];
        const items = [];
        const pushGroup = (title, list) => {
            if (!list || list.length === 0)
                return;
            rows.push({ isHeader: true, isHint: false, title: title, itemIndex: -1, entry: null });
            for (let i = 0; i < list.length; i++) {
                const idx = items.length;
                items.push(list[i]);
                rows.push({ isHeader: false, isHint: false, title: "", itemIndex: idx, entry: list[i] });
            }
        };

        if (q === "") {
            const resolvedRecents = [];
            for (let i = 0; i < recList.length && resolvedRecents.length < 6; i++) {
                const res = resolveRecent(recList[i]);
                if (res)
                    resolvedRecents.push(res);
            }
            rows.push({ isHeader: true, isHint: false, title: "ÚLTIMOS", itemIndex: -1, entry: null });
            if (resolvedRecents.length === 0) {
                rows.push({
                    isHeader: false,
                    isHint: true,
                    title: "Os agentes, canais e páginas que você abrir aparecem aqui.",
                    itemIndex: -1,
                    entry: null
                });
            } else {
                for (let i = 0; i < resolvedRecents.length; i++) {
                    const idx = items.length;
                    items.push(resolvedRecents[i]);
                    rows.push({ isHeader: false, isHint: false, title: "", itemIndex: idx, entry: resolvedRecents[i] });
                }
            }
            pushGroup("AÇÕES", root.actionCatalog.map(a => actionEntry(a)).slice(0, 8));
        } else {
            const tokens = q.split(/\s+/).filter(Boolean);
            pushGroup("AGENTES", matchGroup(agList.map(a => agentEntry(a)), tokens, 8));
            pushGroup("CANAIS", matchGroup(chList.map(c => channelEntry(c)), tokens, 8));
            pushGroup("PESSOAS", matchGroup(memList.map(m => personEntry(m)), tokens, 8));
            pushGroup("PÁGINAS", matchGroup(root.pageCatalog.map(p => pageEntry(p)), tokens, 8));
            pushGroup("AÇÕES", matchGroup(root.actionCatalog.map(a => actionEntry(a)), tokens, 8));
        }

        return { rows: rows, items: items };
    }

    readonly property var displayRows: builtModel.rows
    readonly property var selectableItems: builtModel.items

    onTrimmedQueryChanged: {
        selectedIndex = 0;
        listFlick.contentY = 0;
    }

    onSelectableItemsChanged: {
        if (selectableItems.length === 0)
            selectedIndex = -1;
        else if (selectedIndex < 0 || selectedIndex >= selectableItems.length)
            selectedIndex = 0;
    }

    function moveSelection(delta) {
        const n = selectableItems.length;
        if (n === 0)
            return;
        selectedIndex = (selectedIndex + delta + n) % n;
    }

    function ensureRowVisible(ry, rh, isFirst) {
        if (isFirst) {
            listFlick.contentY = 0;
            return;
        }
        const pad = Theme.space2;
        if (ry - pad < listFlick.contentY)
            listFlick.contentY = Math.max(0, ry - pad);
        else if (ry + rh + pad > listFlick.contentY + listFlick.height)
            listFlick.contentY = Math.max(0, Math.min(listFlick.contentHeight - listFlick.height, ry + rh + pad - listFlick.height));
    }

    function openConnectAI() {
        if (typeof win === "undefined" || !win)
            return;
        if ("section" in win)
            win.section = "modelos";
        if ("configSheet" in win)
            win.configSheet = "connect";
        if (win.sections && win.sections.indexOf("modelos") >= 0 && typeof win.go === "function")
            win.go("modelos:connect");
        else if (typeof win.go === "function")
            win.go("config");
    }

    function createQuickDoc() {
        if (creatingDoc)
            return;
        creatingDoc = true;
        Api.post("/v1/docs", { title: "Novo documento", kind: "doc", content: "# Novo documento\n\n" }, (err, d) => {
            root.creatingDoc = false;
            if (err || !d)
                return;
            DocBus.editNext = d.id;
            DocBus.load();
            if (typeof win !== "undefined" && win && typeof win.openFloat === "function") {
                DocBus.openId = d.id;
                win.openFloat("DocPanel.qml", { docId: d.id, embedded: true });
            } else {
                DocBus.open(d.id);
                if (typeof win !== "undefined" && win && typeof win.go === "function")
                    win.go("docs");
            }
        });
    }

    function executeItem(it) {
        if (!it)
            return;
        rememberItem(it);
        suppressFocusRestore = true;
        close();

        const hasWin = typeof win !== "undefined" && win !== null;
        if (it.kind === "agent") {
            if (hasWin && typeof win.openAgent === "function")
                win.openAgent(it.targetId);
            else if (hasWin && typeof win.openAgentConv === "function")
                win.openAgentConv(it.targetId, "");
            return;
        }
        if (it.kind === "channel") {
            if (hasWin && typeof win.openChannel === "function")
                win.openChannel(it.targetId);
            return;
        }
        if (it.kind === "person") {
            if (hasWin && typeof win.openPerson === "function")
                win.openPerson(it.targetId);
            return;
        }
        if (it.kind === "page") {
            if (hasWin && typeof win.go === "function")
                win.go(it.targetId);
            return;
        }
        if (it.kind === "action") {
            if (it.targetId === "new_agent") {
                if (hasWin && typeof win.newAgent === "function")
                    win.newAgent();
                else if (hasWin && typeof win.openNewAgent === "function")
                    win.openNewAgent();
            } else if (it.targetId === "invite") {
                if (hasWin && typeof win.invite === "function")
                    win.invite();
                else if (hasWin && typeof win.go === "function")
                    win.go("pessoas:convidar");
            } else if (it.targetId === "connect_ai") {
                openConnectAI();
            } else if (it.targetId === "new_channel") {
                if (hasWin && typeof win.newChannel === "function")
                    win.newChannel();
                else
                    chanEditor.open(null, "");
            } else if (it.targetId === "new_doc") {
                if (hasWin && typeof win.newDoc === "function")
                    win.newDoc();
                else
                    createQuickDoc();
            }
        }
    }

    function triggerSelected() {
        if (selectedIndex >= 0 && selectedIndex < selectableItems.length)
            executeItem(selectableItems[selectedIndex]);
    }

    z: 55
    // Read the children's own state: their `visible` is false while this Item
    // is hidden, so binding to it would never let the menu show.
    visible: root.shown || focusScope.opacity > 0 || chanEditor.shown || chanEditor.opacity > 0

    Shortcut {
        sequence: "Esc"
        enabled: root.shown && !focusScope.activeFocus
        onActivated: root.close()
    }

    FocusScope {
        id: focusScope
        anchors.fill: parent
        visible: opacity > 0
        opacity: root.shown ? 1 : 0
        Behavior on opacity { NumberAnimation { duration: Theme.fast; easing.type: Theme.ease } }

        Item {
            id: overlay
            anchors.fill: parent
            visible: focusScope.visible

            Rectangle {
                anchors.fill: parent
                color: Theme.scrim
                MouseArea {
                    anchors.fill: parent
                    enabled: root.shown
                    onClicked: root.close()
                }
            }

            Rectangle {
                id: box
                readonly property real maxListHeight: Math.max(220, Math.min(420, root.height - y - 96))
                width: Math.min(580, parent.width - 2 * Theme.space5)
                height: topBar.height + divider.height + listFlick.height + footerBar.height
                anchors.horizontalCenter: parent.horizontalCenter
                y: Math.max(Theme.space6, Math.round(parent.height * 0.14))
                radius: Theme.radiusLg
                color: Theme.card
                border.width: 1
                border.color: Theme.lineStrong

                transform: Translate {
                    y: root.shown ? 0 : -6
                    Behavior on y { NumberAnimation { duration: Theme.medium; easing.type: Theme.ease } }
                }

                MouseArea {
                    anchors.fill: parent
                    onClicked: searchField.forceActiveFocus()
                }

                Item {
                    id: topBar
                    width: parent.width
                    height: 54

                    Field {
                        id: searchField
                        focus: true
                        anchors {
                            fill: parent
                            margins: Theme.space2
                        }
                        leftPadding: 38
                        rightPadding: clearBtn.visible ? 36 : Theme.space3
                        placeholderText: "Ir para agente, canal, pessoa, página ou ação…"

                        Keys.onShortcutOverride: event => {
                            if (event.key === Qt.Key_Escape)
                                event.accepted = true;
                        }
                        Keys.onEscapePressed: event => {
                            event.accepted = true;
                            root.close();
                        }
                        Keys.onDownPressed: event => {
                            event.accepted = true;
                            root.moveSelection(1);
                        }
                        Keys.onUpPressed: event => {
                            event.accepted = true;
                            root.moveSelection(-1);
                        }
                        Keys.onReturnPressed: event => {
                            event.accepted = true;
                            root.triggerSelected();
                        }
                        Keys.onEnterPressed: event => {
                            event.accepted = true;
                            root.triggerSelected();
                        }

                        Icon {
                            x: Theme.space3
                            anchors.verticalCenter: parent.verticalCenter
                            name: "search"
                            size: 16
                            color: Theme.text3
                        }

                        Rectangle {
                            id: clearBtn
                            visible: searchField.text.length > 0
                            width: 22
                            height: 22
                            radius: height / 2
                            anchors {
                                right: parent.right
                                rightMargin: Theme.space2
                                verticalCenter: parent.verticalCenter
                            }
                            color: clearArea.containsMouse ? Theme.hover : "transparent"
                            Icon {
                                anchors.centerIn: parent
                                name: "close"
                                size: 12
                                color: clearArea.containsMouse ? Theme.white : Theme.text3
                            }
                            MouseArea {
                                id: clearArea
                                anchors.fill: parent
                                hoverEnabled: true
                                cursorShape: Qt.PointingHandCursor
                                onClicked: {
                                    searchField.text = "";
                                    searchField.forceActiveFocus();
                                }
                            }
                        }
                    }
                }

                Rectangle {
                    id: divider
                    y: topBar.height
                    width: parent.width
                    height: 1
                    color: Theme.line
                }

                Flickable {
                    id: listFlick
                    y: topBar.height + divider.height
                    width: parent.width
                    height: Math.min(box.maxListHeight, Math.max(64, listCol.implicitHeight + 2 * Theme.space2))
                    contentHeight: listCol.implicitHeight + 2 * Theme.space2
                    clip: true
                    boundsBehavior: Flickable.StopAtBounds
                    ScrollBar.vertical: SlimScrollBar {}
                    SmoothWheel {}

                    Column {
                        id: listCol
                        x: Theme.space2
                        y: Theme.space2
                        width: listFlick.width - 2 * Theme.space2
                        spacing: 2

                        Text {
                            visible: root.trimmedQuery !== "" && root.selectableItems.length === 0
                            width: parent.width
                            topPadding: Theme.space4
                            bottomPadding: Theme.space4
                            horizontalAlignment: Text.AlignHCenter
                            text: "Nenhum resultado para «" + searchField.text.trim() + "»."
                            color: Theme.text3
                            font.pixelSize: Theme.fontBody
                            wrapMode: Text.Wrap
                        }

                        Repeater {
                            model: root.displayRows
                            delegate: Item {
                                id: rowWrap
                                required property var modelData
                                required property int index
                                readonly property bool isHeader: !!modelData.isHeader
                                readonly property bool isHint: !!modelData.isHint
                                readonly property var entry: modelData.entry
                                readonly property bool selected: !isHeader && !isHint && modelData.itemIndex === root.selectedIndex

                                width: listCol.width
                                height: isHeader ? (index === 0 ? 24 : 30) : isHint ? 32 : 40

                                onSelectedChanged: {
                                    if (selected)
                                        root.ensureRowVisible(y, height, modelData.itemIndex === 0);
                                }

                                Overline {
                                    visible: rowWrap.isHeader
                                    anchors {
                                        left: parent.left
                                        leftMargin: Theme.space2
                                        bottom: parent.bottom
                                        bottomMargin: Theme.space1
                                    }
                                    text: rowWrap.modelData.title
                                }

                                Text {
                                    visible: rowWrap.isHint
                                    anchors {
                                        left: parent.left
                                        right: parent.right
                                        leftMargin: Theme.space2
                                        rightMargin: Theme.space2
                                        verticalCenter: parent.verticalCenter
                                    }
                                    text: rowWrap.modelData.title
                                    color: Theme.text4
                                    font.pixelSize: Theme.fontSmall
                                    elide: Text.ElideRight
                                }

                                Rectangle {
                                    visible: !rowWrap.isHeader && !rowWrap.isHint && rowWrap.entry !== null
                                    anchors.fill: parent
                                    radius: Theme.radiusMd
                                    color: rowWrap.selected ? Theme.active : "transparent"

                                    Item {
                                        id: leadBox
                                        x: Theme.space2
                                        anchors.verticalCenter: parent.verticalCenter
                                        width: 28
                                        height: 28

                                        Avatar {
                                            visible: rowWrap.entry !== null && (rowWrap.entry.kind === "agent" || rowWrap.entry.kind === "person")
                                            anchors.fill: parent
                                            size: 28
                                            name: rowWrap.entry ? rowWrap.entry.name : "?"
                                            photo: rowWrap.entry ? rowWrap.entry.photo : ""
                                            tint: rowWrap.entry && rowWrap.entry.tint ? rowWrap.entry.tint : Theme.text2
                                            working: !!(rowWrap.entry && rowWrap.entry.working)
                                        }

                                        Rectangle {
                                            visible: rowWrap.entry !== null && rowWrap.entry.kind !== "agent" && rowWrap.entry.kind !== "person"
                                            anchors.fill: parent
                                            radius: Theme.radiusSm
                                            color: rowWrap.selected ? Theme.wash4 : Theme.wash1
                                            border.width: 1
                                            border.color: Theme.line

                                            Icon {
                                                anchors.centerIn: parent
                                                name: rowWrap.entry ? rowWrap.entry.icon : "spark"
                                                size: 15
                                                color: rowWrap.selected ? Theme.white : Theme.text2
                                            }
                                        }
                                    }

                                    Row {
                                        anchors {
                                            left: leadBox.right
                                            leftMargin: Theme.space3
                                            right: enterBadge.left
                                            rightMargin: Theme.space2
                                            verticalCenter: parent.verticalCenter
                                        }
                                        spacing: Theme.space2

                                        Text {
                                            id: rowName
                                            text: rowWrap.entry ? rowWrap.entry.name : ""
                                            color: rowWrap.selected ? Theme.white : Theme.text
                                            font.pixelSize: Theme.fontBody
                                            font.weight: rowWrap.selected ? Font.DemiBold : Font.Medium
                                            width: Math.min(implicitWidth, Math.max(120, parent.width * 0.55))
                                            elide: Text.ElideRight
                                            anchors.verticalCenter: parent.verticalCenter
                                        }

                                        Text {
                                            visible: !!(rowWrap.entry && rowWrap.entry.sub)
                                            text: rowWrap.entry && rowWrap.entry.sub ? rowWrap.entry.sub : ""
                                            color: Theme.text3
                                            font.pixelSize: Theme.fontSmall
                                            width: Math.max(0, parent.width - rowName.width - Theme.space2)
                                            elide: Text.ElideRight
                                            anchors.verticalCenter: parent.verticalCenter
                                        }
                                    }

                                    Text {
                                        id: enterBadge
                                        visible: rowWrap.selected
                                        anchors {
                                            right: parent.right
                                            rightMargin: Theme.space3
                                            verticalCenter: parent.verticalCenter
                                        }
                                        text: "↵"
                                        color: Theme.text3
                                        font.family: Theme.mono
                                        font.pixelSize: Theme.fontSmall
                                    }

                                    MouseArea {
                                        anchors.fill: parent
                                        hoverEnabled: true
                                        cursorShape: Qt.PointingHandCursor
                                        onPositionChanged: mouse => {
                                            const p = mapToItem(root, mouse.x, mouse.y);
                                            if (Math.abs(p.x - root.lastMouseX) > 1 || Math.abs(p.y - root.lastMouseY) > 1) {
                                                root.lastMouseX = p.x;
                                                root.lastMouseY = p.y;
                                                root.selectedIndex = rowWrap.modelData.itemIndex;
                                            }
                                        }
                                        onClicked: root.executeItem(rowWrap.entry)
                                    }
                                }
                            }
                        }
                    }
                }

                Rectangle {
                    id: footerBar
                    y: topBar.height + divider.height + listFlick.height
                    width: parent.width
                    height: 32
                    color: "transparent"

                    Rectangle {
                        width: parent.width
                        height: 1
                        color: Theme.line
                    }

                    Row {
                        x: Theme.space3
                        anchors.verticalCenter: parent.verticalCenter
                        spacing: Theme.space4

                        Text {
                            text: "↑↓ navegar"
                            color: Theme.text4
                            font.pixelSize: Theme.fontCaption
                        }
                        Text {
                            text: "Enter abrir"
                            color: Theme.text4
                            font.pixelSize: Theme.fontCaption
                        }
                        Text {
                            text: "Esc fechar"
                            color: Theme.text4
                            font.pixelSize: Theme.fontCaption
                        }
                    }
                }
            }
        }
    }

    ChannelEditor {
        id: chanEditor
        anchors.fill: parent
        agents: root.agents
        projects: root.projects
        onSaved: c => {
            if (c && typeof win !== "undefined" && win && typeof win.openChannel === "function")
                win.openChannel(c.id);
        }
    }
}
