import QtQuick
import QtQuick.Controls.Basic
import Avora
import "util.js" as U

// What the agent remembers, and a quick way to teach it something.
Column {
    id: tab
    required property var page
    spacing: Theme.space5

    readonly property string who: page.agent ? page.agent.name : "ele"
    readonly property var examples: ["Prefiro respostas curtas", "Meu horário é das 9h às 18h", "Nunca mande e-mail sem me mostrar antes",
        "Me chame pelo primeiro nome", "Valores sempre em reais"]

    function teach() {
        const t = lesson.text.trim();
        if (t === "")
            return;
        memory.addMemory(t);
        lesson.text = "";
    }

    Text {
        width: parent.width
        wrapMode: Text.Wrap
        text: "O que " + tab.who + " lembra sobre você e o trabalho. Ele guarda sozinho quando você conta algo importante numa conversa; aqui você corrige, apaga ou ensina."
        color: Theme.text3
        font.pixelSize: Theme.fontBody
        lineHeight: 1.3
    }

    // Teach in one line; the examples fill the field.
    Column {
        width: parent.width
        spacing: Theme.space2
        Overline { text: "ENSINAR" }
        Row {
            width: parent.width
            spacing: Theme.space2
            Field {
                id: lesson
                width: parent.width - teachButton.width - Theme.space2
                maximumLength: 500
                placeholderText: "Algo que " + tab.who + " deve saber. Ex.: prefiro reuniões depois das 14h"
                onAccepted: tab.teach()
            }
            PrimaryButton {
                id: teachButton
                text: "Ensinar"
                icon: "plus"
                onClicked: tab.teach()
            }
        }
        Flow {
            width: parent.width
            spacing: Theme.space2
            Repeater {
                model: tab.examples
                delegate: Pill {
                    required property string modelData
                    text: modelData
                    onHit: {
                        lesson.text = modelData;
                        lesson.forceActiveFocus();
                    }
                }
            }
        }
    }

    AgentMemory {
        id: memory
        width: parent.width
        agent: tab.page.agent
        active: tab.page.shown
        Component.onCompleted: showAll = true
    }
}
