import QtQuick
import Avora

// One block of an agent's rich text (MarkdownView): selectable, with
// comfortable line height and links that show the hand cursor.
TextEdit {
    id: rich
    property string html
    property int size: 15
    width: parent ? parent.width : 0
    text: "<div style=\"line-height:145%\">" + html + "</div>"
    textFormat: TextEdit.RichText
    color: Theme.text
    font.pixelSize: size
    wrapMode: TextEdit.Wrap
    readOnly: true
    selectByMouse: true
    selectionColor: Theme.wash6
    HoverHandler {
        cursorShape: rich.hoveredLink !== "" ? Qt.PointingHandCursor : Qt.IBeamCursor
    }
}
