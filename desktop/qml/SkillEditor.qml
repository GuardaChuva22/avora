import QtQuick
import QtQuick.Controls.Basic
import Avora

// Create, edit or import (paste a SKILL.md) a skill: name, when to use it,
// which project sees it, and the steps.
Sheet {
    id: ed
    property var skill: null // null = new
    property bool importing: false
    property var projects: []
    property string projectId: ""
    property bool saving: false
    property bool confirmDelete: false
    property string error: ""
    signal changed()

    function open(s, paste) {
        skill = s;
        importing = !s && paste;
        error = "";
        saving = false;
        confirmDelete = false;
        nameField.text = s ? s.name : "";
        descField.text = s ? s.description : "";
        descField.cursorPosition = 0;
        body.text = s ? s.body : "";
        markdown.text = "";
        projectId = s ? s.project_id : "";
        shown = true;
        if (importing)
            markdown.area.forceActiveFocus();
        else if (!s)
            nameField.forceActiveFocus();
    }

    function save() {
        if (saving)
            return;
        const req = importing ? { markdown: markdown.text, project_id: projectId }
                              : { name: nameField.text.trim(), description: descField.text.trim(), body: body.text.trim(), project_id: projectId };
        saving = true;
        error = "";
        const done = err => {
            saving = false;
            if (err) {
                error = err;
                return;
            }
            shown = false;
            changed();
        };
        if (skill)
            Api.put("/v1/skills/" + skill.id, req, done);
        else
            Api.post("/v1/skills", req, done);
    }

    function remove() {
        if (!confirmDelete) {
            confirmDelete = true;
            return;
        }
        Api.del("/v1/skills/" + skill.id, err => {
            if (err) {
                error = err;
                return;
            }
            shown = false;
            changed();
        });
    }



    Row {
        spacing: 14
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
                name: "book"
                size: 20
                color: Theme.text
            }
        }
        Column {
            anchors.verticalCenter: parent.verticalCenter
            width: ed.form.width - 58
            spacing: 3
            Text {
                text: ed.skill ? ed.skill.name : ed.importing ? "Importar skill" : "Nova skill"
                color: Theme.white
                font.family: ed.skill ? Theme.mono : Theme.display
                font.pixelSize: Theme.fontHeading
                font.weight: Font.DemiBold
            }
            Text {
                width: parent.width
                wrapMode: Text.Wrap
                text: ed.importing ? "Cole um SKILL.md inteiro: o cabeçalho com name e description, e os passos."
                                   : "A descrição diz quando usar; os passos, o que fazer e com quais ferramentas."
                color: Theme.text3
                font.pixelSize: Theme.fontBody
            }
        }
    }

    Column {
        visible: !ed.importing
        width: parent.width
        spacing: 8
        Overline { text: "NOME" }
        Field {
            id: nameField
            width: parent.width
            mono: true
            enabled: !ed.skill
            placeholderText: "ex.: relatorio-mensal"
            maximumLength: 32
        }
    }

    Column {
        visible: !ed.importing
        width: parent.width
        spacing: 8
        Overline { text: "QUANDO USAR" }
        Field {
            id: descField
            width: parent.width
            placeholderText: "ex.: Fechar as contas do mês e mandar o resumo"
            maximumLength: 1024
        }
    }

    Column {
        width: parent.width
        spacing: 10
        Overline { text: "QUEM VÊ" }
        Flow {
            width: parent.width
            spacing: 8
            Repeater {
                model: [{ id: "", name: "Todo o time", color: "" }].concat(ed.projects)
                delegate: Rectangle {
                    id: opt
                    required property var modelData
                    readonly property bool on: ed.projectId === modelData.id
                    height: 32
                    width: optText.implicitWidth + (modelData.color ? 38 : 26)
                    radius: Theme.radiusSm
                    color: on ? Theme.active : optHov.hovered ? Theme.hover : "transparent"
                    border.width: 1
                    border.color: on ? Theme.lineStrong : Theme.line
                    Rectangle {
                        visible: !!opt.modelData.color
                        x: 12
                        anchors.verticalCenter: parent.verticalCenter
                        width: 8
                        height: 8
                        radius: 3
                        color: opt.modelData.color || "transparent"
                    }
                    Text {
                        id: optText
                        x: opt.modelData.color ? 26 : 13
                        anchors.verticalCenter: parent.verticalCenter
                        text: opt.modelData.name
                        color: opt.on ? Theme.white : Theme.text2
                        font.pixelSize: Theme.fontBody
                    }
                    HoverHandler { id: optHov; cursorShape: Qt.PointingHandCursor }
                    TapHandler { onTapped: ed.projectId = opt.modelData.id }
                }
            }
        }
    }

    Column {
        width: parent.width
        spacing: 8
        Overline { text: ed.importing ? "SKILL.md" : "PASSOS" }
        AreaField {
            id: body
            visible: !ed.importing
            width: parent.width
            minHeight: 200
            maxHeight: 380
            enterSubmits: false
            placeholderText: "1. O que fazer primeiro (e com qual ferramenta)\n2. ...\n3. O que entregar no fim"
        }
        AreaField {
            id: markdown
            visible: ed.importing
            width: parent.width
            minHeight: 240
            maxHeight: 400
            enterSubmits: false
            placeholderText: "---\nname: relatorio-mensal\ndescription: Fecha as contas do mês\n---\n\n1. ..."
        }
    }

    ErrorBanner {
        width: parent.width
        text: ed.error
    }

    Item {
        width: parent.width
        height: 40
        Row {
            spacing: 10
            anchors.verticalCenter: parent.verticalCenter
            PrimaryButton {
                text: ed.skill ? "Salvar" : ed.importing ? "Importar" : "Criar skill"
                icon: "check"
                busy: ed.saving
                onClicked: ed.save()
            }
            GhostButton {
                text: "Cancelar"
                onClicked: ed.shown = false
            }
        }
        GhostButton {
            visible: !!ed.skill
            anchors { right: parent.right; verticalCenter: parent.verticalCenter }
            danger: true
            icon: "trash"
            text: ed.confirmDelete ? "Confirmar exclusão" : "Excluir"
            onClicked: ed.remove()
        }
    }
}
