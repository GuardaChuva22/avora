import QtQuick
import QtQuick.Controls.Basic
import Avora
import "util.js" as U

// Create or edit a channel (D54): its name, what it is for, the project it
// sits under and who is in it. A member can answer on its own (the router
// may pick it) or only when someone writes @Nome. #time keeps everyone.
Sheet {
    id: ed
    property var channel: null // null = new channel
    property var agents: []
    property var projects: []
    property string error: ""
    property bool saving: false
    property bool confirmArchive: false
    signal saved(var channel)
    signal archived(string id)

    property bool everyone: false
    property string projectId: ""
    property var members: ({}) // agent id -> { on, mention_only }

    readonly property bool isTime: !!channel && channel.id === "time"
    readonly property int count: everyone ? agents.length : Object.keys(members).filter(k => members[k].on).length

    function open(c, project) {
        channel = c;
        error = "";
        saving = false;
        confirmArchive = false;
        nameField.text = c ? c.name : "";
        purposeField.text = c ? c.purpose || "" : "";
        everyone = c ? !!c.everyone : false;
        projectId = c ? c.project_id || "" : (project || "");
        const m = {};
        if (c)
            for (const x of c.members || [])
                m[x.agent_id] = { on: true, mention_only: !!x.mention_only };
        members = m;
        shown = true;
        nameField.forceActiveFocus();
    }

    function toggle(id) {
        const m = Object.assign({}, members);
        const cur = m[id] || { on: false, mention_only: false };
        m[id] = { on: !cur.on, mention_only: cur.mention_only };
        members = m;
    }

    function setMentionOnly(id, v) {
        const m = Object.assign({}, members);
        m[id] = { on: true, mention_only: v };
        members = m;
    }

    function save() {
        if (saving)
            return;
        const list = [];
        for (const a of agents) {
            const x = members[a.id];
            if (x && x.on)
                list.push({ agent_id: a.id, mention_only: x.mention_only });
            else if (everyone && x && x.mention_only)
                list.push({ agent_id: a.id, mention_only: true });
        }
        const body = { name: nameField.text, purpose: purposeField.text.trim(), project_id: projectId, everyone: everyone, members: list };
        if (body.name.trim() === "") {
            error = "Dê um nome ao canal.";
            return;
        }
        if (!everyone && list.length === 0) {
            error = "Ponha pelo menos um agente no canal.";
            return;
        }
        saving = true;
        error = "";
        const done = (err, c) => {
            saving = false;
            if (err) {
                error = err;
                return;
            }
            shown = false;
            saved(c);
        };
        if (channel)
            Api.patch("/v1/channels/" + channel.id, body, done);
        else
            Api.post("/v1/channels", body, done);
    }

    function archive() {
        if (!confirmArchive) {
            confirmArchive = true;
            return;
        }
        const id = channel.id;
        Api.patch("/v1/channels/" + id, { archived: true }, err => {
            if (err) {
                error = err;
                return;
            }
            shown = false;
            archived(id);
        });
    }



    // A pill that is on or off.
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
                name: "hash"
                size: 20
                color: Theme.white
            }
        }
        Column {
            anchors.verticalCenter: parent.verticalCenter
            width: ed.form.width - 58
            spacing: 3
            Text {
                text: ed.channel ? "#" + ed.channel.name : "Novo canal"
                color: Theme.white
                font.family: Theme.display
                font.pixelSize: Theme.fontHeading
                font.weight: Font.DemiBold
            }
            Text {
                width: parent.width
                wrapMode: Text.Wrap
                text: "Um grupo seu com os agentes que você escolher. Eles leem o propósito e ficam no assunto."
                color: Theme.text3
                font.pixelSize: Theme.fontBody
            }
        }
    }

    Column {
        width: parent.width
        spacing: 8
        Overline { text: "NOME" }
        Field {
            id: nameField
            width: parent.width
            enabled: !ed.isTime
            placeholderText: "Ex.: marketing, lançamento-app, finanças"
            maximumLength: 40
        }
    }

    Column {
        width: parent.width
        spacing: 8
        Overline { text: "PROPÓSITO" }
        AreaField {
            id: purposeField
            width: parent.width
            minHeight: 70
            maxHeight: 160
            enterSubmits: false
            placeholderText: "Para que serve o canal. Ex.: planejar e acompanhar o lançamento do app até dezembro."
        }
    }

    Column {
        visible: ed.projects.length > 0
        width: parent.width
        spacing: 10
        Overline { text: "PROJETO" }
        Flow {
            width: parent.width
            spacing: 8
            Pill {
                text: "Nenhum"
                on: ed.projectId === ""
                onHit: ed.projectId = ""
            }
            Repeater {
                model: ed.projects
                delegate: Pill {
                    required property var modelData
                    text: modelData.name
                    on: ed.projectId === modelData.id
                    onHit: ed.projectId = modelData.id
                }
            }
        }
    }

    Column {
        width: parent.width
        spacing: 10
        Item {
            width: parent.width
            height: 30
            Overline { text: "QUEM ESTÁ NO CANAL  ·  " + ed.count; anchors.verticalCenter: parent.verticalCenter }
            Pill {
                anchors { right: parent.right; verticalCenter: parent.verticalCenter }
                text: "Todos os agentes"
                on: ed.everyone
                visible: !ed.isTime
                onHit: ed.everyone = !ed.everyone
            }
        }
        Repeater {
            model: ed.agents
            delegate: Rectangle {
                id: row
                required property var modelData
                readonly property var state: ed.members[modelData.id] || { on: false, mention_only: false }
                readonly property bool on: ed.everyone || state.on
                width: ed.form.width
                height: 54
                radius: Theme.radiusMd
                color: rowArea.containsMouse ? Theme.hover : on ? Theme.wash1 : "transparent"
                border.width: on ? 1 : 0
                border.color: Theme.line
                MouseArea {
                    id: rowArea
                    anchors.fill: parent
                    hoverEnabled: true
                    cursorShape: ed.everyone ? Qt.ArrowCursor : Qt.PointingHandCursor
                    onClicked: if (!ed.everyone) ed.toggle(row.modelData.id)
                }
                Rectangle {
                    id: check
                    x: 14
                    anchors.verticalCenter: parent.verticalCenter
                    width: 18
                    height: 18
                    radius: Theme.radiusSm
                    color: row.on ? Theme.white : "transparent"
                    border.width: 1
                    border.color: row.on ? Theme.white : Theme.lineStrong
                    opacity: ed.everyone ? 0.5 : 1
                    Icon {
                        anchors.centerIn: parent
                        visible: row.on
                        name: "check"
                        size: 12
                        weight: 2.4
                        color: Theme.bg
                    }
                }
                Avatar {
                    id: face
                    anchors { left: check.right; leftMargin: 12; verticalCenter: parent.verticalCenter }
                    size: 32
                    name: row.modelData.name
                    photo: U.photo(row.modelData)
                    tint: row.modelData.color || Theme.text2
                }
                Column {
                    anchors { left: face.right; leftMargin: 10; right: answers.left; rightMargin: 10; verticalCenter: parent.verticalCenter }
                    spacing: 1
                    Text {
                        text: row.modelData.name
                        color: row.on ? Theme.white : Theme.text2
                        font.pixelSize: Theme.fontLarge
                        font.weight: Font.DemiBold
                    }
                    Text {
                        width: parent.width
                        text: row.modelData.role || ""
                        color: Theme.text4
                        font.pixelSize: Theme.fontSmall
                        elide: Text.ElideRight
                    }
                }
                // Answers on its own, or only when named.
                Segmented {
                    id: answers
                    visible: row.on
                    anchors { right: parent.right; rightMargin: 10; verticalCenter: parent.verticalCenter }
                    implicitHeight: 30
                    options: [{ value: "auto", label: "Responde" }, { value: "mention", label: "Só com @" }]
                    value: row.state.mention_only ? "mention" : "auto"
                    onActivated: v => ed.setMentionOnly(row.modelData.id, v === "mention")
                }
            }
        }
        Text {
            width: parent.width
            wrapMode: Text.Wrap
            text: "\"Responde\": entra na conversa quando a mensagem tem a ver com ele. \"Só com @\": fica no canal, lê tudo, e só fala quando você ou um colega escreve @Nome."
            color: Theme.text4
            font.pixelSize: Theme.fontSmall
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
                text: ed.channel ? "Salvar" : "Criar canal"
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
            visible: !!ed.channel && !ed.isTime
            anchors { right: parent.right; verticalCenter: parent.verticalCenter }
            danger: true
            icon: "trash"
            text: ed.confirmArchive ? "Confirmar: arquivar o canal" : "Arquivar"
            onClicked: ed.archive()
        }
    }
}
