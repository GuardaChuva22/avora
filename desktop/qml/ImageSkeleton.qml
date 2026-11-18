import QtQuick
import QtQuick.Effects
import Avora

// The frame of an image being drawn: soft colors drifting under frosted
// glass, a light sweeping across, a grain of dots taking shape, and a
// label with the time so far. It stands where the image will appear.
Rectangle {
    id: sk
    property color tint: Theme.text2
    property real since: Date.now()
    readonly property bool running: visible && Qt.application.state === Qt.ApplicationActive
    property real now: Date.now()

    width: 300
    height: 300
    radius: Theme.radiusLg
    color: Theme.card
    border.width: 1
    border.color: Theme.wash4
    clip: true

    Timer {
        interval: 1000
        repeat: true
        running: sk.running
        onTriggered: sk.now = Date.now()
    }

    // Drifting color: three blobs, blurred into one moving glow.
    Item {
        id: blobs
        anchors.fill: parent
        visible: false
        layer.enabled: true
        Repeater {
            model: [
                { c: sk.tint, s: 0.75, dx: 0.30, dy: 0.25, t: 5200 },
                { c: Qt.lighter(sk.tint, 1.5), s: 0.6, dx: 0.65, dy: 0.6, t: 6900 },
                { c: Qt.rgba(0.55, 0.45, 1, 1), s: 0.55, dx: 0.4, dy: 0.75, t: 8300 }
            ]
            delegate: Rectangle {
                id: blob
                required property var modelData
                required property int index
                width: sk.width * modelData.s
                height: width
                radius: width / 2
                color: modelData.c
                opacity: 0.55
                x: sk.width * modelData.dx - width / 2
                y: sk.height * modelData.dy - height / 2
                SequentialAnimation on x {
                    running: sk.running
                    loops: Animation.Infinite
                    NumberAnimation { to: sk.width * (1 - blob.modelData.dx) - blob.width / 2; duration: blob.modelData.t; easing.type: Easing.InOutSine }
                    NumberAnimation { to: sk.width * blob.modelData.dx - blob.width / 2; duration: blob.modelData.t; easing.type: Easing.InOutSine }
                }
                SequentialAnimation on y {
                    running: sk.running
                    loops: Animation.Infinite
                    NumberAnimation { to: sk.height * (1 - blob.modelData.dy) - blob.height / 2; duration: blob.modelData.t * 1.3; easing.type: Easing.InOutSine }
                    NumberAnimation { to: sk.height * blob.modelData.dy - blob.height / 2; duration: blob.modelData.t * 1.3; easing.type: Easing.InOutSine }
                }
            }
        }
    }
    MultiEffect {
        anchors.fill: parent
        source: blobs
        blurEnabled: true
        blur: 1.0
        blurMax: 64
        opacity: 0.55
        SequentialAnimation on opacity {
            running: sk.running
            loops: Animation.Infinite
            NumberAnimation { to: 0.8; duration: 1800; easing.type: Easing.InOutSine }
            NumberAnimation { to: 0.45; duration: 1800; easing.type: Easing.InOutSine }
        }
    }

    // A grain of dots that light up here and there, like the image forming.
    Grid {
        id: grain
        anchors.centerIn: parent
        columns: 14
        spacing: (sk.width - 28 - 14 * 4) / 13
        Repeater {
            model: 14 * 14
            delegate: Rectangle {
                id: dot
                required property int index
                width: 4
                height: 4
                radius: 2
                color: Theme.white
                opacity: 0.05
                SequentialAnimation on opacity {
                    running: sk.running
                    loops: Animation.Infinite
                    PauseAnimation { duration: (dot.index * 977) % 2600 }
                    NumberAnimation { to: 0.32; duration: 500; easing.type: Easing.OutQuad }
                    NumberAnimation { to: 0.05; duration: 900; easing.type: Easing.InQuad }
                    PauseAnimation { duration: (dot.index * 613) % 1700 }
                }
            }
        }
    }

    // The light sweeping across, corner to corner.
    Rectangle {
        id: sweep
        width: sk.width * 0.55
        height: sk.height * 2
        y: -sk.height / 2
        rotation: 20
        opacity: 0.9
        gradient: Gradient {
            orientation: Gradient.Horizontal
            GradientStop { position: 0.0; color: Qt.rgba(1, 1, 1, 0) }
            GradientStop { position: 0.5; color: Theme.wash4 }
            GradientStop { position: 1.0; color: Qt.rgba(1, 1, 1, 0) }
        }
        NumberAnimation on x {
            running: sk.running
            loops: Animation.Infinite
            from: -sk.width * 0.8
            to: sk.width * 1.2
            duration: 1900
            easing.type: Easing.InOutCubic
        }
    }

    // What is happening and for how long.
    Rectangle {
        anchors { left: parent.left; bottom: parent.bottom; margins: 12 }
        width: label.implicitWidth + 34
        height: 28
        radius: Theme.radiusLg
        color: Theme.well
        border.width: 1
        border.color: Theme.wash4
        Icon {
            id: sparkle
            x: 10
            anchors.verticalCenter: parent.verticalCenter
            name: "spark"
            size: 13
            color: Qt.lighter(sk.tint, 1.3)
            RotationAnimation on rotation {
                running: sk.running
                loops: Animation.Infinite
                from: 0
                to: 360
                duration: 3600
            }
        }
        Text {
            id: label
            x: 28
            anchors.verticalCenter: parent.verticalCenter
            readonly property int secs: Math.max(0, Math.floor((sk.now - sk.since) / 1000))
            text: "Gerando imagem  ·  " + secs + " s"
            color: Theme.text
            font.pixelSize: Theme.fontSmall
            font.features: { "tnum": 1 }
        }
    }
}
