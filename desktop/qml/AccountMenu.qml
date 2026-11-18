import QtQuick
import QtQuick.Controls.Basic
import QtQuick.Effects
import Avora

// User account menu anchored to the sidebar footer: profile, presence,
// custom status line (PATCH /v1/me), Settings and Sign out.
Popup {
    id: menu
    property Item anchorItem: null

    readonly property var mePerson: (Api.me && Api.me.person && Api.me.person.id) ? Api.me.person : null

    parent: Overlay.overlay
    width: 288
    padding: Theme.space4
    modal: true
    dim: false
    focus: true
    closePolicy: Popup.CloseOnEscape | Popup.CloseOnPressOutside

    function open(target) {
        if (target !== undefined && target !== null)
            menu.anchorItem = target;
        menu.syncStatus();
        menu.reposition();
        menu.visible = true;
    }

    function syncStatus() {
        statusInput.text = (menu.mePerson && menu.mePerson.status_text) ? menu.mePerson.status_text : "";
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
            menu.y = Math.max(margin, ov.height - popH - 64);
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

    function personPhoto(av) {
        if (!av)
            return "";
        if (av.startsWith("avora:file/"))
            return "image://server/v1/files/" + av.slice(11);
        if (av.startsWith("/v1/files/"))
            return "image://server" + av;
        return av;
    }

    function presenceDot(p) {
        return p === "away" ? "warn" : p === "busy" ? "bad" : p === "invisible" ? "" : "live";
    }

    function setPresence(key) {
        Api.patch("/v1/me", { presence: key }, err => {
            if (!err)
                Api.refreshMe();
        });
    }

    function saveStatusText() {
        const text = statusInput.text.trim();
        Api.patch("/v1/me", { status_text: text }, err => {
            if (!err)
                Api.refreshMe();
        });
    }

    onAboutToShow: {
        menu.syncStatus();
        menu.reposition();
    }

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
        spacing: Theme.space3

        // Identity row: avatar, name and e-mail (or admin token label).
        Row {
            width: parent.width
            spacing: Theme.space3

            Item {
                width: 40
                height: 40
                anchors.verticalCenter: parent.verticalCenter

                Avatar {
                    anchors.fill: parent
                    size: 40
                    name: menu.mePerson ? (menu.mePerson.name || menu.mePerson.email || "?") : "A"
                    photo: menu.mePerson ? menu.personPhoto(menu.mePerson.avatar) : ""
                    tint: Theme.palette[0]
                }

                Rectangle {
                    visible: menu.mePerson !== null
                    width: 11
                    height: 11
                    radius: height / 2
                    color: Theme.card
                    anchors { right: parent.right; bottom: parent.bottom }
                    Dot {
                        anchors.centerIn: parent
                        kind: menu.mePerson ? menu.presenceDot(menu.mePerson.presence) : "live"
                    }
                }
            }

            Column {
                width: parent.width - 40 - Theme.space3
                anchors.verticalCenter: parent.verticalCenter
                spacing: Theme.space1

                Text {
                    width: parent.width
                    elide: Text.ElideRight
                    text: menu.mePerson ? (menu.mePerson.name || menu.mePerson.email) : "Administrador"
                    color: Theme.white
                    font.pixelSize: Theme.fontLarge
                    font.weight: Font.DemiBold
                }

                Text {
                    width: parent.width
                    elide: Text.ElideRight
                    text: menu.mePerson ? (menu.mePerson.email || "") : (Api.server || "Acesso por token")
                    color: Theme.text3
                    font.pixelSize: Theme.fontSmall
                }
            }
        }

        // Presence options (only for authenticated people).
        Rectangle {
            visible: menu.mePerson !== null
            width: parent.width
            height: 1
            color: Theme.line
        }

        Column {
            visible: menu.mePerson !== null
            width: parent.width
            spacing: Theme.space2

            Overline { text: "PRESENÇA" }

            Column {
                width: parent.width
                spacing: Theme.space1

                Repeater {
                    model: [
                        { key: "auto", label: "Automático", dot: "live" },
                        { key: "away", label: "Ausente", dot: "warn" },
                        { key: "busy", label: "Ocupado", dot: "bad" },
                        { key: "invisible", label: "Invisível", dot: "" }
                    ]
                    delegate: Rectangle {
                        id: presOpt
                        required property var modelData
                        readonly property string cur: (menu.mePerson && menu.mePerson.presence) ? menu.mePerson.presence : "auto"
                        readonly property bool on: cur === modelData.key
                        width: parent.width
                        height: 32
                        radius: Theme.radiusSm
                        color: on ? Theme.active : presOptArea.containsMouse ? Theme.hover : "transparent"
                        Behavior on color { ColorAnimation { duration: Theme.fast } }

                        Row {
                            x: Theme.space2
                            anchors.verticalCenter: parent.verticalCenter
                            spacing: Theme.space2

                            Dot {
                                kind: presOpt.modelData.dot
                                anchors.verticalCenter: parent.verticalCenter
                            }
                            Text {
                                text: presOpt.modelData.label
                                color: presOpt.on ? Theme.white : Theme.text2
                                font.pixelSize: Theme.fontBody
                                font.weight: presOpt.on ? Font.DemiBold : Font.Normal
                                anchors.verticalCenter: parent.verticalCenter
                            }
                        }

                        Icon {
                            visible: presOpt.on
                            anchors { right: parent.right; rightMargin: Theme.space2; verticalCenter: parent.verticalCenter }
                            name: "check"
                            size: 14
                            color: Theme.white
                        }

                        MouseArea {
                            id: presOptArea
                            anchors.fill: parent
                            hoverEnabled: true
                            cursorShape: Qt.PointingHandCursor
                            onClicked: menu.setPresence(presOpt.modelData.key)
                        }
                    }
                }
            }
        }

        // Status text line (PATCH /v1/me).
        Rectangle {
            visible: menu.mePerson !== null
            width: parent.width
            height: 1
            color: Theme.line
        }

        Column {
            visible: menu.mePerson !== null
            width: parent.width
            spacing: Theme.space2

            Overline { text: "STATUS" }

            Row {
                width: parent.width
                spacing: Theme.space2

                Field {
                    id: statusInput
                    width: parent.width - saveStatusBtn.width - Theme.space2
                    placeholderText: "O que você está fazendo…"
                    maximumLength: 80
                    onAccepted: menu.saveStatusText()
                }

                GhostButton {
                    id: saveStatusBtn
                    height: 42
                    round: true
                    icon: "check"
                    onClicked: menu.saveStatusText()
                }
            }

            Text {
                visible: !!(menu.mePerson && menu.mePerson.status_text)
                text: "Limpar status"
                color: clearStatusArea.containsMouse ? Theme.white : Theme.text3
                font.pixelSize: Theme.fontSmall

                MouseArea {
                    id: clearStatusArea
                    anchors.fill: parent
                    hoverEnabled: true
                    cursorShape: Qt.PointingHandCursor
                    onClicked: {
                        statusInput.text = "";
                        menu.saveStatusText();
                    }
                }
            }
        }

        Rectangle {
            width: parent.width
            height: 1
            color: Theme.line
        }

        // Navigation & sign-out actions.
        Column {
            width: parent.width
            spacing: Theme.space1

            Rectangle {
                width: parent.width
                height: 34
                radius: Theme.radiusSm
                color: configArea.pressed ? Theme.active : configArea.containsMouse ? Theme.hover : "transparent"
                Behavior on color { ColorAnimation { duration: Theme.fast } }

                Row {
                    x: Theme.space2
                    anchors.verticalCenter: parent.verticalCenter
                    spacing: Theme.space2

                    Icon {
                        name: "gear"
                        size: 15
                        color: configArea.containsMouse ? Theme.white : Theme.text2
                        anchors.verticalCenter: parent.verticalCenter
                    }
                    Text {
                        text: "Configurações"
                        color: configArea.containsMouse ? Theme.white : Theme.text2
                        font.pixelSize: Theme.fontBody
                        anchors.verticalCenter: parent.verticalCenter
                    }
                }

                MouseArea {
                    id: configArea
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

            Rectangle {
                width: parent.width
                height: 34
                radius: Theme.radiusSm
                color: logoutArea.pressed ? Theme.badWash : logoutArea.containsMouse ? Theme.hover : "transparent"
                Behavior on color { ColorAnimation { duration: Theme.fast } }

                Row {
                    x: Theme.space2
                    anchors.verticalCenter: parent.verticalCenter
                    spacing: Theme.space2

                    Icon {
                        name: "logout"
                        size: 15
                        color: Theme.bad
                        anchors.verticalCenter: parent.verticalCenter
                    }
                    Text {
                        text: "Sair"
                        color: Theme.bad
                        font.pixelSize: Theme.fontBody
                        anchors.verticalCenter: parent.verticalCenter
                    }
                }

                MouseArea {
                    id: logoutArea
                    anchors.fill: parent
                    hoverEnabled: true
                    cursorShape: Qt.PointingHandCursor
                    onClicked: {
                        menu.close();
                        Api.logout();
                    }
                }
            }
        }
    }
}
