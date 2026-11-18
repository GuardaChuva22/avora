import QtQuick
import Avora

// Modal sheet to create a new workspace or join an existing one with an invite.
Sheet {
    id: sheet
    sheetWidth: 480

    property string tab: "create" // "create" | "join"
    property string error: ""
    property bool busy: false
    property bool pendingNotice: false

    property var inviteInfo: null
    property string inviteError: ""
    property bool inviteLoading: false

    readonly property var emojis: ["🚀", "⚡", "🛠", "🎨", "📚", "🏠", "🌿", "✨"]
    readonly property string inviteCode: extractCode(codeField.text)

    function open(initialTab) {
        tab = initialTab || "create";
        error = "";
        busy = false;
        pendingNotice = false;
        nameField.text = "";
        iconField.text = "";
        codeField.text = "";
        inviteInfo = null;
        inviteError = "";
        inviteLoading = false;
        shown = true;
        if (tab === "join")
            codeField.forceActiveFocus();
        else
            nameField.forceActiveFocus();
    }

    function extractCode(raw) {
        const t = (raw || "").trim();
        if (t === "")
            return "";
        const m = t.match(/\/convite\/([^\/?#\s]+)/i);
        if (m)
            return decodeURIComponent(m[1]);
        if (/^https?:\/\//i.test(t) || t.indexOf("/") >= 0)
            return "";
        return t;
    }

    function roleLabel(r) {
        return r === "owner" ? "Dono" : r === "admin" ? "Administrador" : "Membro";
    }

    function lookupInvite() {
        const code = inviteCode;
        if (code === "" || Api.server === "") {
            inviteInfo = null;
            inviteError = "";
            inviteLoading = false;
            return;
        }
        inviteLoading = true;
        inviteError = "";
        Api.checkInvite(Api.server, code, (err, data) => {
            if (code !== sheet.inviteCode)
                return;
            inviteLoading = false;
            if (err) {
                inviteInfo = null;
                inviteError = "Convite inválido ou vencido.";
                return;
            }
            inviteInfo = data || null;
            inviteError = "";
        });
    }

    Timer {
        id: lookupTimer
        interval: 250
        onTriggered: sheet.lookupInvite()
    }

    function submit() {
        if (busy)
            return;
        error = "";
        if (tab === "create") {
            const name = nameField.text.trim();
            if (name === "") {
                error = "Dê um nome ao espaço.";
                nameField.forceActiveFocus();
                return;
            }
            busy = true;
            const body = { name: name };
            if (iconField.text.trim() !== "")
                body.icon = iconField.text.trim();
            Api.post("/v1/workspaces", body, (err, ws) => {
                busy = false;
                if (err) {
                    error = err;
                    return;
                }
                Api.refreshMe();
                if (ws && ws.id)
                    Api.setWorkspace(ws.id);
                shown = false;
            });
            return;
        }

        const code = inviteCode;
        if (code === "") {
            error = "Cole o link ou o código de convite.";
            codeField.forceActiveFocus();
            return;
        }
        busy = true;
        Api.post("/v1/invites/" + encodeURIComponent(code) + "/accept", {}, (err, res) => {
            busy = false;
            if (err) {
                error = err;
                return;
            }
            Api.refreshMe();
            if (res && res.status === "pending") {
                pendingNotice = true;
                return;
            }
            if (res && res.workspace_id)
                Api.setWorkspace(res.workspace_id);
            shown = false;
        });
    }

    Column {
        width: parent.width
        spacing: Theme.space1
        Text {
            text: sheet.tab === "create" ? "Novo espaço" : "Entrar em um espaço"
            color: Theme.white
            font.family: Theme.display
            font.pixelSize: Theme.fontHeading
            font.weight: Font.DemiBold
        }
        Text {
            text: sheet.tab === "create" ? "Um espaço reúne agentes, canais, docs e pessoas."
                : "Use o link ou código enviado por alguém do espaço."
            color: Theme.text3
            font.pixelSize: Theme.fontBody
        }
    }

    Segmented {
        value: sheet.tab
        options: [
            { value: "create", label: "Criar espaço" },
            { value: "join", label: "Entrar com convite" }
        ]
        onActivated: v => {
            sheet.tab = v;
            sheet.error = "";
            sheet.pendingNotice = false;
        }
    }

    // Create workspace tab.
    Column {
        visible: sheet.tab === "create"
        width: parent.width
        spacing: Theme.space4

        Row {
            width: parent.width
            spacing: Theme.space3

            Rectangle {
                width: 52
                height: 52
                radius: Theme.radiusLg
                color: Theme.wash3
                border.width: 1
                border.color: Theme.lineStrong
                anchors.bottom: parent.bottom
                Text {
                    anchors.centerIn: parent
                    text: iconField.text.trim() !== "" ? iconField.text.trim() : (nameField.text.trim() || "E").charAt(0).toUpperCase()
                    color: Theme.white
                    font.family: Theme.display
                    font.pixelSize: Theme.fontHeading
                    font.weight: Font.DemiBold
                }
            }

            Column {
                width: 96
                spacing: Theme.space2
                Overline { text: "ÍCONE" }
                Field {
                    id: iconField
                    width: parent.width
                    placeholderText: "🚀"
                    maximumLength: 4
                    onAccepted: nameField.forceActiveFocus()
                }
            }

            Column {
                width: parent.width - 52 - 96 - 2 * Theme.space3
                spacing: Theme.space2
                Overline { text: "NOME DO ESPAÇO" }
                Field {
                    id: nameField
                    width: parent.width
                    placeholderText: "Ex.: Estúdio Norte"
                    maximumLength: 60
                    onAccepted: sheet.submit()
                }
            }
        }

        Flow {
            width: parent.width
            spacing: Theme.space2
            Repeater {
                model: sheet.emojis
                delegate: Pill {
                    required property string modelData
                    text: modelData
                    on: iconField.text.trim() === modelData
                    onHit: iconField.text = iconField.text.trim() === modelData ? "" : modelData
                }
            }
        }
    }

    // Join with invite tab.
    Column {
        visible: sheet.tab === "join"
        width: parent.width
        spacing: Theme.space3

        Column {
            visible: !sheet.pendingNotice
            width: parent.width
            spacing: Theme.space2
            Overline { text: "CONVITE" }
            Row {
                width: parent.width
                spacing: Theme.space2
                Field {
                    id: codeField
                    width: parent.width - pasteBtn.width - Theme.space2
                    mono: true
                    placeholderText: "Código ou link .../convite/..."
                    onTextEdited: lookupTimer.restart()
                    onAccepted: sheet.submit()
                }
                GhostButton {
                    id: pasteBtn
                    icon: "paste"
                    text: "Colar"
                    height: 42
                    onClicked: {
                        codeField.text = Api.clipboardText();
                        sheet.lookupInvite();
                    }
                }
            }
        }

        Rectangle {
            visible: !sheet.pendingNotice && sheet.inviteCode !== "" && (sheet.inviteLoading || sheet.inviteInfo !== null || sheet.inviteError !== "")
            width: parent.width
            height: previewRow.implicitHeight + 2 * Theme.space3
            radius: Theme.radiusMd
            color: sheet.inviteError !== "" ? Theme.badWash : Theme.wash2
            border.width: 1
            border.color: sheet.inviteError !== "" ? Theme.badLine : Theme.lineStrong

            Row {
                id: previewRow
                x: Theme.space3
                width: parent.width - 2 * Theme.space3
                anchors.verticalCenter: parent.verticalCenter
                spacing: Theme.space3

                Rectangle {
                    visible: sheet.inviteInfo !== null
                    width: 40
                    height: 40
                    radius: Theme.radiusMd
                    color: Theme.wash4
                    border.width: 1
                    border.color: Theme.focusLine
                    anchors.verticalCenter: parent.verticalCenter
                    Text {
                        anchors.centerIn: parent
                        text: {
                            const ws = sheet.inviteInfo && sheet.inviteInfo.workspace ? sheet.inviteInfo.workspace : null;
                            if (!ws)
                                return "?";
                            return ws.icon ? ws.icon : (ws.name || "?").charAt(0).toUpperCase();
                        }
                        color: Theme.white
                        font.family: Theme.display
                        font.pixelSize: Theme.fontTitle
                        font.weight: Font.DemiBold
                    }
                }

                Column {
                    width: parent.width - (sheet.inviteInfo !== null ? 40 + Theme.space3 : 0)
                    anchors.verticalCenter: parent.verticalCenter
                    spacing: Theme.space1
                    Text {
                        width: parent.width
                        elide: Text.ElideRight
                        text: sheet.inviteLoading ? "Conferindo convite…"
                            : sheet.inviteError !== "" ? sheet.inviteError
                            : sheet.inviteInfo && sheet.inviteInfo.workspace ? sheet.inviteInfo.workspace.name : "Espaço convidado"
                        color: sheet.inviteError !== "" ? Theme.bad : Theme.white
                        font.pixelSize: Theme.fontLarge
                        font.weight: sheet.inviteInfo !== null ? Font.DemiBold : Font.Normal
                    }
                    Text {
                        visible: sheet.inviteInfo !== null && !sheet.inviteLoading
                        width: parent.width
                        elide: Text.ElideRight
                        text: sheet.inviteInfo ? sheet.roleLabel(sheet.inviteInfo.role) + (sheet.inviteInfo.approval ? "  ·  pede aprovação de um admin" : "  ·  entrada imediata") : ""
                        color: Theme.text3
                        font.pixelSize: Theme.fontSmall
                    }
                }
            }
        }

        Rectangle {
            visible: sheet.pendingNotice
            width: parent.width
            height: pendingText.implicitHeight + 2 * Theme.space4
            radius: Theme.radiusMd
            color: Theme.wash2
            border.width: 1
            border.color: Theme.lineStrong
            Text {
                id: pendingText
                anchors { left: parent.left; right: parent.right; margins: Theme.space4; verticalCenter: parent.verticalCenter }
                wrapMode: Text.WordWrap
                text: "Pedido enviado! Assim que um administrador aprovar a sua entrada, o espaço fica disponível na barra à esquerda."
                color: Theme.text
                font.pixelSize: Theme.fontBody
            }
        }
    }

    ErrorBanner {
        width: parent.width
        text: sheet.error
    }

    Row {
        spacing: Theme.space3
        PrimaryButton {
            visible: !sheet.pendingNotice
            text: sheet.tab === "create" ? "Criar espaço"
                : (sheet.inviteInfo && sheet.inviteInfo.approval ? "Pedir para entrar" : "Entrar no espaço")
            icon: "check"
            busy: sheet.busy
            enabled: !sheet.busy
            onClicked: sheet.submit()
        }
        PrimaryButton {
            visible: sheet.pendingNotice
            text: "Fechar"
            onClicked: sheet.shown = false
        }
        GhostButton {
            visible: !sheet.pendingNotice
            text: "Cancelar"
            onClicked: sheet.shown = false
        }
    }
}
