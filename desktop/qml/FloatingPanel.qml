import QtCore
import QtQuick
import QtQuick.Controls.Basic
import Avora

// Floating right-side card over a full-window scrim: hosts AgentPage, DocPanel,
// RunPane or any detail view without displacing the conversation underneath.
Item {
    id: root
    property bool shown: false
    property real panelWidth: 560

    readonly property real minPanelWidth: 420
    readonly property real maxPanelWidth: width > 0 ? Math.max(minPanelWidth, Math.floor(width * 0.6)) : 960
    readonly property real effectiveWidth: Math.min(clampWidth(panelWidth), Math.max(320, width - 20))
    readonly property alias item: loader.item

    property Item previousFocus: null
    property bool readyItem: false

    signal closed

    Settings {
        category: "floatingPanel"
        property alias savedWidth: root.panelWidth
    }

    Component.onCompleted: {
        if (!(panelWidth >= minPanelWidth))
            panelWidth = 560;
    }

    function clampWidth(w) {
        return Math.max(minPanelWidth, Math.min(maxPanelWidth, Math.round(w || 560)));
    }

    function show(url, props) {
        if (!url)
            return;
        if (!shown && Window.window && Window.window.activeFocusItem)
            previousFocus = Window.window.activeFocusItem;
        readyItem = false;
        loader.setSource(url, props || {});
        shown = true;
    }

    function hide() {
        if (!shown)
            return;
        shown = false;
    }

    onShownChanged: {
        if (!shown) {
            const prev = previousFocus;
            previousFocus = null;
            if (prev && prev.visible && prev.forceActiveFocus)
                Qt.callLater(() => { if (prev && prev.visible) prev.forceActiveFocus(); });
            root.closed();
        }
    }

    onVisibleChanged: {
        if (!visible && !shown) {
            readyItem = false;
            loader.setSource("", {});
        }
    }

    z: 45
    visible: opacity > 0
    opacity: shown ? 1 : 0
    Behavior on opacity { NumberAnimation { duration: Theme.medium; easing.type: Theme.ease } }

    Rectangle {
        anchors.fill: parent
        color: Theme.scrim
        MouseArea {
            anchors.fill: parent
            enabled: root.shown
            onClicked: root.hide()
        }
    }

    Shortcut {
        sequence: "Esc"
        enabled: root.shown && !(loader.item && loader.item.leaving)
        onActivated: root.hide()
    }

    Rectangle {
        id: card
        width: root.effectiveWidth
        anchors {
            top: parent.top
            right: parent.right
            bottom: parent.bottom
            topMargin: 10
            rightMargin: 10
            bottomMargin: 10
        }
        radius: Theme.radiusLg
        color: Theme.card
        border.width: 1
        border.color: resizeArea.pressed || resizeArea.containsMouse ? Theme.lineStrong : Theme.line
        Behavior on border.color { ColorAnimation { duration: Theme.fast } }

        transform: Translate {
            x: root.shown ? 0 : card.width + 24
            Behavior on x { NumberAnimation { duration: Theme.medium; easing.type: Theme.ease } }
        }

        MouseArea { anchors.fill: parent } // keep clicks inside the card

        Loader {
            id: loader
            anchors {
                fill: parent
                margins: 1
            }
            clip: true
            onLoaded: {
                if (!item)
                    return;
                if (item.radius !== undefined && item.radius === 0)
                    item.radius = Theme.radiusLg - 1;
                // AgentPage initializes its draft and tab when open() is called.
                if (typeof item.open === "function" && "agentId" in item && typeof item.shown === "boolean") {
                    if ((!item.agents || item.agents.length === 0) && Api.signedIn) {
                        const target = item;
                        Api.get("/v1/agents", (err, list) => {
                            if (!err && loader.item === target)
                                target.agents = list || [];
                        });
                    }
                    if (!item.shown && item.agentId !== "")
                        item.open(item.agentId, item.tab || "overview");
                }
                root.readyItem = true;
                if (item.forceActiveFocus)
                    Qt.callLater(() => { if (loader.item === item && root.shown) item.forceActiveFocus(); });
            }
        }

        Connections {
            target: loader.item
            ignoreUnknownSignals: true
            function onCloseRequested() {
                root.hide();
            }
            function onClosed() {
                root.hide();
            }
            function onShownChanged() {
                if (root.readyItem && loader.item && typeof loader.item.shown === "boolean" && !loader.item.shown)
                    root.hide();
            }
        }

        Rectangle {
            x: 3
            anchors.verticalCenter: parent.verticalCenter
            width: 4
            height: 36
            radius: width / 2
            z: 31
            color: resizeArea.pressed ? Theme.wash6 : resizeArea.containsMouse ? Theme.wash5 : Theme.wash4
            opacity: resizeArea.pressed || resizeArea.containsMouse ? 1 : 0.65
            Behavior on color { ColorAnimation { duration: Theme.fast } }
            Behavior on opacity { NumberAnimation { duration: Theme.fast } }
        }

        MouseArea {
            id: resizeArea
            enabled: root.shown
            anchors {
                left: parent.left
                top: parent.top
                bottom: parent.bottom
                leftMargin: -5
            }
            width: 10
            z: 30
            hoverEnabled: true
            cursorShape: Qt.SizeHorCursor
            property real startX: 0
            property real startW: 0
            onPressed: mouse => {
                startX = mapToItem(root, mouse.x, 0).x;
                startW = card.width;
            }
            onPositionChanged: mouse => {
                if (!pressed)
                    return;
                const curX = mapToItem(root, mouse.x, 0).x;
                root.panelWidth = root.clampWidth(startW + (startX - curX));
            }
        }
    }
}
