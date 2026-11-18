import QtQuick
import QtQuick.Controls.Basic
import QtWebEngine
import Avora

// One agent's browser inside the app: a Chromium view with its own profile
// (storageName = "agent-" + agentId) so cookies and sign-ins are isolated per
// agent. "Assumir" hands control to the person while the agent waits; "Devolver"
// gives it back.
Rectangle {
    id: panel
    property string agentId: ""
    property string agentName: ""
    property bool embedded: false
    property bool open: false
    signal openRequested
    signal closeRequested

    property bool profileReady: false
    property string script: ""      // the page reader, from the server
    property int maxText: 9000
    property bool polling: false
    property bool owner: false      // a person took over this browser
    property string driver: ""      // the agent driving now
    property string lastAction: ""
    property real drivenAt: 0

    readonly property var view: viewLoader.item
    readonly property string currentUrl: view ? view.url.toString() : "about:blank"
    readonly property string currentHost: {
        const u = currentUrl;
        if (!u || u === "about:blank")
            return "";
        return u.replace(/^https?:\/\/(www\.)?/, "").split(/[/?#]/)[0];
    }

    color: Theme.bg
    clip: true

    // Wait until agentId is bound before instantiating the WebEngineProfile.
    WebEngineProfilePrototype {
        id: proto
        storageName: panel.agentId !== "" ? "agent-" + panel.agentId : "navegador-" + Qt.application.name
        persistentCookiesPolicy: WebEngineProfile.ForcePersistentCookies
        httpCacheType: WebEngineProfile.DiskHttpCache
    }

    Component.onCompleted: {
        profileReady = true;
        if (Api.signedIn) {
            loadScript();
            if (!polling)
                poll();
        }
    }

    // ── Agent commands ──────────────────────────────────────────
    function after(ms, fn) {
        const t = Qt.createQmlObject("import QtQuick; Timer {}", panel);
        t.interval = ms;
        t.triggered.connect(() => {
            t.destroy();
            fn();
        });
        t.start();
    }

    function executeCall(call) {
        if (!call || !call.id)
            return;
        panel.driver = call.agent_name || panel.agentName || call.agent || panel.agentId;
        panel.drivenAt = Date.now();
        panel.run(call.command || {}, res => {
            panel.drivenAt = Date.now();
            Api.post("/v1/browser/app/result", { id: call.id, agent: panel.agentId, result: res }, () => {
                if (!panel.polling)
                    panel.poll();
            });
        });
    }

    function poll() {
        if (!Api.signedIn || !panel.profileReady) {
            polling = false;
            return;
        }
        polling = true;
        const body = panel.agentId !== "" ? { agent: panel.agentId } : {};
        Api.post("/v1/browser/app/next", body, (err, call) => {
            if (err || !call || !call.id) {
                panel.after(err ? 4000 : 50, panel.poll);
                return;
            }
            panel.driver = call.agent_name || panel.agentName || call.agent || panel.agentId;
            panel.drivenAt = Date.now();
            panel.run(call.command || {}, res => {
                panel.drivenAt = Date.now();
                Api.post("/v1/browser/app/result", { id: call.id, agent: panel.agentId, result: res }, () => panel.poll());
            });
        });
    }

    function loadScript() {
        Api.get("/v1/browser/script", (err, d) => {
            if (!err) {
                panel.script = d.snapshot;
                panel.maxText = d.max || 9000;
            }
        });
    }

    Connections {
        target: Api
        function onSignedInChanged() {
            if (Api.signedIn && panel.profileReady) {
                panel.loadScript();
                if (!panel.polling)
                    panel.poll();
            }
        }
    }

    Timer {
        interval: 5000
        repeat: true
        running: Api.signedIn && panel.profileReady
        triggeredOnStart: true
        onTriggered: {
            if (panel.script === "")
                panel.loadScript();
            if (!panel.polling)
                panel.poll();
            const q = panel.agentId !== "" ? "?agent=" + encodeURIComponent(panel.agentId) : "";
            Api.get("/v1/browser" + q, (err, list) => {
                if (err)
                    return;
                const me = (list || []).find(v => v.node_id === "app");
                if (me)
                    panel.owner = me.owner;
            });
        }
    }

    // finish answers a command with the page's address, title and a print.
    function finish(done, res) {
        const out = res || {};
        out.ok = !out.error;
        out.url = view ? view.url.toString() : "";
        out.title = view ? view.title : "";
        const shot = view ? WebInput.grab(view) : "";
        if (shot !== "")
            out.shot = shot;
        done(out);
    }
    function fail(done, why) {
        finish(done, { error: why });
    }
    // settle waits for the page to load, then a beat for scripts to draw.
    function settle(fn, tries) {
        tries = tries === undefined ? 120 : tries;
        if (view && view.loading && tries > 0)
            after(250, () => settle(fn, tries - 1));
        else
            after(700, fn);
    }
    function read(part, done) {
        if (script === "")
            return fail(done, "o leitor de páginas ainda não carregou");
        if (!view)
            return fail(done, "o navegador ainda não abriu");
        view.runJavaScript("(" + script + ")(" + maxText + ", " + (part || 1) + ")", r => {
            try {
                const s = JSON.parse(r);
                finish(done, { text: s.text, elements: s.elements });
            } catch (e) {
                fail(done, "não consegui ler a página");
            }
        });
    }
    function point(ref, fn) {
        if (!view)
            return fn({ error: "o navegador ainda não abriu" });
        view.runJavaScript(`(() => {
  const el = (window.__avoraRefs || [])[${ref}];
  if (!el || !el.isConnected) return {error: "o elemento ${ref} não está mais na página; leia a página de novo"};
  const a = el.closest && el.closest("a[target]"); if (a) a.target = "_self";
  el.scrollIntoView({block: "center", inline: "center", behavior: "instant"});
  const r = el.getBoundingClientRect();
  const kind = el.type === "password" ? "password" : el.isContentEditable ? "editable" : el.tagName.toLowerCase();
  return {x: r.left + Math.min(r.width / 2, 40 + r.width / 4), y: r.top + r.height / 2, kind};
})()`, fn);
    }
    function cleanUrl(u) {
        u = (u || "").trim();
        if (u === "")
            return "";
        if (u.indexOf("://") < 0)
            u = "https://" + u;
        return /^https?:\/\//.test(u) ? u : "";
    }

    function run(cmd, done) {
        if (!open || !view) {
            openRequested();
            after(400, () => run(cmd, done)); // laid out and on screen first
            return;
        }
        switch (cmd.action) {
        case "abrir": {
            const u = cleanUrl(cmd.url);
            if (u === "")
                return fail(done, "endereço inválido (só http e https)");
            lastAction = "abrindo " + u.replace(/^https?:\/\/(www\.)?/, "").split("/")[0];
            view.url = u;
            return after(300, () => settle(() => read(1, done)));
        }
        case "ler":
            lastAction = "lendo a página";
            return read(cmd.part, done);
        case "clicar":
        case "digitar":
            return point(cmd.ref || 0, p => {
                if (!p || p.error)
                    return fail(done, p ? p.error : "elemento não encontrado");
                if (cmd.action === "digitar" && p.kind === "password")
                    return fail(done, "senha é a pessoa quem digita: peça para ela entrar no site pelo painel do navegador");
                after(150, () => {
                    WebInput.click(view, p.x, p.y);
                    if (cmd.action === "clicar") {
                        lastAction = "clicando";
                        return after(300, () => settle(() => read(1, done)));
                    }
                    lastAction = "digitando";
                    after(200, () => {
                        WebInput.key(view, "selectall");
                        WebInput.insertText(view, cmd.text || "");
                        if (cmd.enter) {
                            after(200, () => {
                                WebInput.key(view, "enter");
                                after(300, () => settle(() => read(1, done)));
                            });
                        } else {
                            after(500, () => read(1, done));
                        }
                    });
                });
            });
        case "rolar": {
            lastAction = "rolando";
            let times = Math.max(1, Math.min(10, cmd.times || 2));
            const step = () => {
                WebInput.wheel(view, cmd.down === false ? -700 : 700);
                if (--times > 0)
                    after(450, step);
                else
                    after(400, () => settle(() => read(1, done)));
            };
            return step();
        }
        case "tecla":
            if (!WebInput.key(view, cmd.text || ""))
                return fail(done, "tecla não suportada (enter, esc, tab, pagedown, pageup, baixo, cima)");
            return after(300, () => settle(() => read(1, done)));
        case "voltar":
            if (!view.canGoBack)
                return fail(done, "não há página anterior");
            view.goBack();
            return after(300, () => settle(() => read(1, done)));
        case "abas":
            return finish(done, { text: "Há uma aba só: links que abririam outra aba abrem nesta." });
        case "print":
            return finish(done, { text: "Print tirado." });
        case "fechar":
            view.url = "about:blank";
            return finish(done, { text: "Navegador limpo." });
        }
        fail(done, "ação desconhecida: " + cmd.action);
    }

    // ── Chrome ──────────────────────────────────────────────────
    readonly property bool driving: driver !== "" && Date.now() - drivenAt < 15000 && tick.t >= 0
    Timer {
        id: tick
        property int t: 0
        interval: 1000
        repeat: true
        running: panel.driver !== ""
        onTriggered: {
            t++;
            if (Date.now() - panel.drivenAt > 15000)
                panel.driver = "";
        }
    }

    Rectangle {
        visible: !panel.embedded
        anchors { left: parent.left; top: parent.top; bottom: parent.bottom }
        width: 1
        color: Theme.line
    }

    Column {
        id: bar
        anchors { left: parent.left; right: parent.right; top: parent.top; leftMargin: panel.embedded ? 0 : 1 }
        Item {
            width: parent.width
            height: 44
            Row {
                id: nav
                x: Theme.space2
                anchors.verticalCenter: parent.verticalCenter
                spacing: 2
                RowTool {
                    icon: "back"
                    tip: "Voltar"
                    opacity: panel.view && panel.view.canGoBack ? 1 : 0.35
                    onHit: if (panel.view) panel.view.goBack()
                }
                RowTool {
                    icon: "back"
                    tip: "Avançar"
                    rotation: 180
                    opacity: panel.view && panel.view.canGoForward ? 1 : 0.35
                    onHit: if (panel.view) panel.view.goForward()
                }
                RowTool {
                    icon: panel.view && panel.view.loading ? "close" : "refresh"
                    tip: panel.view && panel.view.loading ? "Parar" : "Recarregar"
                    onHit: if (panel.view) { panel.view.loading ? panel.view.stop() : panel.view.reload(); }
                }
            }
            Rectangle {
                id: addressBox
                anchors { left: nav.right; leftMargin: Theme.space2; right: tools.left; rightMargin: Theme.space2; verticalCenter: parent.verticalCenter }
                height: 30
                radius: Theme.radiusSm
                color: Theme.card
                border.width: 1
                border.color: address.activeFocus ? Theme.lineStrong : Theme.line
                Icon {
                    id: lock
                    x: Theme.space2
                    anchors.verticalCenter: parent.verticalCenter
                    name: panel.currentUrl.startsWith("https://") ? "lock" : "globe"
                    size: 12
                    color: Theme.text4
                }
                TextInput {
                    id: address
                    anchors { left: lock.right; leftMargin: 7; right: parent.right; rightMargin: Theme.space2; verticalCenter: parent.verticalCenter }
                    color: Theme.text
                    selectionColor: Theme.wash6
                    font.pixelSize: Theme.fontBody
                    clip: true
                    selectByMouse: true
                    text: activeFocus ? text : panel.currentUrl === "about:blank" ? "" : panel.currentUrl
                    onActiveFocusChanged: if (activeFocus) selectAll()
                    onAccepted: {
                        let u = text.trim();
                        if (u !== "" && !/^https?:\/\//.test(u))
                            u = /\s/.test(u) || u.indexOf(".") < 0 ? "https://www.google.com/search?q=" + encodeURIComponent(u) : "https://" + u;
                        if (u !== "" && panel.view) {
                            panel.view.url = u;
                            panel.view.forceActiveFocus();
                        }
                    }
                    Text {
                        anchors.verticalCenter: parent.verticalCenter
                        visible: address.text === "" && !address.activeFocus
                        text: "Endereço ou busca"
                        color: Theme.text4
                        font.pixelSize: Theme.fontBody
                    }
                }
            }
            Row {
                id: tools
                anchors { right: parent.right; rightMargin: Theme.space2; verticalCenter: parent.verticalCenter }
                spacing: 6
                Rectangle {
                    height: 28
                    width: takeText.implicitWidth + 20
                    radius: Theme.radiusSm
                    color: panel.owner ? Qt.rgba(Theme.warn.r, Theme.warn.g, Theme.warn.b, 0.14) : takeArea.containsMouse ? Theme.active : "transparent"
                    border.width: 1
                    border.color: panel.owner ? Qt.rgba(Theme.warn.r, Theme.warn.g, Theme.warn.b, 0.4) : Theme.line
                    Text {
                        id: takeText
                        anchors.centerIn: parent
                        text: panel.owner ? "Devolver" : "Assumir"
                        color: panel.owner ? Theme.warn : Theme.text2
                        font.pixelSize: Theme.fontSmall
                        font.weight: panel.owner ? Font.DemiBold : Font.Normal
                    }
                    MouseArea {
                        id: takeArea
                        anchors.fill: parent
                        hoverEnabled: true
                        cursorShape: Qt.PointingHandCursor
                        onClicked: {
                            const next = !panel.owner;
                            panel.owner = next;
                            Api.post("/v1/browser/app/control", { agent: panel.agentId, take: next }, () => {});
                        }
                    }
                    ToolTip.visible: takeArea.containsMouse
                    ToolTip.delay: 500
                    ToolTip.text: panel.owner ? "Devolve o navegador para o agente continuar." : "Assume o controle enquanto o agente espera."
                }
                RowTool {
                    visible: !panel.embedded
                    anchors.verticalCenter: parent.verticalCenter
                    icon: "close"
                    tip: "Fechar o painel"
                    onHit: panel.closeRequested()
                }
            }
        }
        // Takeover / live driving strip.
        Rectangle {
            width: parent.width
            height: panel.driving || panel.owner ? 30 : 0
            visible: height > 0
            color: panel.owner ? Qt.rgba(Theme.warn.r, Theme.warn.g, Theme.warn.b, 0.08) : Qt.rgba(Theme.live.r, Theme.live.g, Theme.live.b, 0.08)
            Behavior on height { NumberAnimation { duration: Theme.fast } }
            Row {
                x: Theme.space3
                width: parent.width - 2 * Theme.space3
                anchors.verticalCenter: parent.verticalCenter
                spacing: Theme.space2
                Dot {
                    anchors.verticalCenter: parent.verticalCenter
                    kind: panel.owner ? "warn" : "live"
                }
                Text {
                    width: parent.width - 16
                    text: panel.owner ? "Você está no controle — o agente espera"
                          : (panel.agentName || panel.driver || "O agente") + " está navegando" + (panel.lastAction !== "" ? " · " + panel.lastAction : "")
                    color: panel.owner ? Theme.warn : Theme.text2
                    font.pixelSize: Theme.fontSmall
                    elide: Text.ElideRight
                }
            }
        }
        Rectangle {
            width: parent.width
            height: 1
            color: Theme.line
        }
    }

    Loader {
        id: viewLoader
        anchors { left: parent.left; right: parent.right; top: bar.bottom; bottom: parent.bottom; leftMargin: panel.embedded ? 0 : 1 }
        active: panel.profileReady
        sourceComponent: WebEngineView {
            id: webView
            profile: proto.instance()
            url: "about:blank"
            // Links that open another window open here: one tab, in view.
            onNewWindowRequested: request => webView.url = request.requestedUrl
            onFeaturePermissionRequested: (origin, feature) => webView.grantFeaturePermission(origin, feature, false)
            Component.onCompleted: {
                // Sites refuse "QtWebEngine" in the user agent; look like Chrome.
                const p = webView.profile;
                p.httpUserAgent = p.httpUserAgent.replace(/ QtWebEngine\/[\d.]+/, "");
                p.httpAcceptLanguage = "pt-BR,pt;q=0.9,en;q=0.8";
                // Set here, not as bindings on the view: declaring settings.* there
                // crashes QtWebEngine 6.11 at startup.
                webView.settings.javascriptCanOpenWindows = false;
                webView.settings.focusOnNavigationEnabled = false;
            }
        }
    }

    // A glow on the edge while the agent drives.
    Rectangle {
        anchors.fill: viewLoader
        color: "transparent"
        border.width: panel.driving ? 2 : 0
        border.color: Qt.rgba(Theme.live.r, Theme.live.g, Theme.live.b, 0.55)
        visible: panel.driving
    }

    // Empty state.
    Column {
        anchors.centerIn: viewLoader
        width: Math.min(viewLoader.width - 48, 360)
        spacing: Theme.space3
        visible: panel.currentUrl === "about:blank" && !(panel.view && panel.view.loading)
        Icon {
            anchors.horizontalCenter: parent.horizontalCenter
            name: "globe"
            size: 32
            color: Theme.text3
        }
        Text {
            width: parent.width
            horizontalAlignment: Text.AlignHCenter
            wrapMode: Text.Wrap
            text: panel.agentName !== ""
                  ? "Navegador exclusivo de " + panel.agentName + ". Entre nas contas que este agente pode usar; cookies e logins ficam separados dos outros."
                  : "Entre nas contas uma vez aqui; o agente usa esta sessão para o que a busca não alcança. Enviar, publicar ou comprar sempre pede sua aprovação."
            color: Theme.text2
            font.pixelSize: Theme.fontBody
            lineHeight: 1.3
        }
        Flow {
            width: parent.width
            spacing: 6
            Repeater {
                model: [
                    { label: "Facebook", url: "https://www.facebook.com/" },
                    { label: "Marketplace", url: "https://www.facebook.com/marketplace/" },
                    { label: "Instagram", url: "https://www.instagram.com/" },
                    { label: "WhatsApp Web", url: "https://web.whatsapp.com/" },
                    { label: "Mercado Livre", url: "https://www.mercadolivre.com.br/" },
                    { label: "OLX", url: "https://www.olx.com.br/" }
                ]
                delegate: GhostButton {
                    required property var modelData
                    small: true
                    text: modelData.label
                    onClicked: if (panel.view) panel.view.url = modelData.url
                }
            }
        }
    }
}
