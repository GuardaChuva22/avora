import QtQuick
import QtQuick.Controls.Basic
import Avora

// Home computer and working folder card for an agent's settings tab.
Column {
    id: card
    required property var page
    spacing: Theme.space3

    property bool ready: false
    property bool syncing: false
    property var nodes: []
    property bool loadingNodes: false
    property string nodeError: ""

    readonly property string agentId: page && page.agent ? page.agent.id : ""
    readonly property string draftHomeNodeId: {
        if (page && page.draft && page.draft.home_node_id !== undefined)
            return page.draft.home_node_id || "";
        return page && page.agent && page.agent.home_node_id ? page.agent.home_node_id : "";
    }
    readonly property string draftHomeDir: {
        if (page && page.draft && page.draft.home_dir !== undefined)
            return page.draft.home_dir || "";
        return page && page.agent && page.agent.home_dir ? page.agent.home_dir : "";
    }
    readonly property var activeNodes: nodes.filter(n => !n.revoked_at)
    readonly property var selectedNode: activeNodes.find(n => n.id === draftHomeNodeId) || null
    readonly property string defaultDirHint: "agents/" + (agentId !== "" ? agentId : "agente")

    Component.onCompleted: {
        ready = true;
        syncFields();
        loadNodes();
    }

    onAgentIdChanged: {
        if (!ready)
            return;
        syncFields();
        loadNodes();
    }

    onDraftHomeDirChanged: {
        if (ready && dirField.text !== draftHomeDir)
            syncFields();
    }

    Connections {
        target: card.page
        ignoreUnknownSignals: true
        function onDraftReset() {
            card.syncFields();
        }
    }

    Connections {
        target: Api
        function onLiveEvent(e) {
            if (e.type === "node.paired" || e.type === "node.revoked" || e.type === "node.online")
                card.loadNodes();
        }
    }

    function syncFields() {
        syncing = true;
        if (dirField.text !== draftHomeDir)
            dirField.text = draftHomeDir;
        syncing = false;
    }

    function loadNodes() {
        loadingNodes = true;
        nodeError = "";
        Api.get("/v1/nodes", (err, list) => {
            card.loadingNodes = false;
            if (err) {
                card.nodeError = err;
                return;
            }
            card.nodes = list || [];
        });
    }

    Overline { text: "COMPUTADOR E PASTA" }

    Text {
        width: parent.width
        wrapMode: Text.Wrap
        text: "Onde este agente trabalha: os arquivos, o git e o navegador dele ficam neste computador"
        color: Theme.text4
        font.pixelSize: Theme.fontSmall
    }

    ErrorBanner {
        width: parent.width
        text: card.nodeError
    }

    Flow {
        width: parent.width
        spacing: Theme.space2

        Pill {
            text: "Automático"
            on: card.draftHomeNodeId === ""
            onHit: {
                if (card.page && card.draftHomeNodeId !== "")
                    card.page.set("home_node_id", "");
            }
        }

        Repeater {
            model: card.activeNodes
            delegate: Pill {
                required property var modelData
                text: modelData.name + " · " + (modelData.online ? "online" : "offline")
                on: card.draftHomeNodeId === modelData.id
                dim: !modelData.online
                onHit: {
                    if (card.page && card.draftHomeNodeId !== modelData.id)
                        card.page.set("home_node_id", modelData.id);
                }
            }
        }
    }

    Text {
        visible: card.loadingNodes && card.activeNodes.length === 0 && card.nodeError === ""
        width: parent.width
        wrapMode: Text.Wrap
        text: "Carregando computadores…"
        color: Theme.text4
        font.pixelSize: Theme.fontSmall
    }

    Text {
        visible: !card.loadingNodes && card.activeNodes.length === 0 && card.nodeError === ""
        width: parent.width
        wrapMode: Text.Wrap
        text: "Nenhum computador conectado neste espaço ainda."
        color: Theme.text4
        font.pixelSize: Theme.fontSmall
    }

    Text {
        visible: !!card.selectedNode && !card.selectedNode.online
        width: parent.width
        wrapMode: Text.Wrap
        text: "Agora: o computador " + (card.selectedNode ? card.selectedNode.name : "") + " está desligado; as tarefas ficam na fila e começam quando ele voltar."
        color: Theme.warn
        font.pixelSize: Theme.fontSmall
    }

    Column {
        width: parent.width
        spacing: Theme.space1

        Text {
            text: "Pasta de trabalho"
            color: Theme.text3
            font.pixelSize: Theme.fontSmall
        }

        Field {
            id: dirField
            width: parent.width
            mono: true
            maximumLength: 260
            placeholderText: card.defaultDirHint
            onTextEdited: {
                if (!card.syncing && card.page)
                    card.page.set("home_dir", text);
            }
        }

        Text {
            width: parent.width
            wrapMode: Text.Wrap
            text: "Em branco, usa " + card.defaultDirHint + " dentro da pasta de trabalho do computador."
            color: Theme.text4
            font.pixelSize: Theme.fontSmall
        }
    }
}
