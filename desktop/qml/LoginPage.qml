import QtQuick
import Avora

// Sign-in screen: email and password first, account creation (with optional
// invite link or code preview), and a quiet link for operator admin tokens.
Item {
    id: login
    property string mode: "signin" // "signin" | "signup" | "token"
    property string error: ""
    property bool busy: false

    property var inviteInfo: null
    property string inviteError: ""
    property bool inviteLoading: false

    readonly property string inviteCode: parseInvite(inviteField.text).code

    onVisibleChanged: {
        if (visible) {
            error = "";
            busy = false;
            password.text = "";
            tokenField.text = "";
        }
    }

    function parseInvite(raw) {
        const t = (raw || "").trim();
        if (t === "")
            return { server: "", code: "" };
        const m = t.match(/^(https?:\/\/[^\/\s]+)?\/?(?:.*\/)?convite\/([^\/?#\s]+)/i);
        if (m)
            return { server: m[1] || "", code: decodeURIComponent(m[2]) };
        if (/^https?:\/\//i.test(t) || t.indexOf("/") >= 0)
            return { server: "", code: "" };
        return { server: "", code: t };
    }

    function roleLabel(r) {
        return r === "owner" ? "Dono" : r === "admin" ? "Administrador" : "Membro";
    }

    function switchMode(next) {
        mode = next;
        error = "";
    }

    function absorbInviteText(raw) {
        const p = parseInvite(raw);
        if (p.server !== "")
            server.text = p.server;
        if (p.code !== "") {
            inviteField.text = p.code;
            mode = "signup";
            lookupInvite();
            return true;
        }
        return false;
    }

    function pasteInvite() {
        const clip = Api.clipboardText();
        if (!absorbInviteText(clip)) {
            mode = "signup";
            inviteField.text = clip;
            lookupInvite();
        }
    }

    function lookupInvite() {
        const code = inviteCode;
        const srv = server.text.trim();
        if (code === "" || srv === "") {
            inviteInfo = null;
            inviteError = "";
            inviteLoading = false;
            return;
        }
        inviteLoading = true;
        inviteError = "";
        Api.checkInvite(srv, code, (err, data) => {
            if (code !== login.inviteCode || srv !== server.text.trim())
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
        id: inviteDebounce
        interval: 260
        onTriggered: login.lookupInvite()
    }

    function submit() {
        if (busy)
            return;
        const srv = server.text.trim();
        if (srv === "") {
            error = "Informe o endereço do seu servidor Avora.";
            server.forceActiveFocus();
            return;
        }
        if (mode === "token") {
            if (tokenField.text.trim() === "") {
                tokenField.forceActiveFocus();
                return;
            }
            busy = true;
            error = "";
            Api.login(srv, tokenField.text, err => {
                busy = false;
                if (err)
                    error = err;
                else
                    tokenField.text = "";
            });
            return;
        }
        if (mode === "signup" && nameField.text.trim() === "") {
            error = "Informe o seu nome.";
            nameField.forceActiveFocus();
            return;
        }
        if (email.text.trim() === "") {
            error = "Informe o seu e-mail.";
            email.forceActiveFocus();
            return;
        }
        if (password.text === "") {
            error = "Digite a sua senha.";
            password.forceActiveFocus();
            return;
        }
        if (mode === "signup" && password.text.length < 8) {
            error = "A senha precisa ter pelo menos 8 caracteres.";
            password.forceActiveFocus();
            return;
        }
        busy = true;
        error = "";
        const done = err => {
            busy = false;
            if (err)
                error = err;
            else
                password.text = "";
        };
        if (mode === "signup")
            Api.signUp(srv, email.text, nameField.text, password.text, inviteCode, done);
        else
            Api.signIn(srv, email.text, password.text, done);
    }

    Card {
        id: card
        anchors.centerIn: parent
        width: 432
        height: col.implicitHeight + 2 * padding
        padding: Theme.space6
        glow: true
        glowStrength: 0.05

        Column {
            id: col
            width: parent.width
            spacing: Theme.space4

            Column {
                width: parent.width
                spacing: Theme.space1
                Orb {
                    width: 96
                    height: 96
                    thinking: login.busy
                    anchors.horizontalCenter: parent.horizontalCenter
                }
                Text {
                    text: "Avora"
                    color: Theme.white
                    font.family: Theme.display
                    font.pixelSize: Theme.fontHero
                    font.weight: Font.DemiBold
                    font.letterSpacing: -0.6
                    anchors.horizontalCenter: parent.horizontalCenter
                }
                Text {
                    text: login.mode === "signup" ? "Crie a sua conta para entrar no espaço."
                        : login.mode === "token" ? "Acesso de operador com o token do servidor."
                        : "Entre na sua conta para conversar com o time."
                    color: Theme.text3
                    font.pixelSize: Theme.fontLarge
                    anchors.horizontalCenter: parent.horizontalCenter
                }
            }

            Column {
                width: parent.width
                spacing: Theme.space2
                Overline { text: "SERVIDOR" }
                Field {
                    id: server
                    width: parent.width
                    mono: true
                    text: Api.server
                    placeholderText: "https://avora.seu-dominio.com"
                    focus: Api.server === ""
                    onTextEdited: {
                        if (text.indexOf("/convite/") >= 0 && login.absorbInviteText(text))
                            return;
                        if (login.inviteCode !== "")
                            inviteDebounce.restart();
                    }
                    onAccepted: {
                        if (login.mode === "token")
                            tokenField.forceActiveFocus();
                        else if (login.mode === "signup")
                            nameField.forceActiveFocus();
                        else
                            email.forceActiveFocus();
                    }
                }
            }

            // Invite field + workspace preview (signup mode).
            Column {
                visible: login.mode === "signup"
                width: parent.width
                spacing: Theme.space2
                Overline { text: "CONVITE" }
                Row {
                    width: parent.width
                    spacing: Theme.space2
                    Field {
                        id: inviteField
                        width: parent.width - pasteInviteBtn.width - Theme.space2
                        mono: true
                        placeholderText: "Código ou link de convite (opcional)"
                        onTextEdited: {
                            const p = login.parseInvite(text);
                            if (p.server !== "") {
                                server.text = p.server;
                                text = p.code;
                            }
                            inviteDebounce.restart();
                        }
                        onAccepted: nameField.forceActiveFocus()
                    }
                    GhostButton {
                        id: pasteInviteBtn
                        icon: "paste"
                        text: "Colar"
                        height: 42
                        onClicked: login.pasteInvite()
                    }
                }

                Rectangle {
                    visible: login.inviteCode !== "" && (login.inviteLoading || login.inviteInfo !== null || login.inviteError !== "")
                    width: parent.width
                    height: invitePreviewRow.implicitHeight + 2 * Theme.space3
                    radius: Theme.radiusMd
                    color: login.inviteError !== "" ? Theme.badWash : Theme.wash2
                    border.width: 1
                    border.color: login.inviteError !== "" ? Theme.badLine : Theme.lineStrong

                    Row {
                        id: invitePreviewRow
                        x: Theme.space3
                        width: parent.width - 2 * Theme.space3
                        anchors.verticalCenter: parent.verticalCenter
                        spacing: Theme.space3

                        Rectangle {
                            visible: login.inviteInfo !== null
                            width: 36
                            height: 36
                            radius: Theme.radiusSm
                            color: Theme.wash4
                            border.width: 1
                            border.color: Theme.focusLine
                            anchors.verticalCenter: parent.verticalCenter
                            Text {
                                anchors.centerIn: parent
                                text: {
                                    const ws = login.inviteInfo && login.inviteInfo.workspace ? login.inviteInfo.workspace : null;
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
                            width: parent.width - (login.inviteInfo !== null ? 36 + Theme.space3 : 0)
                            anchors.verticalCenter: parent.verticalCenter
                            spacing: Theme.space1
                            Text {
                                width: parent.width
                                elide: Text.ElideRight
                                text: login.inviteLoading ? "Conferindo convite…"
                                    : login.inviteError !== "" ? login.inviteError
                                    : login.inviteInfo && login.inviteInfo.workspace ? login.inviteInfo.workspace.name : "Espaço convidado"
                                color: login.inviteError !== "" ? Theme.bad : Theme.white
                                font.pixelSize: Theme.fontBody
                                font.weight: login.inviteInfo !== null ? Font.DemiBold : Font.Normal
                            }
                            Text {
                                visible: login.inviteInfo !== null && !login.inviteLoading
                                width: parent.width
                                elide: Text.ElideRight
                                text: login.inviteInfo ? login.roleLabel(login.inviteInfo.role) + (login.inviteInfo.approval ? "  ·  pede aprovação para entrar" : "  ·  entrada imediata") : ""
                                color: Theme.text3
                                font.pixelSize: Theme.fontSmall
                            }
                        }
                    }
                }
            }

            Column {
                visible: login.mode === "signup"
                width: parent.width
                spacing: Theme.space2
                Overline { text: "SEU NOME" }
                Field {
                    id: nameField
                    width: parent.width
                    placeholderText: "Como as pessoas vão ver você"
                    maximumLength: 80
                    onAccepted: email.forceActiveFocus()
                }
            }

            Column {
                visible: login.mode !== "token"
                width: parent.width
                spacing: Theme.space2
                Overline { text: "E-MAIL" }
                Field {
                    id: email
                    width: parent.width
                    placeholderText: "voce@exemplo.com"
                    focus: Api.server !== "" && login.mode === "signin"
                    onTextEdited: {
                        if (text.indexOf("/convite/") >= 0) {
                            const raw = text;
                            text = "";
                            login.absorbInviteText(raw);
                        }
                    }
                    onAccepted: password.forceActiveFocus()
                }
            }

            Column {
                visible: login.mode !== "token"
                width: parent.width
                spacing: Theme.space2
                Overline { text: "SENHA" }
                Field {
                    id: password
                    width: parent.width
                    echoMode: TextInput.Password
                    passwordCharacter: "•"
                    placeholderText: login.mode === "signup" ? "Pelo menos 8 caracteres" : "••••••••••••"
                    onAccepted: login.submit()
                }
            }

            // Operator mode: raw admin token.
            Column {
                visible: login.mode === "token"
                width: parent.width
                spacing: Theme.space2
                Overline { text: "TOKEN DE ADMINISTRADOR" }
                Row {
                    width: parent.width
                    spacing: Theme.space2
                    Field {
                        id: tokenField
                        width: parent.width - pasteTokenBtn.width - Theme.space2
                        mono: true
                        echoMode: TextInput.Password
                        passwordCharacter: "•"
                        placeholderText: "••••••••••••••••••••••••"
                        onAccepted: login.submit()
                    }
                    GhostButton {
                        id: pasteTokenBtn
                        icon: "paste"
                        text: "Colar"
                        height: 42
                        onClicked: {
                            tokenField.text = Api.clipboardText();
                            login.submit();
                        }
                    }
                }
                Text {
                    width: parent.width
                    wrapMode: Text.WordWrap
                    text: "O token está no servidor, em /etc/avora/avora.env (AVORA_ADMIN_TOKEN). Do Windows, este comando copia ele sem mostrar na tela:"
                    color: Theme.text4
                    font.pixelSize: Theme.fontSmall
                }
                Rectangle {
                    width: parent.width
                    height: 34
                    radius: Theme.radiusSm
                    color: Theme.well
                    border.width: 1
                    border.color: Theme.line
                    Text {
                        anchors { left: parent.left; leftMargin: Theme.space3; verticalCenter: parent.verticalCenter }
                        text: "bash scripts/admin-token.sh"
                        color: Theme.text2
                        font.family: Theme.mono
                        font.pixelSize: Theme.fontSmall
                    }
                }
            }

            ErrorBanner {
                width: parent.width
                text: login.error
            }

            PrimaryButton {
                width: parent.width
                height: 44
                text: login.busy ? "Conferindo…"
                    : login.mode === "signup" ? (login.inviteInfo && login.inviteInfo.workspace && login.inviteInfo.workspace.name
                        ? "Criar conta em " + login.inviteInfo.workspace.name : "Criar conta")
                    : login.mode === "token" ? "Entrar como operador" : "Entrar"
                busy: login.busy
                enabled: !login.busy
                onClicked: login.submit()
            }

            // Mode switch links.
            Column {
                width: parent.width
                spacing: Theme.space2

                Row {
                    visible: login.mode === "signin"
                    anchors.horizontalCenter: parent.horizontalCenter
                    spacing: Theme.space3
                    Text {
                        text: "Criar conta"
                        color: signupLink.containsMouse ? Theme.white : Theme.text2
                        font.pixelSize: Theme.fontBody
                        font.weight: Font.Medium
                        MouseArea {
                            id: signupLink
                            anchors.fill: parent
                            hoverEnabled: true
                            cursorShape: Qt.PointingHandCursor
                            onClicked: login.switchMode("signup")
                        }
                    }
                    Text {
                        text: "·"
                        color: Theme.text4
                        font.pixelSize: Theme.fontBody
                    }
                    Text {
                        text: "Tenho um convite"
                        color: inviteLink.containsMouse ? Theme.white : Theme.text2
                        font.pixelSize: Theme.fontBody
                        font.weight: Font.Medium
                        MouseArea {
                            id: inviteLink
                            anchors.fill: parent
                            hoverEnabled: true
                            cursorShape: Qt.PointingHandCursor
                            onClicked: {
                                const clip = Api.clipboardText();
                                if (clip.indexOf("/convite/") >= 0) {
                                    login.absorbInviteText(clip);
                                } else {
                                    login.switchMode("signup");
                                    inviteField.forceActiveFocus();
                                }
                            }
                        }
                    }
                }

                Text {
                    visible: login.mode === "signup"
                    anchors.horizontalCenter: parent.horizontalCenter
                    text: "Já tem uma conta? Entrar"
                    color: backSigninLink.containsMouse ? Theme.white : Theme.text2
                    font.pixelSize: Theme.fontBody
                    font.weight: Font.Medium
                    MouseArea {
                        id: backSigninLink
                        anchors.fill: parent
                        hoverEnabled: true
                        cursorShape: Qt.PointingHandCursor
                        onClicked: login.switchMode("signin")
                    }
                }

                Text {
                    anchors.horizontalCenter: parent.horizontalCenter
                    text: login.mode === "token" ? "Voltar para e-mail e senha" : "Entrar com token de administrador"
                    color: operatorLink.containsMouse ? Theme.text2 : Theme.text4
                    font.pixelSize: Theme.fontSmall
                    MouseArea {
                        id: operatorLink
                        anchors.fill: parent
                        hoverEnabled: true
                        cursorShape: Qt.PointingHandCursor
                        onClicked: login.switchMode(login.mode === "token" ? "signin" : "token")
                    }
                }
            }
        }
    }

    Text {
        anchors { bottom: parent.bottom; bottomMargin: Theme.space5; horizontalCenter: parent.horizontalCenter }
        text: "A sua sessão fica cifrada neste computador (Windows DPAPI), só para o seu usuário."
        color: Theme.text4
        font.pixelSize: Theme.fontSmall
    }
}
