import QtQuick
import QtQuick.Controls.Basic
import QtQuick.Dialogs
import Avora

// Create or edit a project: name, color, the main folder (on this PC or
// on the VPS, where its AGENTS.md lives), extra folders and the
// instructions every agent of the project follows.
Sheet {
    id: ed
    property var project: null // null = new project
    property string error: ""
    property bool saving: false
    property bool confirmDelete: false
    property string note: ""
    signal saved(var project)
    signal deleted(string id)

    property var nodes: []
    property string color: "#7fd1ff"
    property string mainNode: ""
    property string mainPath: ""
    property var extras: [] // [{ node, path, label }]
    property string picking: "" // "main" or "extra"

    readonly property var swatches: Theme.palette
    readonly property var pc: nodes.find(n => n.kind === "windows") || null
    readonly property var vps: nodes.find(n => n.kind === "vps") || null
    readonly property bool mainOnPC: !!pc && mainNode === pc.id

    function nodeName(id) {
        const n = nodes.find(x => x.id === id);
        return !n ? id : n.kind === "vps" ? "VPS" : "Este PC";
    }

    function slug(s) {
        return s.normalize("NFD").replace(/[̀-ͯ]/g, "").toLowerCase().replace(/[^a-z0-9]+/g, "-").replace(/^-|-$/g, "").slice(0, 32);
    }

    function open(p) {
        project = p;
        error = "";
        note = "";
        saving = false;
        confirmDelete = false;
        nameField.text = p ? p.name : "";
        rules.text = p ? p.instructions : "";
        color = p && p.color ? p.color : swatches[Math.floor(Math.random() * swatches.length)];
        const folders = p && p.folders ? p.folders : [];
        mainNode = folders.length ? folders[0].node : "";
        mainPath = folders.length ? folders[0].path : "";
        extras = folders.slice(1).map(f => Object.assign({}, f));
        shown = true;
        nameField.forceActiveFocus();
        Api.get("/v1/nodes", (err, data) => {
            if (err)
                return;
            nodes = data.filter(n => !n.revoked_at);
            if (mainNode === "")
                mainNode = pc ? pc.id : vps ? vps.id : "";
            readLocal();
        });
    }

    // The AGENTS.md on this PC wins over the stored copy: the owner (or
    // Claude Code) may have edited it there.
    function readLocal() {
        if (!mainOnPC || mainPath === "")
            return;
        const text = Api.readAgentsFile(mainPath);
        if (text !== "" && text !== rules.text.trim()) {
            rules.text = text;
            note = "Lido do AGENTS.md da pasta.";
        }
    }

    function picked(url) {
        const path = Api.localPath(url);
        if (path === "")
            return;
        if (!Api.allowFolder(path)) {
            error = "Não consegui liberar essa pasta para o Avora deste PC.";
            return;
        }
        if (picking === "main") {
            mainPath = path;
            readLocal();
            if (nameField.text.trim() === "")
                nameField.text = path.split("\\").pop();
        } else {
            extras = extras.concat([{ node: pc.id, path: path, label: path.split("\\").pop() }]);
        }
    }

    function folders() {
        const out = [];
        let main = mainPath.trim();
        if (!mainOnPC && main === "" && nameField.text.trim() !== "")
            main = "projects/" + slug(nameField.text);
        if (mainNode !== "" && main !== "")
            out.push({ node: mainNode, path: main, label: "" });
        for (const f of extras)
            if (f.path.trim() !== "")
                out.push({ node: f.node, path: f.path.trim(), label: (f.label || "").trim() });
        return out;
    }

    function save() {
        if (saving)
            return;
        const body = { name: nameField.text.trim(), color: color, instructions: rules.text.trim(), folders: folders() };
        if (body.name === "") {
            error = "Dê um nome ao projeto.";
            return;
        }
        if (mainOnPC && mainPath === "") {
            error = "Escolha a pasta principal no PC (ou mude para a VPS).";
            return;
        }
        saving = true;
        error = "";
        const done = (err, p) => {
            saving = false;
            if (err) {
                error = err;
                return;
            }
            for (const f of p.folders)
                if (pc && f.node === pc.id)
                    Api.allowFolder(f.path);
            if (p.folders.length && pc && p.folders[0].node === pc.id && body.instructions !== "")
                Api.writeAgentsFile(p.folders[0].path, body.instructions);
            shown = false;
            saved(p);
        };
        if (project)
            Api.put("/v1/projects/" + project.id, body, done);
        else
            Api.post("/v1/projects", body, done);
    }

    function remove() {
        if (!confirmDelete) {
            confirmDelete = true;
            return;
        }
        const id = project.id;
        Api.del("/v1/projects/" + id, err => {
            if (err) {
                error = err;
                return;
            }
            shown = false;
            deleted(id);
        });
    }

    FolderDialog {
        id: folderDialog
        title: ed.picking === "main" ? "Pasta principal do projeto" : "Outra pasta do projeto"
        onAccepted: ed.picked(selectedFolder)
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
                name: "folder"
                size: 20
                color: ed.color
            }
        }
        Column {
            anchors.verticalCenter: parent.verticalCenter
            width: ed.form.width - 58
            spacing: 3
            Text {
                text: ed.project ? ed.project.name : "Novo projeto"
                color: Theme.white
                font.family: Theme.display
                font.pixelSize: Theme.fontHeading
                font.weight: Font.DemiBold
            }
            Text {
                width: parent.width
                wrapMode: Text.Wrap
                text: "Os funcionários do projeto seguem as instruções e só falam entre si e com os universais."
                color: Theme.text3
                font.pixelSize: Theme.fontBody
            }
        }
    }

    Column {
        width: parent.width
        spacing: 8
        Overline { text: "NOME" }
        Field { id: nameField; width: parent.width; placeholderText: "Ex.: Trabalho"; maximumLength: 40 }
    }

    Column {
        spacing: 10
        Overline { text: "COR" }
        Row {
            spacing: 10
            Repeater {
                model: ed.swatches
                delegate: Rectangle {
                    id: sw
                    required property string modelData
                    width: 26
                    height: 26
                    radius: Theme.radiusSm
                    color: "transparent"
                    border.width: ed.color === modelData ? 2 : 0
                    border.color: Theme.white
                    Rectangle {
                        anchors.centerIn: parent
                        width: 16
                        height: 16
                        radius: 5
                        color: sw.modelData
                    }
                    MouseArea {
                        anchors.fill: parent
                        cursorShape: Qt.PointingHandCursor
                        onClicked: ed.color = sw.modelData
                    }
                }
            }
        }
    }

    // Main folder: where AGENTS.md lives and Claude Code / Codex work.
    Column {
        width: parent.width
        spacing: 10
        Item {
            width: parent.width
            height: 30
            Overline { text: "PASTA PRINCIPAL"; anchors.verticalCenter: parent.verticalCenter }
            Segmented {
                visible: !!ed.pc && !!ed.vps
                anchors { right: parent.right; verticalCenter: parent.verticalCenter }
                implicitHeight: 30
                options: [{ value: ed.pc ? ed.pc.id : "", label: "Este PC" }, { value: ed.vps ? ed.vps.id : "", label: "VPS" }]
                value: ed.mainNode
                onActivated: v => {
                    if (v !== ed.mainNode) {
                        ed.mainNode = v;
                        ed.mainPath = "";
                    }
                }
            }
        }
        // On this PC: a folder picked in the system dialog.
        Rectangle {
            visible: ed.mainOnPC
            width: parent.width
            height: 46
            radius: Theme.radiusMd
            color: Theme.well
            border.width: 1
            border.color: Theme.line
            Icon {
                id: pcIcon
                x: 14
                anchors.verticalCenter: parent.verticalCenter
                name: "folder"
                size: 16
                color: ed.mainPath ? ed.color : Theme.text4
            }
            Text {
                anchors { left: pcIcon.right; leftMargin: 10; right: pickBtn.left; rightMargin: 10; verticalCenter: parent.verticalCenter }
                text: ed.mainPath || "Nenhuma pasta escolhida"
                color: ed.mainPath ? Theme.text : Theme.text4
                font.family: ed.mainPath ? Theme.mono : Theme.sans
                font.pixelSize: Theme.fontBody
                elide: Text.ElideMiddle
            }
            GhostButton {
                id: pickBtn
                anchors { right: parent.right; rightMargin: 7; verticalCenter: parent.verticalCenter }
                small: true
                text: ed.mainPath ? "Trocar" : "Escolher pasta"
                onClicked: {
                    ed.picking = "main";
                    folderDialog.open();
                }
            }
        }
        // On the VPS: a folder inside the agents' workspace.
        Field {
            visible: !ed.mainOnPC && ed.mainNode !== ""
            width: parent.width
            mono: true
            placeholderText: "projects/" + (ed.slug(nameField.text) || "nome")
            text: ed.mainPath
            onTextEdited: ed.mainPath = text
        }
        Text {
            visible: ed.mainNode === ""
            text: "Nenhum nó pareado: dá para criar o projeto e escolher a pasta depois."
            color: Theme.text4
            font.pixelSize: Theme.fontSmall
        }
    }

    Column {
        width: parent.width
        spacing: 10
        Overline { text: "OUTRAS PASTAS" }
        Repeater {
            model: ed.extras
            delegate: Rectangle {
                id: ex
                required property var modelData
                required property int index
                width: ed.form.width
                height: 46
                radius: Theme.radiusMd
                color: Theme.wash1
                border.width: 1
                border.color: Theme.line
                Field {
                    id: exLabel
                    x: 6
                    anchors.verticalCenter: parent.verticalCenter
                    width: 130
                    implicitHeight: 34
                    placeholderText: "Nome"
                    maximumLength: 40
                    text: ex.modelData.label || ""
                    onTextEdited: ed.extras[ex.index].label = text
                }
                Text {
                    anchors { left: exLabel.right; leftMargin: 12; right: exWhere.left; rightMargin: 10; verticalCenter: parent.verticalCenter }
                    text: ex.modelData.path
                    color: Theme.text2
                    font.family: Theme.mono
                    font.pixelSize: Theme.fontSmall
                    elide: Text.ElideMiddle
                    visible: ex.modelData.node !== (ed.vps ? ed.vps.id : "")
                }
                Field {
                    visible: ex.modelData.node === (ed.vps ? ed.vps.id : "")
                    anchors { left: exLabel.right; leftMargin: 8; right: exWhere.left; rightMargin: 8; verticalCenter: parent.verticalCenter }
                    implicitHeight: 34
                    mono: true
                    placeholderText: "projects/..."
                    text: ex.modelData.path
                    onTextEdited: ed.extras[ex.index].path = text
                }
                Text {
                    id: exWhere
                    anchors { right: exDel.left; rightMargin: 8; verticalCenter: parent.verticalCenter }
                    text: ed.nodeName(ex.modelData.node)
                    color: Theme.text4
                    font.pixelSize: Theme.fontCaption
                }
                RowTool {
                    id: exDel
                    anchors { right: parent.right; rightMargin: 10; verticalCenter: parent.verticalCenter }
                    icon: "close"
                    tip: "Tirar"
                    onHit: {
                        const next = ed.extras.slice();
                        next.splice(ex.index, 1);
                        ed.extras = next;
                    }
                }
            }
        }
        Row {
            spacing: 8
            visible: ed.extras.length < 7
            GhostButton {
                visible: !!ed.pc
                small: true
                icon: "plus"
                text: "Pasta do PC"
                onClicked: {
                    ed.picking = "extra";
                    folderDialog.open();
                }
            }
            GhostButton {
                visible: !!ed.vps
                small: true
                icon: "plus"
                text: "Pasta na VPS"
                onClicked: ed.extras = ed.extras.concat([{ node: ed.vps.id, path: "projects/" + (ed.slug(nameField.text) || "projeto") + "/", label: "" }])
            }
        }
    }

    Column {
        width: parent.width
        spacing: 8
        Item {
            width: parent.width
            height: 16
            Overline { text: "INSTRUÇÕES  ·  AGENTS.md" }
            Text {
                anchors.right: parent.right
                text: ed.note
                color: Theme.text3
                font.pixelSize: Theme.fontCaption
            }
        }
        AreaField {
            id: rules
            width: parent.width
            minHeight: 170
            maxHeight: 340
            enterSubmits: false
            placeholderText: "O que é o projeto, o objetivo, as regras, o que cada um faz, onde ficam as coisas. Fica no AGENTS.md da pasta principal; os funcionários do projeto, o Claude Code e o Codex leem antes de trabalhar."
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
                text: ed.project ? "Salvar" : "Criar projeto"
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
            visible: !!ed.project
            anchors { right: parent.right; verticalCenter: parent.verticalCenter }
            danger: true
            icon: "trash"
            text: ed.confirmDelete ? "Confirmar: os funcionários viram universais" : "Excluir"
            onClicked: ed.remove()
        }
    }
}
