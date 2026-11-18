import QtQuick
import QtQuick.Controls.Basic
import QtQuick.Dialogs
import Avora
import "util.js" as U
import "logos.js" as Logos

// Apps the owner connects once; the agents then use them. Most log in with
// one click in the browser (MCP); a few take a token. Credentials go
// straight to the server, which seals them; this page never sees them again.
Item {
    id: page
    property var connectors: []
    property var agents: []
    property string error: ""
    property string errorKind: "" // which card the error belongs to (kind or account id)
    property string busy: ""      // kind being connected or tested
    property string confirm: ""   // account or app armed for removal
    property string expanded: ""  // token tile opened
    property string details: ""   // app whose details are open (kind)
    property string picked: ""    // account selected in the details
    property string query: ""
    property bool loaded: false   // first answer arrived (or failed)

    // WhatsApp (D32): the owner's number linked to the server as a device,
    // by QR code. Same card as the other apps; the sheet shows the code.
    property var wa: ({})
    property int waTick: 0
    property string waError: ""
    readonly property bool waLinked: wa.available === true && wa.linked === true
    readonly property bool waOffer: wa.available === true && wa.linked !== true
        && (page.query.trim() === "" || "whatsapp mensagens".indexOf(page.query.trim().toLowerCase()) >= 0)
    readonly property var waBadge: ({ kind: "whatsapp", name: "WhatsApp" })
    function waLoad() {
        Api.get("/v1/whatsapp", (err, d) => {
            if (err)
                return;
            page.wa = d;
            if (d.pairing)
                page.waTick++;
        });
    }
    function waPost(path) {
        page.waError = "";
        Api.post(path, {}, (err, d) => {
            if (err)
                page.waError = err;
            else
                page.wa = d;
        });
    }
    function waConnect() {
        page.waPost("/v1/whatsapp/connect");
        waSheet.open();
    }
    function waLogout() {
        if (page.confirm !== "whatsapp") {
            page.confirm = "whatsapp";
            disarm.restart();
            return;
        }
        page.confirm = "";
        page.waPost("/v1/whatsapp/logout");
    }
    Timer {
        interval: page.wa.pairing ? 2500 : 30000
        repeat: true
        triggeredOnStart: true
        running: page.visible
        onTriggered: page.waLoad()
    }

    // One card per connected app: {c: the app, conn: its principal account,
    // conns: every account}. The accounts are listed in the details.
    readonly property var linked: connectors.filter(c => (c.connections || []).length > 0)
        .map(c => ({ c: c, conn: c.connections[0], conns: c.connections }))
    function num(v) {
        return Number(v).toLocaleString(Qt.locale("pt_BR"), "f", 0);
    }
    function usageText(u, left) {
        if (!u)
            return "";
        if (u.pending)
            return "lendo o saldo…";
        if (u.error)
            return u.error;
        if (left < 0)
            return u.plan ? "plano " + u.plan : "";
        const n = v => Number(v).toLocaleString(Qt.locale("pt_BR"), "f", 0);
        let out = n(left) + " / " + n(u.limit) + " " + (u.unit || "");
        if (u.plan)
            out += "  ·  " + u.plan;
        if (u.resets_at)
            out += "  ·  renova " + Qt.formatDate(new Date(u.resets_at * 1000), "dd/MM");
        return out;
    }
    readonly property var detailed: {
        const app = page.linked.find(p => p.c.kind === page.details);
        if (!app)
            return null;
        return { c: app.c, conn: app.conns.find(x => x.id === page.picked) || app.conn };
    }
    function openDetails(c, conn) {
        page.picked = conn ? conn.id : "";
        page.details = c.kind;
    }
    onDetailedChanged: detailed ? sheet.open() : sheet.close()
    readonly property var available: connectors.filter(c => !(c.connections || []).length && page.matches(c))
    readonly property var categories: {
        const seen = page.waOffer ? ["Mensagens"] : [];
        for (const c of available)
            if (seen.indexOf(c.category || "Outros") < 0)
                seen.push(c.category || "Outros");
        return seen;
    }

    function matches(c) {
        const q = page.query.trim().toLowerCase();
        return q === "" || (c.name + " " + c.kind + " " + (c.apps || []).join(" ") + " " + c.description + " " + (c.category || "")).toLowerCase().indexOf(q) >= 0;
    }
    function load() {
        Api.get("/v1/connectors", (err, data) => {
            page.loaded = true;
            if (err)
                page.fail("", err);
            else {
                if (!U.same(page.connectors, data))
                    page.connectors = data;
                // Credits still being read on the server: ask again shortly.
                const pending = data.some(c => Object.values(c.usage || {}).some(u => u.pending));
                if (pending)
                    usageAgain.restart();
            }
        });
    }
    function fail(kind, err) {
        page.error = err || "";
        page.errorKind = err ? kind : "";
    }
    function replace(view) {
        let found = false;
        const list = page.connectors.map(c => {
            if (c.kind !== view.kind)
                return c;
            found = true;
            return view;
        });
        if (!found)
            list.push(view);
        page.connectors = list;
    }
    function done(kind) {
        return (err, view) => {
            page.busy = "";
            page.fail(kind, err);
            if (!err && view) {
                page.replace(view);
                page.expanded = "";
            }
        };
    }
    // connect logs an app in. target is the account being reconnected;
    // without it the login adds an account.
    function connect(c, token, field, target) {
        const key = target || c.kind;
        page.busy = c.kind;
        page.fail("", "");
        if (c.auth === "oauth" || token === page.byLogin) {
            Api.connectOAuth(c.kind, target || "", c.oauth_port || 0, page.done(key));
            return;
        }
        // token is a string, or the values of an app's form (c.fields).
        const body = typeof token === "object" && token !== null ? { fields: token, connection: target || "" } : { token: token || "", connection: target || "" };
        Api.put("/v1/connectors/" + c.kind, body, (err, view) => {
            if (field)
                field.text = "";
            page.done(key)(err, view);
        });
    }
    function accountPath(c, conn, tail) {
        return "/v1/connectors/" + c.kind + (tail || "") + "?id=" + encodeURIComponent(conn.id);
    }
    // byLogin, passed as the token, connects a token app with its own login
    // (ElevenLabs) instead of a pasted key.
    readonly property string byLogin: "\u0000login"
    function cancel() {
        Api.cancelOAuth();
        page.busy = "";
    }
    function test(c, conn) {
        page.busy = c.kind;
        Api.post(page.accountPath(c, conn, "/test"), null, page.done(conn.id));
    }
    function disconnect(c, conn) {
        const key = conn ? conn.id : c.kind;
        if (confirm !== key) {
            confirm = key;
            disarm.restart();
            return;
        }
        confirm = "";
        if (!conn || (c.connections || []).length <= 1)
            page.details = "";
        page.picked = "";
        const path = conn ? page.accountPath(c, conn) : "/v1/connectors/" + c.kind;
        Api.del(path, err => {
            page.fail(c.kind, err);
            if (!err && page.expanded === c.kind)
                page.expanded = "";
            page.load();
        });
    }
    function addByAddress(address, name) {
        page.busy = "__add";
        page.fail("", "");
        Api.post("/v1/connectors/mcp", { url: address, name: name }, (err, view) => {
            page.busy = "";
            if (err) {
                page.fail("__add", err);
                return;
            }
            addUrl.text = "";
            addName.text = "";
            page.replace(view);
            const linked = (view.connections || []).length > 0;
            if (!linked && view.auth === "oauth")
                page.connect(view);
            else if (!linked)
                page.expanded = view.kind;
        });
    }
    function toggleAgent(c, conn, id) {
        let list = (conn.agents || []).slice();
        if (id === "")
            list = [];
        else if (list.indexOf(id) >= 0)
            list.splice(list.indexOf(id), 1);
        else
            list.push(id);
        Api.put(page.accountPath(c, conn, "/agents"), { agents: list }, page.done(conn.id));
    }
    function statusText(c, conn) {
        const st = conn ? conn.status : "";
        return st === "ok" ? "Conectado" : st === "reconnect" ? "Reconectar" : st === "error" ? "Com erro" : !c.ready ? "Falta configurar" : "Não conectado";
    }
    // main says whether an account is its app's default (the first), when
    // the app has more than one.
    function main(c, conn) {
        const list = c && conn ? (c.connections || []) : [];
        return list.length > 1 && list[0].id === conn.id;
    }

    Component.onCompleted: {
        Api.get("/v1/agents", (err, data) => {
            if (!err)
                page.agents = data;
        });
    }
    Timer {
        interval: 30000
        repeat: true
        triggeredOnStart: true
        running: page.visible
        onTriggered: if (page.busy === "") page.load()
    }
    Connections {
        target: Api
        function onStreamingChanged() {
            if (!Api.streaming && page.visible && page.busy === "")
                page.load();
        }
    }
    Timer {
        id: usageAgain
        interval: 1500
        onTriggered: if (page.busy === "") page.load()
    }
    Timer {
        id: disarm
        interval: 4000
        onTriggered: page.confirm = ""
    }

    // App mark, straight on the card: its logo in the brand color, a line
    // icon, or its initial.
    component Badge: Item {
        property var c
        property real side: 38
        width: side
        height: side
        readonly property string picture: c ? Logos.image(c.kind) : ""
        readonly property string logo: c && picture === "" ? Logos.of(c.kind) : ""
        readonly property string icon: c && picture === "" && logo === "" ? U.connectorIcon(c.kind) : ""
        readonly property color tint: c && Logos.color(c.kind) !== "" ? Logos.color(c.kind) : Theme.text
        Image {
            visible: parent.picture !== ""
            anchors.centerIn: parent
            source: parent.picture
            width: parent.side * 0.72
            height: width
            sourceSize: Qt.size(width * 2, height * 2)
            fillMode: Image.PreserveAspectFit
            smooth: true
            mipmap: true
        }
        Icon {
            visible: parent.picture === "" && (parent.logo !== "" || parent.icon !== "")
            anchors.centerIn: parent
            name: parent.icon
            path: parent.logo
            size: parent.side * (parent.logo !== "" ? 0.68 : 0.62)
            color: parent.tint
        }
        Text {
            visible: parent.picture === "" && parent.logo === "" && parent.icon === ""
            anchors.centerIn: parent
            text: parent.c ? parent.c.name.charAt(0).toUpperCase() : ""
            color: parent.tint
            font.family: Theme.display
            font.pixelSize: parent.side * 0.66
            font.weight: Font.Bold
        }
    }

    // A connected account, as clean as the catalog tiles: mark, name,
    // account, status. The gear opens the details.
    component PluginCard: Card {
        id: card
        property var c
        property var conn
        property var conns: []
        // The account that needs the owner, if any; else the principal.
        readonly property var broken: conns.find(x => x.status !== "ok") || null
        readonly property var shown: broken || conn
        readonly property string status: shown ? shown.status : ""
        readonly property bool working: page.busy === c.kind
        height: cardCol.implicitHeight + 2 * padding
        padding: 16
        radius: Theme.radiusLg

        Column {
            id: cardCol
            width: parent.width
            spacing: 12
            Item {
                width: parent.width
                height: Math.max(cbadge.height, ctext.implicitHeight)
                Badge { id: cbadge; c: card.c }
                Column {
                    id: ctext
                    anchors { left: cbadge.right; leftMargin: 12; right: gear.left; rightMargin: 8 }
                    spacing: 3
                    Text { text: card.c.name; color: Theme.white; font.pixelSize: Theme.fontLarge; font.weight: Font.DemiBold; elide: Text.ElideRight; width: parent.width }
                    Text {
                        width: parent.width
                        text: card.conns.length > 1 ? card.conns.length + " contas  ·  " + card.conns.map(x => x.account).join(", ")
                            : card.conn && card.conn.account ? card.conn.account : card.c.description
                        color: Theme.text3
                        font.pixelSize: Theme.fontSmall
                        elide: Text.ElideRight
                    }
                }
                GhostButton {
                    id: gear
                    anchors { right: parent.right; top: parent.top }
                    round: true
                    small: true
                    icon: "gear"
                    onClicked: page.openDetails(card.c, null)
                }
            }
            Item {
                width: parent.width
                height: 30
                Row {
                    anchors.verticalCenter: parent.verticalCenter
                    spacing: 8
                    Dot {
                        kind: card.status === "ok" ? "live" : card.status === "reconnect" ? "bad" : card.status === "error" ? "warn" : ""
                        anchors.verticalCenter: parent.verticalCenter
                    }
                    Text {
                        text: card.conns.length > 1 && card.broken ? card.broken.account + ": " + page.statusText(card.c, card.broken).toLowerCase()
                            : page.statusText(card.c, card.shown)
                        color: card.status === "ok" ? Theme.text2 : card.status === "reconnect" ? Theme.bad : Theme.text3
                        font.pixelSize: Theme.fontSmall
                        font.weight: Font.Medium
                        anchors.verticalCenter: parent.verticalCenter
                    }
                }
                GhostButton {
                    anchors { right: parent.right; verticalCenter: parent.verticalCenter }
                    visible: card.status === "reconnect"
                    small: true
                    icon: card.working ? "" : "refresh"
                    text: card.working ? "Esperando o navegador…" : "Reconectar"
                    enabled: !card.working
                    onClicked: card.c.auth === "oauth" ? page.connect(card.c, "", null, card.shown.id) : page.openDetails(card.c, card.shown)
                }
            }
            ErrorBanner {
                width: parent.width
                visible: (card.conns.some(x => x.id === page.errorKind) || page.errorKind === card.c.kind) && page.error !== "" && page.details === ""
                text: page.error
            }
        }
    }

    // Everything about one connected app: account, test, reconnect,
    // disconnect, who uses it and its tools.
    component PluginDetails: Column {
        id: det
        property var c
        property var conn
        property bool adding: false // token box for another account
        readonly property var accounts: c ? (c.connections || []) : []
        readonly property string status: conn ? conn.status : ""
        readonly property bool working: !!c && page.busy === c.kind
        spacing: 18

        Item {
            width: parent.width
            height: Math.max(dbadge.height, dhead.implicitHeight)
            Badge { id: dbadge; c: det.c; side: 44 }
            Column {
                id: dhead
                anchors { left: dbadge.right; leftMargin: 14; right: dclose.left; rightMargin: 12; verticalCenter: parent.verticalCenter }
                spacing: 4
                Text { text: det.c ? det.c.name : ""; color: Theme.white; font.pixelSize: Theme.fontTitle; font.weight: Font.DemiBold }
                Row {
                    spacing: 8
                    Dot {
                        kind: det.status === "ok" ? "live" : det.status === "reconnect" ? "bad" : det.status === "error" ? "warn" : ""
                        anchors.verticalCenter: parent.verticalCenter
                    }
                    Text {
                        text: !det.c ? "" : det.accounts.length > 1 ? det.accounts.length + " contas conectadas" : page.statusText(det.c, det.conn)
                        color: Theme.text2
                        font.pixelSize: Theme.fontSmall
                        anchors.verticalCenter: parent.verticalCenter
                    }
                }
            }
            GhostButton {
                id: dclose
                anchors { right: parent.right; top: parent.top }
                round: true
                small: true
                icon: "close"
                onClicked: page.details = ""
            }
        }

        Text {
            width: parent.width
            text: det.c ? det.c.description : ""
            color: Theme.text3
            font.pixelSize: Theme.fontBody
            wrapMode: Text.Wrap
        }

        // Every account of the app, with what each has left. With more than
        // one, a click picks the account the actions below act on.
        Column {
            width: parent.width
            spacing: 8
            Text {
                visible: det.accounts.length > 1
                text: "CONTAS  ·  " + det.accounts.length
                color: Theme.text4
                font.pixelSize: Theme.fontCaption
                font.weight: Font.DemiBold
                font.letterSpacing: Theme.trackCaps
            }
            Repeater {
                model: det.accounts
                delegate: Rectangle {
                    id: arow
                    required property var modelData
                    required property int index
                    readonly property var a: modelData
                    readonly property bool selected: det.accounts.length > 1 && !!det.conn && det.conn.id === a.id
                    readonly property var usage: det.c && det.c.usage ? det.c.usage[a.id] || null : null
                    readonly property real remaining: usage && usage.limit > 0 ? Math.max(0, usage.limit - usage.used) : -1
                    width: det.width
                    height: acol.implicitHeight + 24
                    radius: Theme.radiusMd
                    color: selected ? Theme.active : rowArea.containsMouse && det.accounts.length > 1 ? Theme.hover : "transparent"
                    border.width: 1
                    border.color: selected ? Theme.lineStrong : Theme.line
                    MouseArea {
                        id: rowArea
                        anchors.fill: parent
                        hoverEnabled: true
                        enabled: det.accounts.length > 1
                        cursorShape: enabled ? Qt.PointingHandCursor : Qt.ArrowCursor
                        onClicked: page.picked = arow.a.id
                    }
                    Column {
                        id: acol
                        x: 14
                        y: 12
                        width: parent.width - 28
                        spacing: 7
                        Item {
                            width: parent.width
                            height: aname.implicitHeight
                            Dot {
                                id: adot
                                kind: arow.a.status === "ok" ? "live" : arow.a.status === "reconnect" ? "bad" : arow.a.status === "error" ? "warn" : ""
                                anchors.verticalCenter: parent.verticalCenter
                            }
                            Text {
                                id: aname
                                anchors { left: adot.right; leftMargin: 9; right: atag.left; rightMargin: 8 }
                                text: arow.a.account || "conta " + (arow.index + 1)
                                color: Theme.white
                                font.pixelSize: Theme.fontBody
                                font.weight: Font.DemiBold
                                elide: Text.ElideRight
                            }
                            Text {
                                id: atag
                                anchors { right: parent.right; verticalCenter: parent.verticalCenter }
                                text: det.accounts.length > 1 && arow.index === 0 ? "PRINCIPAL" : ""
                                color: Theme.text4
                                font.pixelSize: Theme.fontTiny
                                font.weight: Font.DemiBold
                                font.letterSpacing: Theme.trackCaps
                            }
                        }
                        Text {
                            width: parent.width
                            text: arow.a.last_error ? arow.a.last_error
                                : arow.a.last_ok ? "Funcionou " + U.ago(arow.a.last_ok) : "Conectado " + U.ago(arow.a.created_at)
                            color: arow.a.last_error ? Theme.bad : Theme.text4
                            font.pixelSize: Theme.fontSmall
                            wrapMode: Text.Wrap
                        }
                        // Credits left, for apps that report them.
                        Rectangle {
                            width: parent.width
                            height: 6
                            radius: 3
                            color: Theme.line
                            visible: arow.remaining >= 0
                            Rectangle {
                                width: arow.remaining >= 0 ? parent.width * arow.remaining / arow.usage.limit : 0
                                height: parent.height
                                radius: 3
                                color: arow.remaining / Math.max(1, arow.usage ? arow.usage.limit : 1) < 0.1 ? Theme.bad : Theme.live
                            }
                        }
                        Item {
                            width: parent.width
                            height: ucount.implicitHeight
                            visible: !!arow.usage
                            Text {
                                id: ucount
                                text: !arow.usage ? "" : arow.usage.pending ? "lendo o saldo…" : arow.usage.error ? arow.usage.error
                                    : arow.remaining >= 0 && arow.usage.unit === "usd_cents" ? "US$ " + (arow.remaining / 100).toFixed(2).replace(".", ",") + " de US$ " + (arow.usage.limit / 100).toFixed(2).replace(".", ",") + " restantes"
                                    : arow.remaining >= 0 ? page.num(arow.remaining) + " de " + page.num(arow.usage.limit) + " " + (arow.usage.unit || "") + " restantes"
                                    : ""
                                color: arow.usage && arow.usage.error ? Theme.warn : Theme.text2
                                font.family: Theme.mono
                                font.pixelSize: Theme.fontSmall
                            }
                            Text {
                                anchors { right: parent.right; verticalCenter: parent.verticalCenter }
                                text: !arow.usage || arow.usage.error ? "" : [arow.usage.plan ? "plano " + arow.usage.plan : "",
                                    arow.usage.resets_at ? "renova " + Qt.formatDate(new Date(arow.usage.resets_at * 1000), "dd/MM") : ""].filter(x => x).join("  ·  ")
                                color: Theme.text4
                                font.pixelSize: Theme.fontSmall
                            }
                        }
                    }
                }
            }
            Text {
                visible: !!det.c && !!det.c.usage && det.accounts.length > 1
                width: parent.width
                text: "Total: " + page.num(det.accounts.reduce((sum, a) => {
                    const u = det.c && det.c.usage ? det.c.usage[a.id] : null;
                    return sum + (u && u.limit > 0 ? Math.max(0, u.limit - u.used) : 0);
                }, 0)) + " " + ((det.c && det.c.usage && Object.values(det.c.usage)[0] || {}).unit || "") + " restantes nas contas"
                color: Theme.text3
                font.pixelSize: Theme.fontSmall
            }
        }

        Row {
            spacing: 8
            GhostButton {
                small: true
                visible: det.status === "reconnect" && !!det.c && det.c.auth === "oauth"
                icon: "refresh"
                text: det.working ? "Esperando o navegador…" : "Reconectar"
                enabled: !det.working
                onClicked: page.connect(det.c, "", null, det.conn.id)
            }
            GhostButton {
                small: true
                visible: det.status !== "reconnect"
                icon: "refresh"
                text: det.working ? "Testando…" : "Testar"
                enabled: !det.working
                onClicked: page.test(det.c, det.conn)
            }
            GhostButton {
                small: true
                danger: true
                visible: !!det.c && !det.c.via
                text: !det.conn ? "" : page.confirm === det.conn.id ? "Confirmar" : det.c.custom && det.c.connections.length === 1 ? "Remover"
                    : det.accounts.length > 1 ? "Desconectar " + (det.conn.account || "esta conta") : "Desconectar"
                onClicked: page.disconnect(det.c, det.conn)
            }
        }

        ErrorBanner {
            width: parent.width
            visible: !!det.conn && (page.errorKind === det.conn.id || page.errorKind === det.c.kind) && page.error !== ""
            text: page.error
        }

        // Lost access with the app's login: log in again.
        PrimaryButton {
            visible: det.status === "reconnect" && !!det.c && !!det.c.login && !!det.conn && /· login$/.test(det.conn.account || "")
            text: det.working ? "Esperando o navegador…" : "Entrar de novo com a " + det.c.name
            busy: det.working
            enabled: !det.working
            onClicked: page.connect(det.c, page.byLogin, null, det.conn.id)
        }

        // Lost access with a token: paste a new one.
        Row {
            visible: det.status === "reconnect" && !!det.c && det.c.auth === "token" && !det.c.fields
                && !(!!det.c.login && !!det.conn && /· login$/.test(det.conn.account || ""))
            width: parent.width
            spacing: 10
            Field {
                id: retoken
                width: parent.width - regoBtn.width - 10
                mono: true
                echoMode: TextInput.Password
                placeholderText: "Cole o token novo"
                onAccepted: if (text.trim() !== "") page.connect(det.c, text.trim(), retoken, det.conn.id)
            }
            PrimaryButton {
                id: regoBtn
                text: "Reconectar"
                busy: det.working
                enabled: retoken.text.trim() !== "" && !det.working
                onClicked: page.connect(det.c, retoken.text.trim(), retoken, det.conn.id)
            }
        }

        // Another account of the same app (a second Gmail).
        Text {
            visible: !!det.c && !!det.c.via
            width: parent.width
            text: "Usa as contas do Apify: o crédito, as contas e quem pode usar se mudam no Apify."
            color: Theme.text3
            font.pixelSize: Theme.fontSmall
            wrapMode: Text.Wrap
        }

        Column {
            visible: !!det.c && det.c.auth !== "none" && !det.c.fields && !det.c.via
            width: parent.width
            spacing: 10
            Row {
                spacing: 8
                GhostButton {
                    visible: !det.adding
                    small: true
                    icon: det.working && det.c.auth === "oauth" ? "" : "plus"
                    text: det.working && det.c.auth === "oauth" ? "Esperando o navegador…" : "Adicionar outra conta"
                    enabled: !det.working
                    onClicked: det.c.auth === "oauth" ? page.connect(det.c) : det.adding = true
                }
                GhostButton {
                    visible: !det.adding && !!det.c.login && !det.working
                    small: true
                    icon: "plus"
                    text: "Entrar com outra conta da " + det.c.name
                    onClicked: page.connect(det.c, page.byLogin)
                }
                GhostButton {
                    visible: det.working && (det.c.auth === "oauth" || !!det.c.login)
                    small: true
                    text: "Cancelar"
                    onClicked: page.cancel()
                }
            }
            Text {
                visible: !!det.c && (det.c.connections || []).length > 1
                width: parent.width
                text: "Os funcionários escolhem a conta pelo nome quando você pede (\"no meu Gmail do trabalho\"); sem dizer, usam a principal, a primeira conectada."
                color: Theme.text4
                font.pixelSize: Theme.fontSmall
                wrapMode: Text.Wrap
            }
            Row {
                visible: det.adding
                width: parent.width
                spacing: 10
                Field {
                    id: addToken
                    width: parent.width - addTokenBtn.width - addClose.width - 20
                    mono: true
                    echoMode: TextInput.Password
                    placeholderText: "Token da outra conta"
                    onAccepted: if (text.trim() !== "") page.connect(det.c, text.trim(), addToken)
                }
                PrimaryButton {
                    id: addTokenBtn
                    text: "Adicionar"
                    busy: det.working
                    enabled: addToken.text.trim() !== "" && !det.working
                    onClicked: page.connect(det.c, addToken.text.trim(), addToken)
                }
                GhostButton {
                    id: addClose
                    text: "Fechar"
                    onClicked: det.adding = false
                }
            }
        }

        Column {
            visible: page.agents.length > 1 && !!det.c && !det.c.via
            width: parent.width
            spacing: 8
            Overline { text: "QUEM USA" }
            Flow {
                width: parent.width
                spacing: 8
                Repeater {
                    model: [{ id: "", name: "Todos" }].concat(page.agents)
                    delegate: Rectangle {
                        id: who
                        required property var modelData
                        readonly property var list: det.conn ? (det.conn.agents || []) : []
                        readonly property bool on: modelData.id === "" ? list.length === 0 : list.indexOf(modelData.id) >= 0
                        height: 28
                        width: whoText.implicitWidth + 24
                        radius: Theme.radiusLg
                        color: on ? Theme.wash4 : whoArea.containsMouse ? Theme.hover : "transparent"
                        border.width: 1
                        border.color: on ? Theme.wash6 : Theme.line
                        Text {
                            id: whoText
                            anchors.centerIn: parent
                            text: who.modelData.name
                            color: who.on ? Theme.white : Theme.text3
                            font.pixelSize: Theme.fontSmall
                        }
                        MouseArea {
                            id: whoArea
                            anchors.fill: parent
                            hoverEnabled: true
                            cursorShape: Qt.PointingHandCursor
                            onClicked: page.toggleAgent(det.c, det.conn, who.modelData.id)
                        }
                    }
                }
            }
        }

        Column {
            width: parent.width
            spacing: 6
            visible: !!det.c && det.c.tools.length > 0
            Text {
                text: !det.c ? "" : ("Ferramentas  ·  " + det.c.tools.length + "  ·  "
                      + (n => n === 0 ? "nenhuma pede aprovação" : n + (n === 1 ? " pede" : " pedem") + " aprovação")(det.c.tools.filter(t => t.locked).length)).toUpperCase()
                color: Theme.text4
                font.pixelSize: Theme.fontCaption
                font.weight: Font.DemiBold
                font.letterSpacing: Theme.trackCaps
                bottomPadding: 4
            }
            Repeater {
                model: det.c ? det.c.tools : []
                delegate: Item {
                    required property var modelData
                    width: det.width
                    height: toolText.implicitHeight + 8
                    Icon {
                        id: toolIcon
                        y: 5
                        name: modelData.locked ? "lock" : "check"
                        size: 13
                        color: modelData.locked ? Theme.warn : Theme.text4
                    }
                    Text {
                        id: toolText
                        anchors { left: toolIcon.right; leftMargin: 9; right: parent.right; top: parent.top; topMargin: 3 }
                        text: "<span style=\"color:" + Theme.text2 + "\">" + modelData.name + "</span>  " + U.escape(modelData.description.replace(/^\[[^\]]*\] /, ""))
                        textFormat: Text.StyledText
                        color: Theme.text3
                        font.pixelSize: Theme.fontSmall
                        wrapMode: Text.Wrap
                    }
                }
            }
        }
    }

    // An app to connect: one click, or a token box that opens on click.
    component PluginTile: Card {
        id: tile
        property var c
        readonly property bool working: page.busy === c.kind
        readonly property bool tokenOpen: page.expanded === c.kind
        height: tileCol.implicitHeight + 2 * padding
        padding: 16
        radius: Theme.radiusLg

        Column {
            id: tileCol
            width: parent.width
            spacing: 12
            Item {
                width: parent.width
                height: Math.max(tbadge.height, ttext.implicitHeight)
                Badge { id: tbadge; c: tile.c }
                Column {
                    id: ttext
                    anchors { left: tbadge.right; leftMargin: 12; right: parent.right }
                    spacing: 3
                    Text { text: tile.c.name; color: Theme.white; font.pixelSize: Theme.fontLarge; font.weight: Font.DemiBold; elide: Text.ElideRight; width: parent.width }
                    Text {
                        width: parent.width
                        text: tile.c.description
                        color: Theme.text3
                        font.pixelSize: Theme.fontSmall
                        wrapMode: Text.Wrap
                        maximumLineCount: 2
                        elide: Text.ElideRight
                        height: 32
                    }
                }
            }
            Row {
                spacing: 8
                GhostButton {
                    visible: !tile.tokenOpen
                    small: true
                    icon: tile.working ? "" : "plus"
                    text: tile.working ? (tile.c.auth === "oauth" ? "Esperando o navegador…" : "Conectando…")
                        : !tile.c.ready ? "Como configurar" : tile.c.via ? "Conectar o Apify" : "Conectar"
                    enabled: !tile.working && page.busy === ""
                    onClicked: {
                        if (tile.c.via)
                            page.expanded = tile.c.via;
                        else if (!tile.c.ready || tile.c.auth === "token")
                            page.expanded = tile.c.kind;
                        else
                            page.connect(tile.c);
                    }
                }
                GhostButton {
                    visible: tile.working && tile.c.auth === "oauth"
                    small: true
                    text: "Cancelar"
                    onClicked: page.cancel()
                }
                GhostButton {
                    visible: !!tile.c.custom && page.busy === ""
                    small: true
                    danger: true
                    text: page.confirm === tile.c.kind ? "Confirmar" : "Remover"
                    onClicked: page.disconnect(tile.c, null)
                }
            }
            ErrorBanner {
                width: parent.width
                visible: page.errorKind === tile.c.kind && page.error !== ""
                text: page.error
            }
            Column {
                visible: tile.tokenOpen
                width: parent.width
                spacing: 10
                Text {
                    width: parent.width
                    text: tile.c.help
                    color: Theme.text2
                    font.pixelSize: Theme.fontSmall
                    lineHeight: 1.3
                    wrapMode: Text.Wrap
                }
                Text {
                    visible: !!tile.c.help_url
                    text: "<a href=\"" + tile.c.help_url + "\">" + (tile.c.auth === "oauth" ? "Abrir o Google Cloud" : tile.c.fields ? "Abrir o painel" : "Abrir a página do token") + " ↗</a>"
                    textFormat: Text.StyledText
                    linkColor: Theme.text
                    font.pixelSize: Theme.fontSmall
                    onLinkActivated: link => Qt.openUrlExternally(link)
                    MouseArea { anchors.fill: parent; acceptedButtons: Qt.NoButton; cursorShape: Qt.PointingHandCursor }
                }
                // An app with a form: one box per value.
                Column {
                    visible: !!tile.c.fields
                    width: parent.width
                    spacing: 8
                    Repeater {
                        id: formRep
                        model: tile.c.fields || []
                        // A box, or for a file (a certificate) a button that
                        // reads it as base64; text is the value either way.
                        delegate: Item {
                            id: fd
                            required property var modelData
                            property string text: modelData.file ? fileValue : box.text
                            property string fileValue: ""
                            property string fileName: ""
                            property string fileError: ""
                            width: parent.width
                            height: modelData.file ? fileRow.height : box.height
                            Field {
                                id: box
                                visible: !fd.modelData.file
                                width: parent.width
                                mono: true
                                echoMode: fd.modelData.secret ? TextInput.Password : TextInput.Normal
                                placeholderText: fd.modelData.label + (fd.modelData.optional ? " (opcional)" : "")
                            }
                            Row {
                                id: fileRow
                                visible: !!fd.modelData.file
                                spacing: 10
                                GhostButton {
                                    icon: "lock"
                                    text: fd.fileName !== "" ? "Trocar o arquivo" : fd.modelData.label
                                    onClicked: certDialog.open()
                                }
                                Text {
                                    anchors.verticalCenter: parent.verticalCenter
                                    text: fd.fileError !== "" ? fd.fileError : fd.fileName
                                    color: fd.fileError !== "" ? Theme.bad : Theme.text2
                                    font.pixelSize: Theme.fontSmall
                                }
                            }
                            FileDialog {
                                id: certDialog
                                title: fd.modelData.label
                                nameFilters: ["Certificado (*.pfx *.p12)", "Todos (*)"]
                                onAccepted: {
                                    const v = Api.fileBase64(selectedFile, 36000);
                                    fd.fileValue = v;
                                    fd.fileError = v === "" ? "Arquivo vazio ou grande demais" : "";
                                    const parts = String(selectedFile).split("/");
                                    fd.fileName = v === "" ? "" : decodeURIComponent(parts[parts.length - 1]);
                                }
                            }
                        }
                    }
                }
                PrimaryButton {
                    visible: !!tile.c.login
                    text: tile.working ? "Esperando o navegador…" : "Entrar com a " + tile.c.name
                    busy: tile.working
                    enabled: !tile.working
                    onClicked: page.connect(tile.c, page.byLogin)
                }
                Text {
                    visible: !!tile.c.login
                    text: "ou cole uma chave:"
                    color: Theme.text3
                    font.pixelSize: Theme.fontSmall
                }
                Field {
                    id: token
                    visible: tile.c.auth === "token" && !tile.c.fields
                    width: parent.width
                    mono: true
                    echoMode: TextInput.Password
                    placeholderText: "Cole o token aqui"
                    onAccepted: if (text.trim() !== "") page.connect(tile.c, text.trim(), token)
                }
                Row {
                    spacing: 8
                    PrimaryButton {
                        visible: tile.c.auth === "token"
                        text: tile.working ? "Conectando…" : "Conectar"
                        busy: tile.working
                        enabled: (!!tile.c.fields || token.text.trim() !== "") && !tile.working
                        onClicked: {
                            if (!tile.c.fields) {
                                page.connect(tile.c, token.text.trim(), token);
                                return;
                            }
                            const values = {};
                            for (var i = 0; i < formRep.count; i++)
                                values[tile.c.fields[i].key] = formRep.itemAt(i).text.trim();
                            page.connect(tile.c, values, null);
                        }
                    }
                    GhostButton {
                        visible: tile.working && !!tile.c.login
                        text: "Cancelar"
                        onClicked: page.cancel()
                    }
                    GhostButton {
                        text: "Fechar"
                        onClicked: page.expanded = ""
                    }
                }
            }
        }
    }

    // WhatsApp, as a tile to connect or as a connected card.
    component WhatsAppTile: Card {
        id: wt
        readonly property bool linked: page.waLinked
        height: wtCol.implicitHeight + 2 * padding
        padding: 16
        radius: Theme.radiusLg

        Column {
            id: wtCol
            width: parent.width
            spacing: 12
            Item {
                width: parent.width
                height: Math.max(wbadge.height, wtext.implicitHeight)
                Badge { id: wbadge; c: page.waBadge }
                Column {
                    id: wtext
                    anchors { left: wbadge.right; leftMargin: 12; right: wgear.visible ? wgear.left : parent.right; rightMargin: wgear.visible ? 8 : 0 }
                    spacing: 3
                    Text { text: "WhatsApp"; color: Theme.white; font.pixelSize: Theme.fontLarge; font.weight: Font.DemiBold; elide: Text.ElideRight; width: parent.width }
                    Text {
                        width: parent.width
                        text: wt.linked ? (page.wa.name ? page.wa.name + "  ·  " : "") + (page.wa.phone || "")
                            : "Seu número como aparelho conectado: os funcionários leem e respondem suas conversas, e você fala com a Vega por mensagem."
                        color: Theme.text3
                        font.pixelSize: Theme.fontSmall
                        wrapMode: wt.linked ? Text.NoWrap : Text.Wrap
                        maximumLineCount: wt.linked ? 1 : 2
                        elide: Text.ElideRight
                        height: wt.linked ? implicitHeight : 32
                    }
                }
                GhostButton {
                    id: wgear
                    visible: wt.linked
                    anchors { right: parent.right; top: parent.top }
                    round: true
                    small: true
                    icon: "gear"
                    onClicked: waSheet.open()
                }
            }
            Item {
                visible: wt.linked
                width: parent.width
                height: 30
                Row {
                    anchors.verticalCenter: parent.verticalCenter
                    spacing: 8
                    Dot { kind: page.wa.online ? "live" : "warn"; anchors.verticalCenter: parent.verticalCenter }
                    Text {
                        text: page.wa.online ? "Conectado" : "Reconectando…"
                        color: page.wa.online ? Theme.text2 : Theme.text3
                        font.pixelSize: Theme.fontSmall
                        font.weight: Font.Medium
                        anchors.verticalCenter: parent.verticalCenter
                    }
                }
            }
            GhostButton {
                visible: !wt.linked
                small: true
                icon: page.wa.pairing ? "" : "plus"
                text: page.wa.pairing ? "Mostrar o código" : "Conectar"
                onClicked: page.wa.pairing ? waSheet.open() : page.waConnect()
            }
        }
    }

    Flickable {
        id: scroller
        anchors.fill: parent
        contentHeight: col.height + 110
        clip: true
        boundsBehavior: Flickable.StopAtBounds
        ScrollBar.vertical: SlimScrollBar {}
        SmoothWheel {}

        Column {
            id: col
            // Centered, with even margins on both sides.
            x: Math.round((scroller.width - width) / 2)
            y: 52
            width: Math.min(scroller.width - 2 * Theme.gutter, 1240)
            spacing: 22

            PageHead {
                eyebrow: "Conexões"
                title: "Plugins"
                sub: "Conecte seus apps e os funcionários passam a usar. A maioria entra com um clique no navegador. Ler, buscar e criar no seu espaço é livre; enviar, comentar, apagar ou pagar pede sua aprovação."
            }

            ErrorBanner {
                width: parent.width
                visible: page.error !== "" && page.errorKind === ""
                text: page.error
            }

            // Placeholder cards until the first answer, so the page has its
            // shape at once instead of popping in.
            Flow {
                id: skeleton
                visible: !page.loaded
                width: parent.width
                spacing: 14
                readonly property real cell: {
                    const n = Math.max(1, Math.floor((width + spacing) / (300 + spacing)));
                    return (width - (n - 1) * spacing) / n;
                }
                Repeater {
                    model: 6
                    delegate: Card {
                        required property int index
                        width: skeleton.cell
                        height: 110
                        radius: Theme.radiusLg
                        padding: 18
                        opacity: 0.6
                        Rectangle { width: 26; height: 26; radius: Theme.radiusSm; color: Theme.hover }
                        Rectangle { x: 42; y: 2; width: 110 - index * 7; height: 10; radius: 5; color: Theme.active }
                        Rectangle { x: 42; y: 20; width: 160 - index * 9; height: 8; radius: 4; color: Theme.hover }
                        Rectangle { y: 58; width: 80; height: 8; radius: 4; color: Theme.hover }
                    }
                }
            }

            Column {
                visible: page.linked.length > 0 || page.waLinked
                width: parent.width
                spacing: 12
                Text { text: "CONECTADOS  ·  " + (page.linked.length + (page.waLinked ? 1 : 0));leftPadding: 4; color: Theme.text4; font.pixelSize: Theme.fontCaption; font.weight: Font.DemiBold; font.letterSpacing: Theme.trackCaps }
                Flow {
                    id: linkedGrid
                    width: parent.width
                    spacing: 14
                    readonly property real cell: {
                        const n = Math.max(1, Math.floor((width + spacing) / (300 + spacing)));
                        return (width - (n - 1) * spacing) / n;
                    }
                    Repeater {
                        model: page.linked
                        delegate: PluginCard {
                            required property var modelData
                            c: modelData.c
                            conn: modelData.conn
                            conns: modelData.conns
                            width: linkedGrid.cell
                        }
                    }
                    WhatsAppTile {
                        visible: page.waLinked
                        width: linkedGrid.cell
                    }
                }
            }

            Field {
                width: Math.min(parent.width, 420)
                placeholderText: "Buscar app (" + page.connectors.length + " disponíveis)"
                text: page.query
                onTextChanged: page.query = text
            }

            Repeater {
                model: page.categories
                delegate: Column {
                    id: group
                    required property var modelData
                    width: col.width
                    spacing: 12
                    Text { text: group.modelData.toUpperCase(); leftPadding: 4; color: Theme.text4; font.pixelSize: Theme.fontCaption; font.weight: Font.DemiBold; font.letterSpacing: Theme.trackCaps }
                    Flow {
                        id: tiles
                        width: parent.width
                        spacing: 14
                        readonly property real cell: {
                            const n = Math.max(1, Math.floor((width + spacing) / (300 + spacing)));
                            return (width - (n - 1) * spacing) / n;
                        }
                        Repeater {
                            model: page.available.filter(c => (c.category || "Outros") === group.modelData)
                            delegate: PluginTile {
                                required property var modelData
                                c: modelData
                                width: tiles.cell
                            }
                        }
                        WhatsAppTile {
                            visible: group.modelData === "Mensagens"
                            width: tiles.cell
                        }
                    }
                }
            }

            Text {
                visible: page.available.length === 0 && !page.waOffer && page.query !== ""
                text: "Nenhum app com \"" + page.query + "\". Se ele tiver um servidor MCP, adicione pelo endereço abaixo."
                color: Theme.text3
                font.pixelSize: Theme.fontBody
            }

            // Any other MCP server, by address.
            Card {
                width: col.width
                height: addCol.implicitHeight + 2 * padding
                padding: 22
                Column {
                    id: addCol
                    width: parent.width
                    spacing: 12
                    Overline { text: "OUTRO APP POR ENDEREÇO (MCP)" }
                    Text {
                        width: parent.width
                        text: "Se o app tem um servidor MCP remoto, cole o endereço (https://…). Se ele aceitar login de um clique, o navegador abre; se pedir chave, aparece o campo."
                        color: Theme.text3
                        font.pixelSize: Theme.fontBody
                        wrapMode: Text.Wrap
                    }
                    Row {
                        width: parent.width
                        spacing: 10
                        Field {
                            id: addUrl
                            width: parent.width - addName.width - addBtn.width - 20
                            mono: true
                            placeholderText: "https://mcp.exemplo.com/mcp"
                            onAccepted: if (text.trim() !== "") page.addByAddress(text.trim(), addName.text.trim())
                        }
                        Field {
                            id: addName
                            width: 180
                            placeholderText: "Nome (opcional)"
                        }
                        PrimaryButton {
                            id: addBtn
                            text: page.busy === "__add" ? "Verificando…" : "Adicionar"
                            busy: page.busy === "__add"
                            enabled: addUrl.text.trim() !== "" && page.busy === ""
                            onClicked: page.addByAddress(addUrl.text.trim(), addName.text.trim())
                        }
                    }
                    ErrorBanner {
                        width: parent.width
                        visible: page.errorKind === "__add" && page.error !== ""
                        text: page.error
                    }
                }
            }
        }
    }

    Popup {
        id: sheet
        parent: Overlay.overlay
        anchors.centerIn: parent
        width: Math.min(560, page.width - 48)
        height: Math.min(sheetBody.implicitHeight + 2 * padding, page.height - 80)
        padding: 26
        modal: true
        dim: true
        closePolicy: Popup.CloseOnEscape | Popup.CloseOnPressOutside
        onClosed: page.details = ""
        Overlay.modal: Rectangle { color: Theme.scrim }
        background: Card { radius: 20; padding: 0 }
        enter: Transition { ParallelAnimation {
            NumberAnimation { property: "opacity"; from: 0; to: 1; duration: Theme.medium; easing.type: Theme.ease }
            NumberAnimation { property: "scale"; from: 0.97; to: 1; duration: Theme.medium; easing.type: Theme.ease }
        } }
        exit: Transition { NumberAnimation { property: "opacity"; from: 1; to: 0; duration: Theme.fast; easing.type: Easing.InQuad } }
        contentItem: Flickable {
            contentHeight: sheetBody.implicitHeight
            clip: true
            boundsBehavior: Flickable.StopAtBounds
            ScrollBar.vertical: SlimScrollBar {}
            PluginDetails {
                id: sheetBody
                width: parent.width
                c: page.detailed ? page.detailed.c : null
                conn: page.detailed ? page.detailed.conn : null
            }
        }
    }

    // WhatsApp details: the QR code while pairing; status, disconnect and
    // tools once linked.
    Popup {
        id: waSheet
        parent: Overlay.overlay
        anchors.centerIn: parent
        width: Math.min(560, page.width - 48)
        height: Math.min(waBody.implicitHeight + 2 * padding, page.height - 80)
        padding: 26
        modal: true
        dim: true
        closePolicy: Popup.CloseOnEscape | Popup.CloseOnPressOutside
        Overlay.modal: Rectangle { color: Theme.scrim }
        background: Card { radius: 20; padding: 0 }
        enter: Transition { ParallelAnimation {
            NumberAnimation { property: "opacity"; from: 0; to: 1; duration: Theme.medium; easing.type: Theme.ease }
            NumberAnimation { property: "scale"; from: 0.97; to: 1; duration: Theme.medium; easing.type: Theme.ease }
        } }
        exit: Transition { NumberAnimation { property: "opacity"; from: 1; to: 0; duration: Theme.fast; easing.type: Easing.InQuad } }
        contentItem: Flickable {
            contentHeight: waBody.implicitHeight
            clip: true
            boundsBehavior: Flickable.StopAtBounds
            ScrollBar.vertical: SlimScrollBar {}
            Column {
                id: waBody
                width: parent.width
                spacing: 18

                Item {
                    width: parent.width
                    height: Math.max(wsBadge.height, wsHead.implicitHeight)
                    Badge { id: wsBadge; c: page.waBadge; side: 44 }
                    Column {
                        id: wsHead
                        anchors { left: wsBadge.right; leftMargin: 14; right: wsClose.left; rightMargin: 12; verticalCenter: parent.verticalCenter }
                        spacing: 4
                        Text { text: "WhatsApp"; color: Theme.white; font.pixelSize: Theme.fontTitle; font.weight: Font.DemiBold }
                        Row {
                            spacing: 8
                            Dot {
                                kind: page.waLinked ? (page.wa.online ? "live" : "warn") : ""
                                anchors.verticalCenter: parent.verticalCenter
                            }
                            Text {
                                text: page.waLinked ? (page.wa.online ? "Conectado" : "Reconectando…") + "  ·  " + (page.wa.phone || "")
                                    : page.wa.pairing ? "Esperando você ler o código" : "Não conectado"
                                color: Theme.text2
                                font.pixelSize: Theme.fontSmall
                                anchors.verticalCenter: parent.verticalCenter
                            }
                        }
                    }
                    GhostButton {
                        id: wsClose
                        anchors { right: parent.right; top: parent.top }
                        round: true
                        small: true
                        icon: "close"
                        onClicked: waSheet.close()
                    }
                }

                Text {
                    width: parent.width
                    text: "Seu número entra como um aparelho conectado, igual ao WhatsApp Web. Os funcionários leem suas conversas e respondem clientes com a sua aprovação (ou dentro do mandato de um objetivo). Mande mensagem para você mesmo e a Vega responde ali."
                    color: Theme.text3
                    font.pixelSize: Theme.fontBody
                    wrapMode: Text.Wrap
                }

                // The code to scan, refreshed while it waits.
                Column {
                    visible: page.wa.pairing === true
                    width: parent.width
                    spacing: 12
                    Rectangle {
                        width: 236
                        height: 236
                        radius: Theme.radiusMd
                        color: "white"
                        Image {
                            anchors.centerIn: parent
                            width: 220
                            height: 220
                            cache: false
                            smooth: false
                            fillMode: Image.PreserveAspectFit
                            source: page.wa.pairing ? "image://server/v1/whatsapp/qr.png?t=" + page.waTick : ""
                        }
                    }
                    Text {
                        width: parent.width
                        text: "No celular: WhatsApp → Aparelhos conectados → Conectar um aparelho, e aponte a câmera para o código."
                        color: Theme.text2
                        font.pixelSize: Theme.fontSmall
                        lineHeight: 1.3
                        wrapMode: Text.Wrap
                    }
                }

                Row {
                    spacing: 8
                    GhostButton {
                        small: true
                        visible: !page.waLinked && !page.wa.pairing
                        icon: "plus"
                        text: "Conectar"
                        onClicked: page.waConnect()
                    }
                    GhostButton {
                        small: true
                        visible: page.waLinked
                        danger: true
                        text: page.confirm === "whatsapp" ? "Confirmar" : "Desconectar"
                        onClicked: page.waLogout()
                    }
                }

                ErrorBanner {
                    width: parent.width
                    visible: text !== ""
                    text: page.waError || page.wa.error || ""
                }

                Column {
                    width: parent.width
                    spacing: 6
                    Text {
                        text: "FERRAMENTAS  ·  3  ·  1 PEDE APROVAÇÃO"
                        color: Theme.text4
                        font.pixelSize: Theme.fontCaption
                        font.weight: Font.DemiBold
                        font.letterSpacing: Theme.trackCaps
                        bottomPadding: 4
                    }
                    Repeater {
                        model: [
                            { name: "whatsapp_conversas", description: "Lista as conversas recentes, com quem e a última mensagem.", locked: false },
                            { name: "whatsapp_ler", description: "Lê as mensagens de uma conversa.", locked: false },
                            { name: "whatsapp_enviar", description: "Envia uma mensagem de texto para um contato ou número.", locked: true }
                        ]
                        delegate: Item {
                            required property var modelData
                            width: waBody.width
                            height: waToolText.implicitHeight + 8
                            Icon {
                                id: waToolIcon
                                y: 5
                                name: modelData.locked ? "lock" : "check"
                                size: 13
                                color: modelData.locked ? Theme.warn : Theme.text4
                            }
                            Text {
                                id: waToolText
                                anchors { left: waToolIcon.right; leftMargin: 9; right: parent.right; top: parent.top; topMargin: 3 }
                                text: "<span style=\"color:" + Theme.text2 + "\">" + modelData.name + "</span>  " + modelData.description
                                textFormat: Text.StyledText
                                color: Theme.text3
                                font.pixelSize: Theme.fontSmall
                                wrapMode: Text.Wrap
                            }
                        }
                    }
                }

                Text {
                    width: parent.width
                    text: "O WhatsApp não tem API oficial para isso; use com moderação para não ter o número bloqueado. Áudio, foto e documento recebidos ainda não são lidos."
                    color: Theme.text4
                    font.pixelSize: Theme.fontSmall
                    wrapMode: Text.Wrap
                }
            }
        }
    }
}
