import QtQuick
import QtQuick.Controls.Basic
import Avora
import "md.js" as Md

// An agent's text, drawn block by block with the app's own pieces: tables
// as cards, code with its language and a copy button, callouts, task
// lists, headings. While an answer streams only the blocks that changed
// are redrawn, so a long answer does not flicker or slow down.
Column {
    id: view
    property string text: ""
    property color tint: Theme.text2
    property int fontSize: 15
    property color color: Theme.text
    signal linkActivated(string link)

    // Team members, [{name, color}]: "@Name" becomes a chip in their color.
    property var mentions: []
    // The width the text wants when it is only paragraphs, so a short
    // answer gets a bubble its size; anything else wants the full width.
    readonly property real naturalWidth: measure.plain ? Math.ceil(measure.implicitWidth) + 2 : Infinity

    spacing: 10

    readonly property var theme: ({ link: "#a9b8ff", codeBg: "#25252c", codeFg: "#e9ddff", codeSize: fontSize - 2, mentions: mentions,
                                          tint: String(tint), size: fontSize, quiet: String(Theme.text3), head: String(Theme.white) })

    ListModel { id: blocks }

    Text {
        id: measure
        property bool plain: false
        visible: false
        textFormat: Text.RichText
        font.pixelSize: view.fontSize
    }

    // Keep the model in step with the text: same kind in place, rest rebuilt.
    function sync() {
        const parsed = Md.parse(text, theme);
        measure.plain = parsed.length > 0 && parsed.every(b => b.k === "p");
        measure.text = measure.plain ? parsed.map(b => b.html).join("<br>") : "";
        const list = Md.flow(parsed, theme);
        for (let i = 0; i < list.length; i++) {
            const js = JSON.stringify(list[i]);
            if (i < blocks.count && blocks.get(i).k === list[i].k) {
                if (blocks.get(i).json !== js)
                    blocks.setProperty(i, "json", js);
            } else if (i < blocks.count) {
                blocks.set(i, { k: list[i].k, json: js });
            } else
                blocks.append({ k: list[i].k, json: js });
        }
        if (blocks.count > list.length)
            blocks.remove(list.length, blocks.count - list.length);
    }
    onTextChanged: sync()
    onMentionsChanged: sync()
    Component.onCompleted: sync()

    Repeater {
        model: blocks
        delegate: Loader {
            id: slot
            required property string k
            required property string json
            readonly property var b: JSON.parse(json)
            width: view.width
            sourceComponent: k === "flow" ? flowText : k === "code" ? code
                           : k === "table" ? table : k === "quote" ? quote : k === "hr" ? rule : null
        }
    }

    // ── Pieces ──────────────────────────────────────────────────

    // Paragraphs, headings and lists in one text, so a selection runs
    // through them all (md.js flow).
    Component {
        id: flowText
        MdText {
            html: parent ? parent.b.html : ""
            size: view.fontSize
            color: view.color
            onLinkActivated: link => view.linkActivated(link)
        }
    }

    Component {
        id: rule
        Item {
            width: parent ? parent.width : 0
            height: 17
            Rectangle {
                anchors.verticalCenter: parent.verticalCenter
                width: parent.width
                height: 1
                color: Theme.line
            }
        }
    }

    Component {
        id: quote
        Rectangle {
            id: q
            readonly property var b: parent ? parent.b : ({ kind: "", html: "" })
            readonly property var look: ({
                nota: { c: "#8fb4ff", icon: "alert", t: "Nota" },
                dica: { c: "#86e3bd", icon: "spark", t: "Dica" },
                importante: { c: "#c9b8ff", icon: "bookmark", t: "Importante" },
                aviso: { c: "#f2cd82", icon: "alert", t: "Atenção" },
                perigo: { c: "#ff8a8a", icon: "alert", t: "Cuidado" }
            })[b.kind] || null
            width: parent ? parent.width : 0
            height: qcol.implicitHeight + (look ? 24 : 8)
            radius: look ? 12 : 0
            color: look ? Qt.rgba(Qt.color(look.c).r, Qt.color(look.c).g, Qt.color(look.c).b, 0.07) : "transparent"
            border.width: look ? 1 : 0
            border.color: look ? Qt.rgba(Qt.color(look.c).r, Qt.color(look.c).g, Qt.color(look.c).b, 0.25) : "transparent"
            Rectangle {
                visible: !q.look
                width: 3
                height: parent.height
                radius: 1.5
                color: Theme.wash5
            }
            Column {
                id: qcol
                x: q.look ? 14 : 16
                y: q.look ? 12 : 4
                width: parent.width - x - (q.look ? 14 : 0)
                spacing: 4
                Row {
                    visible: q.look !== null
                    spacing: 7
                    Icon {
                        anchors.verticalCenter: parent.verticalCenter
                        name: q.look ? q.look.icon : "alert"
                        size: 14
                        color: q.look ? q.look.c : Theme.text2
                    }
                    Text {
                        text: q.look ? q.look.t : ""
                        color: q.look ? q.look.c : Theme.text2
                        font.pixelSize: view.fontSize - 2
                        font.weight: Font.DemiBold
                    }
                }
                MdText {
                    width: qcol.width
                    html: q.b.html
                    size: view.fontSize
                    color: q.look ? view.color : Theme.text2
                    onLinkActivated: link => view.linkActivated(link)
                }
            }
        }
    }

    Component {
        id: code
        Rectangle {
            id: cb
            readonly property var b: parent ? parent.b : ({ lang: "", code: "" })
            property bool copied: false
            width: parent ? parent.width : 0
            height: 34 + codeFlick.height
            radius: Theme.radiusMd
            color: "#0d0d11"
            border.width: 1
            border.color: Theme.wash4
            clip: true
            Rectangle {
                width: parent.width
                height: 34
                radius: Theme.radiusMd
                color: Theme.wash1
                Rectangle { anchors.bottom: parent.bottom; width: parent.width; height: 1; color: Theme.active }
                Text {
                    x: 14
                    anchors.verticalCenter: parent.verticalCenter
                    text: cb.b.lang || "código"
                    color: Theme.text3
                    font.family: Theme.mono
                    font.pixelSize: Theme.fontSmall
                }
                Row {
                    anchors { right: parent.right; rightMargin: 10; verticalCenter: parent.verticalCenter }
                    spacing: 5
                    opacity: copyArea.containsMouse || cb.copied ? 1 : 0.7
                    Icon {
                        anchors.verticalCenter: parent.verticalCenter
                        name: cb.copied ? "check" : "copy"
                        size: 13
                        color: cb.copied ? Theme.ok : Theme.text2
                    }
                    Text {
                        anchors.verticalCenter: parent.verticalCenter
                        text: cb.copied ? "Copiado" : "Copiar"
                        color: cb.copied ? Theme.ok : Theme.text2
                        font.pixelSize: Theme.fontSmall
                    }
                    MouseArea {
                        id: copyArea
                        width: 70
                        height: 24
                        x: -24
                        y: -4
                        hoverEnabled: true
                        cursorShape: Qt.PointingHandCursor
                        onClicked: {
                            Api.copyText(cb.b.code);
                            cb.copied = true;
                            copiedTimer.restart();
                        }
                    }
                }
                Timer {
                    id: copiedTimer
                    interval: 1600
                    onTriggered: cb.copied = false
                }
            }
            Flickable {
                id: codeFlick
                y: 34
                width: parent.width
                height: codeText.implicitHeight + 24
                contentWidth: Math.max(width, codeText.implicitWidth + 28)
                contentHeight: height
                clip: true
                boundsBehavior: Flickable.StopAtBounds
                flickableDirection: Flickable.HorizontalFlick
                interactive: contentWidth > width
                ScrollBar.horizontal: SlimScrollBar {}
                TextEdit {
                    id: codeText
                    x: 14
                    y: 12
                    text: cb.b.code
                    textFormat: TextEdit.PlainText
                    color: "#e6e6ee"
                    font.family: Theme.mono
                    font.pixelSize: view.fontSize - 2
                    readOnly: true
                    selectByMouse: true
                    selectionColor: Theme.wash6
                }
            }
        }
    }

    Component {
        id: table
        Rectangle {
            id: tb
            readonly property var b: parent ? parent.b : ({ head: [], rows: [], align: [], weight: [] })
            readonly property int cols: b.head.length
            // Column widths follow how much each column says (square root,
            // so a long column doesn't starve the others) and fill the card
            // exactly; a column never goes under minCol, and only when even
            // that doesn't fit does the table scroll sideways.
            readonly property real minCol: 96
            readonly property var widths: {
                const w = b.weight.map(x => Math.sqrt(Math.min(Math.max(x, 4), 400)));
                const mins = w.map((x, i) => Math.min(240, Math.max(minCol, ((b.word || [])[i] || 0) * view.fontSize * 0.64 + 30)));
                const room = Math.max(width - 2, mins.reduce((a, x) => a + x, 0));
                const out = w.map(() => 0);
                let free = w.map((x, i) => i), left = room;
                // Water filling: columns whose share is under their minimum
                // get the minimum, the rest share what is left.
                for (let pass = 0; pass < cols; pass++) {
                    const sum = free.reduce((a, i) => a + w[i], 0) || 1;
                    const small = free.filter(i => left * w[i] / sum < mins[i]);
                    if (small.length === 0) {
                        free.forEach(i => out[i] = left * w[i] / sum);
                        break;
                    }
                    small.forEach(i => { out[i] = mins[i]; left -= mins[i]; });
                    free = free.filter(i => small.indexOf(i) < 0);
                    if (free.length === 0)
                        break;
                }
                return out;
            }
            readonly property real fullWidth: widths.reduce((a, x) => a + x, 0)
            property bool copied: false

            width: parent ? parent.width : 0
            height: flick.height + 2
            radius: Theme.radiusMd
            color: Theme.wash1
            border.width: 1
            border.color: Theme.wash4
            clip: true

            Flickable {
                id: flick
                x: 1
                y: 1
                width: parent.width - 2
                height: grid.height
                contentWidth: tb.fullWidth
                contentHeight: grid.height
                clip: true
                boundsBehavior: Flickable.StopAtBounds
                flickableDirection: Flickable.HorizontalFlick
                interactive: contentWidth > width + 1
                ScrollBar.horizontal: SlimScrollBar {}

                Column {
                    id: grid
                    width: tb.fullWidth
                    // Header.
                    Rectangle {
                        width: parent.width
                        height: headRow.height
                        color: Theme.hover
                        Row {
                            id: headRow
                            Repeater {
                                model: tb.b.head
                                delegate: Item {
                                    required property var modelData
                                    required property int index
                                    width: tb.widths[index]
                                    height: Math.max(hText.implicitHeight + 20, 38)
                                    Text {
                                        id: hText
                                        x: 14
                                        width: parent.width - 28
                                        anchors.verticalCenter: parent.verticalCenter
                                        text: modelData
                                        textFormat: Text.RichText
                                        horizontalAlignment: tb.b.align[index] === "right" ? Text.AlignRight : tb.b.align[index] === "center" ? Text.AlignHCenter : Text.AlignLeft
                                        color: Theme.text2
                                        font.pixelSize: view.fontSize - 3
                                        font.weight: Font.DemiBold
                                        font.letterSpacing: 0.3
                                        wrapMode: Text.Wrap
                                    }
                                }
                            }
                        }
                        Rectangle { anchors.bottom: parent.bottom; width: parent.width; height: 1; color: Theme.wash4 }
                    }
                    // Rows.
                    Repeater {
                        model: tb.b.rows
                        delegate: Rectangle {
                            id: row
                            required property var modelData
                            required property int index
                            width: grid.width
                            height: cellsRow.height
                            color: rowHover.hovered ? Theme.hover : index % 2 === 1 ? Theme.wash1 : "transparent"
                            HoverHandler { id: rowHover }
                            Row {
                                id: cellsRow
                                Repeater {
                                    model: row.modelData
                                    delegate: Item {
                                        required property var modelData
                                        required property int index
                                        width: tb.widths[index]
                                        height: cText.implicitHeight + 20
                                        Text {
                                            id: cText
                                            x: 14
                                            y: 10
                                            width: parent.width - 28
                                            text: "<div style=\"line-height:128%\">" + modelData + "</div>"
                                            textFormat: Text.RichText
                                            horizontalAlignment: tb.b.align[index] === "right" ? Text.AlignRight : tb.b.align[index] === "center" ? Text.AlignHCenter : Text.AlignLeft
                                            color: index === 0 ? Theme.white : view.color
                                            font.pixelSize: view.fontSize - 2
                                            wrapMode: Text.Wrap
                                            onLinkActivated: link => view.linkActivated(link)
                                            HoverHandler {
                                                cursorShape: cText.hoveredLink !== "" ? Qt.PointingHandCursor : Qt.ArrowCursor
                                            }
                                        }
                                    }
                                }
                            }
                            Rectangle {
                                visible: row.index < tb.b.rows.length - 1
                                anchors.bottom: parent.bottom
                                width: parent.width
                                height: 1
                                color: Theme.active
                            }
                        }
                    }
                }
            }

            // Copy the table as tab-separated text (pastes into a sheet).
            Rectangle {
                anchors { top: parent.top; right: parent.right; margins: 6 }
                width: 28
                height: 28
                radius: Theme.radiusSm
                color: tCopy.containsMouse ? Theme.wash4 : Qt.rgba(0.05, 0.05, 0.07, 0.85)
                opacity: tHover.hovered || tb.copied ? 1 : 0
                Behavior on opacity { NumberAnimation { duration: 140 } }
                Icon {
                    anchors.centerIn: parent
                    name: tb.copied ? "check" : "copy"
                    size: 13
                    color: tb.copied ? Theme.ok : Theme.text2
                }
                MouseArea {
                    id: tCopy
                    anchors.fill: parent
                    hoverEnabled: true
                    cursorShape: Qt.PointingHandCursor
                    onClicked: {
                        Api.copyText(Md.tableText(tb.b));
                        tb.copied = true;
                        tCopied.restart();
                    }
                }
                ToolTip.visible: tCopy.containsMouse
                ToolTip.text: "Copiar tabela (cola numa planilha)"
                ToolTip.delay: 400
                Timer {
                    id: tCopied
                    interval: 1600
                    onTriggered: tb.copied = false
                }
            }
            HoverHandler { id: tHover }
        }
    }
}
