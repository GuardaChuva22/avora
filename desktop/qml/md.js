.pragma library

// Markdown for the chat, read into blocks the app draws with its own
// components (MarkdownView.qml): headings, paragraphs, lists and task
// lists, code, tables, quotes and callouts, rules. Inline text becomes the
// small HTML subset Qt's rich text draws well (b, i, s, code, links).
// Images are left out: MessageImages shows them.

const MONO = "Cascadia Mono, Consolas, monospace";

function esc(s) {
    return String(s).replace(/&/g, "&amp;").replace(/</g, "&lt;").replace(/>/g, "&gt;").replace(/"/g, "&quot;");
}

// inline turns one line of Markdown into rich text. linkColor and
// codeBg come from the theme.
function inline(src, theme) {
    const th = theme || {};
    const link = th.link || "#9fb4ff";
    const codeBg = th.codeBg || "#26262c";
    const codeFg = th.codeFg || "#e8d9ff";
    // Code spans first: nothing inside them is Markdown.
    const codes = [];
    let s = String(src).replace(/`([^`\n]+)`/g, (m, c) => {
        codes.push(c);
        return "\u0000" + (codes.length - 1) + "\u0000";
    });
    // Images are drawn apart.
    s = s.replace(/!\[[^\]]*\]\([^)\s]+\)/g, "");
    // Links: [text](url), then bare addresses.
    const links = [];
    s = s.replace(/\[([^\]]+)\]\(([^)\s]+)(?:\s+"[^"]*")?\)/g, (m, t, u) => {
        links.push({ t: t, u: u });
        return "\u0001" + (links.length - 1) + "\u0001";
    });
    s = s.replace(/(^|[\s(])(https?:\/\/[^\s<>()]+[^\s<>().,;:!?'"])/g, (m, pre, u) => {
        links.push({ t: u.replace(/^https?:\/\/(www\.)?/, "").slice(0, 60), u: u });
        return pre + "\u0001" + (links.length - 1) + "\u0001";
    });
    s = esc(s);
    s = s.replace(/\*\*\*([^*]+)\*\*\*/g, "<b><i>$1</i></b>")
         .replace(/\*\*([^*]+?)\*\*/g, "<b>$1</b>")
         .replace(/__([^_]+?)__/g, "<b>$1</b>")
         .replace(/(^|[^*\w])\*([^*\s][^*]*?)\*(?!\w)/g, "$1<i>$2</i>")
         .replace(/(^|[^_\w])_([^_\s][^_]*?)_(?!\w)/g, "$1<i>$2</i>")
         .replace(/~~([^~]+)~~/g, "<s>$1</s>");
    s = mentionChips(s, th.mentions);
    s = s.replace(/\u0001(\d+)\u0001/g, (m, i) => {
        const l = links[+i];
        return "<a href=\"" + esc(l.u) + "\" style=\"color:" + link + "; text-decoration:none\">" + inlineText(l.t) + "</a>";
    });
    s = s.replace(/\u0000(\d+)\u0000/g, (m, i) =>
        "<span style=\"font-family:" + MONO + "; background-color:" + codeBg + "; color:" + codeFg + "; font-size:" + (th.codeSize || 13) + "px\">&nbsp;" + esc(codes[+i]) + "&nbsp;</span>");
    return s;
}

// hex reads "#rrggbb" (or a QML color's string) into [r, g, b].
function rgb(c) {
    const m = /^#?([0-9a-f]{2})([0-9a-f]{2})([0-9a-f]{2})/i.exec(String(c || ""));
    return m ? [parseInt(m[1], 16), parseInt(m[2], 16), parseInt(m[3], 16)] : [169, 184, 255];
}
function hex(v) {
    return "#" + v.map(x => ("0" + Math.round(Math.max(0, Math.min(255, x))).toString(16)).slice(-2)).join("");
}

// mentionChips turns "@Name" of a team member into a chip in their color:
// a dim fill of the color behind lighter text.
function mentionChips(s, mentions) {
    if (!mentions || !mentions.length)
        return s;
    for (const m of mentions) {
        if (!m || !m.name)
            continue;
        const c = rgb(m.color);
        const bg = hex(c.map(x => 22 + (x - 22) * 0.2));
        const fg = hex(c.map(x => x + (255 - x) * 0.3));
        const name = m.name.replace(/[.*+?^${}()|[\]\\]/g, "\\$&");
        const re = new RegExp("(^|[^\\w@])@(" + name + ")(?![\\wÀ-ɏ])", "gi");
        s = s.replace(re, (all, pre, n) => pre + "<span style=\"background-color:" + bg + "; color:" + fg + "; font-weight:600\">&nbsp;@" + n + "&nbsp;</span>");
    }
    return s;
}

// inlineText is the text of a link: bold and italics, no nested links.
function inlineText(t) {
    return esc(t).replace(/\*\*([^*]+?)\*\*/g, "<b>$1</b>").replace(/`([^`]+)`/g, "$1");
}

function cells(line) {
    let t = line.trim();
    if (t.startsWith("|"))
        t = t.slice(1);
    if (t.endsWith("|") && !t.endsWith("\\|"))
        t = t.slice(0, -1);
    const out = [];
    let cur = "", code = false;
    for (let i = 0; i < t.length; i++) {
        const c = t[i];
        if (c === "\\" && t[i + 1] === "|") {
            cur += "|";
            i++;
        } else if (c === "`") {
            code = !code;
            cur += c;
        } else if (c === "|" && !code) {
            out.push(cur.trim());
            cur = "";
        } else
            cur += c;
    }
    out.push(cur.trim());
    return out;
}

function isDivider(line) {
    const c = cells(line);
    return c.length > 0 && c.every(x => /^:?-{1,}:?$/.test(x));
}

const CALLOUTS = {
    NOTE: "nota", INFO: "nota", NOTA: "nota",
    TIP: "dica", DICA: "dica",
    IMPORTANT: "importante", IMPORTANTE: "importante",
    WARNING: "aviso", AVISO: "aviso", ATENCAO: "aviso", "ATENÇÃO": "aviso",
    CAUTION: "perigo", PERIGO: "perigo"
};

// parse reads the whole text into blocks.
function parse(text, theme) {
    const lines = String(text || "").replace(/\r\n?/g, "\n").split("\n");
    const blocks = [];
    let para = [];
    const flush = () => {
        if (para.length) {
            blocks.push({ k: "p", html: para.map(l => inline(l, theme)).join("<br>") });
            para = [];
        }
    };
    let i = 0;
    while (i < lines.length) {
        const line = lines[i];
        const t = line.trim();

        // Fenced code.
        const fence = /^ {0,3}(`{3,}|~{3,})\s*([\w+#.-]*)/.exec(line);
        if (fence) {
            flush();
            const body = [];
            i++;
            while (i < lines.length && !new RegExp("^ {0,3}" + fence[1][0] + "{" + fence[1].length + ",}\\s*$").test(lines[i]))
                body.push(lines[i++]);
            i++;
            blocks.push({ k: "code", lang: fence[2] || "", code: body.join("\n") });
            continue;
        }
        if (t === "") {
            flush();
            i++;
            continue;
        }
        // A line that is only an image: MessageImages draws it.
        if (/^!\[[^\]]*\]\([^)\s]+\)$/.test(t)) {
            flush();
            i++;
            continue;
        }
        // Headings.
        const h = /^ {0,3}(#{1,6})\s+(.*?)\s*#*\s*$/.exec(line);
        if (h) {
            flush();
            blocks.push({ k: "h", level: h[1].length, html: inline(h[2], theme) });
            i++;
            continue;
        }
        // Rules.
        if (/^ {0,3}([-*_])(\s*\1){2,}\s*$/.test(line)) {
            flush();
            blocks.push({ k: "hr" });
            i++;
            continue;
        }
        // Tables: a header row, a divider, then rows.
        if (t.includes("|") && i + 1 < lines.length && isDivider(lines[i + 1]) && cells(line).length === cells(lines[i + 1]).length) {
            flush();
            const head = cells(line);
            const align = cells(lines[i + 1]).map(c => c.endsWith(":") ? (c.startsWith(":") ? "center" : "right") : "left");
            const rows = [];
            i += 2;
            while (i < lines.length && lines[i].trim() !== "" && lines[i].includes("|")) {
                const r = cells(lines[i]);
                while (r.length < head.length)
                    r.push("");
                rows.push(r.slice(0, head.length));
                i++;
            }
            // Raw text lengths decide the column widths.
            const raw = s => s.replace(/\*\*|`|__/g, "");
            const weight = head.map((hc, c) => Math.max(hc.length, ...rows.map(r => raw(r[c]).length), 3));
            // The longest word of each column, so a column is never narrower
            // than a word (no "preventiv-o").
            const word = head.map((hc, c) => Math.max(...[hc].concat(rows.map(r => r[c])).map(x => Math.max(0, ...raw(x).split(/\s+/).map(w => w.length)))));
            blocks.push({
                k: "table", align: align, weight: weight, word: word,
                head: head.map(c => inline(c, theme)),
                rows: rows.map(r => r.map(c => inline(c, theme)))
            });
            continue;
        }
        // Quotes and callouts (> [!NOTE] ...).
        if (/^ {0,3}>/.test(line)) {
            flush();
            const body = [];
            while (i < lines.length && /^ {0,3}>/.test(lines[i]))
                body.push(lines[i++].replace(/^ {0,3}> ?/, ""));
            let kind = "";
            const m = /^\[!([A-ZÇÃa-zçã]+)\]\s*(.*)$/.exec(body[0] || "");
            if (m) {
                kind = CALLOUTS[m[1].toUpperCase()] || "nota";
                body[0] = m[2];
                if (body[0] === "")
                    body.shift();
            }
            blocks.push({ k: "quote", kind: kind, html: body.map(l => l.trim() === "" ? "" : inline(l, theme)).join("<br>").replace(/(<br>){3,}/g, "<br><br>") });
            continue;
        }
        // Lists (with nesting by indent, and task boxes).
        const li = /^(\s*)([-*+]|\d{1,3}[.)])\s+(.*)$/.exec(line);
        if (li) {
            flush();
            const items = [];
            const ordered = /\d/.test(li[2]);
            let start = ordered ? parseInt(li[2]) : 1;
            while (i < lines.length) {
                const m = /^(\s*)([-*+]|\d{1,3}[.)])\s+(.*)$/.exec(lines[i]);
                if (m) {
                    let body = m[3], task = -1;
                    const tb = /^\[([ xX])\]\s+(.*)$/.exec(body);
                    if (tb) {
                        task = tb[1] === " " ? 0 : 1;
                        body = tb[2];
                    }
                    const depth = Math.min(3, Math.floor(m[1].replace(/\t/g, "    ").length / 2));
                    items.push({ depth: depth, mark: /\d/.test(m[2]) ? parseInt(m[2]) + "." : "", task: task, html: inline(body, theme) });
                    i++;
                } else if (lines[i].trim() !== "" && /^\s{2,}\S/.test(lines[i]) && items.length) {
                    // A continuation line of the item above.
                    items[items.length - 1].html += "<br>" + inline(lines[i].trim(), theme);
                    i++;
                } else
                    break;
            }
            blocks.push({ k: "list", ordered: ordered, start: start, items: items });
            continue;
        }
        para.push(t);
        i++;
    }
    flush();
    return blocks;
}

// flow joins each run of paragraphs, headings and lists into one rich text
// block, so a selection dragged across them keeps going (separate text
// items each keep their own selection). Code, tables, quotes and rules stay
// apart. theme gives tint (list marks), size (body px) and head (heading color).
function flow(blocks, theme) {
    const th = theme || {};
    const tint = String(th.tint || "#a9b8ff").replace(/^#..(......)$/, "#$1");
    const quiet = th.quiet || "#80808a";
    const size = th.size || 15;
    const out = [];
    let run = [];
    const push = () => {
        if (run.length) {
            out.push({ k: "flow", html: run.join("") });
            run = [];
        }
    };
    for (const b of blocks) {
        if (b.k !== "p" && b.k !== "h" && b.k !== "list") {
            push();
            out.push(b);
            continue;
        }
        const first = run.length === 0;
        if (b.k === "p") {
            run.push("<p style=\"margin-top:" + (first ? 0 : 10) + "px; margin-bottom:0px; line-height:145%\">" + b.html + "</p>");
        } else if (b.k === "h") {
            const px = b.level === 1 ? size + 6 : b.level === 2 ? size + 3 : size + 1;
            run.push("<p style=\"margin-top:" + (first ? 4 : b.level <= 2 ? 20 : 14) + "px; margin-bottom:0px; line-height:130%; font-size:" + px +
                     "px; font-weight:600; color:" + (th.head || "#ffffff") + "\">" + b.html + "</p>");
        } else {
            b.items.forEach((it, j) => {
                const top = j === 0 ? (first ? 0 : 10) : 2;
                let mark, color = tint, hang = 16;
                if (it.task >= 0) {
                    mark = it.task ? "☑" : "☐";
                    color = it.task ? tint : quiet;
                    hang = 20;
                } else if (it.mark) {
                    mark = it.mark;
                    hang = 8 + 8 * it.mark.length;
                } else {
                    mark = it.depth ? "◦" : "•";
                    color = it.depth ? quiet : tint;
                }
                run.push("<p style=\"margin-top:" + top + "px; margin-bottom:0px; margin-left:" + (8 + hang + it.depth * 22) + "px; text-indent:-" + hang +
                         "px; line-height:145%\"><span style=\"color:" + color + "; font-weight:600\">" + mark + "</span>&nbsp;&nbsp;" + it.html + "</p>");
            });
        }
    }
    push();
    return out;
}

// plain is the text of a block, for copying a code block or a table.
function tableText(b) {
    const strip = h => h.replace(/<[^>]+>/g, "").replace(/&nbsp;/g, " ").replace(/&lt;/g, "<").replace(/&gt;/g, ">").replace(/&quot;/g, "\"").replace(/&amp;/g, "&").trim();
    return [b.head.map(strip).join("\t")].concat(b.rows.map(r => r.map(strip).join("\t"))).join("\n");
}
