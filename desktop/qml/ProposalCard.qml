import QtQuick
import Avora

// Card shown in chat or on an agent's profile for a proposed change to
// instructions, role or apps, with a compact diff and Aplicar / Recusar / Desfazer.
Rectangle {
    id: card
    property var proposal: null
    property string proposalId: ""
    property var agents: []
    property bool expanded: false
    property bool busy: false
    property string error: ""
    signal updated(var proposal)

    readonly property var p: proposal
    readonly property string effectiveId: p && p.id ? p.id : proposalId

    visible: p !== null || loading
    property bool loading: false

    implicitHeight: body.implicitHeight + 2 * Theme.space4
    radius: Theme.radiusLg
    color: Theme.card
    border.width: 1
    border.color: p && p.status === "pending" ? Theme.lineStrong : Theme.line

    function loadById() {
        if (proposalId === "" || proposal !== null)
            return;
        loading = true;
        Api.get("/v1/proposals/" + encodeURIComponent(proposalId), (err, data) => {
            card.loading = false;
            if (err) {
                card.error = err;
                return;
            }
            card.proposal = data;
        });
    }

    onProposalIdChanged: loadById()
    Component.onCompleted: loadById()

    Connections {
        target: Api
        function onLiveEvent(ev) {
            if (!ev || ev.type !== "proposal.updated" || card.effectiveId === "")
                return;
            if (ev.payload && ev.payload.id && ev.payload.id !== card.effectiveId)
                return;
            Api.get("/v1/proposals/" + encodeURIComponent(card.effectiveId), (err, data) => {
                if (!err && data)
                    card.proposal = data;
            });
        }
    }

    function agentName(id) {
        if (!id)
            return "";
        const found = (agents || []).find(a => a.id === id);
        return found ? found.name : id;
    }

    function headlineText() {
        if (!p)
            return "Carregando proposta…";
        if (p.headline)
            return p.headline;
        const from = p.from_name || agentName(p.from_agent) || "Avora";
        const target = p.target_name || agentName(p.target_agent) || "funcionário";
        if (p.field === "role")
            return from + " propõe mudar o cargo de " + target;
        if (p.field === "apps")
            return from + " propõe mudar os apps de " + target;
        return from + " propõe mudar as instruções de " + target;
    }

    function diffLines() {
        if (!p)
            return [];
        const beforeLines = (p.before || "").split("\n").filter((l, idx, arr) => !(arr.length === 1 && l === ""));
        const afterLines = (p.after || "").split("\n").filter((l, idx, arr) => !(arr.length === 1 && l === ""));
        const beforeSet = {};
        const afterSet = {};
        for (let i = 0; i < beforeLines.length; i++)
            beforeSet[beforeLines[i]] = (beforeSet[beforeLines[i]] || 0) + 1;
        for (let i = 0; i < afterLines.length; i++)
            afterSet[afterLines[i]] = (afterSet[afterLines[i]] || 0) + 1;

        const out = [];
        for (let i = 0; i < beforeLines.length; i++) {
            const line = beforeLines[i];
            if (!afterSet[line]) {
                out.push({ sign: "-", text: line, kind: "del" });
            } else {
                afterSet[line]--;
            }
        }
        for (let i = 0; i < afterLines.length; i++) {
            const line = afterLines[i];
            if (!beforeSet[line]) {
                out.push({ sign: "+", text: line, kind: "add" });
            } else {
                beforeSet[line]--;
            }
        }
        if (out.length === 0 && (p.before || "") !== (p.after || "")) {
            if (p.before)
                out.push({ sign: "-", text: p.before, kind: "del" });
            if (p.after)
                out.push({ sign: "+", text: p.after, kind: "add" });
        }
        return out;
    }

    readonly property var allDiff: diffLines()
    readonly property int collapsedMax: 6
    readonly property var visibleDiff: expanded ? allDiff : allDiff.slice(0, collapsedMax)

    function act(action) {
        if (!p || busy)
            return;
        busy = true;
        error = "";
        Api.post("/v1/proposals/" + encodeURIComponent(p.id) + "/" + action, {}, (err, data) => {
            card.busy = false;
            if (err) {
                card.error = err;
                return;
            }
            if (data)
                card.proposal = data;
            card.updated(card.proposal);
        });
    }

    Column {
        id: body
        x: Theme.space4
        y: Theme.space4
        width: parent.width - 2 * Theme.space4
        spacing: Theme.space3

        // Header: icon, headline, status badge.
        Item {
            width: parent.width
            height: Math.max(titleRow.implicitHeight, statusPill.height)

            Row {
                id: titleRow
                width: parent.width - (statusPill.visible ? statusPill.width + Theme.space2 : 0)
                spacing: Theme.space2

                Icon {
                    anchors.verticalCenter: parent.verticalCenter
                    name: "spark"
                    size: 16
                    color: card.p && card.p.status === "applied" ? Theme.ok : Theme.white
                }
                Text {
                    width: parent.width - 24
                    anchors.verticalCenter: parent.verticalCenter
                    text: card.headlineText()
                    color: Theme.white
                    font.pixelSize: Theme.fontLarge
                    font.weight: Font.DemiBold
                    wrapMode: Text.Wrap
                }
            }

            Rectangle {
                id: statusPill
                anchors { right: parent.right; verticalCenter: parent.verticalCenter }
                visible: !!card.p && card.p.status !== "pending"
                height: 22
                width: statusTxt.implicitWidth + 2 * Theme.space2
                radius: height / 2
                color: Theme.wash3
                border.width: 1
                border.color: Theme.line
                Text {
                    id: statusTxt
                    anchors.centerIn: parent
                    text: !card.p ? "" :
                          card.p.status === "applied" ? ("Aplicada" + (card.p.applied_version ? " · v" + card.p.applied_version : "")) :
                          card.p.status === "undone" ? "Desfeita" :
                          card.p.status === "rejected" ? "Recusada" : card.p.status
                    color: card.p && card.p.status === "applied" ? Theme.ok :
                           card.p && card.p.status === "undone" ? Theme.warn : Theme.text3
                    font.pixelSize: Theme.fontTiny
                    font.weight: Font.DemiBold
                }
            }
        }

        // Reason.
        Text {
            visible: !!card.p && !!card.p.reason
            width: parent.width
            text: card.p ? card.p.reason : ""
            color: Theme.text2
            font.pixelSize: Theme.fontBody
            lineHeight: 1.25
            wrapMode: Text.Wrap
        }

        // Short diff well.
        Rectangle {
            visible: card.visibleDiff.length > 0
            width: parent.width
            height: diffCol.implicitHeight + 2 * Theme.space3
            radius: Theme.radiusMd
            color: Theme.well
            border.width: 1
            border.color: Theme.line

            Column {
                id: diffCol
                x: Theme.space3
                y: Theme.space3
                width: parent.width - 2 * Theme.space3
                spacing: Theme.space1

                Repeater {
                    model: card.visibleDiff
                    delegate: Text {
                        required property var modelData
                        width: diffCol.width
                        text: modelData.sign + " " + (modelData.text !== "" ? modelData.text : "(vazio)")
                        color: modelData.kind === "add" ? Theme.ok : modelData.kind === "del" ? Theme.bad : Theme.text3
                        font.family: Theme.mono
                        font.pixelSize: Theme.fontSmall
                        wrapMode: Text.Wrap
                    }
                }

                Text {
                    visible: card.allDiff.length > card.collapsedMax
                    text: card.expanded
                        ? "mostrar resumo"
                        : "ver mais (" + (card.allDiff.length - card.collapsedMax) + " linhas)"
                    color: expandArea.containsMouse ? Theme.white : Theme.text3
                    font.pixelSize: Theme.fontCaption
                    font.underline: expandArea.containsMouse
                    MouseArea {
                        id: expandArea
                        anchors.fill: parent
                        anchors.margins: -Theme.space1
                        hoverEnabled: true
                        cursorShape: Qt.PointingHandCursor
                        onClicked: card.expanded = !card.expanded
                    }
                }
            }
        }

        // Actions: Aplicar / Recusar when pending; Desfazer when applied.
        Row {
            visible: !!card.p && (card.p.status === "pending" || card.p.status === "applied")
            spacing: Theme.space2
            enabled: !card.busy

            GhostButton {
                visible: !!card.p && card.p.status === "pending"
                small: true
                icon: "check"
                text: "Aplicar"
                onClicked: card.act("apply")
            }
            GhostButton {
                visible: !!card.p && card.p.status === "pending"
                small: true
                danger: true
                icon: "close"
                text: "Recusar"
                onClicked: card.act("reject")
            }
            GhostButton {
                visible: !!card.p && card.p.status === "applied"
                small: true
                icon: "history"
                text: "Desfazer"
                onClicked: card.act("undo")
            }
        }

        ErrorBanner {
            width: parent.width
            text: card.error
        }
    }
}
