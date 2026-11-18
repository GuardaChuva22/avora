import QtQuick
import QtQuick.Controls.Basic
import QtQuick.Effects
import Avora

// Workspace switcher and actions menu anchored to the sidebar workspace card.
// Reuses WorkspaceRail's switching rules and opens WorkspaceSheet to create/join.
Popup {
    id: menu
    property Item anchorItem: null

    readonly property var mePerson: (Api.me && Api.me.person && Api.me.person.id) ? Api.me.person : null
    readonly property bool adminOnly: !Api.me || mePerson === null
    readonly property var rawWorkspaces: (Api.me && Api.me.workspaces) ? Api.me.workspaces : []
    readonly property var workspaces: {
        if (rawWorkspaces.length > 0) {
            if (adminOnly) {
                const cur = rawWorkspaces.find((w, idx) => Api.workspace === w.id || (Api.workspace === "" && idx === 0)) || rawWorkspaces[0];
                return [cur];
            }
            return rawWorkspaces;
        }
        const fallbackId = Api.workspace !== "" ? Api.workspace : "pessoal";
        const fallbackName = fallbackId === "pessoal" ? "Pessoal" : fallbackId;
        return [{ id: fallbackId, name: fallbackName, icon: "", mode: "personal", role: "owner", status: "active" }];
    }

    parent: Overlay.overlay
    width: 280
    padding: Theme.space3
    modal: true
    dim: false
    focus: true
    closePolicy: Popup.CloseOnEscape | Popup.CloseOnPressOutside

    function open(target) {
        if (target !== undefined && target !== null)
            menu.anchorItem = target;
        menu.reposition();
        menu.visible = true;
    }

    function reposition() {
        const ov = Overlay.overlay;
        if (!ov)
            return;
        const margin = Theme.space2;
        const popW = menu.width;
        const popH = Math.max(menu.height, menuCol.implicitHeight + 2 * menu.padding);
        if (!menu.anchorItem) {
            menu.x = margin;
            menu.y = Theme.titleBar + Theme.space2;
            return;
        }
        const pt = menu.anchorItem.mapToItem(ov, 0, 0);
        let nx = pt.x;
        if (nx + popW > ov.width - margin)
            nx = ov.width - popW - margin;
        if (nx < margin)
            nx = margin;

        let ny = pt.y + menu.anchorItem.height + Theme.space1;
        if (pt.y > ov.height * 0.55 || ny + popH > ov.height - margin) {
            const above = pt.y - popH - Theme.space1;
            ny = above >= margin ? above : Math.max(margin, ov.height - popH - margin);
        }
        menu.x = Math.round(nx);
        menu.y = Math.round(ny);
    }

    function roleLabel(r) {
        return r === "owner" ? "Dono" : r === "admin" ? "Administrador" : "Membro";
    }

    function selectWorkspace(ws) {
        if (!ws || ws.status === "pending" || !ws.id)
            return;
        menu.close();
        Api.setWorkspace(ws.id);
    }

    function openWorkspaceSettings() {
        menu.close();
        if (typeof win !== "undefined" && win && win.go) {
            if ("section" in win)
                win.section = "pessoas";
            win.go("pessoas");
            if (win.page !== "config")
                win.go("config");
        }
    }

    onAboutToShow: menu.reposition()

    onImplicitHeightChanged: {
        if (menu.visible)
            Qt.callLater(menu.reposition);
    }

    enter: Transition {
        NumberAnimation { property: "opacity"; from: 0.0; to: 1.0; duration: Theme.fast; easing.type: Theme.ease }
    }
    exit: Transition {
        NumberAnimation { property: "opacity"; from: 1.0; to: 0.0; duration: Theme.fast; easing.type: Theme.ease }
    }

    background: Item {
        RectangularShadow {
            anchors.fill: bgCard
            radius: bgCard.radius
            blur: 24
            color: Qt.rgba(0, 0, 0, 0.55)
        }
        Rectangle {
            id: bgCard
            anchors.fill: parent
            radius: Theme.radiusLg
            color: Theme.card
            border.width: 1
            border.color: Theme.lineStrong
        }
    }

    contentItem: Column {
        id: menuCol
        width: menu.availableWidth
        spacing: Theme.space2

        Overline {
            leftPadding: Theme.space2
            topPadding: Theme.space1
            text: menu.adminOnly ? "ESPAÇO ATUAL" : "ESPAÇOS"
        }

        Flickable {
            id: wsScroll
            width: parent.width
            height: Math.min(wsListCol.implicitHeight, 260)
            contentHeight: wsListCol.implicitHeight
            clip: true
            boundsBehavior: Flickable.StopAtBounds
            ScrollBar.vertical: SlimScrollBar {}
            SmoothWheel {}

            Column {
                id: wsListCol
                width: wsScroll.width
                spacing: Theme.space1

                Repeater {
                    model: menu.workspaces
                    delegate: Rectangle {
                        id: wsRow
                        required property var modelData
                        required property int index

                        readonly property bool active: menu.adminOnly
                            || Api.workspace === modelData.id
                            || (Api.workspace === "" && index === 0)
                        readonly property bool pending: modelData.status === "pending"

                        width: wsListCol.width
                        height: 44
                        radius: Theme.radiusSm
                        color: active ? Theme.active : (!pending && wsArea.containsMouse) ? Theme.hover : "transparent"
                        opacity: pending ? 0.6 : 1
                        Behavior on color { ColorAnimation { duration: Theme.fast } }

                        Rectangle {
                            id: wsBadge
                            x: Theme.space2
                            width: 30
                            height: 30
                            radius: Theme.radiusSm
                            anchors.verticalCenter: parent.verticalCenter
                            color: wsRow.active ? Theme.wash4 : Theme.wash2
                            border.width: 1
                            border.color: wsRow.active ? Theme.focusLine : Theme.line

                            Text {
                                anchors.centerIn: parent
                                text: wsRow.modelData.icon ? wsRow.modelData.icon : (wsRow.modelData.name || "?").charAt(0).toUpperCase()
                                color: wsRow.active ? Theme.white : Theme.text2
                                font.family: Theme.display
                                font.pixelSize: Theme.fontBody
                                font.weight: Font.DemiBold
                            }

                            Dot {
                                visible: wsRow.pending
                                kind: "warn"
                                anchors { right: parent.right; bottom: parent.bottom; margins: Theme.space1 }
                            }
                        }

                        Column {
                            anchors {
                                left: wsBadge.right
                                leftMargin: Theme.space2
                                right: wsCheck.left
                                rightMargin: Theme.space2
                                verticalCenter: parent.verticalCenter
                            }
                            spacing: 0

                            Text {
                                width: parent.width
                                elide: Text.ElideRight
                                text: wsRow.modelData.name || "Espaço"
                                color: wsRow.active ? Theme.white : Theme.text
                                font.pixelSize: Theme.fontBody
                                font.weight: wsRow.active ? Font.DemiBold : Font.Medium
                            }

                            Text {
                                width: parent.width
                                elide: Text.ElideRight
                                text: wsRow.pending ? "Aguardando aprovação"
                                    : wsRow.modelData.mode === "personal" ? "Pessoal"
                                    : menu.roleLabel(wsRow.modelData.role)
                                color: wsRow.pending ? Theme.warn : Theme.text3
                                font.pixelSize: Theme.fontCaption
                            }
                        }

                        Item {
                            id: wsCheck
                            width: 20
                            height: 20
                            anchors { right: parent.right; rightMargin: Theme.space2; verticalCenter: parent.verticalCenter }

                            Icon {
                                visible: wsRow.active && !wsRow.pending
                                anchors.centerIn: parent
                                name: "check"
                                size: 15
                                color: Theme.white
                            }
                        }

                        MouseArea {
                            id: wsArea
                            anchors.fill: parent
                            hoverEnabled: true
                            cursorShape: wsRow.pending ? Qt.ArrowCursor : Qt.PointingHandCursor
                            onClicked: menu.selectWorkspace(wsRow.modelData)
                        }
                    }
                }
            }
        }

        Rectangle {
            width: parent.width
            height: 1
            color: Theme.line
        }

        // Workspace management actions (full account mode).
        Column {
            visible: !menu.adminOnly
            width: parent.width
            spacing: Theme.space1

            Rectangle {
                width: parent.width
                height: 34
                radius: Theme.radiusSm
                color: inviteArea.pressed ? Theme.active : inviteArea.containsMouse ? Theme.hover : "transparent"
                Behavior on color { ColorAnimation { duration: Theme.fast } }

                Row {
                    x: Theme.space2
                    anchors.verticalCenter: parent.verticalCenter
                    spacing: Theme.space2

                    Icon {
                        name: "users"
                        size: 15
                        color: inviteArea.containsMouse ? Theme.white : Theme.text2
                        anchors.verticalCenter: parent.verticalCenter
                    }
                    Text {
                        text: "Convidar pessoas"
                        color: inviteArea.containsMouse ? Theme.white : Theme.text2
                        font.pixelSize: Theme.fontBody
                        anchors.verticalCenter: parent.verticalCenter
                    }
                }

                MouseArea {
                    id: inviteArea
                    anchors.fill: parent
                    hoverEnabled: true
                    cursorShape: Qt.PointingHandCursor
                    onClicked: {
                        menu.close();
                        if (typeof win !== "undefined" && win && win.invite)
                            win.invite();
                    }
                }
            }

            Rectangle {
                width: parent.width
                height: 34
                radius: Theme.radiusSm
                color: wsSettingsArea.pressed ? Theme.active : wsSettingsArea.containsMouse ? Theme.hover : "transparent"
                Behavior on color { ColorAnimation { duration: Theme.fast } }

                Row {
                    x: Theme.space2
                    anchors.verticalCenter: parent.verticalCenter
                    spacing: Theme.space2

                    Icon {
                        name: "gear"
                        size: 15
                        color: wsSettingsArea.containsMouse ? Theme.white : Theme.text2
                        anchors.verticalCenter: parent.verticalCenter
                    }
                    Text {
                        text: "Configurações do espaço"
                        color: wsSettingsArea.containsMouse ? Theme.white : Theme.text2
                        font.pixelSize: Theme.fontBody
                        anchors.verticalCenter: parent.verticalCenter
                    }
                }

                MouseArea {
                    id: wsSettingsArea
                    anchors.fill: parent
                    hoverEnabled: true
                    cursorShape: Qt.PointingHandCursor
                    onClicked: menu.openWorkspaceSettings()
                }
            }
        }

        Rectangle {
            visible: !menu.adminOnly
            width: parent.width
            height: 1
            color: Theme.line
        }

        // Create or join another workspace (full account mode).
        Column {
            visible: !menu.adminOnly
            width: parent.width
            spacing: Theme.space1

            Rectangle {
                width: parent.width
                height: 34
                radius: Theme.radiusSm
                color: createWsArea.pressed ? Theme.active : createWsArea.containsMouse ? Theme.hover : "transparent"
                Behavior on color { ColorAnimation { duration: Theme.fast } }

                Row {
                    x: Theme.space2
                    anchors.verticalCenter: parent.verticalCenter
                    spacing: Theme.space2

                    Icon {
                        name: "plus"
                        size: 15
                        color: createWsArea.containsMouse ? Theme.white : Theme.text2
                        anchors.verticalCenter: parent.verticalCenter
                    }
                    Text {
                        text: "Criar espaço"
                        color: createWsArea.containsMouse ? Theme.white : Theme.text2
                        font.pixelSize: Theme.fontBody
                        anchors.verticalCenter: parent.verticalCenter
                    }
                }

                MouseArea {
                    id: createWsArea
                    anchors.fill: parent
                    hoverEnabled: true
                    cursorShape: Qt.PointingHandCursor
                    onClicked: {
                        menu.close();
                        win.openWorkspaceSheet("create");
                    }
                }
            }

            Rectangle {
                width: parent.width
                height: 34
                radius: Theme.radiusSm
                color: joinWsArea.pressed ? Theme.active : joinWsArea.containsMouse ? Theme.hover : "transparent"
                Behavior on color { ColorAnimation { duration: Theme.fast } }

                Row {
                    x: Theme.space2
                    anchors.verticalCenter: parent.verticalCenter
                    spacing: Theme.space2

                    Icon {
                        name: "paste"
                        size: 15
                        color: joinWsArea.containsMouse ? Theme.white : Theme.text2
                        anchors.verticalCenter: parent.verticalCenter
                    }
                    Text {
                        text: "Entrar com convite"
                        color: joinWsArea.containsMouse ? Theme.white : Theme.text2
                        font.pixelSize: Theme.fontBody
                        anchors.verticalCenter: parent.verticalCenter
                    }
                }

                MouseArea {
                    id: joinWsArea
                    anchors.fill: parent
                    hoverEnabled: true
                    cursorShape: Qt.PointingHandCursor
                    onClicked: {
                        menu.close();
                        win.openWorkspaceSheet("join");
                    }
                }
            }
        }

        // Admin-token fallback: only current workspace + Settings.
        Rectangle {
            visible: menu.adminOnly
            width: parent.width
            height: 34
            radius: Theme.radiusSm
            color: adminConfigArea.pressed ? Theme.active : adminConfigArea.containsMouse ? Theme.hover : "transparent"
            Behavior on color { ColorAnimation { duration: Theme.fast } }

            Row {
                x: Theme.space2
                anchors.verticalCenter: parent.verticalCenter
                spacing: Theme.space2

                Icon {
                    name: "gear"
                    size: 15
                    color: adminConfigArea.containsMouse ? Theme.white : Theme.text2
                    anchors.verticalCenter: parent.verticalCenter
                }
                Text {
                    text: "Configurações"
                    color: adminConfigArea.containsMouse ? Theme.white : Theme.text2
                    font.pixelSize: Theme.fontBody
                    anchors.verticalCenter: parent.verticalCenter
                }
            }

            MouseArea {
                id: adminConfigArea
                anchors.fill: parent
                hoverEnabled: true
                cursorShape: Qt.PointingHandCursor
                onClicked: {
                    menu.close();
                    if (typeof win !== "undefined" && win && win.go)
                        win.go("config");
                }
            }
        }
    }
}
