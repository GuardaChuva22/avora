import QtQuick
import QtQuick.Controls.Basic
import Avora

// Objetivos: outcomes the agents pursue on their own for days, under the
// owner's mandate. Each goal shows its funnel as columns, when the agent
// wakes next and why, what it learned, and the pages it watches. The work
// itself happens in the goal's conversation.
Item {
    id: page
    property var goals: []
    property var agents: []
    property string error: ""
    signal openConversation(string id)

    function load() {
        Api.get("/v1/goals", (err, data) => {
            if (err) {
                error = err;
                return;
            }
            error = "";
            const list = (data || []).slice().reverse();
            if (JSON.stringify(list) !== JSON.stringify(goals))
                goals = list;
        });
        Api.get("/v1/agents", (err, data) => { if (!err) agents = data; });
    }
    Component.onCompleted: load()
    Timer {
        interval: 15000
        running: page.visible
        repeat: true
        onTriggered: page.load()
    }

    function agentName(id) {
        const a = agents.find(x => x.id === id);
        return a ? a.name : id;
    }
    // A wait (aguardar_evento): what the agent waits for, and until when.
    function waitText(w) {
        const labels = { email: "e-mail", tarefa: "tarefa no ClickUp", instagram_dm: "DM no Instagram",
                         tinder_mensagem: "mensagem no Tinder", tinder_match: "match no Tinder" };
        const a = w.args || {};
        let t = labels[w.event] || w.event;
        if (a.busca)
            t += " «" + a.busca + "»";
        if (w.why)
            t += " (" + w.why + ")";
        if (w.until)
            t += " até " + when(w.until);
        if (w.error)
            t += " · erro: " + w.error;
        return t;
    }
    function when(iso) {
        if (!iso)
            return "";
        const d = new Date(iso), now = new Date();
        const pad = n => (n < 10 ? "0" : "") + n;
        const hm = pad(d.getHours()) + ":" + pad(d.getMinutes());
        if (d.toDateString() === now.toDateString())
            return "hoje " + hm;
        return pad(d.getDate()) + "/" + pad(d.getMonth() + 1) + " " + hm;
    }
    function setStatus(g, st) {
        Api.post("/v1/goals/" + g.id, { status: st }, err => {
            if (err)
                error = err;
            load();
        });
    }
    function wake(g) {
        Api.post("/v1/goals/" + g.id + "/wake", {}, err => {
            if (err)
                error = err;
            load();
        });
    }

    readonly property var statusWords: ({ ativo: "Ativo", pausado: "Pausado", concluido: "Concluído", cancelado: "Cancelado" })

    Flickable {
        id: scroller
        anchors.fill: parent
        contentHeight: col.height + 110
        clip: true
        boundsBehavior: Flickable.StopAtBounds
        ScrollBar.vertical: SlimScrollBar {}
        SmoothWheel {}

        Column {
            id: col
            x: Math.round((scroller.width - width) / 2)
            y: 52
            width: Math.min(scroller.width - 2 * Theme.gutter, 1240)
            spacing: 22

            PageHead {
                eyebrow: "Autonomia"
                title: "Objetivos"
                sub: "Resultados que os funcionários perseguem sozinhos por dias, dentro do mandato que você deu. No chat, peça algo como “consegue 5 reuniões com clínicas até o fim do mês; pode mandar a primeira mensagem no Instagram”."
            }

            ErrorBanner {
                width: parent.width
                text: page.error
                visible: page.error !== ""
            }

            Text {
                visible: page.goals.length === 0 && page.error === ""
                width: parent.width
                wrapMode: Text.WordWrap
                text: "Nenhum objetivo ainda."
                color: Theme.text3
                font.pixelSize: Theme.fontLarge
            }

            Repeater {
                model: page.goals
                delegate: Card {
                    id: goalCard
                    required property var modelData
                    readonly property var g: modelData
                    readonly property bool active: g.status === "ativo"
                    width: col.width
                    padding: 20
                    height: goalCol.implicitHeight + 2 * padding
                    opacity: g.status === "ativo" || g.status === "pausado" ? 1 : 0.6

                    Column {
                        id: goalCol
                        width: parent.width
                        spacing: 12

                        // Title, owner, status and actions.
                        Item {
                            width: parent.width
                            height: Math.max(titleCol.implicitHeight, actions.height)
                            Column {
                                id: titleCol
                                anchors { left: parent.left; right: actions.left; rightMargin: 16 }
                                spacing: 4
                                Text {
                                    width: parent.width
                                    elide: Text.ElideRight
                                    text: "🎯 " + goalCard.g.title
                                    color: Theme.white
                                    font.pixelSize: Theme.fontTitle
                                    font.weight: Font.DemiBold
                                }
                                Text {
                                    width: parent.width
                                    wrapMode: Text.WordWrap
                                    text: page.agentName(goalCard.g.agent_id) + " · " + page.statusWords[goalCard.g.status]
                                          + (goalCard.g.deadline ? " · prazo " + page.when(goalCard.g.deadline).replace(/ \d\d:\d\d$/, "") : "")
                                          + " · " + goalCard.g.outcome
                                    color: Theme.text3
                                    font.pixelSize: Theme.fontBody
                                }
                            }
                            Row {
                                id: actions
                                anchors.right: parent.right
                                spacing: 6
                                GhostButton {
                                    small: true
                                    text: "Conversa"
                                    onClicked: page.openConversation(goalCard.g.conversation_id)
                                }
                                GhostButton {
                                    small: true
                                    visible: goalCard.active
                                    text: "Acordar agora"
                                    onClicked: page.wake(goalCard.g)
                                }
                                GhostButton {
                                    small: true
                                    visible: goalCard.g.status === "ativo" || goalCard.g.status === "pausado"
                                    text: goalCard.active ? "Pausar" : "Retomar"
                                    onClicked: page.setStatus(goalCard.g, goalCard.active ? "pausado" : "ativo")
                                }
                            }
                        }

                        // Mandate, next wake, strategy, lessons.
                        Repeater {
                            model: [
                                { k: "Mandato", v: goalCard.g.mandate || "nenhum: toda ação arriscada pede a sua aprovação" },
                                { k: "Nunca", v: goalCard.g.never || "" },
                                { k: "Acorda", v: goalCard.active && goalCard.g.next_wake ? page.when(goalCard.g.next_wake) + (goalCard.g.wake_reason ? " · " + goalCard.g.wake_reason : "") : "" },
                                { k: "Estratégia", v: goalCard.g.strategy || "" },
                                { k: "Aprendeu", v: (goalCard.g.lessons || []).join(" · ") },
                                { k: "Vigia", v: (goalCard.g.watches || []).filter(w => !w.event).map(w => w.url.replace(/^https?:\/\/(www\.)?/, "") + (w.why ? " (" + w.why + ")" : "")).join(" · ") },
                                { k: "Espera", v: (goalCard.g.watches || []).filter(w => !!w.event).map(w => page.waitText(w)).join(" · ") }
                            ].filter(r => r.v !== "")
                            delegate: Row {
                                required property var modelData
                                width: goalCol.width
                                spacing: 10
                                Text {
                                    width: 84
                                    text: modelData.k
                                    color: Theme.text4
                                    font.pixelSize: Theme.fontSmall
                                    font.weight: Font.DemiBold
                                }
                                Text {
                                    width: parent.width - 94
                                    wrapMode: Text.WordWrap
                                    text: modelData.v
                                    color: Theme.text2
                                    font.pixelSize: Theme.fontBody
                                }
                            }
                        }

                        // The funnel, one column per stage.
                        Flickable {
                            width: parent.width
                            height: funnel.height
                            contentWidth: funnel.width
                            clip: true
                            boundsBehavior: Flickable.StopAtBounds
                            interactive: contentWidth > width
                            Row {
                                id: funnel
                                spacing: 10
                                Repeater {
                                    model: goalCard.g.stages || []
                                    delegate: Rectangle {
                                        id: stageCol
                                        required property string modelData
                                        readonly property var items: (goalCard.g.items || []).filter(it => it.stage === modelData)
                                        width: 196
                                        height: Math.max(90, stageInner.implicitHeight + 20)
                                        radius: Theme.radiusMd
                                        color: Theme.wash1
                                        border.width: 1
                                        border.color: Theme.line
                                        Column {
                                            id: stageInner
                                            x: 10
                                            y: 10
                                            width: parent.width - 20
                                            spacing: 6
                                            Text {
                                                width: parent.width
                                                elide: Text.ElideRight
                                                text: stageCol.modelData + "  " + stageCol.items.length
                                                color: Theme.text3
                                                font.pixelSize: Theme.fontSmall
                                                font.weight: Font.DemiBold
                                            }
                                            Repeater {
                                                model: stageCol.items.slice(0, 12)
                                                delegate: Rectangle {
                                                    required property var modelData
                                                    width: stageInner.width
                                                    height: itemCol.implicitHeight + 14
                                                    radius: Theme.radiusSm
                                                    color: Theme.card
                                                    border.width: 1
                                                    border.color: itemTip.containsMouse ? Theme.lineStrong : Theme.line
                                                    Column {
                                                        id: itemCol
                                                        x: 8
                                                        y: 7
                                                        width: parent.width - 16
                                                        spacing: 2
                                                        Text {
                                                            width: parent.width
                                                            elide: Text.ElideRight
                                                            text: modelData.name
                                                            color: Theme.text
                                                            font.pixelSize: Theme.fontBody
                                                        }
                                                        Text {
                                                            visible: text !== ""
                                                            width: parent.width
                                                            elide: Text.ElideRight
                                                            text: modelData.next || modelData.contact || ""
                                                            color: Theme.text4
                                                            font.pixelSize: Theme.fontCaption
                                                        }
                                                    }
                                                    MouseArea {
                                                        id: itemTip
                                                        anchors.fill: parent
                                                        hoverEnabled: true
                                                        cursorShape: modelData.link ? Qt.PointingHandCursor : Qt.ArrowCursor
                                                        onClicked: if (modelData.link) Qt.openUrlExternally(modelData.link)
                                                    }
                                                    ToolTip.visible: itemTip.containsMouse && (modelData.notes || "") !== ""
                                                    ToolTip.delay: 400
                                                    ToolTip.text: modelData.notes || ""
                                                }
                                            }
                                            Text {
                                                visible: stageCol.items.length > 12
                                                text: "+" + (stageCol.items.length - 12)
                                                color: Theme.text4
                                                font.pixelSize: Theme.fontCaption
                                            }
                                        }
                                    }
                                }
                            }
                        }
                    }
                }
            }
        }
    }
}
