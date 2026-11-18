import QtQuick
import Avora
import "util.js" as U

// The technical side of one message: who, when, model, tokens, time.
Item {
    id: dlg
    property var msg: null // { who, role, at, mdl, prompt_tokens, completion_tokens, duration_ms, steps, id, err }
    property bool shown: false

    function open(m) {
        msg = m;
        shown = true;
    }

    readonly property var rows: {
        const m = msg || {};
        const out = [];
        const add = (k, v) => {
            if (v !== undefined && v !== null && v !== "" && v !== 0)
                out.push({ k: k, v: String(v) });
        };
        add("De", m.who);
        add("Enviada", m.at ? new Date(m.at).toLocaleString(Qt.locale("pt_BR"), "dd/MM/yyyy HH:mm:ss") : "");
        add("Modelo", m.mdl);
        add("Tokens de entrada", m.prompt_tokens ? U.tokens(m.prompt_tokens) : "");
        add("Tokens de saída", m.completion_tokens ? U.tokens(m.completion_tokens) : "");
        add("Tempo de resposta", m.duration_ms ? U.seconds(m.duration_ms) : "");
        add("Ações", m.steps ? (m.steps === 1 ? "1 passo" : m.steps + " passos") : "");
        add("Erro", m.err);
        add("ID", m.id > 0 ? m.id : "");
        return out;
    }

    visible: opacity > 0
    opacity: shown ? 1 : 0
    Behavior on opacity { NumberAnimation { duration: Theme.medium } }
    z: 60

    Rectangle {
        anchors.fill: parent
        color: Theme.scrim
        MouseArea {
            anchors.fill: parent
            onClicked: dlg.shown = false
        }
    }
    Shortcut {
        sequence: "Esc"
        enabled: dlg.shown
        onActivated: dlg.shown = false
    }

    Card {
        width: Math.min(420, parent.width - 48)
        height: col.implicitHeight + 2 * padding
        anchors.centerIn: parent
        padding: 24
        radius: Theme.radius
        transform: Translate {
            y: dlg.shown ? 0 : 8
            Behavior on y { NumberAnimation { duration: Theme.slow; easing.type: Theme.ease } }
        }
        MouseArea { anchors.fill: parent }

        Column {
            id: col
            width: parent.width
            spacing: 14
            Item {
                width: parent.width
                height: 28
                Text {
                    anchors.verticalCenter: parent.verticalCenter
                    text: "Detalhes da mensagem"
                    color: Theme.white
                    font.family: Theme.display
                    font.pixelSize: Theme.fontTitle
                    font.weight: Font.DemiBold
                }
                GhostButton {
                    anchors { right: parent.right; verticalCenter: parent.verticalCenter }
                    round: true
                    small: true
                    icon: "close"
                    onClicked: dlg.shown = false
                }
            }
            Column {
                width: parent.width
                spacing: 0
                Repeater {
                    model: dlg.rows
                    delegate: Item {
                        required property var modelData
                        required property int index
                        width: col.width
                        height: Math.max(34, val.implicitHeight + 16)
                        Rectangle {
                            visible: index > 0
                            width: parent.width
                            height: 1
                            color: Theme.line
                        }
                        Text {
                            anchors.verticalCenter: parent.verticalCenter
                            text: modelData.k
                            color: Theme.text3
                            font.pixelSize: Theme.fontBody
                        }
                        Text {
                            id: val
                            anchors { right: parent.right; verticalCenter: parent.verticalCenter }
                            width: Math.min(implicitWidth, col.width - 150)
                            horizontalAlignment: Text.AlignRight
                            text: modelData.v
                            color: modelData.k === "Erro" ? Theme.bad : Theme.text
                            font.family: modelData.k === "Modelo" || modelData.k === "ID" ? Theme.mono : Theme.sans
                            font.pixelSize: Theme.fontBody
                            wrapMode: Text.Wrap
                        }
                    }
                }
            }
        }
    }
}
