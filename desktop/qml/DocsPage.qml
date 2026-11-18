import QtQuick
import QtQuick.Controls.Basic
import QtQuick.Dialogs
import Avora
import "util.js" as U

// Docs (D60): the team's shared drive. Documents in Markdown, HTML pages and
// files the agents and the owner write, find and cite. Three columns: views
// and folders, the list, and the doc open in the page itself (never in the
// side panel, which closes here). Dragged into the chat a doc becomes a
// citation; pinned docs every agent sees in every turn.
Item {
    id: page
    property var agents: []
    property string query: ""
    property string kind: ""      // "", "doc", "html", "file"
    property string folder: ""    // "" = every folder
    property string view: "all"   // all | pinned | agents | mine | trash
    readonly property bool trashView: view === "trash"
    property bool readerWide: false
    property var hits: []         // a search's results, from the server
    property var trash: []
    property string error: ""
    property int uploading: 0
    property string armed: ""     // a trashed doc one click away from gone for good

    readonly property bool searching: query.trim() !== ""
    readonly property var filtered: {
        DocBus.rev;
        const src = searching ? hits : DocBus.docs;
        return src.filter(d => (kind === "" || d.kind === kind) && inView(d, view) &&
                               (folder === "" || d.folder === folder || (d.folder || "").startsWith(folder + "/")));
    }
    function inView(d, v) {
        if (v === "pinned")
            return d.pinned;
        if (v === "agents")
            return (d.author || "").indexOf("agent:") === 0;
        if (v === "mine")
            return (d.author || "").indexOf("agent:") !== 0;
        return true;
    }
    function countOf(v) {
        DocBus.rev;
        return DocBus.docs.filter(d => inView(d, v)).length;
    }
    readonly property string viewTitle: trashView ? "Lixeira" : folder !== "" ? folder.split("/").pop()
        : view === "pinned" ? "Fixados" : view === "agents" ? "Feitos pelos agentes" : view === "mine" ? "Feitos por você" : "Todos os docs"
    readonly property bool reading: DocBus.openId !== "" && !trashView
    readonly property var pinned: searching ? [] : filtered.filter(d => d.pinned)
    readonly property var rest: searching ? filtered : filtered.filter(d => !d.pinned)

    Component.onCompleted: {
        Api.get("/v1/agents", (err, data) => { if (!err) agents = data; });
        DocBus.load();
    }

    function search() {
        const q = query.trim();
        if (q === "") {
            hits = [];
            return;
        }
        Api.get("/v1/docs?limit=200&q=" + encodeURIComponent(q), (err, data) => {
            if (q !== page.query.trim())
                return;
            if (err)
                error = err;
            else {
                error = "";
                hits = data;
            }
        });
    }
    Timer {
        id: searchDelay
        interval: 220
        onTriggered: page.search()
    }
    onQueryChanged: searchDelay.restart()

    function loadTrash() {
        Api.get("/v1/docs?trash=1&limit=500", (err, data) => {
            if (err)
                error = err;
            else
                trash = data;
        });
    }
    onTrashViewChanged: if (trashView) loadTrash()
    Connections {
        target: DocBus
        function onChanged(id, type) {
            if (page.trashView)
                page.loadTrash();
            if (page.searching)
                searchDelay.restart();
        }
    }

    function upload(urls) {
        for (const u of urls) {
            uploading++;
            Api.uploadFile("", u, (err, f) => {
                if (err) {
                    uploading--;
                    error = err;
                    return;
                }
                Api.post("/v1/docs", { kind: "file", file_id: f.id, folder: page.folder }, (err2, d) => {
                    uploading--;
                    if (err2)
                        error = err2;
                    else {
                        error = "";
                        DocBus.load();
                        if (urls.length === 1)
                            DocBus.open(d.id);
                    }
                });
            });
        }
    }
    function pin(d) {
        Api.patch("/v1/docs/" + d.id, { pinned: !d.pinned }, err => { if (err) error = err; });
    }
    function remove(d) {
        Api.del("/v1/docs/" + d.id, err => {
            if (err)
                error = err;
            else if (DocBus.openId === d.id)
                DocBus.openId = "";
        });
    }
    function restore(d) {
        Api.post("/v1/docs/" + d.id + "/restore", {}, err => {
            if (err)
                error = err;
            loadTrash();
        });
    }
    function purge(d) {
        if (armed !== d.id) {
            armed = d.id;
            disarm.restart();
            return;
        }
        armed = "";
        Api.del("/v1/docs/" + d.id + "?forever=1", err => {
            if (err)
                error = err;
            loadTrash();
        });
    }
    Timer {
        id: disarm
        interval: 4000
        onTriggered: page.armed = ""
    }

    FileDialog {
        id: uploadDialog
        title: "Guardar arquivos nos Docs"
        fileMode: FileDialog.OpenFiles
        onAccepted: page.upload(selectedFiles)
    }

    // ── Views and folders ───────────────────────────────────────
    Rectangle {
        id: nav
        anchors { left: parent.left; top: parent.top; bottom: parent.bottom }
        width: page.readerWide && page.reading ? 0 : 236
        visible: width > 0
        color: Theme.wash1
        Rectangle {
            anchors { right: parent.right; top: parent.top; bottom: parent.bottom }
            width: 1
            color: Theme.line
        }
        Flickable {
            anchors.fill: parent
            contentHeight: navCol.height + 40
            clip: true
            boundsBehavior: Flickable.StopAtBounds
            ScrollBar.vertical: SlimScrollBar {}
            Column {
                id: navCol
                x: Theme.space4
                y: 52
                width: nav.width - 2 * Theme.space4
                spacing: Theme.space1
                Text {
                    text: "Docs"
                    color: Theme.white
                    font.family: Theme.display
                    font.pixelSize: Theme.fontTitle
                    font.weight: Font.DemiBold
                    bottomPadding: Theme.space3
                }
                PrimaryButton {
                    width: parent.width
                    text: "Novo doc"
                    icon: "plus"
                    onClicked: newSheet.open()
                }
                GhostButton {
                    width: parent.width
                    text: page.uploading > 0 ? "Enviando…" : "Enviar arquivo"
                    icon: "upload"
                    onClicked: uploadDialog.open()
                }
                Item { width: 1; height: Theme.space3 }
                Repeater {
                    model: [
                        { v: "all", label: "Todos", icon: "docs" },
                        { v: "pinned", label: "Fixados", icon: "pin" },
                        { v: "agents", label: "Dos agentes", icon: "spark" },
                        { v: "mine", label: "Meus", icon: "user" },
                        { v: "trash", label: "Lixeira", icon: "trash" }
                    ]
                    delegate: NavRow {
                        required property var modelData
                        icon: modelData.icon
                        label: modelData.label
                        count: modelData.v === "trash" ? -1 : page.countOf(modelData.v)
                        on: page.view === modelData.v && page.folder === ""
                        onHit: {
                            page.view = modelData.v;
                            page.folder = "";
                        }
                    }
                }
                Overline {
                    visible: folderRep.count > 0
                    leftPadding: 10
                    topPadding: Theme.space4
                    bottomPadding: Theme.space1
                    text: "PASTAS"
                }
                Repeater {
                    id: folderRep
                    model: {
                        DocBus.rev;
                        return DocBus.folders();
                    }
                    delegate: NavRow {
                        required property var modelData
                        indent: (modelData.path.split("/").length - 1) * 14
                        icon: "folder"
                        label: modelData.path.split("/").pop()
                        count: modelData.count
                        on: page.folder === modelData.path
                        onHit: {
                            page.folder = on ? "" : modelData.path;
                            if (page.trashView)
                                page.view = "all";
                        }
                    }
                }
            }
        }
    }

    // One row of the column: icon, name, count.
    component NavRow: Rectangle {
        id: navRow
        property string icon
        property string label
        property int count: -1
        property bool on: false
        property real indent: 0
        signal hit
        width: navCol.width
        height: 34
        radius: Theme.radiusMd
        color: on ? Theme.active : navHover.hovered ? Theme.hover : "transparent"
        Icon {
            id: navIcon
            x: 10 + navRow.indent
            anchors.verticalCenter: parent.verticalCenter
            name: navRow.icon
            size: 14
            color: navRow.on ? Theme.text : Theme.text3
        }
        Text {
            anchors { left: navIcon.right; leftMargin: 10; right: navCount.left; rightMargin: 6; verticalCenter: parent.verticalCenter }
            text: navRow.label
            color: navRow.on ? Theme.white : Theme.text2
            font.pixelSize: Theme.fontBody
            elide: Text.ElideRight
        }
        Text {
            id: navCount
            anchors { right: parent.right; rightMargin: 10; verticalCenter: parent.verticalCenter }
            visible: navRow.count >= 0
            text: navRow.count
            color: Theme.text4
            font.pixelSize: Theme.fontSmall
        }
        HoverHandler {
            id: navHover
            cursorShape: Qt.PointingHandCursor
        }
        TapHandler {
            onTapped: navRow.hit()
        }
    }

    // ── The doc being read, in the page ─────────────────────────
    DocPanel {
        id: reader
        // Under the title bar, so its tools never meet the window's buttons.
        anchors { right: parent.right; top: parent.top; bottom: parent.bottom; topMargin: Theme.titleBar }
        readonly property real room: page.width - nav.width
        width: !page.reading ? 0 : page.readerWide ? room : Math.max(440, Math.min(room - 380, room * 0.58))
        visible: page.reading && page.visible
        wide: page.readerWide
        onCloseRequested: DocBus.openId = ""
        onWideToggled: page.readerWide = !page.readerWide
        Rectangle {
            anchors { left: parent.left; top: parent.top; bottom: parent.bottom }
            width: 1
            color: Theme.line
        }
    }

    Flickable {
        id: scroller
        anchors { left: nav.right; right: page.reading ? reader.left : parent.right; top: parent.top; bottom: parent.bottom }
        visible: width > 40
        contentHeight: col.height + 110
        clip: true
        boundsBehavior: Flickable.StopAtBounds
        ScrollBar.vertical: SlimScrollBar {}
        SmoothWheel {}

        Column {
            id: col
            x: Math.round((scroller.width - width) / 2)
            y: 52
            width: Math.min(scroller.width - 2 * Theme.space5, 1040)
            spacing: 20

            Column {
                width: parent.width
                spacing: 4
                Text {
                    width: parent.width
                    text: page.viewTitle
                    color: Theme.white
                    font.family: Theme.display
                    font.pixelSize: Theme.fontHeading
                    font.weight: Font.DemiBold
                    elide: Text.ElideRight
                }
                Text {
                    width: parent.width
                    text: page.trashView ? "Apagados de vez depois de 30 dias."
                        : page.view === "pinned" ? "Todo agente vê o título e o resumo destes em cada conversa."
                        : "Arraste um doc para o chat para citá-lo: o agente recebe o texto."
                    color: Theme.text3
                    font.pixelSize: Theme.fontSmall
                    elide: Text.ElideRight
                }
            }

            ErrorBanner {
                width: parent.width
                text: page.error
            }

            // ── Search and filters ──────────────────────────────
            Item {
                id: searchRow
                // Narrow (the Docs panel open beside): the kinds go under the search.
                readonly property bool narrow: width < kinds.implicitWidth + 300
                visible: !page.trashView
                width: parent.width
                height: narrow ? 42 + 10 + kinds.height : 42
                Field {
                    id: searchField
                    anchors { left: parent.left; right: searchRow.narrow ? parent.right : kinds.left; rightMargin: searchRow.narrow ? 0 : 12 }
                    leftPadding: 38
                    placeholderText: "Buscar por título, texto, pasta…"
                    text: page.query
                    onTextEdited: page.query = text
                    Keys.onEscapePressed: {
                        text = "";
                        page.query = "";
                    }
                    Icon {
                        x: 13
                        anchors.verticalCenter: parent.verticalCenter
                        name: "search"
                        size: 15
                        color: Theme.text4
                    }
                }
                Segmented {
                    id: kinds
                    x: searchRow.narrow ? 0 : parent.width - width
                    y: searchRow.narrow ? 52 : (42 - height) / 2
                    value: page.kind
                    options: [{ value: "", label: "Tudo" }, { value: "doc", label: "Documentos" }, { value: "html", label: "Páginas" }, { value: "file", label: "Arquivos" }]
                    onActivated: v => page.kind = v
                }
            }

            // ── Pinned ──────────────────────────────────────────
            Column {
                visible: !page.trashView && page.pinned.length > 0
                width: parent.width
                spacing: 8
                Overline { leftPadding: 4; text: "FIXADOS  ·  " + page.pinned.length }
                Rectangle {
                    width: parent.width
                    height: pinCol.implicitHeight + 12
                    radius: Theme.radius
                    color: Qt.rgba(0.95, 0.8, 0.5, 0.025)
                    border.width: 1
                    border.color: Theme.line
                    Column {
                        id: pinCol
                        x: 6
                        y: 6
                        width: parent.width - 12
                        spacing: 1
                        Repeater {
                            model: page.pinned
                            delegate: DocRow {
                                required property var modelData
                                width: pinCol.width
                                doc: modelData
                                agents: page.agents
                                selected: DocBus.openId === modelData.id
                                onOpened: DocBus.open(modelData.id)
                                onPin: page.pin(modelData)
                                onRemove: page.remove(modelData)
                            }
                        }
                    }
                }
            }

            // ── Everything else (or the search's results) ───────
            Column {
                visible: !page.trashView
                width: parent.width
                spacing: 8
                Overline { leftPadding: 4;
                    text: page.searching ? "RESULTADOS  ·  " + page.rest.length : (page.pinned.length ? "OUTROS" : "DOCS") + "  ·  " + page.rest.length
                }
                Rectangle {
                    width: parent.width
                    height: page.rest.length ? listCol.implicitHeight + 12 : 0
                    visible: page.rest.length > 0
                    radius: Theme.radius
                    color: Theme.wash1
                    border.width: 1
                    border.color: Theme.line
                    Column {
                        id: listCol
                        x: 6
                        y: 6
                        width: parent.width - 12
                        spacing: 1
                        Repeater {
                            model: page.rest
                            delegate: DocRow {
                                required property var modelData
                                width: listCol.width
                                doc: modelData
                                agents: page.agents
                                selected: DocBus.openId === modelData.id
                                onOpened: DocBus.open(modelData.id)
                                onPin: page.pin(modelData)
                                onRemove: page.remove(modelData)
                            }
                        }
                    }
                }
                // Nothing yet, or nothing found.
                Rectangle {
                    visible: page.rest.length === 0 && page.pinned.length === 0
                    width: parent.width
                    height: 190
                    radius: Theme.radius
                    color: dropZone.containsDrag ? Theme.hover : Theme.wash1
                    border.width: 1
                    border.color: dropZone.containsDrag ? Theme.focusLine : Theme.line
                    Column {
                        anchors.centerIn: parent
                        width: parent.width - 60
                        spacing: 10
                        Icon {
                            anchors.horizontalCenter: parent.horizontalCenter
                            name: page.searching ? "search" : "docs"
                            size: 26
                            color: Theme.text3
                        }
                        Text {
                            width: parent.width
                            horizontalAlignment: Text.AlignHCenter
                            text: page.searching ? "Nada com “" + page.query.trim() + "”." : !DocBus.loaded ? "Carregando…" : page.kind !== "" || page.folder !== "" || page.view !== "all" ? "Nenhum doc aqui." : "Os docs do time aparecem aqui."
                            color: Theme.text
                            font.pixelSize: Theme.fontLarge
                            font.weight: Font.Medium
                        }
                        Text {
                            visible: !page.searching && DocBus.loaded
                            width: parent.width
                            horizontalAlignment: Text.AlignHCenter
                            wrapMode: Text.Wrap
                            text: "Peça no chat: “escreve a proposta para o cliente e salva nos Docs”. Ou crie um aqui, ou solte arquivos nesta página."
                            color: Theme.text3
                            font.pixelSize: Theme.fontBody
                        }
                    }
                }
            }

            // ── Trash ───────────────────────────────────────────
            Column {
                visible: page.trashView
                width: parent.width
                spacing: 8
                Overline { leftPadding: 4; text: "NA LIXEIRA  ·  " + page.trash.length }
                Rectangle {
                    width: parent.width
                    height: page.trash.length ? trashCol.implicitHeight + 12 : 90
                    radius: Theme.radius
                    color: Theme.wash1
                    border.width: 1
                    border.color: Theme.line
                    Text {
                        visible: page.trash.length === 0
                        anchors.centerIn: parent
                        text: "A lixeira está vazia."
                        color: Theme.text3
                        font.pixelSize: Theme.fontBody
                    }
                    Column {
                        id: trashCol
                        x: 6
                        y: 6
                        width: parent.width - 12
                        spacing: 1
                        Repeater {
                            model: page.trash
                            delegate: DocRow {
                                required property var modelData
                                width: trashCol.width
                                doc: modelData
                                agents: page.agents
                                trash: true
                                onRestore: page.restore(modelData)
                                onPurge: page.purge(modelData)
                                Text {
                                    visible: page.armed === modelData.id
                                    anchors { right: parent.right; rightMargin: 74; verticalCenter: parent.verticalCenter }
                                    text: "Clique de novo para apagar para sempre"
                                    color: Theme.bad
                                    font.pixelSize: Theme.fontSmall
                                }
                            }
                        }
                    }
                }
            }
        }
    }

    // Files dropped anywhere on the page go into the Docs (in the folder
    // being looked at).
    DropArea {
        id: dropZone
        anchors.fill: parent
        keys: ["text/uri-list"]
        onDropped: drop => {
            if (drop.hasUrls) {
                page.upload(drop.urls);
                drop.acceptProposedAction();
            }
        }
    }
    Rectangle {
        visible: dropZone.containsDrag
        anchors.fill: parent
        anchors.margins: 14
        radius: 22
        color: Theme.well
        border.width: 2
        border.color: Theme.focusLine
        Column {
            anchors.centerIn: parent
            spacing: 10
            Icon {
                anchors.horizontalCenter: parent.horizontalCenter
                name: "upload"
                size: 30
                color: Theme.white
            }
            Text {
                text: "Solte para guardar nos Docs" + (page.folder ? " · " + page.folder : "")
                color: Theme.white
                font.pixelSize: Theme.fontTitle
                font.weight: Font.DemiBold
            }
        }
    }

    // ── New doc ─────────────────────────────────────────────────
    Item {
        id: newSheet
        anchors.fill: parent
        visible: false
        z: 50
        property string kind: "doc"
        property bool busy: false
        function open() {
            kind = "doc";
            newTitle.text = "";
            newFolder.text = page.folder;
            visible = true;
            Qt.callLater(() => newTitle.forceActiveFocus());
        }
        function create() {
            const t = newTitle.text.trim();
            if (t === "" || busy)
                return;
            busy = true;
            const start = kind === "html" ? "<!doctype html>\n<html lang=\"pt-BR\">\n<head>\n<meta charset=\"utf-8\">\n<title>" + U.esc(t) + "</title>\n<style>\n  body { font-family: system-ui, sans-serif; margin: 40px; }\n</style>\n</head>\n<body>\n<h1>" + U.esc(t) + "</h1>\n</body>\n</html>\n" : "# " + t + "\n\n";
            Api.post("/v1/docs", { title: t, kind: kind, content: start, folder: newFolder.text.trim() }, (err, d) => {
                busy = false;
                if (err) {
                    page.error = err;
                    return;
                }
                visible = false;
                DocBus.editNext = d.id;
                DocBus.load();
                DocBus.open(d.id);
            });
        }
        Shortcut {
            sequence: "Esc"
            enabled: newSheet.visible
            onActivated: newSheet.visible = false
        }
        Rectangle {
            anchors.fill: parent
            color: Qt.rgba(0, 0, 0, 0.55)
            MouseArea {
                anchors.fill: parent
                onClicked: newSheet.visible = false
            }
        }
        Card {
            anchors.centerIn: parent
            width: Math.min(parent.width - 48, 520)
            height: sheetCol.implicitHeight + 44
            Column {
                id: sheetCol
                width: parent.width
                spacing: 12
                Text {
                    text: "Novo doc"
                    color: Theme.white
                    font.pixelSize: Theme.fontHeading
                    font.weight: Font.DemiBold
                }
                Segmented {
                    value: newSheet.kind
                    options: [{ value: "doc", label: "Documento (Markdown)" }, { value: "html", label: "Página (HTML)" }]
                    onActivated: v => newSheet.kind = v
                }
                Overline { leftPadding: 4; text: "TÍTULO" }
                Field {
                    id: newTitle
                    width: parent.width
                    placeholderText: newSheet.kind === "html" ? "Ex.: Painel de vendas de novembro" : "Ex.: Plano de lançamento"
                    onAccepted: newSheet.create()
                }
                Overline { leftPadding: 4; text: "PASTA (OPCIONAL)  ·  USE / PARA SUBPASTAS" }
                Field {
                    id: newFolder
                    width: parent.width
                    placeholderText: "Ex.: Clientes/Acme"
                    onAccepted: newSheet.create()
                }
                Row {
                    spacing: 8
                    topPadding: 6
                    PrimaryButton {
                        text: "Criar e escrever"
                        busy: newSheet.busy
                        onClicked: newSheet.create()
                    }
                    GhostButton {
                        text: "Cancelar"
                        onClicked: newSheet.visible = false
                    }
                }
            }
        }
    }
}
