import QtQuick
import QtQuick.Effects

// Dark tooltip that slides out to the right of its parent after a short hover.
Item {
    id: tip
    property string text
    property bool show: false
    property real gap: 12

    x: parent.width + gap
    anchors.verticalCenter: parent.verticalCenter
    width: box.width
    height: box.height
    z: 100
    visible: opacity > 0
    opacity: armed ? 1 : 0
    Behavior on opacity { NumberAnimation { duration: Theme.fast } }

    property bool armed: false
    onShowChanged: {
        if (show)
            delay.restart();
        else {
            delay.stop();
            armed = false;
        }
    }
    Timer {
        id: delay
        interval: 350
        onTriggered: tip.armed = true
    }

    transform: Translate {
        x: tip.armed ? 0 : -4
        Behavior on x { NumberAnimation { duration: Theme.medium; easing.type: Theme.ease } }
    }

    RectangularShadow {
        anchors.fill: box
        radius: box.radius
        offset.y: 6
        blur: 20
        color: Theme.scrim
    }
    Rectangle {
        id: box
        width: label.implicitWidth + 22
        height: 30
        radius: Theme.radiusSm
        color: "#16161a"
        border.width: 1
        border.color: Theme.lineStrong
        Text {
            id: label
            anchors.centerIn: parent
            text: tip.text
            color: Theme.text
            font.pixelSize: Theme.fontSmall
            font.weight: Font.Medium
        }
    }
}
