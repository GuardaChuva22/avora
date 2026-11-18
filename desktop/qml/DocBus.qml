pragma Singleton
import QtQuick
import Avora

// The Docs (D60) as every page sees them: the list (kept in step with the
// server's live events), and the two things any page can ask for — open a
// doc in the panel beside the page, or cite it in the chat's message box.
QtObject {
    id: bus
    property var docs: []      // live docs, newest first, without content
    property bool loaded: false
    property string error: ""
    property int rev: 0        // bumps on every change, for views to re-read
    property string openId: "" // the doc in the panel ("" = the list)
    property string editNext: "" // a doc just made, to open in the editor
    property var pendingCite: null // a citation asked before the chat page opened

    signal openRequested(string id)
    signal listRequested
    signal citeRequested(var doc)
    // A doc changed on the server (saved, trashed, restored, deleted).
    signal changed(string id, string type)

    function load() {
        if (!Api.signedIn)
            return;
        Api.get("/v1/docs?limit=500", (err, data) => {
            if (err) {
                error = err;
                return;
            }
            error = "";
            loaded = true;
            docs = data || [];
            rev++;
        });
    }
    function byId(id) {
        for (let i = 0; i < docs.length; i++)
            if (docs[i].id === id)
                return docs[i];
        return null;
    }
    function open(id) {
        openRequested(id);
    }
    function showList() {
        listRequested();
    }
    function cite(doc) {
        if (!doc)
            return;
        pendingCite = doc;
        citeRequested(doc);
    }

    // ── How a doc looks ─────────────────────────────────────────
    function isImage(d) {
        return !!d && d.kind === "file" && (d.mime || "").startsWith("image/");
    }
    function icon(d) {
        if (!d)
            return "doc";
        if (d.kind === "html")
            return "code";
        if (d.kind === "file")
            return isImage(d) ? "image" : "file";
        return "doc";
    }
    function tone(d) {
        if (!d)
            return "#9aa4b8";
        if (d.kind === "html")
            return "#7dd3fc";
        if (d.kind === "file")
            return isImage(d) ? "#f0abfc" : d.mime === "application/pdf" ? "#fca5a5" : "#c4b5fd";
        return "#f5c97a";
    }
    function kindLabel(d) {
        if (!d)
            return "Doc";
        if (d.kind === "html")
            return "Página";
        if (d.kind === "file") {
            if (isImage(d))
                return "Imagem";
            if (d.mime === "application/pdf")
                return "PDF";
            const ext = (d.title.match(/\.([a-z0-9]{1,5})$/i) || [])[1];
            return ext ? ext.toUpperCase() : "Arquivo";
        }
        return "Documento";
    }
    function size(n) {
        if (n >= 1 << 20)
            return (n / (1 << 20)).toFixed(1).replace(".", ",") + " MB";
        if (n >= 1024)
            return Math.round(n / 1024) + " KB";
        return n + " B";
    }
    // fileName is what a doc is saved as on the PC.
    function fileName(d) {
        if (d.kind === "file")
            return d.title;
        const ext = d.kind === "html" ? ".html" : ".md";
        return d.title.toLowerCase().endsWith(ext) ? d.title : d.title + ext;
    }
    // link is how a doc is cited in a message.
    function link(d) {
        return "[📄 " + String(d.title).replace(/[\[\]]/g, "") + "](avora:doc/" + d.id + ")";
    }
    function fold(t) {
        return String(t || "").toLowerCase().normalize("NFD").replace(/[̀-ͯ]/g, "");
    }
    // match finds docs by title, folder and summary, for the @ list.
    function match(q, limit) {
        const want = fold(q).trim();
        const out = [];
        for (let i = 0; i < docs.length && out.length < limit; i++) {
            const d = docs[i];
            if (want === "" || fold(d.title).indexOf(want) >= 0 || (want.length > 2 && fold(d.folder + " " + d.summary).indexOf(want) >= 0))
                out.push(d);
        }
        return out;
    }
    // folders: every folder with its count, parents included.
    function folders() {
        const count = {};
        for (const d of docs) {
            if (!d.folder)
                continue;
            const parts = d.folder.split("/");
            for (let i = 1; i <= parts.length; i++) {
                const k = parts.slice(0, i).join("/");
                count[k] = (count[k] || 0) + 1;
            }
        }
        return Object.keys(count).sort((a, b) => a.localeCompare(b, "pt-BR")).map(k => ({ path: k, count: count[k] }));
    }

    property Timer reload: Timer {
        interval: 250
        onTriggered: bus.load()
    }
    property Connections live: Connections {
        target: Api
        function onLiveEvent(e) {
            if (!e.type || !e.type.startsWith("doc."))
                return;
            bus.reload.restart();
            const p = e.payload || {};
            bus.changed(p.id || "", e.type);
        }
        function onLiveReset() {
            bus.reload.restart();
        }
        function onSignedInChanged() {
            if (Api.signedIn)
                bus.load();
            else {
                bus.docs = [];
                bus.loaded = false;
                bus.openId = "";
            }
        }
    }
    // Without the live channel, a slow poll keeps the list fresh.
    property Timer poll: Timer {
        interval: 30000
        repeat: true
        running: Api.signedIn && !Api.live
        onTriggered: bus.load()
    }
    Component.onCompleted: load()
}
