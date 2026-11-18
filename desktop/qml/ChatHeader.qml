import QtQuick
import QtQuick.Controls.Basic
import Avora
import "util.js" as U

// Top bar of the open conversation: agent identity + model + Conversations menu
// + right column toggle, or channel #name + topic + pinned + settings + members + search.
Rectangle {
    id: header
    property string kind: "agent" // "agent" | "channel" | "person"

    // Agent mode
    property var agent: null
    property var agents: []
    property var projects: []
    property string convId: ""
    property int msgsCount: 0
    property bool working: false
    property string workStep: ""
    property int pendingCount: 0
    property var conversations: []
    property var liveMap: ({})
    property bool showArchived: false
    property string confirmDelete: ""

    // Channel mode
    property string channelId: "time"
    property string channelName: "time"
    property string channelPurpose: ""
    property int channelMemberCount: 0
    property bool channelEveryone: false
    property bool channelBusy: false
    property bool channelRouting: false
    property string channelWorkingText: ""
    property int channelStreak: 0
    property int channelLimit: 20
    property bool channelLocked: false
    property var channelPinnedDocs: []

    // Person DM mode
    property var person: null

    // Right column state
    property bool rightOpen: false

    signal pickModelRequested
    signal selectConversationRequested(string id)
    signal newConversationRequested
    signal toggleArchivedRequested
    signal archiveConversationRequested(string id, bool archived)
    signal deleteConversationRequested(string id)
    signal toggleRightRequested
    signal channelSettingsRequested
    signal stopChannelRequested
    signal continueChannelRequested
    signal openDocRequested(string docId)

    height: 107
    color: Theme.bg

    readonly property color agentTint: agent && agent.color ? agent.color : Theme.text2
    readonly property string agentStatusLine: {
        const a = header.agent;
        if (!a)
            return "";
        if (header.pendingCount > 0)
            return "Esperando sua permissão";
        if (header.working && header.workStep !== "")
            return U.stepLabel(header.workStep.split(" · ")[0]);
        if (header.working)
            return "Trabalhando nesta conversa…";
        if (a.working && a.status_channel)
            return "Trabalhando em #" + String(a.status_channel).replace(/^#/, "");
        if (a.working)
            return "Trabalhando…";
        if (a.status_text)
            return a.status_text;
        return a.role || "Livre";
    }
    readonly property color agentStatusColor: {
        if (header.pendingCount > 0)
            return Theme.warn;
        if (header.working || (header.agent && header.agent.working))
            return Qt.lighter(header.agentTint, 1.1);
        return Theme.text3;
    }
    readonly property string modelLabel: {
        const b = header.agent && header.agent.brain ? header.agent.brain : null;
        if (b && b.provider)
            return b.model || b.provider;
        return "Automático";
    }

    function openConversations() {
        if (kind === "agent" && agent !== null)
            convsMenu.toggleAt(convsBtn);
    }

    function openAgentFloat() {
        if (!agent || !agent.id)
            return;
        if (typeof win !== "undefined" && win && typeof win.openFloat === "function") {
            win.openFloat("AgentPage.qml", {
                agentId: agent.id,
                agents: header.agents,
                projects: header.projects,
                shown: true,
                tab: "overview"
            });
        }
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

    function personDotKind(p) {
        if (!p)
            return "";
        if (p.presence === "away")
            return "warn";
        if (p.online)
            return p.presence === "busy" ? "bad" : "live";
        return "";
    }

    function personStatusLine(p) {
        if (!p)
            return "";
        if (p.status_text)
            return p.status_text;
        if (p.presence === "away")
            return "Ausente";
        if (p.presence === "busy")
            return "Ocupado";
        return p.online ? "Online" : "Offline";
    }

    // Channel search state
    property var chanMessages: []
    property bool loadingChanMessages: false

    function loadChannelMessages() {
        if (!Api.signedIn || channelId === "")
            return;
        const asked = channelId;
        loadingChanMessages = true;
        Api.get("/v1/conversations/" + encodeURIComponent(asked) + "/messages", (err, list) => {
            if (asked !== header.channelId)
                return;
            header.loadingChanMessages = false;
            if (!err && Array.isArray(list))
                header.chanMessages = list.filter(m => m.role !== "system" && (m.content || "") !== "").reverse();
        });
    }

    function fold(t) {
        return String(t || "").toLowerCase().normalize("NFD").replace(/[\u0300-\u036f]/g, "");
    }

    function authorNameOf(m) {
        if (!m || m.role === "user")
            return "Você";
        const a = (header.agents || []).find(x => x.id === m.agent_id);
        return a ? a.name : (m.agent_id || "Agente");
    }

    onChannelIdChanged: {
        chanMessages = [];
        if (chanSearchPopup.opened)
            chanSearchPopup.close();
        if (pinnedPopup.opened)
            pinnedPopup.close();
    }

    // Block clicks from reaching ChannelView's legacy header below.
    MouseArea {
        anchors.fill: parent
    }

    // ── Agent header ────────────────────────────────────────────
    Item {
        visible: header.kind === "agent" && header.agent !== null
        anchors {
            top: parent.top
            topMargin: Theme.titleBar + Theme.space1
            left: parent.left
            right: parent.right
            leftMargin: Theme.space5
            rightMargin: Theme.space4
        }
        height: 48

        Rectangle {
            id: agentIdentity
            anchors {
                left: parent.left
                top: parent.top
                bottom: parent.bottom
            }
            width: Math.min(
                Math.max(120, parent.width - agentActions.width - Theme.space3),
                agentFace.width + Theme.space3 + Math.max(agentNameText.implicitWidth, agentSubText.implicitWidth) + 2 * Theme.space2
            )
            radius: Theme.radiusMd
            color: identityArea.pressed ? Theme.active : identityArea.containsMouse ? Theme.hover : "transparent"
            Behavior on color { ColorAnimation { duration: Theme.fast } }

            Avatar {
                id: agentFace
                x: Theme.space1
                anchors.verticalCenter: parent.verticalCenter
                size: 40
                name: header.agent ? header.agent.name : ""
                photo: U.photo(header.agent)
                tint: header.agentTint
                working: header.working || !!(header.agent && header.agent.working)
            }

            Column {
                anchors {
                    left: agentFace.right
                    leftMargin: Theme.space3
                    right: parent.right
                    rightMargin: Theme.space2
                    verticalCenter: parent.verticalCenter
                }
                spacing: 2

                Text {
                    id: agentNameText
                    width: parent.width
                    text: header.agent ? header.agent.name : ""
                    color: Theme.white
                    font.pixelSize: Theme.fontTitle
                    font.weight: Font.DemiBold
                    elide: Text.ElideRight
                }

                Text {
                    id: agentSubText
                    width: parent.width
                    text: header.agentStatusLine
                    color: header.agentStatusColor
                    font.pixelSize: Theme.fontSmall
                    elide: Text.ElideRight
                }
            }

            MouseArea {
                id: identityArea
                anchors.fill: parent
                hoverEnabled: true
                cursorShape: Qt.PointingHandCursor
                onClicked: header.openAgentFloat()
            }

            Tip {
                text: "Abrir página do agente"
                show: identityArea.containsMouse
            }
        }

        Row {
            id: agentActions
            anchors { right: parent.right; verticalCenter: parent.verticalCenter }
            spacing: Theme.space2

            ContextMeter {
                id: ctxMeter
                visible: ctxMeter.view !== null && header.width >= 680
                anchors.verticalCenter: parent.verticalCenter
                convId: header.convId
                refreshKey: header.msgsCount
            }

            GhostButton {
                id: modelBtn
                small: true
                icon: "spark"
                text: header.modelLabel
                anchors.verticalCenter: parent.verticalCenter
                onClicked: header.pickModelRequested()
            }

            GhostButton {
                id: convsBtn
                small: true
                icon: "history"
                text: "Conversas"
                anchors.verticalCenter: parent.verticalCenter
                color: convsMenu.opened ? Theme.active : (convsBtn.hovered ? Theme.hover : "transparent")
                onClicked: convsMenu.toggleAt(convsBtn)
            }

            GhostButton {
                id: agentSideBtn
                small: true
                round: true
                icon: "sidebar"
                anchors.verticalCenter: parent.verticalCenter
                color: header.rightOpen ? Theme.active : (agentSideBtn.hovered ? Theme.hover : "transparent")
                onClicked: header.toggleRightRequested()
                Tip {
                    x: -width - Theme.space2
                    text: header.rightOpen ? "Esconder coluna" : "Mostrar agente"
                    show: agentSideBtn.hovered
                }
            }
        }
    }

    ConversationsMenu {
        id: convsMenu
        conversations: header.conversations
        currentId: header.convId
        liveMap: header.liveMap
        showArchived: header.showArchived
        confirmDelete: header.confirmDelete
        tint: header.agentTint
        onSelectRequested: id => header.selectConversationRequested(id)
        onNewRequested: header.newConversationRequested()
        onToggleArchivedRequested: header.toggleArchivedRequested()
        onArchiveRequested: (id, archived) => header.archiveConversationRequested(id, archived)
        onDeleteRequested: id => header.deleteConversationRequested(id)
    }

    // ── Channel header ──────────────────────────────────────────
    Item {
        visible: header.kind === "channel"
        anchors {
            top: parent.top
            topMargin: Theme.titleBar + Theme.space1
            left: parent.left
            right: parent.right
            leftMargin: Theme.space5
            rightMargin: Theme.space4
        }
        height: 48

        Column {
            anchors {
                left: parent.left
                right: chanActions.left
                rightMargin: Theme.space3
                verticalCenter: parent.verticalCenter
            }
            spacing: 2

            Row {
                width: parent.width
                spacing: Theme.space2

                Icon {
                    anchors.verticalCenter: parent.verticalCenter
                    name: "hash"
                    size: 18
                    color: Theme.text2
                }

                Text {
                    text: header.channelName || header.channelId
                    color: Theme.white
                    font.pixelSize: Theme.fontTitle
                    font.weight: Font.DemiBold
                    elide: Text.ElideRight
                    width: Math.min(implicitWidth, Math.max(80, parent.width - 110))
                }

                Rectangle {
                    visible: header.channelStreak > 0 || header.channelLocked
                    anchors.verticalCenter: parent.verticalCenter
                    height: 22
                    width: lockRow.implicitWidth + 16
                    radius: height / 2
                    color: header.channelLocked ? Theme.wash3 : Theme.wash1
                    border.width: 1
                    border.color: header.channelLocked ? Theme.warn : Theme.line

                    Row {
                        id: lockRow
                        anchors.centerIn: parent
                        spacing: 5
                        Icon {
                            name: "lock"
                            size: 11
                            color: header.channelLocked ? Theme.warn : Theme.text3
                            anchors.verticalCenter: parent.verticalCenter
                        }
                        Text {
                            text: header.channelLocked ? "travado" : (header.channelStreak + "/" + header.channelLimit)
                            color: header.channelLocked ? Theme.warn : Theme.text3
                            font.family: Theme.mono
                            font.pixelSize: Theme.fontCaption
                            anchors.verticalCenter: parent.verticalCenter
                        }
                    }
                }
            }

            Text {
                width: parent.width
                text: header.channelBusy && header.channelWorkingText !== ""
                      ? header.channelWorkingText
                      : header.channelRouting
                        ? "Vendo quem responde…"
                        : header.channelPurpose !== ""
                          ? header.channelPurpose
                          : header.channelEveryone
                            ? "Você e o time inteiro  ·  @Nome chama alguém"
                            : "Você e " + header.channelMemberCount + (header.channelMemberCount === 1 ? " agente" : " agentes") + "  ·  @Nome chama alguém"
                color: header.channelBusy || header.channelRouting ? Theme.text2 : Theme.text3
                font.pixelSize: Theme.fontSmall
                elide: Text.ElideRight
            }
        }

        Row {
            id: chanActions
            anchors { right: parent.right; verticalCenter: parent.verticalCenter }
            spacing: Theme.space2

            GhostButton {
                visible: header.channelLocked && !header.channelBusy
                small: true
                icon: "send"
                text: "Continuar"
                anchors.verticalCenter: parent.verticalCenter
                onClicked: header.continueChannelRequested()
            }

            GhostButton {
                visible: header.channelBusy
                small: true
                danger: true
                icon: "stop"
                text: "Parar"
                anchors.verticalCenter: parent.verticalCenter
                onClicked: header.stopChannelRequested()
            }

            GhostButton {
                id: pinBtn
                visible: header.channelPinnedDocs.length > 0
                small: true
                round: true
                icon: "pin"
                anchors.verticalCenter: parent.verticalCenter
                color: pinnedPopup.opened ? Theme.active : (pinBtn.hovered ? Theme.hover : "transparent")
                onClicked: {
                    if (pinnedPopup.opened) {
                        pinnedPopup.close();
                    } else {
                        pinnedPopup.parent = pinBtn;
                        pinnedPopup.x = Math.round(pinBtn.width - pinnedPopup.width);
                        pinnedPopup.y = pinBtn.height + Theme.space2;
                        pinnedPopup.open();
                    }
                }
                Tip {
                    x: -width - Theme.space2
                    text: "Fixados (" + header.channelPinnedDocs.length + ")"
                    show: pinBtn.hovered && !pinnedPopup.opened
                }
            }

            GhostButton {
                id: chanGearBtn
                small: true
                round: true
                icon: "gear"
                anchors.verticalCenter: parent.verticalCenter
                onClicked: header.channelSettingsRequested()
                Tip {
                    x: -width - Theme.space2
                    text: "Configurações do canal"
                    show: chanGearBtn.hovered
                }
            }

            GhostButton {
                id: chanMembersBtn
                small: true
                round: true
                icon: "users"
                anchors.verticalCenter: parent.verticalCenter
                color: header.rightOpen ? Theme.active : (chanMembersBtn.hovered ? Theme.hover : "transparent")
                onClicked: header.toggleRightRequested()
                Tip {
                    x: -width - Theme.space2
                    text: header.rightOpen ? "Esconder membros" : "Membros do canal"
                    show: chanMembersBtn.hovered
                }
            }

            GhostButton {
                id: chanSearchBtn
                small: true
                round: true
                icon: "search"
                anchors.verticalCenter: parent.verticalCenter
                color: chanSearchPopup.opened ? Theme.active : (chanSearchBtn.hovered ? Theme.hover : "transparent")
                onClicked: {
                    if (chanSearchPopup.opened) {
                        chanSearchPopup.close();
                    } else {
                        chanSearchPopup.parent = chanSearchBtn;
                        chanSearchPopup.x = Math.round(chanSearchBtn.width - chanSearchPopup.width);
                        chanSearchPopup.y = chanSearchBtn.height + Theme.space2;
                        chanSearchField.text = "";
                        header.loadChannelMessages();
                        chanSearchPopup.open();
                        Qt.callLater(() => chanSearchField.forceActiveFocus());
                    }
                }
                Tip {
                    x: -width - Theme.space2
                    text: "Buscar no canal"
                    show: chanSearchBtn.hovered && !chanSearchPopup.opened
                }
            }
        }
    }

    // Pinned items popup (shown when channelPinnedDocs exists)
    Popup {
        id: pinnedPopup
        width: 340
        padding: Theme.space3
        closePolicy: Popup.CloseOnEscape | Popup.CloseOnPressOutside
        background: Rectangle {
            radius: Theme.radiusLg
            color: Theme.card
            border.width: 1
            border.color: Theme.lineStrong
        }
        contentItem: Column {
            spacing: Theme.space2
            Overline {
                leftPadding: Theme.space1
                text: "FIXADOS  ·  " + header.channelPinnedDocs.length
            }
            Repeater {
                model: header.channelPinnedDocs
                delegate: DocRow {
                    required property var modelData
                    width: 340 - 2 * Theme.space3
                    dense: true
                    doc: modelData
                    agents: header.agents
                    onOpened: {
                        pinnedPopup.close();
                        header.openDocRequested(modelData.id);
                    }
                }
            }
        }
    }

    // Channel search popup
    Popup {
        id: chanSearchPopup
        width: 380
        padding: Theme.space3
        closePolicy: Popup.CloseOnEscape | Popup.CloseOnPressOutside

        readonly property string q: header.fold(chanSearchField.text.trim())
        readonly property var hits: {
            const query = chanSearchPopup.q;
            const list = header.chanMessages || [];
            if (query === "")
                return list.slice(0, 20);
            return list.filter(m => header.fold(header.authorNameOf(m) + " " + (m.content || "")).indexOf(query) >= 0).slice(0, 40);
        }

        enter: Transition {
            ParallelAnimation {
                NumberAnimation { property: "opacity"; from: 0; to: 1; duration: Theme.medium; easing.type: Theme.ease }
                NumberAnimation { property: "scale"; from: 0.97; to: 1; duration: Theme.medium; easing.type: Theme.ease }
            }
        }
        exit: Transition {
            NumberAnimation { property: "opacity"; from: 1; to: 0; duration: Theme.fast; easing.type: Easing.InQuad }
        }
        background: Rectangle {
            radius: Theme.radiusLg
            color: Theme.card
            border.width: 1
            border.color: Theme.lineStrong
        }

        contentItem: Column {
            spacing: Theme.space2

            Overline {
                leftPadding: Theme.space1
                text: "BUSCAR EM #" + (header.channelName || header.channelId)
            }

            Field {
                id: chanSearchField
                width: parent.width
                implicitHeight: 38
                leftPadding: 34
                placeholderText: "Buscar mensagens ou autor…"
                Keys.onEscapePressed: event => {
                    if (text !== "") {
                        text = "";
                        event.accepted = true;
                    } else {
                        chanSearchPopup.close();
                        event.accepted = true;
                    }
                }
                Icon {
                    x: Theme.space3
                    anchors.verticalCenter: parent.verticalCenter
                    name: "search"
                    size: 14
                    color: Theme.text4
                }
            }

            ListView {
                id: hitList
                visible: chanSearchPopup.hits.length > 0
                width: parent.width
                height: Math.min(contentHeight, 320)
                clip: true
                spacing: 4
                model: chanSearchPopup.hits
                boundsBehavior: Flickable.StopAtBounds
                ScrollBar.vertical: SlimScrollBar {}
                SmoothWheel {}

                delegate: Rectangle {
                    id: hitRow
                    required property var modelData
                    readonly property string whoName: header.authorNameOf(modelData)
                    readonly property var whoAgent: (header.agents || []).find(x => x.id === modelData.agent_id) || null
                    width: hitList.width
                    height: hitCol.implicitHeight + 2 * Theme.space2
                    radius: Theme.radiusMd
                    color: hitArea.containsMouse ? Theme.hover : Theme.wash1
                    border.width: 1
                    border.color: Theme.line
                    Behavior on color { ColorAnimation { duration: Theme.fast } }

                    Column {
                        id: hitCol
                        x: Theme.space3
                        y: Theme.space2
                        width: parent.width - 2 * Theme.space3
                        spacing: 3

                        Item {
                            width: parent.width
                            height: 18
                            Text {
                                anchors { left: parent.left; right: hitTime.left; rightMargin: Theme.space2; verticalCenter: parent.verticalCenter }
                                text: hitRow.whoName
                                color: hitRow.whoAgent && hitRow.whoAgent.color ? hitRow.whoAgent.color : Theme.white
                                font.pixelSize: Theme.fontSmall
                                font.weight: Font.DemiBold
                                elide: Text.ElideRight
                            }
                            Text {
                                id: hitTime
                                anchors { right: parent.right; verticalCenter: parent.verticalCenter }
                                text: U.ago(hitRow.modelData.created_at)
                                color: Theme.text4
                                font.pixelSize: Theme.fontCaption
                            }
                        }

                        Text {
                            width: parent.width
                            text: String(hitRow.modelData.content || "").replace(/\s+/g, " ").trim()
                            color: Theme.text2
                            font.pixelSize: Theme.fontSmall
                            wrapMode: Text.Wrap
                            maximumLineCount: 3
                            elide: Text.ElideRight
                        }
                    }

                    MouseArea {
                        id: hitArea
                        anchors.fill: parent
                        hoverEnabled: true
                        cursorShape: Qt.PointingHandCursor
                        onClicked: {
                            Api.copyText(hitRow.modelData.content || "");
                            chanSearchPopup.close();
                        }
                    }
                }
            }

            Text {
                visible: chanSearchPopup.hits.length === 0
                width: parent.width
                topPadding: Theme.space3
                bottomPadding: Theme.space3
                horizontalAlignment: Text.AlignHCenter
                wrapMode: Text.Wrap
                text: header.loadingChanMessages
                      ? "Carregando mensagens…"
                      : chanSearchPopup.q !== ""
                        ? "Nenhuma mensagem encontrada para «" + chanSearchField.text.trim() + "»."
                        : "Ainda não há mensagens neste canal."
                color: Theme.text3
                font.pixelSize: Theme.fontSmall
            }
        }
    }

    // ── Person DM header ────────────────────────────────────────
    Item {
        visible: header.kind === "person"
        anchors {
            top: parent.top
            topMargin: Theme.titleBar + Theme.space1
            left: parent.left
            right: parent.right
            leftMargin: Theme.space5
            rightMargin: Theme.space4
        }
        height: 48

        Avatar {
            id: dmFace
            anchors.verticalCenter: parent.verticalCenter
            size: 40
            name: header.person && header.person.name ? header.person.name : "Pessoa"
            photo: header.person ? header.personPhoto(header.person.avatar) : ""
            tint: Theme.text2
        }

        Column {
            anchors {
                left: dmFace.right
                leftMargin: Theme.space3
                right: parent.right
                verticalCenter: parent.verticalCenter
            }
            spacing: 2

            Row {
                spacing: Theme.space2
                Text {
                    text: header.person && header.person.name ? header.person.name : "Mensagem direta"
                    color: Theme.white
                    font.pixelSize: Theme.fontTitle
                    font.weight: Font.DemiBold
                }
                Dot {
                    visible: header.person !== null
                    anchors.verticalCenter: parent.verticalCenter
                    kind: header.personDotKind(header.person)
                }
            }

            Text {
                text: header.person ? header.personStatusLine(header.person) : "Em breve"
                color: Theme.text3
                font.pixelSize: Theme.fontSmall
                elide: Text.ElideRight
            }
        }
    }

    Rectangle {
        anchors { left: parent.left; right: parent.right; bottom: parent.bottom }
        height: 1
        color: Theme.line
    }
}
