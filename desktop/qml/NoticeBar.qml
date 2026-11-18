import QtQuick
import Avora
import "util.js" as U

// Single-line workspace notice strips shown at the top of the work area only
// when attention is needed: no AI connected, offline home computer, or paused agents.
Item {
    id: root
    property var notices: []

    visible: notices.length > 0
    implicitHeight: notices.length > 0 ? col.implicitHeight : 0
    height: implicitHeight

    function joinNames(names) {
        if (!names || names.length === 0)
            return "";
        if (names.length === 1)
            return names[0];
        if (names.length === 2)
            return names[0] + " e " + names[1];
        return names.slice(0, -1).join(", ") + " e " + names[names.length - 1];
    }

    function applyNotices(list) {
        const clean = Array.isArray(list) ? list : [];
        if (!U.same(root.notices, clean))
            root.notices = clean;
    }

    // Fallback when talking to an older server without GET /v1/notices.
    function refreshFallback() {
        Api.get("/v1/nodes", (nErr, nodesData) => {
            if (nErr || !Api.signedIn)
                return;
            Api.get("/v1/agents", (aErr, agentsData) => {
                if (aErr || !Api.signedIn)
                    return;
                Api.get("/v1/models", (mErr, modelsData) => {
                    if (mErr || !Api.signedIn)
                        return;
                    Api.get("/v1/quotas", (qErr, quotasData) => {
                        if (!Api.signedIn)
                            return;
                        const nodes = Array.isArray(nodesData) ? nodesData : [];
                        const agents = Array.isArray(agentsData) ? agentsData : [];
                        const providers = modelsData && Array.isArray(modelsData.providers) ? modelsData.providers : [];
                        const quotas = !qErr && Array.isArray(quotasData) ? quotasData : [];

                        const hasProv = providers.some(p => p.id === "antigravity" ? !!p.codex_main : p.status === "ok");
                        const hasQuota = quotas.some(q => !q.error);
                        const hasMain = !!(modelsData && modelsData.main && modelsData.main.provider);
                        const out = [];

                        if (!hasProv && !hasQuota && !hasMain)
                            out.push({ kind: "no_ai", text: "Nenhuma IA conectada", action: "modelos:connect" });

                        const byHome = {};
                        const paused = [];
                        for (let i = 0; i < agents.length; i++) {
                            const a = agents[i];
                            const nm = a.name || a.id;
                            if (a.home_node_id) {
                                if (!byHome[a.home_node_id])
                                    byHome[a.home_node_id] = [];
                                byHome[a.home_node_id].push(nm);
                            }
                            if (a.autonomy && a.autonomy.can_start_alone === false)
                                paused.push(nm);
                        }

                        for (let i = 0; i < nodes.length; i++) {
                            const n = nodes[i];
                            if (n.revoked_at || n.online)
                                continue;
                            const waiting = byHome[n.id] || [];
                            if (waiting.length === 0)
                                continue;
                            const label = "O computador " + (n.name || n.id) + " está desligado: " + joinNames(waiting)
                                + (waiting.length === 1 ? " espera por ele" : " esperam por ele");
                            out.push({ kind: "node_offline", text: label, action: "nos" });
                        }

                        if (paused.length === 1)
                            out.push({ kind: "agents_paused", text: paused[0] + " está pausado e não começa tarefas sozinho", action: "team" });
                        else if (paused.length > 1)
                            out.push({ kind: "agents_paused", text: joinNames(paused) + " estão pausados e não começam tarefas sozinhos", action: "team" });

                        root.applyNotices(out);
                    });
                });
            });
        });
    }

    function refresh() {
        if (!Api.signedIn) {
            applyNotices([]);
            return;
        }
        Api.get("/v1/notices", (err, data, status) => {
            if (!Api.signedIn)
                return;
            if (status === 404) {
                root.refreshFallback();
                return;
            }
            if (err)
                return;
            root.applyNotices(data);
        });
    }

    function iconFor(kind) {
        if (kind === "no_ai")
            return "alert";
        if (kind === "node_offline")
            return "monitor";
        return "clock";
    }

    function buttonLabelFor(item) {
        if (!item)
            return "Abrir";
        if (item.kind === "no_ai" || item.action === "modelos:connect")
            return "Conectar";
        if (item.kind === "node_offline" || item.action === "nos")
            return "Ver nós";
        if (item.kind === "agents_paused" || item.action === "team")
            return "Ver equipe";
        return "Abrir";
    }

    function runAction(act) {
        if (!act || typeof win === "undefined" || !win)
            return;
        if (act === "modelos:connect") {
            if ("section" in win)
                win.section = "modelos";
            if ("configSheet" in win)
                win.configSheet = "connect";
            if (win.sections && win.sections.indexOf("modelos") >= 0 && typeof win.go === "function")
                win.go("modelos:connect");
            else if (typeof win.go === "function")
                win.go("config");
            return;
        }
        if (typeof win.go === "function")
            win.go(act);
    }

    Component.onCompleted: {
        if (Api.signedIn)
            refresh();
    }

    Timer {
        interval: 30000
        running: Api.signedIn
        repeat: true
        onTriggered: root.refresh()
    }

    Timer {
        id: liveDebounce
        interval: 300
        onTriggered: root.refresh()
    }

    Connections {
        target: Api
        function onSignedInChanged() {
            if (Api.signedIn)
                root.refresh();
            else
                root.applyNotices([]);
        }
        function onWorkspaceChanged() {
            if (Api.signedIn)
                root.refresh();
        }
        function onLiveEvent(e) {
            const t = (e && e.type) ? e.type : "";
            if (t.startsWith("node.") || t.startsWith("agent.") || t.startsWith("brain.") || t.startsWith("connection."))
                liveDebounce.restart();
        }
        function onLiveReset() {
            liveDebounce.restart();
        }
    }

    Column {
        id: col
        width: parent.width
        spacing: 0

        Repeater {
            model: root.notices
            delegate: Rectangle {
                id: strip
                required property var modelData
                required property int index

                readonly property bool isBad: modelData.kind === "no_ai"
                readonly property color tone: isBad ? Theme.bad : Theme.warn
                readonly property bool nearWinButtons: index === 0 && root.y < Theme.titleBar

                width: col.width
                height: 36
                color: isBad ? Theme.badWash : Qt.rgba(Theme.warn.r, Theme.warn.g, Theme.warn.b, 0.08)

                Rectangle {
                    anchors {
                        left: parent.left
                        right: parent.right
                        bottom: parent.bottom
                    }
                    height: 1
                    color: strip.isBad ? Theme.badLine : Qt.rgba(Theme.warn.r, Theme.warn.g, Theme.warn.b, 0.20)
                }

                Icon {
                    id: stripIcon
                    x: Theme.space4
                    anchors.verticalCenter: parent.verticalCenter
                    name: root.iconFor(strip.modelData.kind)
                    size: 15
                    color: strip.tone
                }

                Text {
                    anchors {
                        left: stripIcon.right
                        leftMargin: Theme.space2
                        right: actionBtn.left
                        rightMargin: Theme.space3
                        verticalCenter: parent.verticalCenter
                    }
                    text: strip.modelData.text || ""
                    color: Theme.text
                    font.pixelSize: Theme.fontSmall
                    maximumLineCount: 1
                    elide: Text.ElideRight
                }

                Rectangle {
                    id: actionBtn
                    visible: !!(strip.modelData.action && strip.modelData.action !== "")
                    anchors {
                        right: parent.right
                        rightMargin: strip.nearWinButtons ? 148 : Theme.space4
                        verticalCenter: parent.verticalCenter
                    }
                    height: 26
                    width: btnLabel.implicitWidth + 2 * Theme.space3
                    radius: height / 2
                    color: btnArea.pressed ? Theme.active : btnArea.containsMouse ? Theme.hover : Theme.wash1
                    border.width: 1
                    border.color: btnArea.containsMouse ? Theme.lineStrong : Theme.line
                    Behavior on color { ColorAnimation { duration: Theme.fast } }
                    Behavior on border.color { ColorAnimation { duration: Theme.fast } }

                    Text {
                        id: btnLabel
                        anchors.centerIn: parent
                        text: root.buttonLabelFor(strip.modelData)
                        color: btnArea.containsMouse ? Theme.white : Theme.text
                        font.pixelSize: Theme.fontSmall
                        font.weight: Font.DemiBold
                    }

                    MouseArea {
                        id: btnArea
                        anchors.fill: parent
                        hoverEnabled: true
                        cursorShape: Qt.PointingHandCursor
                        onClicked: root.runAction(strip.modelData.action)
                    }
                }
            }
        }
    }
}
