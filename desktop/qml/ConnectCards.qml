import QtQuick
import QtQuick.Controls.Basic
import Avora
import "util.js" as U
import "logos.js" as Logos

// Connect from the chat (D37): the agent asked for an app with
// pedir_conexao; each app gets a card with its logo and a Conectar button
// (one click for a login, the token box or the QR code for the others).
// connected(name) fires when the owner finishes, so the chat can tell the
// agent to carry on.
Column {
    id: root
    property var kinds: []
    property var connectors: []
    property var wa: ({})
    signal connected(string name)
    spacing: 10
    visible: kinds.length > 0

    function load() {
        if (kinds.length === 0)
            return;
        Api.get("/v1/connectors", (err, data) => {
            if (!err)
                root.connectors = data;
        });
        if (kinds.indexOf("whatsapp") >= 0)
            Api.get("/v1/whatsapp", (err, d) => {
                if (!err)
                    root.wa = d;
            });
    }
    onKindsChanged: load()
    Component.onCompleted: load()

    Repeater {
        model: root.kinds
        delegate: Card {
            id: card
            required property string modelData
            readonly property bool isWA: modelData === "whatsapp"
            readonly property var c: isWA ? ({ kind: "whatsapp", name: "WhatsApp", auth: "qr", ready: true,
                                                  description: "Seu número como aparelho conectado: ler, responder e falar com a Vega." })
                                          : root.connectors.find(x => x.kind === modelData) || null
            readonly property bool linked: isWA ? root.wa.linked === true
                                                : !!c && (c.connections || []).some(x => x.status === "ok")
            readonly property string logo: Logos.of(modelData)
            property bool open: false
            property bool busy: false
            property string error: ""
            property int qrTick: 0
            width: Math.min(root.width, 480)
            height: cc.implicitHeight + 2 * padding
            padding: 16
            radius: Theme.radiusLg
            visible: !!c

            function finish(err, view) {
                busy = false;
                error = err || "";
                if (err)
                    return;
                root.load();
                open = false;
                root.connected(c.name);
            }
            function start() {
                error = "";
                if (isWA) {
                    busy = true;
                    Api.post("/v1/whatsapp/connect", {}, (err, d) => {
                        busy = false;
                        if (err)
                            error = err;
                        else {
                            root.wa = d;
                            open = true;
                        }
                    });
                } else if (c.auth === "oauth") {
                    busy = true;
                    Api.connectOAuth(c.kind, "", c.oauth_port || 0, card.finish);
                } else {
                    open = true;
                }
            }
            function sendToken() {
                busy = true;
                let body;
                if (c.fields) {
                    const values = {};
                    for (let i = 0; i < formRep.count; i++)
                        values[c.fields[i].key] = formRep.itemAt(i).text.trim();
                    body = { fields: values, connection: "" };
                } else {
                    body = { token: tokenField.text.trim(), connection: "" };
                }
                Api.put("/v1/connectors/" + c.kind, body, (err, view) => {
                    tokenField.text = "";
                    card.finish(err, view);
                });
            }
            // While the QR code waits, look again every 2.5 s.
            Timer {
                interval: 2500
                repeat: true
                running: card.isWA && card.open && !card.linked
                onTriggered: Api.get("/v1/whatsapp", (err, d) => {
                    if (err)
                        return;
                    const was = root.wa.linked === true;
                    root.wa = d;
                    card.qrTick++;
                    if (d.linked && !was) {
                        card.open = false;
                        root.connected("WhatsApp");
                    }
                })
            }

            Column {
                id: cc
                width: parent.width
                spacing: 12
                Item {
                    width: parent.width
                    height: Math.max(36, head.implicitHeight)
                    Item {
                        id: badge
                        width: 36
                        height: 36
                        anchors.verticalCenter: parent.verticalCenter
                        Image {
                            visible: source != ""
                            anchors.centerIn: parent
                            source: Logos.image(card.modelData)
                            width: 26
                            height: 26
                            sourceSize: Qt.size(52, 52)
                            fillMode: Image.PreserveAspectFit
                            smooth: true
                            mipmap: true
                        }
                        Icon {
                            visible: Logos.image(card.modelData) === ""
                            anchors.centerIn: parent
                            path: card.logo
                            name: card.logo !== "" ? "" : U.connectorIcon(card.modelData) || "plug"
                            size: 24
                            color: Logos.color(card.modelData) !== "" ? Logos.color(card.modelData) : Theme.text
                        }
                    }
                    Column {
                        id: head
                        anchors { left: badge.right; leftMargin: 12; right: action.left; rightMargin: 10; verticalCenter: parent.verticalCenter }
                        spacing: 2
                        Text { text: card.c ? (card.linked ? card.c.name : "Conectar " + card.c.name) : ""; color: Theme.white; font.pixelSize: Theme.fontLarge; font.weight: Font.DemiBold; elide: Text.ElideRight; width: parent.width }
                        Text { text: card.c ? card.c.description || "" : ""; color: Theme.text3; font.pixelSize: Theme.fontSmall; elide: Text.ElideRight; width: parent.width }
                    }
                    Item {
                        id: action
                        anchors { right: parent.right; verticalCenter: parent.verticalCenter }
                        width: card.linked ? doneRow.implicitWidth : go.width
                        height: 32
                        Row {
                            id: doneRow
                            visible: card.linked
                            anchors.verticalCenter: parent.verticalCenter
                            spacing: 7
                            Dot { kind: "live"; anchors.verticalCenter: parent.verticalCenter }
                            Text { text: "Conectado"; color: Theme.text2; font.pixelSize: Theme.fontSmall; font.weight: Font.Medium; anchors.verticalCenter: parent.verticalCenter }
                        }
                        GhostButton {
                            id: go
                            visible: !card.linked && !card.open
                            anchors.verticalCenter: parent.verticalCenter
                            small: true
                            icon: card.busy ? "" : "plus"
                            text: card.busy ? (card.c && card.c.auth === "oauth" ? "Esperando o navegador…" : "Conectando…")
                                : card.c && !card.c.ready ? "Como configurar" : "Conectar"
                            enabled: !card.busy
                            onClicked: card.c && !card.c.ready ? card.open = true : card.start()
                        }
                    }
                }

                // WhatsApp: the code to scan.
                Column {
                    visible: card.isWA && card.open && !card.linked && root.wa.pairing === true
                    spacing: 10
                    Rectangle {
                        width: 220
                        height: 220
                        radius: Theme.radiusMd
                        color: "white"
                        Image {
                            anchors.centerIn: parent
                            width: 204
                            height: 204
                            cache: false
                            smooth: false
                            source: card.isWA && card.open && root.wa.pairing ? "image://server/v1/whatsapp/qr.png?t=" + card.qrTick : ""
                        }
                    }
                    Text {
                        width: cc.width
                        text: "No celular: WhatsApp → Aparelhos conectados → Conectar um aparelho, e aponte para o código."
                        color: Theme.text2
                        font.pixelSize: Theme.fontSmall
                        wrapMode: Text.Wrap
                    }
                }

                // Token or form apps: how to get it, and the box.
                Column {
                    visible: !card.isWA && card.open && !card.linked && !!card.c
                    width: parent.width
                    spacing: 10
                    Text {
                        width: parent.width
                        text: card.c ? card.c.help || "" : ""
                        color: Theme.text2
                        font.pixelSize: Theme.fontSmall
                        lineHeight: 1.3
                        wrapMode: Text.Wrap
                    }
                    Text {
                        visible: !!card.c && !!card.c.help_url
                        text: "<a href=\"" + (card.c ? card.c.help_url : "") + "\">Abrir a página ↗</a>"
                        textFormat: Text.StyledText
                        linkColor: Theme.text
                        font.pixelSize: Theme.fontSmall
                        onLinkActivated: link => Qt.openUrlExternally(link)
                        MouseArea { anchors.fill: parent; acceptedButtons: Qt.NoButton; cursorShape: Qt.PointingHandCursor }
                    }
                    Column {
                        visible: !!card.c && !!card.c.fields
                        width: parent.width
                        spacing: 8
                        Repeater {
                            id: formRep
                            model: card.c && card.c.fields ? card.c.fields : []
                            delegate: Field {
                                required property var modelData
                                width: parent.width
                                mono: true
                                echoMode: modelData.secret ? TextInput.Password : TextInput.Normal
                                placeholderText: modelData.label
                            }
                        }
                    }
                    Field {
                        id: tokenField
                        visible: !!card.c && card.c.auth === "token" && !card.c.fields && card.c.ready
                        width: parent.width
                        mono: true
                        echoMode: TextInput.Password
                        placeholderText: "Cole o token aqui"
                        onAccepted: if (text.trim() !== "") card.sendToken()
                    }
                    Row {
                        spacing: 8
                        PrimaryButton {
                            visible: !!card.c && card.c.auth === "token" && card.c.ready
                            text: card.busy ? "Conectando…" : "Conectar"
                            busy: card.busy
                            enabled: !card.busy && (!!(card.c && card.c.fields) || tokenField.text.trim() !== "")
                            onClicked: card.sendToken()
                        }
                        GhostButton {
                            text: "Fechar"
                            onClicked: card.open = false
                        }
                    }
                }

                ErrorBanner {
                    width: parent.width
                    visible: text !== ""
                    text: card.error
                }
            }
        }
    }
}
