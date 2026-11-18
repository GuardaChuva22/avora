import QtQuick
import QtQuick.Controls.Basic
import Avora
import "util.js" as U

// Agent instructions tab: persona, speaking style, responsibilities, routines and version history.
Column {
    id: tab
    required property var page
    spacing: Theme.space5

    property bool ready: false
    property bool syncing: false

    property var routines: []
    property bool loadingRoutines: false
    property string routinesError: ""

    property var versions: []
    property bool loadingVersions: false
    property string versionsError: ""
    property bool restoring: false
    property int confirmRestore: -1
    property int expandedVersion: -1

    readonly property string agentId: page && page.agent ? page.agent.id : ""
    readonly property int agentVersion: page && page.agent ? page.agent.version || 0 : 0
    readonly property string draftPersona: page && page.draft && page.draft.persona !== undefined ? page.draft.persona : ""
    readonly property var draftStyle: page && page.draft && page.draft.style ? page.draft.style : ({})
    readonly property string draftVoice: draftStyle.voice !== undefined ? draftStyle.voice : ""
    readonly property var draftResps: page && page.draft && page.draft.responsibilities ? page.draft.responsibilities : []

    Component.onCompleted: {
        ready = true;
        syncFields();
        load();
    }

    onAgentIdChanged: {
        if (!ready)
            return;
        confirmRestore = -1;
        expandedVersion = -1;
        routines = [];
        versions = [];
        syncFields();
        load();
    }

    onAgentVersionChanged: {
        if (ready)
            loadVersions();
    }

    onDraftPersonaChanged: {
        if (ready && personaField.text !== draftPersona)
            syncFields();
    }

    onDraftVoiceChanged: {
        if (ready && voiceField.text !== draftVoice)
            syncFields();
    }

    Connections {
        target: tab.page
        ignoreUnknownSignals: true
        function onDraftReset() {
            tab.syncFields();
        }
    }

    Connections {
        target: Api
        function onLiveEvent(e) {
            if (e.type === "routine.saved" || e.type === "routine.deleted" || e.type === "routine.run")
                tab.loadRoutines();
            else if (e.type === "agent.saved")
                tab.loadVersions();
        }
    }

    // Only write into text controls when the draft changed from outside, avoiding cursor resets and loops.
    function syncFields() {
        syncing = true;
        if (personaField.text !== draftPersona)
            personaField.text = draftPersona;
        if (voiceField.text !== draftVoice)
            voiceField.text = draftVoice;
        syncing = false;
    }

    function load() {
        loadRoutines();
        loadVersions();
    }

    function loadRoutines() {
        if (!page || !page.agent)
            return;
        const id = page.agent.id;
        loadingRoutines = true;
        routinesError = "";
        Api.get("/v1/routines?agent=" + encodeURIComponent(id), (err, list) => {
            if (!tab.page || !tab.page.agent || tab.page.agent.id !== id)
                return;
            tab.loadingRoutines = false;
            if (err) {
                tab.routinesError = err;
                return;
            }
            tab.routines = list || [];
        });
    }

    function loadVersions() {
        if (!page || !page.agent)
            return;
        const id = page.agent.id;
        loadingVersions = true;
        versionsError = "";
        Api.get("/v1/agents/" + encodeURIComponent(id) + "/versions", (err, list) => {
            if (!tab.page || !tab.page.agent || tab.page.agent.id !== id)
                return;
            tab.loadingVersions = false;
            if (err) {
                tab.versionsError = err;
                return;
            }
            tab.versions = list || [];
        });
    }

    function formatChars(n) {
        const num = String(n).replace(/\B(?=(\d{3})+(?!\d))/g, ".");
        return num + (n === 1 ? " caractere" : " caracteres");
    }

    function copyResponsibility(r) {
        return {
            id: r && r.id ? r.id : "",
            goal: r && r.goal ? r.goal : "",
            metric: r && r.metric ? r.metric : "",
            cadence: r && r.cadence ? r.cadence : ""
        };
    }

    function updateResponsibility(idx, key, val) {
        if (!page)
            return;
        const next = draftResps.map((r, i) => {
            const copy = copyResponsibility(r);
            if (i === idx)
                copy[key] = val;
            return copy;
        });
        page.set("responsibilities", next);
    }

    function addResponsibility() {
        if (!page || draftResps.length >= 10)
            return;
        const next = draftResps.map(copyResponsibility).concat([{ id: "", goal: "", metric: "", cadence: "" }]);
        page.set("responsibilities", next);
    }

    function removeResponsibility(idx) {
        if (!page)
            return;
        const next = draftResps.filter((_, i) => i !== idx).map(copyResponsibility);
        page.set("responsibilities", next);
    }

    function versionAuthor(v) {
        const raw = v.actor || v.created_by || v.author || "";
        if (!raw)
            return "";
        if (raw === "admin" || raw === "dono")
            return "você";
        if (raw === "system")
            return "sistema";
        if (raw.startsWith("agent:")) {
            const id = raw.slice(6);
            const list = page && page.agents ? page.agents : [];
            const found = list.find(a => a.id === id);
            return found ? found.name : id;
        }
        return raw;
    }

    function restoreVersion(v) {
        if (restoring || !page || !page.agent)
            return;
        const id = page.agent.id;
        restoring = true;
        versionsError = "";
        Api.post("/v1/agents/" + encodeURIComponent(id) + "/versions/" + v + "/restore", null, err => {
            tab.restoring = false;
            if (err) {
                tab.versionsError = err;
                return;
            }
            tab.confirmRestore = -1;
            if (tab.page)
                tab.page.reload();
            tab.loadVersions();
        });
    }

    // ── Persona e funções ───────────────────────────────────────
    Column {
        width: parent.width
        spacing: Theme.space2
        Overline { text: "PERSONA E FUNÇÕES" }
        AreaField {
            id: personaField
            width: parent.width
            minHeight: 220
            maxHeight: 520
            enterSubmits: false
            placeholderText: "Como esse funcionário trabalha, do que cuida e o que nunca faz."
            onTextChanged: {
                if (!tab.syncing && tab.page && tab.draftPersona !== text)
                    tab.page.set("persona", text);
            }
        }
        Item {
            width: parent.width
            implicitHeight: Math.max(personaHint.implicitHeight, personaCount.implicitHeight)
            height: implicitHeight
            Text {
                id: personaHint
                anchors { left: parent.left; right: personaCount.left; rightMargin: Theme.space3 }
                text: "Diga como ele trabalha, do que cuida e o que nunca faz."
                color: Theme.text4
                font.pixelSize: Theme.fontSmall
                wrapMode: Text.Wrap
            }
            Text {
                id: personaCount
                anchors { right: parent.right; top: parent.top }
                text: tab.formatChars(personaField.text.length)
                color: Theme.text4
                font.pixelSize: Theme.fontSmall
            }
        }
    }

    // ── Jeito de falar ──────────────────────────────────────────
    Column {
        width: parent.width
        spacing: Theme.space3
        Overline { text: "JEITO DE FALAR" }
        Field {
            id: voiceField
            width: parent.width
            maximumLength: 400
            placeholderText: "Ex.: descontraída e direta, usa gíria de SP, zoa o time de leve"
            onTextEdited: {
                if (!tab.syncing && tab.page)
                    tab.page.set("style.voice", text);
            }
        }
        Grid {
            width: parent.width
            columns: 2
            columnSpacing: Theme.space4
            rowSpacing: Theme.space2
            verticalItemAlignment: Grid.AlignVCenter
            Text { text: "No #time"; color: Theme.text2; font.pixelSize: Theme.fontBody }
            Segmented {
                value: tab.draftStyle.talk || ""
                options: [{ value: "pouco", label: "Fala pouco" }, { value: "", label: "Normal" }, { value: "muito", label: "Participa muito" }]
                onActivated: v => { if (tab.page) tab.page.set("style.talk", v); }
            }
            Text { text: "Emoji"; color: Theme.text2; font.pixelSize: Theme.fontBody }
            Segmented {
                value: tab.draftStyle.emoji || ""
                options: [{ value: "nunca", label: "Nunca" }, { value: "", label: "Às vezes" }, { value: "bastante", label: "Bastante" }]
                onActivated: v => { if (tab.page) tab.page.set("style.emoji", v); }
            }
            Text { text: "Respostas"; color: Theme.text2; font.pixelSize: Theme.fontBody }
            Segmented {
                value: tab.draftStyle.length || ""
                options: [{ value: "curta", label: "Curtas" }, { value: "", label: "Normais" }, { value: "longa", label: "Detalhadas" }]
                onActivated: v => { if (tab.page) tab.page.set("style.length", v); }
            }
        }
    }

    // ── Responsabilidades e rotinas ─────────────────────────────
    Column {
        width: parent.width
        spacing: Theme.space3
        Overline { text: "RESPONSABILIDADES" }
        Text {
            visible: tab.draftResps.length === 0
            width: parent.width
            wrapMode: Text.Wrap
            text: "Metas permanentes, com métrica e cadência. Ex.: gastos do mês abaixo do orçamento, checar todo dia às 8h. Uma rotina pode cuidar de cada uma."
            color: Theme.text4
            font.pixelSize: Theme.fontSmall
        }
        // Bind Repeater to count so replacing the draft array on each keystroke keeps focus in the active Field.
        Repeater {
            model: tab.draftResps.length
            delegate: Rectangle {
                id: rr
                required property int index
                readonly property var item: tab.draftResps[index] || ({ id: "", goal: "", metric: "", cadence: "" })
                width: tab.width
                height: rrCol.implicitHeight + Theme.space3 * 2
                radius: Theme.radiusMd
                color: Theme.wash1
                border.width: 1
                border.color: Theme.line

                Component.onCompleted: syncRow()
                onItemChanged: syncRow()

                function syncRow() {
                    const g = item.goal || "";
                    if (goalField.text !== g)
                        goalField.text = g;
                    const m = item.metric || "";
                    if (metricField.text !== m)
                        metricField.text = m;
                    const c = item.cadence || "";
                    if (cadenceField.text !== c)
                        cadenceField.text = c;
                }

                Column {
                    id: rrCol
                    x: Theme.space3
                    y: Theme.space3
                    width: parent.width - Theme.space6 - Theme.space5
                    spacing: Theme.space2
                    Field {
                        id: goalField
                        width: parent.width
                        placeholderText: "Objetivo. Ex.: gastos do mês abaixo do orçamento"
                        maximumLength: 200
                        onTextEdited: tab.updateResponsibility(rr.index, "goal", text)
                    }
                    Row {
                        width: parent.width
                        spacing: Theme.space2
                        Field {
                            id: metricField
                            width: (parent.width - Theme.space2) * 0.58
                            placeholderText: "Métrica. Ex.: gasto / orçamento"
                            maximumLength: 200
                            onTextEdited: tab.updateResponsibility(rr.index, "metric", text)
                        }
                        Field {
                            id: cadenceField
                            width: (parent.width - Theme.space2) * 0.42
                            placeholderText: "Cadência. Ex.: todo dia 8h"
                            maximumLength: 100
                            onTextEdited: tab.updateResponsibility(rr.index, "cadence", text)
                        }
                    }
                }
                GhostButton {
                    anchors { right: parent.right; rightMargin: Theme.space2; top: parent.top; topMargin: Theme.space3 }
                    round: true
                    small: true
                    icon: "close"
                    onClicked: tab.removeResponsibility(rr.index)
                }
            }
        }
        GhostButton {
            visible: tab.draftResps.length < 10
            small: true
            icon: "plus"
            text: "Responsabilidade"
            onClicked: tab.addResponsibility()
        }

        ErrorBanner {
            width: parent.width
            text: tab.routinesError
        }
        Text {
            visible: tab.loadingRoutines && tab.routines.length === 0 && tab.routinesError === ""
            width: parent.width
            wrapMode: Text.Wrap
            text: "Carregando rotinas…"
            color: Theme.text4
            font.pixelSize: Theme.fontSmall
        }
        AgentRoutines {
            width: parent.width
            agent: tab.page ? tab.page.agent : null
            routines: tab.routines
            onEdit: r => { if (tab.page) tab.page.editRoutine(r); }
            onCreate: preset => { if (tab.page) tab.page.newRoutine(preset); }
            onChanged: tab.loadRoutines()
        }
    }

    // ── Histórico de versões ────────────────────────────────────
    Column {
        width: parent.width
        spacing: Theme.space2
        Overline { text: "HISTÓRICO DE VERSÕES" }
        ErrorBanner {
            width: parent.width
            text: tab.versionsError
        }
        Text {
            visible: tab.loadingVersions && tab.versions.length === 0 && tab.versionsError === ""
            width: parent.width
            wrapMode: Text.Wrap
            text: "Carregando histórico de versões…"
            color: Theme.text4
            font.pixelSize: Theme.fontSmall
        }
        Text {
            visible: !tab.loadingVersions && tab.versions.length === 0 && tab.versionsError === ""
            width: parent.width
            wrapMode: Text.Wrap
            text: "Nenhuma versão registrada ainda. Cada vez que você salvar alterações, uma nova versão aparece aqui."
            color: Theme.text4
            font.pixelSize: Theme.fontSmall
        }
        Repeater {
            model: tab.versions
            delegate: Rectangle {
                id: ver
                required property var modelData
                readonly property bool current: tab.agentVersion > 0 && modelData.version === tab.agentVersion
                readonly property bool confirming: !current && tab.confirmRestore === modelData.version
                readonly property bool expanded: tab.expandedVersion === modelData.version
                readonly property string author: tab.versionAuthor(modelData)
                readonly property var snapshot: modelData.agent || null
                readonly property int nextVersion: (tab.agentVersion > 0 ? tab.agentVersion : (tab.versions.length > 0 ? tab.versions[0].version : 0)) + 1

                width: tab.width
                height: verCol.implicitHeight + Theme.space3 * 2
                radius: Theme.radiusMd
                color: verHeadArea.containsMouse && !confirming ? Theme.hover : Theme.wash1
                border.width: 1
                border.color: confirming ? Theme.lineStrong : Theme.line
                Behavior on color { ColorAnimation { duration: Theme.fast } }

                Column {
                    id: verCol
                    x: Theme.space3
                    y: Theme.space3
                    width: parent.width - Theme.space3 * 2
                    spacing: Theme.space2

                    Item {
                        width: parent.width
                        implicitHeight: Math.max(30, verMeta.implicitHeight, verRight.implicitHeight)
                        height: implicitHeight

                        MouseArea {
                            id: verHeadArea
                            anchors.fill: parent
                            hoverEnabled: true
                            cursorShape: Qt.PointingHandCursor
                            onClicked: tab.expandedVersion = ver.expanded ? -1 : ver.modelData.version
                        }

                        Row {
                            id: verMeta
                            anchors { left: parent.left; verticalCenter: parent.verticalCenter }
                            spacing: Theme.space2
                            Icon {
                                anchors.verticalCenter: parent.verticalCenter
                                name: "chevron"
                                size: 12
                                color: Theme.text4
                                rotation: ver.expanded ? 90 : 0
                                Behavior on rotation { NumberAnimation { duration: Theme.fast } }
                            }
                            Text {
                                anchors.verticalCenter: parent.verticalCenter
                                text: "v" + ver.modelData.version
                                color: ver.current ? Theme.white : Theme.text
                                font.family: Theme.mono
                                font.pixelSize: Theme.fontBody
                                font.weight: Font.DemiBold
                            }
                            Text {
                                anchors.verticalCenter: parent.verticalCenter
                                text: U.ago(ver.modelData.created_at) + (ver.author !== "" ? "  ·  " + ver.author : "")
                                color: Theme.text3
                                font.pixelSize: Theme.fontSmall
                            }
                            Rectangle {
                                visible: ver.current
                                anchors.verticalCenter: parent.verticalCenter
                                height: 20
                                width: curLabel.implicitWidth + Theme.space3
                                radius: height / 2
                                color: Theme.wash4
                                border.width: 1
                                border.color: Theme.line
                                Text {
                                    id: curLabel
                                    anchors.centerIn: parent
                                    text: "atual"
                                    color: Theme.white
                                    font.pixelSize: Theme.fontCaption
                                    font.weight: Font.Medium
                                }
                            }
                        }

                        Row {
                            id: verRight
                            anchors { right: parent.right; verticalCenter: parent.verticalCenter }
                            spacing: Theme.space2
                            GhostButton {
                                visible: !ver.current && !ver.confirming
                                small: true
                                text: "Restaurar"
                                enabled: !tab.restoring
                                onClicked: tab.confirmRestore = ver.modelData.version
                            }
                        }
                    }

                    // Inline confirmation on the same row card when restoring an older version.
                    Flow {
                        visible: ver.confirming
                        width: parent.width
                        spacing: Theme.space2
                        Text {
                            height: 30
                            verticalAlignment: Text.AlignVCenter
                            text: "Restaurar a v" + ver.modelData.version + "? Isso cria a v" + ver.nextVersion + "."
                            color: Theme.text2
                            font.pixelSize: Theme.fontSmall
                        }
                        GhostButton {
                            small: true
                            icon: "check"
                            text: tab.restoring ? "Restaurando…" : "Confirmar"
                            enabled: !tab.restoring
                            onClicked: tab.restoreVersion(ver.modelData.version)
                        }
                        GhostButton {
                            small: true
                            text: "Cancelar"
                            enabled: !tab.restoring
                            onClicked: tab.confirmRestore = -1
                        }
                    }

                    // Expanded snapshot persona for quick comparison.
                    Column {
                        visible: ver.expanded && ver.snapshot !== null
                        width: parent.width
                        spacing: Theme.space2
                        Rectangle {
                            width: parent.width
                            height: 1
                            color: Theme.line
                        }
                        Text {
                            width: parent.width
                            text: ver.snapshot && ver.snapshot.persona ? ver.snapshot.persona : "Sem persona definida nesta versão."
                            color: Theme.text3
                            font.pixelSize: Theme.fontBody
                            lineHeight: 1.3
                            maximumLineCount: 12
                            elide: Text.ElideRight
                            wrapMode: Text.Wrap
                        }
                    }
                }
            }
        }
    }
}
