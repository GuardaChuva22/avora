import QtQuick
import QtQuick.Controls.Basic
import Avora
import "util.js" as U

// Agent settings tab: identity, color, brain, project assignment and archive danger zone.
Column {
    id: tab
    required property var page
    spacing: Theme.space5

    property bool ready: false
    property bool syncing: false

    property bool removingPhoto: false
    property string localPhotoError: ""

    property var projects: []
    property bool loadingProjects: false
    property bool movingProject: false
    property string projectError: ""

    property bool confirmArchive: false

    readonly property string agentId: page && page.agent ? page.agent.id : ""
    readonly property string agentName: page && page.agent && page.agent.name ? page.agent.name : ""
    readonly property bool hasAvatar: !!(page && page.agent && page.agent.avatar_at)
    readonly property bool uploading: !!(page && page.uploading)
    readonly property string photoErr: (page && page.photoError ? page.photoError : "") || localPhotoError

    readonly property string draftName: page && page.draft && page.draft.name !== undefined ? page.draft.name : ""
    readonly property string draftRole: page && page.draft && page.draft.role !== undefined ? page.draft.role : ""
    readonly property string draftColor: page && page.draft && page.draft.color ? page.draft.color : ""

    readonly property var brains: page && page.brains ? page.brains : []
    readonly property var draftBrain: page && page.draft && page.draft.brain ? page.draft.brain : ({})
    readonly property string brainProvider: draftBrain.provider || ""
    readonly property string brainModel: draftBrain.model || ""
    readonly property string brainEffort: draftBrain.effort || ""
    readonly property string brainFallback: draftBrain.fallback === "none" ? "none" : ""
    readonly property bool brainRotate: draftBrain.rotate !== false
    readonly property string brainAccount: draftBrain.account || ""
    readonly property int brainContext: draftBrain.context || 0
    readonly property string brainSpeed: draftBrain.speed || ""
    readonly property var currentBrain: brains.find(b => b.provider === brainProvider) || null
    readonly property var brainAccounts: currentBrain && currentBrain.accounts ? currentBrain.accounts : []
    readonly property bool supportsSpeed: !!(currentBrain && currentBrain.speeds && currentBrain.speeds.length > 0)
    readonly property int maxModelContext: {
        if (!currentBrain)
            return 1000000;
        if (brainModel !== "" && currentBrain.model_context && currentBrain.model_context[brainModel])
            return currentBrain.model_context[brainModel];
        return currentBrain.max_context || 200000;
    }
    readonly property var contextOptions: {
        const opts = [{ value: "0", label: "Padrão" }];
        if (maxModelContext >= 200000)
            opts.push({ value: "200000", label: "200K" });
        if (maxModelContext >= 400000)
            opts.push({ value: "400000", label: "400K" });
        if (maxModelContext >= 1000000)
            opts.push({ value: "1000000", label: "1M" });
        return opts;
    }
    readonly property var otherAgents: page && page.agents ? page.agents.filter(a => a.id !== agentId) : []

    property bool copyPanelOpen: false
    property var copyTargets: []
    property bool copyingBrain: false
    property bool savingDefaultBrain: false
    property string brainActionMsg: ""
    property string brainActionErr: ""

    readonly property var brainModels: {
        const all = currentBrain ? currentBrain.models || [] : [];
        const q = modelSearch.text.trim().toLowerCase();
        const hits = all.filter(m => q === "" || m.toLowerCase().includes(q) || modelName(m).toLowerCase().includes(q));
        const top = hits.slice(0, 10);
        if (brainModel !== "" && !top.includes(brainModel))
            top.unshift(brainModel);
        return top;
    }

    readonly property string currentProjectId: page && page.agent && page.agent.project_id ? page.agent.project_id : ""
    readonly property bool canArchive: agentName !== "" && confirmNameField.text === agentName

    Component.onCompleted: {
        ready = true;
        syncFields();
        load();
    }

    onAgentIdChanged: {
        if (!ready)
            return;
        confirmArchive = false;
        confirmNameField.text = "";
        localPhotoError = "";
        projectError = "";
        copyPanelOpen = false;
        copyTargets = [];
        brainActionMsg = "";
        brainActionErr = "";
        modelSearch.text = "";
        syncFields();
        load();
    }

    onDraftNameChanged: {
        if (ready && nameField.text !== draftName)
            syncFields();
    }

    onDraftRoleChanged: {
        if (ready && roleField.text !== draftRole)
            syncFields();
    }

    Connections {
        target: tab.page
        ignoreUnknownSignals: true
        function onDraftReset() {
            modelSearch.text = "";
            tab.syncFields();
        }
    }

    Connections {
        target: Api
        function onLiveEvent(e) {
            if (e.type === "project.saved" || e.type === "project.deleted")
                tab.loadProjects();
        }
    }

    function syncFields() {
        syncing = true;
        if (nameField.text !== draftName)
            nameField.text = draftName;
        if (roleField.text !== draftRole)
            roleField.text = draftRole;
        syncing = false;
    }

    function load() {
        loadProjects();
    }

    function loadProjects() {
        loadingProjects = true;
        projectError = "";
        Api.get("/v1/projects", (err, list) => {
            tab.loadingProjects = false;
            if (err) {
                tab.projectError = err;
                return;
            }
            tab.projects = list || [];
        });
    }

    function modelName(m) {
        return currentBrain && currentBrain.model_names && currentBrain.model_names[m] ? currentBrain.model_names[m] : m;
    }

    function isBrainAvail(b) {
        if (!b)
            return false;
        if (b.available !== undefined)
            return !!b.available;
        return !!b.ready;
    }

    function brainReason(b) {
        if (!b)
            return "Indisponível agora.";
        return b.reason || b.note || "Indisponível agora.";
    }

    function toggleCopyTarget(id) {
        const idx = copyTargets.indexOf(id);
        const next = copyTargets.slice();
        if (idx >= 0)
            next.splice(idx, 1);
        else
            next.push(id);
        copyTargets = next;
    }

    function copyBrainToOthers() {
        if (copyingBrain || copyTargets.length === 0 || !page || !page.agent)
            return;
        const doCopy = () => {
            tab.copyingBrain = true;
            tab.brainActionErr = "";
            tab.brainActionMsg = "";
            Api.post("/v1/agents/" + encodeURIComponent(tab.page.agent.id) + "/brain/copy", { to: tab.copyTargets }, (err, res) => {
                tab.copyingBrain = false;
                if (err) {
                    tab.brainActionErr = err;
                    return;
                }
                const n = res && res.copied !== undefined ? res.copied : tab.copyTargets.length;
                tab.brainActionMsg = n === 1 ? "Cérebro copiado para 1 agente." : "Cérebro copiado para " + n + " agentes.";
                tab.copyPanelOpen = false;
                tab.copyTargets = [];
                if (tab.page)
                    tab.page.reload();
            });
        };
        if (page.changes && page.changes.length > 0)
            page.save(doCopy);
        else
            doCopy();
    }

    function saveAsDefaultBrain() {
        if (savingDefaultBrain || !page)
            return;
        savingDefaultBrain = true;
        brainActionErr = "";
        brainActionMsg = "";
        const payload = {
            provider: brainProvider,
            model: brainProvider === "" ? "" : brainModel,
            effort: brainEffort,
            fallback: brainFallback,
            rotate: brainRotate,
            account: brainProvider === "" ? "" : brainAccount,
            context: brainContext,
            speed: brainSpeed
        };
        Api.put("/v1/brain/default", payload, err => {
            tab.savingDefaultBrain = false;
            if (err) {
                tab.brainActionErr = err;
                return;
            }
            tab.brainActionMsg = "Definido como cérebro padrão para agentes novos.";
        });
    }

    function removePhoto() {
        if (removingPhoto || !page || !page.agent)
            return;
        removingPhoto = true;
        localPhotoError = "";
        Api.del("/v1/agents/" + encodeURIComponent(page.agent.id) + "/avatar", err => {
            tab.removingPhoto = false;
            if (err) {
                tab.localPhotoError = err;
                return;
            }
            if (tab.page)
                tab.page.reload();
        });
    }

    function moveToProject(projectId) {
        if (movingProject || !page || !page.agent || currentProjectId === projectId)
            return;
        movingProject = true;
        projectError = "";
        Api.put("/v1/agents/" + encodeURIComponent(page.agent.id) + "/project", { project_id: projectId }, err => {
            tab.movingProject = false;
            if (err) {
                tab.projectError = err;
                return;
            }
            if (tab.page)
                tab.page.reload();
        });
    }

    function triggerArchive() {
        if (!canArchive || !page)
            return;
        page.archive();
    }

    // ── Identidade ──────────────────────────────────────────────
    Column {
        width: parent.width
        spacing: Theme.space3
        Overline { text: "IDENTIDADE" }
        Row {
            width: parent.width
            spacing: Theme.space5

            Column {
                id: photoCol
                spacing: Theme.space2
                Avatar {
                    size: 72
                    photo: U.photo(tab.page ? tab.page.agent : null)
                    tint: tab.draftColor !== "" ? tab.draftColor : Theme.text2
                    name: tab.draftName
                    opacity: tab.uploading ? 0.5 : 1
                    Behavior on opacity { NumberAnimation { duration: Theme.fast } }
                    Rectangle {
                        anchors.fill: parent
                        anchors.margins: parent.size * 0.07
                        radius: width / 2
                        color: Theme.scrim
                        opacity: avatarArea.containsMouse && !tab.uploading ? 1 : 0
                        Behavior on opacity { NumberAnimation { duration: Theme.fast } }
                        Icon {
                            anchors.centerIn: parent
                            name: "image"
                            size: 18
                            color: Theme.white
                        }
                    }
                    MouseArea {
                        id: avatarArea
                        anchors.fill: parent
                        hoverEnabled: true
                        cursorShape: tab.uploading ? Qt.ArrowCursor : Qt.PointingHandCursor
                        onClicked: {
                            if (!tab.uploading && tab.page) {
                                tab.localPhotoError = "";
                                tab.page.pickPhoto();
                            }
                        }
                    }
                }
                GhostButton {
                    small: true
                    icon: "image"
                    text: tab.uploading ? "Enviando…" : "Trocar foto"
                    enabled: !tab.uploading && !tab.removingPhoto
                    onClicked: {
                        if (tab.page) {
                            tab.localPhotoError = "";
                            tab.page.pickPhoto();
                        }
                    }
                }
                GhostButton {
                    visible: tab.hasAvatar && !tab.uploading
                    small: true
                    text: tab.removingPhoto ? "Removendo…" : "Remover foto"
                    enabled: !tab.removingPhoto
                    onClicked: tab.removePhoto()
                }
            }

            Column {
                width: parent.width - photoCol.width - Theme.space5
                spacing: Theme.space3
                Column {
                    width: parent.width
                    spacing: Theme.space1
                    Text {
                        text: "Nome"
                        color: Theme.text3
                        font.pixelSize: Theme.fontSmall
                    }
                    Field {
                        id: nameField
                        width: parent.width
                        placeholderText: "Ex.: Lia"
                        maximumLength: 40
                        onTextEdited: {
                            if (!tab.syncing && tab.page)
                                tab.page.set("name", text);
                        }
                    }
                }
                Column {
                    width: parent.width
                    spacing: Theme.space1
                    Text {
                        text: "Cargo"
                        color: Theme.text3
                        font.pixelSize: Theme.fontSmall
                    }
                    Field {
                        id: roleField
                        width: parent.width
                        placeholderText: "Ex.: Tutora de estudos"
                        maximumLength: 60
                        onTextEdited: {
                            if (!tab.syncing && tab.page)
                                tab.page.set("role", text);
                        }
                    }
                }
            }
        }
        ErrorBanner {
            width: parent.width
            text: tab.photoErr
        }
    }

    // ── Cor ─────────────────────────────────────────────────────
    Column {
        width: parent.width
        spacing: Theme.space3
        Overline { text: "COR" }
        Flow {
            width: parent.width
            spacing: Theme.space3
            Repeater {
                model: Theme.palette
                delegate: Rectangle {
                    id: sw
                    required property string modelData
                    readonly property bool on: tab.draftColor.toLowerCase() === modelData.toLowerCase()
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
                        onClicked: {
                            if (tab.page)
                                tab.page.set("color", sw.modelData);
                        }
                    }
                }
            }
        }
    }

    // ── Cérebro ─────────────────────────────────────────────────
    Column {
        width: parent.width
        spacing: Theme.space3
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
            spacing: Theme.space2
            Pill {
                text: "Automático"
                on: tab.brainProvider === ""
                onHit: {
                    if (!tab.page)
                        return;
                    if (tab.brainProvider !== "") {
                        tab.page.set("brain.provider", "");
                        tab.page.set("brain.model", "");
                        tab.page.set("brain.account", "");
                        modelSearch.text = "";
                    } else if (tab.brainModel !== "") {
                        tab.page.set("brain.model", "");
                    }
                }
            }
            Repeater {
                model: tab.brains
                delegate: Pill {
                    required property var modelData
                    text: modelData.name
                    on: tab.brainProvider === modelData.provider
                    dim: !tab.isBrainAvail(modelData)
                    onHit: {
                        if (tab.page && tab.brainProvider !== modelData.provider) {
                            tab.page.set("brain.provider", modelData.provider);
                            tab.page.set("brain.model", "");
                            tab.page.set("brain.account", "");
                            modelSearch.text = "";
                        }
                    }
                }
            }
            Pill {
                text: "+ Conectar outra IA"
                dim: true
                onHit: if (tab.page) tab.page.pageRequested("modelos:connect")
            }
        }
        Text {
            visible: (!!tab.currentBrain && !tab.isBrainAvail(tab.currentBrain)) || (tab.brains.length > 0 && tab.brainProvider !== "" && !tab.currentBrain)
            width: parent.width
            wrapMode: Text.Wrap
            text: "Agora: " + tab.brainReason(tab.currentBrain) + " "
                + (tab.brainFallback === "none" ? "Ele vai avisar e parar até voltar." : "Enquanto isso, responde o Automático.")
            color: Theme.warn
            font.pixelSize: Theme.fontSmall
        }

        // Account selection & rotation
        Column {
            visible: !!tab.currentBrain && tab.brainAccounts.length > 0
            width: parent.width
            spacing: Theme.space2
            Text {
                text: "Conta"
                color: Theme.text2
                font.pixelSize: Theme.fontBody
            }
            Flow {
                width: parent.width
                spacing: Theme.space2
                Pill {
                    text: "Qualquer conta pronta"
                    on: tab.brainAccount === ""
                    onHit: if (tab.page) tab.page.set("brain.account", "")
                }
                Repeater {
                    model: tab.brainAccounts
                    delegate: Pill {
                        required property var modelData
                        text: modelData.name
                        on: tab.brainAccount === modelData.id
                        dim: !modelData.available
                        onHit: if (tab.page) tab.page.set("brain.account", modelData.id)
                    }
                }
            }
            Row {
                spacing: Theme.space4
                Text {
                    text: "Quando chegar no limite"
                    color: Theme.text2
                    font.pixelSize: Theme.fontBody
                    anchors.verticalCenter: parent.verticalCenter
                }
                Segmented {
                    value: tab.brainRotate ? "rotate" : "single"
                    options: [
                        { value: "rotate", label: "Passar para outra conta" },
                        { value: "single", label: "Só esta conta" }
                    ]
                    onActivated: v => {
                        if (tab.page)
                            tab.page.set("brain.rotate", v === "rotate");
                    }
                }
            }
        }

        Column {
            visible: !!tab.currentBrain && (tab.currentBrain.models || []).length > 0
            width: parent.width
            spacing: Theme.space2
            Text {
                text: "Modelo"
                color: Theme.text2
                font.pixelSize: Theme.fontBody
            }
            Field {
                id: modelSearch
                visible: !!tab.currentBrain && (tab.currentBrain.models || []).length > 10
                width: parent.width
                placeholderText: "Buscar entre " + (tab.currentBrain ? (tab.currentBrain.models || []).length : 0) + " modelos"
            }
            Flow {
                width: parent.width
                spacing: Theme.space2
                Pill {
                    text: "Padrão"
                    on: tab.brainModel === ""
                    onHit: {
                        if (tab.page)
                            tab.page.set("brain.model", "");
                    }
                }
                Repeater {
                    model: tab.brainModels
                    delegate: Pill {
                        required property string modelData
                        text: tab.modelName(modelData)
                        on: tab.brainModel === modelData
                        onHit: {
                            if (tab.page)
                                tab.page.set("brain.model", modelData);
                        }
                    }
                }
            }
        }

        Row {
            visible: tab.brainProvider === "" || (!!tab.currentBrain && (tab.currentBrain.efforts || []).length > 0)
            spacing: Theme.space4
            Text {
                text: "Esforço"
                color: Theme.text2
                font.pixelSize: Theme.fontBody
                anchors.verticalCenter: parent.verticalCenter
            }
            Segmented {
                value: tab.brainEffort
                options: [{ value: "", label: "Padrão" }, { value: "low", label: "Baixo" }, { value: "medium", label: "Médio" },
                          { value: "high", label: "Alto" }, { value: "max", label: "Máximo" }]
                onActivated: v => {
                    if (tab.page)
                        tab.page.set("brain.effort", v);
                }
            }
        }

        Row {
            visible: tab.contextOptions.length > 1
            spacing: Theme.space4
            Text {
                text: "Janela de contexto"
                color: Theme.text2
                font.pixelSize: Theme.fontBody
                anchors.verticalCenter: parent.verticalCenter
            }
            Segmented {
                value: String(tab.brainContext > tab.maxModelContext ? tab.maxModelContext : tab.brainContext)
                options: tab.contextOptions
                onActivated: v => {
                    if (tab.page)
                        tab.page.set("brain.context", parseInt(v, 10) || 0);
                }
            }
        }

        Row {
            visible: tab.supportsSpeed
            spacing: Theme.space4
            Text {
                text: "Velocidade"
                color: Theme.text2
                font.pixelSize: Theme.fontBody
                anchors.verticalCenter: parent.verticalCenter
            }
            Segmented {
                value: tab.brainSpeed === "fast" ? "fast" : ""
                options: [
                    { value: "", label: "Padrão" },
                    { value: "fast", label: "Rápida" }
                ]
                onActivated: v => {
                    if (tab.page)
                        tab.page.set("brain.speed", v);
                }
            }
        }

        Row {
            visible: tab.brainProvider !== ""
            spacing: Theme.space4
            Text {
                text: "Se a conta falhar"
                color: Theme.text2
                font.pixelSize: Theme.fontBody
                anchors.verticalCenter: parent.verticalCenter
            }
            Segmented {
                value: tab.brainFallback
                options: [
                    { value: "", label: "Cair no Automático" },
                    { value: "none", label: "Parar e avisar" }
                ]
                onActivated: v => {
                    if (tab.page)
                        tab.page.set("brain.fallback", v);
                }
            }
        }

        Text {
            visible: tab.brainProvider === "" && tab.brainEffort !== ""
            width: parent.width
            wrapMode: Text.Wrap
            text: "No Automático, o esforço vale quando ele cai no Codex ou no Antigravity."
            color: Theme.text4
            font.pixelSize: Theme.fontSmall
        }

        // Reuse brain across agents or set as default for new agents
        Flow {
            width: parent.width
            spacing: Theme.space2
            GhostButton {
                visible: tab.otherAgents.length > 0
                small: true
                icon: "copy"
                text: "Usar em outros agentes"
                onClicked: {
                    tab.copyPanelOpen = !tab.copyPanelOpen;
                    tab.brainActionMsg = "";
                    tab.brainActionErr = "";
                }
            }
            GhostButton {
                small: true
                icon: "spark"
                text: tab.savingDefaultBrain ? "Salvando…" : "Padrão para agentes novos"
                enabled: !tab.savingDefaultBrain
                onClicked: tab.saveAsDefaultBrain()
            }
        }

        Rectangle {
            visible: tab.copyPanelOpen && tab.otherAgents.length > 0
            width: parent.width
            height: copyCol.implicitHeight + Theme.space4 * 2
            radius: Theme.radiusMd
            color: Theme.wash1
            border.width: 1
            border.color: Theme.line

            Column {
                id: copyCol
                x: Theme.space4
                y: Theme.space4
                width: parent.width - Theme.space4 * 2
                spacing: Theme.space3
                Text {
                    width: parent.width
                    text: "Escolha os agentes que vão receber esta configuração de cérebro:"
                    color: Theme.text2
                    font.pixelSize: Theme.fontSmall
                    wrapMode: Text.WordWrap
                }
                Flow {
                    width: parent.width
                    spacing: Theme.space2
                    Repeater {
                        model: tab.otherAgents
                        delegate: Pill {
                            required property var modelData
                            text: modelData.name
                            on: tab.copyTargets.indexOf(modelData.id) >= 0
                            onHit: tab.toggleCopyTarget(modelData.id)
                        }
                    }
                }
                Row {
                    spacing: Theme.space2
                    PrimaryButton {
                        text: tab.copyingBrain ? "Copiando…" : "Aplicar nos selecionados"
                        busy: tab.copyingBrain
                        enabled: !tab.copyingBrain && tab.copyTargets.length > 0
                        onClicked: tab.copyBrainToOthers()
                    }
                    GhostButton {
                        text: "Cancelar"
                        onClicked: {
                            tab.copyPanelOpen = false;
                            tab.copyTargets = [];
                        }
                    }
                }
            }
        }

        Text {
            visible: tab.brainActionMsg !== ""
            width: parent.width
            wrapMode: Text.Wrap
            text: tab.brainActionMsg
            color: Theme.ok
            font.pixelSize: Theme.fontSmall
        }

        ErrorBanner {
            width: parent.width
            text: tab.brainActionErr
        }

        Text {
            width: parent.width
            wrapMode: Text.Wrap
            text: "Vale a partir da próxima resposta."
            color: Theme.text4
            font.pixelSize: Theme.fontSmall
        }
    }

    // ── Onde trabalha ───────────────────────────────────────────
    Column {
        width: parent.width
        spacing: Theme.space3
        Overline { text: "ONDE TRABALHA" }
        AgentHomeCard { page: tab.page; width: parent.width }
    }

    // ── Projeto ─────────────────────────────────────────────────
    Column {
        width: parent.width
        spacing: Theme.space3
        Overline { text: "PROJETO" }
        ErrorBanner {
            width: parent.width
            text: tab.projectError
        }
        Flow {
            width: parent.width
            spacing: Theme.space2
            Pill {
                text: "Universal"
                on: tab.currentProjectId === ""
                enabled: !tab.movingProject
                onHit: tab.moveToProject("")
            }
            Repeater {
                model: tab.projects
                delegate: Pill {
                    required property var modelData
                    text: modelData.name
                    on: tab.currentProjectId === modelData.id
                    enabled: !tab.movingProject
                    onHit: tab.moveToProject(modelData.id)
                }
            }
        }
        Text {
            visible: tab.loadingProjects && tab.projects.length === 0 && tab.projectError === ""
            width: parent.width
            wrapMode: Text.Wrap
            text: "Carregando projetos…"
            color: Theme.text4
            font.pixelSize: Theme.fontSmall
        }
        Text {
            width: parent.width
            wrapMode: Text.Wrap
            text: "Universal vê todos os agentes e projetos. Num projeto, ele só vê o time e os arquivos de lá."
            color: Theme.text4
            font.pixelSize: Theme.fontSmall
        }
    }

    // ── Zona de perigo ──────────────────────────────────────────
    Column {
        width: parent.width
        spacing: Theme.space3
        Overline { text: "ZONA DE PERIGO" }
        Rectangle {
            width: parent.width
            height: dangerCol.implicitHeight + Theme.space4 * 2
            radius: Theme.radiusMd
            color: Theme.badWash
            border.width: 1
            border.color: Theme.badLine

            Column {
                id: dangerCol
                x: Theme.space4
                y: Theme.space4
                width: parent.width - Theme.space4 * 2
                spacing: Theme.space3

                Column {
                    width: parent.width
                    spacing: Theme.space1
                    Text {
                        width: parent.width
                        text: "Arquivar funcionário"
                        color: Theme.white
                        font.pixelSize: Theme.fontLarge
                        font.weight: Font.DemiBold
                        wrapMode: Text.Wrap
                    }
                    Text {
                        width: parent.width
                        text: "Ele some da lista e para as rotinas. As conversas e os arquivos ficam guardados e dá para trazer de volta."
                        color: Theme.text2
                        font.pixelSize: Theme.fontSmall
                        wrapMode: Text.Wrap
                    }
                }

                GhostButton {
                    visible: !tab.confirmArchive
                    danger: true
                    text: "Arquivar"
                    onClicked: {
                        tab.confirmArchive = true;
                        confirmNameField.text = "";
                        confirmNameField.forceActiveFocus();
                    }
                }

                Column {
                    visible: tab.confirmArchive
                    width: parent.width
                    spacing: Theme.space2
                    Text {
                        width: parent.width
                        text: "Digite \"" + tab.agentName + "\" para confirmar o arquivamento:"
                        color: Theme.text2
                        font.pixelSize: Theme.fontSmall
                        wrapMode: Text.Wrap
                    }
                    Field {
                        id: confirmNameField
                        width: parent.width
                        placeholderText: tab.agentName
                        maximumLength: 40
                        Keys.onReturnPressed: if (tab.canArchive) tab.triggerArchive()
                        Keys.onEnterPressed: if (tab.canArchive) tab.triggerArchive()
                        Keys.onEscapePressed: {
                            tab.confirmArchive = false;
                            confirmNameField.text = "";
                        }
                    }
                    Row {
                        spacing: Theme.space2
                        GhostButton {
                            danger: true
                            text: "Confirmar arquivamento"
                            enabled: tab.canArchive
                            onClicked: tab.triggerArchive()
                        }
                        GhostButton {
                            text: "Cancelar"
                            onClicked: {
                                tab.confirmArchive = false;
                                confirmNameField.text = "";
                            }
                        }
                    }
                }
            }
        }
    }
}
