import QtQuick
import QtQuick.Shapes
import QtQuick.Effects
import "logos.js" as Logos

// A subscription's limits as two rings around its logo: the outer ring is the
// weekly window, the inner one the 5-hour window. Full means untouched; the
// arc empties clockwise as the quota is used.
Item {
    id: ring
    property string engine: "claude"
    property real weekly: -1 // remaining 0-100; -1 = unknown
    property real fiveHour: -1
    property bool limited: false
    property real size: 36

    implicitWidth: size
    implicitHeight: size

    readonly property real outerStroke: 2.6
    readonly property real innerStroke: 2.2
    readonly property real outerR: size / 2 - outerStroke / 2
    readonly property real innerR: outerR - outerStroke / 2 - 2.4 - innerStroke / 2

    function tint(left, base) {
        if (left < 0)
            return Theme.text4;
        if (left <= 0.5)
            return Theme.bad;
        if (left < 15)
            return Theme.warn;
        return base;
    }

    // Arcs start empty and fill up to their value on first show.
    property real shownWeekly: 0
    property real shownFive: 0
    Behavior on shownWeekly { NumberAnimation { duration: 900; easing.type: Theme.ease } }
    Behavior on shownFive { NumberAnimation { duration: 900; easing.type: Theme.ease } }
    Component.onCompleted: {
        shownWeekly = Qt.binding(() => Math.max(0, ring.weekly));
        shownFive = Qt.binding(() => Math.max(0, ring.fiveHour));
    }

    readonly property color weeklyColor: tint(weekly, Theme.white)
    readonly property color fiveColor: tint(fiveHour, Qt.rgba(1, 1, 1, 0.62))

    // Soft halo in the outer ring's color, only while there is quota left.
    RectangularShadow {
        anchors.centerIn: parent
        width: ring.size - 6
        height: width
        radius: width / 2
        blur: 14
        visible: false // flat at rest
        color: "transparent"
    }

    Shape {
        anchors.fill: parent
        preferredRendererType: Shape.CurveRenderer

        // Tracks.
        ShapePath {
            strokeColor: ring.weekly >= 0 && ring.weekly <= 0.5 ? Theme.badLine : Theme.active
            strokeWidth: ring.outerStroke
            fillColor: "transparent"
            PathAngleArc { centerX: ring.size / 2; centerY: ring.size / 2; radiusX: ring.outerR; radiusY: ring.outerR; startAngle: 0; sweepAngle: 360 }
        }
        ShapePath {
            strokeColor: ring.fiveHour >= 0 && ring.fiveHour <= 0.5 ? Theme.badLine : Theme.active
            strokeWidth: ring.innerStroke
            fillColor: "transparent"
            PathAngleArc { centerX: ring.size / 2; centerY: ring.size / 2; radiusX: ring.innerR; radiusY: ring.innerR; startAngle: 0; sweepAngle: 360 }
        }
        // What is left.
        ShapePath {
            strokeColor: ring.shownWeekly > 0.2 ? ring.weeklyColor : "transparent"
            strokeWidth: ring.outerStroke
            fillColor: "transparent"
            capStyle: ShapePath.RoundCap
            PathAngleArc { centerX: ring.size / 2; centerY: ring.size / 2; radiusX: ring.outerR; radiusY: ring.outerR; startAngle: -90; sweepAngle: 3.6 * ring.shownWeekly }
        }
        ShapePath {
            strokeColor: ring.shownFive > 0.2 ? ring.fiveColor : "transparent"
            strokeWidth: ring.innerStroke
            fillColor: "transparent"
            capStyle: ShapePath.RoundCap
            PathAngleArc { centerX: ring.size / 2; centerY: ring.size / 2; radiusX: ring.innerR; radiusY: ring.innerR; startAngle: -90; sweepAngle: 3.6 * ring.shownFive }
        }
    }

    // Logo on a dark disc.
    Rectangle {
        anchors.centerIn: parent
        width: Math.round((ring.innerR - ring.innerStroke / 2 - 1.2) * 2)
        height: width
        radius: width / 2
        color: "#0a0a0c"
        Icon {
            anchors.centerIn: parent
            name: ring.engine === "codex" ? "openai" : ring.engine === "antigravity" ? "" : "claude"
            path: ring.engine === "antigravity" ? Logos.of("antigravity") : ""
            size: Math.round(parent.width * 0.62)
            color: ring.engine === "codex" ? Theme.white : ring.engine === "antigravity" ? Logos.color("antigravity") : "#d97757"
            opacity: ring.weekly < 0 ? 0.45 : 1
        }
    }
}
