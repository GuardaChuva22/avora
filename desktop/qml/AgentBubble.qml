import QtQuick
import Avora

// An agent's message as a chat bubble: the name on top in its color, then
// what it said. A short answer gets a bubble its own size (natural is the
// width its text wants); anything with cards or tables takes the full
// width. The corner by the avatar is tucked in, like a speech bubble.
Rectangle {
    id: bub
    property string name: ""
    property string sub: ""
    property color tint: Theme.text2
    property bool head: true
    property real maxWidth: 600
    property real natural: Infinity
    property real spacing: 10
    default property alias content: inner.data

    width: Math.min(maxWidth, Math.max(head ? headRow.implicitWidth + 32 : 0, natural + 34, 64))
    height: inner.y + inner.implicitHeight + 13
    radius: Theme.radius
    topLeftRadius: head ? 5 : 16
    color: Theme.hover
    border.width: 1
    border.color: Theme.active

    Row {
        id: headRow
        visible: bub.head
        x: 16
        y: 10
        spacing: 8
        Text {
            text: bub.name
            color: Qt.lighter(bub.tint, 1.2)
            font.pixelSize: Theme.fontLarge
            font.weight: Font.DemiBold
        }
        Text {
            visible: text !== ""
            topPadding: 1.5
            text: bub.sub
            color: Theme.text4
            font.pixelSize: Theme.fontSmall
        }
    }

    Column {
        id: inner
        x: 16
        y: bub.head ? headRow.y + headRow.height + 6 : 12
        width: bub.width - 32
        spacing: bub.spacing
    }
}
