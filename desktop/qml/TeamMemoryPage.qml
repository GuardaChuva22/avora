import QtQuick
import QtQuick.Controls.Basic
import Avora

// Memória do time: workspace-wide facts, rules, preferences and decisions
// shared across every agent in the active workspace.
Item {
    id: page
    property string selectedKind: "fact"

    readonly property var quickKinds: [
        { value: "fact", label: "Fato" },
        { value: "decision", label: "Decisão" },
        { value: "rule", label: "Regra" },
        { value: "preference", label: "Preferência" },
        { value: "lesson", label: "Lição" }
    ]
    readonly property var examples: [
        "Deploy em produção somente às terças e quintas",
        "Nunca enviar mensagem externa sem revisão humana",
        "Relatórios financeiros sempre em reais (BRL)",
        "Nosso público principal fala português do Brasil"
    ]

    function teachTeam() {
        const t = quickInput.text.trim();
        if (t === "")
            return;
        teamMemory.addMemory(t, selectedKind);
        quickInput.text = "";
    }

    Connections {
        target: Api
        function onWorkspaceChanged() {
            quickInput.text = "";
            page.selectedKind = "fact";
        }
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
                eyebrow: "Conhecimento compartilhado"
                title: "Memória do time"
                sub: "O que vale para todos os agentes deste espaço: decisões, regras fixas, fatos e aprendizados compartilhados."
            }

            Card {
                width: parent.width
                height: teachCol.implicitHeight + 2 * padding
                padding: Theme.space5
                radius: Theme.radius

                Column {
                    id: teachCol
                    width: parent.width
                    spacing: Theme.space3

                    Overline { text: "ENSINAR AO TIME" }

                    Flow {
                        width: parent.width
                        spacing: Theme.space2
                        Repeater {
                            model: page.quickKinds
                            delegate: Pill {
                                required property var modelData
                                text: modelData.label
                                on: page.selectedKind === modelData.value
                                onHit: page.selectedKind = modelData.value
                            }
                        }
                    }

                    Row {
                        width: parent.width
                        spacing: Theme.space2

                        Field {
                            id: quickInput
                            width: parent.width - saveBtn.width - Theme.space2
                            maximumLength: 800
                            placeholderText: "Ex.: Nunca publicar sem revisão, ou Fechamento todo dia 28"
                            onAccepted: page.teachTeam()
                        }
                        PrimaryButton {
                            id: saveBtn
                            icon: "plus"
                            text: "Guardar"
                            enabled: quickInput.text.trim() !== ""
                            onClicked: page.teachTeam()
                        }
                    }

                    Flow {
                        width: parent.width
                        spacing: Theme.space2
                        Repeater {
                            model: page.examples
                            delegate: Pill {
                                required property string modelData
                                text: modelData
                                dim: true
                                onHit: {
                                    quickInput.text = modelData;
                                    quickInput.forceActiveFocus();
                                }
                            }
                        }
                    }
                }
            }

            AgentMemory {
                id: teamMemory
                width: parent.width
                teamMode: true
                active: page.visible
                showAll: true
            }
        }
    }
}
