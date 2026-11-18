import QtQuick

// Full-window atmosphere: bloom and vignette from one static shader. Drawn
// directly: at rest the window is not redrawn, so it costs nothing, and a
// cached layer here made Qt Quick lose every cursor shape in the window.
ShaderEffect {
    property size size: Qt.size(width * Screen.devicePixelRatio, height * Screen.devicePixelRatio)
    fragmentShader: "qrc:/shaders/backdrop.frag.qsb"
}
