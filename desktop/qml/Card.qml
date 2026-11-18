import QtQuick
import QtQuick.Effects

// Glass card: one flat dark surface and a hairline border. "glow" marks the
// selected/hovered card with a lit top edge and a soft white aura; at rest a
// card gives off no light.
Item {
    id: card
    default property alias content: inner.data
    property real radius: Theme.radius
    property real padding: 22
    property bool glow: false
    property real glowStrength: Theme.glow
    property color topColor: Theme.card
    property color bottomColor: Theme.card // unused; the surface is flat
    readonly property alias inner: inner

    RectangularShadow {
        anchors.fill: face
        radius: card.radius
        blur: 22
        visible: color.a > 0
        color: Qt.rgba(1, 1, 1, card.glow ? card.glowStrength : 0)
        Behavior on color { ColorAnimation { duration: Theme.medium } }
    }
    Rectangle {
        id: face
        anchors.fill: parent
        radius: card.radius
        color: card.topColor
        border.width: 1
        border.color: card.glow ? Theme.lineStrong : Theme.line
        Behavior on color { ColorAnimation { duration: Theme.fast } }
        Behavior on border.color { ColorAnimation { duration: Theme.fast } }
    }
    // Lit top edge, only on the glowing card.
    Rectangle {
        x: card.radius * 0.8
        width: parent.width - card.radius * 1.6
        height: 1
        opacity: card.glow ? 1 : 0
        Behavior on opacity { NumberAnimation { duration: Theme.medium } }
        gradient: Gradient {
            orientation: Gradient.Horizontal
            GradientStop { position: 0; color: "transparent" }
            GradientStop { position: 0.5; color: Qt.rgba(1, 1, 1, 0.32) }
            GradientStop { position: 1; color: "transparent" }
        }
    }
    Item {
        id: inner
        anchors.fill: parent
        anchors.margins: card.padding
    }
}
