import QtQuick
import QtQuick.Controls.Basic
import QtQuick.Effects
import Avora

// Leftmost 64 px workspace switcher, like Discord's server rail. Hidden in
// personal mode when there is only one workspace ("personal mode only hides").
Rectangle {
    id: rail
    readonly property var workspaces: (Api.me && Api.me.workspaces) ? Api.me.workspaces : []
    readonly property bool shown: workspaces.length > 1 || (workspaces.length === 1 && workspaces[0].mode !== "personal")

    signal addRequested

    property string tipText: ""
    property real tipY: 0
    property bool tipShown: false

    function showTip(item, label) {
        const pt = item.mapToItem(rail, 0, 0);
        tipY = pt.y;
        tipText = label;
        tipShown = true;
    }

    function hideTip(label) {
        if (tipText === label)
            tipShown = false;
    }

    width: 64
    visible: shown
    color: Theme.well

    // Right hairline border separating the rail from the navigation sidebar.
    Rectangle {
        anchors { top: parent.top; bottom: parent.bottom; right: parent.right }
        width: 1
        color: Theme.line
    }

    // Top window-drag strip for the frameless window.
    Item {
        width: parent.width
        height: Theme.titleBar
        DragHandler {
            target: null
            onActiveChanged: if (active && Window.window) Window.window.startSystemMove()
        }
    }

    Flickable {
        id: scroller
        anchors { top: parent.top; topMargin: Theme.space4; bottom: parent.bottom; bottomMargin: Theme.space4; left: parent.left; right: parent.right }
        contentHeight: col.implicitHeight
        clip: true
        boundsBehavior: Flickable.StopAtBounds
        ScrollBar.vertical: SlimScrollBar {}
        SmoothWheel {}

        Column {
            id: col
            width: scroller.width
            spacing: Theme.space2

            Repeater {
                model: rail.workspaces
                delegate: Item {
                    id: tile
                    required property var modelData
                    required property int index

                    readonly property bool active: Api.workspace === modelData.id || (Api.workspace === "" && index === 0)
                    readonly property bool pending: modelData.status === "pending"
                    readonly property string labelText: (modelData.name || "Espaço") + (pending ? "  ·  aguardando aprovação" : "")

                    width: col.width
                    height: 44

                    // Active / hover pill on the left edge.
                    RectangularShadow {
                        anchors.fill: indicator
                        radius: indicator.radius
                        blur: 8
                        visible: tile.active
                        color: Qt.rgba(1, 1, 1, 0.5)
                    }
                    Rectangle {
                        id: indicator
                        x: 0
                        width: 3
                        height: tile.active ? 28 : area.containsMouse ? 14 : 0
                        radius: width / 2
                        anchors.verticalCenter: parent.verticalCenter
                        color: Theme.white
                        Behavior on height { NumberAnimation { duration: Theme.move; easing.type: Theme.ease } }
                    }

                    Rectangle {
                        id: box
                        width: 42
                        height: 42
                        anchors.centerIn: parent
                        radius: tile.active || area.containsMouse ? Theme.radiusMd : Theme.radiusLg
                        color: tile.active ? Theme.wash4 : area.pressed ? Theme.active : area.containsMouse ? Theme.hover : Theme.wash1
                        border.width: 1
                        border.color: tile.active ? Theme.focusLine : area.containsMouse ? Theme.lineStrong : Theme.line
                        opacity: tile.pending ? 0.55 : 1
                        scale: area.pressed ? 0.96 : 1
                        Behavior on radius { NumberAnimation { duration: Theme.fast; easing.type: Theme.ease } }
                        Behavior on color { ColorAnimation { duration: Theme.fast } }
                        Behavior on border.color { ColorAnimation { duration: Theme.fast } }
                        Behavior on scale { NumberAnimation { duration: Theme.fast; easing.type: Theme.ease } }

                        Text {
                            anchors.centerIn: parent
                            text: tile.modelData.icon ? tile.modelData.icon : (tile.modelData.name || "?").charAt(0).toUpperCase()
                            color: tile.active ? Theme.white : area.containsMouse ? Theme.text : Theme.text2
                            font.family: Theme.display
                            font.pixelSize: Theme.fontTitle
                            font.weight: Font.DemiBold
                        }

                        Dot {
                            visible: tile.pending
                            kind: "warn"
                            anchors { right: parent.right; bottom: parent.bottom; margins: Theme.space1 }
                        }
                    }

                    MouseArea {
                        id: area
                        anchors.fill: box
                        hoverEnabled: true
                        cursorShape: tile.pending ? Qt.ArrowCursor : Qt.PointingHandCursor
                        onContainsMouseChanged: {
                            if (containsMouse)
                                rail.showTip(tile, tile.labelText);
                            else
                                rail.hideTip(tile.labelText);
                        }
                        onClicked: {
                            if (!tile.pending && tile.modelData.id)
                                Api.setWorkspace(tile.modelData.id);
                        }
                    }
                }
            }

            Rectangle {
                width: 28
                height: 1
                color: Theme.line
                anchors.horizontalCenter: parent.horizontalCenter
            }

            // "+" tile to create a workspace or join with an invite code.
            Item {
                id: addTile
                readonly property string addLabel: "Criar ou entrar em espaço"
                width: col.width
                height: 44

                Rectangle {
                    id: addBox
                    width: 42
                    height: 42
                    anchors.centerIn: parent
                    radius: addArea.containsMouse ? Theme.radiusMd : Theme.radiusLg
                    color: addArea.pressed ? Theme.active : addArea.containsMouse ? Theme.hover : "transparent"
                    border.width: 1
                    border.color: addArea.containsMouse ? Theme.lineStrong : Theme.line
                    scale: addArea.pressed ? 0.96 : 1
                    Behavior on radius { NumberAnimation { duration: Theme.fast; easing.type: Theme.ease } }
                    Behavior on color { ColorAnimation { duration: Theme.fast } }
                    Behavior on border.color { ColorAnimation { duration: Theme.fast } }
                    Behavior on scale { NumberAnimation { duration: Theme.fast; easing.type: Theme.ease } }

                    Icon {
                        anchors.centerIn: parent
                        name: "plus"
                        size: 18
                        color: addArea.containsMouse ? Theme.white : Theme.text3
                    }
                }

                MouseArea {
                    id: addArea
                    anchors.fill: addBox
                    hoverEnabled: true
                    cursorShape: Qt.PointingHandCursor
                    onContainsMouseChanged: {
                        if (containsMouse)
                            rail.showTip(addTile, addTile.addLabel);
                        else
                            rail.hideTip(addTile.addLabel);
                    }
                    onClicked: rail.addRequested()
                }
            }
        }
    }

    Item {
        x: 0
        y: rail.tipY
        width: rail.width
        height: 44
        Tip {
            text: rail.tipText
            show: rail.tipShown
            gap: Theme.space2
        }
    }
}
