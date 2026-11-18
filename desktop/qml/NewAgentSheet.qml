import QtQuick
import QtQuick.Controls.Basic
import Avora
import "util.js" as U

// Two-step sheet to create an agent (Phase 5):
// Step 1: pick a ready template (PM, Dev, Pesquisa, Ops, Assistente pessoal, Em branco).
// Step 2: pick name, color and brain (Automático by default), then open its chat
// with a clickable "Pra eu começar:" checklist.
Sheet {
    id: sheet
    sheetWidth: 640

    property string projectId: ""
    property int step: 1
    property var templates: []
    property var chosen: null
    property string color: Theme.palette[0]
    property var brains: []
    property string brainProvider: ""
    property string brainModel: ""
    property bool saving: false
    property string error: ""

    signal created(var agent, string conversationId)

    readonly property var currentBrain: brains.find(b => b.provider === brainProvider) || null
    readonly property var currentModels: currentBrain && currentBrain.models ? currentBrain.models.slice(0, 8) : []

    function modelLabel(m) {
        return currentBrain && currentBrain.model_names && currentBrain.model_names[m]
            ? currentBrain.model_names[m] : m;
    }

    function open(project) {
        projectId = project || "";
        step = 1;
        chosen = null;
        error = "";
        saving = false;
        brainProvider = "";
        brainModel = "";
        color = Theme.palette[0];
        nameField.text = "";
        roleField.text = "";

        Api.get("/v1/agent-templates", (err, list) => {
            if (!err && list)
                sheet.templates = list;
        });
        Api.get("/v1/brains", (err, list) => {
            if (!err && list)
                sheet.brains = list;
        });
        shown = true;
    }

    function pickTemplate(tpl) {
        chosen = tpl;
        error = "";
        nameField.text = tpl.default_name || "";
        roleField.text = tpl.role || "";
        color = tpl.color && tpl.color !== "" ? tpl.color : Theme.palette[0];
        brainProvider = "";
        brainModel = "";
        step = 2;
        Qt.callLater(() => nameField.forceActiveFocus());
    }

    function create() {
        if (saving)
            return;
        const cleanName = nameField.text.trim();
        if (cleanName === "") {
            error = "Dê um nome ao agente.";
            nameField.forceActiveFocus();
            return;
        }
        const tpl = chosen || { id: "em-branco", persona: "", responsibilities: [], suggested_skills: [] };
        const body = {
            name: cleanName,
            role: roleField.text.trim(),
            persona: tpl.persona || "",
            color: color,
            responsibilities: tpl.responsibilities || [],
            brain: {
                provider: brainProvider,
                model: brainProvider === "" ? "" : brainModel,
                effort: ""
            }
        };
        if (projectId !== "")
            body.project_id = projectId;

        saving = true;
        error = "";
        Api.post("/v1/agents", body, (err, agent) => {
            if (err) {
                sheet.saving = false;
                sheet.error = err;
                return;
            }
            Api.post("/v1/agents/" + encodeURIComponent(agent.id) + "/welcome", {
                template_id: tpl.id || "",
                suggested_skills: tpl.suggested_skills || []
            }, (wErr, welcome) => {
                sheet.saving = false;
                sheet.shown = false;
                const convId = (!wErr && welcome && welcome.conversation) ? (welcome.conversation.id || "") : "";
                sheet.created(agent, convId);
            });
        });
    }

    // Header
    Item {
        width: parent.width
        height: headCol.implicitHeight

        Column {
            id: headCol
            width: parent.width
            spacing: Theme.space1

            Overline {
                text: sheet.step === 1 ? "PASSO 1 DE 2 · MODELO" : "PASSO 2 DE 2 · IDENTIDADE E CÉREBRO"
            }
            Text {
                text: sheet.step === 1 ? "Novo agente" : ("Configurar " + (nameField.text.trim() || "novo agente"))
                color: Theme.white
                font.family: Theme.display
                font.pixelSize: Theme.fontHeading
                font.weight: Font.DemiBold
            }
            Text {
                width: parent.width
                wrapMode: Text.WordWrap
                text: sheet.step === 1
                    ? "Escolha um modelo pronto para já vir com instruções e responsabilidades, ou comece em branco."
                    : "Defina como ele se chama, a cor dele e com qual cérebro pensa."
                color: Theme.text3
                font.pixelSize: Theme.fontBody
            }
        }
    }

    // Step 1: Template cards
    Column {
        visible: sheet.step === 1
        width: parent.width
        spacing: Theme.space3

        Text {
            visible: sheet.templates.length === 0
            text: "Carregando modelos prontos…"
            color: Theme.text3
            font.pixelSize: Theme.fontBody
        }

        Grid {
            id: tplGrid
            width: parent.width
            columns: 2
            columnSpacing: Theme.space3
            rowSpacing: Theme.space3

            Repeater {
                model: sheet.templates
                delegate: Rectangle {
                    id: tplCard
                    required property var modelData
                    readonly property color accent: modelData.color || Theme.palette[0]
                    width: (tplGrid.width - Theme.space3) / 2
                    height: tplCol.implicitHeight + 28
                    radius: Theme.radiusLg
                    color: tplArea.containsMouse ? Theme.hover : Theme.wash1
                    border.width: 1
                    border.color: tplArea.containsMouse ? Theme.focusLine : Theme.line
                    Behavior on color { ColorAnimation { duration: Theme.fast } }
                    Behavior on border.color { ColorAnimation { duration: Theme.fast } }

                    Column {
                        id: tplCol
                        x: 14
                        y: 14
                        width: parent.width - 28
                        spacing: Theme.space2

                        Row {
                            width: parent.width
                            spacing: Theme.space2

                            Avatar {
                                size: 32
                                name: tplCard.modelData.default_name || tplCard.modelData.title || "?"
                                tint: tplCard.accent
                                anchors.verticalCenter: parent.verticalCenter
                            }

                            Column {
                                width: parent.width - 44
                                anchors.verticalCenter: parent.verticalCenter
                                spacing: 1

                                Text {
                                    width: parent.width
                                    text: tplCard.modelData.title
                                    color: Theme.white
                                    font.pixelSize: Theme.fontLarge
                                    font.weight: Font.DemiBold
                                    elide: Text.ElideRight
                                }
                                Text {
                                    visible: (tplCard.modelData.role || "") !== ""
                                    width: parent.width
                                    text: tplCard.modelData.role || ""
                                    color: Theme.text4
                                    font.pixelSize: Theme.fontCaption
                                    elide: Text.ElideRight
                                }
                            }
                        }

                        Text {
                            width: parent.width
                            wrapMode: Text.WordWrap
                            text: tplCard.modelData.summary || ""
                            color: Theme.text2
                            font.pixelSize: Theme.fontSmall
                            maximumLineCount: 3
                            elide: Text.ElideRight
                        }

                        Flow {
                            visible: (tplCard.modelData.suggested_skills || []).length > 0
                            width: parent.width
                            spacing: Theme.space1

                            Repeater {
                                model: tplCard.modelData.suggested_skills || []
                                delegate: Rectangle {
                                    required property string modelData
                                    height: 20
                                    width: skTxt.implicitWidth + 12
                                    radius: height / 2
                                    color: Theme.wash3
                                    Text {
                                        id: skTxt
                                        anchors.centerIn: parent
                                        text: modelData
                                        color: Theme.text3
                                        font.pixelSize: Theme.fontTiny
                                    }
                                }
                            }
                        }
                    }

                    MouseArea {
                        id: tplArea
                        anchors.fill: parent
                        hoverEnabled: true
                        cursorShape: Qt.PointingHandCursor
                        onClicked: sheet.pickTemplate(tplCard.modelData)
                    }
                }
            }
        }
    }

    // Step 2: Name, Color & Brain
    Column {
        visible: sheet.step === 2
        width: parent.width
        spacing: Theme.space4

        Row {
            width: parent.width
            spacing: Theme.space3

            Avatar {
                size: 48
                name: nameField.text.trim() || "?"
                tint: sheet.color
                anchors.verticalCenter: parent.verticalCenter
            }

            Row {
                width: parent.width - 48 - Theme.space3
                spacing: Theme.space3
                anchors.verticalCenter: parent.verticalCenter

                Column {
                    width: (parent.width - Theme.space3) * 0.5
                    spacing: Theme.space2
                    Overline { text: "NOME" }
                    Field {
                        id: nameField
                        width: parent.width
                        maximumLength: 40
                        placeholderText: "Ex.: Clara"
                        onAccepted: sheet.create()
                    }
                }

                Column {
                    width: (parent.width - Theme.space3) * 0.5
                    spacing: Theme.space2
                    Overline { text: "FUNÇÃO" }
                    Field {
                        id: roleField
                        width: parent.width
                        maximumLength: 60
                        placeholderText: "Ex.: Produto, Dev, Pesquisa…"
                        onAccepted: sheet.create()
                    }
                }
            }
        }

        Column {
            width: parent.width
            spacing: Theme.space2

            Overline { text: "COR" }

            Row {
                spacing: Theme.space2
                Repeater {
                    model: Theme.palette
                    delegate: Rectangle {
                        id: sw
                        required property string modelData
                        readonly property bool on: sheet.color.toLowerCase() === modelData.toLowerCase()
                        width: 28
                        height: 28
                        radius: height / 2
                        color: modelData
                        border.width: on ? 2 : 1
                        border.color: on ? Theme.white : Theme.lineStrong

                        Icon {
                            visible: sw.on
                            anchors.centerIn: parent
                            name: "check"
                            size: 14
                            weight: 2.2
                            color: Theme.ink
                        }

                        MouseArea {
                            anchors.fill: parent
                            cursorShape: Qt.PointingHandCursor
                            onClicked: sheet.color = sw.modelData
                        }
                    }
                }
            }
        }

        Column {
            width: parent.width
            spacing: Theme.space2

            Overline { text: "CÉREBRO" }

            Text {
                width: parent.width
                wrapMode: Text.WordWrap
                text: "No modo Automático, o agente usa a conta principal conectada no espaço."
                color: Theme.text4
                font.pixelSize: Theme.fontSmall
            }

            Flow {
                width: parent.width
                spacing: Theme.space2

                Pill {
                    text: "Automático"
                    on: sheet.brainProvider === ""
                    onHit: {
                        sheet.brainProvider = "";
                        sheet.brainModel = "";
                    }
                }

                Repeater {
                    model: sheet.brains
                    delegate: Pill {
                        required property var modelData
                        text: modelData.name
                        on: sheet.brainProvider === modelData.provider
                        dim: !modelData.ready
                        onHit: {
                            sheet.brainProvider = modelData.provider;
                            sheet.brainModel = (modelData.models && modelData.models.length > 0) ? modelData.models[0] : "";
                        }
                    }
                }
            }

            Flow {
                visible: sheet.brainProvider !== "" && sheet.currentModels.length > 0
                width: parent.width
                spacing: Theme.space2

                Repeater {
                    model: sheet.currentModels
                    delegate: Pill {
                        required property string modelData
                        text: sheet.modelLabel(modelData)
                        on: sheet.brainModel === modelData
                        onHit: sheet.brainModel = modelData
                    }
                }
            }
        }

        ErrorBanner {
            width: parent.width
            text: sheet.error
        }

        Item {
            width: parent.width
            height: 42

            GhostButton {
                anchors { left: parent.left; verticalCenter: parent.verticalCenter }
                icon: "back"
                text: "Modelos"
                onClicked: sheet.step = 1
            }

            Row {
                anchors { right: parent.right; verticalCenter: parent.verticalCenter }
                spacing: Theme.space2

                GhostButton {
                    text: "Cancelar"
                    anchors.verticalCenter: parent.verticalCenter
                    onClicked: sheet.shown = false
                }
                PrimaryButton {
                    text: "Criar agente"
                    icon: "check"
                    busy: sheet.saving
                    anchors.verticalCenter: parent.verticalCenter
                    onClicked: sheet.create()
                }
            }
        }
    }
}
