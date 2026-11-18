import QtQuick
import Avora

// "O que mudou": floats at the bottom of a page while it has unsaved
// changes, lists them (old value struck out, new value) and saves or
// discards them together.
Item {
    id: bar
    property var changes: [] // [{ label, from, to }]
    property bool saving: false
    property string error: ""
    property bool expanded: false
    signal save
    signal discard

    readonly property bool active: changes.length > 0
    onActiveChanged: if (!active) expanded = false

    implicitHeight: card.height
    height: card.height
    visible: card.opacity > 0
    enabled: active

    Shortcut {
        sequence: "Ctrl+S"
        enabled: bar.active && !bar.saving
        onActivated: bar.save()
    }

    Rectangle {
        id: card
        width: parent.width
        height: col.implicitHeight + 2 * Theme.space3
        radius: Theme.radiusLg
        color: Theme.card
        border.width: 1
        border.color: Theme.lineStrong
        opacity: bar.active ? 1 : 0
        y: bar.active ? 0 : Theme.space4
        Behavior on opacity { NumberAnimation { duration: Theme.medium } }
        Behavior on y { NumberAnimation { duration: Theme.medium; easing.type: Theme.ease } }
        MouseArea { anchors.fill: parent } // clicks stay on the bar

        Column {
            id: col
            x: Theme.space4
            y: Theme.space3
            width: parent.width - 2 * Theme.space4
            spacing: Theme.space2

            // Every change, when expanded.
            Column {
                width: parent.width
                spacing: Theme.space1
                visible: bar.expanded
                bottomPadding: Theme.space1
                Repeater {
                    model: bar.expanded ? bar.changes : []
                    delegate: Row {
                        id: ch
                        required property var modelData
                        width: col.width
                        spacing: Theme.space2
                        Text {
                            width: 128
                            text: ch.modelData.label
                            color: Theme.text3
                            font.pixelSize: Theme.fontSmall
                            elide: Text.ElideRight
                        }
                        Text {
                            width: (ch.width - 128 - 18 - 3 * Theme.space2) / 2
                            text: String(ch.modelData.from) === "" ? "vazio" : String(ch.modelData.from)
                            color: Theme.text4
                            font.pixelSize: Theme.fontSmall
                            font.strikeout: String(ch.modelData.from) !== ""
                            elide: Text.ElideMiddle
                        }
                        Icon {
                            name: "chevron"
                            size: 12
                            color: Theme.text4
                            anchors.verticalCenter: parent.verticalCenter
                        }
                        Text {
                            width: (ch.width - 128 - 18 - 3 * Theme.space2) / 2
                            text: String(ch.modelData.to) === "" ? "vazio" : String(ch.modelData.to)
                            color: Theme.text
                            font.pixelSize: Theme.fontSmall
                            elide: Text.ElideMiddle
                        }
                    }
                }
                Rectangle {
                    width: parent.width
                    height: 1
                    color: Theme.line
                }
            }

            Text {
                visible: bar.error !== ""
                width: parent.width
                text: bar.error
                color: Theme.bad
                font.pixelSize: Theme.fontSmall
                wrapMode: Text.Wrap
            }

            Item {
                width: parent.width
                height: Math.max(actions.height, summary.height)
                Row {
                    id: summary
                    anchors.verticalCenter: parent.verticalCenter
                    spacing: Theme.space2
                    Icon {
                        anchors.verticalCenter: parent.verticalCenter
                        name: "pen"
                        size: 14
                        color: Theme.warn
                    }
                    Text {
                        anchors.verticalCenter: parent.verticalCenter
                        text: bar.changes.length === 1 ? "1 alteração não salva" : bar.changes.length + " alterações não salvas"
                        color: Theme.text
                        font.pixelSize: Theme.fontBody
                        font.weight: Font.DemiBold
                    }
                    Text {
                        anchors.verticalCenter: parent.verticalCenter
                        text: bar.expanded ? "esconder" : "ver"
                        color: seeArea.containsMouse ? Theme.white : Theme.text3
                        font.pixelSize: Theme.fontSmall
                        font.underline: seeArea.containsMouse
                        MouseArea {
                            id: seeArea
                            anchors.fill: parent
                            anchors.margins: -6
                            hoverEnabled: true
                            cursorShape: Qt.PointingHandCursor
                            onClicked: bar.expanded = !bar.expanded
                        }
                    }
                }
                Row {
                    id: actions
                    anchors { right: parent.right; verticalCenter: parent.verticalCenter }
                    spacing: Theme.space2
                    GhostButton {
                        text: "Descartar"
                        small: true
                        onClicked: bar.discard()
                    }
                    PrimaryButton {
                        text: "Salvar"
                        icon: "check"
                        busy: bar.saving
                        onClicked: bar.save()
                    }
                }
            }
        }
    }
}
