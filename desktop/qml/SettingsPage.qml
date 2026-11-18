import QtQuick
import Avora

// Settings: a slim inner menu (Modelos, Atividade, Nós) and the section
// beside it. Sections stay alive once opened, like the main pages.
Item {
    id: page
    property string section: "modelos"
    // sheet opens a sheet of the first section shown ("pick", "add-openrouter").
    property string sheet: ""
    onSheetChanged: deliver()
    onSectionChanged: deliver()
    function deliver() {
        for (var i = 0; i < loaders.count; i++) {
            const l = loaders.itemAt(i);
            if (sheet !== "" && l && l.current && l.item && l.item.openSheet) {
                l.item.openSheet(sheet);
                sheet = "";
            }
        }
    }

    readonly property var sections: [
        { key: "pessoas", label: "Pessoas", icon: "users", url: "MembersPage.qml" },
        { key: "modelos", label: "Contas de IA", icon: "spark", url: "ModelsPage.qml" },
        { key: "heartbeat", label: "Heartbeat", icon: "bolt", url: "HeartbeatPage.qml" },
        { key: "noite", label: "Turno da noite", icon: "history", url: "NightPage.qml" },
        { key: "atividade", label: "Atividade", icon: "pulse", url: "ActivityPage.qml" },
        { key: "nos", label: "Nós", icon: "server", url: "NodesPage.qml" }
    ]

    Rectangle {
        id: menu
        width: 212
        anchors { top: parent.top; bottom: parent.bottom; left: parent.left }
        color: Qt.rgba(0, 0, 0, 0.16)
        Rectangle {
            anchors { top: parent.top; bottom: parent.bottom; right: parent.right }
            width: 1
            color: Theme.line
        }
        Column {
            anchors { top: parent.top; topMargin: 56; left: parent.left; right: parent.right; leftMargin: 14; rightMargin: 14 }
            spacing: 3
            Text {
                text: "CONFIGURAÇÕES"
                leftPadding: 12
                bottomPadding: 8
                color: Theme.text4
                font.pixelSize: Theme.fontCaption
                font.weight: Font.DemiBold
                font.letterSpacing: Theme.trackCaps
            }
            Repeater {
                model: page.sections
                delegate: NavItem {
                    required property var modelData
                    width: parent.width
                    icon: modelData.icon
                    label: modelData.label
                    active: page.section === modelData.key
                    onClicked: page.section = modelData.key
                }
            }
        }
    }

    Item {
        anchors { top: parent.top; bottom: parent.bottom; left: menu.right; right: parent.right }
        Repeater {
            id: loaders
            model: page.sections
            delegate: Loader {
                id: loader
                required property var modelData
                readonly property bool current: page.section === modelData.key
                property bool opened: false
                onCurrentChanged: if (current) opened = true
                Component.onCompleted: if (current) opened = true
                anchors.fill: parent
                active: opened
                visible: current
                source: modelData.url
                onLoaded: page.deliver()
                opacity: current ? 1 : 0
                Behavior on opacity { NumberAnimation { duration: Theme.medium; easing.type: Theme.ease } }
            }
        }
    }
}
