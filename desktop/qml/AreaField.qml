import QtQuick
import QtQuick.Controls.Basic
import QtQuick.Effects

// Multi-line input that grows with its text up to maxHeight, then scrolls.
Item {
    id: root
    property alias text: area.text
    property alias placeholderText: area.placeholderText
    property alias area: area
    property real minHeight: 46
    property real maxHeight: 220
    property real rightInset: 0
    property bool framed: true
    property bool enterSubmits: true
    // rich: the text can hold pictures (the chat's @ chips); text is HTML then,
    // area.getText() the plain words.
    property bool rich: false
    // pasteFilter() can take a paste first (an image); true means it did.
    property var pasteFilter: null
    // keyFilter(event) can take Enter, Tab, Up, Down and Esc first (a
    // suggestion popup); it returns true when it handled the key.
    property var keyFilter: null
    signal submitted

    function filtered(event) {
        if (keyFilter && keyFilter(event))
            return;
        event.accepted = false;
    }

    function handleEnter(event) {
        if (keyFilter && keyFilter(event))
            return;
        const ctrl = event.modifiers & Qt.ControlModifier;
        const shift = event.modifiers & Qt.ShiftModifier;
        if ((enterSubmits && !shift) || (!enterSubmits && ctrl))
            submitted();
        else
            event.accepted = false;
    }

    implicitHeight: Math.max(minHeight, Math.min(maxHeight, area.implicitHeight))

    HoverHandler {
        id: fieldHover
    }
    RectangularShadow {
        visible: root.framed && color.a > 0
        anchors.fill: well
        radius: well.radius
        blur: 14
        color: Qt.rgba(1, 1, 1, area.activeFocus ? 0.05 : 0)
        Behavior on color { ColorAnimation { duration: Theme.medium } }
    }
    Rectangle {
        id: well
        visible: root.framed
        anchors.fill: parent
        radius: Theme.radiusLg
        color: Theme.well
        border.width: 1
        border.color: area.activeFocus ? Theme.focusLine : fieldHover.hovered ? Theme.lineStrong : Theme.line
        Behavior on border.color { ColorAnimation { duration: Theme.fast } }
    }
    FontMetrics {
        id: fm
        font: area.font
    }
    ScrollView {
        id: scroll
        anchors.fill: parent
        anchors.rightMargin: root.rightInset
        clip: true
        ScrollBar.vertical: SlimScrollBar {}
        TextArea {
            id: area
            wrapMode: TextEdit.Wrap
            textFormat: root.rich ? TextEdit.RichText : TextEdit.PlainText
            color: Theme.text
            placeholderTextColor: Theme.text4
            selectionColor: Theme.wash6
            selectedTextColor: Theme.white
            font.pixelSize: Theme.fontLarge
            padding: 14
            // One line sits centered in the box; more lines grow it.
            topPadding: root.enterSubmits ? Math.max(12, (root.minHeight - fm.height) / 2) : 13
            bottomPadding: topPadding
            background: null
            // Chat: Enter sends, Shift+Enter breaks the line.
            // Long prompts (enterSubmits false): Ctrl+Enter sends.
            Keys.onReturnPressed: event => root.handleEnter(event)
            Keys.onEnterPressed: event => root.handleEnter(event)
            Keys.onTabPressed: event => root.filtered(event)
            Keys.onUpPressed: event => root.filtered(event)
            Keys.onDownPressed: event => root.filtered(event)
            Keys.onEscapePressed: event => root.filtered(event)
            Keys.onPressed: event => {
                if (event.matches(StandardKey.Paste) && root.pasteFilter && root.pasteFilter())
                    event.accepted = true;
            }
        }
    }
}
