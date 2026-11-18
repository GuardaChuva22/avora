import QtQuick
import Avora

// A question the agent put to the owner (the perguntar tool), with answers
// to click. While it is the last word in the conversation it is live: one
// click sends the answer (or, with several allowed, tick them and send);
// "Outra resposta" hands over to the composer. Once answered it shows what
// was picked.
Rectangle {
    id: card
    property string question: ""
    property var options: []
    property bool multiple: false
    property bool other: false
    property bool live: false
    // The owner's message that came after, if any: the answer given.
    property string answer: ""
    property color tint: Theme.text2
    signal picked(string text)
    signal otherAnswer()

    property var ticked: []
    readonly property bool answered: answer !== ""

    function chosen(o) {
        if (!answered)
            return false;
        const a = answer.toLowerCase(), x = o.toLowerCase();
        return a === x || (multiple && a.split(/\s*[,;\n]\s*|\s+e\s+/).indexOf(x) >= 0);
    }
    function toggle(o) {
        const t = ticked.slice();
        const i = t.indexOf(o);
        if (i >= 0)
            t.splice(i, 1);
        else
            t.push(o);
        ticked = t;
    }
    function sendTicked() {
        if (ticked.length === 0)
            return;
        // In the order offered, not the order clicked.
        picked(options.filter(o => ticked.indexOf(o) >= 0).join(", "));
    }

    width: parent ? parent.width : 0
    height: inner.implicitHeight + 28
    radius: Theme.radiusLg
    color: Theme.wash1
    border.width: 1
    border.color: live ? Qt.rgba(tint.r, tint.g, tint.b, 0.35) : Theme.wash4
    Behavior on border.color { ColorAnimation { duration: 200 } }

    Column {
        id: inner
        x: 14
        y: 14
        width: parent.width - 28
        spacing: 10

        Row {
            width: parent.width
            spacing: 10
            Icon {
                y: 2
                name: "question"
                size: 17
                color: card.tint
            }
            Text {
                width: parent.width - 27
                text: card.question
                color: Theme.white
                font.pixelSize: Theme.fontLarge
                font.weight: Font.DemiBold
                wrapMode: Text.Wrap
                lineHeight: 1.25
            }
        }

        Text {
            visible: card.live && card.multiple
            x: 27
            text: "Marque uma ou mais"
            color: Theme.text4
            font.pixelSize: Theme.fontSmall
        }

        Column {
            width: parent.width
            spacing: 6

            Repeater {
                model: card.options
                delegate: Rectangle {
                    id: opt
                    required property string modelData
                    required property int index
                    readonly property bool on: card.multiple && card.ticked.indexOf(modelData) >= 0
                    readonly property bool was: card.chosen(modelData)
                    width: parent.width
                    height: optText.implicitHeight + 20
                    radius: Theme.radiusMd
                    opacity: card.answered && !was ? 0.45 : 1
                    color: was || on ? Qt.rgba(card.tint.r, card.tint.g, card.tint.b, 0.14)
                         : card.live && optArea.containsMouse ? Theme.active : Theme.hover
                    border.width: 1
                    border.color: was || on ? Qt.rgba(card.tint.r, card.tint.g, card.tint.b, 0.55)
                                : card.live && optArea.containsMouse ? Theme.wash5 : Theme.active
                    Behavior on color { ColorAnimation { duration: 120 } }

                    // The mark: a number to pick, a box to tick, a check once picked.
                    Rectangle {
                        id: mark
                        x: 10
                        anchors.verticalCenter: parent.verticalCenter
                        width: 22
                        height: 22
                        radius: card.multiple ? 6 : 11
                        color: opt.was || opt.on ? card.tint : "transparent"
                        border.width: opt.was || opt.on ? 0 : 1
                        border.color: Theme.wash6
                        Text {
                            anchors.centerIn: parent
                            visible: !(opt.was || opt.on) && !card.multiple
                            text: opt.index + 1
                            color: Theme.text3
                            font.pixelSize: Theme.fontCaption
                            font.weight: Font.DemiBold
                        }
                        Icon {
                            anchors.centerIn: parent
                            visible: opt.was || opt.on
                            name: "check"
                            size: 14
                            weight: 2.2
                            color: Theme.bg
                        }
                    }
                    Text {
                        id: optText
                        x: 44
                        width: parent.width - 56
                        anchors.verticalCenter: parent.verticalCenter
                        text: opt.modelData
                        color: Theme.text
                        font.pixelSize: Theme.fontLarge
                        wrapMode: Text.Wrap
                    }
                    MouseArea {
                        id: optArea
                        anchors.fill: parent
                        enabled: card.live
                        hoverEnabled: true
                        cursorShape: Qt.PointingHandCursor
                        onClicked: card.multiple ? card.toggle(opt.modelData) : card.picked(opt.modelData)
                    }
                }
            }

            // Another answer: written in the composer.
            Rectangle {
                visible: card.other && card.live
                width: parent.width
                height: 40
                radius: Theme.radiusMd
                color: otherArea.containsMouse ? Theme.hover : "transparent"
                border.width: 1
                border.color: Qt.rgba(1, 1, 1, otherArea.containsMouse ? 0.16 : 0.07)
                Icon {
                    x: 14
                    anchors.verticalCenter: parent.verticalCenter
                    name: "pen"
                    size: 14
                    color: Theme.text3
                }
                Text {
                    x: 44
                    anchors.verticalCenter: parent.verticalCenter
                    text: "Outra resposta…"
                    color: Theme.text3
                    font.pixelSize: Theme.fontLarge
                }
                MouseArea {
                    id: otherArea
                    anchors.fill: parent
                    hoverEnabled: true
                    cursorShape: Qt.PointingHandCursor
                    onClicked: card.otherAnswer()
                }
            }
        }

        // Several picks: send them together.
        Item {
            visible: card.live && card.multiple
            width: parent.width
            height: 34
            Rectangle {
                anchors.right: parent.right
                width: sendText.implicitWidth + 32
                height: 34
                radius: Theme.radiusMd
                color: card.ticked.length ? card.tint : Theme.active
                opacity: card.ticked.length ? (sendArea.containsMouse ? 0.9 : 1) : 0.6
                Text {
                    id: sendText
                    anchors.centerIn: parent
                    text: card.ticked.length ? "Enviar (" + card.ticked.length + ")" : "Enviar"
                    color: card.ticked.length ? Theme.bg : Theme.text3
                    font.pixelSize: Theme.fontBody
                    font.weight: Font.DemiBold
                }
                MouseArea {
                    id: sendArea
                    anchors.fill: parent
                    enabled: card.ticked.length > 0
                    hoverEnabled: true
                    cursorShape: Qt.PointingHandCursor
                    onClicked: card.sendTicked()
                }
            }
        }

        // Answered with something else (typed).
        Text {
            visible: card.answered && !card.options.some(o => card.chosen(o))
            x: 2
            width: parent.width
            text: "Você respondeu: " + card.answer
            color: Theme.text3
            font.pixelSize: Theme.fontBody
            wrapMode: Text.Wrap
            maximumLineCount: 3
            elide: Text.ElideRight
        }
    }
}
