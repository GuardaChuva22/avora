import QtQuick
import QtQuick.Controls.Basic
import Avora
import "util.js" as U

// Settings → Pessoas: workspace name/icon, invite links, pending join
// requests, active members with role controls, remove, and leave workspace.
Item {
    id: page
    property var workspaces: []
    property var members: []
    property bool loading: false
    property bool savingWs: false
    property bool syncFields: true
    property string error: ""
    property string confirmAction: ""

    readonly property string wsId: Api.workspace !== "" ? Api.workspace
        : (Api.me && Api.me.workspaces && Api.me.workspaces.length > 0 ? Api.me.workspaces[0].id : "pessoal")

    readonly property var currentWorkspace: {
        const fromList = workspaces.find(w => w.id === wsId);
        if (fromList)
            return fromList;
        const meList = (Api.me && Api.me.workspaces) ? Api.me.workspaces : [];
        return meList.find(w => w.id === wsId) || null;
    }

    readonly property string wsName: currentWorkspace && currentWorkspace.name ? currentWorkspace.name : "Pessoal"
    readonly property string wsIcon: currentWorkspace && currentWorkspace.icon ? currentWorkspace.icon : ""

    readonly property var myPerson: (Api.me && Api.me.person) ? Api.me.person : ({})
    readonly property string myId: myPerson.id || ""
    readonly property var myMember: members.find(m => m.person_id === myId) || null
    readonly property string myRole: {
        if (myMember && myMember.role)
            return myMember.role;
        if (currentWorkspace && currentWorkspace.role)
            return currentWorkspace.role;
        return myId === "" ? "owner" : "member";
    }
    readonly property bool isOwner: myRole === "owner"
    readonly property bool canManage: myRole === "owner" || myRole === "admin"

    readonly property var pendingMembers: members.filter(m => m.status === "pending")
    readonly property var activeMembers: members.filter(m => m.status !== "pending")

    readonly property bool wsDirty: wsNameField.text.trim() !== wsName || wsIconField.text.trim() !== wsIcon

    function openSheet(s) {
        if (s === "convidar" || s === "invite")
            inviteSheet.open(wsId, wsName);
    }

    function roleLabel(r) {
        return r === "owner" ? "Dono" : r === "admin" ? "Administrador" : "Membro";
    }

    function personPhoto(av) {
        if (!av)
            return "";
        if (av.startsWith("avora:file/"))
            return "image://server/v1/files/" + av.slice(11);
        if (av.startsWith("/v1/files/"))
            return "image://server" + av;
        return av;
    }

    function tintFor(seed) {
        const s = String(seed || "?");
        let h = 0;
        for (let i = 0; i < s.length; i++)
            h = (h * 31 + s.charCodeAt(i)) >>> 0;
        return Theme.palette[h % Theme.palette.length];
    }

    function presenceDot(m) {
        if (!m || !m.online || m.presence === "invisible")
            return "";
        if (m.presence === "busy")
            return "bad";
        if (m.presence === "away")
            return "warn";
        return "live";
    }

    function load() {
        const id = wsId;
        if (id === "")
            return;
        loading = true;
        Api.get("/v1/workspaces", (err, list) => {
            if (!err && list) {
                if (!U.same(workspaces, list))
                    workspaces = list;
                if (syncFields) {
                    const cur = list.find(w => w.id === id);
                    if (cur) {
                        wsNameField.text = cur.name || "";
                        wsIconField.text = cur.icon || "";
                        syncFields = false;
                    }
                }
            }
        });
        Api.get("/v1/workspaces/" + encodeURIComponent(id) + "/members", (err, list) => {
            loading = false;
            if (err) {
                error = err;
                return;
            }
            error = "";
            if (!U.same(members, list || []))
                members = list || [];
        });
    }

    onCurrentWorkspaceChanged: {
        if (syncFields && currentWorkspace) {
            wsNameField.text = currentWorkspace.name || "";
            wsIconField.text = currentWorkspace.icon || "";
            syncFields = false;
        }
    }

    function saveWorkspace() {
        if (savingWs || !canManage)
            return;
        const name = wsNameField.text.trim();
        if (name === "") {
            error = "O espaço precisa ter um nome.";
            wsNameField.forceActiveFocus();
            return;
        }
        savingWs = true;
        error = "";
        Api.patch("/v1/workspaces/" + encodeURIComponent(wsId),
                  { name: name, icon: wsIconField.text.trim() },
                  err => {
            savingWs = false;
            if (err) {
                error = err;
                return;
            }
            syncFields = true;
            Api.refreshMe();
            load();
        });
    }

    function approveMember(personId) {
        error = "";
        Api.post("/v1/workspaces/" + encodeURIComponent(wsId) + "/members/" + encodeURIComponent(personId) + "/approve", {}, err => {
            if (err) {
                error = err;
                return;
            }
            Api.refreshMe();
            load();
        });
    }

    function declineMember(personId) {
        error = "";
        Api.del("/v1/workspaces/" + encodeURIComponent(wsId) + "/members/" + encodeURIComponent(personId), err => {
            if (err) {
                error = err;
                return;
            }
            load();
        });
    }

    function setMemberRole(personId, nextRole) {
        error = "";
        Api.patch("/v1/workspaces/" + encodeURIComponent(wsId) + "/members/" + encodeURIComponent(personId),
                  { role: nextRole },
                  err => {
            if (err) {
                error = err;
                return;
            }
            load();
        });
    }

    function removeMember(personId) {
        const key = "rm:" + personId;
        if (confirmAction !== key) {
            confirmAction = key;
            disarm.restart();
            return;
        }
        confirmAction = "";
        error = "";
        Api.del("/v1/workspaces/" + encodeURIComponent(wsId) + "/members/" + encodeURIComponent(personId), err => {
            if (err) {
                error = err;
                return;
            }
            Api.refreshMe();
            load();
        });
    }

    function leaveWorkspace() {
        if (myId === "")
            return;
        if (confirmAction !== "leave") {
            confirmAction = "leave";
            disarm.restart();
            return;
        }
        confirmAction = "";
        error = "";
        Api.del("/v1/workspaces/" + encodeURIComponent(wsId) + "/members/" + encodeURIComponent(myId), err => {
            if (err) {
                error = err;
                return;
            }
            Api.refreshMe();
        });
    }

    Timer {
        interval: Api.live ? 30000 : 8000
        repeat: true
        running: page.visible
        triggeredOnStart: true
        onTriggered: page.load()
    }

    Timer {
        id: disarm
        interval: 4000
        onTriggered: page.confirmAction = ""
    }

    Connections {
        target: Api
        function onWorkspaceChanged() {
            page.syncFields = true;
            page.confirmAction = "";
            page.load();
        }
        function onLiveEvent(e) {
            const t = e.type || "";
            if (t.startsWith("member.") || t.startsWith("workspace.") || t.startsWith("person."))
                page.load();
        }
        function onLiveReset() {
            page.load();
        }
    }

    Flickable {
        id: scroller
        anchors.fill: parent
        contentHeight: col.height + 110
        clip: true
        boundsBehavior: Flickable.StopAtBounds
        ScrollBar.vertical: SlimScrollBar {}
        SmoothWheel {}

        Column {
            id: col
            x: Theme.gutter
            y: 52
            width: scroller.width - 2 * Theme.gutter
            spacing: Theme.space5

            Item {
                width: parent.width
                height: head.implicitHeight

                PageHead {
                    id: head
                    eyebrow: "Espaço"
                    title: "Pessoas"
                    sub: "Quem faz parte deste espaço, convites de acesso e cargos."
                }

                PrimaryButton {
                    visible: page.canManage
                    anchors { right: parent.right; bottom: parent.bottom }
                    icon: "plus"
                    text: "Convidar pessoas"
                    onClicked: inviteSheet.open(page.wsId, page.wsName)
                }
            }

            ErrorBanner {
                width: parent.width
                text: page.error
            }

            // Current workspace settings (name, icon, leave).
            Card {
                width: parent.width
                height: wsCol.implicitHeight + 2 * padding
                padding: Theme.space5

                Column {
                    id: wsCol
                    width: parent.width
                    spacing: Theme.space3

                    Overline { text: "ESPAÇO ATUAL" }

                    // Editable view for owner / admin.
                    Row {
                        visible: page.canManage
                        width: parent.width
                        spacing: Theme.space3

                        Rectangle {
                            width: 44
                            height: 44
                            radius: Theme.radiusMd
                            color: Theme.wash3
                            border.width: 1
                            border.color: Theme.lineStrong
                            anchors.bottom: parent.bottom
                            Text {
                                anchors.centerIn: parent
                                text: wsIconField.text.trim() !== "" ? wsIconField.text.trim()
                                    : (wsNameField.text.trim() || page.wsName || "E").charAt(0).toUpperCase()
                                color: Theme.white
                                font.family: Theme.display
                                font.pixelSize: Theme.fontTitle
                                font.weight: Font.DemiBold
                            }
                        }

                        Column {
                            width: 78
                            spacing: Theme.space1
                            Overline { text: "ÍCONE" }
                            Field {
                                id: wsIconField
                                width: parent.width
                                maximumLength: 4
                                placeholderText: "🚀"
                                onAccepted: page.saveWorkspace()
                            }
                        }

                        Column {
                            width: Math.max(140, parent.width - 44 - 78 - saveWsBtn.width - (leaveAdminBtn.visible ? leaveAdminBtn.width + Theme.space3 : 0) - 3 * Theme.space3)
                            spacing: Theme.space1
                            Overline { text: "NOME" }
                            Field {
                                id: wsNameField
                                width: parent.width
                                maximumLength: 60
                                placeholderText: "Nome do espaço"
                                onAccepted: page.saveWorkspace()
                            }
                        }

                        GhostButton {
                            id: saveWsBtn
                            anchors.bottom: parent.bottom
                            height: 42
                            icon: "check"
                            text: page.savingWs ? "Salvando…" : "Salvar"
                            enabled: !page.savingWs && page.wsDirty
                            onClicked: page.saveWorkspace()
                        }

                        GhostButton {
                            id: leaveAdminBtn
                            visible: page.myId !== "" && page.wsId !== "pessoal"
                            anchors.bottom: parent.bottom
                            height: 42
                            danger: true
                            icon: "logout"
                            text: page.confirmAction === "leave" ? "Confirmar saída" : "Sair do espaço"
                            onClicked: page.leaveWorkspace()
                        }
                    }

                    // Read-only view for regular members.
                    Item {
                        visible: !page.canManage
                        width: parent.width
                        height: 48

                        Row {
                            anchors { left: parent.left; verticalCenter: parent.verticalCenter }
                            spacing: Theme.space3
                            Rectangle {
                                width: 44
                                height: 44
                                radius: Theme.radiusMd
                                color: Theme.wash3
                                border.width: 1
                                border.color: Theme.lineStrong
                                Text {
                                    anchors.centerIn: parent
                                    text: page.wsIcon !== "" ? page.wsIcon : (page.wsName || "E").charAt(0).toUpperCase()
                                    color: Theme.white
                                    font.family: Theme.display
                                    font.pixelSize: Theme.fontTitle
                                    font.weight: Font.DemiBold
                                }
                            }
                            Column {
                                anchors.verticalCenter: parent.verticalCenter
                                spacing: Theme.space1
                                Text {
                                    text: page.wsName
                                    color: Theme.white
                                    font.pixelSize: Theme.fontTitle
                                    font.weight: Font.DemiBold
                                }
                                Text {
                                    text: "Seu cargo: " + page.roleLabel(page.myRole)
                                    color: Theme.text3
                                    font.pixelSize: Theme.fontSmall
                                }
                            }
                        }

                        GhostButton {
                            visible: page.myId !== "" && page.wsId !== "pessoal"
                            anchors { right: parent.right; verticalCenter: parent.verticalCenter }
                            danger: true
                            icon: "logout"
                            text: page.confirmAction === "leave" ? "Confirmar saída" : "Sair do espaço"
                            onClicked: page.leaveWorkspace()
                        }
                    }
                }
            }

            // Pending join approvals.
            Column {
                visible: page.pendingMembers.length > 0
                width: parent.width
                spacing: Theme.space3

                Overline { text: "PEDIDOS PARA ENTRAR  ·  " + page.pendingMembers.length }

                Card {
                    width: parent.width
                    height: pendingCol.implicitHeight + 2 * padding
                    padding: Theme.space2

                    Column {
                        id: pendingCol
                        width: parent.width

                        Repeater {
                            model: page.pendingMembers
                            delegate: Item {
                                id: pRow
                                required property var modelData
                                required property int index
                                width: pendingCol.width
                                height: 62

                                Rectangle {
                                    visible: pRow.index > 0
                                    anchors { top: parent.top; left: parent.left; right: parent.right; leftMargin: Theme.space3; rightMargin: Theme.space3 }
                                    height: 1
                                    color: Theme.line
                                }

                                Avatar {
                                    id: pAv
                                    x: Theme.space3
                                    anchors.verticalCenter: parent.verticalCenter
                                    size: 40
                                    name: pRow.modelData.name || pRow.modelData.email || "?"
                                    photo: page.personPhoto(pRow.modelData.avatar)
                                    tint: page.tintFor(pRow.modelData.person_id || pRow.modelData.email)
                                }

                                Column {
                                    anchors {
                                        left: pAv.right
                                        leftMargin: Theme.space3
                                        right: pActions.left
                                        rightMargin: Theme.space3
                                        verticalCenter: parent.verticalCenter
                                    }
                                    spacing: Theme.space1
                                    Text {
                                        width: parent.width
                                        elide: Text.ElideRight
                                        text: pRow.modelData.name || pRow.modelData.email
                                        color: Theme.white
                                        font.pixelSize: Theme.fontLarge
                                        font.weight: Font.DemiBold
                                    }
                                    Text {
                                        width: parent.width
                                        elide: Text.ElideRight
                                        text: (pRow.modelData.email || "") + "  ·  cargo pedido: " + page.roleLabel(pRow.modelData.role)
                                        color: Theme.text3
                                        font.pixelSize: Theme.fontSmall
                                    }
                                }

                                Row {
                                    id: pActions
                                    visible: page.canManage
                                    anchors { right: parent.right; rightMargin: Theme.space3; verticalCenter: parent.verticalCenter }
                                    spacing: Theme.space2
                                    GhostButton {
                                        small: true
                                        icon: "check"
                                        text: "Aprovar"
                                        onClicked: page.approveMember(pRow.modelData.person_id)
                                    }
                                    GhostButton {
                                        small: true
                                        danger: true
                                        icon: "close"
                                        text: "Recusar"
                                        onClicked: page.declineMember(pRow.modelData.person_id)
                                    }
                                }
                            }
                        }
                    }
                }
            }

            // Active members list.
            Column {
                width: parent.width
                spacing: Theme.space3

                Overline { text: "MEMBROS  ·  " + page.activeMembers.length }

                Card {
                    visible: page.loading && page.members.length === 0
                    width: parent.width
                    height: 84
                    padding: Theme.space5
                    Text {
                        anchors.verticalCenter: parent.verticalCenter
                        text: "Carregando as pessoas deste espaço…"
                        color: Theme.text3
                        font.pixelSize: Theme.fontBody
                    }
                }

                Card {
                    visible: !page.loading && page.activeMembers.length === 0
                    width: parent.width
                    height: 92
                    padding: Theme.space5
                    Text {
                        width: parent.width
                        anchors.verticalCenter: parent.verticalCenter
                        wrapMode: Text.WordWrap
                        text: "Ainda não há nenhuma pessoa listada neste espaço. Use Convidar pessoas para gerar um link de entrada."
                        color: Theme.text3
                        font.pixelSize: Theme.fontBody
                    }
                }

                Card {
                    visible: page.activeMembers.length > 0
                    width: parent.width
                    height: activeCol.implicitHeight + 2 * padding
                    padding: Theme.space2

                    Column {
                        id: activeCol
                        width: parent.width

                        Repeater {
                            model: page.activeMembers
                            delegate: Item {
                                id: mRow
                                required property var modelData
                                required property int index

                                readonly property bool isMe: modelData.person_id === page.myId && page.myId !== ""
                                readonly property bool canChangeRole: page.canManage && !isMe && modelData.role !== "owner" && (page.isOwner || modelData.role !== "admin")
                                readonly property bool canRemove: page.canManage && !isMe && modelData.role !== "owner" && (page.isOwner || modelData.role !== "admin")

                                width: activeCol.width
                                height: 64

                                Rectangle {
                                    visible: mRow.index > 0
                                    anchors { top: parent.top; left: parent.left; right: parent.right; leftMargin: Theme.space3; rightMargin: Theme.space3 }
                                    height: 1
                                    color: Theme.line
                                }

                                Item {
                                    id: mAvBox
                                    x: Theme.space3
                                    width: 40
                                    height: 40
                                    anchors.verticalCenter: parent.verticalCenter

                                    Avatar {
                                        anchors.fill: parent
                                        size: 40
                                        name: mRow.modelData.name || mRow.modelData.email || "?"
                                        photo: page.personPhoto(mRow.modelData.avatar)
                                        tint: page.tintFor(mRow.modelData.person_id || mRow.modelData.email)
                                    }

                                    Rectangle {
                                        width: 11
                                        height: 11
                                        radius: height / 2
                                        color: Theme.card
                                        anchors { right: parent.right; bottom: parent.bottom }
                                        Dot {
                                            anchors.centerIn: parent
                                            kind: page.presenceDot(mRow.modelData)
                                        }
                                    }
                                }

                                Column {
                                    anchors {
                                        left: mAvBox.right
                                        leftMargin: Theme.space3
                                        right: mControls.left
                                        rightMargin: Theme.space3
                                        verticalCenter: parent.verticalCenter
                                    }
                                    spacing: Theme.space1

                                    Row {
                                        width: parent.width
                                        spacing: Theme.space2
                                        Text {
                                            text: mRow.modelData.name || mRow.modelData.email
                                            color: Theme.white
                                            font.pixelSize: Theme.fontLarge
                                            font.weight: Font.DemiBold
                                            elide: Text.ElideRight
                                        }
                                        Text {
                                            visible: mRow.isMe
                                            text: "você"
                                            color: Theme.text4
                                            font.pixelSize: Theme.fontCaption
                                            font.weight: Font.DemiBold
                                            anchors.verticalCenter: parent.verticalCenter
                                        }
                                    }

                                    Text {
                                        width: parent.width
                                        elide: Text.ElideRight
                                        text: (mRow.modelData.email || "")
                                            + (mRow.modelData.status_text ? "  ·  " + mRow.modelData.status_text : "")
                                        color: Theme.text3
                                        font.pixelSize: Theme.fontSmall
                                    }
                                }

                                Row {
                                    id: mControls
                                    anchors { right: parent.right; rightMargin: Theme.space3; verticalCenter: parent.verticalCenter }
                                    spacing: Theme.space2

                                    Segmented {
                                        visible: mRow.canChangeRole
                                        anchors.verticalCenter: parent.verticalCenter
                                        value: mRow.modelData.role
                                        options: [
                                            { value: "member", label: "Membro" },
                                            { value: "admin", label: "Admin" }
                                        ]
                                        onActivated: v => page.setMemberRole(mRow.modelData.person_id, v)
                                    }

                                    Rectangle {
                                        visible: !mRow.canChangeRole
                                        height: 28
                                        width: roleTxt.implicitWidth + 2 * Theme.space3
                                        radius: height / 2
                                        color: mRow.modelData.role === "owner" ? Theme.wash3 : Theme.wash1
                                        border.width: 1
                                        border.color: mRow.modelData.role === "owner" ? Theme.lineStrong : Theme.line
                                        anchors.verticalCenter: parent.verticalCenter
                                        Text {
                                            id: roleTxt
                                            anchors.centerIn: parent
                                            text: page.roleLabel(mRow.modelData.role)
                                            color: mRow.modelData.role === "owner" ? Theme.white : Theme.text2
                                            font.pixelSize: Theme.fontSmall
                                            font.weight: Font.Medium
                                        }
                                    }

                                    GhostButton {
                                        visible: mRow.canRemove
                                        anchors.verticalCenter: parent.verticalCenter
                                        small: true
                                        danger: true
                                        text: page.confirmAction === "rm:" + mRow.modelData.person_id ? "Confirmar" : "Remover"
                                        onClicked: page.removeMember(mRow.modelData.person_id)
                                    }
                                }
                            }
                        }
                    }
                }
            }
        }
    }

    InviteSheet {
        id: inviteSheet
        anchors.fill: parent
    }
}
