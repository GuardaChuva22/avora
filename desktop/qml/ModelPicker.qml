import QtQuick
import QtQuick.Controls.Basic
import Avora

// Quick model switcher opened from an agent conversation header.
// Saves only `brain` via PUT /v1/agents/{id} and offers an Undo toast.
Item {
    id: picker
    property var agent: null
    property var brains: []
    property bool loading: false
    property bool saving: false
    property string error: ""

    property bool undoVisible: false
    property string undoAgentId: ""
    property var previousBrain: null

    signal saved(var updatedAgent)

    // Read the sheet's own state, not sheet.visible: a child's `visible` is
    // false while this Item is hidden, so binding to it never let us show.
    visible: sheet.shown || sheet.opacity > 0 || picker.undoVisible
    z: 55

    function copyObj(o) {
        return JSON.parse(JSON.stringify(o || {}));
    }

    function open(targetAgent) {
        if (!targetAgent)
            return;
        agent = targetAgent;
        error = "";
        searchField.text = "";
        sheet.shown = true;
        searchField.forceActiveFocus();
        loadBrains();
    }

    function loadBrains() {
        loading = true;
        Api.get("/v1/brains", (err, list) => {
            picker.loading = false;
            if (err) {
                picker.error = err;
                return;
            }
            picker.brains = list || [];
        });
    }

    function isAvail(b) {
        if (!b)
            return false;
        if (b.available !== undefined)
            return !!b.available;
        return !!b.ready;
    }

    function reasonOf(b) {
        if (!b)
            return "";
        return b.reason || b.note || "Indisponível agora.";
    }

    function modelTag(modelId, label, meta, kind) {
        if (meta) {
            if (meta.output_price >= 10)
                return "Topo de linha";
            if (meta.output_price > 0 && meta.output_price <= 1.5)
                return "Econômico";
        }
        const s = ((modelId || "") + " " + (label || "")).toLowerCase();
        if (s.indexOf("mini") >= 0 || s.indexOf("nano") >= 0 || s.indexOf("lite") >= 0
            || s.indexOf("small") >= 0 || s.indexOf("8b") >= 0 || s.indexOf("7b") >= 0
            || s.indexOf("free") >= 0 || kind === "ollama" || kind === "lmstudio" || kind === "local")
            return "Econômico";
        if (s.indexOf("flash") >= 0 || s.indexOf("haiku") >= 0 || s.indexOf("instant") >= 0
            || s.indexOf("turbo") >= 0 || s.indexOf("fast") >= 0 || s.indexOf("argon") >= 0
            || kind === "groq" || kind === "cerebras")
            return "Rápido";
        if (s.indexOf("opus") >= 0 || s.indexOf("pro") >= 0 || s.indexOf("gpt-5") >= 0
            || s.indexOf("gpt-6") >= 0 || s.indexOf("o3") >= 0 || s.indexOf("o4") >= 0
            || s.indexOf("xhigh") >= 0 || s.indexOf("high") >= 0 || s.indexOf("sonnet") >= 0
            || s.indexOf("grok") >= 0 || s.indexOf("large") >= 0 || s.indexOf("r1") >= 0)
            return "Topo de linha";
        return "Rápido";
    }

    readonly property string query: searchField.text.trim().toLowerCase()
    readonly property string curProvider: agent && agent.brain && agent.brain.provider ? agent.brain.provider : ""
    readonly property string curModel: agent && agent.brain && agent.brain.model ? agent.brain.model : ""

    readonly property var items: {
        const out = [];
        const q = query;
        for (const b of brains || []) {
            const avail = isAvail(b);
            const models = b.models || [];
            if (!avail) {
                const hay = (b.name + " " + reasonOf(b)).toLowerCase();
                if (q === "" || hay.indexOf(q) >= 0) {
                    out.push({
                        key: "off:" + b.provider,
                        provider: b.provider,
                        providerName: b.name,
                        model: "",
                        label: b.name,
                        tag: "",
                        toolsOff: false,
                        available: false,
                        reason: reasonOf(b)
                    });
                }
                continue;
            }
            if (models.length === 0) {
                const hay = b.name.toLowerCase();
                if (q === "" || hay.indexOf(q) >= 0) {
                    out.push({
                        key: b.provider + ":",
                        provider: b.provider,
                        providerName: b.name,
                        model: "",
                        label: "Padrão · " + b.name,
                        tag: modelTag(b.provider, b.name, null, b.kind),
                        toolsOff: false,
                        available: true,
                        reason: ""
                    });
                }
                continue;
            }
            const limit = q === "" ? 12 : 40;
            let count = 0;
            for (const m of models) {
                const mLabel = b.model_names && b.model_names[m] ? b.model_names[m] : m;
                const meta = b.model_meta && b.model_meta[m] ? b.model_meta[m] : null;
                const tag = modelTag(m, mLabel, meta, b.kind);
                const hay = (m + " " + mLabel + " " + b.name + " " + tag).toLowerCase();
                if (q !== "" && hay.indexOf(q) < 0)
                    continue;
                out.push({
                    key: b.provider + ":" + m,
                    provider: b.provider,
                    providerName: b.name,
                    model: m,
                    label: mLabel,
                    tag: tag,
                    toolsOff: !!(meta && meta.tools === false),
                    available: true,
                    reason: ""
                });
                count++;
                if (count >= limit)
                    break;
            }
        }
        return out;
    }

    function applyBrain(provider, model) {
        if (saving || !agent)
            return;
        const prev = copyObj(agent.brain || {});
        const next = copyObj(prev);
        next.provider = provider;
        next.model = provider === "" ? "" : model;
        if (provider !== (prev.provider || ""))
            next.account = "";

        saving = true;
        error = "";
        const targetId = agent.id;
        Api.put("/v1/agents/" + encodeURIComponent(targetId), { brain: next }, (err, updated) => {
            picker.saving = false;
            if (err) {
                picker.error = err;
                return;
            }
            picker.previousBrain = prev;
            picker.undoAgentId = targetId;
            picker.agent = updated;
            sheet.shown = false;
            picker.undoVisible = true;
            undoTimer.restart();
            picker.saved(updated);
        });
    }

    function undo() {
        if (saving || undoAgentId === "" || !previousBrain)
            return;
        saving = true;
        const targetId = undoAgentId;
        const prev = copyObj(previousBrain);
        Api.put("/v1/agents/" + encodeURIComponent(targetId), { brain: prev }, (err, restored) => {
            picker.saving = false;
            if (!err) {
                picker.undoVisible = false;
                picker.previousBrain = null;
                if (picker.agent && picker.agent.id === targetId)
                    picker.agent = restored;
                picker.saved(restored);
            }
        });
    }

    Timer {
        id: undoTimer
        interval: 8000
        onTriggered: picker.undoVisible = false
    }

    Sheet {
        id: sheet
        anchors.fill: parent
        sheetWidth: 560

        Item {
            width: sheet.form.width
            height: 36
            Text {
                anchors { left: parent.left; right: closeBtn.left; rightMargin: Theme.space2; verticalCenter: parent.verticalCenter }
                text: "Mudar o modelo do " + (picker.agent && picker.agent.name ? picker.agent.name : "agente")
                color: Theme.white
                font.family: Theme.display
                font.pixelSize: Theme.fontTitle
                font.weight: Font.DemiBold
                elide: Text.ElideRight
            }
            GhostButton {
                id: closeBtn
                anchors { right: parent.right; verticalCenter: parent.verticalCenter }
                round: true
                small: true
                icon: "close"
                onClicked: sheet.shown = false
            }
        }

        Field {
            id: searchField
            width: sheet.form.width
            placeholderText: "Buscar modelo, conta ou etiqueta (Rápido, Econômico…)"
        }

        ErrorBanner {
            width: sheet.form.width
            text: picker.error
        }

        Column {
            width: sheet.form.width
            spacing: Theme.space2

            // Automático option
            Rectangle {
                width: parent.width
                height: autoCol.implicitHeight + Theme.space3 * 2
                visible: picker.query === "" || "automático automatico padrao".indexOf(picker.query) >= 0
                radius: Theme.radiusMd
                color: picker.curProvider === "" ? Theme.wash3 : autoArea.containsMouse ? Theme.hover : Theme.wash1
                border.width: 1
                border.color: picker.curProvider === "" ? Theme.focusLine : Theme.line
                Behavior on color { ColorAnimation { duration: Theme.fast } }

                Column {
                    id: autoCol
                    x: Theme.space3
                    y: Theme.space3
                    width: parent.width - Theme.space3 * 2
                    spacing: Theme.space1
                    Row {
                        spacing: Theme.space2
                        Icon {
                            name: "spark"
                            size: 15
                            color: Theme.white
                            anchors.verticalCenter: parent.verticalCenter
                        }
                        Text {
                            text: "Automático"
                            color: Theme.white
                            font.pixelSize: Theme.fontBody
                            font.weight: Font.DemiBold
                            anchors.verticalCenter: parent.verticalCenter
                        }
                    }
                    Text {
                        width: parent.width
                        text: "O Avora escolhe a melhor conta pronta para cada resposta."
                        color: Theme.text3
                        font.pixelSize: Theme.fontSmall
                        wrapMode: Text.WordWrap
                    }
                }
                MouseArea {
                    id: autoArea
                    anchors.fill: parent
                    hoverEnabled: true
                    cursorShape: Qt.PointingHandCursor
                    enabled: !picker.saving
                    onClicked: picker.applyBrain("", "")
                }
            }

            Repeater {
                model: picker.items
                delegate: Rectangle {
                    id: itemRow
                    required property var modelData
                    readonly property bool selected: modelData.available
                        && picker.curProvider === modelData.provider
                        && (picker.curModel === modelData.model || (picker.curModel === "" && modelData.model === ""))
                    width: sheet.form.width
                    height: itemCol.implicitHeight + Theme.space3 * 2
                    radius: Theme.radiusMd
                    color: !modelData.available ? Theme.wash1
                        : selected ? Theme.wash3
                        : rowArea.containsMouse ? Theme.hover : Theme.wash1
                    border.width: 1
                    border.color: selected ? Theme.focusLine : Theme.line
                    opacity: modelData.available ? 1 : 0.62
                    Behavior on color { ColorAnimation { duration: Theme.fast } }

                    Column {
                        id: itemCol
                        x: Theme.space3
                        y: Theme.space3
                        width: parent.width - Theme.space3 * 2
                        spacing: Theme.space1

                        Item {
                            width: parent.width
                            height: Math.max(titleLbl.implicitHeight, badgesRow.implicitHeight)
                            Text {
                                id: titleLbl
                                anchors { left: parent.left; right: badgesRow.left; rightMargin: Theme.space2; verticalCenter: parent.verticalCenter }
                                text: itemRow.modelData.label
                                color: itemRow.modelData.available ? Theme.white : Theme.text4
                                font.pixelSize: Theme.fontBody
                                font.weight: Font.DemiBold
                                elide: Text.ElideRight
                            }
                            Row {
                                id: badgesRow
                                anchors { right: parent.right; verticalCenter: parent.verticalCenter }
                                spacing: Theme.space1
                                Rectangle {
                                    visible: itemRow.modelData.toolsOff
                                    width: noToolsTxt.implicitWidth + Theme.space2 * 2
                                    height: noToolsTxt.implicitHeight + Theme.space1
                                    radius: height / 2
                                    color: Theme.wash2
                                    Text {
                                        id: noToolsTxt
                                        anchors.centerIn: parent
                                        text: "Sem ferramentas"
                                        color: Theme.text3
                                        font.pixelSize: Theme.fontTiny
                                    }
                                }
                                Rectangle {
                                    visible: itemRow.modelData.tag !== ""
                                    width: tagTxt.implicitWidth + Theme.space2 * 2
                                    height: tagTxt.implicitHeight + Theme.space1
                                    radius: height / 2
                                    color: Theme.wash4
                                    Text {
                                        id: tagTxt
                                        anchors.centerIn: parent
                                        text: itemRow.modelData.tag
                                        color: itemRow.modelData.tag === "Topo de linha" ? Theme.white
                                            : itemRow.modelData.tag === "Econômico" ? Theme.ok : Theme.text2
                                        font.pixelSize: Theme.fontTiny
                                        font.weight: Font.DemiBold
                                    }
                                }
                            }
                        }

                        Text {
                            width: parent.width
                            text: itemRow.modelData.available ? itemRow.modelData.providerName : itemRow.modelData.reason
                            color: itemRow.modelData.available ? Theme.text3 : Theme.text4
                            font.pixelSize: Theme.fontSmall
                            wrapMode: Text.WordWrap
                        }
                    }

                    MouseArea {
                        id: rowArea
                        anchors.fill: parent
                        hoverEnabled: itemRow.modelData.available
                        enabled: itemRow.modelData.available && !picker.saving
                        cursorShape: itemRow.modelData.available ? Qt.PointingHandCursor : Qt.ArrowCursor
                        onClicked: picker.applyBrain(itemRow.modelData.provider, itemRow.modelData.model)
                    }
                }
            }

            Text {
                visible: !picker.loading && picker.items.length === 0 && picker.query !== ""
                width: parent.width
                text: "Nenhum modelo encontrado para essa busca."
                color: Theme.text4
                font.pixelSize: Theme.fontSmall
            }
        }

        Text {
            width: sheet.form.width
            text: "Vale a partir da próxima resposta."
            color: Theme.text4
            font.pixelSize: Theme.fontSmall
        }
    }

    // Confirmation bar with Undo after switching models.
    Rectangle {
        id: undoToast
        visible: picker.undoVisible && !sheet.shown
        anchors { horizontalCenter: parent.horizontalCenter; bottom: parent.bottom; bottomMargin: Theme.space5 }
        width: toastRow.implicitWidth + Theme.space4 * 2
        height: toastRow.implicitHeight + Theme.space2 * 2
        radius: height / 2
        color: Theme.card
        border.width: 1
        border.color: Theme.lineStrong

        Row {
            id: toastRow
            anchors.centerIn: parent
            spacing: Theme.space3
            Icon {
                name: "check"
                size: 14
                color: Theme.ok
                anchors.verticalCenter: parent.verticalCenter
            }
            Text {
                text: "Vale a partir da próxima resposta"
                color: Theme.text
                font.pixelSize: Theme.fontSmall
                anchors.verticalCenter: parent.verticalCenter
            }
            GhostButton {
                small: true
                text: "Desfazer"
                enabled: !picker.saving
                anchors.verticalCenter: parent.verticalCenter
                onClicked: picker.undo()
            }
        }
    }
}
