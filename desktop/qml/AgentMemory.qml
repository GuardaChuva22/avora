import QtQuick
import Avora

// Agent or workspace team memory panel: search, filter by kind and verification/archive
// status, confirm unverified hints, pin ("Sempre no contexto"), edit, archive or delete.
Column {
    id: box
    property var agent: null
    property bool teamMode: false
    property bool active: false
    property var memories: []
    property bool loading: false
    property bool loadedOnce: false
    property bool showAll: false
    property bool adding: false
    property string newKind: "fact"
    property string query: ""
    property string kindFilter: ""
    property string stateFilter: "active" // active | verified | unverified | pinned | archived
    property var editingId: null
    property var confirmId: null
    property string error: ""

    readonly property int folded: 6
    readonly property bool hasFilters: query.trim() !== "" || kindFilter !== "" || stateFilter !== "active"
    spacing: Theme.space3

    readonly property var kindOptions: [
        { value: "", label: "Todos" },
        { value: "decision", label: "Decisão" },
        { value: "fact", label: "Fato" },
        { value: "preference", label: "Preferência" },
        { value: "lesson", label: "Lição" },
        { value: "rule", label: "Regra" }
    ]
    readonly property var createKinds: [
        { value: "fact", label: "Fato" },
        { value: "preference", label: "Preferência" },
        { value: "decision", label: "Decisão" },
        { value: "rule", label: "Regra" },
        { value: "lesson", label: "Lição" }
    ]
    readonly property var stateOptions: [
        { value: "active", label: "Ativas" },
        { value: "verified", label: "Verificadas" },
        { value: "unverified", label: "Não verificadas" },
        { value: "pinned", label: "Fixadas" },
        { value: "archived", label: "Arquivadas" }
    ]

    function kindLabel(k) {
        switch (k) {
        case "decision": return "Decisão";
        case "preference": return "Preferência";
        case "lesson": return "Lição";
        case "rule": return "Regra";
        default: return "Fato";
        }
    }

    function buildPath() {
        const params = [];
        params.push("scope=" + (teamMode ? "team" : "agent"));
        if (!teamMode) {
            if (!agent || !agent.id)
                return "";
            params.push("agent=" + encodeURIComponent(agent.id));
        }
        if (query.trim() !== "")
            params.push("q=" + encodeURIComponent(query.trim()));
        if (kindFilter !== "")
            params.push("kind=" + encodeURIComponent(kindFilter));
        if (stateFilter === "verified")
            params.push("verified=1");
        else if (stateFilter === "unverified")
            params.push("verified=0");
        else if (stateFilter === "pinned")
            params.push("pinned=1");
        else if (stateFilter === "archived")
            params.push("archived=1");
        return "/v1/memories?" + params.join("&");
    }

    function load() {
        const path = buildPath();
        if (path === "")
            return;
        const targetId = teamMode ? "__team__" : agent.id;
        loading = !loadedOnce;
        Api.get(path, (err, list) => {
            box.loading = false;
            box.loadedOnce = true;
            const nowTarget = box.teamMode ? "__team__" : (box.agent ? box.agent.id : "");
            if (nowTarget !== targetId)
                return;
            if (err) {
                box.error = err;
                return;
            }
            box.error = "";
            box.memories = list || [];
        });
    }

    function done(err) {
        error = err || "";
        if (!err) {
            adding = false;
            editingId = null;
        }
        load();
    }

    function addMemory(text, kindOverride) {
        const clean = (text || "").trim();
        if (clean === "")
            return;
        const k = kindOverride || newKind || "fact";
        if (teamMode) {
            Api.post("/v1/memories", {
                scope: "team",
                kind: k,
                content: clean
            }, err => box.done(err));
            return;
        }
        if (!agent)
            return;
        Api.post("/v1/agents/" + agent.id + "/memories", {
            kind: k,
            content: clean
        }, err => box.done(err));
    }

    function save(m, text) {
        const clean = (text || "").trim();
        if (clean === "" || clean === m.content) {
            editingId = null;
            return;
        }
        Api.patch("/v1/memories/" + m.id, { content: clean }, err => box.done(err));
    }

    function patchMemory(m, body) {
        Api.patch("/v1/memories/" + m.id, body, err => box.done(err));
    }

    function remove(m) {
        if (confirmId !== m.id) {
            confirmId = m.id;
            confirmTimer.restart();
            return;
        }
        confirmId = null;
        Api.del("/v1/memories/" + m.id, err => box.done(err));
    }

    property string shownId: ""
    onAgentChanged: {
        if (teamMode)
            return;
        const id = agent ? agent.id : "";
        if (id === shownId)
            return;
        shownId = id;
        memories = [];
        loadedOnce = false;
        adding = false;
        editingId = null;
        error = "";
        load();
    }
    onActiveChanged: if (active) load()
    onKindFilterChanged: if (active || teamMode) load()
    onStateFilterChanged: if (active || teamMode) load()
    Component.onCompleted: if (teamMode || active) load()

    Connections {
        target: Api
        function onWorkspaceChanged() {
            box.memories = [];
            box.loadedOnce = false;
            if (box.active || box.teamMode)
                box.load();
        }
        function onLiveEvent(ev) {
            if (!ev || (!box.active && !box.teamMode))
                return;
            if (ev.type === "agent.memory_saved" || ev.type === "agent.memory_deleted")
                box.load();
        }
    }

    Timer {
        id: searchDebounce
        interval: 220
        onTriggered: box.load()
    }
    Timer {
        interval: 10000
        repeat: true
        running: (box.active || box.teamMode) && !Api.live && box.editingId === null && !box.adding
        onTriggered: box.load()
    }
    Timer {
        id: confirmTimer
        interval: 3000
        onTriggered: box.confirmId = null
    }

    // Header row: count and Nova button.
    Item {
        width: parent.width
        height: 30
        Overline {
            anchors.verticalCenter: parent.verticalCenter
            text: (box.teamMode ? "MEMÓRIAS DO TIME" : "MEMÓRIA") + (box.memories.length ? "  ·  " + box.memories.length : "")
        }
        GhostButton {
            anchors { right: parent.right; verticalCenter: parent.verticalCenter }
            visible: !box.adding
            small: true
            icon: "plus"
            text: "Nova"
            onClicked: {
                box.editingId = null;
                box.adding = true;
                newField.text = "";
                newField.area.forceActiveFocus();
            }
        }
    }

    // Add memory inline composer with kind picker.
    Card {
        visible: box.adding
        width: parent.width
        height: addCol.implicitHeight + 2 * padding
        padding: Theme.space3
        radius: Theme.radiusMd

        Column {
            id: addCol
            width: parent.width
            spacing: Theme.space2

            Flow {
                width: parent.width
                spacing: Theme.space1
                Repeater {
                    model: box.createKinds
                    delegate: Pill {
                        required property var modelData
                        text: modelData.label
                        on: box.newKind === modelData.value
                        onHit: box.newKind = modelData.value
                    }
                }
            }
            AreaField {
                id: newField
                width: parent.width
                minHeight: 44
                maxHeight: 140
                placeholderText: box.teamMode
                    ? "Algo que todo o time deste espaço deve saber"
                    : "Algo que " + (box.agent ? box.agent.name : "o funcionário") + " deve saber"
                onSubmitted: box.addMemory(text, box.newKind)
                Keys.onEscapePressed: box.adding = false
            }
            Row {
                spacing: Theme.space2
                GhostButton {
                    small: true
                    icon: "check"
                    text: "Guardar"
                    onClicked: box.addMemory(newField.text, box.newKind)
                }
                GhostButton {
                    small: true
                    text: "Cancelar"
                    onClicked: box.adding = false
                }
            }
        }
    }

    // Search and filters.
    Field {
        id: searchField
        width: parent.width
        placeholderText: box.teamMode ? "Buscar na memória do time…" : "Buscar memórias…"
        onTextChanged: {
            box.query = text;
            searchDebounce.restart();
        }
    }

    Flow {
        width: parent.width
        spacing: Theme.space1
        Repeater {
            model: box.kindOptions
            delegate: Pill {
                required property var modelData
                text: modelData.label
                on: box.kindFilter === modelData.value
                onHit: box.kindFilter = modelData.value
            }
        }
    }

    Flow {
        width: parent.width
        spacing: Theme.space1
        Repeater {
            model: box.stateOptions
            delegate: Pill {
                required property var modelData
                text: modelData.label
                dim: modelData.value === "archived"
                on: box.stateFilter === modelData.value
                onHit: box.stateFilter = modelData.value
            }
        }
    }

    // Loading state.
    Text {
        visible: box.loading && box.memories.length === 0
        width: parent.width
        text: "Carregando memórias…"
        color: Theme.text4
        font.pixelSize: Theme.fontSmall
    }

    // Empty state.
    Text {
        visible: !box.loading && box.memories.length === 0 && !box.adding
        width: parent.width
        wrapMode: Text.Wrap
        text: box.hasFilters
            ? "Nenhuma memória encontrada para este filtro."
            : box.teamMode
                ? "Nada na memória do time ainda. Decisões, regras e fatos guardados aqui entram no contexto de todos os agentes deste espaço."
                : "Nada ainda. " + (box.agent ? box.agent.name : "O agente") + " guarda sozinho o que você contar de importante, ou aprende quando é corrigido."
        color: Theme.text4
        font.pixelSize: Theme.fontSmall
    }

    // Memory rows.
    Repeater {
        model: box.showAll || box.hasFilters ? box.memories : box.memories.slice(0, box.folded)
        delegate: Rectangle {
            id: row
            required property var modelData
            readonly property bool editing: box.editingId === modelData.id
            readonly property bool isArchived: !!modelData.archived_at
            width: box.width
            height: editing ? editCol.implicitHeight + 2 * Theme.space3 : innerCol.implicitHeight + 2 * Theme.space3
            radius: Theme.radiusMd
            color: rowArea.containsMouse ? Theme.cardHover : Theme.card
            border.width: 1
            border.color: modelData.pinned ? Theme.lineStrong : Theme.line
            Behavior on color { ColorAnimation { duration: Theme.fast } }

            MouseArea {
                id: rowArea
                anchors.fill: parent
                hoverEnabled: true
                acceptedButtons: Qt.NoButton
            }

            Column {
                id: innerCol
                visible: !row.editing
                x: Theme.space3
                y: Theme.space3
                width: row.width - 2 * Theme.space3
                spacing: Theme.space2

                // Top metadata & quick actions bar.
                Item {
                    width: parent.width
                    height: Math.max(metaFlow.implicitHeight, toolsRow.implicitHeight)

                    Flow {
                        id: metaFlow
                        width: parent.width - toolsRow.width - Theme.space2
                        spacing: Theme.space2

                        Rectangle {
                            height: 20
                            width: kindTxt.implicitWidth + 2 * Theme.space2
                            radius: height / 2
                            color: row.modelData.kind === "rule" ? Theme.wash4 : Theme.wash2
                            border.width: 1
                            border.color: Theme.line
                            Text {
                                id: kindTxt
                                anchors.centerIn: parent
                                text: box.kindLabel(row.modelData.kind)
                                color: Theme.text2
                                font.pixelSize: Theme.fontTiny
                                font.weight: Font.DemiBold
                            }
                        }

                        Row {
                            height: 20
                            spacing: Theme.space1
                            Icon {
                                anchors.verticalCenter: parent.verticalCenter
                                name: row.modelData.verified ? "check" : "alert"
                                size: 12
                                color: row.modelData.verified ? Theme.ok : Theme.warn
                            }
                            Text {
                                anchors.verticalCenter: parent.verticalCenter
                                text: row.modelData.verified ? "Verificado" : "Não verificado · dica"
                                color: row.modelData.verified ? Theme.ok : Theme.warn
                                font.pixelSize: Theme.fontCaption
                            }
                        }

                        Row {
                            visible: !!row.modelData.pinned
                            height: 20
                            spacing: Theme.space1
                            Icon {
                                anchors.verticalCenter: parent.verticalCenter
                                name: "pin"
                                size: 12
                                color: Theme.white
                            }
                            Text {
                                anchors.verticalCenter: parent.verticalCenter
                                text: "Sempre no contexto"
                                color: Theme.white
                                font.pixelSize: Theme.fontCaption
                                font.weight: Font.Medium
                            }
                        }

                        Text {
                            visible: (row.modelData.occurrences || 1) > 1
                            height: 20
                            verticalAlignment: Text.AlignVCenter
                            text: row.modelData.occurrences + "× repetida"
                            color: Theme.text3
                            font.pixelSize: Theme.fontCaption
                        }

                        Text {
                            visible: !!row.modelData.source
                            height: 20
                            verticalAlignment: Text.AlignVCenter
                            text: "origem: " + row.modelData.source
                            color: Theme.text4
                            font.pixelSize: Theme.fontCaption
                        }
                    }

                    Row {
                        id: toolsRow
                        anchors { right: parent.right; top: parent.top }
                        spacing: Theme.space1

                        GhostButton {
                            visible: !row.modelData.verified && !row.isArchived
                            small: true
                            icon: "check"
                            text: "Confirmar"
                            onClicked: box.patchMemory(row.modelData, { verified: true })
                        }
                        GhostButton {
                            visible: !row.isArchived
                            small: true
                            icon: "pin"
                            text: row.modelData.pinned ? "Desafixar" : "Fixar"
                            onClicked: box.patchMemory(row.modelData, { pinned: !row.modelData.pinned })
                        }
                        RowTool {
                            visible: !row.isArchived
                            icon: "pen"
                            tip: "Editar"
                            onHit: {
                                box.adding = false;
                                box.editingId = row.modelData.id;
                                editField.text = row.modelData.content;
                                editField.area.forceActiveFocus();
                            }
                        }
                        GhostButton {
                            small: true
                            icon: row.isArchived ? "restore" : "archive"
                            text: row.isArchived ? "Restaurar" : "Arquivar"
                            onClicked: box.patchMemory(row.modelData, { archived: !row.isArchived })
                        }
                        RowTool {
                            icon: "trash"
                            tip: "Apagar"
                            danger: box.confirmId === row.modelData.id
                            onHit: box.remove(row.modelData)
                        }
                    }
                }

                Text {
                    width: parent.width
                    text: row.modelData.content
                    color: row.isArchived ? Theme.text3 : Theme.text
                    font.pixelSize: Theme.fontBody
                    lineHeight: 1.3
                    wrapMode: Text.Wrap
                }
            }

            Column {
                id: editCol
                visible: row.editing
                x: Theme.space3
                y: Theme.space3
                width: row.width - 2 * Theme.space3
                spacing: Theme.space2

                AreaField {
                    id: editField
                    width: parent.width
                    minHeight: 44
                    maxHeight: 160
                    onSubmitted: box.save(row.modelData, text)
                    Keys.onEscapePressed: box.editingId = null
                }
                Row {
                    spacing: Theme.space2
                    GhostButton {
                        small: true
                        icon: "check"
                        text: "Salvar"
                        onClicked: box.save(row.modelData, editField.text)
                    }
                    GhostButton {
                        small: true
                        text: "Cancelar"
                        onClicked: box.editingId = null
                    }
                }
            }
        }
    }

    Text {
        visible: !box.hasFilters && box.memories.length > box.folded
        text: box.showAll ? "mostrar menos" : "ver todas (" + box.memories.length + ")"
        color: moreArea.containsMouse ? Theme.white : Theme.text3
        font.pixelSize: Theme.fontSmall
        font.underline: moreArea.containsMouse
        MouseArea {
            id: moreArea
            anchors.fill: parent
            anchors.margins: -Theme.space1
            hoverEnabled: true
            cursorShape: Qt.PointingHandCursor
            onClicked: box.showAll = !box.showAll
        }
    }

    Text {
        visible: box.confirmId !== null
        text: "Clique de novo na lixeira para apagar de vez."
        color: Theme.bad
        font.pixelSize: Theme.fontSmall
    }

    ErrorBanner {
        width: parent.width
        text: box.error
    }
}
