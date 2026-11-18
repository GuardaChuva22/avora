import QtQuick
import Avora

// Create, rename, color, pick icon/leader, reorder by dragging, or delete
// sectors in the active workspace.
Sheet {
    id: ed
    property var agents: []
    property var sectors: []
    property var selected: null // null = creating a new sector
    property string pickedColor: Theme.palette[0]
    property string pickedIcon: "users"
    property string pickedLead: ""
    property bool loading: false
    property bool saving: false
    property bool confirmDelete: false
    property string error: ""
    property int draggingIndex: -1

    signal changed

    readonly property var swatches: Theme.palette
    readonly property var iconChoices: [
        "users", "code", "spark", "target", "globe", "book",
        "bolt", "folder", "tasks", "terminal", "chat", "pulse"
    ]

    function open(initialSector) {
        error = "";
        saving = false;
        confirmDelete = false;
        draggingIndex = -1;
        shown = true;
        loadAll(initialSector || null);
    }

    function loadAll(selectAfter) {
        loading = true;
        Api.get("/v1/sectors", (err, list) => {
            ed.loading = false;
            if (err) {
                ed.error = err;
                return;
            }
            ed.sectors = list || [];
            if (selectAfter && selectAfter.id) {
                const found = ed.sectors.find(s => s.id === selectAfter.id) || null;
                ed.selectSector(found);
            } else if (ed.selected && ed.selected.id) {
                const keep = ed.sectors.find(s => s.id === ed.selected.id) || null;
                ed.selectSector(keep);
            } else {
                ed.selectSector(null);
            }
        });
        Api.get("/v1/agents", (err, list) => {
            if (!err && list)
                ed.agents = list;
        });
    }

    function selectSector(sec) {
        selected = sec;
        confirmDelete = false;
        error = "";
        if (sec) {
            nameField.text = sec.name || "";
            pickedColor = sec.color || swatches[0];
            pickedIcon = sec.icon || "users";
            pickedLead = sec.lead_agent_id || "";
        } else {
            nameField.text = "";
            pickedColor = swatches[ed.sectors.length % swatches.length];
            pickedIcon = "users";
            pickedLead = "";
        }
    }

    function agentName(id) {
        if (!id)
            return "Sem líder";
        const a = (agents || []).find(x => x.id === id);
        return a ? a.name : id;
    }

    function moveSector(fromIdx, toIdx) {
        if (fromIdx < 0 || toIdx < 0 || fromIdx >= sectors.length || toIdx >= sectors.length || fromIdx === toIdx)
            return;
        const next = sectors.slice();
        const moved = next.splice(fromIdx, 1)[0];
        next.splice(toIdx, 0, moved);
        sectors = next;
    }

    function persistOrder() {
        if (sectors.length === 0)
            return;
        const ids = sectors.map(s => s.id);
        Api.post("/v1/sectors/reorder", { ids: ids }, (err, list) => {
            if (err) {
                ed.error = err;
                return;
            }
            if (list)
                ed.sectors = list;
            ed.changed();
        });
    }

    function save() {
        if (saving)
            return;
        const cleanName = nameField.text.trim();
        if (cleanName === "") {
            error = "Dê um nome ao setor.";
            return;
        }
        saving = true;
        error = "";
        const body = {
            name: cleanName,
            color: pickedColor,
            icon: pickedIcon,
            lead_agent_id: pickedLead
        };
        const finish = (err, savedSec) => {
            ed.saving = false;
            if (err) {
                ed.error = err;
                return;
            }
            ed.changed();
            ed.loadAll(savedSec || null);
        };
        if (selected && selected.id)
            Api.patch("/v1/sectors/" + encodeURIComponent(selected.id), body, finish);
        else
            Api.post("/v1/sectors", body, finish);
    }

    function removeSelected() {
        if (!selected || !selected.id)
            return;
        if (!confirmDelete) {
            confirmDelete = true;
            return;
        }
        const id = selected.id;
        Api.del("/v1/sectors/" + encodeURIComponent(id), err => {
            if (err) {
                ed.error = err;
                return;
            }
            ed.selected = null;
            ed.confirmDelete = false;
            ed.changed();
            ed.loadAll(null);
        });
    }

    // Header.
    Row {
        spacing: Theme.space3
        Rectangle {
            width: 44
            height: 44
            radius: Theme.radiusMd
            color: Theme.hover
            border.width: 1
            border.color: Theme.line
            anchors.verticalCenter: parent.verticalCenter
            Icon {
                anchors.centerIn: parent
                name: ed.pickedIcon
                size: 20
                color: ed.pickedColor
            }
        }
        Column {
            width: ed.form.width - 60
            anchors.verticalCenter: parent.verticalCenter
            spacing: Theme.space1
            Text {
                text: "Setores do time"
                color: Theme.white
                font.family: Theme.display
                font.pixelSize: Theme.fontHeading
                font.weight: Font.DemiBold
            }
            Text {
                width: parent.width
                wrapMode: Text.Wrap
                text: "Agrupe os funcionários por área, defina quem lidera cada setor e arraste para ordenar."
                color: Theme.text3
                font.pixelSize: Theme.fontBody
            }
        }
    }

    // Existing sectors list (click to edit, drag handle to reorder).
    Column {
        width: parent.width
        spacing: Theme.space2

        Item {
            width: parent.width
            height: 28
            Overline {
                anchors.verticalCenter: parent.verticalCenter
                text: "ORDEM DOS SETORES" + (ed.sectors.length ? "  ·  " + ed.sectors.length : "")
            }
            GhostButton {
                anchors { right: parent.right; verticalCenter: parent.verticalCenter }
                visible: ed.selected !== null
                small: true
                icon: "plus"
                text: "Novo setor"
                onClicked: {
                    ed.selectSector(null);
                    nameField.forceActiveFocus();
                }
            }
        }

        Text {
            visible: ed.loading && ed.sectors.length === 0
            text: "Carregando setores…"
            color: Theme.text4
            font.pixelSize: Theme.fontSmall
        }

        Text {
            visible: !ed.loading && ed.sectors.length === 0
            width: parent.width
            wrapMode: Text.Wrap
            text: "Nenhum setor criado ainda. Crie o primeiro abaixo para organizar a lista de conversas."
            color: Theme.text4
            font.pixelSize: Theme.fontSmall
        }

        Repeater {
            model: ed.sectors
            delegate: Rectangle {
                id: secRow
                required property var modelData
                required property int index
                readonly property bool isCurrent: !!ed.selected && ed.selected.id === modelData.id
                width: ed.form.width
                height: 44
                radius: Theme.radiusMd
                color: isCurrent ? Theme.wash3 : rowHover.containsMouse ? Theme.hover : Theme.well
                border.width: 1
                border.color: isCurrent ? Theme.focusLine : Theme.line
                Behavior on color { ColorAnimation { duration: Theme.fast } }

                MouseArea {
                    id: rowHover
                    anchors.fill: parent
                    hoverEnabled: true
                    cursorShape: Qt.PointingHandCursor
                    property real pressY: 0
                    onPressed: mouse => {
                        pressY = mouse.y;
                        ed.draggingIndex = secRow.index;
                    }
                    onPositionChanged: mouse => {
                        if (!pressed || ed.draggingIndex !== secRow.index)
                            return;
                        const dy = mouse.y - pressY;
                        if (dy > 28 && secRow.index < ed.sectors.length - 1) {
                            ed.moveSector(secRow.index, secRow.index + 1);
                            ed.draggingIndex = secRow.index + 1;
                        } else if (dy < -28 && secRow.index > 0) {
                            ed.moveSector(secRow.index, secRow.index - 1);
                            ed.draggingIndex = secRow.index - 1;
                        }
                    }
                    onReleased: {
                        if (ed.draggingIndex !== -1) {
                            ed.draggingIndex = -1;
                            ed.persistOrder();
                        }
                    }
                    onClicked: ed.selectSector(secRow.modelData)
                }

                Row {
                    anchors { left: parent.left; leftMargin: Theme.space3; verticalCenter: parent.verticalCenter }
                    spacing: Theme.space3

                    // Drag grip indicator.
                    Icon {
                        anchors.verticalCenter: parent.verticalCenter
                        name: "tasks"
                        size: 14
                        color: Theme.text4
                    }
                    Icon {
                        anchors.verticalCenter: parent.verticalCenter
                        name: secRow.modelData.icon || "users"
                        size: 16
                        color: secRow.modelData.color || Theme.text2
                    }
                    Text {
                        anchors.verticalCenter: parent.verticalCenter
                        text: secRow.modelData.name
                        color: Theme.white
                        font.pixelSize: Theme.fontBody
                        font.weight: Font.Medium
                    }
                    Text {
                        visible: !!secRow.modelData.lead_agent_id
                        anchors.verticalCenter: parent.verticalCenter
                        text: "líder: " + ed.agentName(secRow.modelData.lead_agent_id)
                        color: Theme.text3
                        font.pixelSize: Theme.fontSmall
                    }
                }

                Row {
                    anchors { right: parent.right; rightMargin: Theme.space2; verticalCenter: parent.verticalCenter }
                    spacing: Theme.space1

                    RowTool {
                        visible: secRow.index > 0
                        icon: "send"
                        tip: "Subir"
                        onHit: {
                            ed.moveSector(secRow.index, secRow.index - 1);
                            ed.persistOrder();
                        }
                    }
                    RowTool {
                        icon: "pen"
                        tip: "Editar"
                        onHit: ed.selectSector(secRow.modelData)
                    }
                }
            }
        }
    }

    // Create / edit form.
    Column {
        width: parent.width
        spacing: Theme.space3

        Overline { text: ed.selected ? "EDITAR SETOR" : "NOVO SETOR" }

        Column {
            width: parent.width
            spacing: Theme.space2
            Overline { text: "NOME" }
            Field {
                id: nameField
                width: parent.width
                maximumLength: 60
                placeholderText: "Ex.: Engenharia, Operações, Comercial"
                onAccepted: ed.save()
            }
        }

        Column {
            width: parent.width
            spacing: Theme.space2
            Overline { text: "COR" }
            Row {
                spacing: Theme.space2
                Repeater {
                    model: ed.swatches
                    delegate: Rectangle {
                        required property string modelData
                        width: 28
                        height: 28
                        radius: height / 2
                        color: modelData
                        border.width: ed.pickedColor === modelData ? 2 : 0
                        border.color: Theme.white
                        scale: ed.pickedColor === modelData ? 1.08 : 1
                        Behavior on scale { NumberAnimation { duration: Theme.fast } }
                        MouseArea {
                            anchors.fill: parent
                            cursorShape: Qt.PointingHandCursor
                            onClicked: ed.pickedColor = modelData
                        }
                    }
                }
            }
        }

        Column {
            width: parent.width
            spacing: Theme.space2
            Overline { text: "ÍCONE" }
            Flow {
                width: parent.width
                spacing: Theme.space2
                Repeater {
                    model: ed.iconChoices
                    delegate: Rectangle {
                        required property string modelData
                        width: 36
                        height: 36
                        radius: Theme.radiusSm
                        color: ed.pickedIcon === modelData ? Theme.wash4 : iconArea.containsMouse ? Theme.hover : Theme.well
                        border.width: 1
                        border.color: ed.pickedIcon === modelData ? Theme.focusLine : Theme.line
                        Icon {
                            anchors.centerIn: parent
                            name: modelData
                            size: 16
                            color: ed.pickedIcon === modelData ? ed.pickedColor : Theme.text2
                        }
                        MouseArea {
                            id: iconArea
                            anchors.fill: parent
                            hoverEnabled: true
                            cursorShape: Qt.PointingHandCursor
                            onClicked: ed.pickedIcon = modelData
                        }
                    }
                }
            }
        }

        Column {
            width: parent.width
            spacing: Theme.space2
            Overline { text: "LÍDER DO SETOR" }
            Flow {
                width: parent.width
                spacing: Theme.space2
                Pill {
                    text: "Sem líder"
                    on: ed.pickedLead === ""
                    onHit: ed.pickedLead = ""
                }
                Repeater {
                    model: ed.agents
                    delegate: Pill {
                        required property var modelData
                        text: modelData.name
                        on: ed.pickedLead === modelData.id
                        onHit: ed.pickedLead = modelData.id
                    }
                }
            }
        }

        ErrorBanner {
            width: parent.width
            text: ed.error
        }

        Item {
            width: parent.width
            height: 42

            GhostButton {
                visible: ed.selected !== null
                anchors { left: parent.left; verticalCenter: parent.verticalCenter }
                danger: true
                icon: "trash"
                text: ed.confirmDelete ? "Confirmar exclusão" : "Excluir setor"
                onClicked: ed.removeSelected()
            }

            Row {
                anchors { right: parent.right; verticalCenter: parent.verticalCenter }
                spacing: Theme.space2

                GhostButton {
                    text: "Fechar"
                    onClicked: ed.shown = false
                }
                PrimaryButton {
                    icon: "check"
                    busy: ed.saving
                    text: ed.selected ? "Salvar setor" : "Criar setor"
                    onClicked: ed.save()
                }
            }
        }
    }
}
