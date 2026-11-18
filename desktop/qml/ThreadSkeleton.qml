import QtQuick
import Avora

// A conversation on its way: the shapes of a few messages (theirs with an
// avatar on the left, yours on the right) with a soft light sweeping across.
// It fades in only if loading takes a moment, so a quick switch never
// flashes it.
Item {
    id: sk
    property bool active: false
    property real column: width
    property color tint: Theme.text2

    readonly property bool shown: active && late
    property bool late: false
    onActiveChanged: {
        late = false;
        if (active)
            wait.restart();
    }
    Timer {
        id: wait
        interval: 140
        onTriggered: sk.late = sk.active
    }

    visible: opacity > 0
    opacity: shown ? 1 : 0
    Behavior on opacity { NumberAnimation { duration: sk.shown ? Theme.medium : 120; easing.type: Easing.OutCubic } }

    // The light: one band moving left to right, shared by every shape.
    property real sweep: 0
    NumberAnimation on sweep {
        running: sk.visible
        from: -0.4
        to: 1.4
        duration: 1300
        loops: Animation.Infinite
        easing.type: Easing.InOutSine
    }

    readonly property var rows: [
        { mine: false, lines: [0.62, 0.48, 0.30] },
        { mine: true, lines: [0.34] },
        { mine: false, lines: [0.55, 0.40] },
        { mine: true, lines: [0.46, 0.22] },
        { mine: false, lines: [0.66, 0.58, 0.36] }
    ]

    component Shape: Rectangle {
        id: shape
        color: Theme.hover
        clip: true
        Rectangle {
            width: 160
            height: shape.height
            x: sk.sweep * sk.width - shape.mapToItem(sk, 0, 0).x - width / 2
            gradient: Gradient {
                orientation: Gradient.Horizontal
                GradientStop { position: 0; color: "transparent" }
                GradientStop { position: 0.5; color: Theme.active }
                GradientStop { position: 1; color: "transparent" }
            }
        }
    }

    Column {
        anchors { bottom: parent.bottom; bottomMargin: 18; horizontalCenter: parent.horizontalCenter }
        width: sk.column
        spacing: 26
        Repeater {
            model: sk.rows
            delegate: Item {
                id: row
                required property var modelData
                required property int index
                width: parent.width
                height: bubble.height
                // The rows rise in softly as the skeleton appears.
                opacity: sk.shown ? 1 : 0
                Behavior on opacity { NumberAnimation { duration: 260; easing.type: Easing.OutCubic } }
                transform: Translate {
                    y: sk.shown ? 0 : 8
                    Behavior on y { NumberAnimation { duration: 320; easing.type: Easing.OutCubic } }
                }

                Shape {
                    visible: !row.modelData.mine
                    width: 36
                    height: 36
                    radius: Theme.radius
                    color: Qt.rgba(sk.tint.r, sk.tint.g, sk.tint.b, 0.14)
                }
                Shape {
                    id: bubble
                    x: row.modelData.mine ? row.width - width : 52
                    width: Math.min(row.width * (row.modelData.mine ? 0.42 : 0.62), 560)
                    height: lines.implicitHeight + 28
                    radius: Theme.radiusLg
                    Column {
                        id: lines
                        anchors { left: parent.left; right: parent.right; top: parent.top; margins: 14 }
                        spacing: 9
                        Repeater {
                            model: row.modelData.lines
                            delegate: Rectangle {
                                required property real modelData
                                width: lines.width * Math.min(1, modelData / 0.66)
                                height: 9
                                radius: 4.5
                                color: Theme.active
                            }
                        }
                    }
                }
            }
        }
    }
}
