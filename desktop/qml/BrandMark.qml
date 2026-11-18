import QtQuick
import QtQuick.Shapes

// The Avora "A", lit from the top left.
Shape {
    id: mark
    property real size: 22
    width: size
    height: size
    preferredRendererType: Shape.CurveRenderer

    ShapePath {
        strokeWidth: -1
        scale: Qt.size(mark.size / 64, mark.size / 64)
        fillGradient: LinearGradient {
            x1: 0; y1: 0; x2: 64; y2: 64
            GradientStop { position: 0; color: "#ffffff" }
            GradientStop { position: 0.6; color: "#cfcfd6" }
            GradientStop { position: 1; color: "#7a7a84" }
        }
        PathSvg { path: "M32 6 50 56h-8.6l-4-11.4H26.6L22.6 56H14L32 6Zm-3.1 31.2h6.2L32 27.6l-3.1 9.6Z" }
    }
}
