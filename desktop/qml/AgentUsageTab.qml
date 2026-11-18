import QtQuick
import QtQuick.Controls.Basic
import Avora
import "util.js" as U

// What the agent spent over the last 30 days: tokens per day and model,
// and the plan limits of the account it thinks with.
Column {
    id: tab
    required property var page
    spacing: Theme.space5

    readonly property var usage: page.usage
    readonly property color tint: page.agent && page.agent.color ? page.agent.color : Theme.text2
    property var quotas: []

    Component.onCompleted: loadQuotas()
    function loadQuotas() {
        Api.get("/v1/quotas", (err, list) => {
            if (!err)
                tab.quotas = list || [];
        });
    }

    // The brain's kind decides which plan limits matter here.
    readonly property var brain: page.brains.find(b => b.provider === page.draft.brain.provider) || null
    readonly property string kind: brain ? brain.kind : ""
    readonly property var myQuotas: kind === "" ? [] : quotas.filter(q => q.engine === kind || (kind === "antigravity" && q.engine === "gemini"))

    // Every one of the last 30 days, oldest first, zeros where unused.
    readonly property var days: {
        const byDay = {};
        for (const d of (usage ? usage.by_day : []))
            byDay[d.day] = d;
        const out = [];
        const now = new Date();
        const n = usage ? usage.days : 30;
        for (let i = n - 1; i >= 0; i--) {
            const t = new Date(now.getFullYear(), now.getMonth(), now.getDate() - i);
            const key = t.getFullYear() + "-" + U.pad(t.getMonth() + 1) + "-" + U.pad(t.getDate());
            const d = byDay[key];
            out.push({ day: key, date: t, replies: d ? d.replies : 0, prompt: d ? d.prompt_tokens : 0, completion: d ? d.completion_tokens : 0 });
        }
        return out;
    }
    readonly property real peak: Math.max(1, ...days.map(d => d.prompt + d.completion))
    readonly property real totalTokens: usage ? usage.totals.prompt_tokens + usage.totals.completion_tokens : 0

    // ── Summary ─────────────────────────────────────────────────
    Column {
        width: parent.width
        spacing: Theme.space2
        Overline { text: "ÚLTIMOS 30 DIAS" }
        Row {
            id: cards
            width: parent.width
            spacing: Theme.space3
            readonly property var items: [
                { label: "Tokens de entrada", value: tab.usage ? U.tokens(tab.usage.totals.prompt_tokens) : "…" },
                { label: "Tokens de saída", value: tab.usage ? U.tokens(tab.usage.totals.completion_tokens) : "…" },
                { label: "Respostas", value: tab.usage ? String(tab.usage.totals.replies) : "…" }
            ]
            Repeater {
                model: cards.items
                delegate: Rectangle {
                    required property var modelData
                    width: (cards.width - 2 * Theme.space3) / 3
                    height: 84
                    radius: Theme.radiusMd
                    color: Theme.wash1
                    border.width: 1
                    border.color: Theme.line
                    Column {
                        anchors { left: parent.left; leftMargin: Theme.space4; verticalCenter: parent.verticalCenter }
                        spacing: Theme.space1
                        Text {
                            text: modelData.value
                            color: Theme.white
                            font.family: Theme.display
                            font.pixelSize: Theme.fontDisplay
                            font.weight: Font.DemiBold
                        }
                        Text {
                            text: modelData.label
                            color: Theme.text3
                            font.pixelSize: Theme.fontSmall
                        }
                    }
                }
            }
        }
    }

    // ── Per day ─────────────────────────────────────────────────
    Column {
        width: parent.width
        spacing: Theme.space3
        Item {
            width: parent.width
            height: 18
            Overline { text: "POR DIA"; anchors.verticalCenter: parent.verticalCenter }
            Row {
                anchors { right: parent.right; verticalCenter: parent.verticalCenter }
                spacing: Theme.space3
                Repeater {
                    model: [{ label: "Entrada", alpha: 0.45 }, { label: "Saída", alpha: 1 }]
                    delegate: Row {
                        required property var modelData
                        spacing: 6
                        Rectangle {
                            anchors.verticalCenter: parent.verticalCenter
                            width: 8
                            height: 8
                            radius: 2
                            color: Qt.rgba(tab.tint.r, tab.tint.g, tab.tint.b, modelData.alpha)
                        }
                        Text {
                            text: modelData.label
                            color: Theme.text3
                            font.pixelSize: Theme.fontCaption
                        }
                    }
                }
            }
        }

        Item {
            id: chart
            width: parent.width
            height: 140 + 22
            property int hover: -1
            readonly property real slot: width / tab.days.length

            Repeater {
                model: tab.days
                delegate: Item {
                    id: barSlot
                    required property var modelData
                    required property int index
                    x: index * chart.slot
                    width: chart.slot
                    height: 140
                    readonly property real full: 140 * (modelData.prompt + modelData.completion) / tab.peak
                    readonly property real outPart: 140 * modelData.completion / tab.peak
                    Rectangle {
                        anchors.fill: parent
                        anchors.margins: 1
                        radius: Theme.radiusXs
                        color: chart.hover === barSlot.index ? Theme.hover : "transparent"
                    }
                    // Input stacked under output.
                    Rectangle {
                        anchors { bottom: parent.bottom; horizontalCenter: parent.horizontalCenter }
                        width: Math.max(2, chart.slot - 4)
                        height: barSlot.full - barSlot.outPart
                        color: Qt.rgba(tab.tint.r, tab.tint.g, tab.tint.b, 0.45)
                    }
                    Rectangle {
                        anchors { bottom: parent.bottom; bottomMargin: barSlot.full - barSlot.outPart; horizontalCenter: parent.horizontalCenter }
                        width: Math.max(2, chart.slot - 4)
                        height: barSlot.outPart
                        color: tab.tint
                    }
                    Text {
                        visible: (tab.days.length - 1 - barSlot.index) % 7 === 0
                        anchors { top: parent.bottom; topMargin: 6; horizontalCenter: parent.horizontalCenter }
                        text: U.pad(barSlot.modelData.date.getDate()) + "/" + U.pad(barSlot.modelData.date.getMonth() + 1)
                        color: Theme.text4
                        font.pixelSize: Theme.fontTiny
                    }
                    MouseArea {
                        anchors.fill: parent
                        hoverEnabled: true
                        onEntered: chart.hover = barSlot.index
                        onExited: if (chart.hover === barSlot.index) chart.hover = -1
                    }
                }
            }
            Rectangle {
                y: 140
                width: parent.width
                height: 1
                color: Theme.line
            }

            // The hovered day's numbers.
            Rectangle {
                id: tip
                readonly property var d: chart.hover >= 0 ? tab.days[chart.hover] : null
                visible: d !== null
                width: tipCol.implicitWidth + 2 * Theme.space3
                height: tipCol.implicitHeight + 2 * Theme.space2
                x: Math.max(0, Math.min(chart.width - width, (chart.hover + 0.5) * chart.slot - width / 2))
                y: -height / 2
                radius: Theme.radiusSm
                color: Theme.card
                border.width: 1
                border.color: Theme.lineStrong
                Column {
                    id: tipCol
                    x: Theme.space3
                    y: Theme.space2
                    spacing: 2
                    Text {
                        text: tip.d ? U.pad(tip.d.date.getDate()) + "/" + U.pad(tip.d.date.getMonth() + 1) : ""
                        color: Theme.white
                        font.pixelSize: Theme.fontSmall
                        font.weight: Font.DemiBold
                    }
                    Text {
                        text: tip.d ? "Entrada " + U.tokens(tip.d.prompt) + "  ·  Saída " + U.tokens(tip.d.completion) : ""
                        color: Theme.text2
                        font.pixelSize: Theme.fontCaption
                    }
                    Text {
                        text: tip.d ? (tip.d.replies === 1 ? "1 resposta" : tip.d.replies + " respostas") : ""
                        color: Theme.text3
                        font.pixelSize: Theme.fontCaption
                    }
                }
            }
        }
    }

    // ── Per model ───────────────────────────────────────────────
    Column {
        width: parent.width
        spacing: Theme.space2
        Overline { text: "POR MODELO" }
        Text {
            visible: !!tab.usage && tab.usage.by_model.length === 0
            width: parent.width
            wrapMode: Text.Wrap
            text: "Ainda sem uso neste período."
            color: Theme.text4
            font.pixelSize: Theme.fontSmall
        }
        Repeater {
            model: tab.usage ? tab.usage.by_model : []
            delegate: Item {
                id: mrow
                required property var modelData
                readonly property real tokens: modelData.prompt_tokens + modelData.completion_tokens
                width: parent.width
                height: 44
                Text {
                    id: mname
                    anchors { left: parent.left; top: parent.top; topMargin: 4 }
                    width: parent.width * 0.42
                    text: mrow.modelData.model
                    color: Theme.text
                    font.family: Theme.mono
                    font.pixelSize: Theme.fontSmall
                    elide: Text.ElideMiddle
                }
                Text {
                    anchors { right: parent.right; top: parent.top; topMargin: 4 }
                    text: (mrow.modelData.replies === 1 ? "1 resposta" : mrow.modelData.replies + " respostas")
                          + "  ·  entrada " + U.tokens(mrow.modelData.prompt_tokens) + "  ·  saída " + U.tokens(mrow.modelData.completion_tokens)
                    color: Theme.text3
                    font.pixelSize: Theme.fontSmall
                }
                Rectangle {
                    anchors { left: parent.left; right: parent.right; bottom: parent.bottom; bottomMargin: 10 }
                    height: 4
                    radius: 2
                    color: Theme.wash2
                    Rectangle {
                        width: tab.totalTokens > 0 ? parent.width * mrow.tokens / tab.totalTokens : 0
                        height: parent.height
                        radius: 2
                        color: tab.tint
                    }
                }
            }
        }
    }

    // ── Plan accounts ───────────────────────────────────────────
    Column {
        width: parent.width
        spacing: Theme.space2
        Overline { text: "LIMITES DO PLANO" }
        Text {
            visible: tab.myQuotas.length === 0
            width: parent.width
            wrapMode: Text.Wrap
            text: tab.kind === "" ? "No Automático, os limites ficam em Configurações → Contas de IA."
                                  : (tab.brain ? tab.brain.name : "Esse cérebro") + " não informa limite de uso agora."
            color: Theme.text4
            font.pixelSize: Theme.fontSmall
        }
        Repeater {
            model: tab.myQuotas
            delegate: Rectangle {
                id: qrow
                required property var modelData
                readonly property real leftWeek: modelData.weekly ? 100 - modelData.weekly.used_percent : -1
                readonly property real leftFive: modelData.five_hour ? 100 - modelData.five_hour.used_percent : -1
                width: parent.width
                height: 60
                radius: Theme.radiusMd
                color: Theme.wash1
                border.width: 1
                border.color: Theme.line
                QuotaRing {
                    id: ring
                    anchors { left: parent.left; leftMargin: Theme.space3; verticalCenter: parent.verticalCenter }
                    size: 36
                    engine: qrow.modelData.engine
                    weekly: qrow.leftWeek
                    fiveHour: qrow.leftFive
                    limited: qrow.modelData.limited
                }
                Column {
                    anchors { left: ring.right; leftMargin: Theme.space3; right: parent.right; rightMargin: Theme.space3; verticalCenter: parent.verticalCenter }
                    spacing: 2
                    Text {
                        width: parent.width
                        text: (qrow.modelData.name || qrow.modelData.account || qrow.modelData.engine) + (qrow.modelData.plan ? "  ·  " + qrow.modelData.plan : "")
                        color: Theme.text
                        font.pixelSize: Theme.fontBody
                        elide: Text.ElideRight
                    }
                    Text {
                        width: parent.width
                        text: qrow.modelData.limited ? "No limite" + (qrow.modelData.five_hour && qrow.modelData.five_hour.resets_at ? ", renova " + U.when(new Date(qrow.modelData.five_hour.resets_at * 1000).toISOString()) : "")
                              : [qrow.leftFive >= 0 ? Math.round(qrow.leftFive) + "% livre nas 5 horas" : "",
                                 qrow.leftWeek >= 0 ? Math.round(qrow.leftWeek) + "% livre na semana" : ""].filter(x => x !== "").join("  ·  ")
                        color: qrow.modelData.limited ? Theme.warn : Theme.text3
                        font.pixelSize: Theme.fontSmall
                        elide: Text.ElideRight
                    }
                }
            }
        }
    }

    Text {
        width: parent.width
        wrapMode: Text.Wrap
        text: "Contamos as respostas nas conversas e no #time. Execuções no Claude Code, Codex e Antigravity entram pelos tokens que eles informam."
        color: Theme.text4
        font.pixelSize: Theme.fontSmall
    }
}
