import QtQuick
import Avora

// What the owner said "Sempre permitir" to, for the profile panel. Each
// permission can be taken back; the agent then asks again.
Column {
    id: box
    property var agent: null
    property var grants: []

    spacing: 6
    visible: grants.length > 0

    function load() {
        const id = agent ? agent.id : "";
        if (id === "") {
            grants = [];
            return;
        }
        Api.get("/v1/grants?agent=" + id, (err, data) => {
            if (!err && box.agent && box.agent.id === id)
                box.grants = data;
        });
    }
    onAgentChanged: load()

    Overline { text: "SEMPRE PERMITIDO" }
    Repeater {
        model: box.grants
        delegate: Item {
            id: g
            required property var modelData
            width: box.width
            height: Math.max(30, label.implicitHeight)
            Text {
                id: label
                anchors { left: parent.left; right: revoke.left; rightMargin: 8; verticalCenter: parent.verticalCenter }
                text: g.modelData.label
                color: Theme.text2
                font.pixelSize: Theme.fontBody
                wrapMode: Text.Wrap
            }
            GhostButton {
                id: revoke
                anchors { right: parent.right; verticalCenter: parent.verticalCenter }
                small: true
                danger: true
                text: "Revogar"
                onClicked: Api.del("/v1/grants/" + g.modelData.id, () => box.load())
            }
        }
    }
}
