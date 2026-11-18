import QtQuick

// Small monospace tag for metadata (model, engine, timings).
Rectangle {
    property alias text: label.text
    property bool bright: false
    implicitHeight: 24
    implicitWidth: label.implicitWidth + 18
    radius: Theme.radiusSm
    color: Qt.rgba(1, 1, 1, bright ? 0.07 : 0.03)
    border.width: 1
    border.color: Theme.line
    Text {
        id: label
        anchors.centerIn: parent
        font.family: Theme.mono
        font.pixelSize: Theme.fontSmall
        color: parent.bright ? Theme.text : Theme.text2
    }
}
