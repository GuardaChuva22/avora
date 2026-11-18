import QtQuick
import QtQuick.Controls.Basic
import QtQuick.Effects
import Avora
import "util.js" as U

// Create or edit a funcionário. Every save becomes a new version.
Sheet {
    id: ed
    property var agent: null // null = new agent
    property string projectId: "" // a new agent starts in this project
    property string error: ""
    property bool saving: false
    signal saved(var agent)

    property string color: "#c9b8ff"
    property var resps: [] // [{ id, goal, metric, cadence }]
    // How it talks (D50): how often it joins in #time, emojis, length.
    property string talk: ""
    property string emoji: ""
    property string length: ""
    // What it thinks with (D55): a provider, its model, its effort.
    property var brains: [] // GET /v1/brains
    property string brainProvider: ""
    property string brainModel: ""
    property string brainEffort: ""
    // When it asks you before acting (D56).
    property string approvals: ""
    readonly property var brain: brains.find(b => b.provider === brainProvider) || null
    readonly property var brainModels: {
        const all = brain ? brain.models || [] : [];
        const q = modelSearch.text.trim().toLowerCase();
        const hits = all.filter(m => q === "" || m.toLowerCase().includes(q) || modelName(m).toLowerCase().includes(q));
        const top = hits.slice(0, 10);
        if (brainModel !== "" && !top.includes(brainModel))
            top.unshift(brainModel);
        return top;
    }
    function modelName(m) {
        return brain && brain.model_names && brain.model_names[m] ? brain.model_names[m] : m;
    }

    readonly property var swatches: Theme.palette
    function open(a, project) {
        agent = a;
        projectId = project || "";
        error = "";
        saving = false;
        nameField.text = a ? a.name : "";
        roleField.text = a ? a.role : "";
        persona.text = a ? a.persona : "";
        color = a && a.color ? a.color : swatches[Math.floor(Math.random() * swatches.length)];
        resps = a && a.responsibilities ? a.responsibilities.map(r => Object.assign({}, r)) : [];
        const st = a && a.style ? a.style : {};
        voice.text = st.voice || "";
        talk = st.talk || "";
        emoji = st.emoji || "";
        length = st.length || "";
        const b = a && a.brain ? a.brain : {};
        brainProvider = b.provider || "";
        brainModel = b.model || "";
        brainEffort = b.effort || "";
        approvals = a && a.approvals ? a.approvals : "";
        modelSearch.text = "";
        Api.get("/v1/brains", (err, list) => {
            if (!err)
                brains = list;
        });
        shown = true;
        nameField.forceActiveFocus();
    }

    function save() {
        if (saving)
            return;
        const body = { name: nameField.text.trim(), role: roleField.text.trim(), persona: persona.text.trim(),
                       color: color,
                       style: { voice: voice.text.trim(), talk: talk, emoji: emoji, length: length },
                       approvals: approvals,
                       brain: { provider: brainProvider, model: brainProvider === "" ? "" : brainModel, effort: brainEffort },
                       responsibilities: resps.filter(r => r.goal.trim() !== "")
                                              .map(r => ({ id: r.id || "", goal: r.goal.trim(), metric: (r.metric || "").trim(), cadence: (r.cadence || "").trim() })) };
        if (!agent && projectId !== "")
            body.project_id = projectId;
        if (body.name === "") {
            error = "Dê um nome ao funcionário.";
            return;
        }
        saving = true;
        const done = (err, a) => {
            saving = false;
            if (err) {
                error = err;
                return;
            }
            shown = false;
            saved(a);
        };
        if (agent)
            Api.put("/v1/agents/" + agent.id, body, done);
        else
            Api.post("/v1/agents", body, done);
    }



    Row {
        spacing: 14
        Avatar {
            size: 46
            name: nameField.text || "?"
            photo: U.photo(ed.agent)
            tint: ed.color
            anchors.verticalCenter: parent.verticalCenter
        }
        Column {
            anchors.verticalCenter: parent.verticalCenter
            spacing: 3
            Text {
                text: ed.agent ? "Editar " + ed.agent.name : "Novo funcionário"
                color: Theme.white
                font.family: Theme.display
                font.pixelSize: Theme.fontHeading
                font.weight: Font.DemiBold
            }
            Text {
                text: ed.agent ? "Salvar cria a versão " + (ed.agent.version + 1) + "; dá para voltar depois." : "Nome, cargo e jeito de trabalhar."
                color: Theme.text3
                font.pixelSize: Theme.fontBody
            }
        }
    }

    Row {
        width: parent.width
        spacing: 12
        Column {
            width: (parent.width - 12) * 0.42
            spacing: 8
            Overline { text: "NOME" }
            Field { id: nameField; width: parent.width; placeholderText: "Ex.: Lia"; maximumLength: 40 }
        }
        Column {
            width: (parent.width - 12) * 0.58
            spacing: 8
            Overline { text: "CARGO" }
            Field { id: roleField; width: parent.width; placeholderText: "Ex.: Tutora de estudos"; maximumLength: 60 }
        }
    }

    Column {
        width: parent.width
        spacing: 8
        Overline { text: "PERSONA E FUNÇÕES" }
        AreaField {
            id: persona
            width: parent.width
            minHeight: 150
            maxHeight: 280
            enterSubmits: false
            placeholderText: "Como esse funcionário trabalha, do que cuida e o que nunca faz."
        }
    }

    // How it talks: its voice, and how it behaves in the group.
    Column {
        width: parent.width
        spacing: 10
        Overline { text: "JEITO DE FALAR" }
        Field {
            id: voice
            width: parent.width
            maximumLength: 400
            placeholderText: "Ex.: descontraída e direta, usa gíria de SP, zoa o time de leve"
        }
        Grid {
            width: parent.width
            columns: 2
            columnSpacing: 14
            rowSpacing: 10
            verticalItemAlignment: Grid.AlignVCenter
            Text { text: "No #time"; color: Theme.text2; font.pixelSize: Theme.fontBody }
            Segmented {
                value: ed.talk
                options: [{ value: "pouco", label: "Fala pouco" }, { value: "", label: "Normal" }, { value: "muito", label: "Participa muito" }]
                onActivated: v => ed.talk = v
            }
            Text { text: "Emoji"; color: Theme.text2; font.pixelSize: Theme.fontBody }
            Segmented {
                value: ed.emoji
                options: [{ value: "nunca", label: "Nunca" }, { value: "", label: "Às vezes" }, { value: "bastante", label: "Bastante" }]
                onActivated: v => ed.emoji = v
            }
            Text { text: "Respostas"; color: Theme.text2; font.pixelSize: Theme.fontBody }
            Segmented {
                value: ed.length
                options: [{ value: "curta", label: "Curtas" }, { value: "", label: "Normais" }, { value: "longa", label: "Detalhadas" }]
                onActivated: v => ed.length = v
            }
        }
    }

    // What it thinks with (D55): which account, model and effort.
    Column {
        width: parent.width
        spacing: 10
        Overline { text: "CÉREBRO" }
        Text {
            width: parent.width
            wrapMode: Text.Wrap
            text: "Quem pensa por esse funcionário. Seja qual for, ele age só pelas ferramentas do Avora, com as mesmas aprovações."
            color: Theme.text4
            font.pixelSize: Theme.fontSmall
        }
        Flow {
            width: parent.width
            spacing: 8
            Pill {
                text: "Automático"
                on: ed.brainProvider === ""
                onHit: {
                    ed.brainProvider = "";
                    ed.brainModel = "";
                }
            }
            Repeater {
                model: ed.brains
                delegate: Pill {
                    required property var modelData
                    text: modelData.name
                    on: ed.brainProvider === modelData.provider
                    dim: !modelData.ready
                    onHit: {
                        if (ed.brainProvider !== modelData.provider) {
                            ed.brainProvider = modelData.provider;
                            ed.brainModel = "";
                            modelSearch.text = "";
                        }
                    }
                }
            }
        }
        Text {
            visible: !!ed.brain && !ed.brain.ready
            width: parent.width
            wrapMode: Text.Wrap
            text: ed.brain ? "Agora: " + (ed.brain.note || "indisponível") + ". Enquanto isso, responde o Automático." : ""
            color: Theme.warn
            font.pixelSize: Theme.fontSmall
        }
        Column {
            visible: !!ed.brain && (ed.brain.models || []).length > 0
            width: parent.width
            spacing: 8
            Text { text: "Modelo"; color: Theme.text2; font.pixelSize: Theme.fontBody }
            Field {
                id: modelSearch
                visible: !!ed.brain && (ed.brain.models || []).length > 10
                width: parent.width
                placeholderText: "Buscar entre " + (ed.brain ? (ed.brain.models || []).length : 0) + " modelos"
            }
            Flow {
                width: parent.width
                spacing: 8
                Pill {
                    text: "Padrão"
                    on: ed.brainModel === ""
                    onHit: ed.brainModel = ""
                }
                Repeater {
                    model: ed.brainModels
                    delegate: Pill {
                        required property string modelData
                        text: ed.modelName(modelData)
                        on: ed.brainModel === modelData
                        onHit: ed.brainModel = modelData
                    }
                }
            }
        }
        Row {
            visible: ed.brainProvider === "" || !!ed.brain && (ed.brain.efforts || []).length > 0
            spacing: 14
            Text { text: "Esforço"; color: Theme.text2; font.pixelSize: Theme.fontBody; anchors.verticalCenter: parent.verticalCenter }
            Segmented {
                value: ed.brainEffort
                options: [{ value: "", label: "Padrão" }, { value: "low", label: "Baixo" }, { value: "medium", label: "Médio" },
                          { value: "high", label: "Alto" }, { value: "max", label: "Máximo" }]
                onActivated: v => ed.brainEffort = v
            }
        }
        Text {
            visible: ed.brainProvider === "" && ed.brainEffort !== ""
            width: parent.width
            wrapMode: Text.Wrap
            text: "No Automático, o esforço vale quando ele cai no Codex ou no Antigravity."
            color: Theme.text4
            font.pixelSize: Theme.fontSmall
        }
    }

    // When it asks before acting (D56).
    Column {
        width: parent.width
        spacing: 10
        Overline { text: "APROVAÇÕES" }
        Segmented {
            value: ed.approvals
            options: [{ value: "always", label: "Sempre pergunta" }, { value: "", label: "Só o arriscado" }, { value: "never", label: "Age sozinho" }]
            onActivated: v => ed.approvals = v
        }
        Text {
            width: parent.width
            wrapMode: Text.Wrap
            text: ed.approvals === "always" ? "Pede a sua aprovação antes de qualquer coisa que mude algo: criar rotinas, escrever nos seus apps e arquivos, rodar comandos."
                  : ed.approvals === "never" ? "Faz sem perguntar, inclusive enviar e apagar. Pagamentos e enviar ou comprar pelo navegador ainda pedem a sua aprovação."
                  : "Pergunta só antes de enviar, apagar, pagar ou editar arquivos. O resto ele faz e conta."
            color: ed.approvals === "never" ? Theme.warn : Theme.text4
            font.pixelSize: Theme.fontSmall
        }
    }

    // Standing goals: what to keep true, how to measure, how often.
    Column {
        width: parent.width
        spacing: 10
        Overline { text: "RESPONSABILIDADES" }
        Text {
            visible: ed.resps.length === 0
            width: parent.width
            wrapMode: Text.Wrap
            text: "Metas permanentes, com métrica e cadência. Ex.: gastos do mês abaixo do orçamento, checar todo dia às 8h. Uma rotina pode cuidar de cada uma."
            color: Theme.text4
            font.pixelSize: Theme.fontSmall
        }
        Repeater {
            model: ed.resps
            delegate: Rectangle {
                id: rr
                required property var modelData
                required property int index
                width: ed.form.width
                height: rrCol.implicitHeight + 20
                radius: Theme.radiusMd
                color: Theme.wash1
                border.width: 1
                border.color: Theme.line
                Column {
                    id: rrCol
                    x: 10
                    y: 10
                    width: parent.width - 52
                    spacing: 8
                    Field {
                        width: parent.width
                        placeholderText: "Objetivo. Ex.: gastos do mês abaixo do orçamento"
                        maximumLength: 200
                        text: rr.modelData.goal
                        onTextEdited: ed.resps[rr.index].goal = text
                    }
                    Row {
                        width: parent.width
                        spacing: 8
                        Field {
                            width: (parent.width - 8) * 0.58
                            placeholderText: "Métrica. Ex.: gasto / orçamento"
                            maximumLength: 200
                            text: rr.modelData.metric || ""
                            onTextEdited: ed.resps[rr.index].metric = text
                        }
                        Field {
                            width: (parent.width - 8) * 0.42
                            placeholderText: "Cadência. Ex.: todo dia 8h"
                            maximumLength: 100
                            text: rr.modelData.cadence || ""
                            onTextEdited: ed.resps[rr.index].cadence = text
                        }
                    }
                }
                GhostButton {
                    anchors { right: parent.right; rightMargin: 8; top: parent.top; topMargin: 10 }
                    round: true
                    small: true
                    icon: "close"
                    onClicked: {
                        const next = ed.resps.slice();
                        next.splice(rr.index, 1);
                        ed.resps = next;
                    }
                }
            }
        }
        GhostButton {
            visible: ed.resps.length < 10
            small: true
            icon: "plus"
            text: "Responsabilidade"
            onClicked: ed.resps = ed.resps.concat([{ id: "", goal: "", metric: "", cadence: "" }])
        }
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
                    readonly property bool on: ed.color === modelData
                    width: 26
                    height: 26
                    radius: Theme.radiusMd
                    color: "transparent"
                    border.width: on ? 2 : 0
                    border.color: Theme.white
                    Rectangle {
                        anchors.centerIn: parent
                        width: 18
                        height: 18
                        radius: Theme.radiusSm
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

    ErrorBanner {
        width: parent.width
        text: ed.error
    }

    Row {
        spacing: 10
        PrimaryButton {
            text: ed.agent ? "Salvar versão" : "Contratar"
            icon: "check"
            busy: ed.saving
            onClicked: ed.save()
        }
        GhostButton {
            text: "Cancelar"
            onClicked: ed.shown = false
        }
    }
}
