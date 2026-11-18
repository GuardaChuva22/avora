import QtQuick
import Avora
import "util.js" as U

// One doc as a card: its kind, title and where it is. A click opens it in
// the panel beside the page; dragging it into the message box cites it.
Rectangle {
    id: card
    property string docId: ""
    property string fallbackTitle: ""
    property string tag: ""         // "Criou", "Editou · v3"…
    property bool compact: false
    readonly property var doc: { DocBus.rev; return DocBus.byId(docId); }
    readonly property bool gone: DocBus.loaded && doc === null
    readonly property bool current: DocBus.openId === docId && docId !== ""

    implicitWidth: 460
    implicitHeight: compact ? 48 : 60
    radius: Theme.radiusMd
    color: area.pressed ? Theme.active : area.containsMouse || current ? Theme.active : Theme.wash1
    border.width: 1
    border.color: current ? Theme.lineStrong : area.containsMouse ? Theme.lineStrong : Theme.line
    Behavior on color { ColorAnimation { duration: Theme.fast } }
    opacity: gone ? 0.6 : 1

    // Dragged out, it carries the link: into the box it becomes a chip,
    // into another app the Markdown link.
    Drag.dragType: Drag.Automatic
    Drag.supportedActions: Qt.CopyAction
    Drag.mimeData: ({ "text/x-avora-doc": card.docId, "text/plain": card.doc ? DocBus.link(card.doc) : "" })
    Drag.active: dragger.active && !card.gone

    Rectangle {
        id: tile
        x: 10
        anchors.verticalCenter: parent.verticalCenter
        width: card.compact ? 30 : 38
        height: width
        radius: card.compact ? 9 : 11
        color: Theme.hover
        border.width: 1
        border.color: Theme.active
        Icon {
            anchors.centerIn: parent
            name: DocBus.icon(card.doc)
            size: card.compact ? 15 : 18
            weight: 1.5
            color: DocBus.tone(card.doc)
        }
    }
    Column {
        anchors { left: tile.right; leftMargin: 12; right: open.left; rightMargin: 10; verticalCenter: parent.verticalCenter }
        spacing: 3
        Text {
            width: parent.width
            text: card.doc ? card.doc.title : card.fallbackTitle || "Doc"
            color: Theme.white
            font.pixelSize: card.compact ? Theme.fontBody : Theme.fontLarge
            font.weight: Font.DemiBold
            elide: Text.ElideRight
        }
        Text {
            width: parent.width
            visible: !card.compact || card.tag !== ""
            textFormat: Text.StyledText
            text: {
                if (card.gone)
                    return "Doc removido ou na lixeira";
                const d = card.doc;
                if (!d)
                    return "Docs";
                const parts = [];
                if (card.tag)
                    parts.push("<font color='" + Theme.text2 + "'>" + U.esc(card.tag) + "</font>");
                parts.push(DocBus.kindLabel(d) + (d.kind === "file" ? " · " + DocBus.size(d.size) : " · v" + d.version));
                if (d.folder)
                    parts.push(U.esc(d.folder));
                return parts.join("  ·  ");
            }
            color: Theme.text3
            font.pixelSize: Theme.fontSmall
            elide: Text.ElideRight
        }
    }
    Row {
        id: open
        anchors { right: parent.right; rightMargin: 14; verticalCenter: parent.verticalCenter }
        spacing: 4
        visible: !card.gone
        Text {
            anchors.verticalCenter: parent.verticalCenter
            text: card.current ? "Aberto" : "Abrir"
            color: area.containsMouse ? Theme.text : Theme.text3
            font.pixelSize: Theme.fontSmall
        }
        Icon {
            anchors.verticalCenter: parent.verticalCenter
            name: "chevron"
            rotation: -90
            size: 13
            color: area.containsMouse ? Theme.text : Theme.text4
        }
    }
    // Handlers on the card itself, so a press can become a click or a drag.
    QtObject {
        id: area
        readonly property bool containsMouse: hover.hovered
        readonly property bool pressed: tap.pressed
    }
    HoverHandler {
        id: hover
        cursorShape: card.gone ? Qt.ArrowCursor : Qt.PointingHandCursor
    }
    TapHandler {
        id: tap
        enabled: !card.gone
        onTapped: DocBus.open(card.docId)
    }
    DragHandler {
        id: dragger
        target: null
        enabled: !card.gone
        onActiveChanged: if (active) card.grabToImage(r => card.Drag.imageSource = r.url, Qt.size(card.width / 2, card.height / 2))
    }
    Tip {
        text: "Clique para abrir · arraste para o campo de mensagem para citar"
        show: area.containsMouse && !dragger.active
    }
}
