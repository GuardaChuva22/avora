import QtQuick
import QtQuick.Controls.Basic
import Avora
import "util.js" as U

// Esperando você: pending approvals, multiple-choice questions and mission
// decisions waiting for the owner across the active workspace.
Item {
    id: page
    property var approvals: []
    property var waitingItems: []
    property var agents: []
    property bool loading: true
    property string error: ""
    property var sending: ({})
    property var customOpen: ({})

    signal goPage(string name)
    signal openAgentConv(string agentId, string convId)

    readonly property var questions: waitingItems.filter(x => x.kind === "question")
    readonly property var decisions: waitingItems.filter(x => x.kind === "decision")
    readonly property int totalCount: approvals.length + questions.length + decisions.length

    function agentPhoto(agentId) {
        const a = agents.find(x => x.id === agentId);
        return a ? U.photo(a) : "";
    }

    function openConversationFor(agentId, convId) {
        if (typeof win !== "undefined" && win) {
            if (win.openAgent && agentId) {
                win.openAgent(agentId);
                return;
            }
            if (win.openAgentConv && agentId) {
                win.openAgentConv(agentId, convId || "");
                return;
            }
        }
        openAgentConv(agentId || "", convId || "");
    }

    function subtitleText() {
        if (loading && totalCount === 0)
            return "Conferindo se algum agente espera por você…";
        if (totalCount === 0)
            return "Nenhuma pendência agora.";
        if (totalCount === 1)
            return "1 item aguarda sua resposta para o agente continuar.";
        return totalCount + " itens aguardam sua resposta.";
    }

    function load() {
        Api.get("/v1/approvals?status=pending&limit=50", (err, list) => {
            if (!err && !U.same(approvals, list || []))
                approvals = list || [];
        });
        Api.get("/v1/waiting", (err, list) => {
            loading = false;
            if (err) {
                error = err;
                return;
            }
            error = "";
            if (!U.same(waitingItems, list || []))
                waitingItems = list || [];
        });
        Api.get("/v1/agents", (err, list) => {
            if (!err && !U.same(agents, list || []))
                agents = list || [];
        });
    }

    function sendAnswer(item, text) {
        const clean = (text || "").trim();
        if (clean === "" || !item || !item.conversation_id || sending[item.id])
            return;
        const next = Object.assign({}, sending);
        next[item.id] = true;
        sending = next;
        error = "";
        // Remove optimistically so the card answers at once while the turn starts.
        waitingItems = waitingItems.filter(x => x.id !== item.id);
        Api.post("/v1/conversations/" + encodeURIComponent(item.conversation_id) + "/messages", { content: clean }, err => {
            const done = Object.assign({}, page.sending);
            delete done[item.id];
            page.sending = done;
            if (err)
                page.error = err;
            page.load();
        });
    }

    function cancelMission(missionId) {
        if (!missionId)
            return;
        error = "";
        Api.post("/v1/missions/" + encodeURIComponent(missionId) + "/cancel", null, err => {
            if (err)
                page.error = err;
            page.load();
        });
    }

    Component.onCompleted: load()
    onVisibleChanged: if (visible) load()

    Connections {
        target: Api
        function onWorkspaceChanged() {
            page.approvals = [];
            page.waitingItems = [];
            page.agents = [];
            page.customOpen = ({});
            page.loading = true;
            page.load();
        }
        function onLiveEvent(e) {
            if (!page.visible)
                return;
            const t = e.type || "";
            if (t.startsWith("approval.") || t.startsWith("mission.") || t.startsWith("message."))
                reloadTimer.restart();
        }
        function onLiveReset() {
            if (page.visible)
                page.load();
        }
    }

    Timer {
        id: reloadTimer
        interval: 200
        onTriggered: page.load()
    }

    Timer {
        interval: Api.live ? 20000 : 4000
        running: page.visible
        repeat: true
        onTriggered: page.load()
    }

    Flickable {
        id: scroller
        anchors.fill: parent
        contentHeight: body.implicitHeight + 96
        clip: true
        boundsBehavior: Flickable.StopAtBounds
        ScrollBar.vertical: SlimScrollBar {}
        SmoothWheel {}

        Column {
            id: body
            readonly property real colWidth: Math.min(scroller.width - 2 * Theme.gutter, 820)
            x: Math.max(Theme.gutter, (scroller.width - colWidth) / 2)
            y: 44
            width: colWidth
            spacing: Theme.space5

            PageHead {
                width: parent.width
                eyebrow: "Trabalho visível"
                title: "Esperando você"
                sub: page.subtitleText()
            }

            ErrorBanner {
                width: parent.width
                text: page.error
            }

            // Loading state
            Card {
                visible: page.loading && page.totalCount === 0
                width: parent.width
                height: 88
                padding: Theme.space5

                Text {
                    anchors.verticalCenter: parent.verticalCenter
                    text: "Conferindo aprovações, perguntas e decisões pendentes…"
                    color: Theme.text3
                    font.pixelSize: Theme.fontBody
                }
            }

            // Empty state
            Card {
                visible: !page.loading && page.totalCount === 0
                width: parent.width
                height: emptyCol.implicitHeight + 2 * padding
                padding: Theme.space5

                Column {
                    id: emptyCol
                    width: parent.width
                    spacing: Theme.space2

                    Text {
                        text: "Nada esperando por você agora"
                        color: Theme.white
                        font.family: Theme.display
                        font.pixelSize: Theme.fontHeading
                        font.weight: Font.DemiBold
                    }
                    Text {
                        width: parent.width
                        wrapMode: Text.WordWrap
                        text: "Quando um agente pedir permissão para uma ação sensível, fizer uma pergunta ou pausar uma missão para alinhar algo com você, ela aparece aqui."
                        color: Theme.text3
                        font.pixelSize: Theme.fontBody
                    }
                }
            }

            // 1. Approvals
            Column {
                visible: page.approvals.length > 0
                width: parent.width
                spacing: Theme.space3

                Overline { text: "PERMISSÕES PENDENTES" }

                ApprovalCards {
                    width: parent.width
                    items: page.approvals
                    agents: page.agents
                    onDecided: page.load()
                }
            }

            // 2. Questions
            Column {
                id: qSec
                visible: page.questions.length > 0
                width: parent.width
                spacing: Theme.space3

                Overline { text: "PERGUNTAS DOS AGENTES" }

                Repeater {
                    model: page.questions
                    delegate: Card {
                        id: qWrap
                        required property var modelData
                        readonly property bool showCustom: !!page.customOpen[modelData.id]
                        width: qSec.width
                        height: qCol.implicitHeight + 2 * padding
                        padding: Theme.space4

                        Column {
                            id: qCol
                            width: parent.width
                            spacing: Theme.space3

                            Item {
                                width: parent.width
                                height: 30

                                Row {
                                    spacing: Theme.space2
                                    anchors.verticalCenter: parent.verticalCenter
                                    Avatar {
                                        size: 26
                                        name: qWrap.modelData.agent_name || qWrap.modelData.agent_id
                                        photo: page.agentPhoto(qWrap.modelData.agent_id)
                                        tint: qWrap.modelData.agent_color || Theme.text2
                                        anchors.verticalCenter: parent.verticalCenter
                                    }
                                    Text {
                                        text: qWrap.modelData.agent_name || qWrap.modelData.agent_id
                                        color: Theme.white
                                        font.pixelSize: Theme.fontBody
                                        font.weight: Font.DemiBold
                                        anchors.verticalCenter: parent.verticalCenter
                                    }
                                    Text {
                                        text: "· " + U.ago(qWrap.modelData.created_at)
                                        color: Theme.text4
                                        font.pixelSize: Theme.fontSmall
                                        anchors.verticalCenter: parent.verticalCenter
                                    }
                                }

                                GhostButton {
                                    anchors { right: parent.right; verticalCenter: parent.verticalCenter }
                                    small: true
                                    icon: "chat"
                                    text: "Abrir conversa"
                                    onClicked: page.openConversationFor(qWrap.modelData.agent_id, qWrap.modelData.conversation_id)
                                }
                            }

                            QuestionCard {
                                width: parent.width
                                question: qWrap.modelData.title || ""
                                options: qWrap.modelData.options || []
                                multiple: !!qWrap.modelData.multiple
                                other: !!qWrap.modelData.other
                                live: !page.sending[qWrap.modelData.id]
                                tint: qWrap.modelData.agent_color || Theme.text2
                                onPicked: text => page.sendAnswer(qWrap.modelData, text)
                                onOtherAnswer: {
                                    const c = Object.assign({}, page.customOpen);
                                    c[qWrap.modelData.id] = true;
                                    page.customOpen = c;
                                    customField.forceActiveFocus();
                                }
                            }

                            Row {
                                visible: qWrap.showCustom
                                width: parent.width
                                spacing: Theme.space2

                                Field {
                                    id: customField
                                    width: parent.width - customSendBtn.width - Theme.space2
                                    placeholderText: "Escreva sua resposta…"
                                    onAccepted: page.sendAnswer(qWrap.modelData, text)
                                }
                                PrimaryButton {
                                    id: customSendBtn
                                    text: "Enviar"
                                    icon: "send"
                                    enabled: customField.text.trim() !== ""
                                    onClicked: page.sendAnswer(qWrap.modelData, customField.text)
                                }
                            }
                        }
                    }
                }
            }

            // 3. Mission decisions
            Column {
                id: dSec
                visible: page.decisions.length > 0
                width: parent.width
                spacing: Theme.space3

                Overline { text: "DECISÕES PENDENTES" }

                Repeater {
                    model: page.decisions
                    delegate: Card {
                        id: dCard
                        required property var modelData
                        width: dSec.width
                        height: dCol.implicitHeight + 2 * padding
                        padding: Theme.space4

                        Column {
                            id: dCol
                            width: parent.width
                            spacing: Theme.space3

                            Item {
                                width: parent.width
                                height: 30

                                Row {
                                    spacing: Theme.space2
                                    anchors.verticalCenter: parent.verticalCenter
                                    Avatar {
                                        size: 26
                                        name: dCard.modelData.agent_name || dCard.modelData.agent_id
                                        photo: page.agentPhoto(dCard.modelData.agent_id)
                                        tint: dCard.modelData.agent_color || Theme.warn
                                        anchors.verticalCenter: parent.verticalCenter
                                    }
                                    Text {
                                        text: (dCard.modelData.agent_name || dCard.modelData.agent_id) + " · " + (dCard.modelData.title || "")
                                        color: Theme.white
                                        font.pixelSize: Theme.fontBody
                                        font.weight: Font.DemiBold
                                        anchors.verticalCenter: parent.verticalCenter
                                    }
                                }

                                Row {
                                    anchors { right: parent.right; verticalCenter: parent.verticalCenter }
                                    spacing: Theme.space2
                                    GhostButton {
                                        small: true
                                        icon: "chat"
                                        text: "Abrir conversa"
                                        onClicked: page.openConversationFor(dCard.modelData.agent_id, dCard.modelData.conversation_id)
                                    }
                                    GhostButton {
                                        small: true
                                        danger: true
                                        icon: "close"
                                        text: "Parar"
                                        onClicked: page.cancelMission(dCard.modelData.mission_id)
                                    }
                                }
                            }

                            Text {
                                width: parent.width
                                wrapMode: Text.WordWrap
                                text: dCard.modelData.summary || ""
                                color: Theme.text2
                                font.pixelSize: Theme.fontBody
                            }

                            Row {
                                width: parent.width
                                spacing: Theme.space2

                                Field {
                                    id: decReply
                                    width: parent.width - decSend.width - Theme.space2
                                    placeholderText: "Responda para o agente continuar…"
                                    onAccepted: page.sendAnswer(dCard.modelData, text)
                                }
                                PrimaryButton {
                                    id: decSend
                                    icon: "send"
                                    text: "Responder"
                                    enabled: decReply.text.trim() !== ""
                                    onClicked: page.sendAnswer(dCard.modelData, decReply.text)
                                }
                            }
                        }
                    }
                }
            }
        }
    }
}
