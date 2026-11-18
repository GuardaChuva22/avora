import QtQuick

// The small caps label over a section or a field ("SERVIDOR", "MODELO").
// One style for the whole app: write the text in any case, it shows in caps.
Text {
    color: Theme.text4
    font.pixelSize: Theme.fontCaption
    font.weight: Font.DemiBold
    font.letterSpacing: Theme.trackCaps
    font.capitalization: Font.AllUppercase
}
