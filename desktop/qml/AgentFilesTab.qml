import QtQuick
import QtQuick.Controls.Basic
import Avora
import "util.js" as U

// Documents this agent created or edited, searchable and grouped by day.
Column {
    id: tab
    required property var page   // the AgentPage that hosts this tab
    spacing: Theme.space5
    // width is set by the page; do not set it here

    property var docs: []
    property bool loaded: false
    property bool ready: false
    property string error: ""
    property string query: ""
    property string filterMode: "all"

    readonly property string agentId: page && page.agent ? page.agent.id || "" : ""
    readonly property string actorKey: "agent:" + agentId
    readonly property var teamAgents: page && page.agents ? page.agents : []

    readonly property var filtered: {
        const want = DocBus.fold(query).trim();
        const list = docs.filter(d => {
            if (filterMode === "created" && d.author !== actorKey)
                return false;
            if (filterMode === "edited" && !(d.editor === actorKey && d.author !== actorKey))
                return false;
            if (want !== "" && DocBus.fold(d.title || "").indexOf(want) < 0)
                return false;
            return true;
        });
        return list.slice().sort((a, b) => (new Date(b.updated_at).getTime() || 0) - (new Date(a.updated_at).getTime() || 0));
    }

    readonly property var dayGroups: {
        const out = [];
        for (let i = 0; i < filtered.length; i++) {
            const d = filtered[i];
            const k = dayKey(d.updated_at);
            if (out.length === 0 || out[out.length - 1].key !== k)
                out.push({ key: k, label: dayLabel(d.updated_at), items: [d] });
            else
                out[out.length - 1].items.push(d);
        }
        return out;
    }

    Component.onCompleted: {
        ready = true;
        load();
    }

    onAgentIdChanged: {
        if (ready)
            load();
    }

    Connections {
        target: DocBus
        function onChanged() {
            tab.load();
        }
    }

    function load() {
        const id = tab.agentId;
        if (id === "") {
            tab.docs = [];
            tab.loaded = true;
            return;
        }
        Api.get("/v1/docs?actor=agent:" + encodeURIComponent(id) + "&limit=100", (err, data) => {
            if (tab.agentId !== id)
                return;
            tab.loaded = true;
            if (err) {
                tab.error = err;
                return;
            }
            tab.error = "";
            tab.docs = data || [];
        });
    }

    function dayKey(iso) {
        if (!iso)
            return "";
        const d = new Date(iso);
        return d.getFullYear() + "-" + U.pad(d.getMonth() + 1) + "-" + U.pad(d.getDate());
    }

    function dayLabel(iso) {
        if (!iso)
            return "Recentes";
        const d = new Date(iso);
        const now = new Date();
        const startOf = dt => new Date(dt.getFullYear(), dt.getMonth(), dt.getDate()).getTime();
        const diff = Math.round((startOf(now) - startOf(d)) / 86400000);
        if (diff === 0)
            return "Hoje";
        if (diff === 1)
            return "Ontem";
        return d.getFullYear() === now.getFullYear() ? U.day(iso) : U.day(iso) + " " + d.getFullYear();
    }

    function togglePin(d) {
        Api.patch("/v1/docs/" + d.id, { pinned: !d.pinned }, err => {
            if (err)
                tab.error = err;
            else
                tab.load();
        });
    }

    function trashDoc(d) {
        Api.del("/v1/docs/" + d.id, err => {
            if (err) {
                tab.error = err;
                return;
            }
            if (DocBus.openId === d.id)
                DocBus.openId = "";
            tab.load();
        });
    }

    // ── Search & author/editor filter ───────────────────────────
    Item {
        id: topBar
        readonly property bool narrow: width < modeSeg.implicitWidth + 260
        width: parent.width
        height: narrow ? searchField.implicitHeight + Theme.space2 + modeSeg.implicitHeight : searchField.implicitHeight

        Field {
            id: searchField
            anchors {
                left: parent.left
                right: topBar.narrow ? parent.right : modeSeg.left
                rightMargin: topBar.narrow ? 0 : Theme.space3
            }
            leftPadding: Theme.space6 + Theme.space1
            placeholderText: "Buscar pelo título…"
            text: tab.query
            onTextEdited: tab.query = text
            Keys.onEscapePressed: {
                text = "";
                tab.query = "";
            }

            Icon {
                x: Theme.space3
                anchors.verticalCenter: parent.verticalCenter
                name: "search"
                size: 15
                color: Theme.text4
            }
        }

        Segmented {
            id: modeSeg
            x: topBar.narrow ? 0 : parent.width - width
            y: topBar.narrow ? searchField.implicitHeight + Theme.space2 : Math.round((searchField.implicitHeight - height) / 2)
            value: tab.filterMode
            options: [
                { value: "all", label: "Todos" },
                { value: "created", label: "Criados por ele" },
                { value: "edited", label: "Editados por ele" }
            ]
            onActivated: v => tab.filterMode = v
        }
    }

    ErrorBanner {
        width: parent.width
        text: tab.error
    }

    // ── Loading state ───────────────────────────────────────────
    Rectangle {
        visible: !tab.loaded && tab.error === ""
        width: parent.width
        height: loadingText.implicitHeight + Theme.space5 * 2
        radius: Theme.radiusMd
        color: Theme.wash1
        border.width: 1
        border.color: Theme.line

        Text {
            id: loadingText
            anchors.centerIn: parent
            text: "Carregando documentos…"
            color: Theme.text3
            font.pixelSize: Theme.fontBody
        }
    }

    // ── Empty states (no docs yet, or no filter match) ──────────
    Rectangle {
        visible: tab.loaded && tab.filtered.length === 0
        width: parent.width
        height: emptyCol.implicitHeight + Theme.space6 * 2
        radius: Theme.radiusMd
        color: Theme.wash1
        border.width: 1
        border.color: Theme.line

        Column {
            id: emptyCol
            anchors.centerIn: parent
            width: Math.max(0, parent.width - Theme.space6 * 2)
            spacing: Theme.space2

            Icon {
                anchors.horizontalCenter: parent.horizontalCenter
                name: tab.docs.length > 0 && tab.query.trim() !== "" ? "search" : "docs"
                size: 24
                color: Theme.text4
            }

            Text {
                width: parent.width
                horizontalAlignment: Text.AlignHCenter
                wrapMode: Text.Wrap
                text: tab.docs.length === 0
                      ? "Ele ainda não criou nenhum doc. Quando escrever um relatório, plano ou planilha, aparece aqui."
                      : tab.query.trim() !== ""
                      ? "Nenhum doc com “" + tab.query.trim() + "”."
                      : tab.filterMode === "created"
                      ? "Nenhum doc criado por ele ainda."
                      : "Nenhum doc de outro autor editado por ele ainda."
                color: Theme.text3
                font.pixelSize: Theme.fontBody
            }
        }
    }

    // ── Grouped document list ───────────────────────────────────
    Column {
        visible: tab.dayGroups.length > 0
        width: parent.width
        spacing: Theme.space4

        Repeater {
            model: tab.dayGroups
            delegate: Column {
                id: groupBox
                required property var modelData
                width: parent.width
                spacing: Theme.space2

                Overline { text: groupBox.modelData.label }

                Rectangle {
                    width: parent.width
                    height: rowsCol.implicitHeight + Theme.space2 * 2
                    radius: Theme.radiusMd
                    color: Theme.wash1
                    border.width: 1
                    border.color: Theme.line

                    Column {
                        id: rowsCol
                        x: Theme.space2
                        y: Theme.space2
                        width: parent.width - Theme.space2 * 2
                        spacing: 1

                        Repeater {
                            model: groupBox.modelData.items
                            delegate: DocRow {
                                required property var modelData
                                width: rowsCol.width
                                doc: modelData
                                agents: tab.teamAgents
                                selected: DocBus.openId === modelData.id
                                onOpened: if (tab.page) tab.page.openDoc(modelData.id)
                                onPin: tab.togglePin(modelData)
                                onRemove: tab.trashDoc(modelData)
                            }
                        }
                    }
                }
            }
        }
    }
}
