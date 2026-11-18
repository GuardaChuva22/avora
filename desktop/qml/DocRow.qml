import QtQuick
import Avora
import "util.js" as U

// A doc in a list: kind, title, where it is and who touched it last, and
// the text around a search hit. Click opens it; drag it into the chat's
// message box to cite it.
Rectangle {
    id: row
    property var doc: ({})
    property var agents: []
    property bool selected: false
    property bool trash: false
    property bool dense: false
    signal opened
    signal pin
    signal remove
    signal restore
    signal purge

    readonly property string who: {
        const a = doc.editor || doc.author || "";
        if (a === "" || a === "admin")
            return "você";
        if (a.startsWith("agent:")) {
            const ag = agents.find(x => x.id === a.slice(6));
            return ag ? ag.name : a.slice(6);
        }
        return a;
    }
    readonly property color whoTint: {
        const a = doc.editor || doc.author || "";
        const ag = a.startsWith("agent:") ? agents.find(x => x.id === a.slice(6)) : null;
        return ag && ag.color ? ag.color : Theme.text3;
    }
    readonly property bool hovered: hover.hovered

    implicitHeight: (dense ? 50 : 58) + (snippet.visible ? snippet.implicitHeight + 4 : 0)
    radius: Theme.radiusMd
    color: selected ? Theme.active : tap.pressed ? Theme.active : hover.hovered ? Theme.hover : "transparent"
    border.width: selected ? 1 : 0
    border.color: Theme.lineStrong
    Behavior on color { ColorAnimation { duration: Theme.fast } }

    Drag.dragType: Drag.Automatic
    Drag.supportedActions: Qt.CopyAction
    Drag.mimeData: ({ "text/x-avora-doc": row.doc.id || "", "text/plain": row.doc.id ? DocBus.link(row.doc) : "" })
    Drag.active: dragger.active && !row.trash

    HoverHandler {
        id: hover
        cursorShape: Qt.PointingHandCursor
    }
    TapHandler {
        id: tap
        onTapped: row.opened()
    }
    DragHandler {
        id: dragger
        target: null
        enabled: !row.trash
        onActiveChanged: if (active) row.grabToImage(r => row.Drag.imageSource = r.url, Qt.size(row.width / 2, row.height / 2))
    }

    Rectangle {
        id: tile
        x: 10
        y: (row.dense ? 50 : 58) / 2 - height / 2
        width: row.dense ? 30 : 36
        height: width
        radius: row.dense ? 9 : 10
        color: Theme.hover
        border.width: 1
        border.color: Theme.active
        Icon {
            anchors.centerIn: parent
            name: DocBus.icon(row.doc)
            size: row.dense ? 15 : 17
            weight: 1.5
            color: DocBus.tone(row.doc)
        }
        Rectangle {
            visible: !!row.doc.pinned
            anchors { right: parent.right; top: parent.top; margins: -3 }
            width: 13
            height: 13
            radius: Theme.radiusSm
            color: Theme.card
            border.width: 1
            border.color: Theme.lineStrong
            Icon {
                anchors.centerIn: parent
                name: "pin"
                size: 9
                weight: 2
                color: Theme.warn
            }
        }
    }
    Column {
        id: texts
        anchors { left: tile.right; leftMargin: 12; right: tools.visible ? tools.left : parent.right; rightMargin: 12 }
        y: (row.dense ? 50 : 58) / 2 - (title.implicitHeight + 3 + sub.implicitHeight) / 2
        spacing: 3
        Text {
            id: title
            width: parent.width
            text: row.doc.title || ""
            color: Theme.white
            font.pixelSize: row.dense ? Theme.fontBody : Theme.fontLarge
            font.weight: Font.Medium
            elide: Text.ElideRight
        }
        Text {
            id: sub
            width: parent.width
            textFormat: Text.StyledText
            text: {
                const d = row.doc;
                const parts = [DocBus.kindLabel(d) + (d.kind === "file" ? "" : " · v" + d.version)];
                if (d.folder)
                    parts.push(U.esc(d.folder));
                if (row.trash)
                    parts.push("apagado " + U.ago(d.deleted_at));
                else
                    parts.push("<font color='" + row.whoTint + "'>" + U.esc(row.who) + "</font> " + U.ago(d.updated_at));
                return parts.join("  ·  ");
            }
            color: Theme.text3
            font.pixelSize: Theme.fontSmall
            elide: Text.ElideRight
        }
        Text {
            id: snippet
            visible: !!row.doc.snippet || (!row.dense && !!row.doc.summary)
            width: parent.width
            topPadding: 2
            text: row.doc.snippet || row.doc.summary || ""
            color: Theme.text3
            font.pixelSize: Theme.fontSmall
            wrapMode: Text.Wrap
            maximumLineCount: 2
            elide: Text.ElideRight
        }
    }
    Row {
        id: tools
        anchors { right: parent.right; rightMargin: 10; top: parent.top; topMargin: ((row.dense ? 50 : 58) - 24) / 2 }
        spacing: 2
        visible: hover.hovered && !row.dense
        RowTool {
            visible: !row.trash
            icon: "pin"
            tip: row.doc.pinned ? "Desafixar" : "Fixar: todo agente vê"
            onHit: row.pin()
        }
        RowTool {
            visible: !row.trash
            icon: "quote"
            tip: "Citar no chat"
            onHit: DocBus.cite(row.doc)
        }
        RowTool {
            visible: !row.trash
            icon: "trash"
            tip: "Mover para a lixeira"
            onHit: row.remove()
        }
        RowTool {
            visible: row.trash
            icon: "restore"
            tip: "Restaurar"
            onHit: row.restore()
        }
        RowTool {
            visible: row.trash
            icon: "trash"
            danger: true
            tip: "Apagar para sempre"
            onHit: row.purge()
        }
    }
}
