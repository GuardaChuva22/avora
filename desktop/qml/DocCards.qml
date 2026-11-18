import QtQuick
import Avora

// The docs an answer made, changed or cites, one card each, under the text:
// what an agent saves in the Docs shows like an artifact in the chat.
Column {
    id: cards
    property string text: ""
    property var steps: []

    // Ids in order: the steps that wrote docs, then the links in the text.
    readonly property var items: {
        const out = [], seen = {};
        for (const st of steps || []) {
            const tool = (st.tool || "").split(" · ").pop();
            if ((tool === "docs_criar" || tool === "docs_editar") && st.detail && !seen[st.detail]) {
                seen[st.detail] = true;
                const v = (st.summary || "").match(/ · v(\d+)$/);
                out.push({ id: st.detail, title: (st.summary || "").replace(/ · v\d+$/, ""),
                           tag: tool === "docs_criar" ? "Criou" : "Editou" + (v ? " · v" + v[1] : "") });
            }
        }
        const re = /\[([^\]]*)\]\(avora:doc\/([0-9a-f]{8,32})\)/g;
        let m;
        while ((m = re.exec(text || "")) !== null) {
            if (seen[m[2]])
                continue;
            seen[m[2]] = true;
            out.push({ id: m[2], title: m[1].replace(/^📄\s*/, ""), tag: "" });
        }
        return out.slice(0, 8);
    }

    visible: items.length > 0
    spacing: 6

    Repeater {
        model: cards.items
        delegate: DocCard {
            required property var modelData
            width: Math.min(cards.width, 520)
            docId: modelData.id
            fallbackTitle: modelData.title
            tag: modelData.tag
        }
    }
}
