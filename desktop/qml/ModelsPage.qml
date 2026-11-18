import QtQuick
import QtQuick.Controls.Basic
import Avora
import "logos.js" as Logos

// Settings → Contas de IA: every AI the agents can think with, in one list,
// connected the same way whatever it is (a subscription's sign-in or an API
// key), as many of each as wanted. There is no main model to pick: each
// agent picks its own brain, and agents on "Automático" (plus the server's
// quick work) use the best AI connected, which the server picks by itself.
Item {
    id: page
    property var info: ({ main: {}, pinned: false, providers: [], kinds: [] })
    property var quotas: []
    property var nodes: []
    property var agents: []
    property bool loaded: false
    property string openId: "" // the expanded row
    property string renaming: ""
    property string busy: ""
    property string error: ""
    property string confirm: ""

    // The connect sheet: pick → key | where → login → done.
    property string step: ""
    property var kindSel: null
    property string apiFormat: "openai"
    property string editId: "" // an API whose key or address is changed
    property string pricingModel: "" // model currently editing prices in openId
    property string loginId: ""
    property var login: ({})
    property string nodeId: ""
    property var added: null

    readonly property var onlineNodes: nodes.filter(n => n.online && !n.revoked_at)
    readonly property var rows: buildRows(info, quotas, nodes, agents)
    readonly property var mainProvider: find(info.main ? info.main.provider : "")
    readonly property var options: connectOptions(info.kinds || [], search.text)

    // ── Deep links: "connect", "connect-<kind>", "conta-<id>" ──────
    property string pendingSheet: ""
    function openSheet(s) {
        pendingSheet = s;
        if (loaded)
            showPending();
    }
    function showPending() {
        const s = pendingSheet.replace(/^(add|account)-/, "connect-");
        pendingSheet = "";
        if (s === "connect" || s === "pick")
            connect();
        else if (s.indexOf("connect-") === 0 && kind(s.slice(8)))
            choose(kind(s.slice(8)));
        else if (s.indexOf("conta-") === 0)
            openId = s.slice(6);
        else if (s.indexOf("login-") === 0 && find(s.slice(6)))
            followLogin(s.slice(6));
    }

    // ── What each kind is, in the person's words ──────────────────
    function isLogin(k) {
        return k === "claude" || k === "codex" || k === "antigravity";
    }
    function kind(k) {
        return (info.kinds || []).find(x => x.kind === k) || null;
    }
    function find(id) {
        return (info.providers || []).find(p => p.id === id) || null;
    }
    function isLocal(k) {
        return !!k && !k.login && k.needs_url && !k.needs_key && k.kind !== "openai";
    }
    function brand(k) {
        return k === "codex" ? "ChatGPT" : k === "antigravity" ? "Google AI" : k === "claude" ? "Claude" : (kind(k) ? kind(k).name : k);
    }
    // What a kind's card in the picker says under its name.
    function tagline(k) {
        if (k.kind === "claude")
            return "Assinatura Pro ou Max · pelo Claude Code";
        if (k.kind === "codex")
            return "Assinatura Plus, Pro ou Business · pelo Codex";
        if (k.kind === "antigravity")
            return "Assinatura Google AI Pro ou Ultra · pelo Antigravity";
        if (isLocal(k))
            return "No seu computador · sem chave";
        return k.kind === "openai" ? "Qualquer API no formato OpenAI ou Anthropic" : "Chave de API · pago por uso";
    }

    function keyPrefixError(kindName, rawKey) {
        const k = (rawKey || "").trim();
        if (k === "")
            return "";
        switch (kindName) {
        case "openrouter":
            return k.indexOf("sk-or-v1-") === 0 ? "" : "As chaves da OpenRouter começam com sk-or-v1-";
        case "anthropic":
            return k.indexOf("sk-ant-") === 0 ? "" : "As chaves da Anthropic começam com sk-ant-";
        case "openai_api":
            return (k.indexOf("sk-") === 0 && k.indexOf("sk-or-v1-") !== 0 && k.indexOf("sk-ant-") !== 0)
                ? "" : "As chaves da OpenAI começam com sk-";
        case "groq":
            return k.indexOf("gsk_") === 0 ? "" : "As chaves da Groq começam com gsk_";
        case "xai":
            return k.indexOf("xai-") === 0 ? "" : "As chaves da xAI começam com xai-";
        case "gemini":
            return k.indexOf("AIza") === 0 ? "" : "As chaves do Gemini começam com AIza";
        }
        return "";
    }
    // Subscriptions first (what the person already pays), then APIs, then
    // servers on their own machines; "em breve" last.
    function connectOptions(kinds, q) {
        const rank = k => k.soon ? 3 : k.login ? 0 : isLocal(k) ? 2 : 1;
        const t = (q || "").trim().toLowerCase();
        return kinds.map((k, i) => ({ k: k, i: i }))
            .filter(x => x.k.login || x.k.main || x.k.soon)
            .filter(x => t === "" || (brand(x.k.kind) + " " + x.k.name + " " + tagline(x.k)).toLowerCase().indexOf(t) >= 0)
            .sort((a, b) => rank(a.k) - rank(b.k) || a.i - b.i)
            .map(x => x.k);
    }
    function nodeName(id) {
        const n = nodes.find(x => x.id === id);
        return n ? n.name : "computador";
    }
    function host(url) {
        return (url || "").replace(/^https?:\/\//, "").replace(/\/v1$/, "").replace(/\/.*$/, "");
    }

    // ── The rows ──────────────────────────────────────────────────
    function quotaOf(p) {
        if (p.id.indexOf("codex@") === 0)
            return quotas.find(q => !q.account && q.engine === "codex" && q.node === nodeName(p.node_id)) || null;
        return quotas.find(q => q.account === p.id) || null;
    }
    function free(w) {
        return w ? Math.max(0, Math.round(100 - w.used_percent)) : -1;
    }
    function resetText(w) {
        if (!w || !w.resets_at)
            return "";
        const ms = (w.resets_at < 1e12 ? w.resets_at * 1000 : w.resets_at) - Date.now();
        if (ms <= 0)
            return "renova agora";
        const h = Math.floor(ms / 3600000);
        if (h >= 24)
            return "renova em " + Math.round(h / 24) + (Math.round(h / 24) === 1 ? " dia" : " dias");
        return h >= 1 ? "renova em " + h + " h" : "renova em " + Math.max(1, Math.round(ms / 60000)) + " min";
    }
    // state: ok | limited | login | error
    function stateOf(p, q) {
        if (p.status === "login")
            return "login";
        if (p.status !== "ok")
            return "error";
        if (q && q.limited)
            return "limited";
        return "ok";
    }
    function lineOf(p, q, state) {
        if (state === "login")
            return "falta entrar";
        if (state === "error")
            return p.last_error || (isLogin(p.kind) ? "o login falhou" : "não respondeu");
        if (state === "limited")
            return "limite atingido" + (q && q.weekly && q.weekly.used_percent >= 100 ? "  ·  " + resetText(q.weekly) : q && q.five_hour ? "  ·  " + resetText(q.five_hour) : "");
        if (isLogin(p.kind)) {
            if (q && !q.error && (q.five_hour || q.weekly)) {
                const parts = [];
                if (q.five_hour)
                    parts.push(free(q.five_hour) + "% das 5 h");
                if (q.weekly)
                    parts.push(free(q.weekly) + "% da semana");
                return parts.join("  ·  ") + " livres";
            }
            return "pronta";
        }
        const n = (p.models || []).length;
        return n === 1 ? "1 modelo" : n + " modelos";
    }
    function titleOf(p) {
        if (p.id === "servidor")
            return "Modelo do servidor";
        if (p.id.indexOf("codex@") === 0)
            return "ChatGPT";
        if (isLogin(p.kind)) {
            const who = p.account && p.account.indexOf("@") > 0 ? p.account : "";
            if (p.name && p.name !== brand(p.kind) && p.name !== "Antigravity" && p.name !== who)
                return p.name;
            return who || brand(p.kind);
        }
        return p.name;
    }
    function subOf(p) {
        if (p.id === "servidor")
            return "Configurado no servidor  ·  " + (p.models && p.models[0] ? p.models[0] : "");
        if (p.id.indexOf("codex@") === 0)
            return "Assinatura  ·  conta do " + nodeName(p.node_id) + ", configurada no computador";
        if (isLogin(p.kind)) {
            const parts = ["Assinatura " + brand(p.kind)];
            const t = titleOf(p);
            if (p.account && p.account.indexOf("@") > 0 && p.account !== t)
                parts.push(p.account);
            if (p.node_id)
                parts.push(nodeName(p.node_id));
            return parts.join("  ·  ");
        }
        const k = kind(p.kind);
        const fmt = p.api_format === "anthropic" ? "API no formato Anthropic" : "API no formato OpenAI";
        return (isLocal(k) ? "No seu computador" : p.kind === "openai" ? fmt : "Chave de API" + (p.has_key ? "" : " (sem chave)")) + "  ·  " + host(p.base_url);
    }
    // Who thinks with it: agents that picked it (or, for a Google account,
    // all of them together), and "Automático" when the server picked it.
    function usersOf(p) {
        return agents.filter(a => {
            const b = a.brain ? a.brain.provider : "";
            return b !== "" && (b === p.id || p.kind === "antigravity" && b === "antigravity");
        }).map(a => a.name);
    }
    function isAuto(p) {
        const m = info.main ? info.main.provider : "";
        return m !== "" && (m === p.id || m === "antigravity" && p.kind === "antigravity" && p.status === "ok");
    }
    function buildRows() {
        const out = [];
        for (const p of info.providers || []) {
            if (p.id === "antigravity")
                continue; // the pool: its accounts are listed one by one
            const q = isLogin(p.kind) ? quotaOf(p) : null;
            const st = stateOf(p, q);
            out.push({
                id: p.id, p: p, kind: p.kind, q: q, state: st, sub: subOf(p),
                title: titleOf(p), line: lineOf(p, q, st), login: isLogin(p.kind),
                fixed: !!p.builtin, users: usersOf(p), auto: isAuto(p)
            });
        }
        // A computer's own Claude login, set up on the computer itself.
        for (const q of quotas) {
            if (q.account || q.engine !== "claude")
                continue;
            const p = { id: "claude@" + q.node, kind: "claude", status: "ok", builtin: true };
            const st = q.limited ? "limited" : "ok";
            out.push({
                id: p.id, p: p, kind: "claude", q: q, state: st, title: "Claude",
                sub: "Assinatura" + (q.plan ? " " + q.plan : "") + "  ·  conta do " + q.node + ", configurada no computador",
                line: lineOf(p, q, st), login: true, fixed: true, users: usersOf({ id: "claude", kind: "claude" }), auto: false
            });
        }
        const rank = r => r.state === "ok" ? 0 : r.state === "limited" ? 1 : r.state === "login" ? 2 : 3;
        out.forEach((r, i) => r.at = i);
        return out.sort((a, b) => (a.login === b.login ? 0 : a.login ? -1 : 1) || rank(a) - rank(b) || a.at - b.at);
    }

    // ── Server calls ──────────────────────────────────────────────
    function load() {
        Api.get("/v1/models", (err, d) => {
            if (err)
                return;
            page.info = d;
            page.loaded = true;
            if (page.pendingSheet !== "")
                page.showPending();
        });
        Api.get("/v1/quotas", (err, d) => {
            if (!err)
                page.quotas = d || [];
        });
        Api.get("/v1/nodes", (err, d) => {
            if (!err)
                page.nodes = d || [];
        });
        Api.get("/v1/agents", (err, d) => {
            if (!err)
                page.agents = d || [];
        });
    }
    function rename(id, name) {
        page.busy = "rename";
        Api.put("/v1/models/providers/" + id, { name: name }, err => {
            page.busy = "";
            page.error = err || "";
            if (!err)
                page.renaming = "";
            page.load();
        });
    }
    function refresh(id) {
        page.busy = "refresh:" + id;
        Api.post("/v1/models/providers/" + id + "/refresh", null, (err, view) => {
            page.busy = "";
            page.error = err ? err : view && view.status === "error" ? view.last_error : "";
            page.load();
        });
    }
    function remove(id) {
        if (confirm !== id) {
            confirm = id;
            disarm.restart();
            return;
        }
        confirm = "";
        Api.del("/v1/models/providers/" + id, err => {
            page.error = err || "";
            if (!err && page.openId === id)
                page.openId = "";
            page.load();
        });
    }
    function unpin() {
        Api.del("/v1/models/main", err => {
            page.error = err || "";
            page.load();
        });
    }

    // ── Connecting ────────────────────────────────────────────────
    function connect() {
        error = "";
        kindSel = null;
        editId = "";
        added = null;
        search.text = "";
        step = "pick";
        sheet.shown = true;
        search.forceActiveFocus();
    }
    function back() {
        error = "";
        step = "pick";
        kindSel = null;
        editId = "";
    }
    // Nodes a subscription can sign in on: Antigravity runs on the Windows PC.
    function placesFor(k) {
        return onlineNodes.filter(n => k.kind !== "antigravity" || n.kind === "windows");
    }
    function choose(k) {
        if (!k || k.soon)
            return;
        error = "";
        kindSel = k;
        editId = "";
        added = null;
        sheet.shown = true;
        if (!k.login) {
            nameField.text = "";
            keyField.text = "";
            urlField.text = k.needs_url ? (k.base_url || "") : "";
            apiFormat = "openai";
            step = "key";
            return;
        }
        const places = placesFor(k);
        if (places.length === 1) {
            nodeId = places[0].id;
            startLogin();
            return;
        }
        const prefer = places.find(n => n.kind === "vps");
        nodeId = places.length > 0 ? (prefer || places[0]).id : "";
        step = "where";
    }
    function editKey(p) {
        error = "";
        kindSel = kind(p.kind);
        editId = p.id;
        added = null;
        nameField.text = p.name;
        keyField.text = "";
        urlField.text = p.base_url || "";
        apiFormat = p.api_format === "anthropic" ? "anthropic" : "openai";
        step = "key";
        sheet.shown = true;
    }
    function saveModelPrice(providerId, modelId, inPrice, outPrice) {
        const meta = {};
        meta[modelId] = {
            input_price: Math.max(0, parseFloat(inPrice) || 0),
            output_price: Math.max(0, parseFloat(outPrice) || 0)
        };
        busy = "price";
        error = "";
        Api.put("/v1/models/providers/" + encodeURIComponent(providerId), { model_meta: meta }, err => {
            page.busy = "";
            page.error = err || "";
            if (!err)
                page.pricingModel = "";
            page.load();
        });
    }
    function saveKey() {
        const body = {
            kind: kindSel.kind,
            name: nameField.text.trim(),
            base_url: urlField.text.trim(),
            api_key: keyField.text.trim()
        };
        if (kindSel.kind === "openai")
            body.api_format = apiFormat;
        busy = "save";
        error = "";
        const done = (err, view) => {
            page.busy = "";
            if (err) {
                page.error = err;
                return;
            }
            keyField.text = "";
            page.load();
            if (view && view.status === "error") {
                page.error = view.last_error || "o provedor não respondeu";
                page.editId = view.id; // saved, but broken: fix it here
                return;
            }
            page.added = view;
            page.step = "done";
        };
        if (editId)
            Api.put("/v1/models/providers/" + editId, body, done);
        else
            Api.post("/v1/models/providers", body, done);
    }
    function startLogin() {
        busy = "save";
        error = "";
        step = "starting";
        Api.post("/v1/models/providers", { kind: kindSel.kind, name: "", node_id: nodeId }, (err, view) => {
            page.busy = "";
            if (err) {
                page.error = err;
                page.step = "where";
                return;
            }
            page.load();
            page.followLogin(view.id);
        });
    }
    function followLogin(id) {
        const p = find(id);
        if (p)
            kindSel = kind(p.kind);
        loginId = id;
        login = { state: "starting" };
        step = "login";
        sheet.shown = true;
        poll.restart();
    }
    function relogin(id) {
        error = "";
        Api.post("/v1/models/providers/" + id + "/login", null, err => {
            if (err)
                page.error = err;
            else
                page.followLogin(id);
        });
    }
    function sendCode(code) {
        busy = "code";
        error = "";
        Api.post("/v1/models/providers/" + loginId + "/login/code", { code: code }, err => {
            page.busy = "";
            page.error = err || "";
            if (!err)
                page.login = Object.assign({}, page.login, { sent: true });
        });
    }

    Component.onCompleted: load()
    onVisibleChanged: if (visible) load()
    Connections {
        target: Api
        function onLiveEvent(e) {
            const t = e && e.type ? e.type : "";
            if (t.indexOf("provider.") === 0 || t.indexOf("agent.") === 0)
                reload.restart();
        }
    }
    Timer {
        id: reload
        interval: 500
        onTriggered: page.load()
    }
    // Follows a sign-in while its sheet is open.
    Timer {
        id: poll
        interval: 1500
        repeat: true
        running: sheet.shown && page.step === "login"
        triggeredOnStart: true
        onTriggered: Api.get("/v1/models/providers/" + page.loginId + "/login", (err, v) => {
            if (err)
                return;
            // A new link (the old one ran out) wants a new code.
            const sent = page.login.sent && v.state === "waiting" && v.url === page.login.url;
            page.login = Object.assign(v, { sent: sent });
            if (v.state === "done" || v.state === "error")
                page.load();
        })
    }
    Timer {
        id: disarm
        interval: 4000
        onTriggered: page.confirm = ""
    }

    // A provider's logo in its color, or a line icon.
    component Mark: Item {
        property string k
        property real side: 36
        width: side
        height: side
        readonly property string logo: Logos.of(k)
        readonly property string icon: logo !== "" ? "" : k === "openai_api" || k === "codex" ? "openai" : k === "local" ? "monitor" : k === "openai" ? "plug" : "bolt"
        Icon {
            anchors.centerIn: parent
            name: parent.icon
            path: parent.logo
            size: parent.side * 0.66
            color: Logos.color(parent.k) !== "" ? Logos.color(parent.k) : Theme.text
        }
    }
    // One window of a subscription's limits, as a bar.
    component Meter: Column {
        property string label
        property var w: null
        readonly property int freePct: page.free(w)
        spacing: Theme.space1
        Item {
            width: parent.width
            height: meterLabel.height
            Text {
                id: meterLabel
                text: parent.parent.label
                color: Theme.text3
                font.pixelSize: Theme.fontSmall
            }
            Text {
                anchors.right: parent.right
                text: parent.parent.freePct + "% livre" + (page.resetText(parent.parent.w) !== "" ? "  ·  " + page.resetText(parent.parent.w) : "")
                color: parent.parent.freePct <= 10 ? Theme.warn : Theme.text3
                font.pixelSize: Theme.fontSmall
            }
        }
        Rectangle {
            width: parent.width
            height: 6
            radius: 3
            color: Theme.wash3
            Rectangle {
                width: parent.width * Math.max(0, Math.min(1, (100 - parent.parent.freePct) / 100))
                height: parent.height
                radius: 3
                color: parent.parent.freePct <= 10 ? Theme.bad : parent.parent.freePct <= 30 ? Theme.warn : Theme.text2
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
            x: Theme.gutter
            y: 52
            width: Math.min(scroller.width - 2 * Theme.gutter, 980)
            spacing: Theme.space5

            Item {
                width: parent.width
                height: head.height
                PageHead {
                    id: head
                    width: parent.width - connectBtn.width - Theme.space4
                    eyebrow: "Configurações"
                    title: "Contas de IA"
                    sub: "Conecte quantas quiser: assinaturas que você já paga ou chaves de API. Cada agente escolhe com qual pensa."
                }
                PrimaryButton {
                    id: connectBtn
                    anchors { right: parent.right; bottom: parent.bottom }
                    visible: page.rows.length > 0
                    text: "Conectar uma IA"
                    icon: "plus"
                    onClicked: page.connect()
                }
            }

            // ── Automático: what agents without a brain use ──────
            Rectangle {
                width: parent.width
                height: autoRow.height + 2 * Theme.space3
                radius: Theme.radiusMd
                color: Theme.wash1
                border.width: 1
                border.color: Theme.line
                visible: page.loaded && page.rows.length > 0
                Row {
                    id: autoRow
                    x: Theme.space4
                    anchors.verticalCenter: parent.verticalCenter
                    width: parent.width - 2 * Theme.space4
                    spacing: Theme.space3
                    Icon {
                        anchors.verticalCenter: parent.verticalCenter
                        name: "spark"
                        size: 16
                        color: page.mainProvider ? Theme.text2 : Theme.warn
                    }
                    Text {
                        anchors.verticalCenter: parent.verticalCenter
                        width: parent.width - 16 - Theme.space3 - (unpinBtn.visible ? unpinBtn.width + Theme.space3 : 0)
                        textFormat: Text.StyledText
                        wrapMode: Text.WordWrap
                        color: Theme.text3
                        font.pixelSize: Theme.fontSmall
                        text: {
                            const p = page.mainProvider;
                            if (!p)
                                return "Nenhuma conta pronta agora: os agentes no <b>Automático</b> ficam sem responder até uma voltar.";
                            const m = page.info.main.model ? "  ·  " + ((p.model_names && p.model_names[page.info.main.model]) || page.info.main.model) : "";
                            const who = p.id === "antigravity" ? "Google AI (todas as contas)" : page.titleOf(p);
                            return "Agentes no <b>Automático</b> e as tarefas rápidas usam <font color=\"" + Theme.text + "\">" + who + m + "</font>"
                                + (page.info.pinned ? ", fixado por uma versão antiga." : ". O Avora escolhe sozinho a melhor conta pronta.");
                        }
                    }
                    GhostButton {
                        id: unpinBtn
                        anchors.verticalCenter: parent.verticalCenter
                        visible: !!page.info.pinned
                        small: true
                        text: "Voltar ao automático"
                        onClicked: page.unpin()
                    }
                }
            }

            // ── Nothing connected yet ─────────────────────────────
            Card {
                width: parent.width
                height: emptyCol.implicitHeight + 2 * padding
                padding: Theme.space6
                visible: page.loaded && page.rows.length === 0
                Column {
                    id: emptyCol
                    width: parent.width
                    spacing: Theme.space3
                    Row {
                        spacing: Theme.space2
                        Repeater {
                            model: ["claude", "codex", "antigravity", "openrouter"]
                            delegate: Mark {
                                required property string modelData
                                k: modelData
                                side: 30
                            }
                        }
                    }
                    Text {
                        text: "Use a IA que você já paga"
                        color: Theme.white
                        font.family: Theme.display
                        font.pixelSize: Theme.fontTitle
                        font.weight: Font.DemiBold
                    }
                    Text {
                        width: Math.min(parent.width, 620)
                        text: "Entre com a sua assinatura do Claude, do ChatGPT ou do Google AI, ou cole uma chave de API (OpenRouter, Anthropic, OpenAI, Gemini e outras). Pode conectar várias contas do mesmo tipo."
                        color: Theme.text3
                        font.pixelSize: Theme.fontBody
                        wrapMode: Text.WordWrap
                    }
                    PrimaryButton {
                        text: "Conectar uma IA"
                        icon: "plus"
                        onClicked: page.connect()
                    }
                }
            }

            // ── Everything connected, one list ────────────────────
            Column {
                width: parent.width
                spacing: Theme.space2
                visible: page.rows.length > 0
                Overline {
                    text: page.rows.length === 1 ? "1 CONECTADA" : page.rows.length + " CONECTADAS"
                }
                Repeater {
                    model: page.rows
                    delegate: Rectangle {
                        id: row
                        required property var modelData
                        readonly property var r: modelData
                        readonly property bool expanded: page.openId === r.id
                        width: col.width
                        height: rowBody.implicitHeight
                        radius: Theme.radiusLg
                        color: expanded ? Theme.card : headArea.containsMouse ? Theme.hover : Theme.wash1
                        border.width: 1
                        border.color: expanded ? Theme.lineStrong : Theme.line
                        Behavior on color { ColorAnimation { duration: Theme.fast } }

                        Column {
                            id: rowBody
                            width: parent.width

                            // Head: mark, title, what it is, state.
                            Item {
                                width: parent.width
                                height: 68
                                MouseArea {
                                    id: headArea
                                    anchors.fill: parent
                                    hoverEnabled: true
                                    cursorShape: Qt.PointingHandCursor
                                    onClicked: {
                                        page.openId = row.expanded ? "" : row.r.id;
                                        page.renaming = "";
                                        page.confirm = "";
                                    }
                                }
                                Item {
                                    id: markBox
                                    x: Theme.space4
                                    width: 38
                                    height: 38
                                    anchors.verticalCenter: parent.verticalCenter
                                    QuotaRing {
                                        anchors.centerIn: parent
                                        visible: row.r.login
                                        engine: row.r.kind
                                        size: 38
                                        weekly: row.r.q && !row.r.q.error && row.r.q.weekly ? page.free(row.r.q.weekly) : -1
                                        fiveHour: row.r.q && !row.r.q.error && row.r.q.five_hour ? page.free(row.r.q.five_hour) : -1
                                        limited: row.r.state === "limited"
                                    }
                                    Mark {
                                        anchors.centerIn: parent
                                        visible: !row.r.login
                                        k: row.r.kind
                                        side: 34
                                    }
                                }
                                Column {
                                    anchors { left: markBox.right; leftMargin: Theme.space3; right: stateBox.left; rightMargin: Theme.space4; verticalCenter: parent.verticalCenter }
                                    spacing: 3
                                    Row {
                                        width: parent.width
                                        spacing: Theme.space2
                                        Text {
                                            id: titleText
                                            width: Math.min(implicitWidth, parent.width - (autoTag.visible ? autoTag.width + Theme.space2 : 0))
                                            text: row.r.title
                                            color: Theme.white
                                            font.pixelSize: Theme.fontLarge
                                            font.weight: Font.DemiBold
                                            elide: Text.ElideMiddle
                                        }
                                        Rectangle {
                                            id: autoTag
                                            visible: row.r.auto
                                            anchors.verticalCenter: titleText.verticalCenter
                                            width: autoText.implicitWidth + 2 * Theme.space2
                                            height: autoText.implicitHeight + 4
                                            radius: height / 2
                                            color: Theme.wash3
                                            Text {
                                                id: autoText
                                                anchors.centerIn: parent
                                                text: "AUTOMÁTICO"
                                                color: Theme.text2
                                                font.pixelSize: Theme.fontTiny
                                                font.weight: Font.DemiBold
                                                font.letterSpacing: Theme.trackCaps
                                            }
                                        }
                                    }
                                    Text {
                                        width: parent.width
                                        text: row.r.sub
                                        color: Theme.text3
                                        font.pixelSize: Theme.fontSmall
                                        elide: Text.ElideRight
                                    }
                                }
                                Row {
                                    id: stateBox
                                    anchors { right: parent.right; rightMargin: Theme.space4; verticalCenter: parent.verticalCenter }
                                    spacing: Theme.space3
                                    Row {
                                        anchors.verticalCenter: parent.verticalCenter
                                        spacing: Theme.space2
                                        Dot {
                                            anchors.verticalCenter: parent.verticalCenter
                                            kind: row.r.state === "ok" ? "live" : row.r.state === "limited" ? "bad" : row.r.state === "login" ? "" : "warn"
                                        }
                                        Text {
                                            anchors.verticalCenter: parent.verticalCenter
                                            width: Math.min(implicitWidth, 280)
                                            text: row.r.line
                                            color: row.r.state === "ok" ? Theme.text2 : row.r.state === "login" ? Theme.text3 : Theme.warn
                                            font.pixelSize: Theme.fontSmall
                                            font.weight: Font.Medium
                                            elide: Text.ElideRight
                                        }
                                    }
                                    GhostButton {
                                        anchors.verticalCenter: parent.verticalCenter
                                        visible: !row.r.fixed && (row.r.state === "login" || row.r.state === "error")
                                        small: true
                                        text: row.r.login ? "Entrar" : page.isLocal(page.kind(row.r.kind)) ? "Trocar endereço" : "Trocar chave"
                                        onClicked: row.r.login ? page.relogin(row.r.id) : page.editKey(row.r.p)
                                    }
                                    Icon {
                                        anchors.verticalCenter: parent.verticalCenter
                                        name: "chevron"
                                        size: 14
                                        color: Theme.text4
                                        rotation: row.expanded ? 90 : 0
                                        Behavior on rotation { NumberAnimation { duration: Theme.fast } }
                                    }
                                }
                            }

                            // Details: limits, who uses it, what can be done.
                            Column {
                                id: details
                                visible: row.expanded
                                x: Theme.space4 + 38 + Theme.space3
                                width: parent.width - x - Theme.space4
                                spacing: Theme.space4
                                bottomPadding: Theme.space4

                                Rectangle {
                                    width: parent.width
                                    height: 1
                                    color: Theme.line
                                }
                                Grid {
                                    width: parent.width
                                    columns: 2
                                    columnSpacing: Theme.space5
                                    rowSpacing: Theme.space3
                                    visible: !!row.r.q && !row.r.q.error && (!!row.r.q.five_hour || !!row.r.q.weekly)
                                    Meter {
                                        width: (details.width - Theme.space5) / 2
                                        visible: !!row.r.q && !!row.r.q.five_hour
                                        label: "Últimas 5 horas"
                                        w: row.r.q ? row.r.q.five_hour : null
                                    }
                                    Meter {
                                        width: (details.width - Theme.space5) / 2
                                        visible: !!row.r.q && !!row.r.q.weekly
                                        label: "Semana"
                                        w: row.r.q ? row.r.q.weekly : null
                                    }
                                }
                                Text {
                                    width: parent.width
                                    visible: row.r.login && (!row.r.q || !!row.r.q.error)
                                    text: row.r.q && row.r.q.error ? "Não deu para ler os limites agora: " + row.r.q.error
                                        : row.r.state === "ok" ? "Os limites aparecem depois do primeiro uso."
                                        : "Os limites aparecem quando a conta estiver pronta."
                                    color: Theme.text4
                                    font.pixelSize: Theme.fontSmall
                                    wrapMode: Text.WordWrap
                                }
                                Text {
                                    width: parent.width
                                    textFormat: Text.StyledText
                                    wrapMode: Text.WordWrap
                                    color: Theme.text3
                                    font.pixelSize: Theme.fontSmall
                                    text: (row.r.users.length === 0
                                        ? (row.r.auto ? "Nenhum agente escolheu esta conta, mas ela responde pelos que estão no Automático."
                                                      : "Nenhum agente pensa com ela ainda. Escolha-a na página do agente, em Configurações → Cérebro.")
                                        : "Pensam com ela: <font color=\"" + Theme.text + "\">" + row.r.users.join(", ") + "</font>"
                                          + (row.r.auto ? ", e os agentes no Automático." : "."))
                                        + (row.r.kind === "antigravity" ? " As contas Google AI trabalham juntas: o Avora usa a mais livre e pula para a próxima no limite." : "")
                                }

                                // Models, tool support & optional token pricing ($/1M)
                                Column {
                                    width: parent.width
                                    spacing: Theme.space2
                                    visible: !row.r.login && !row.r.fixed && (row.r.p.models || []).length > 0
                                    Text {
                                        text: "Modelos (ferramentas e preço opcional por 1M tokens)"
                                        color: Theme.text3
                                        font.pixelSize: Theme.fontSmall
                                    }
                                    Repeater {
                                        model: (row.r.p.models || []).slice(0, 8)
                                        delegate: Column {
                                            id: mItem
                                            required property string modelData
                                            readonly property var meta: row.r.p.model_meta && row.r.p.model_meta[modelData] ? row.r.p.model_meta[modelData] : null
                                            readonly property bool editingPrice: page.pricingModel === (row.r.id + ":" + modelData)
                                            width: details.width
                                            spacing: Theme.space2

                                            Row {
                                                width: parent.width
                                                spacing: Theme.space2
                                                Text {
                                                    text: mItem.modelData
                                                    color: Theme.text
                                                    font.family: Theme.mono
                                                    font.pixelSize: Theme.fontSmall
                                                    anchors.verticalCenter: parent.verticalCenter
                                                }
                                                Rectangle {
                                                    visible: mItem.meta !== null && mItem.meta.tools !== undefined
                                                    width: toolBadgeTxt.implicitWidth + Theme.space2 * 2
                                                    height: toolBadgeTxt.implicitHeight + Theme.space1
                                                    radius: height / 2
                                                    color: Theme.wash2
                                                    anchors.verticalCenter: parent.verticalCenter
                                                    Text {
                                                        id: toolBadgeTxt
                                                        anchors.centerIn: parent
                                                        text: mItem.meta && mItem.meta.tools ? "Com ferramentas" : "Sem ferramentas"
                                                        color: mItem.meta && mItem.meta.tools ? Theme.ok : Theme.text4
                                                        font.pixelSize: Theme.fontTiny
                                                    }
                                                }
                                                Text {
                                                    visible: !!(mItem.meta && (mItem.meta.input_price > 0 || mItem.meta.output_price > 0))
                                                    text: mItem.meta ? ("US$ " + (mItem.meta.input_price || 0) + " / US$ " + (mItem.meta.output_price || 0) + " por 1M") : ""
                                                    color: Theme.text3
                                                    font.pixelSize: Theme.fontSmall
                                                    anchors.verticalCenter: parent.verticalCenter
                                                }
                                                GhostButton {
                                                    small: true
                                                    text: mItem.editingPrice ? "Fechar" : "Preço"
                                                    anchors.verticalCenter: parent.verticalCenter
                                                    onClicked: {
                                                        const key = row.r.id + ":" + mItem.modelData;
                                                        if (page.pricingModel === key) {
                                                            page.pricingModel = "";
                                                        } else {
                                                            page.pricingModel = key;
                                                            inPriceField.text = mItem.meta && mItem.meta.input_price ? String(mItem.meta.input_price) : "";
                                                            outPriceField.text = mItem.meta && mItem.meta.output_price ? String(mItem.meta.output_price) : "";
                                                        }
                                                    }
                                                }
                                            }

                                            Row {
                                                visible: mItem.editingPrice
                                                spacing: Theme.space2
                                                Field {
                                                    id: inPriceField
                                                    width: 150
                                                    placeholderText: "Entrada (US$/1M)"
                                                }
                                                Field {
                                                    id: outPriceField
                                                    width: 150
                                                    placeholderText: "Saída (US$/1M)"
                                                }
                                                PrimaryButton {
                                                    text: page.busy === "price" ? "Salvando…" : "Salvar preço"
                                                    busy: page.busy === "price"
                                                    enabled: page.busy === ""
                                                    onClicked: page.saveModelPrice(row.r.id, mItem.modelData, inPriceField.text, outPriceField.text)
                                                }
                                            }
                                        }
                                    }
                                }

                                // Rename, inline.
                                Item {
                                    width: parent.width
                                    height: 42
                                    visible: page.renaming === row.r.id
                                    Field {
                                        id: renameField
                                        anchors { left: parent.left; right: renameSave.left; rightMargin: Theme.space2; verticalCenter: parent.verticalCenter }
                                        placeholderText: "Nome (ex.: Trabalho, Pessoal)"
                                        onAccepted: if (text.trim() !== "") page.rename(row.r.id, text.trim())
                                    }
                                    PrimaryButton {
                                        id: renameSave
                                        anchors { right: parent.right; verticalCenter: parent.verticalCenter }
                                        text: "Salvar"
                                        busy: page.busy === "rename"
                                        enabled: renameField.text.trim() !== ""
                                        onClicked: page.rename(row.r.id, renameField.text.trim())
                                    }
                                }

                                Flow {
                                    width: parent.width
                                    spacing: Theme.space2
                                    visible: !row.r.fixed
                                    GhostButton {
                                        small: true
                                        icon: "pen"
                                        text: "Renomear"
                                        visible: page.renaming !== row.r.id
                                        onClicked: {
                                            page.renaming = row.r.id;
                                            renameField.text = row.r.p.name || "";
                                            renameField.forceActiveFocus();
                                        }
                                    }
                                    GhostButton {
                                        small: true
                                        icon: "refresh"
                                        visible: row.r.login
                                        text: "Entrar de novo"
                                        onClicked: page.relogin(row.r.id)
                                    }
                                    GhostButton {
                                        small: true
                                        icon: "lock"
                                        visible: !row.r.login
                                        text: page.isLocal(page.kind(row.r.kind)) ? "Trocar endereço" : "Trocar chave"
                                        onClicked: page.editKey(row.r.p)
                                    }
                                    GhostButton {
                                        small: true
                                        icon: "refresh"
                                        visible: !row.r.login
                                        text: page.busy === "refresh:" + row.r.id ? "Atualizando…" : "Atualizar modelos"
                                        enabled: page.busy === ""
                                        onClicked: page.refresh(row.r.id)
                                    }
                                    GhostButton {
                                        small: true
                                        danger: true
                                        icon: "trash"
                                        text: page.confirm === row.r.id ? "Confirmar: desconectar" : "Desconectar"
                                        onClicked: page.remove(row.r.id)
                                    }
                                }
                                Text {
                                    width: parent.width
                                    visible: row.r.fixed
                                    text: row.r.id === "servidor"
                                        ? "Vem das variáveis de ambiente do servidor (AVORA_LLM_*). Para tirar, mude lá."
                                        : "Configurada direto no computador. Para trocar de conta, entre nela de novo lá, no terminal."
                                    color: Theme.text4
                                    font.pixelSize: Theme.fontSmall
                                    wrapMode: Text.WordWrap
                                }
                            }
                        }
                    }
                }
            }

            ErrorBanner {
                width: parent.width
                visible: !sheet.shown && page.error !== ""
                text: page.error
            }
        }
    }

    // ── Conectar uma IA ─────────────────────────────────────────────
    Sheet {
        id: sheet
        anchors.fill: parent
        sheetWidth: page.step === "pick" ? 760 : 560
        onShownChanged: if (!shown) {
            page.step = "";
            page.login = ({});
            page.confirm = "";
            page.error = "";
            pasteField.text = "";
            keyField.text = "";
        }

        // Header
        Item {
            width: sheet.form.width
            height: 44
            GhostButton {
                id: backBtn
                anchors.verticalCenter: parent.verticalCenter
                visible: page.step === "key" && page.editId === "" || page.step === "where"
                round: true
                small: true
                icon: "back"
                onClicked: page.back()
            }
            Mark {
                id: sheetMark
                anchors { left: backBtn.visible ? backBtn.right : parent.left; leftMargin: backBtn.visible ? Theme.space2 : 0; verticalCenter: parent.verticalCenter }
                visible: page.step !== "pick" && !!page.kindSel
                k: page.kindSel ? page.kindSel.kind : ""
                side: 36
            }
            Column {
                anchors { left: sheetMark.visible ? sheetMark.right : backBtn.visible ? backBtn.right : parent.left; leftMargin: sheetMark.visible || backBtn.visible ? Theme.space3 : 0; right: closeBtn.left; rightMargin: Theme.space2; verticalCenter: parent.verticalCenter }
                spacing: 2
                Text {
                    width: parent.width
                    text: page.step === "pick" || !page.kindSel ? "Conectar uma IA"
                        : page.editId !== "" ? (page.find(page.editId) ? page.find(page.editId).name : page.kindSel.name)
                        : page.kindSel.login ? "Sua assinatura " + page.brand(page.kindSel.kind)
                        : page.kindSel.name
                    color: Theme.white
                    font.family: Theme.display
                    font.pixelSize: Theme.fontTitle
                    font.weight: Font.DemiBold
                    elide: Text.ElideRight
                }
                Text {
                    width: parent.width
                    text: page.step === "pick" ? "Assinatura ou chave de API: escolha e conecte. Dá para ter várias do mesmo tipo."
                        : page.kindSel ? page.tagline(page.kindSel) : ""
                    color: Theme.text3
                    font.pixelSize: Theme.fontSmall
                    elide: Text.ElideRight
                }
            }
            GhostButton {
                id: closeBtn
                anchors { right: parent.right; verticalCenter: parent.verticalCenter }
                round: true
                small: true
                icon: "close"
                onClicked: sheet.shown = false
            }
        }

        // ── Pick: every kind, one grid ──────────────────────────
        Column {
            width: sheet.form.width
            spacing: Theme.space3
            visible: page.step === "pick"
            Field {
                id: search
                width: parent.width
                placeholderText: "Buscar (Claude, OpenRouter, Ollama…)"
            }
            Flow {
                width: parent.width
                spacing: Theme.space2
                Repeater {
                    model: page.step === "pick" ? page.options : []
                    delegate: Rectangle {
                        id: opt
                        required property var modelData
                        readonly property var k: modelData
                        readonly property int have: page.rows.filter(r => r.kind === k.kind).length
                        width: Math.floor((sheet.form.width - 2 * Theme.space2) / 3)
                        height: 86
                        radius: Theme.radiusMd
                        color: optArea.containsMouse && !k.soon ? Theme.hover : Theme.wash1
                        border.width: 1
                        border.color: optArea.containsMouse && !k.soon ? Theme.lineStrong : Theme.line
                        opacity: k.soon ? 0.5 : 1
                        Mark {
                            id: optMark
                            x: Theme.space3
                            y: Theme.space3
                            k: opt.k.kind
                            side: 28
                        }
                        Text {
                            anchors { left: optMark.right; leftMargin: Theme.space2; right: haveText.left; rightMargin: Theme.space1; verticalCenter: optMark.verticalCenter }
                            text: page.brand(opt.k.kind)
                            color: Theme.white
                            font.pixelSize: Theme.fontBody
                            font.weight: Font.DemiBold
                            elide: Text.ElideRight
                        }
                        Text {
                            id: haveText
                            anchors { right: parent.right; rightMargin: Theme.space3; verticalCenter: optMark.verticalCenter }
                            text: opt.k.soon ? "EM BREVE" : opt.have > 0 ? (opt.have === 1 ? "1 conectada" : opt.have + " conectadas") : ""
                            color: Theme.text4
                            font.pixelSize: Theme.fontTiny
                            font.weight: Font.DemiBold
                        }
                        Text {
                            anchors { left: parent.left; leftMargin: Theme.space3; right: parent.right; rightMargin: Theme.space3; top: optMark.bottom; topMargin: Theme.space2 }
                            text: page.tagline(opt.k)
                            color: Theme.text3
                            font.pixelSize: Theme.fontSmall
                            wrapMode: Text.WordWrap
                            maximumLineCount: 2
                            elide: Text.ElideRight
                        }
                        MouseArea {
                            id: optArea
                            anchors.fill: parent
                            hoverEnabled: true
                            enabled: !opt.k.soon
                            cursorShape: Qt.PointingHandCursor
                            onClicked: page.choose(opt.k)
                        }
                    }
                }
            }
            Text {
                visible: page.options.length === 0
                text: "Nada com esse nome. Para qualquer outra API no formato OpenAI, use \"Outro\"."
                color: Theme.text3
                font.pixelSize: Theme.fontSmall
            }
        }

        // ── An API key (or a local server's address) ────────────
        Column {
            id: keyFormCol
            width: sheet.form.width
            spacing: Theme.space3
            visible: page.step === "key"
            readonly property string liveKeyErr: page.kindSel ? page.keyPrefixError(page.kindSel.kind, keyField.text) : ""

            Text {
                width: parent.width
                visible: page.editId === ""
                text: page.kindSel ? page.kindSel.description || "" : ""
                color: Theme.text2
                font.pixelSize: Theme.fontBody
                wrapMode: Text.WordWrap
            }
            Row {
                visible: !!page.kindSel && page.kindSel.kind === "openai"
                spacing: Theme.space3
                Text {
                    text: "Formato"
                    color: Theme.text3
                    font.pixelSize: Theme.fontSmall
                    anchors.verticalCenter: parent.verticalCenter
                }
                Segmented {
                    value: page.apiFormat
                    options: [
                        { value: "openai", label: "Formato OpenAI" },
                        { value: "anthropic", label: "Formato Anthropic" }
                    ]
                    onActivated: v => page.apiFormat = v
                }
            }
            Field {
                id: urlField
                width: parent.width
                mono: true
                visible: !!page.kindSel && (page.kindSel.needs_url || page.editId !== "")
                placeholderText: page.kindSel ? (page.kindSel.placeholder || page.kindSel.base_url || "https://…/v1") : ""
            }
            Field {
                id: keyField
                width: parent.width
                mono: true
                echoMode: TextInput.Password
                placeholderText: page.editId !== "" && page.find(page.editId) && page.find(page.editId).has_key ? "Chave salva (cole outra para trocar)"
                    : page.kindSel && page.kindSel.needs_key ? "Cole a chave da API" : "Chave da API (opcional)"
                onAccepted: if (connectKey.enabled) page.saveKey()
            }
            Text {
                visible: keyFormCol.liveKeyErr !== ""
                width: parent.width
                text: keyFormCol.liveKeyErr
                color: Theme.warn
                font.pixelSize: Theme.fontSmall
                wrapMode: Text.WordWrap
            }
            Field {
                id: nameField
                width: parent.width
                placeholderText: page.kindSel ? "Nome (opcional, ex.: " + page.kindSel.name + " Trabalho)" : "Nome (opcional)"
            }
            Text {
                width: parent.width
                visible: text !== ""
                text: page.kindSel ? (page.kindSel.help || "") : ""
                color: Theme.text3
                font.pixelSize: Theme.fontSmall
                wrapMode: Text.WordWrap
            }
            Text {
                width: parent.width
                text: "A chave vai cifrada para o cofre do servidor e nunca volta para o app."
                color: Theme.text4
                font.pixelSize: Theme.fontSmall
                wrapMode: Text.WordWrap
                visible: !!page.kindSel && !page.isLocal(page.kindSel)
            }
            ErrorBanner {
                width: parent.width
                text: page.step === "key" ? page.error : ""
            }
            Item {
                width: parent.width
                height: 42
                PrimaryButton {
                    id: connectKey
                    anchors { right: parent.right; verticalCenter: parent.verticalCenter }
                    text: page.busy === "save" ? "Testando…" : page.editId !== "" ? "Salvar e testar" : "Conectar"
                    busy: page.busy === "save"
                    enabled: page.busy === "" && keyFormCol.liveKeyErr === "" && !!page.kindSel && (page.editId !== "" || ((!page.kindSel.needs_url || urlField.text.trim() !== "") && (!page.kindSel.needs_key || keyField.text.trim() !== "")))
                    onClicked: page.saveKey()
                }
            }
        }

        // ── Where a subscription signs in ───────────────────────
        Column {
            width: sheet.form.width
            spacing: Theme.space3
            visible: page.step === "where" || page.step === "starting"
            Text {
                width: parent.width
                text: page.kindSel && page.kindSel.kind === "antigravity"
                    ? "A conta entra pelo Antigravity no PC com Windows. O Google mostra a página de login; a senha e o token ficam no PC."
                    : "A conta entra num dos seus computadores e fica lá. O Avora só repassa o link e o código do login, nunca vê a senha nem o token."
                color: Theme.text2
                font.pixelSize: Theme.fontBody
                wrapMode: Text.WordWrap
            }
            Column {
                width: parent.width
                spacing: Theme.space2
                visible: !!page.kindSel && page.placesFor(page.kindSel).length > 1
                Text {
                    text: "Em qual computador"
                    color: Theme.text3
                    font.pixelSize: Theme.fontSmall
                }
                Segmented {
                    options: page.kindSel ? page.placesFor(page.kindSel).map(n => ({ value: n.id, label: n.name })) : []
                    value: page.nodeId
                    onActivated: v => page.nodeId = v
                }
            }
            ErrorBanner {
                width: parent.width
                text: page.step !== "where" ? ""
                    : page.kindSel && page.placesFor(page.kindSel).length === 0
                        ? (page.kindSel.kind === "antigravity" ? "O PC com Windows precisa estar ligado e online para entrar numa conta Google AI." : "Nenhum computador online agora. Ligue um (ou instale o Avora nele) e tente de novo.")
                        : page.error
            }
            Item {
                width: parent.width
                height: 42
                PrimaryButton {
                    anchors { right: parent.right; verticalCenter: parent.verticalCenter }
                    text: page.step === "starting" ? "Abrindo…" : "Entrar"
                    busy: page.step === "starting"
                    enabled: page.step === "where" && page.nodeId !== ""
                    onClicked: page.startLogin()
                }
            }
        }

        // ── A sign-in, step by step ─────────────────────────────
        Column {
            width: sheet.form.width
            spacing: Theme.space4
            visible: page.step === "login"
            readonly property bool agy: !!page.kindSel && page.kindSel.kind === "antigravity"

            Row {
                spacing: Theme.space2
                visible: page.login.state === "starting" || (page.login.state === "waiting" && !page.login.url)
                Dot { kind: "warn"; anchors.verticalCenter: parent.verticalCenter }
                Text {
                    text: parent.parent.agy ? "Abrindo o login do Google no PC…" : "Abrindo o login no computador…"
                    color: Theme.text2
                    font.pixelSize: Theme.fontBody
                }
            }
            // 1. Open the page
            Column {
                width: parent.width
                spacing: Theme.space2
                visible: page.login.state === "waiting" && !!page.login.url
                Text {
                    text: parent.parent.agy ? "1  ·  Abra a página e escolha a conta Google" : "1  ·  Abra a página e entre na conta"
                    color: Theme.white
                    font.pixelSize: Theme.fontBody
                    font.weight: Font.DemiBold
                }
                Row {
                    spacing: Theme.space2
                    PrimaryButton {
                        text: "Abrir página"
                        icon: "globe"
                        onClicked: Qt.openUrlExternally(page.login.url)
                    }
                    GhostButton {
                        text: "Copiar link"
                        icon: "copy"
                        anchors.verticalCenter: parent.verticalCenter
                        onClicked: Api.copyText(page.login.url)
                    }
                }
            }
            // 2a. ChatGPT: the device code to type there
            Column {
                width: parent.width
                spacing: Theme.space2
                visible: page.login.state === "waiting" && !!page.login.code
                Text {
                    text: "2  ·  Digite este código na página"
                    color: Theme.white
                    font.pixelSize: Theme.fontBody
                    font.weight: Font.DemiBold
                }
                Rectangle {
                    width: codeText.implicitWidth + 40
                    height: 54
                    radius: Theme.radiusLg
                    color: Theme.well
                    border.width: 1
                    border.color: Theme.lineStrong
                    Text {
                        id: codeText
                        anchors.centerIn: parent
                        text: page.login.code || ""
                        color: Theme.white
                        font.family: Theme.mono
                        font.pixelSize: Theme.fontDisplay
                        font.weight: Font.DemiBold
                        font.letterSpacing: 3
                    }
                    MouseArea {
                        anchors.fill: parent
                        cursorShape: Qt.PointingHandCursor
                        onClicked: Api.copyText(page.login.code)
                    }
                }
                Text {
                    width: parent.width
                    text: "No ChatGPT, o login por código precisa estar ligado em Configurações → Segurança."
                    color: Theme.text4
                    font.pixelSize: Theme.fontSmall
                    wrapMode: Text.WordWrap
                }
            }
            // 2b. Claude and Google: paste the code the page shows
            Column {
                width: parent.width
                spacing: Theme.space2
                visible: page.login.state === "waiting" && !!page.login.needs_code
                Text {
                    text: parent.parent.agy ? "2  ·  Se a página mostrar um código, cole aqui" : "2  ·  Cole aqui o código que a página mostrar"
                    color: Theme.white
                    font.pixelSize: Theme.fontBody
                    font.weight: Font.DemiBold
                }
                Text {
                    width: parent.width
                    visible: parent.parent.agy
                    text: "O Google dá cerca de um minuto por link; se passar, um link novo aparece aqui sozinho."
                    color: Theme.text4
                    font.pixelSize: Theme.fontSmall
                    wrapMode: Text.WordWrap
                }
                Item {
                    width: parent.width
                    height: 42
                    Field {
                        id: pasteField
                        anchors { left: parent.left; right: sendBtn.left; rightMargin: Theme.space2 }
                        mono: true
                        placeholderText: "Código de autorização"
                        onAccepted: if (text.trim() !== "") page.sendCode(text.trim())
                    }
                    PrimaryButton {
                        id: sendBtn
                        anchors.right: parent.right
                        text: page.busy === "code" ? "Enviando…" : "Enviar"
                        enabled: pasteField.text.trim() !== "" && page.busy === "" && !page.login.sent
                        onClicked: page.sendCode(pasteField.text.trim())
                    }
                }
            }
            Row {
                spacing: Theme.space2
                visible: page.login.state === "waiting" && !!page.login.url
                Dot { kind: "live"; anchors.verticalCenter: parent.verticalCenter }
                Text {
                    text: page.login.sent ? "Código enviado, confirmando…" : "Esperando você entrar…"
                    color: Theme.text3
                    font.pixelSize: Theme.fontSmall
                }
            }
            // Done
            Row {
                spacing: Theme.space3
                visible: page.login.state === "done"
                Icon { name: "check"; size: 20; color: Theme.live; anchors.verticalCenter: parent.verticalCenter }
                Column {
                    spacing: 2
                    Text {
                        text: "Conta conectada"
                        color: Theme.white
                        font.pixelSize: Theme.fontLarge
                        font.weight: Font.DemiBold
                    }
                    Text {
                        text: page.find(page.loginId) && page.find(page.loginId).account ? page.find(page.loginId).account + "  ·  os agentes já podem usar." : "Os agentes já podem usar."
                        color: Theme.text3
                        font.pixelSize: Theme.fontSmall
                    }
                }
            }
            ErrorBanner {
                width: parent.width
                text: page.step === "login" ? (page.login.state === "error" ? (page.login.error || "o login falhou") : page.error) : ""
            }
            Item {
                width: parent.width
                height: 42
                visible: page.login.state === "done" || page.login.state === "error"
                GhostButton {
                    anchors { left: parent.left; verticalCenter: parent.verticalCenter }
                    visible: page.login.state === "error"
                    text: "Tentar de novo"
                    icon: "refresh"
                    onClicked: page.relogin(page.loginId)
                }
                Row {
                    anchors { right: parent.right; verticalCenter: parent.verticalCenter }
                    spacing: Theme.space2
                    GhostButton {
                        visible: page.login.state === "done"
                        text: "Conectar outra"
                        icon: "plus"
                        anchors.verticalCenter: parent.verticalCenter
                        onClicked: page.connect()
                    }
                    PrimaryButton {
                        text: "Pronto"
                        anchors.verticalCenter: parent.verticalCenter
                        onClicked: sheet.shown = false
                    }
                }
            }
        }

        // ── An API connected ────────────────────────────────────
        Column {
            width: sheet.form.width
            spacing: Theme.space4
            visible: page.step === "done"
            Row {
                spacing: Theme.space3
                Icon { name: "check"; size: 20; color: Theme.live; anchors.verticalCenter: parent.verticalCenter }
                Column {
                    spacing: 2
                    Text {
                        text: page.editId !== "" ? "Salvo e testado" : "Conectada"
                        color: Theme.white
                        font.pixelSize: Theme.fontLarge
                        font.weight: Font.DemiBold
                    }
                    Text {
                        text: page.added ? ((page.added.models || []).length === 1 ? "1 modelo disponível" : (page.added.models || []).length + " modelos disponíveis") + ". Escolha-a como cérebro de um agente." : ""
                        color: Theme.text3
                        font.pixelSize: Theme.fontSmall
                    }
                }
            }
            Item {
                width: parent.width
                height: 42
                Row {
                    anchors { right: parent.right; verticalCenter: parent.verticalCenter }
                    spacing: Theme.space2
                    GhostButton {
                        text: "Conectar outra"
                        icon: "plus"
                        anchors.verticalCenter: parent.verticalCenter
                        onClicked: page.connect()
                    }
                    PrimaryButton {
                        text: "Pronto"
                        anchors.verticalCenter: parent.verticalCenter
                        onClicked: sheet.shown = false
                    }
                }
            }
        }
    }
}
