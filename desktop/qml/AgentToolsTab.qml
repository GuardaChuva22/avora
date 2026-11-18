import QtQuick
import QtQuick.Controls.Basic
import Avora
import "util.js" as U
import "logos.js" as Logos

// What this agent may use (connected apps, skills, built-in tools) and when it asks first.
Column {
    id: tab
    required property var page   // the AgentPage that hosts this tab
    spacing: Theme.space5
    // width is set by the page; do not set it here

    property var connectors: []
    property bool connectorsLoaded: false
    property string connectorError: ""
    property string savingKind: ""

    property var skills: []
    property bool skillsLoaded: false
    property string skillsError: ""

    property var nodeTools: []
    property var fallbackAgents: []
    property int draftTick: 0

    readonly property string agentId: page && page.agent ? page.agent.id || "" : ""
    readonly property string agentProjectId: page && page.agent ? page.agent.project_id || "" : ""
    readonly property color agentColor: {
        draftTick;
        if (page && page.draft && page.draft.color)
            return page.draft.color;
        if (page && page.agent && page.agent.color)
            return page.agent.color;
        return Theme.white;
    }
    readonly property string approvals: {
        draftTick;
        return page && page.draft && page.draft.approvals ? page.draft.approvals : "";
    }
    readonly property var teamAgents: page && page.agents && page.agents.length > 0 ? page.agents : fallbackAgents

    readonly property var connectedApps: connectors.filter(c => primaryConn(c) !== null)

    // Matches skillSeen in internal/control/skills.go: universal skills for all,
    // project skills for that project's agents and universal agents.
    readonly property var visibleSkills: skills.filter(sk => {
        const sp = sk.project_id || "";
        return sp === "" || agentProjectId === "" || agentProjectId === sp;
    })

    readonly property var nativeTools: [
        { icon: "search", title: "Pesquisar na web", desc: "Busca preços, notícias e informações atuais na internet." },
        { icon: "globe", title: "Ler sites", desc: "Abre links públicos e lê o conteúdo das páginas." },
        { icon: "docs", title: "Guardar e editar Docs", desc: "Cria relatórios, planos e páginas compartilhadas com o time." },
        { icon: "terminal", title: "Rodar comandos num sandbox", desc: "Executa scripts e cálculos isolados em ambiente seguro." },
        { icon: "bookmark", title: "Lembrar do que você conta", desc: "Guarda preferências, datas e decisões para as próximas conversas." },
        { icon: "clock", title: "Criar rotinas", desc: "Agenda tarefas recorrentes e lembretes no horário combinado." },
        { icon: "users", title: "Falar com colegas", desc: "Consulta outros agentes do time ou passa recados." },
        { icon: "bolt", title: "Delegar trabalho pesado", desc: "Aciona Claude Code, Codex ou Antigravity em tarefas longas." },
        { icon: "monitor", title: "Usar o navegador", desc: "Navega e interage com sites quando não há app conectado." }
    ]

    // Node programs installed on an online node (GET /v1/tools) join the capabilities grid.
    readonly property var activeNodeTools: nodeTools
        .filter(t => (t.nodes || []).some(n => n.online && n.has))
        .map(t => ({ icon: "plug", title: t.label, desc: capFirst(t.desc || "") }))

    readonly property var capabilities: nativeTools.concat(activeNodeTools)

    Component.onCompleted: load()

    Connections {
        target: tab.page
        function onDraftReset() {
            tab.draftTick++;
        }
    }

    function capFirst(s) {
        if (!s)
            return "";
        const out = s.charAt(0).toUpperCase() + s.slice(1);
        return out.endsWith(".") ? out : out + ".";
    }

    function load() {
        loadConnectors();
        loadSkills();
        Api.get("/v1/tools", (err, data) => {
            if (!err && data)
                tab.nodeTools = data;
        });
        if (!page || !page.agents || page.agents.length === 0) {
            Api.get("/v1/agents", (err, data) => {
                if (!err && data)
                    tab.fallbackAgents = data;
            });
        }
    }

    function loadConnectors() {
        Api.get("/v1/connectors", (err, data) => {
            tab.connectorsLoaded = true;
            if (err) {
                tab.connectorError = err;
                return;
            }
            tab.connectorError = "";
            tab.connectors = data || [];
        });
    }

    function loadSkills() {
        Api.get("/v1/skills", (err, data) => {
            tab.skillsLoaded = true;
            if (err) {
                tab.skillsError = err;
                return;
            }
            tab.skillsError = "";
            tab.skills = data || [];
        });
    }

    function primaryConn(c) {
        if (!c)
            return null;
        if (c.connections && c.connections.length > 0)
            return c.connections[0];
        return c.connection || null;
    }

    function shownConn(c) {
        const list = c && c.connections ? c.connections : [];
        const broken = list.find(x => x.status !== "ok");
        return broken || primaryConn(c);
    }

    function statusLabel(st) {
        if (st === "ok")
            return "Conectado";
        if (st === "reconnect")
            return "Reconectar";
        if (st === "error")
            return "Com erro";
        return "Conectado";
    }

    function replaceConnector(view) {
        let found = false;
        const next = tab.connectors.map(c => {
            if (c.kind !== view.kind)
                return c;
            found = true;
            return view;
        });
        if (!found)
            next.push(view);
        tab.connectors = next;
    }

    // Empty agents list on a connection means every agent may use it.
    function toggleConnector(c) {
        if (!c || c.via || tab.agentId === "" || tab.savingKind === c.kind)
            return;
        const conn = primaryConn(c);
        if (!conn)
            return;
        const allIds = tab.teamAgents.map(a => a.id).filter(Boolean);
        const raw = conn.agents || [];
        const current = allIds.length > 0 ? raw.filter(id => allIds.indexOf(id) >= 0) : raw.slice();
        const currentlyOn = raw.length === 0 || raw.indexOf(tab.agentId) >= 0;

        let next = [];
        if (currentlyOn) {
            next = raw.length === 0
                ? allIds.filter(id => id !== tab.agentId)
                : current.filter(id => id !== tab.agentId);
            if (next.length === 0) {
                tab.connectorError = "Pelo menos um agente precisa continuar com acesso; para desligar para todos, desconecte o app em Plugins.";
                return;
            }
        } else {
            next = current.concat([tab.agentId]);
            if (allIds.length > 0 && allIds.every(id => next.indexOf(id) >= 0))
                next = [];
        }

        tab.savingKind = c.kind;
        tab.connectorError = "";
        Api.put("/v1/connectors/" + encodeURIComponent(c.kind) + "/agents", { agents: next }, (err, view) => {
            tab.savingKind = "";
            if (err) {
                tab.connectorError = err;
                return;
            }
            if (view)
                tab.replaceConnector(view);
            else
                tab.loadConnectors();
            if (tab.page && tab.page.reload)
                tab.page.reload();
        });
    }

    component AppBadge: Item {
        property var c
        property real side: 34
        width: side
        height: side
        readonly property string picture: c ? Logos.image(c.kind) : ""
        readonly property string logo: c && picture === "" ? Logos.of(c.kind) : ""
        readonly property string icon: c && picture === "" && logo === "" ? U.connectorIcon(c.kind) : ""
        readonly property color tint: c && Logos.color(c.kind) !== "" ? Logos.color(c.kind) : Theme.text

        Image {
            visible: parent.picture !== ""
            anchors.centerIn: parent
            source: parent.picture
            width: parent.side * 0.72
            height: width
            sourceSize: Qt.size(width * 2, height * 2)
            fillMode: Image.PreserveAspectFit
            smooth: true
            mipmap: true
        }
        Icon {
            visible: parent.picture === "" && (parent.logo !== "" || parent.icon !== "")
            anchors.centerIn: parent
            name: parent.icon
            path: parent.logo
            size: parent.side * (parent.logo !== "" ? 0.68 : 0.62)
            color: parent.tint
        }
        Text {
            visible: parent.picture === "" && parent.logo === "" && parent.icon === ""
            anchors.centerIn: parent
            text: parent.c && parent.c.name ? parent.c.name.charAt(0).toUpperCase() : ""
            color: parent.tint
            font.family: Theme.display
            font.pixelSize: Theme.fontTitle
            font.weight: Font.Bold
        }
    }

    component SectionLink: Item {
        id: linkItem
        property string text: ""
        signal triggered
        implicitWidth: linkRow.implicitWidth
        implicitHeight: linkRow.implicitHeight

        Row {
            id: linkRow
            spacing: Theme.space1
            Text {
                text: linkItem.text
                color: linkMouse.containsMouse ? Theme.white : Theme.text2
                font.pixelSize: Theme.fontSmall
                font.weight: Font.Medium
                font.underline: linkMouse.containsMouse
                anchors.verticalCenter: parent.verticalCenter
                Behavior on color { ColorAnimation { duration: Theme.fast } }
            }
            Icon {
                name: "chevron"
                size: 13
                color: linkMouse.containsMouse ? Theme.white : Theme.text3
                anchors.verticalCenter: parent.verticalCenter
            }
        }
        MouseArea {
            id: linkMouse
            anchors.fill: parent
            anchors.margins: -Theme.space1
            hoverEnabled: true
            cursorShape: Qt.PointingHandCursor
            onClicked: linkItem.triggered()
        }
    }

    // ── 1. Approvals & standing grants ──────────────────────────
    Column {
        width: parent.width
        spacing: Theme.space3

        Overline { text: "APROVAÇÕES" }

        Segmented {
            value: tab.approvals
            options: [
                { value: "always", label: "Sempre pergunta" },
                { value: "", label: "Só o arriscado" },
                { value: "never", label: "Age sozinho" }
            ]
            onActivated: v => {
                if (tab.page) {
                    tab.page.set("approvals", v);
                    tab.draftTick++;
                }
            }
        }

        Text {
            width: parent.width
            wrapMode: Text.Wrap
            text: tab.approvals === "always" ? "Pede a sua aprovação antes de qualquer coisa que mude algo: criar rotinas, escrever nos seus apps e arquivos, rodar comandos."
                  : tab.approvals === "never" ? "Faz sem perguntar, inclusive enviar e apagar. Pagamentos e enviar ou comprar pelo navegador ainda pedem a sua aprovação."
                  : "Pergunta só antes de enviar, apagar, pagar ou editar arquivos. O resto ele faz e conta."
            color: tab.approvals === "never" ? Theme.warn : Theme.text4
            font.pixelSize: Theme.fontSmall
        }

        Rectangle {
            width: parent.width
            height: grantsInner.implicitHeight + Theme.space3 * 2
            radius: Theme.radiusMd
            color: Theme.wash1
            border.width: 1
            border.color: Theme.line

            Column {
                id: grantsInner
                x: Theme.space3
                y: Theme.space3
                width: parent.width - Theme.space3 * 2
                spacing: Theme.space2

                AgentGrants {
                    id: grantsPanel
                    agent: tab.page ? tab.page.agent : null
                    width: parent.width
                }

                Column {
                    visible: !grantsPanel.visible
                    width: parent.width
                    spacing: Theme.space2
                    Overline { text: "SEMPRE PERMITIDO" }
                    Text {
                        width: parent.width
                        wrapMode: Text.Wrap
                        text: "Quando você responder \"Sempre permitir\" num pedido dele, a permissão aparece aqui e dá para tirar."
                        color: Theme.text4
                        font.pixelSize: Theme.fontSmall
                    }
                }
            }
        }
    }

    // ── 2. Connected apps ───────────────────────────────────────
    Column {
        width: parent.width
        spacing: Theme.space3

        Overline { text: "APPS CONECTADOS" }

        ErrorBanner {
            width: parent.width
            text: tab.connectorError
        }

        Text {
            visible: !tab.connectorsLoaded && tab.connectorError === ""
            width: parent.width
            wrapMode: Text.Wrap
            text: "Carregando apps conectados…"
            color: Theme.text4
            font.pixelSize: Theme.fontSmall
        }

        Text {
            visible: tab.connectorsLoaded && tab.connectedApps.length === 0
            width: parent.width
            wrapMode: Text.Wrap
            text: "Nenhum app conectado ainda. Conecte Gmail, Agenda, ClickUp e outros para ele usar."
            color: Theme.text4
            font.pixelSize: Theme.fontSmall
        }

        Column {
            visible: tab.connectedApps.length > 0
            width: parent.width
            spacing: Theme.space2

            Repeater {
                model: tab.connectedApps
                delegate: Rectangle {
                    id: appRow
                    required property var modelData
                    readonly property var conn: tab.primaryConn(modelData)
                    readonly property var shown: tab.shownConn(modelData)
                    readonly property var conns: modelData.connections || (conn ? [conn] : [])
                    readonly property string status: shown ? shown.status || "ok" : "ok"
                    readonly property var agentsList: conn && conn.agents ? conn.agents : []
                    readonly property bool allAgents: agentsList.length === 0
                    readonly property bool canUse: allAgents || agentsList.indexOf(tab.agentId) >= 0
                    readonly property bool viaOnly: !!modelData.via
                    readonly property bool saving: tab.savingKind === modelData.kind
                    readonly property string accountNote: conns.length > 1
                        ? conns.length + " contas"
                        : (conn && conn.account ? conn.account : "")

                    width: parent.width
                    height: Math.max(58, midCol.implicitHeight + Theme.space3 * 2)
                    radius: Theme.radiusMd
                    color: Theme.wash1
                    border.width: 1
                    border.color: Theme.line

                    AppBadge {
                        id: appBadge
                        c: appRow.modelData
                        x: Theme.space3
                        anchors.verticalCenter: parent.verticalCenter
                    }

                    Column {
                        id: midCol
                        anchors {
                            left: appBadge.right
                            leftMargin: Theme.space3
                            right: rightSide.left
                            rightMargin: Theme.space3
                            verticalCenter: parent.verticalCenter
                        }
                        spacing: Theme.space1

                        Text {
                            width: parent.width
                            text: appRow.modelData.name || appRow.modelData.kind
                            color: Theme.white
                            font.pixelSize: Theme.fontBody
                            font.weight: Font.Medium
                            elide: Text.ElideRight
                        }

                        Row {
                            spacing: Theme.space2
                            Dot {
                                id: stDot
                                kind: appRow.status === "ok" ? "live" : appRow.status === "reconnect" ? "bad" : appRow.status === "error" ? "warn" : ""
                                anchors.verticalCenter: parent.verticalCenter
                            }
                            Text {
                                width: Math.max(0, midCol.width - stDot.width - Theme.space2)
                                text: tab.statusLabel(appRow.status) + (appRow.accountNote !== "" ? "  ·  " + appRow.accountNote : "")
                                color: appRow.status === "reconnect" ? Theme.bad : appRow.status === "error" ? Theme.warn : Theme.text3
                                font.pixelSize: Theme.fontSmall
                                elide: Text.ElideRight
                                anchors.verticalCenter: parent.verticalCenter
                            }
                        }
                    }

                    Item {
                        id: rightSide
                        anchors {
                            right: parent.right
                            rightMargin: Theme.space3
                            verticalCenter: parent.verticalCenter
                        }
                        implicitWidth: appRow.viaOnly ? viaLabel.implicitWidth : accessRow.implicitWidth
                        implicitHeight: appRow.viaOnly ? viaLabel.implicitHeight : accessRow.implicitHeight

                        Text {
                            id: viaLabel
                            visible: appRow.viaOnly
                            text: "Usado só pelo Avora"
                            color: Theme.text4
                            font.pixelSize: Theme.fontSmall
                        }

                        Row {
                            id: accessRow
                            visible: !appRow.viaOnly
                            spacing: Theme.space3

                            Text {
                                text: appRow.allAgents ? "Todos os agentes" : "Só alguns"
                                color: Theme.text4
                                font.pixelSize: Theme.fontSmall
                                anchors.verticalCenter: parent.verticalCenter
                            }

                            Item {
                                implicitWidth: toggleGroup.implicitWidth
                                implicitHeight: Math.max(toggleTrack.height, toggleText.implicitHeight)
                                anchors.verticalCenter: parent.verticalCenter

                                Row {
                                    id: toggleGroup
                                    spacing: Theme.space2
                                    anchors.verticalCenter: parent.verticalCenter

                                    Text {
                                        id: toggleText
                                        text: "Pode usar"
                                        color: appRow.canUse ? Theme.text2 : Theme.text3
                                        font.pixelSize: Theme.fontSmall
                                        anchors.verticalCenter: parent.verticalCenter
                                    }

                                    Rectangle {
                                        id: toggleTrack
                                        width: 36
                                        height: 20
                                        radius: height / 2
                                        color: appRow.canUse ? tab.agentColor : Theme.wash4
                                        border.width: 1
                                        border.color: appRow.canUse ? tab.agentColor : Theme.line
                                        opacity: appRow.saving ? 0.5 : 1
                                        anchors.verticalCenter: parent.verticalCenter
                                        Behavior on color { ColorAnimation { duration: Theme.fast } }
                                        Behavior on border.color { ColorAnimation { duration: Theme.fast } }

                                        Rectangle {
                                            width: 14
                                            height: 14
                                            radius: height / 2
                                            y: 3
                                            x: appRow.canUse ? parent.width - width - 3 : 3
                                            color: appRow.canUse ? Theme.ink : Theme.text3
                                            Behavior on x { NumberAnimation { duration: Theme.fast; easing.type: Theme.ease } }
                                            Behavior on color { ColorAnimation { duration: Theme.fast } }
                                        }
                                    }
                                }

                                MouseArea {
                                    anchors.fill: parent
                                    anchors.margins: -Theme.space1
                                    enabled: !appRow.saving
                                    cursorShape: Qt.PointingHandCursor
                                    onClicked: tab.toggleConnector(appRow.modelData)
                                }
                            }
                        }
                    }
                }
            }
        }

        SectionLink {
            text: "Conectar mais apps"
            onTriggered: if (tab.page) tab.page.openPlugins()
        }
    }

    // ── 3. Skills ───────────────────────────────────────────────
    Column {
        width: parent.width
        spacing: Theme.space3

        Overline { text: "HABILIDADES" }

        ErrorBanner {
            width: parent.width
            text: tab.skillsError
        }

        Text {
            visible: !tab.skillsLoaded && tab.skillsError === ""
            width: parent.width
            wrapMode: Text.Wrap
            text: "Carregando habilidades…"
            color: Theme.text4
            font.pixelSize: Theme.fontSmall
        }

        Text {
            visible: tab.skillsLoaded && tab.visibleSkills.length === 0
            width: parent.width
            wrapMode: Text.Wrap
            text: "Nenhuma habilidade cadastrada para ele ainda. Ensine um passo a passo no chat ou crie uma na tela de habilidades."
            color: Theme.text4
            font.pixelSize: Theme.fontSmall
        }

        Column {
            visible: tab.visibleSkills.length > 0
            width: parent.width
            spacing: Theme.space2

            Repeater {
                model: tab.visibleSkills
                delegate: Rectangle {
                    id: skRow
                    required property var modelData
                    width: parent.width
                    height: Math.max(54, skCol.implicitHeight + Theme.space3 * 2)
                    radius: Theme.radiusMd
                    color: Theme.wash1
                    border.width: 1
                    border.color: Theme.line

                    Rectangle {
                        id: skIcon
                        x: Theme.space3
                        anchors.verticalCenter: parent.verticalCenter
                        width: 32
                        height: 32
                        radius: Theme.radiusSm
                        color: Theme.hover
                        border.width: 1
                        border.color: Theme.line
                        Icon {
                            anchors.centerIn: parent
                            name: "book"
                            size: 15
                            color: Theme.text2
                        }
                    }

                    Column {
                        id: skCol
                        anchors {
                            left: skIcon.right
                            leftMargin: Theme.space3
                            right: parent.right
                            rightMargin: Theme.space3
                            verticalCenter: parent.verticalCenter
                        }
                        spacing: Theme.space1

                        Text {
                            width: parent.width
                            text: skRow.modelData.name || ""
                            color: Theme.white
                            font.family: Theme.mono
                            font.pixelSize: Theme.fontBody
                            elide: Text.ElideRight
                        }

                        Text {
                            width: parent.width
                            text: skRow.modelData.description || ""
                            color: Theme.text3
                            font.pixelSize: Theme.fontSmall
                            wrapMode: Text.Wrap
                            maximumLineCount: 2
                            elide: Text.ElideRight
                        }
                    }
                }
            }
        }

        SectionLink {
            text: "Gerenciar habilidades"
            onTriggered: if (tab.page) tab.page.openSkills()
        }
    }

    // ── 4. Built-in capabilities ────────────────────────────────
    Column {
        width: parent.width
        spacing: Theme.space3

        Overline { text: "O QUE ELE SABE FAZER" }

        Flow {
            id: capGrid
            width: parent.width
            spacing: Theme.space3
            readonly property int cols: width < 460 ? 1 : 2
            readonly property real cellWidth: cols === 1 ? width : Math.floor((width - spacing) / 2)

            Repeater {
                model: tab.capabilities
                delegate: Rectangle {
                    id: capCard
                    required property var modelData
                    width: capGrid.cellWidth
                    height: Math.max(60, capText.implicitHeight + Theme.space3 * 2)
                    radius: Theme.radiusMd
                    color: Theme.wash1
                    border.width: 1
                    border.color: Theme.line

                    Rectangle {
                        id: capBadge
                        x: Theme.space3
                        anchors.verticalCenter: parent.verticalCenter
                        width: 32
                        height: 32
                        radius: Theme.radiusSm
                        color: Theme.hover
                        border.width: 1
                        border.color: Theme.line
                        Icon {
                            anchors.centerIn: parent
                            name: capCard.modelData.icon
                            size: 15
                            color: Theme.text2
                        }
                    }

                    Column {
                        id: capText
                        anchors {
                            left: capBadge.right
                            leftMargin: Theme.space3
                            right: parent.right
                            rightMargin: Theme.space3
                            verticalCenter: parent.verticalCenter
                        }
                        spacing: Theme.space1

                        Text {
                            width: parent.width
                            text: capCard.modelData.title
                            color: Theme.white
                            font.pixelSize: Theme.fontBody
                            font.weight: Font.Medium
                            elide: Text.ElideRight
                        }

                        Text {
                            width: parent.width
                            text: capCard.modelData.desc
                            color: Theme.text3
                            font.pixelSize: Theme.fontSmall
                            wrapMode: Text.Wrap
                            maximumLineCount: 2
                            elide: Text.ElideRight
                        }
                    }
                }
            }
        }
    }
}
