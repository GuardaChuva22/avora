import QtQuick
import QtQuick.Controls.Basic
import QtQuick.Effects
import Avora

// The images of a message (an image an agent drew, browser prints, photos
// of an ad...) as rounded thumbnails with a download button; a click opens
// the viewer: zoom with the wheel, drag to move, double-click to fit or see
// at full size, arrows between images, download and Esc to close.
Flow {
    id: gallery
    property var images: []
    spacing: 8
    visible: images.length > 0

    // Saved files, by image src: the button turns into a check.
    property var saved: ({})

    function download(image) {
        if (!image || !image.id)
            return;
        Api.saveFile(image.id, image.alt && image.alt !== "imagem gerada" ? image.alt : "imagem", (err, path) => {
            if (err) {
                toast.show("Não consegui salvar: " + err, "");
                return;
            }
            const m = Object.assign({}, saved);
            m[image.src] = path;
            saved = m;
            toast.show("Salva em Downloads › Avora", path);
        });
    }

    Repeater {
        model: gallery.images
        delegate: Item {
            id: thumb
            required property var modelData
            required property int index
            readonly property real ratio: img.status === Image.Ready && img.implicitHeight > 0 ? img.implicitWidth / img.implicitHeight : 1
            height: gallery.images.length === 1 ? Math.min(340, gallery.width / ratio) : 170
            width: Math.min(gallery.width, height * ratio)

            // The picture, cut to rounded corners.
            Image {
                id: img
                anchors.fill: parent
                source: thumb.modelData.src
                sourceSize.width: 1400
                fillMode: Image.PreserveAspectCrop
                asynchronous: true
                cache: true
                visible: false
            }
            Item {
                id: mask
                anchors.fill: parent
                visible: false
                layer.enabled: true
                Rectangle {
                    anchors.fill: parent
                    radius: Theme.radius
                }
            }
            Rectangle {
                anchors.fill: parent
                radius: Theme.radius
                color: Theme.card
                visible: img.status !== Image.Ready
                Text {
                    anchors.centerIn: parent
                    text: img.status === Image.Error ? "imagem indisponível" : "carregando…"
                    color: Theme.text4
                    font.pixelSize: Theme.fontSmall
                }
            }
            MultiEffect {
                anchors.fill: parent
                source: img
                maskEnabled: true
                maskSource: mask
                maskThresholdMin: 0.5
                maskSpreadAtMin: 1.0
                // A fresh image comes in softly, not in one jump.
                opacity: img.status === Image.Ready ? 1 : 0
                scale: img.status !== Image.Ready ? 1.03 : hover.hovered ? 1.008 : 1
                brightness: hover.hovered ? 0.04 : 0
                Behavior on opacity { NumberAnimation { duration: 420; easing.type: Easing.OutCubic } }
                Behavior on scale { NumberAnimation { duration: 380; easing.type: Easing.OutCubic } }
                Behavior on brightness { NumberAnimation { duration: 200 } }
            }
            Rectangle {
                anchors.fill: parent
                radius: Theme.radius
                color: "transparent"
                border.width: 1
                border.color: hover.hovered ? Theme.wash5 : Theme.active
                Behavior on border.color { ColorAnimation { duration: 200 } }
            }

            HoverHandler {
                id: hover
                cursorShape: Qt.PointingHandCursor
            }
            TapHandler {
                onTapped: viewer.show(thumb.index)
            }

            // Hover tools: open and download.
            Row {
                anchors { top: parent.top; right: parent.right; margins: 10 }
                spacing: 6
                opacity: hover.hovered && img.status === Image.Ready ? 1 : 0
                Behavior on opacity { NumberAnimation { duration: 160 } }
                GlassButton {
                    icon: "expand"
                    tip: "Abrir"
                    onHit: viewer.show(thumb.index)
                }
                GlassButton {
                    visible: !!thumb.modelData.id
                    icon: gallery.saved[thumb.modelData.src] ? "check" : "download"
                    tip: gallery.saved[thumb.modelData.src] ? "Mostrar na pasta" : "Baixar"
                    onHit: gallery.saved[thumb.modelData.src] ? Api.showInFolder(gallery.saved[thumb.modelData.src]) : gallery.download(thumb.modelData)
                }
            }
        }
    }

    // A small round button on glass, over an image.
    component GlassButton: Rectangle {
        id: gb
        property string icon
        property string tip
        signal hit
        width: 32
        height: 32
        radius: Theme.radius
        color: gbArea.containsMouse ? Qt.rgba(0, 0, 0, 0.72) : Qt.rgba(0, 0, 0, 0.5)
        border.width: 1
        border.color: Qt.rgba(1, 1, 1, gbArea.containsMouse ? 0.22 : 0.12)
        Behavior on color { ColorAnimation { duration: 120 } }
        Icon {
            anchors.centerIn: parent
            name: gb.icon
            size: 15
            color: Theme.white
        }
        MouseArea {
            id: gbArea
            anchors.fill: parent
            hoverEnabled: true
            cursorShape: Qt.PointingHandCursor
            onClicked: gb.hit()
        }
        ToolTip.visible: gbArea.containsMouse && gb.tip !== ""
        ToolTip.text: gb.tip
        ToolTip.delay: 400
    }

    // ── Viewer ──────────────────────────────────────────────────
    Popup {
        id: viewer
        property int at: 0
        readonly property var image: gallery.images[at] || ({})
        parent: Overlay.overlay
        x: 0
        y: 0
        width: parent ? parent.width : 800
        height: parent ? parent.height : 600
        modal: true
        padding: 0
        closePolicy: Popup.CloseOnEscape

        function show(i) {
            at = i;
            stage.fit();
            open();
        }
        function step(d) {
            if (gallery.images.length < 2)
                return;
            at = (at + d + gallery.images.length) % gallery.images.length;
            stage.fit();
        }

        background: Rectangle {
            color: Qt.rgba(0.02, 0.02, 0.03, 0.9)
        }
        enter: Transition {
            NumberAnimation { property: "opacity"; from: 0; to: 1; duration: 200; easing.type: Easing.OutCubic }
        }
        exit: Transition {
            NumberAnimation { property: "opacity"; from: 1; to: 0; duration: 140; easing.type: Easing.InQuad }
        }

        contentItem: Item {
            focus: true
            Keys.onLeftPressed: viewer.step(-1)
            Keys.onRightPressed: viewer.step(1)
            Keys.onPressed: e => {
                if (e.key === Qt.Key_0 || e.key === Qt.Key_F) {
                    stage.fit();
                    e.accepted = true;
                }
            }

            // Where the picture lives: wheel zooms at the cursor, drag moves.
            Item {
                id: stage
                anchors.fill: parent
                anchors.topMargin: 64
                anchors.bottomMargin: 56
                clip: true

                property real zoom: 1
                readonly property real fitScale: big.implicitWidth > 0 ? Math.min(width / big.implicitWidth, height / big.implicitHeight, 1) : 1
                function fit() {
                    zoom = 1;
                    big.x = Qt.binding(() => (stage.width - big.width) / 2);
                    big.y = Qt.binding(() => (stage.height - big.height) / 2);
                }
                function zoomAt(f, px, py) {
                    const old = zoom;
                    zoom = Math.max(1, Math.min(zoom * f, 8 / Math.max(fitScale, 0.01)));
                    const k = zoom / old;
                    big.x = px - (px - big.x) * k;
                    big.y = py - (py - big.y) * k;
                }

                MouseArea {
                    anchors.fill: parent
                    onClicked: viewer.close()
                }

                Image {
                    id: big
                    source: viewer.opened ? viewer.image.src || "" : ""
                    asynchronous: true
                    width: implicitWidth * stage.fitScale * stage.zoom
                    height: implicitHeight * stage.fitScale * stage.zoom
                    fillMode: Image.PreserveAspectFit
                    smooth: true
                    mipmap: true
                    visible: false
                    onStatusChanged: if (status === Image.Ready) stage.fit()
                }
                Item {
                    id: bigMask
                    width: big.width
                    height: big.height
                    visible: false
                    layer.enabled: true
                    Rectangle {
                        anchors.fill: parent
                        radius: stage.zoom > 1.01 ? 0 : 14
                    }
                }
                MultiEffect {
                    x: big.x
                    y: big.y
                    width: big.width
                    height: big.height
                    source: big
                    maskEnabled: true
                    maskSource: bigMask
                    maskThresholdMin: 0.5
                    maskSpreadAtMin: 1.0
                    opacity: big.status === Image.Ready ? 1 : 0
                    scale: viewer.opened && big.status === Image.Ready ? 1 : 0.97
                    Behavior on opacity { NumberAnimation { duration: 260; easing.type: Easing.OutCubic } }
                    Behavior on scale { NumberAnimation { duration: 320; easing.type: Easing.OutCubic } }
                    shadowEnabled: stage.zoom <= 1.01
                    shadowBlur: 1
                    shadowOpacity: 0.6
                    shadowVerticalOffset: 12

                    MouseArea {
                        anchors.fill: parent
                        cursorShape: stage.zoom > 1.01 ? (pressed ? Qt.ClosedHandCursor : Qt.OpenHandCursor) : Qt.ArrowCursor
                        drag.target: stage.zoom > 1.01 ? big : null
                        onDoubleClicked: m => {
                            if (stage.zoom > 1.01)
                                stage.fit();
                            else
                                stage.zoomAt(1 / stage.fitScale, m.x + big.x, m.y + big.y);
                        }
                        onWheel: w => {
                            stage.zoomAt(w.angleDelta.y > 0 ? 1.18 : 1 / 1.18, w.x + big.x, w.y + big.y);
                            w.accepted = true;
                        }
                    }
                }
                Text {
                    anchors.centerIn: parent
                    visible: big.status !== Image.Ready
                    text: big.status === Image.Error ? "imagem indisponível" : "carregando…"
                    color: Theme.text3
                    font.pixelSize: Theme.fontBody
                }
            }

            // Top bar: what it is, and the actions.
            Item {
                anchors { top: parent.top; left: parent.left; right: parent.right }
                height: 64
                Column {
                    anchors { left: parent.left; leftMargin: 24; verticalCenter: parent.verticalCenter }
                    spacing: 2
                    Text {
                        text: viewer.image.alt || "Imagem"
                        color: Theme.white
                        font.pixelSize: Theme.fontLarge
                        font.weight: Font.DemiBold
                    }
                    Text {
                        text: (big.implicitWidth > 0 ? big.implicitWidth + " × " + big.implicitHeight : "")
                              + (gallery.images.length > 1 ? "  ·  " + (viewer.at + 1) + " de " + gallery.images.length : "")
                              + "  ·  " + Math.round(stage.fitScale * stage.zoom * 100) + "%"
                        color: Theme.text3
                        font.pixelSize: Theme.fontSmall
                        font.features: { "tnum": 1 }
                    }
                }
                Row {
                    anchors { right: parent.right; rightMargin: 20; verticalCenter: parent.verticalCenter }
                    spacing: 8
                    GhostButton {
                        small: true
                        visible: !!viewer.image.web
                        icon: "globe"
                        text: "Abrir no navegador"
                        onClicked: Qt.openUrlExternally(viewer.image.web)
                    }
                    GhostButton {
                        small: true
                        visible: !!viewer.image.id
                        icon: gallery.saved[viewer.image.src] ? "folder" : "download"
                        text: gallery.saved[viewer.image.src] ? "Mostrar na pasta" : "Baixar"
                        onClicked: gallery.saved[viewer.image.src] ? Api.showInFolder(gallery.saved[viewer.image.src]) : gallery.download(viewer.image)
                    }
                    GhostButton {
                        small: true
                        round: true
                        icon: "close"
                        onClicked: viewer.close()
                    }
                }
            }

            // Between images.
            Repeater {
                model: gallery.images.length > 1 ? [-1, 1] : []
                delegate: GlassButton {
                    required property int modelData
                    anchors.verticalCenter: parent.verticalCenter
                    x: modelData < 0 ? 20 : parent.width - width - 20
                    width: 40
                    height: 40
                    radius: 20
                    icon: modelData < 0 ? "back" : "chevron"
                    onHit: viewer.step(modelData)
                }
            }

            Text {
                anchors { bottom: parent.bottom; bottomMargin: 20; horizontalCenter: parent.horizontalCenter }
                text: "roda do mouse aproxima  ·  arraste para mover  ·  dois cliques: tamanho real  ·  Esc fecha"
                color: Theme.text4
                font.pixelSize: Theme.fontSmall
            }
        }
    }

    // A short note at the bottom of the window after a download.
    Popup {
        id: toast
        property string path: ""
        property string text: ""
        function show(t, p) {
            text = t;
            path = p;
            open();
            toastTimer.restart();
        }
        parent: Overlay.overlay
        x: parent ? (parent.width - width) / 2 : 0
        y: parent ? parent.height - height - 28 : 0
        z: 1000
        padding: 0
        closePolicy: Popup.NoAutoClose
        background: Rectangle {
            radius: Theme.radius
            color: Theme.cardTop
            border.width: 1
            border.color: Theme.lineStrong
        }
        enter: Transition {
            ParallelAnimation {
                NumberAnimation { property: "opacity"; from: 0; to: 1; duration: 180 }
                NumberAnimation { property: "y"; from: (toast.parent ? toast.parent.height : 0) - toast.height - 12; to: (toast.parent ? toast.parent.height : 0) - toast.height - 28; duration: 220; easing.type: Easing.OutCubic }
            }
        }
        exit: Transition {
            NumberAnimation { property: "opacity"; from: 1; to: 0; duration: 160 }
        }
        contentItem: Row {
            spacing: 12
            leftPadding: 16
            rightPadding: toast.path ? 8 : 16
            topPadding: 8
            bottomPadding: 8
            Icon {
                anchors.verticalCenter: parent.verticalCenter
                name: toast.path ? "check" : "alert"
                size: 15
                color: toast.path ? Theme.ok : Theme.bad
            }
            Text {
                anchors.verticalCenter: parent.verticalCenter
                text: toast.text
                color: Theme.text
                font.pixelSize: Theme.fontBody
            }
            GhostButton {
                visible: toast.path !== ""
                small: true
                icon: "folder"
                text: "Mostrar"
                onClicked: {
                    Api.showInFolder(toast.path);
                    toast.close();
                }
            }
        }
        Timer {
            id: toastTimer
            interval: 3800
            onTriggered: toast.close()
        }
    }
}
