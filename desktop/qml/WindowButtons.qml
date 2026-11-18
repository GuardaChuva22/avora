import QtQuick

// Minimize, maximize/restore and close, drawn by the app (frameless window).
Row {
    id: buttons
    required property Window window
    readonly property bool maximized: window.visibility === Window.Maximized

    Repeater {
        model: [
            { name: "min", act: "min" },
            { name: buttons.maximized ? "restore" : "max", act: "max" },
            { name: "close", act: "close" }
        ]
        delegate: Rectangle {
            id: b
            required property var modelData
            width: 46
            height: Theme.titleBar
            color: area.containsMouse ? (b.modelData.act === "close" ? "#d83b3b" : Theme.active) : "transparent"
            Behavior on color { ColorAnimation { duration: Theme.fast } }
            Icon {
                anchors.centerIn: parent
                name: b.modelData.name
                size: 15
                weight: 1.3
                color: area.containsMouse ? Theme.white : Theme.text3
            }
            MouseArea {
                id: area
                anchors.fill: parent
                hoverEnabled: true
                onClicked: {
                    if (b.modelData.act === "min")
                        buttons.window.showMinimized();
                    else if (b.modelData.act === "max")
                        buttons.maximized ? buttons.window.showNormal() : buttons.window.showMaximized();
                    else
                        buttons.window.close();
                }
            }
        }
    }
}
