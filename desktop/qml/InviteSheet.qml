import QtQuick
import Avora
import "util.js" as U

// Modal sheet to create invite links (role, expiration days, approval toggle),
// copy the freshly generated link, and view/revoke active invites.
Sheet {
    id: sheet
    sheetWidth: 540

    property string workspaceId: ""
    property string workspaceName: ""

    property string role: "member"
    property int days: 7
    property bool approval: true

    property string createdCode: ""
    property bool copied: false
    property var invites: []
    property bool loading: false
    property bool creating: false
    property string confirmRevoke: ""
    property string error: ""

    readonly property string inviteLink: createdCode !== "" ? Api.server + "/convite/" + createdCode : ""

    function open(wsId, wsName) {
        workspaceId = wsId || Api.workspace || "pessoal";
        workspaceName = wsName || "";
        role = "member";
        days = 7;
        approval = true;
        createdCode = "";
        copied = false;
        confirmRevoke = "";
        error = "";
        creating = false;
        shown = true;
        load();
    }

    function load() {
        if (workspaceId === "")
            return;
        loading = true;
        Api.get("/v1/workspaces/" + encodeURIComponent(workspaceId) + "/invites", (err, data) => {
            loading = false;
            if (err) {
                error = err;
                return;
            }
            invites = data || [];
        });
    }

    function createInvite() {
        if (creating || workspaceId === "")
            return;
        creating = true;
        error = "";
        copied = false;
        Api.post("/v1/workspaces/" + encodeURIComponent(workspaceId) + "/invites",
                 { role: role, days: days, approval: approval },
                 (err, data) => {
            creating = false;
            if (err) {
                error = err;
                return;
            }
            createdCode = (data && data.code) ? data.code : "";
            load();
        });
    }

    function revokeInvite(id) {
        if (confirmRevoke !== id) {
            confirmRevoke = id;
            disarm.restart();
            return;
        }
        confirmRevoke = "";
        Api.del("/v1/workspaces/" + encodeURIComponent(workspaceId) + "/invites/" + encodeURIComponent(id), err => {
            if (err) {
                error = err;
                return;
            }
            load();
        });
    }

    function roleLabel(r) {
        return r === "admin" ? "Administrador" : r === "owner" ? "Dono" : "Membro";
    }

    Timer {
        id: disarm
        interval: 4000
        onTriggered: sheet.confirmRevoke = ""
    }

    Timer {
        id: copiedReset
        interval: 2500
        onTriggered: sheet.copied = false
    }

    Column {
        width: parent.width
        spacing: Theme.space1
        Text {
            text: sheet.workspaceName !== "" ? "Convidar para " + sheet.workspaceName : "Convidar pessoas"
            color: Theme.white
            font.family: Theme.display
            font.pixelSize: Theme.fontHeading
            font.weight: Font.DemiBold
        }
        Text {
            text: "Gere um link com prazo de validade para chamar pessoas para o espaço."
            color: Theme.text3
            font.pixelSize: Theme.fontBody
        }
    }

    // Invite creation options.
    Column {
        width: parent.width
        spacing: Theme.space3

        Row {
            width: parent.width
            spacing: Theme.space4
            Column {
                spacing: Theme.space2
                Overline { text: "CARGO" }
                Segmented {
                    value: sheet.role
                    options: [
                        { value: "member", label: "Membro" },
                        { value: "admin", label: "Administrador" }
                    ]
                    onActivated: v => sheet.role = v
                }
            }

            Column {
                spacing: Theme.space2
                Overline { text: "ENTRADA" }
                Segmented {
                    value: sheet.approval ? "approval" : "direct"
                    options: [
                        { value: "approval", label: "Pedir aprovação" },
                        { value: "direct", label: "Entrar direto" }
                    ]
                    onActivated: v => sheet.approval = (v === "approval")
                }
            }
        }

        Column {
            width: parent.width
            spacing: Theme.space2
            Overline { text: "VALIDADE" }
            Flow {
                width: parent.width
                spacing: Theme.space2
                Repeater {
                    model: [
                        { days: 1, label: "1 dia" },
                        { days: 7, label: "7 dias" },
                        { days: 14, label: "14 dias" },
                        { days: 30, label: "30 dias" }
                    ]
                    delegate: Pill {
                        required property var modelData
                        text: modelData.label
                        on: sheet.days === modelData.days
                        onHit: sheet.days = modelData.days
                    }
                }
            }
        }

        PrimaryButton {
            text: sheet.creating ? "Gerando link…" : "Criar link de convite"
            icon: "plus"
            busy: sheet.creating
            enabled: !sheet.creating
            onClicked: sheet.createInvite()
        }
    }

    // Newly generated link (shown once because the server stores only its hash).
    Rectangle {
        visible: sheet.createdCode !== ""
        width: parent.width
        height: createdCol.implicitHeight + 2 * Theme.space4
        radius: Theme.radiusMd
        color: Theme.wash2
        border.width: 1
        border.color: Theme.focusLine

        Column {
            id: createdCol
            x: Theme.space4
            y: Theme.space4
            width: parent.width - 2 * Theme.space4
            spacing: Theme.space2

            Overline { text: "LINK PRONTO PARA ENVIAR" }

            Row {
                width: parent.width
                spacing: Theme.space2

                Rectangle {
                    width: parent.width - copyBtn.width - Theme.space2
                    height: 38
                    radius: Theme.radiusSm
                    color: Theme.well
                    border.width: 1
                    border.color: Theme.line
                    Text {
                        anchors { left: parent.left; right: parent.right; margins: Theme.space3; verticalCenter: parent.verticalCenter }
                        text: sheet.inviteLink
                        color: Theme.white
                        font.family: Theme.mono
                        font.pixelSize: Theme.fontSmall
                        elide: Text.ElideMiddle
                    }
                }

                GhostButton {
                    id: copyBtn
                    height: 38
                    icon: sheet.copied ? "check" : "copy"
                    text: sheet.copied ? "Copiado" : "Copiar"
                    onClicked: {
                        Api.copyText(sheet.inviteLink);
                        sheet.copied = true;
                        copiedReset.restart();
                    }
                }
            }

            Text {
                width: parent.width
                wrapMode: Text.WordWrap
                text: "Copie agora: por segurança, o código completo só aparece nesta criação."
                color: Theme.text3
                font.pixelSize: Theme.fontSmall
            }
        }
    }

    ErrorBanner {
        width: parent.width
        text: sheet.error
    }

    // Active invites list.
    Column {
        width: parent.width
        spacing: Theme.space2

        Overline { text: "CONVITES ATIVOS" }

        Text {
            visible: sheet.loading && sheet.invites.length === 0
            width: parent.width
            text: "Carregando os convites ativos deste espaço…"
            color: Theme.text3
            font.pixelSize: Theme.fontBody
        }

        Text {
            visible: !sheet.loading && sheet.invites.length === 0
            width: parent.width
            wrapMode: Text.WordWrap
            text: "Nenhum convite ativo no momento. Crie um link acima quando quiser chamar alguém."
            color: Theme.text4
            font.pixelSize: Theme.fontBody
        }

        Column {
            visible: sheet.invites.length > 0
            width: parent.width
            spacing: Theme.space2

            Repeater {
                model: sheet.invites
                delegate: Rectangle {
                    id: invRow
                    required property var modelData
                    width: parent.width
                    height: 54
                    radius: Theme.radiusMd
                    color: Theme.wash1
                    border.width: 1
                    border.color: Theme.line

                    Column {
                        anchors { left: parent.left; leftMargin: Theme.space3; right: revokeBtn.left; rightMargin: Theme.space3; verticalCenter: parent.verticalCenter }
                        spacing: Theme.space1
                        Row {
                            spacing: Theme.space2
                            Text {
                                text: sheet.roleLabel(invRow.modelData.role)
                                color: Theme.text
                                font.pixelSize: Theme.fontBody
                                font.weight: Font.DemiBold
                            }
                            Text {
                                text: invRow.modelData.approval ? "· pede aprovação" : "· entrada direta"
                                color: Theme.text3
                                font.pixelSize: Theme.fontSmall
                                anchors.verticalCenter: parent.verticalCenter
                            }
                        }
                        Text {
                            readonly property int uses: invRow.modelData.uses || 0
                            text: (uses === 1 ? "1 uso" : uses + " usos")
                                + (invRow.modelData.expires_at ? "  ·  vence " + U.when(invRow.modelData.expires_at) : "")
                            color: Theme.text4
                            font.family: Theme.mono
                            font.pixelSize: Theme.fontCaption
                        }
                    }

                    GhostButton {
                        id: revokeBtn
                        anchors { right: parent.right; rightMargin: Theme.space3; verticalCenter: parent.verticalCenter }
                        small: true
                        danger: true
                        text: sheet.confirmRevoke === invRow.modelData.id ? "Confirmar" : "Revogar"
                        onClicked: sheet.revokeInvite(invRow.modelData.id)
                    }
                }
            }
        }
    }

    Row {
        spacing: Theme.space2
        GhostButton {
            text: "Fechar"
            onClicked: sheet.shown = false
        }
    }
}
