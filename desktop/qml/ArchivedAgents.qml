import QtQuick
import Avora
import "util.js" as U

// The archived agents, each one a click away from coming back.
Sheet {
    id: sh
    property var agents: [] // GET /v1/agents/archived
    property string error: ""
    signal restored(string id)

    function open() {
        error = "";
        shown = true;
    }
    function restore(a) {
        Api.post("/v1/agents/" + a.id + "/archive", { archived: false }, err => {
            if (err) {
                sh.error = err;
                return;
            }
            sh.restored(a.id);
            if (sh.agents.length <= 1)
                sh.shown = false;
        });
    }

    Column {
        spacing: Theme.space1
        Text {
            text: "Arquivados"
            color: Theme.white
            font.family: Theme.display
            font.pixelSize: Theme.fontHeading
            font.weight: Font.DemiBold
        }
        Text {
            text: "Fora do time e sem rotinas. As conversas e os arquivos continuam guardados."
            color: Theme.text3
            font.pixelSize: Theme.fontBody
        }
    }

    Text {
        visible: sh.agents.length === 0
        text: "Nenhum funcionário arquivado."
        color: Theme.text4
        font.pixelSize: Theme.fontBody
    }

    Repeater {
        model: sh.agents
        delegate: Rectangle {
            id: row
            required property var modelData
            width: sh.form.width
            height: 60
            radius: Theme.radiusMd
            color: Theme.wash1
            border.width: 1
            border.color: Theme.line
            Avatar {
                id: face
                anchors { left: parent.left; leftMargin: Theme.space3; verticalCenter: parent.verticalCenter }
                size: 36
                name: row.modelData.name
                photo: U.photo(row.modelData)
                tint: row.modelData.color || Theme.text2
            }
            Column {
                anchors { left: face.right; leftMargin: Theme.space3; right: back.left; rightMargin: Theme.space3; verticalCenter: parent.verticalCenter }
                spacing: 2
                Text {
                    width: parent.width
                    text: row.modelData.name + (row.modelData.role ? "  ·  " + row.modelData.role : "")
                    color: Theme.text
                    font.pixelSize: Theme.fontBody
                    elide: Text.ElideRight
                }
                Text {
                    text: "Arquivado " + U.ago(row.modelData.archived_at)
                    color: Theme.text4
                    font.pixelSize: Theme.fontSmall
                }
            }
            GhostButton {
                id: back
                anchors { right: parent.right; rightMargin: Theme.space3; verticalCenter: parent.verticalCenter }
                small: true
                icon: "restore"
                text: "Trazer de volta"
                onClicked: sh.restore(row.modelData)
            }
        }
    }

    ErrorBanner {
        width: sh.form.width
        text: sh.error
    }
}
