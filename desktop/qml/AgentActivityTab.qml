import QtQuick
import QtQuick.Controls.Basic
import Avora
import "util.js" as U

// History of runs delegated or executed by this agent, grouped by day.
Column {
    id: tab
    required property var page
    spacing: Theme.space5

    property var runs: []
    property bool loading: true
    property string error: ""
    property string filter: "all"
    property int shownCount: 30
    property real tick: Date.now()
    property bool ready: false

    readonly property int pageSize: 30
    readonly property string agentId: page && page.agent ? page.agent.id : ""

    function matchesFilter(r) {
        switch (tab.filter) {
        case "active":
            return !!r.active;
        case "done":
            return r.status === "succeeded" || r.status === "done";
        case "error":
            return r.status === "failed" || r.status === "canceled" || r.status === "timed_out";
        default:
            return true;
        }
    }

    function engineLabel(eng) {
        switch (eng) {
        case "claude":
            return "Claude Code";
        case "codex":
            return "Codex";
        case "antigravity":
            return "Antigravity";
        case "native":
            return "Avora";
        default:
            return eng || "";
        }
    }

    function durationText(r) {
        if (r.active)
            return U.ago(r.started_at || r.created_at);
        if (r.started_at && r.completed_at) {
            const ms = new Date(r.completed_at).getTime() - new Date(r.started_at).getTime();
            return ms > 0 ? U.seconds(ms) : "";
        }
        return "";
    }

    function dayKey(iso) {
        if (!iso)
            return "";
        const d = new Date(iso);
        return d.getFullYear() + "-" + U.pad(d.getMonth() + 1) + "-" + U.pad(d.getDate());
    }

    function dayHeading(iso) {
        if (!iso)
            return "";
        const d = new Date(iso);
        const now = new Date();
        const startOfDay = new Date(d.getFullYear(), d.getMonth(), d.getDate()).getTime();
        const startOfToday = new Date(now.getFullYear(), now.getMonth(), now.getDate()).getTime();
        const diffDays = Math.round((startOfToday - startOfDay) / 86400000);
        if (diffDays === 0)
            return "Hoje";
        if (diffDays === 1)
            return "Ontem";
        return U.day(iso);
    }

    readonly property var filteredRuns: {
        const q = searchField.text.trim().toLowerCase();
        return tab.runs.filter(r => {
            if (!tab.matchesFilter(r))
                return false;
            if (q !== "" && !(r.title || "").toLowerCase().includes(q))
                return false;
            return true;
        });
    }

    readonly property var visibleRuns: tab.filteredRuns.slice(0, tab.shownCount)

    readonly property var dayGroups: {
        void tab.tick;
        const groups = [];
        for (const r of tab.visibleRuns) {
            const key = tab.dayKey(r.created_at);
            if (groups.length === 0 || groups[groups.length - 1].key !== key)
                groups.push({ key: key, label: tab.dayHeading(r.created_at), items: [r] });
            else
                groups[groups.length - 1].items.push(r);
        }
        return groups;
    }

    Component.onCompleted: {
        ready = true;
        load();
    }

    onAgentIdChanged: {
        if (!ready)
            return;
        runs = [];
        loading = true;
        error = "";
        filter = "all";
        searchField.text = "";
        shownCount = pageSize;
        load();
    }

    function load() {
        const id = agentId;
        if (id === "")
            return;
        Api.get("/v1/runs?agent=" + id, (err, list) => {
            if (tab.agentId !== id)
                return;
            tab.loading = false;
            if (err) {
                tab.error = err;
                return;
            }
            tab.error = "";
            const next = list || [];
            if (!U.same(tab.runs, next))
                tab.runs = next;
        });
    }

    // Poll every 5 s while at least one run is still active.
    Timer {
        interval: 5000
        repeat: true
        running: tab.visible && tab.runs.some(r => r.active)
        onTriggered: {
            tab.tick = Date.now();
            tab.load();
        }
    }

    Connections {
        target: Api
        function onLiveEvent(e) {
            const t = e && e.type ? e.type : "";
            if (t.startsWith("job."))
                liveReload.restart();
        }
    }

    Timer {
        id: liveReload
        interval: 400
        onTriggered: {
            tab.tick = Date.now();
            tab.load();
        }
    }

    ErrorBanner {
        width: parent.width
        text: tab.error
    }

    // ── Filters and count ───────────────────────────────────────
    Column {
        width: parent.width
        spacing: Theme.space3

        Flow {
            width: parent.width
            spacing: Theme.space3

            Segmented {
                value: tab.filter
                options: [
                    { value: "all", label: "Todas" },
                    { value: "active", label: "Em andamento" },
                    { value: "done", label: "Concluídas" },
                    { value: "error", label: "Com erro" }
                ]
                onActivated: v => {
                    tab.filter = v;
                    tab.shownCount = tab.pageSize;
                }
            }

            Field {
                id: searchField
                width: 220
                height: 38
                font.pixelSize: Theme.fontBody
                placeholderText: "Buscar pelo título…"
                onTextEdited: tab.shownCount = tab.pageSize
            }
        }

        Text {
            text: tab.loading && tab.runs.length === 0
                  ? "Carregando execuções…"
                  : (tab.filteredRuns.length === 1 ? "1 execução" : tab.filteredRuns.length + " execuções")
            color: Theme.text3
            font.pixelSize: Theme.fontSmall
        }
    }

    // ── Empty states ────────────────────────────────────────────
    Rectangle {
        visible: !tab.loading && tab.filteredRuns.length === 0
        width: parent.width
        height: emptyCol.implicitHeight + Theme.space5 * 2
        radius: Theme.radiusMd
        color: Theme.wash1
        border.width: 1
        border.color: Theme.line

        Column {
            id: emptyCol
            x: Theme.space4
            y: Theme.space5
            width: parent.width - Theme.space4 * 2
            spacing: Theme.space2

            Text {
                width: parent.width
                text: tab.runs.length === 0
                      ? "Ainda não fez nenhuma execução."
                      : "Nenhuma execução encontrada."
                color: Theme.text2
                font.pixelSize: Theme.fontLarge
                font.weight: Font.Medium
                wrapMode: Text.Wrap
            }

            Text {
                width: parent.width
                text: tab.runs.length === 0
                      ? "Quando pesquisar a fundo, cuidar de uma rotina ou pedir ajuda ao Claude Code ou ao Codex, o histórico aparece aqui."
                      : "Tente outro filtro ou apague o texto da busca para ver todo o histórico."
                color: Theme.text4
                font.pixelSize: Theme.fontSmall
                wrapMode: Text.Wrap
            }

            GhostButton {
                visible: tab.runs.length > 0 && (tab.filter !== "all" || searchField.text.trim() !== "")
                small: true
                text: "Limpar filtros"
                onClicked: {
                    tab.filter = "all";
                    searchField.text = "";
                    tab.shownCount = tab.pageSize;
                }
            }
        }
    }

    // ── Day groups ──────────────────────────────────────────────
    Repeater {
        model: tab.dayGroups
        delegate: Column {
            id: dayGroup
            required property var modelData
            width: tab.width
            spacing: Theme.space2

            Overline {
                text: dayGroup.modelData.label
            }

            Rectangle {
                width: parent.width
                height: groupRows.implicitHeight + Theme.space1 * 2
                radius: Theme.radiusMd
                color: Theme.wash1
                border.width: 1
                border.color: Theme.line

                Column {
                    id: groupRows
                    x: Theme.space1
                    y: Theme.space1
                    width: parent.width - Theme.space1 * 2
                    spacing: 0

                    Repeater {
                        model: dayGroup.modelData.items
                        delegate: Rectangle {
                            id: row
                            required property var modelData
                            required property int index
                            width: groupRows.width
                            height: 44
                            radius: Theme.radiusSm
                            color: rowArea.containsMouse ? Theme.hover : "transparent"
                            Behavior on color { ColorAnimation { duration: Theme.fast } }

                            Rectangle {
                                visible: row.index > 0 && !rowArea.containsMouse
                                anchors {
                                    top: parent.top
                                    left: parent.left
                                    right: parent.right
                                    leftMargin: Theme.space3
                                    rightMargin: Theme.space3
                                }
                                height: 1
                                color: Theme.line
                            }

                            StatusPill {
                                id: pill
                                x: Theme.space2
                                anchors.verticalCenter: parent.verticalCenter
                                status: row.modelData.status
                            }

                            Text {
                                anchors {
                                    left: pill.right
                                    leftMargin: Theme.space3
                                    right: metaRow.left
                                    rightMargin: Theme.space3
                                    verticalCenter: parent.verticalCenter
                                }
                                text: row.modelData.title || ""
                                color: Theme.text
                                font.pixelSize: Theme.fontBody
                                font.weight: Font.Medium
                                elide: Text.ElideRight
                            }

                            Row {
                                id: metaRow
                                anchors {
                                    right: parent.right
                                    rightMargin: Theme.space3
                                    verticalCenter: parent.verticalCenter
                                }
                                spacing: Theme.space3

                                Text {
                                    visible: text !== ""
                                    anchors.verticalCenter: parent.verticalCenter
                                    text: tab.engineLabel(row.modelData.engine)
                                    color: Theme.text3
                                    font.pixelSize: Theme.fontSmall
                                }

                                Text {
                                    visible: text !== ""
                                    anchors.verticalCenter: parent.verticalCenter
                                    text: {
                                        tab.tick; // re-read every tick
                                        return tab.durationText(row.modelData);
                                    }
                                    color: row.modelData.active ? Theme.text2 : Theme.text4
                                    font.family: Theme.mono
                                    font.pixelSize: Theme.fontSmall
                                }

                                Text {
                                    anchors.verticalCenter: parent.verticalCenter
                                    text: U.clock(row.modelData.created_at)
                                    color: Theme.text4
                                    font.family: Theme.mono
                                    font.pixelSize: Theme.fontSmall
                                }
                            }

                            MouseArea {
                                id: rowArea
                                anchors.fill: parent
                                hoverEnabled: true
                                cursorShape: Qt.PointingHandCursor
                                onClicked: tab.page.openRun(row.modelData.id)
                            }
                        }
                    }
                }
            }
        }
    }

    GhostButton {
        visible: tab.filteredRuns.length > tab.shownCount
        small: true
        text: "Mostrar mais"
        onClicked: tab.shownCount += tab.pageSize
    }
}
