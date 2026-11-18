import QtQuick
import QtQuick.Controls.Basic
import Avora
import "util.js" as U

// How full the open conversation's context is ("23k"), and on click what
// fills it, the summary of what was compacted, and Compactar agora.
Rectangle {
    id: meter
    property string convId: ""
    property int refreshKey: 0
    property var view: null
    property string error: ""
    property bool showSummary: false

    readonly property real share: view ? Math.min(1, view.used / view.window) : 0
    readonly property var colors: ["#ededf0", "#9a9aa5", "#86e3bd", "#5f5f6a"]

    function k(n) {
        return n >= 1000 ? (n / 1000).toFixed(n >= 10000 ? 0 : 1).replace(".", ",") + "k" : String(n);
    }
    function load() {
        const id = convId;
        if (id === "" || id === "time") {
            view = null;
            return;
        }
        Api.get("/v1/conversations/" + id + "/context", (err, v) => {
            if (!err && meter.convId === id)
                meter.view = v;
        });
    }
    function compactNow() {
        error = "";
        Api.post("/v1/conversations/" + convId + "/compact", null, (err, v) => {
            if (err)
                meter.error = err;
            else
                meter.view = v;
        });
    }
    onConvIdChanged: {
        showSummary = false;
        load();
    }
    onRefreshKeyChanged: loadSoon.restart()
    Timer {
        id: loadSoon
        interval: 1200
        onTriggered: meter.load()
    }
    Timer {
        interval: 2000
        repeat: true
        running: !!meter.view && meter.view.compacting
        onTriggered: meter.load()
    }

    visible: view !== null
    width: meterRow.implicitWidth + 22
    height: 30
    radius: Theme.radiusLg
    color: pop.opened ? Theme.active : meterArea.containsMouse ? Theme.hover : "transparent"
    border.width: 1
    border.color: meterArea.containsMouse || pop.opened ? Theme.lineStrong : Theme.line

    Row {
        id: meterRow
        anchors.centerIn: parent
        spacing: 7
        Rectangle {
            anchors.verticalCenter: parent.verticalCenter
            width: 26
            height: 4
            radius: 2
            color: Theme.wash4
            Rectangle {
                width: Math.max(3, parent.width * meter.share)
                height: parent.height
                radius: 2
                color: meter.view && meter.view.used >= meter.view.compact_at ? Theme.warn : Theme.text2
            }
        }
        Text {
            anchors.verticalCenter: parent.verticalCenter
            text: meter.view ? (meter.view.compacting ? "compactando…" : meter.k(meter.view.used)) : ""
            color: Theme.text3
            font.pixelSize: Theme.fontSmall
        }
    }
    MouseArea {
        id: meterArea
        anchors.fill: parent
        hoverEnabled: true
        cursorShape: Qt.PointingHandCursor
        onClicked: {
            if (pop.opened)
                pop.close();
            else {
                meter.load();
                pop.open();
            }
        }
    }

    Popup {
        id: pop
        y: meter.height + 8
        x: meter.width - width
        width: 340
        padding: 16
        closePolicy: Popup.CloseOnEscape | Popup.CloseOnPressOutsideParent
        background: Rectangle {
            radius: Theme.radiusLg
            color: Theme.cardTop
            border.width: 1
            border.color: Theme.lineStrong
        }
        enter: Transition { ParallelAnimation {
            NumberAnimation { property: "opacity"; from: 0; to: 1; duration: Theme.medium; easing.type: Theme.ease }
            NumberAnimation { property: "scale"; from: 0.97; to: 1; duration: Theme.medium; easing.type: Theme.ease }
        } }
        exit: Transition { NumberAnimation { property: "opacity"; from: 1; to: 0; duration: Theme.fast; easing.type: Easing.InQuad } }
        contentItem: Column {
            spacing: 12
            Text {
                text: "Contexto"
                color: Theme.text
                font.pixelSize: Theme.fontLarge
                font.weight: Font.DemiBold
            }
            Item {
                width: parent.width
                height: 18
                Text {
                    text: meter.view ? Math.round(meter.share * 100) + "% usado" : ""
                    color: Theme.text2
                    font.pixelSize: Theme.fontBody
                }
                Text {
                    anchors.right: parent.right
                    text: meter.view ? "≈ " + meter.k(meter.view.used) + " / " + meter.k(meter.view.window) : ""
                    color: Theme.text3
                    font.pixelSize: Theme.fontBody
                }
            }
            // One bar, split by what fills it.
            Rectangle {
                width: parent.width
                height: 6
                radius: 3
                color: Theme.wash4
                clip: true
                Row {
                    spacing: 2
                    Repeater {
                        model: meter.view ? meter.view.parts : []
                        delegate: Rectangle {
                            required property var modelData
                            required property int index
                            width: Math.max(modelData.tokens > 0 ? 3 : 0, pop.availableWidth * modelData.tokens / meter.view.window)
                            height: 6
                            color: meter.colors[index % meter.colors.length]
                        }
                    }
                }
            }
            Column {
                width: parent.width
                spacing: 7
                Repeater {
                    model: meter.view ? meter.view.parts : []
                    delegate: Item {
                        required property var modelData
                        required property int index
                        width: parent.width
                        height: 18
                        Rectangle {
                            anchors.verticalCenter: parent.verticalCenter
                            width: 8
                            height: 8
                            radius: 2
                            color: meter.colors[index % meter.colors.length]
                        }
                        Text {
                            x: 18
                            anchors.verticalCenter: parent.verticalCenter
                            text: modelData.name
                            color: Theme.text2
                            font.pixelSize: Theme.fontBody
                        }
                        Text {
                            anchors.right: parent.right
                            anchors.verticalCenter: parent.verticalCenter
                            text: meter.k(modelData.tokens) + "   " + (100 * modelData.tokens / meter.view.window).toFixed(1).replace(".", ",") + "%"
                            color: Theme.text3
                            font.pixelSize: Theme.fontSmall
                        }
                    }
                }
            }
            Rectangle { width: parent.width; height: 1; color: Theme.line }
            Text {
                width: parent.width
                text: !meter.view ? "" : meter.view.note ? meter.view.note
                      : "Compacta sozinho em " + meter.k(meter.view.compact_at) + ": as mensagens mais antigas viram um resumo e saem do contexto (você continua vendo todas)."
                color: Theme.text3
                font.pixelSize: Theme.fontSmall
                lineHeight: 1.2
                wrapMode: Text.Wrap
            }
            // What was compacted.
            Text {
                visible: !!meter.view && !!meter.view.summary
                width: parent.width
                textFormat: Text.StyledText
                text: !meter.view || !meter.view.summary ? "" :
                      "Compactada " + meter.view.summary.count + "× · " + meter.view.hidden + " mensagens resumidas  <font color='" +
                      Theme.text2 + "'>" + (meter.showSummary ? "esconder resumo ▴" : "ver resumo ▾") + "</font>"
                color: Theme.text3
                font.pixelSize: Theme.fontSmall
                wrapMode: Text.Wrap
                MouseArea {
                    anchors.fill: parent
                    cursorShape: Qt.PointingHandCursor
                    onClicked: meter.showSummary = !meter.showSummary
                }
            }
            Rectangle {
                visible: meter.showSummary && !!meter.view && !!meter.view.summary
                width: parent.width
                height: Math.min(sumText.implicitHeight + 20, 220)
                radius: Theme.radiusMd
                color: Theme.well
                clip: true
                Flickable {
                    anchors.fill: parent
                    anchors.margins: 10
                    contentHeight: sumText.implicitHeight
                    boundsBehavior: Flickable.StopAtBounds
                    TextEdit {
                        id: sumText
                        width: parent.width
                        text: meter.view && meter.view.summary ? meter.view.summary.summary : ""
                        textFormat: TextEdit.MarkdownText
                        color: Theme.text2
                        font.pixelSize: Theme.fontSmall
                        wrapMode: TextEdit.Wrap
                        readOnly: true
                        selectByMouse: true
                    }
                }
            }
            ErrorBanner {
                width: parent.width
                text: meter.error
            }
            GhostButton {
                small: true
                icon: "spark"
                text: meter.view && meter.view.compacting ? "Compactando…" : "Compactar agora"
                enabled: !!meter.view && meter.view.can_compact && !meter.view.compacting
                onClicked: meter.compactNow()
            }
        }
    }
}
