import QtQuick
import QtQuick.Controls.Basic
import QtWebEngine
import Avora
import "util.js" as U

// The Docs beside the page, like Claude's artifacts: the list (search,
// drag a doc into the chat to cite it) or one doc — a Markdown document
// drawn with the chat's own pieces, an HTML page running isolated, an
// image, a PDF's text. Edit it in place (each save is a version), go back
// to an older version, pin it for every agent, cite it in the chat. An
// agent's edit shows up live.
Rectangle {
    id: panel
    property string docId: DocBus.openId
    property bool wide: false
    property bool embedded: false
    signal closeRequested
    signal wideToggled

    property var doc: null      // the doc shown, with its content
    property int shown: 0       // an older version being looked at; 0 = the current one
    property bool loading: false
    property bool editing: false
    property bool saving: false
    property bool sourceMode: false
    property bool versionsOpen: false
    property var versions: []
    property bool infoOpen: false
    property string error: ""
    property string notice: ""
    property string undoId: ""  // a doc just trashed, for Desfazer
    property bool stale: false  // someone saved while the owner was editing
    property var agents: []
    property string query: ""
    property var hits: []       // a search's results (list view)

    readonly property bool old: shown > 0 && doc && shown !== doc.version
    readonly property bool textual: doc && doc.kind !== "file"

    color: Theme.bg
    clip: true

    Rectangle {
        visible: !panel.embedded
        anchors { left: parent.left; top: parent.top; bottom: parent.bottom }
        width: 1
        color: Theme.line
    }

    Component.onCompleted: {
        if (docId !== "")
            load();
        if (visible && Api.signedIn && agents.length === 0)
            Api.get("/v1/agents", (err, data) => { if (!err) agents = data; });
    }

    onDocIdChanged: {
        editing = false;
        versionsOpen = false;
        infoOpen = false;
        sourceMode = false;
        shown = 0;
        stale = false;
        error = "";
        doc = null;
        load();
    }
    // The panel exists from the start (signed out too): it asks for nothing
    // until it is shown.
    onVisibleChanged: if (visible && Api.signedIn && agents.length === 0) Api.get("/v1/agents", (err, data) => { if (!err) agents = data; })

    function load() {
        if (docId === "" || !Api.signedIn)
            return;
        const id = docId;
        loading = true;
        Api.get("/v1/docs/" + id + (shown > 0 ? "?version=" + shown : ""), (err, d) => {
            if (id !== panel.docId)
                return;
            loading = false;
            if (err) {
                error = err.indexOf("not found") >= 0 ? "Este doc não existe mais." : err;
                doc = null;
                return;
            }
            error = "";
            doc = d;
            if (versionsOpen)
                loadVersions();
            if (DocBus.editNext === d.id) {
                DocBus.editNext = "";
                startEdit();
            }
        });
    }
    function loadVersions() {
        if (docId === "")
            return;
        Api.get("/v1/docs/" + docId + "/versions", (err, data) => { if (!err) versions = data; });
    }
    function flash(text) {
        notice = text;
        noticeTimer.restart();
    }
    Timer {
        id: noticeTimer
        interval: 4200
        onTriggered: {
            panel.notice = "";
            panel.undoId = "";
        }
    }
    function startEdit() {
        if (!textual || old)
            return;
        editor.text = doc.content || "";
        noteField.text = "";
        stale = false;
        editing = true;
        Qt.callLater(() => editor.forceActiveFocus());
    }
    function save() {
        if (!editing || saving || !doc)
            return;
        saving = true;
        Api.patch("/v1/docs/" + doc.id, { content: editor.text, note: noteField.text.trim(), base_version: doc.version }, (err, d) => {
            saving = false;
            if (err) {
                error = err;
                return;
            }
            error = "";
            editing = false;
            stale = false;
            doc = d;
            flash(d.version > 1 ? "Salvo como v" + d.version : "Salvo");
            if (versionsOpen)
                loadVersions();
        });
    }
    function patch(body, done) {
        Api.patch("/v1/docs/" + doc.id, body, (err, d) => {
            if (err) {
                error = err;
                return;
            }
            error = "";
            if (shown === 0)
                doc = d;
            if (done)
                done(d);
        });
    }
    function togglePin() {
        const on = !doc.pinned;
        patch({ pinned: on }, () => flash(on ? "Fixado: todo agente vê o título e o resumo" : "Desafixado"));
    }
    function restoreVersion(n) {
        if (!doc || n < 1)
            return;
        Api.post("/v1/docs/" + doc.id + "/versions/" + n + "/restore", {}, (err, d) => {
            if (err) {
                error = err;
                return;
            }
            error = "";
            shown = 0;
            doc = d;
            DocBus.load();
            flash("A v" + n + " virou a v" + d.version);
            loadVersions();
        });
    }
    function restoreShown() {
        restoreVersion(shown);
    }
    function trash() {
        const id = doc.id, title = doc.title;
        Api.del("/v1/docs/" + id, err => {
            if (err) {
                error = err;
                return;
            }
            undoId = id;
            DocBus.openId = "";
            flash("«" + title + "» foi para a lixeira");
        });
    }
    function undoTrash() {
        const id = undoId;
        undoId = "";
        Api.post("/v1/docs/" + id + "/restore", {}, err => {
            if (!err)
                DocBus.open(id);
        });
    }
    function download() {
        if (doc.kind === "file") {
            Api.openFile(doc.file_id, doc.title, err => { if (err) error = err; });
            return;
        }
        Api.saveText(DocBus.fileName(doc), doc.content || "", false, (err, path) => {
            if (err)
                error = err;
            else {
                flash("Salvo em Downloads\\Avora");
                Api.showInFolder(path);
            }
        });
    }
    function openOutside() {
        Api.saveText(DocBus.fileName(doc), doc.content || "", true, err => { if (err) error = err; });
    }
    function search() {
        const q = query.trim();
        if (q === "") {
            hits = [];
            return;
        }
        Api.get("/v1/docs?limit=60&q=" + encodeURIComponent(q), (err, data) => {
            if (!err && q === panel.query.trim())
                hits = data;
        });
    }
    Timer {
        id: searchDelay
        interval: 220
        onTriggered: panel.search()
    }
    onQueryChanged: searchDelay.restart()

    // A doc changed on the server: the one open reloads (unless the owner
    // is typing in it), a search runs again.
    Connections {
        target: DocBus
        function onChanged(id, type) {
            if (panel.query.trim() !== "")
                searchDelay.restart();
            if (id !== panel.docId || panel.docId === "")
                return;
            if (type === "doc.trashed" || type === "doc.deleted") {
                if (!panel.editing)
                    panel.error = "Este doc foi para a lixeira.";
                return;
            }
            if (panel.editing) {
                Api.get("/v1/docs/" + id, (err, d) => {
                    if (!err && panel.doc && d.version !== panel.doc.version)
                        panel.stale = true;
                });
                return;
            }
            if (panel.shown === 0)
                panel.load();
            if (panel.versionsOpen)
                panel.loadVersions();
        }
    }

    function authorName(a) {
        if (!a || a === "admin")
            return "você";
        if (a.startsWith("agent:")) {
            const ag = agents.find(x => x.id === a.slice(6));
            return ag ? ag.name : a.slice(6);
        }
        return a;
    }
    function authorTint(a) {
        const ag = a && a.startsWith("agent:") ? agents.find(x => x.id === a.slice(6)) : null;
        return ag && ag.color ? ag.color : Theme.text2;
    }

    Shortcut {
        sequence: "Ctrl+S"
        enabled: panel.visible && panel.editing
        onActivated: panel.save()
    }
    Shortcut {
        sequence: "Esc"
        enabled: panel.visible && (panel.editing || panel.versionsOpen || panel.infoOpen)
        onActivated: {
            if (panel.infoOpen)
                panel.infoOpen = false;
            else if (panel.versionsOpen)
                panel.versionsOpen = false;
            else
                panel.editing = false;
        }
    }

    // ── Header ──────────────────────────────────────────────────
    Item {
        id: head
        anchors { left: parent.left; right: parent.right; top: parent.top; leftMargin: panel.embedded ? 0 : 1 }
        height: panel.embedded ? 50 : 58

        // The list's header.
        Row {
            visible: panel.docId === "" && !panel.embedded
            x: 18
            anchors.verticalCenter: parent.verticalCenter
            spacing: 10
            Icon {
                anchors.verticalCenter: parent.verticalCenter
                name: "docs"
                size: 18
                color: Theme.text
            }
            Text {
                anchors.verticalCenter: parent.verticalCenter
                text: "Docs"
                color: Theme.white
                font.pixelSize: Theme.fontTitle
                font.weight: Font.DemiBold
            }
            Text {
                anchors.verticalCenter: parent.verticalCenter
                text: DocBus.docs.length ? DocBus.docs.length + (DocBus.docs.length === 1 ? " doc" : " docs") : ""
                color: Theme.text4
                font.pixelSize: Theme.fontSmall
            }
        }

        // A doc's header: back to the list, the doc (drag it to cite), tools.
        Item {
            visible: panel.docId !== ""
            anchors { left: parent.left; top: parent.top; bottom: parent.bottom; right: panel.embedded ? parent.right : winTools.left; rightMargin: panel.embedded ? Theme.space2 : 6 }
            RowTool {
                id: backBtn
                x: panel.embedded ? Theme.space2 : 10
                anchors.verticalCenter: parent.verticalCenter
                icon: "back"
                tip: "Todos os docs"
                onHit: DocBus.openId = ""
            }
            Rectangle {
                id: grip
                anchors { left: backBtn.right; leftMargin: panel.embedded ? Theme.space1 : 6; verticalCenter: parent.verticalCenter }
                width: panel.embedded ? 28 : 34
                height: width
                radius: Theme.radiusMd
                color: gripHover.hovered ? Theme.active : Theme.hover
                border.width: 1
                border.color: Theme.line
                Icon {
                    anchors.centerIn: parent
                    name: DocBus.icon(panel.doc)
                    size: 17
                    weight: 1.5
                    color: DocBus.tone(panel.doc)
                }
                Drag.dragType: Drag.Automatic
                Drag.supportedActions: Qt.CopyAction
                Drag.mimeData: ({ "text/x-avora-doc": panel.docId, "text/plain": panel.doc ? DocBus.link(panel.doc) : "" })
                Drag.active: gripDrag.active && panel.doc !== null
                HoverHandler {
                    id: gripHover
                    cursorShape: Qt.OpenHandCursor
                }
                DragHandler {
                    id: gripDrag
                    target: null
                }
                Tip {
                    text: "Arraste para o campo de mensagem para citar"
                    show: gripHover.hovered && !gripDrag.active
                }
            }
            Column {
                anchors { left: grip.right; leftMargin: 11; right: tools.left; rightMargin: 10; verticalCenter: parent.verticalCenter }
                spacing: 2
                Text {
                    width: parent.width
                    text: panel.doc ? panel.doc.title : panel.loading ? "Abrindo…" : ""
                    color: Theme.white
                    font.pixelSize: Theme.fontLarge
                    font.weight: Font.DemiBold
                    elide: Text.ElideRight
                    MouseArea {
                        anchors.fill: parent
                        enabled: panel.doc !== null && !panel.old
                        cursorShape: Qt.PointingHandCursor
                        onClicked: panel.infoOpen = !panel.infoOpen
                    }
                }
                Text {
                    width: parent.width
                    visible: panel.doc !== null
                    textFormat: Text.StyledText
                    text: {
                        const d = panel.doc;
                        if (!d)
                            return "";
                        const parts = [DocBus.kindLabel(d) + (d.kind === "file" ? " · " + DocBus.size(d.size) : " · v" + (panel.old ? panel.shown + " de " + d.version : d.version))];
                        if (d.folder)
                            parts.push(U.esc(d.folder));
                        const ed = d.editor || d.author;
                        parts.push("<font color='" + panel.authorTint(ed) + "'>" + U.esc(panel.authorName(ed)) + "</font> " + U.ago(d.updated_at));
                        return parts.join("  ·  ");
                    }
                    color: Theme.text3
                    font.pixelSize: Theme.fontSmall
                    elide: Text.ElideRight
                }
            }
            Row {
                id: tools
                anchors { right: parent.right; verticalCenter: parent.verticalCenter }
                spacing: 2
                visible: panel.doc !== null
                RowTool {
                    icon: "quote"
                    tip: "Citar no chat"
                    onHit: DocBus.cite(panel.doc)
                }
                RowTool {
                    icon: "pin"
                    tip: panel.doc && panel.doc.pinned ? "Desafixar" : "Fixar"
                    color: panel.doc && panel.doc.pinned ? Qt.rgba(0.95, 0.8, 0.5, 0.16) : hovered ? Theme.active : "transparent"
                    onHit: panel.togglePin()
                }
                RowTool {
                    visible: panel.textual && !panel.old
                    icon: "pen"
                    tip: "Editar"
                    color: panel.editing ? Theme.active : hovered ? Theme.active : "transparent"
                    onHit: panel.editing ? (panel.editing = false) : panel.startEdit()
                }
                RowTool {
                    visible: panel.doc && panel.doc.kind === "html" && !panel.editing
                    icon: "code"
                    tip: panel.sourceMode ? "Ver a página" : "Ver o código"
                    color: panel.sourceMode ? Theme.active : hovered ? Theme.active : "transparent"
                    onHit: panel.sourceMode = !panel.sourceMode
                }
                RowTool {
                    icon: "history"
                    tip: "Versões"
                    color: panel.versionsOpen ? Theme.active : hovered ? Theme.active : "transparent"
                    onHit: {
                        panel.versionsOpen = !panel.versionsOpen;
                        if (panel.versionsOpen)
                            panel.loadVersions();
                    }
                }
                RowTool {
                    icon: "more"
                    tip: "Mais"
                    color: moreMenu.visible ? Theme.active : hovered ? Theme.active : "transparent"
                    onHit: moreMenu.visible ? moreMenu.close() : moreMenu.open()
                    Popup {
                        id: moreMenu
                        y: parent.height + 6
                        x: parent.width - width
                        width: 236
                        padding: 6
                        background: Rectangle {
                            radius: Theme.radiusMd
                            color: "#141418"
                            border.width: 1
                            border.color: Theme.lineStrong
                        }
                        contentItem: Column {
                            spacing: 1
                            Repeater {
                                model: {
                                    const d = panel.doc;
                                    if (!d)
                                        return [];
                                    const out = [{ k: "info", icon: "pen", label: "Título, pasta e resumo" }];
                                    if (d.kind !== "file")
                                        out.push({ k: "copy", icon: "copy", label: "Copiar o texto" });
                                    out.push({ k: "download", icon: "download", label: d.kind === "file" ? "Abrir no app do PC" : "Baixar (" + (d.kind === "html" ? ".html" : ".md") + ")" });
                                    if (d.kind === "html")
                                        out.push({ k: "outside", icon: "globe", label: "Abrir no navegador" });
                                    out.push({ k: "link", icon: "paste", label: "Copiar o link para citar" });
                                    out.push({ k: "trash", icon: "trash", label: "Mover para a lixeira", danger: true });
                                    return out;
                                }
                                delegate: Rectangle {
                                    required property var modelData
                                    width: 224
                                    height: 34
                                    radius: Theme.radiusSm
                                    color: itemArea.containsMouse ? Theme.hover : "transparent"
                                    Icon {
                                        x: 10
                                        anchors.verticalCenter: parent.verticalCenter
                                        name: modelData.icon
                                        size: 14
                                        color: modelData.danger ? Theme.bad : Theme.text2
                                    }
                                    Text {
                                        x: 34
                                        anchors.verticalCenter: parent.verticalCenter
                                        text: modelData.label
                                        color: modelData.danger ? Theme.bad : Theme.text
                                        font.pixelSize: Theme.fontBody
                                    }
                                    MouseArea {
                                        id: itemArea
                                        anchors.fill: parent
                                        hoverEnabled: true
                                        cursorShape: Qt.PointingHandCursor
                                        onClicked: {
                                            moreMenu.close();
                                            switch (modelData.k) {
                                            case "info": panel.infoOpen = true; break;
                                            case "copy": Api.copyText(panel.doc.content || ""); panel.flash("Texto copiado"); break;
                                            case "download": panel.download(); break;
                                            case "outside": panel.openOutside(); break;
                                            case "link": Api.copyText(DocBus.link(panel.doc)); panel.flash("Link copiado: cole numa mensagem"); break;
                                            case "trash": panel.trash(); break;
                                            }
                                        }
                                    }
                                }
                            }
                        }
                    }
                }
            }
        }
        Row {
            id: winTools
            visible: !panel.embedded
            anchors { right: parent.right; rightMargin: 10; verticalCenter: parent.verticalCenter }
            spacing: 2
            RowTool {
                icon: panel.wide ? "min" : "expand"
                tip: panel.wide ? "Painel menor" : "Painel maior"
                onHit: panel.wideToggled()
            }
            RowTool {
                icon: "close"
                tip: "Fechar  ·  Ctrl+Shift+D"
                onHit: panel.closeRequested()
            }
        }
        Rectangle {
            anchors { left: parent.left; right: parent.right; bottom: parent.bottom }
            height: 1
            color: Theme.line
        }
    }

    // ── Banners: an old version, an edit that went stale, an error ──
    Column {
        id: banners
        anchors { left: parent.left; right: parent.right; top: head.bottom; leftMargin: 1 }
        Rectangle {
            visible: panel.old && !panel.editing
            width: parent.width
            height: 40
            color: Qt.rgba(0.95, 0.8, 0.5, 0.07)
            Text {
                x: 18
                anchors.verticalCenter: parent.verticalCenter
                text: panel.doc ? "Vendo a v" + panel.shown + ". A atual é a v" + panel.doc.version + "." : ""
                color: Theme.warn
                font.pixelSize: Theme.fontBody
            }
            Row {
                anchors { right: parent.right; rightMargin: 12; verticalCenter: parent.verticalCenter }
                spacing: 6
                GhostButton {
                    small: true
                    text: "Ver a atual"
                    onClicked: {
                        panel.shown = 0;
                        panel.load();
                    }
                }
                GhostButton {
                    small: true
                    icon: "restore"
                    text: "Voltar para esta"
                    onClicked: panel.restoreShown()
                }
            }
        }
        Rectangle {
            visible: panel.stale && panel.editing
            width: parent.width
            height: 40
            color: Theme.badWash
            Text {
                x: 18
                width: parent.width - 36
                anchors.verticalCenter: parent.verticalCenter
                text: "Alguém salvou uma versão nova enquanto você editava. Copie o que escreveu, cancele e edite a mais recente."
                color: Theme.bad
                font.pixelSize: Theme.fontSmall
                wrapMode: Text.Wrap
            }
        }
        ErrorBanner {
            visible: text !== ""
            width: parent.width - 24
            x: 12
            text: panel.error
        }
    }

    // ── The list ────────────────────────────────────────────────
    Item {
        id: listView
        visible: panel.docId === ""
        anchors { left: parent.left; right: parent.right; top: banners.bottom; bottom: parent.bottom; leftMargin: 1 }

        Field {
            id: searchField
            anchors { left: parent.left; right: parent.right; top: parent.top; margins: 14 }
            leftPadding: 36
            placeholderText: "Buscar nos docs (título, texto, pasta)…"
            text: panel.query
            onTextEdited: panel.query = text
            Icon {
                x: 12
                anchors.verticalCenter: parent.verticalCenter
                name: "search"
                size: 15
                color: Theme.text4
            }
        }
        Text {
            id: listHint
            anchors { left: parent.left; right: parent.right; top: searchField.bottom; leftMargin: 18; rightMargin: 18; topMargin: 10 }
            text: "Clique para abrir · arraste para o campo de mensagem para citar"
            color: Theme.text4
            font.pixelSize: Theme.fontCaption
            elide: Text.ElideRight
        }
        ListView {
            id: docList
            anchors { left: parent.left; right: parent.right; top: listHint.bottom; bottom: parent.bottom; topMargin: 8; leftMargin: 8; rightMargin: 8 }
            clip: true
            spacing: 2
            boundsBehavior: Flickable.StopAtBounds
            ScrollBar.vertical: SlimScrollBar {}
            model: {
                if (panel.query.trim() !== "")
                    return panel.hits;
                const d = DocBus.docs;
                return d.filter(x => x.pinned).concat(d.filter(x => !x.pinned));
            }
            delegate: DocRow {
                required property var modelData
                width: docList.width - 6
                doc: modelData
                agents: panel.agents
                onOpened: DocBus.open(modelData.id)
                onPin: Api.patch("/v1/docs/" + modelData.id, { pinned: !modelData.pinned }, () => {})
                onRemove: Api.del("/v1/docs/" + modelData.id, err => {
                    if (!err) {
                        panel.undoId = modelData.id;
                        panel.flash("«" + modelData.title + "» foi para a lixeira");
                    }
                })
            }
            footer: Item {
                width: docList.width
                height: 64
                visible: docList.count === 0
                Text {
                    anchors.centerIn: parent
                    width: parent.width - 40
                    horizontalAlignment: Text.AlignHCenter
                    wrapMode: Text.Wrap
                    text: panel.query.trim() !== "" ? "Nenhum doc com isso." : DocBus.loaded ? "Ainda não há docs. Peça a um agente: “escreve um plano e salva nos Docs”." : "Carregando…"
                    color: Theme.text3
                    font.pixelSize: Theme.fontBody
                }
            }
        }
    }

    // ── One doc ─────────────────────────────────────────────────
    Item {
        id: body
        visible: panel.docId !== "" && panel.doc !== null
        anchors { left: parent.left; right: versionsBox.visible ? versionsBox.left : parent.right; top: banners.bottom; bottom: parent.bottom; leftMargin: 1 }

        property var backlinks: []
        property bool backlinksLoaded: false
        property string missingTitle: ""
        property bool creatingPage: false
        readonly property string docStamp: panel.doc ? panel.doc.id + ":" + panel.doc.version : ""

        onDocStampChanged: {
            missingTitle = "";
            backlinks = [];
            backlinksLoaded = false;
            if (panel.doc && panel.doc.id && Api.signedIn)
                loadBacklinks(panel.doc.id);
        }

        Connections {
            target: DocBus
            function onChanged() {
                if (panel.doc && panel.doc.id && Api.signedIn)
                    body.loadBacklinks(panel.doc.id);
            }
        }

        function loadBacklinks(id) {
            Api.get("/v1/docs/" + id + "/backlinks", (err, data) => {
                if (!panel.doc || panel.doc.id !== id)
                    return;
                body.backlinksLoaded = true;
                if (!err)
                    body.backlinks = data || [];
            });
        }

        // Turn [[Title]] and [[Title|label]] into clickable wiki links, skipping code blocks and spans.
        function wikiMarkdown(src) {
            const raw = String(src || "");
            if (raw.indexOf("[[") < 0)
                return raw;
            const lines = raw.split("\n");
            let fence = "";
            for (let i = 0; i < lines.length; i++) {
                const line = lines[i];
                const fm = /^ {0,3}(`{3,}|~{3,})/.exec(line);
                if (fence !== "") {
                    if (fm && fm[1][0] === fence[0] && fm[1].length >= fence.length && line.trim() === fm[1])
                        fence = "";
                    continue;
                }
                if (fm) {
                    fence = fm[1];
                    continue;
                }
                const codes = [];
                let s = line.replace(/`+[^`]*`+/g, m => {
                    codes.push(m);
                    return "\u0002" + (codes.length - 1) + "\u0002";
                });
                s = s.replace(/\[\[([^\[\]\n|]+?)(?:\|([^\[\]\n]*?))?\]\]/g, (all, rawTarget, rawLabel) => {
                    const target = rawTarget.trim().replace(/\s+/g, " ");
                    if (target === "")
                        return all;
                    const label = (rawLabel !== undefined && rawLabel.trim() !== "" ? rawLabel.trim() : target).replace(/[\[\]]/g, "");
                    const url = "avora:wiki/" + encodeURIComponent(target).replace(/\(/g, "%28").replace(/\)/g, "%29");
                    return "[" + label + "](" + url + ")";
                });
                lines[i] = s.replace(/\u0002(\d+)\u0002/g, (m, idx) => codes[+idx]);
            }
            return lines.join("\n");
        }

        function openLink(link) {
            if (link.startsWith("avora:doc/")) {
                DocBus.open(link.slice(10));
                return;
            }
            if (link.startsWith("avora:wiki/")) {
                const title = decodeURIComponent(link.slice(11)).trim();
                if (title === "")
                    return;
                const folder = panel.doc && panel.doc.folder ? "&folder=" + encodeURIComponent(panel.doc.folder) : "";
                Api.get("/v1/docs/resolve?title=" + encodeURIComponent(title) + folder, (err, d) => {
                    if (!err && d && d.id) {
                        body.missingTitle = "";
                        DocBus.open(d.id);
                    } else {
                        body.missingTitle = title;
                    }
                });
                return;
            }
            Qt.openUrlExternally(link);
        }

        function createMissingPage() {
            const t = body.missingTitle.trim();
            if (t === "" || body.creatingPage)
                return;
            body.creatingPage = true;
            const folder = panel.doc && panel.doc.folder ? panel.doc.folder : "";
            Api.post("/v1/docs", { title: t, kind: "doc", content: "# " + t + "\n\n", folder: folder }, (err, d) => {
                body.creatingPage = false;
                if (err) {
                    panel.error = err;
                    return;
                }
                body.missingTitle = "";
                DocBus.editNext = d.id;
                DocBus.load();
                DocBus.open(d.id);
            });
        }

        // Markdown document (and an HTML page's code).
        Flickable {
            id: reader
            visible: !panel.editing && panel.doc !== null && (panel.doc.kind === "doc" || (panel.doc.kind === "html" && panel.sourceMode) || (panel.doc.kind === "file" && !DocBus.isImage(panel.doc)))
            anchors.fill: parent
            contentHeight: readCol.implicitHeight + (body.missingTitle !== "" ? 120 : 64)
            clip: true
            boundsBehavior: Flickable.StopAtBounds
            ScrollBar.vertical: SlimScrollBar {}
            SmoothWheel {}
            Column {
                id: readCol
                x: Math.max(Theme.space5, (reader.width - width) / 2)
                y: Theme.space5
                width: Math.min(reader.width - 2 * Theme.space5, 760)
                spacing: Theme.space3
                Text {
                    visible: !!panel.doc && !!panel.doc.summary && panel.doc.kind === "doc"
                    width: parent.width
                    text: panel.doc ? panel.doc.summary || "" : ""
                    color: Theme.text3
                    font.pixelSize: Theme.fontBody
                    font.italic: true
                    wrapMode: Text.Wrap
                }
                // A file: what it is, a way to open it, and its text.
                Card {
                    visible: !!panel.doc && panel.doc.kind === "file"
                    width: parent.width
                    height: 76
                    padding: Theme.space4
                    Row {
                        spacing: Theme.space3
                        anchors.verticalCenter: parent.verticalCenter
                        Icon {
                            anchors.verticalCenter: parent.verticalCenter
                            name: DocBus.icon(panel.doc)
                            size: 26
                            color: DocBus.tone(panel.doc)
                        }
                        Column {
                            anchors.verticalCenter: parent.verticalCenter
                            spacing: Theme.space1
                            Text {
                                text: panel.doc ? panel.doc.title : ""
                                color: Theme.white
                                font.pixelSize: Theme.fontLarge
                                font.weight: Font.DemiBold
                            }
                            Text {
                                text: panel.doc ? DocBus.kindLabel(panel.doc) + " · " + DocBus.size(panel.doc.size) + (panel.doc.content ? " · o texto abaixo é o que os agentes leem" : " · sem texto para os agentes") : ""
                                color: Theme.text3
                                font.pixelSize: Theme.fontSmall
                            }
                        }
                    }
                    PrimaryButton {
                        anchors { right: parent.right; verticalCenter: parent.verticalCenter }
                        text: "Abrir"
                        icon: "download"
                        onClicked: panel.download()
                    }
                }
                MarkdownView {
                    width: parent.width
                    visible: !!panel.doc && panel.doc.kind === "doc"
                    text: panel.doc && panel.doc.kind === "doc" ? body.wikiMarkdown(panel.doc.content || "") : ""
                    onLinkActivated: link => body.openLink(link)
                }
                TextEdit {
                    visible: !!panel.doc && panel.doc.kind !== "doc"
                    width: parent.width
                    readOnly: true
                    selectByMouse: true
                    wrapMode: TextEdit.Wrap
                    text: panel.doc && panel.doc.kind !== "doc" ? (panel.doc.content || "").slice(0, 200000) : ""
                    color: Theme.text2
                    selectionColor: Theme.wash6
                    font.family: panel.doc && panel.doc.kind === "html" ? Theme.mono : Theme.sans
                    font.pixelSize: panel.doc && panel.doc.kind === "html" ? Theme.fontSmall : Theme.fontBody
                }
                Text {
                    visible: !!panel.doc && panel.doc.kind === "doc" && !panel.doc.content
                    text: "Documento vazio. Clique no lápis para escrever."
                    color: Theme.text4
                    font.pixelSize: Theme.fontBody
                }

                // Versions toggle and backlinks ("Mencionado em") at the bottom of the doc.
                Item { width: 1; height: Theme.space2 }
                Rectangle {
                    width: parent.width
                    height: 1
                    color: Theme.line
                }
                Row {
                    width: parent.width
                    spacing: Theme.space2
                    GhostButton {
                        small: true
                        icon: "history"
                        text: panel.doc ? "Versões · v" + panel.doc.version : "Versões"
                        onClicked: {
                            panel.versionsOpen = !panel.versionsOpen;
                            if (panel.versionsOpen)
                                panel.loadVersions();
                        }
                    }
                }
                Column {
                    width: parent.width
                    spacing: Theme.space2
                    Overline {
                        text: "MENCIONADO EM" + (body.backlinks.length > 0 ? "  ·  " + body.backlinks.length : "")
                    }
                    Text {
                        visible: body.backlinks.length === 0
                        width: parent.width
                        wrapMode: Text.Wrap
                        text: !body.backlinksLoaded ? "Carregando menções…" : "Nenhuma outra página menciona esta ainda. Escreva [[" + (panel.doc ? panel.doc.title : "Título") + "]] em outro doc para criar um link."
                        color: Theme.text4
                        font.pixelSize: Theme.fontSmall
                    }
                    Column {
                        visible: body.backlinks.length > 0
                        width: parent.width
                        spacing: Theme.space2
                        Repeater {
                            model: body.backlinks
                            delegate: Rectangle {
                                id: blRow
                                required property var modelData
                                width: parent.width
                                height: blCol.implicitHeight + 2 * Theme.space3
                                radius: Theme.radiusMd
                                color: blHover.hovered ? Theme.hover : Theme.wash1
                                border.width: 1
                                border.color: blHover.hovered ? Theme.lineStrong : Theme.line
                                Behavior on color { ColorAnimation { duration: Theme.fast } }
                                HoverHandler {
                                    id: blHover
                                    cursorShape: Qt.PointingHandCursor
                                }
                                TapHandler {
                                    onTapped: DocBus.open(blRow.modelData.id)
                                }
                                Icon {
                                    id: blIcon
                                    x: Theme.space3
                                    y: Theme.space3 + 2
                                    name: "doc"
                                    size: 15
                                    color: Theme.text2
                                }
                                Column {
                                    id: blCol
                                    anchors { left: blIcon.right; leftMargin: Theme.space2; right: parent.right; rightMargin: Theme.space3 }
                                    y: Theme.space3
                                    spacing: Theme.space1
                                    Row {
                                        width: parent.width
                                        spacing: Theme.space2
                                        Text {
                                            text: blRow.modelData.title || ""
                                            color: Theme.white
                                            font.pixelSize: Theme.fontBody
                                            font.weight: Font.Medium
                                            elide: Text.ElideRight
                                            width: Math.min(implicitWidth, parent.width - blMeta.implicitWidth - Theme.space2)
                                        }
                                        Text {
                                            id: blMeta
                                            anchors.verticalCenter: parent.verticalCenter
                                            textFormat: Text.StyledText
                                            text: "<font color='" + panel.authorTint(blRow.modelData.author) + "'>" + U.esc(panel.authorName(blRow.modelData.author)) + "</font>  ·  " + U.ago(blRow.modelData.updated_at)
                                            color: Theme.text3
                                            font.pixelSize: Theme.fontSmall
                                        }
                                    }
                                    Text {
                                        visible: !!blRow.modelData.snippet
                                        width: parent.width
                                        text: blRow.modelData.snippet || ""
                                        color: Theme.text3
                                        font.pixelSize: Theme.fontSmall
                                        wrapMode: Text.Wrap
                                        maximumLineCount: 2
                                        elide: Text.ElideRight
                                    }
                                }
                            }
                        }
                    }
                }
            }
        }

        // Offer to create the page when a [[Title]] link points to a doc that does not exist yet.
        Rectangle {
            visible: body.missingTitle !== "" && !panel.editing
            anchors { left: parent.left; right: parent.right; bottom: parent.bottom; margins: Theme.space4 }
            height: missingCol.implicitHeight + 2 * Theme.space3
            radius: Theme.radiusLg
            color: Theme.card
            border.width: 1
            border.color: Theme.lineStrong
            z: 6
            MouseArea { anchors.fill: parent }
            Column {
                id: missingCol
                x: Theme.space4
                y: Theme.space3
                width: parent.width - 2 * Theme.space4
                spacing: Theme.space2
                Text {
                    width: parent.width
                    wrapMode: Text.Wrap
                    text: "A página «" + body.missingTitle + "» ainda não existe."
                    color: Theme.text
                    font.pixelSize: Theme.fontBody
                }
                Row {
                    spacing: Theme.space2
                    PrimaryButton {
                        icon: "plus"
                        text: "Criar página " + body.missingTitle
                        busy: body.creatingPage
                        onClicked: body.createMissingPage()
                    }
                    GhostButton {
                        small: true
                        text: "Agora não"
                        onClicked: body.missingTitle = ""
                    }
                }
            }
        }

        // An image file.
        Item {
            visible: !!panel.doc && DocBus.isImage(panel.doc) && !panel.editing
            anchors.fill: parent
            anchors.margins: 20
            Image {
                anchors.fill: parent
                fillMode: Image.PreserveAspectFit
                asynchronous: true
                source: parent.visible && panel.doc ? "image://server/v1/files/" + panel.doc.file_id : ""
            }
        }

        // An HTML page, isolated: no network but a few script and font CDNs,
        // no navigation away, links open in the PC's browser.
        Loader {
            id: pageLoader
            anchors.fill: parent
            active: !!panel.doc && panel.doc.kind === "html" && !panel.sourceMode && !panel.editing && panel.visible
            sourceComponent: WebEngineView {
                id: page
                readonly property string base: "https://doc.avora.invalid/"
                readonly property string html: panel.doc ? panel.doc.content || "" : ""
                backgroundColor: "white"
                function show() {
                    const csp = "default-src 'none'; script-src 'unsafe-inline' 'unsafe-eval' https://cdnjs.cloudflare.com https://cdn.jsdelivr.net https://unpkg.com; " +
                                "style-src 'unsafe-inline' https://cdnjs.cloudflare.com https://cdn.jsdelivr.net https://unpkg.com https://fonts.googleapis.com; " +
                                "font-src data: https://fonts.gstatic.com https://cdnjs.cloudflare.com https://cdn.jsdelivr.net; img-src data: blob:; media-src data: blob:; " +
                                "connect-src 'none'; form-action 'none'; base-uri 'none'; frame-src 'none'; worker-src blob:";
                    loadHtml("<!doctype html><meta http-equiv=\"Content-Security-Policy\" content=\"" + csp + "\">" + html, base);
                }
                onHtmlChanged: show()
                Component.onCompleted: {
                    settings.javascriptCanOpenWindows = false;
                    settings.focusOnNavigationEnabled = false;
                    settings.localContentCanAccessRemoteUrls = false;
                    show();
                }
                onNavigationRequested: request => {
                    const u = request.url.toString();
                    if (u.startsWith(base) || u.startsWith("data:") || u === "about:blank") {
                        request.accept();
                        return;
                    }
                    request.reject();
                    if (request.isMainFrame && /^https?:\/\//.test(u))
                        Qt.openUrlExternally(u);
                }
                onNewWindowRequested: request => {
                    const u = request.requestedUrl.toString();
                    if (/^https?:\/\//.test(u))
                        Qt.openUrlExternally(u);
                }
            }
        }

        // Editing: the Markdown (or the page's HTML) itself.
        Item {
            visible: panel.editing
            anchors.fill: parent
            Rectangle {
                anchors { left: parent.left; right: parent.right; top: parent.top; bottom: saveBar.top; margins: 14 }
                radius: Theme.radiusLg
                color: Theme.well
                border.width: 1
                border.color: editor.activeFocus ? Theme.focusLine : Theme.line
                ScrollView {
                    anchors.fill: parent
                    clip: true
                    ScrollBar.vertical: SlimScrollBar {}
                    TextArea {
                        id: editor
                        wrapMode: TextEdit.Wrap
                        textFormat: TextEdit.PlainText
                        color: Theme.text
                        selectionColor: Theme.wash6
                        selectedTextColor: Theme.white
                        font.family: Theme.mono
                        font.pixelSize: Theme.fontBody
                        padding: 18
                        background: null
                        placeholderText: panel.doc && panel.doc.kind === "html" ? "<!doctype html>…" : "# Título\n\nEscreva em Markdown…"
                        placeholderTextColor: Theme.text4
                        Keys.onTabPressed: event => {
                            insert(cursorPosition, "    ");
                            event.accepted = true;
                        }
                    }
                }
            }
            Item {
                id: saveBar
                anchors { left: parent.left; right: parent.right; bottom: parent.bottom; leftMargin: 14; rightMargin: 14; bottomMargin: 14 }
                height: 42
                Field {
                    id: noteField
                    anchors { left: parent.left; right: saveButtons.left; rightMargin: 10; verticalCenter: parent.verticalCenter }
                    placeholderText: "O que mudou? (opcional, aparece nas versões)"
                    onAccepted: panel.save()
                }
                Row {
                    id: saveButtons
                    anchors { right: parent.right; verticalCenter: parent.verticalCenter }
                    spacing: 8
                    GhostButton {
                        text: "Cancelar"
                        onClicked: panel.editing = false
                    }
                    PrimaryButton {
                        text: panel.saving ? "Salvando…" : "Salvar  ·  Ctrl+S"
                        busy: panel.saving
                        onClicked: panel.save()
                    }
                }
            }
        }

        // Title, folder, summary.
        Rectangle {
            visible: panel.infoOpen && panel.doc !== null
            anchors { left: parent.left; right: parent.right; top: parent.top; margins: 12 }
            height: infoCol.implicitHeight + 32
            radius: Theme.radiusLg
            color: "#121216"
            border.width: 1
            border.color: Theme.lineStrong
            z: 5
            MouseArea { anchors.fill: parent }
            Column {
                id: infoCol
                x: 16
                y: 16
                width: parent.width - 32
                spacing: 10
                property var src: panel.infoOpen ? panel.doc : null
                onSrcChanged: if (src) {
                    titleField.text = src.title;
                    folderField.text = src.folder || "";
                    summaryField.text = src.summary || "";
                }
                Text { text: "TÍTULO"; color: Theme.text4; font.pixelSize: Theme.fontTiny; font.weight: Font.DemiBold; font.letterSpacing: Theme.trackCaps }
                Field { id: titleField; width: parent.width }
                Text { text: "PASTA  ·  use / para subpastas, ex.: Clientes/Acme"; color: Theme.text4; font.pixelSize: Theme.fontTiny; font.weight: Font.DemiBold; font.letterSpacing: Theme.trackCaps }
                Field { id: folderField; width: parent.width; placeholderText: "Sem pasta" }
                Text { text: "RESUMO  ·  aparece na busca e, fixado, para todo agente"; color: Theme.text4; font.pixelSize: Theme.fontTiny; font.weight: Font.DemiBold; font.letterSpacing: Theme.trackCaps }
                Field { id: summaryField; width: parent.width; placeholderText: "Uma frase sobre o que o doc tem" }
                Row {
                    spacing: 8
                    PrimaryButton {
                        text: "Salvar"
                        onClicked: panel.patch({ title: titleField.text, folder: folderField.text, summary: summaryField.text }, () => {
                            panel.infoOpen = false;
                            panel.flash("Detalhes salvos");
                        })
                    }
                    GhostButton {
                        text: "Cancelar"
                        onClicked: panel.infoOpen = false
                    }
                }
            }
        }
    }

    // ── Versions ────────────────────────────────────────────────
    Rectangle {
        id: versionsBox
        visible: panel.versionsOpen && panel.docId !== ""
        anchors { right: parent.right; top: banners.bottom; bottom: parent.bottom }
        width: Math.min(280, panel.width * 0.45)
        color: Theme.wash1
        Rectangle {
            anchors { left: parent.left; top: parent.top; bottom: parent.bottom }
            width: 1
            color: Theme.line
        }
        Text {
            id: vHead
            x: 16
            y: 14
            text: "VERSÕES"
            color: Theme.text4
            font.pixelSize: Theme.fontCaption
            font.weight: Font.DemiBold
            font.letterSpacing: Theme.trackCaps
        }
        ListView {
            id: vList
            anchors { left: parent.left; right: parent.right; top: vHead.bottom; bottom: parent.bottom; topMargin: 8; leftMargin: 8; rightMargin: 8 }
            clip: true
            spacing: 2
            model: panel.versions
            ScrollBar.vertical: SlimScrollBar {}
            delegate: Rectangle {
                id: vRow
                required property var modelData
                readonly property bool here: panel.doc && (panel.shown === 0 ? modelData.version === panel.doc.version : modelData.version === panel.shown)
                width: vList.width - 4
                height: vCol.implicitHeight + 2 * Theme.space2
                radius: Theme.radiusMd
                color: here ? Theme.active : vHover.hovered ? Theme.hover : "transparent"
                HoverHandler {
                    id: vHover
                    cursorShape: Qt.PointingHandCursor
                }
                MouseArea {
                    id: vArea
                    anchors.fill: parent
                    onClicked: {
                        panel.editing = false;
                        panel.shown = panel.doc && vRow.modelData.version === panel.doc.version ? 0 : vRow.modelData.version;
                        panel.load();
                    }
                }
                Column {
                    id: vCol
                    x: 10
                    y: Theme.space2
                    width: parent.width - 20
                    spacing: Theme.space1
                    Text {
                        width: parent.width
                        textFormat: Text.StyledText
                        text: "<b>v" + vRow.modelData.version + "</b>" + (panel.doc && vRow.modelData.version === panel.doc.version ? "  <font color='" + Theme.ok + "'>atual</font>" : "") +
                              "  <font color='" + panel.authorTint(vRow.modelData.author) + "'>" + U.esc(panel.authorName(vRow.modelData.author)) + "</font>"
                        color: Theme.text
                        font.pixelSize: Theme.fontBody
                        elide: Text.ElideRight
                    }
                    Text {
                        width: parent.width
                        text: U.day(vRow.modelData.created_at) + " " + U.clock(vRow.modelData.created_at).slice(0, 5) + (vRow.modelData.note ? "  ·  " + vRow.modelData.note : "")
                        color: Theme.text3
                        font.pixelSize: Theme.fontSmall
                        wrapMode: Text.Wrap
                        maximumLineCount: 3
                        elide: Text.ElideRight
                    }
                    GhostButton {
                        visible: panel.doc !== null && vRow.modelData.version !== panel.doc.version
                        small: true
                        icon: "restore"
                        text: "Restaurar"
                        onClicked: panel.restoreVersion(vRow.modelData.version)
                    }
                }
            }
        }
    }

    // ── A short notice at the bottom (saved, trashed + undo) ────
    Rectangle {
        visible: panel.notice !== ""
        anchors { horizontalCenter: parent.horizontalCenter; bottom: parent.bottom; bottomMargin: panel.editing ? 70 : 18 }
        width: noticeRow.implicitWidth + 28
        height: 38
        radius: Theme.radius
        color: "#1a1a20"
        border.width: 1
        border.color: Theme.lineStrong
        z: 10
        Row {
            id: noticeRow
            anchors.centerIn: parent
            spacing: 12
            Text {
                anchors.verticalCenter: parent.verticalCenter
                text: panel.notice
                color: Theme.text
                font.pixelSize: Theme.fontBody
            }
            Text {
                visible: panel.undoId !== ""
                anchors.verticalCenter: parent.verticalCenter
                text: "Desfazer"
                color: Theme.white
                font.pixelSize: Theme.fontBody
                font.weight: Font.DemiBold
                font.underline: undoArea.containsMouse
                MouseArea {
                    id: undoArea
                    anchors.fill: parent
                    anchors.margins: -6
                    hoverEnabled: true
                    cursorShape: Qt.PointingHandCursor
                    onClicked: panel.undoTrash()
                }
            }
        }
    }

    // Loading a doc for the first time.
    Text {
        visible: panel.docId !== "" && panel.doc === null && panel.loading
        anchors.centerIn: parent
        text: "Abrindo o doc…"
        color: Theme.text3
        font.pixelSize: Theme.fontBody
    }
}
