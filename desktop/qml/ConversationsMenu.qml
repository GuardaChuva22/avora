import QtQuick
import QtQuick.Controls.Basic
import Avora
import "util.js" as U

// Conversation history menu for one agent: search, start a new conversation,
// switch between active and archived, archive or delete.
Popup {
    id: menu
    property var conversations: []
    property string currentId: ""
    property var liveMap: ({})
    property bool showArchived: false
    property string confirmDelete: ""
    property color tint: Theme.text2

    signal selectRequested(string convId)
    signal newRequested
    signal toggleArchivedRequested
    signal archiveRequested(string convId, bool archived)
    signal deleteRequested(string convId)

    width: 348
    padding: Theme.space3
    closePolicy: Popup.CloseOnEscape | Popup.CloseOnPressOutside

    function fold(t) {
        return String(t || "").toLowerCase().normalize("NFD").replace(/[\u0300-\u036f]/g, "");
    }

    function toggleAt(anchor) {
        if (opened) {
            close();
            return;
        }
        if (anchor) {
            parent = anchor;
            x = Math.round(anchor.width - width);
            y = anchor.height + Theme.space2;
        }
        searchField.text = "";
        open();
        Qt.callLater(() => searchField.forceActiveFocus());
    }

    readonly property string cleanQuery: fold(searchField.text.trim())
    readonly property var filtered: {
        const q = menu.cleanQuery;
        const list = menu.conversations || [];
        if (q === "")
            return list;
        return list.filter(c => menu.fold((c.title || "") + " " + (c.preview || "")).indexOf(q) >= 0);
    }

    enter: Transition {
        ParallelAnimation {
            NumberAnimation { property: "opacity"; from: 0; to: 1; duration: Theme.medium; easing.type: Theme.ease }
            NumberAnimation { property: "scale"; from: 0.97; to: 1; duration: Theme.medium; easing.type: Theme.ease }
        }
    }
    exit: Transition {
        NumberAnimation { property: "opacity"; from: 1; to: 0; duration: Theme.fast; easing.type: Easing.InQuad }
    }

    background: Rectangle {
        radius: Theme.radiusLg
        color: Theme.card
        border.width: 1
        border.color: Theme.lineStrong
    }

    contentItem: Column {
        spacing: Theme.space2

        Item {
            width: parent.width
            height: 32

            Overline {
                anchors { left: parent.left; leftMargin: Theme.space1; verticalCenter: parent.verticalCenter }
                text: menu.showArchived ? "ARQUIVADAS" : "CONVERSAS"
            }

            Row {
                anchors { right: parent.right; verticalCenter: parent.verticalCenter }
                spacing: Theme.space1

                GhostButton {
                    id: archToggleBtn
                    small: true
                    round: true
                    icon: "archive"
                    color: menu.showArchived ? Theme.active : (archToggleBtn.hovered ? Theme.hover : "transparent")
                    onClicked: menu.toggleArchivedRequested()
                    Tip {
                        text: menu.showArchived ? "Voltar às conversas" : "Ver arquivadas"
                        show: archToggleBtn.hovered
                    }
                }

                GhostButton {
                    small: true
                    icon: "plus"
                    text: "Nova conversa"
                    onClicked: {
                        menu.newRequested();
                        menu.close();
                    }
                }
            }
        }

        Field {
            id: searchField
            width: parent.width
            implicitHeight: 38
            leftPadding: 34
            placeholderText: menu.showArchived ? "Buscar nas arquivadas…" : "Buscar conversa…"
            Keys.onEscapePressed: event => {
                if (text !== "") {
                    text = "";
                    event.accepted = true;
                } else {
                    menu.close();
                    event.accepted = true;
                }
            }
            Icon {
                x: Theme.space3
                anchors.verticalCenter: parent.verticalCenter
                name: "search"
                size: 14
                color: Theme.text4
            }
        }

        ListView {
            id: convList
            visible: menu.filtered.length > 0
            width: parent.width
            height: Math.min(contentHeight, 320)
            clip: true
            spacing: 2
            model: menu.filtered
            boundsBehavior: Flickable.StopAtBounds
            ScrollBar.vertical: SlimScrollBar {}
            SmoothWheel {}

            delegate: Rectangle {
                id: thr
                required property var modelData
                readonly property bool here: modelData.id === menu.currentId
                readonly property bool armed: menu.confirmDelete === modelData.id
                readonly property bool hot: thrArea.containsMouse || archBtn.hovered || delBtn.hovered || armed
                readonly property bool beat: (modelData.title || "").startsWith("💓")
                readonly property bool runningNow: !!menu.liveMap[modelData.id]

                width: convList.width
                height: 52
                radius: Theme.radiusMd
                color: here ? Theme.active : hot ? Theme.hover : "transparent"
                Behavior on color { ColorAnimation { duration: Theme.fast } }

                Rectangle {
                    x: 0
                    anchors.verticalCenter: parent.verticalCenter
                    width: 2
                    height: 18
                    radius: 1
                    visible: thr.here
                    color: menu.tint
                }

                Column {
                    anchors {
                        left: parent.left
                        leftMargin: Theme.space3
                        right: thr.hot ? thrTools.left : parent.right
                        rightMargin: Theme.space2
                        verticalCenter: parent.verticalCenter
                    }
                    spacing: 2

                    Text {
                        width: parent.width
                        text: thr.armed ? "Clique de novo para apagar" : (thr.modelData.title || "Conversa")
                        color: thr.armed ? Theme.bad : thr.here ? Theme.white : thr.beat ? Theme.text3 : Theme.text
                        font.pixelSize: Theme.fontBody
                        font.weight: thr.here ? Font.DemiBold : Font.Normal
                        elide: Text.ElideRight
                    }

                    Row {
                        width: parent.width
                        spacing: 6

                        Dot {
                            visible: thr.runningNow
                            anchors.verticalCenter: parent.verticalCenter
                            kind: "live"
                        }

                        Text {
                            width: parent.width - (thr.runningNow ? 14 : 0)
                            text: thr.runningNow
                                  ? "trabalhando agora"
                                  : U.ago(thr.modelData.updated_at) + (thr.modelData.preview ? "  ·  " + thr.modelData.preview : "")
                            color: thr.runningNow ? Qt.lighter(menu.tint, 1.1) : Theme.text4
                            font.pixelSize: Theme.fontCaption
                            elide: Text.ElideRight
                        }
                    }
                }

                MouseArea {
                    id: thrArea
                    anchors.fill: parent
                    hoverEnabled: true
                    cursorShape: Qt.PointingHandCursor
                    onClicked: {
                        menu.selectRequested(thr.modelData.id);
                        menu.close();
                    }
                }

                Row {
                    id: thrTools
                    visible: thr.hot
                    anchors { right: parent.right; rightMargin: 6; verticalCenter: parent.verticalCenter }
                    spacing: 2

                    RowTool {
                        id: archBtn
                        icon: "archive"
                        tip: menu.showArchived ? "Desarquivar" : "Arquivar"
                        onHit: menu.archiveRequested(thr.modelData.id, !menu.showArchived)
                    }

                    RowTool {
                        id: delBtn
                        icon: "trash"
                        tip: "Apagar"
                        danger: thr.armed
                        onHit: menu.deleteRequested(thr.modelData.id)
                    }
                }
            }
        }

        Rectangle {
            visible: menu.filtered.length === 0
            width: parent.width
            height: emptyText.implicitHeight + 2 * Theme.space4
            radius: Theme.radiusMd
            color: Theme.wash1
            border.width: 1
            border.color: Theme.line

            Text {
                id: emptyText
                anchors.centerIn: parent
                width: parent.width - 2 * Theme.space4
                horizontalAlignment: Text.AlignHCenter
                wrapMode: Text.Wrap
                text: menu.cleanQuery !== ""
                      ? "Nenhuma conversa encontrada para «" + searchField.text.trim() + "»."
                      : menu.showArchived
                        ? "Nenhuma conversa arquivada com este agente."
                        : "Nenhuma conversa salva ainda. Escreva uma mensagem para começar."
                color: Theme.text3
                font.pixelSize: Theme.fontSmall
            }
        }
    }
}
