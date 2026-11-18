import QtQuick
import QtQuick.Controls.Basic
import Avora
import "util.js" as U

// Computers of the current workspace: online state, resident agents, detected
// local models (Ollama / LM Studio) and the computer's shared folder.
Item {
    id: page
    property var nodes: []
    property var agents: []
    property var providers: []
    property bool loading: false
    property string actionError: ""
    property string connectingKey: ""
    property string confirm: ""

    readonly property var active: nodes.filter(n => !n.revoked_at)
    readonly property var revoked: nodes.filter(n => n.revoked_at)

    function load() {
        loading = nodes.length === 0;
        Api.get("/v1/nodes", (err, data) => {
            page.loading = false;
            if (!err && !U.same(page.nodes, data || []))
                page.nodes = data || [];
        });
        Api.get("/v1/agents", (err, data) => {
            if (!err && !U.same(page.agents, data || []))
                page.agents = data || [];
        });
        Api.get("/v1/models", (err, data) => {
            const list = data && data.providers ? data.providers : [];
            if (!err && !U.same(page.providers, list))
                page.providers = list;
        });
    }

    function agentsOn(nodeId) {
        return agents.filter(a => a.home_node_id === nodeId);
    }

    function connectedProvider(nodeId, kind) {
        return providers.find(p => p.node_id === nodeId && p.kind === kind) || null;
    }

    function localKindLabel(kind) {
        if (kind === "ollama")
            return "Ollama";
        if (kind === "lmstudio")
            return "LM Studio";
        return kind;
    }

    function connectLocalModel(nodeObj, localModel) {
        if (connectingKey !== "")
            return;
        const key = nodeObj.id + ":" + localModel.kind;
        connectingKey = key;
        actionError = "";
        Api.post("/v1/nodes/" + encodeURIComponent(nodeObj.id) + "/providers", {
            kind: localModel.kind,
            base_url: localModel.url || ""
        }, err => {
            page.connectingKey = "";
            if (err) {
                page.actionError = err;
                return;
            }
            page.load();
        });
    }

    function revoke(id) {
        if (confirm !== id) {
            confirm = id;
            disarm.restart();
            return;
        }
        confirm = "";
        actionError = "";
        Api.post("/v1/nodes/" + encodeURIComponent(id) + "/revoke", null, err => {
            if (err) {
                page.actionError = err;
                return;
            }
            page.load();
        });
    }

    Connections {
        target: Api
        function onLiveEvent(e) {
            if (!page.visible)
                return;
            if (e.type === "node.paired" || e.type === "node.revoked" || e.type === "node.online"
                    || e.type === "agent.saved" || e.type === "provider.saved" || e.type === "provider.deleted")
                page.load();
        }
    }

    Timer {
        interval: 5000
        repeat: true
        running: page.visible
        triggeredOnStart: true
        onTriggered: page.load()
    }

    Timer {
        id: disarm
        interval: 4000
        onTriggered: page.confirm = ""
    }

    Flickable {
        id: scroller
        anchors.fill: parent
        contentHeight: col.height + Theme.space6 * 3
        clip: true
        boundsBehavior: Flickable.StopAtBounds
        ScrollBar.vertical: SlimScrollBar {}
        SmoothWheel {}

        Column {
            id: col
            x: Theme.gutter
            y: Theme.titleBar + Theme.space3
            width: scroller.width - 2 * Theme.gutter
            spacing: Theme.space5

            PageHead {
                eyebrow: "Infraestrutura"
                title: "Computadores"
                sub: "As máquinas onde os agentes trabalham, guardam arquivos e rodam modelos locais."
            }

            ErrorBanner {
                width: parent.width
                text: page.actionError
            }

            Card {
                visible: !page.loading && page.active.length === 0
                width: parent.width
                height: emptyCol.implicitHeight + 2 * padding
                padding: Theme.space5

                Column {
                    id: emptyCol
                    width: parent.width
                    spacing: Theme.space2

                    Text {
                        width: parent.width
                        wrapMode: Text.Wrap
                        text: "Nenhum computador conectado neste espaço"
                        color: Theme.white
                        font.family: Theme.display
                        font.pixelSize: Theme.fontTitle
                        font.weight: Font.DemiBold
                    }

                    Text {
                        width: parent.width
                        wrapMode: Text.Wrap
                        text: "Quando você parear seu PC ou um servidor, ele aparece aqui com os agentes que moram nele, os modelos locais detectados e a pasta compartilhada."
                        color: Theme.text3
                        font.pixelSize: Theme.fontBody
                    }
                }
            }

            Text {
                visible: page.loading && page.active.length === 0
                text: "Carregando computadores…"
                color: Theme.text4
                font.pixelSize: Theme.fontBody
            }

            Flow {
                id: grid
                visible: page.active.length > 0
                width: parent.width
                spacing: Theme.space4
                readonly property real cell: {
                    const n = Math.max(1, Math.floor((width + spacing) / (380 + spacing)));
                    return (width - (n - 1) * spacing) / n;
                }

                Repeater {
                    model: page.active
                    delegate: Card {
                        id: node
                        required property var modelData
                        readonly property bool online: !!modelData.online
                        readonly property var residentAgents: page.agentsOn(modelData.id)
                        readonly property var detectedModels: modelData.local_models || []
                        readonly property string sharedPath: modelData.shared_dir && modelData.shared_dir !== "" ? modelData.shared_dir : "<pasta do computador>/shared"

                        width: grid.cell
                        height: cardCol.implicitHeight + 2 * padding
                        padding: Theme.space5
                        glow: false

                        Column {
                            id: cardCol
                            width: parent.width
                            spacing: Theme.space4

                            // Header: icon, name/kind and online badge.
                            Item {
                                width: parent.width
                                height: 44

                                Rectangle {
                                    id: badge
                                    width: 44
                                    height: 44
                                    radius: Theme.radiusMd
                                    color: Theme.well
                                    border.width: 1
                                    border.color: node.online ? Theme.wash5 : Theme.line
                                    Icon {
                                        anchors.centerIn: parent
                                        name: node.modelData.kind === "windows" ? "monitor" : "server"
                                        size: 20
                                        color: node.online ? Theme.white : Theme.text3
                                    }
                                }

                                Column {
                                    anchors {
                                        left: badge.right
                                        leftMargin: Theme.space3
                                        right: statusBadge.left
                                        rightMargin: Theme.space2
                                        verticalCenter: badge.verticalCenter
                                    }
                                    spacing: Theme.space1

                                    Text {
                                        width: parent.width
                                        text: node.modelData.name
                                        color: Theme.white
                                        font.pixelSize: Theme.fontTitle
                                        font.weight: Font.DemiBold
                                        elide: Text.ElideRight
                                    }

                                    Text {
                                        width: parent.width
                                        text: U.kindLabel(node.modelData.kind)
                                        color: Theme.text3
                                        font.pixelSize: Theme.fontBody
                                        elide: Text.ElideRight
                                    }
                                }

                                Rectangle {
                                    id: statusBadge
                                    anchors { right: parent.right; verticalCenter: badge.verticalCenter }
                                    height: 26
                                    width: st.implicitWidth + 22
                                    radius: height / 2
                                    color: node.online ? Theme.wash4 : Theme.wash1
                                    border.width: 1
                                    border.color: node.online ? Theme.wash5 : Theme.line

                                    Row {
                                        id: st
                                        anchors.centerIn: parent
                                        spacing: Theme.space2
                                        Dot { kind: node.online ? "live" : ""; anchors.verticalCenter: parent.verticalCenter }
                                        Text {
                                            text: node.online ? "Online" : "Offline"
                                            color: node.online ? Theme.white : Theme.text3
                                            font.pixelSize: Theme.fontSmall
                                            font.weight: Font.Medium
                                            anchors.verticalCenter: parent.verticalCenter
                                        }
                                    }
                                }
                            }

                            // Presence metadata.
                            Column {
                                width: parent.width
                                spacing: Theme.space2

                                Repeater {
                                    model: [
                                        ["Último sinal", U.ago(node.modelData.last_seen)],
                                        ["Pareado", U.ago(node.modelData.created_at)],
                                        ["ID", node.modelData.id]
                                    ]
                                    delegate: Item {
                                        required property var modelData
                                        width: parent.width
                                        height: 18

                                        Text {
                                            text: modelData[0]
                                            color: Theme.text3
                                            font.pixelSize: Theme.fontBody
                                        }

                                        Text {
                                            anchors.right: parent.right
                                            text: modelData[1]
                                            color: Theme.text2
                                            font.family: Theme.mono
                                            font.pixelSize: Theme.fontSmall
                                        }
                                    }
                                }
                            }

                            Rectangle {
                                width: parent.width
                                height: 1
                                color: Theme.line
                            }

                            // Agents living on this computer.
                            Column {
                                width: parent.width
                                spacing: Theme.space2

                                Overline { text: "AGENTES NESTE COMPUTADOR" }

                                Text {
                                    visible: node.residentAgents.length === 0
                                    width: parent.width
                                    wrapMode: Text.Wrap
                                    text: "Nenhum agente mora fixo neste computador."
                                    color: Theme.text4
                                    font.pixelSize: Theme.fontSmall
                                }

                                Flow {
                                    visible: node.residentAgents.length > 0
                                    width: parent.width
                                    spacing: Theme.space2

                                    Repeater {
                                        model: node.residentAgents
                                        delegate: Rectangle {
                                            required property var modelData
                                            height: 30
                                            width: agRow.implicitWidth + 20
                                            radius: height / 2
                                            color: Theme.wash1
                                            border.width: 1
                                            border.color: Theme.line

                                            Row {
                                                id: agRow
                                                anchors.centerIn: parent
                                                spacing: Theme.space2

                                                Avatar {
                                                    size: 20
                                                    name: modelData.name
                                                    tint: modelData.color || Theme.text2
                                                    photo: U.photo(modelData)
                                                    working: !!modelData.working
                                                    anchors.verticalCenter: parent.verticalCenter
                                                }

                                                Text {
                                                    text: modelData.name
                                                    color: Theme.text
                                                    font.pixelSize: Theme.fontSmall
                                                    font.weight: Font.Medium
                                                    anchors.verticalCenter: parent.verticalCenter
                                                }
                                            }
                                        }
                                    }
                                }
                            }

                            // Detected local models (Ollama / LM Studio).
                            Column {
                                width: parent.width
                                spacing: Theme.space2

                                Overline { text: "MODELOS LOCAIS" }

                                Text {
                                    visible: node.detectedModels.length === 0
                                    width: parent.width
                                    wrapMode: Text.Wrap
                                    text: node.online
                                          ? "Nenhum Ollama ou LM Studio detectado neste computador."
                                          : "Quando o computador estiver online, o Avora detecta o Ollama e o LM Studio dele."
                                    color: Theme.text4
                                    font.pixelSize: Theme.fontSmall
                                }

                                Column {
                                    visible: node.detectedModels.length > 0
                                    width: parent.width
                                    spacing: Theme.space2

                                    Repeater {
                                        model: node.detectedModels
                                        delegate: Rectangle {
                                            id: lmRow
                                            required property var modelData
                                            readonly property var existing: page.connectedProvider(node.modelData.id, modelData.kind)
                                            readonly property bool isConnecting: page.connectingKey === (node.modelData.id + ":" + modelData.kind)
                                            readonly property int count: (modelData.models || []).length

                                            width: parent.width
                                            height: lmCol.implicitHeight + Theme.space3 * 2
                                            radius: Theme.radiusMd
                                            color: Theme.well
                                            border.width: 1
                                            border.color: Theme.line

                                            Column {
                                                id: lmCol
                                                x: Theme.space3
                                                y: Theme.space3
                                                width: parent.width - Theme.space3 * 2
                                                spacing: Theme.space1

                                                Item {
                                                    width: parent.width
                                                    height: 30

                                                    Column {
                                                        anchors {
                                                            left: parent.left
                                                            right: lmAction.left
                                                            rightMargin: Theme.space2
                                                            verticalCenter: parent.verticalCenter
                                                        }
                                                        spacing: Theme.space1

                                                        Text {
                                                            width: parent.width
                                                            text: "Usar o " + page.localKindLabel(lmRow.modelData.kind) + " deste computador"
                                                            color: Theme.white
                                                            font.pixelSize: Theme.fontBody
                                                            font.weight: Font.Medium
                                                            elide: Text.ElideRight
                                                        }

                                                        Text {
                                                            width: parent.width
                                                            text: lmRow.count === 0
                                                                  ? "Servidor ativo · nenhum modelo baixado ainda"
                                                                  : (lmRow.count === 1 ? "1 modelo: " : lmRow.count + " modelos: ") + (lmRow.modelData.models || []).slice(0, 3).join(", ")
                                                            color: Theme.text3
                                                            font.pixelSize: Theme.fontSmall
                                                            elide: Text.ElideRight
                                                        }
                                                    }

                                                    Row {
                                                        id: lmAction
                                                        anchors { right: parent.right; verticalCenter: parent.verticalCenter }
                                                        spacing: Theme.space2

                                                        Row {
                                                            visible: !!lmRow.existing
                                                            spacing: Theme.space1
                                                            anchors.verticalCenter: parent.verticalCenter
                                                            Dot { kind: "ok"; anchors.verticalCenter: parent.verticalCenter }
                                                            Text {
                                                                text: "Conectado"
                                                                color: Theme.ok
                                                                font.pixelSize: Theme.fontSmall
                                                                font.weight: Font.Medium
                                                                anchors.verticalCenter: parent.verticalCenter
                                                            }
                                                        }

                                                        GhostButton {
                                                            visible: !lmRow.existing
                                                            small: true
                                                            icon: "plus"
                                                            enabled: !lmRow.isConnecting
                                                            text: lmRow.isConnecting ? "Conectando…" : "Conectar"
                                                            onClicked: page.connectLocalModel(node.modelData, lmRow.modelData)
                                                        }
                                                    }
                                                }
                                            }
                                        }
                                    }
                                }
                            }

                            // Computer-wide shared folder (AVORA_SHARED).
                            Column {
                                width: parent.width
                                spacing: Theme.space2

                                Overline { text: "PASTA COMPARTILHADA" }

                                Rectangle {
                                    width: parent.width
                                    height: 36
                                    radius: Theme.radiusMd
                                    color: Theme.well
                                    border.width: 1
                                    border.color: Theme.line

                                    Row {
                                        anchors {
                                            left: parent.left
                                            leftMargin: Theme.space3
                                            right: parent.right
                                            rightMargin: Theme.space3
                                            verticalCenter: parent.verticalCenter
                                        }
                                        spacing: Theme.space2

                                        Icon {
                                            name: "folder"
                                            size: 16
                                            color: Theme.text3
                                            anchors.verticalCenter: parent.verticalCenter
                                        }

                                        Text {
                                            width: parent.width - 24
                                            text: node.sharedPath
                                            color: Theme.text2
                                            font.family: Theme.mono
                                            font.pixelSize: Theme.fontSmall
                                            elide: Text.ElideMiddle
                                            anchors.verticalCenter: parent.verticalCenter
                                        }
                                    }
                                }

                                Text {
                                    width: parent.width
                                    wrapMode: Text.Wrap
                                    text: "Dividida entre os agentes deste computador."
                                    color: Theme.text4
                                    font.pixelSize: Theme.fontSmall
                                }
                            }

                            // Revoke action.
                            Item {
                                width: parent.width
                                height: 30

                                GhostButton {
                                    anchors.right: parent.right
                                    small: true
                                    danger: true
                                    text: page.confirm === node.modelData.id ? "Confirmar: cortar acesso" : "Revogar"
                                    onClicked: page.revoke(node.modelData.id)
                                }
                            }
                        }
                    }
                }
            }

            Column {
                visible: page.revoked.length > 0
                width: parent.width
                spacing: Theme.space3

                Overline { text: "REVOGADOS" }

                Card {
                    width: parent.width
                    height: revCol.implicitHeight + 2 * padding
                    padding: Theme.space2

                    Column {
                        id: revCol
                        width: parent.width

                        Repeater {
                            model: page.revoked
                            delegate: Item {
                                required property var modelData
                                width: revCol.width
                                height: 42

                                Row {
                                    x: Theme.space4
                                    anchors.verticalCenter: parent.verticalCenter
                                    spacing: Theme.space2
                                    Dot { kind: "bad"; anchors.verticalCenter: parent.verticalCenter }
                                    Text {
                                        text: modelData.name + "  ·  " + U.kindLabel(modelData.kind)
                                        color: Theme.text2
                                        font.pixelSize: Theme.fontBody
                                    }
                                }

                                Text {
                                    anchors {
                                        right: parent.right
                                        rightMargin: Theme.space4
                                        verticalCenter: parent.verticalCenter
                                    }
                                    text: U.ago(modelData.revoked_at)
                                    color: Theme.text4
                                    font.family: Theme.mono
                                    font.pixelSize: Theme.fontSmall
                                }
                            }
                        }
                    }
                }
            }
        }
    }
}
