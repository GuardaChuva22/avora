import QtQuick
import QtQuick.Effects

// Status light. "live" glows green; ok/bad are tinted. Static on purpose:
// any running animation makes Qt redraw the whole window every frame.
Item {
    id: dot
    property string kind: "" // live | ok | bad | warn | ""
    readonly property color tint: kind === "live" ? Theme.live : kind === "ok" ? Theme.ok : kind === "bad" ? Theme.bad : kind === "warn" ? Theme.warn : Theme.text4

    implicitWidth: 7
    implicitHeight: 7

    RectangularShadow {
        anchors.fill: core
        radius: core.radius
        blur: 8
        visible: dot.kind === "live"
        color: Qt.rgba(dot.tint.r, dot.tint.g, dot.tint.b, 0.55)
        opacity: core.opacity
    }
    Rectangle {
        id: core
        anchors.fill: parent
        radius: width / 2
        color: dot.tint
    }
}
