import QtQuick
import Avora
import "util.js" as U

// The actions an agent took in one answer, one small card each: "Criou a
// rotina «Revisão de gastos» · seg às 09:00", "Pediu ao Claude Code …".
// Readings and Claude Code's inner steps stay out (they fold under the
// answer's step list).
Column {
    id: cards
    property var steps: []
    property color tint: Theme.text2
    signal openRun(string id)
    signal openConv(string agent, string conv)
    readonly property var actions: steps.map(U.action).filter(a => a !== null)

    visible: actions.length > 0
    spacing: 6

    Repeater {
        model: cards.actions
        delegate: Rectangle {
            id: card
            required property var modelData
            readonly property bool opens: !!modelData.job || !!modelData.conv
            width: Math.min(cards.width, cardText.implicitWidth + 52)
            height: cardText.implicitHeight + 16
            radius: Theme.radiusMd
            color: modelData.failed ? Qt.rgba(1, 0.54, 0.54, 0.06) : opens && cardArea.containsMouse ? Theme.active : Theme.hover
            border.width: 1
            border.color: modelData.failed ? Theme.badLine : Theme.line
            Icon {
                x: 12
                y: 9
                name: card.modelData.icon
                size: 15
                weight: 1.5
                color: card.modelData.failed ? Theme.bad : cards.tint
            }
            Text {
                id: cardText
                x: 36
                y: 8
                width: Math.min(implicitWidth, cards.width - 48)
                textFormat: Text.StyledText
                text: "<b>" + U.esc(card.modelData.label) + "</b>" + card.modelData.note +
                      (card.modelData.summary ? "  <font color='" + Theme.text3 + "'>" + U.esc(card.modelData.summary) + "</font>" : "") +
                      (card.modelData.conv ? "  <font color='" + Theme.text2 + "'>· abrir a conversa ›</font>"
                       : card.opens ? "  <font color='" + Theme.text2 + "'>· " + (card.modelData.icon === "openai" && card.modelData.label === "Pesquisou a fundo" ? "abrir o relatório" : "ver o que fez") + " ›</font>" : "")
                color: card.modelData.failed ? Theme.bad : Theme.text
                font.pixelSize: Theme.fontBody
                wrapMode: Text.Wrap
                maximumLineCount: 3
                elide: Text.ElideRight
            }
            MouseArea {
                id: cardArea
                anchors.fill: parent
                enabled: card.opens
                hoverEnabled: true
                cursorShape: Qt.PointingHandCursor
                onClicked: {
                    if (card.modelData.conv) {
                        const cut = card.modelData.conv.indexOf("/");
                        cards.openConv(card.modelData.conv.slice(0, cut), card.modelData.conv.slice(cut + 1));
                    } else
                        cards.openRun(card.modelData.job);
                }
            }
        }
    }
}
