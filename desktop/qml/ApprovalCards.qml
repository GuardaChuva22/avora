import QtQuick
import Avora
import "util.js" as U

// Locked actions waiting for the owner, above the composer: what the agent
// wants to do, in words, and Permitir / Sempre permitir / Negar. The agent
// waits; without an answer the action expires and is not done.
Column {
    id: box
    property var items: []   // pending approvals (GET /v1/approvals?status=pending)
    property var agents: []
    signal decided

    visible: items.length > 0
    spacing: 8

    property var sending: ({}) // approval id -> true while posting
    property string error: ""

    function decide(ap, decision) {
        const s = Object.assign({}, sending);
        s[ap.id] = true;
        sending = s;
        error = "";
        Api.post("/v1/approvals/" + ap.id, { decision: decision }, err => {
            const t = Object.assign({}, box.sending);
            delete t[ap.id];
            box.sending = t;
            if (err && err.indexOf("not in a valid state") < 0) // already answered or expired: nothing to say
                box.error = err;
            box.decided();
        });
    }

    // Minutes left, refreshed by the clock below.
    property real now: Date.now()
    Timer {
        interval: 15000
        repeat: true
        running: box.visible
        onTriggered: box.now = Date.now()
    }
    function expiresText(ap) {
        const m = Math.ceil((new Date(ap.expires_at).getTime() - box.now) / 60000);
        return m <= 1 ? "expira em menos de 1 min" : "expira em " + m + " min";
    }

    Repeater {
        model: box.items
        delegate: Rectangle {
            id: card
            required property var modelData
            readonly property var who: box.agents.find(a => a.id === modelData.agent_id) || null
            readonly property color tint: who && who.color ? who.color : Theme.warn
            readonly property bool busy: !!box.sending[modelData.id]
            width: box.width
            height: col.implicitHeight + 26
            radius: Theme.radiusLg
            color: Qt.rgba(0.95, 0.8, 0.51, 0.045)
            border.width: 1
            border.color: Qt.rgba(0.95, 0.8, 0.51, 0.28)

            Column {
                id: col
                x: 14
                y: 13
                width: parent.width - 28
                spacing: 10
                Row {
                    spacing: 8
                    Icon { name: "lock"; size: 15; weight: 1.6; color: Theme.warn; anchors.verticalCenter: parent.verticalCenter }
                    Text {
                        anchors.verticalCenter: parent.verticalCenter
                        textFormat: Text.StyledText
                        text: "<b>" + U.esc(card.who ? card.who.name : card.modelData.agent_id) + "</b> precisa da sua permissão" +
                              "  <font color='" + Theme.text4 + "'>" + box.expiresText(card.modelData) + "</font>"
                        color: Theme.text
                        font.pixelSize: Theme.fontBody
                    }
                }
                Text {
                    width: col.width
                    text: card.modelData.summary
                    color: Theme.text2
                    font.pixelSize: Theme.fontBody
                    lineHeight: 1.2
                    wrapMode: Text.Wrap
                    maximumLineCount: 6
                    elide: Text.ElideRight
                }
                Row {
                    spacing: 8
                    enabled: !card.busy
                    GhostButton {
                        small: true
                        icon: "check"
                        text: "Permitir"
                        onClicked: box.decide(card.modelData, "once")
                    }
                    GhostButton {
                        small: true
                        text: "Sempre permitir"
                        onClicked: box.decide(card.modelData, "always")
                    }
                    GhostButton {
                        small: true
                        danger: true
                        icon: "close"
                        text: "Negar"
                        onClicked: box.decide(card.modelData, "deny")
                    }
                }
            }
        }
    }
    ErrorBanner {
        width: box.width
        text: box.error
    }
}
