import QtQuick

// Avora's presence: a chrome sphere that breathes. "thinking" speeds the
// light up and lifts the halo.
ShaderEffect {
    id: orb
    property bool thinking: false
    property real time: 0
    property real level: thinking ? 1 : 0
    Behavior on level { NumberAnimation { duration: 900; easing.type: Easing.InOutCubic } }

    implicitWidth: 120
    implicitHeight: 120
    fragmentShader: "qrc:/shaders/orb.frag.qsb"

    NumberAnimation on time {
        from: 0
        to: 6283.18
        duration: 6283180 // one unit per second
        loops: Animation.Infinite
        running: orb.visible
    }
}
