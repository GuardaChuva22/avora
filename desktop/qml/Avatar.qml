import QtQuick
import QtQuick.Shapes
import QtQuick.Effects

// An agent's face: its initial on a disc tinted with the agent's color. While
// the agent works, a bright arc spins around it.
Item {
    id: av
    property string name: "?"
    property color tint: Theme.text2
    property real size: 40
    property bool working: false
    property string photo: "" // image://avatar/… (U.photo)

    implicitWidth: size
    implicitHeight: size

    // Colored aura, only while the agent works.
    RectangularShadow {
        anchors.fill: disc
        radius: disc.radius
        blur: av.size * 0.28
        visible: color.a > 0
        color: Qt.rgba(av.tint.r, av.tint.g, av.tint.b, av.working ? 0.30 : 0)
        Behavior on color { ColorAnimation { duration: Theme.slow } }
    }
    Rectangle {
        id: disc
        anchors.fill: parent
        anchors.margins: av.size * 0.07
        radius: width / 2
        gradient: Gradient {
            GradientStop { position: 0; color: Qt.rgba(av.tint.r, av.tint.g, av.tint.b, 0.30) }
            GradientStop { position: 1; color: Qt.rgba(av.tint.r * 0.25, av.tint.g * 0.25, av.tint.b * 0.25, 0.55) }
        }
        border.width: 1
        border.color: Qt.rgba(av.tint.r, av.tint.g, av.tint.b, 0.45)
        // The agent's photo, already round, over the initial.
        Image {
            id: pic
            anchors.fill: parent
            anchors.margins: 1
            visible: av.photo !== "" && status === Image.Ready
            source: av.photo
            sourceSize: Qt.size(Math.ceil(width * Screen.devicePixelRatio), Math.ceil(height * Screen.devicePixelRatio))
            asynchronous: true
            cache: true
            smooth: true
            mipmap: true
        }
        Text {
            visible: !pic.visible
            anchors.centerIn: parent
            anchors.verticalCenterOffset: av.size * 0.01
            text: (av.name || "?").charAt(0).toUpperCase()
            color: Qt.lighter(av.tint, 1.25)
            font.family: Theme.display
            font.pixelSize: Math.max(10, Math.round(av.size * 0.42))
            font.weight: Font.DemiBold
        }
    }
    // Working ring.
    Shape {
        anchors.fill: parent
        visible: av.working
        preferredRendererType: Shape.CurveRenderer
        ShapePath {
            strokeColor: av.tint
            strokeWidth: Math.max(1.6, av.size * 0.045)
            fillColor: "transparent"
            capStyle: ShapePath.RoundCap
            PathAngleArc {
                centerX: av.size / 2
                centerY: av.size / 2
                radiusX: av.size / 2 - av.size * 0.03
                radiusY: radiusX
                startAngle: 0
                sweepAngle: 110
            }
        }
        RotationAnimation on rotation {
            running: av.working && av.visible
            from: 0
            to: 360
            duration: 1300
            loops: Animation.Infinite
        }
    }
}
