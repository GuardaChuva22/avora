import QtQuick

// Page title block: eyebrow, large title, one line of context.
Column {
    property string eyebrow
    property string title
    property string sub
    spacing: 8

    Text {
        text: parent.eyebrow.toUpperCase()
        color: Theme.text4
        font.pixelSize: Theme.fontCaption
        font.weight: Font.DemiBold
        font.letterSpacing: Theme.trackCaps
    }
    Text {
        text: parent.title
        color: Theme.white
        font.family: Theme.display
        font.pixelSize: Theme.fontHero
        font.weight: Font.DemiBold
        font.letterSpacing: -0.8
    }
    Text {
        visible: text !== ""
        text: parent.sub
        color: Theme.text3
        font.pixelSize: Theme.fontLarge
        width: Math.min(implicitWidth, 640)
        wrapMode: Text.WordWrap
    }
}
