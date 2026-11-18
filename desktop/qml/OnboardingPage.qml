import QtCore
import QtQuick
import QtQuick.Controls.Basic
import QtQuick.Dialogs
import Avora
import "util.js" as U

// Guided first-run flow (Phase 5): shown when the workspace has no agents
// and the person has not finished onboarding yet. Can be skipped and resumed.
Rectangle {
    id: root
    color: Theme.bg

    property string workspaceId: "pessoal"
    property int step: 1
    property var templates: []
    property var brains: []
    property string startChoice: "team" // "team" (3 ready agents) or "scratch"
    property string avatarValue: ""
    property bool uploadingPhoto: false
    property bool busy: false
    property string error: ""
    property bool prefilled: false

    signal skipped
    signal connectAIRequested
    signal completed(string channelId)

    readonly property var person: (Api.me && Api.me.person && Api.me.person.id) ? Api.me.person : null
    readonly property var starterTemplates: {
        const wanted = ["assistente", "dev", "pesquisa"];
        const out = [];
        for (const id of wanted) {
            const found = templates.find(t => t.id === id);
            if (found)
                out.push(found);
        }
        return out.length > 0 ? out : templates.slice(0, 3);
    }
    readonly property var readyBrains: brains.filter(b => b.ready)

    function personPhotoUrl(av) {
        if (!av)
            return "";
        if (av.startsWith("avora:file/"))
            return "image://server/v1/files/" + av.slice(11);
        if (av.startsWith("/v1/files/"))
            return "image://server" + av;
        return av;
    }

    function refreshData() {
        Api.get("/v1/agent-templates", (err, list) => {
            if (!err && list)
                root.templates = list;
        });
        Api.get("/v1/brains", (err, list) => {
            if (!err && list)
                root.brains = list;
        });
        if (!prefilled && person) {
            nameField.text = person.name || "";
            aboutField.text = person.about || "";
            avatarValue = person.avatar || "";
            prefilled = true;
        }
    }

    onPersonChanged: {
        if (!prefilled && person) {
            nameField.text = person.name || "";
            aboutField.text = person.about || "";
            avatarValue = person.avatar || "";
            prefilled = true;
        }
    }

    onVisibleChanged: if (visible) refreshData()
    Component.onCompleted: refreshData()

    function saveStep1() {
        if (busy)
            return;
        const cleanName = nameField.text.trim();
        if (cleanName === "") {
            error = "Diga como podemos chamar você.";
            nameField.forceActiveFocus();
            return;
        }
        busy = true;
        error = "";
        const body = {
            name: cleanName,
            about: aboutField.text.trim()
        };
        if (avatarValue !== "")
            body.avatar = avatarValue;

        Api.patch("/v1/me", body, err => {
            root.busy = false;
            if (err) {
                root.error = err;
                return;
            }
            Api.refreshMe();
            root.step = 2;
        });
    }

    function saveStep2() {
        if (busy)
            return;
        error = "";
        if (startChoice === "scratch") {
            step = 3;
            return;
        }
        const list = starterTemplates;
        if (list.length === 0) {
            step = 3;
            return;
        }
        busy = true;
        let idx = 0;
        const next = () => {
            if (idx >= list.length) {
                root.busy = false;
                root.step = 3;
                return;
            }
            const tpl = list[idx++];
            Api.post("/v1/agents", {
                name: tpl.default_name || tpl.title,
                role: tpl.role || "",
                persona: tpl.persona || "",
                color: tpl.color || Theme.palette[idx % Theme.palette.length],
                responsibilities: tpl.responsibilities || []
            }, err => {
                // Ignore 409 if retrying after partial creation.
                if (err && err.indexOf("already exists") < 0 && err.indexOf("409") < 0) {
                    root.busy = false;
                    root.error = err;
                    return;
                }
                next();
            });
        };
        next();
    }

    function finishStep4() {
        if (busy)
            return;
        busy = true;
        error = "";
        Api.post("/v1/onboarding/greet", null, (err, res) => {
            root.busy = false;
            if (err) {
                root.error = err;
                return;
            }
            root.completed((res && res.channel_id) ? res.channel_id : "time");
        });
    }

    FileDialog {
        id: photoPicker
        title: "Escolher sua foto"
        nameFilters: ["Imagens (*.png *.jpg *.jpeg *.webp)"]
        onAccepted: {
            root.uploadingPhoto = true;
            root.error = "";
            Api.uploadFile("", selectedFile, (err, f) => {
                root.uploadingPhoto = false;
                if (err) {
                    root.error = err;
                    return;
                }
                if (f && f.id)
                    root.avatarValue = "avora:file/" + f.id;
            });
        }
    }

    Flickable {
        id: scroller
        anchors.fill: parent
        contentHeight: Math.max(scroller.height, col.implicitHeight + 96)
        clip: true
        boundsBehavior: Flickable.StopAtBounds
        ScrollBar.vertical: SlimScrollBar {}
        SmoothWheel {}

        Column {
            id: col
            readonly property real cardWidth: Math.min(scroller.width - 2 * Theme.gutter, 620)
            x: Math.max(Theme.gutter, (scroller.width - cardWidth) / 2)
            y: Math.max(48, (scroller.height - col.implicitHeight) / 2)
            width: cardWidth
            spacing: Theme.space4

            // Progress & Skip bar
            Item {
                width: parent.width
                height: 32

                Row {
                    anchors.verticalCenter: parent.verticalCenter
                    spacing: Theme.space2

                    Repeater {
                        model: [1, 2, 3, 4]
                        delegate: Rectangle {
                            required property int modelData
                            width: modelData === root.step ? 28 : 10
                            height: 6
                            radius: height / 2
                            color: modelData <= root.step ? Theme.white : Theme.wash4
                            Behavior on width { NumberAnimation { duration: Theme.medium; easing.type: Theme.ease } }
                        }
                    }

                    Text {
                        leftPadding: Theme.space2
                        text: "Passo " + root.step + " de 4"
                        color: Theme.text4
                        font.pixelSize: Theme.fontCaption
                        font.weight: Font.DemiBold
                        anchors.verticalCenter: parent.verticalCenter
                    }
                }

                GhostButton {
                    anchors { right: parent.right; verticalCenter: parent.verticalCenter }
                    small: true
                    text: "Pular por agora"
                    onClicked: root.skipped()
                }
            }

            Card {
                width: parent.width
                height: innerStep.implicitHeight + 2 * padding
                padding: Theme.space5

                Column {
                    id: innerStep
                    width: parent.width
                    spacing: Theme.space4

                    // ── STEP 1: Sobre você ──────────────────────────────────
                    Column {
                        visible: root.step === 1
                        width: parent.width
                        spacing: Theme.space4

                        Column {
                            width: parent.width
                            spacing: Theme.space1
                            Overline { text: "ABERTURA GUIADA" }
                            Text {
                                text: "Sobre você"
                                color: Theme.white
                                font.family: Theme.display
                                font.pixelSize: Theme.fontDisplay
                                font.weight: Font.DemiBold
                            }
                            Text {
                                width: parent.width
                                wrapMode: Text.WordWrap
                                text: "Todo agente lê esse perfil antes de responder, para já conhecer você e o seu contexto."
                                color: Theme.text3
                                font.pixelSize: Theme.fontBody
                            }
                        }

                        Row {
                            width: parent.width
                            spacing: Theme.space4

                            Avatar {
                                size: 60
                                name: nameField.text.trim() || "?"
                                photo: root.personPhotoUrl(root.avatarValue)
                                tint: Theme.palette[0]
                                anchors.verticalCenter: parent.verticalCenter
                            }

                            Column {
                                anchors.verticalCenter: parent.verticalCenter
                                spacing: Theme.space2

                                GhostButton {
                                    small: true
                                    icon: "upload"
                                    text: root.uploadingPhoto ? "Enviando…" : (root.avatarValue !== "" ? "Trocar foto" : "Escolher foto")
                                    enabled: !root.uploadingPhoto
                                    onClicked: photoPicker.open()
                                }
                                Text {
                                    text: "Opcional · aparece no seu perfil e nos espaços."
                                    color: Theme.text4
                                    font.pixelSize: Theme.fontCaption
                                }
                            }
                        }

                        Column {
                            width: parent.width
                            spacing: Theme.space2
                            Overline { text: "SEU NOME" }
                            Field {
                                id: nameField
                                width: parent.width
                                maximumLength: 80
                                placeholderText: "Ex.: Ana Silva"
                            }
                        }

                        Column {
                            width: parent.width
                            spacing: Theme.space2
                            Overline { text: "DO QUE VOCÊ CUIDA" }
                            AreaField {
                                id: aboutField
                                width: parent.width
                                minHeight: 96
                                maxHeight: 180
                                enterSubmits: false
                                placeholderText: "Ex.: Cuido de produto e atendimento na minha empresa, acompanho entregas de clientes e organizo minhas finanças pessoais."
                            }
                        }

                        Row {
                            anchors.right: parent.right
                            spacing: Theme.space2
                            PrimaryButton {
                                text: "Continuar"
                                icon: "chevron"
                                busy: root.busy
                                onClicked: root.saveStep1()
                            }
                        }
                    }

                    // ── STEP 2: Time pronto ou do zero ──────────────────────
                    Column {
                        visible: root.step === 2
                        width: parent.width
                        spacing: Theme.space4

                        Column {
                            width: parent.width
                            spacing: Theme.space1
                            Overline { text: "SEU TIME" }
                            Text {
                                text: "Como prefere começar?"
                                color: Theme.white
                                font.family: Theme.display
                                font.pixelSize: Theme.fontDisplay
                                font.weight: Font.DemiBold
                            }
                            Text {
                                width: parent.width
                                wrapMode: Text.WordWrap
                                text: "Você pode editar, arquivar ou criar outros agentes a qualquer momento."
                                color: Theme.text3
                                font.pixelSize: Theme.fontBody
                            }
                        }

                        Column {
                            width: parent.width
                            spacing: Theme.space3

                            // Option A: Ready team of 3 agents
                            Rectangle {
                                width: parent.width
                                height: optTeamCol.implicitHeight + 28
                                radius: Theme.radiusLg
                                color: root.startChoice === "team" ? Theme.active : optTeamArea.containsMouse ? Theme.hover : Theme.wash1
                                border.width: 1
                                border.color: root.startChoice === "team" ? Theme.focusLine : Theme.line

                                Column {
                                    id: optTeamCol
                                    x: 16
                                    y: 14
                                    width: parent.width - 32
                                    spacing: Theme.space2

                                    Row {
                                        spacing: Theme.space2
                                        Dot {
                                            kind: root.startChoice === "team" ? "live" : ""
                                            anchors.verticalCenter: parent.verticalCenter
                                        }
                                        Text {
                                            text: "Começar com um time pronto (3 agentes)"
                                            color: Theme.white
                                            font.pixelSize: Theme.fontLarge
                                            font.weight: Font.DemiBold
                                            anchors.verticalCenter: parent.verticalCenter
                                        }
                                    }

                                    Text {
                                        width: parent.width
                                        wrapMode: Text.WordWrap
                                        text: "Cria Assistente pessoal, Dev e Pesquisa já configurados para conversar e dividir tarefas."
                                        color: Theme.text3
                                        font.pixelSize: Theme.fontBody
                                    }

                                    Flow {
                                        width: parent.width
                                        spacing: Theme.space2
                                        Repeater {
                                            model: root.starterTemplates
                                            delegate: Row {
                                                required property var modelData
                                                spacing: Theme.space1
                                                Avatar {
                                                    size: 22
                                                    name: modelData.default_name || modelData.title
                                                    tint: modelData.color || Theme.palette[0]
                                                    anchors.verticalCenter: parent.verticalCenter
                                                }
                                                Text {
                                                    text: (modelData.default_name || modelData.title) + " · " + (modelData.role || modelData.title)
                                                    color: Theme.text2
                                                    font.pixelSize: Theme.fontSmall
                                                    anchors.verticalCenter: parent.verticalCenter
                                                }
                                            }
                                        }
                                    }
                                }

                                MouseArea {
                                    id: optTeamArea
                                    anchors.fill: parent
                                    hoverEnabled: true
                                    cursorShape: Qt.PointingHandCursor
                                    onClicked: root.startChoice = "team"
                                }
                            }

                            // Option B: Start from scratch
                            Rectangle {
                                width: parent.width
                                height: optZeroCol.implicitHeight + 28
                                radius: Theme.radiusLg
                                color: root.startChoice === "scratch" ? Theme.active : optZeroArea.containsMouse ? Theme.hover : Theme.wash1
                                border.width: 1
                                border.color: root.startChoice === "scratch" ? Theme.focusLine : Theme.line

                                Column {
                                    id: optZeroCol
                                    x: 16
                                    y: 14
                                    width: parent.width - 32
                                    spacing: Theme.space1

                                    Row {
                                        spacing: Theme.space2
                                        Dot {
                                            kind: root.startChoice === "scratch" ? "live" : ""
                                            anchors.verticalCenter: parent.verticalCenter
                                        }
                                        Text {
                                            text: "Começar do zero"
                                            color: Theme.white
                                            font.pixelSize: Theme.fontLarge
                                            font.weight: Font.DemiBold
                                            anchors.verticalCenter: parent.verticalCenter
                                        }
                                    }

                                    Text {
                                        width: parent.width
                                        wrapMode: Text.WordWrap
                                        text: "Abre o espaço limpo para você criar cada agente manualmente quando quiser."
                                        color: Theme.text3
                                        font.pixelSize: Theme.fontBody
                                    }
                                }

                                MouseArea {
                                    id: optZeroArea
                                    anchors.fill: parent
                                    hoverEnabled: true
                                    cursorShape: Qt.PointingHandCursor
                                    onClicked: root.startChoice = "scratch"
                                }
                            }
                        }

                        Item {
                            width: parent.width
                            height: 42

                            GhostButton {
                                anchors { left: parent.left; verticalCenter: parent.verticalCenter }
                                icon: "back"
                                text: "Voltar"
                                onClicked: root.step = 1
                            }
                            PrimaryButton {
                                anchors { right: parent.right; verticalCenter: parent.verticalCenter }
                                text: root.startChoice === "team" ? "Criar meu time" : "Continuar do zero"
                                icon: "check"
                                busy: root.busy
                                onClicked: root.saveStep2()
                            }
                        }
                    }

                    // ── STEP 3: Conectar uma IA ─────────────────────────────
                    Column {
                        visible: root.step === 3
                        width: parent.width
                        spacing: Theme.space4

                        Column {
                            width: parent.width
                            spacing: Theme.space1
                            Overline { text: "CÉREBRO DOS AGENTES" }
                            Text {
                                text: "Conectar uma IA"
                                color: Theme.white
                                font.family: Theme.display
                                font.pixelSize: Theme.fontDisplay
                                font.weight: Font.DemiBold
                            }
                            Text {
                                width: parent.width
                                wrapMode: Text.WordWrap
                                text: "Ligue sua assinatura (Claude, Codex, Antigravity) ou uma chave de API para os agentes pensarem e responderem."
                                color: Theme.text3
                                font.pixelSize: Theme.fontBody
                            }
                        }

                        Rectangle {
                            visible: root.readyBrains.length > 0
                            width: parent.width
                            height: readyRow.implicitHeight + 24
                            radius: Theme.radiusMd
                            color: Theme.wash1
                            border.width: 1
                            border.color: Theme.line

                            Row {
                                id: readyRow
                                x: Theme.space3
                                anchors.verticalCenter: parent.verticalCenter
                                spacing: Theme.space2

                                Dot {
                                    kind: "ok"
                                    anchors.verticalCenter: parent.verticalCenter
                                }
                                Text {
                                    text: "Já há IA pronta neste espaço: " + root.readyBrains.map(b => b.name).join(", ")
                                    color: Theme.ok
                                    font.pixelSize: Theme.fontBody
                                    anchors.verticalCenter: parent.verticalCenter
                                }
                            }
                        }

                        Row {
                            spacing: Theme.space2

                            PrimaryButton {
                                visible: root.readyBrains.length === 0
                                icon: "spark"
                                text: "Conectar uma IA"
                                onClicked: {
                                    root.step = 4;
                                    root.connectAIRequested();
                                }
                            }
                            GhostButton {
                                visible: root.readyBrains.length > 0
                                icon: "plus"
                                text: "Conectar outra conta"
                                onClicked: {
                                    root.step = 4;
                                    root.connectAIRequested();
                                }
                            }
                        }

                        Item {
                            width: parent.width
                            height: 42

                            GhostButton {
                                anchors { left: parent.left; verticalCenter: parent.verticalCenter }
                                icon: "back"
                                text: "Voltar"
                                onClicked: root.step = 2
                            }
                            Row {
                                anchors { right: parent.right; verticalCenter: parent.verticalCenter }
                                spacing: Theme.space2

                                GhostButton {
                                    visible: root.readyBrains.length === 0
                                    text: "Conectar depois"
                                    onClicked: root.step = 4
                                }
                                PrimaryButton {
                                    visible: root.readyBrains.length > 0
                                    text: "Continuar"
                                    icon: "chevron"
                                    onClicked: root.step = 4
                                }
                            }
                        }
                    }

                    // ── STEP 4: O time diz oi no canal ──────────────────────
                    Column {
                        visible: root.step === 4
                        width: parent.width
                        spacing: Theme.space4

                        Column {
                            width: parent.width
                            spacing: Theme.space1
                            Overline { text: "PRONTO PARA COMEÇAR" }
                            Text {
                                text: root.startChoice === "team" ? "O time diz oi no canal" : "Seu espaço está pronto"
                                color: Theme.white
                                font.family: Theme.display
                                font.pixelSize: Theme.fontDisplay
                                font.weight: Font.DemiBold
                            }
                            Text {
                                width: parent.width
                                wrapMode: Text.WordWrap
                                text: root.startChoice === "team"
                                    ? "Vamos abrir o canal #time para os seus agentes darem as boas-vindas e você fazer o primeiro pedido."
                                    : "Tudo configurado! Você já pode criar agentes quando quiser ou explorar o seu espaço."
                                color: Theme.text3
                                font.pixelSize: Theme.fontBody
                            }
                        }

                        Item {
                            width: parent.width
                            height: 42

                            GhostButton {
                                anchors { left: parent.left; verticalCenter: parent.verticalCenter }
                                icon: "back"
                                text: "Voltar"
                                onClicked: root.step = 3
                            }
                            PrimaryButton {
                                anchors { right: parent.right; verticalCenter: parent.verticalCenter }
                                icon: "chat"
                                text: root.startChoice === "team" ? "Ver o time no #time" : "Entrar no Avora"
                                busy: root.busy
                                onClicked: root.finishStep4()
                            }
                        }
                    }

                    ErrorBanner {
                        width: parent.width
                        text: root.error
                    }
                }
            }
        }
    }
}
