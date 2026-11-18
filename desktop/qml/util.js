.pragma library

// Formatting and list helpers shared by the pages. Portuguese (Brazil).

var MONTHS = ["jan.", "fev.", "mar.", "abr.", "mai.", "jun.", "jul.", "ago.", "set.", "out.", "nov.", "dez."];

// same tells whether two API payloads are equal. Polls assign only when
// something changed: a new array rebuilds every Repeater row bound to it.
function same(a, b) {
    return JSON.stringify(a) === JSON.stringify(b);
}

function pad(n) {
    return n < 10 ? "0" + n : "" + n;
}

function ago(iso) {
    if (!iso)
        return "nunca";
    var s = Math.round((Date.now() - new Date(iso).getTime()) / 1000);
    if (s < 5)
        return "agora";
    if (s < 60)
        return "há " + s + " s";
    if (s < 3600)
        return "há " + Math.round(s / 60) + " min";
    if (s < 86400)
        return "há " + Math.round(s / 3600) + " h";
    var d = Math.round(s / 86400);
    return "há " + d + (d === 1 ? " dia" : " dias");
}

function clock(iso) {
    var d = new Date(iso);
    return pad(d.getHours()) + ":" + pad(d.getMinutes()) + ":" + pad(d.getSeconds());
}

function day(iso) {
    var d = new Date(iso);
    return pad(d.getDate()) + " de " + MONTHS[d.getMonth()];
}

function seconds(ms) {
    if (!ms)
        return "";
    return ms < 10000 ? (ms / 1000).toFixed(1) + " s" : Math.round(ms / 1000) + " s";
}

function tokens(n) {
    if (!n)
        return "0";
    return n >= 1000 ? (n / 1000).toFixed(n >= 10000 ? 0 : 1) + "k" : "" + n;
}

var STATUS = {
    queued: "Na fila",
    running: "Executando",
    succeeded: "Concluída",
    failed: "Falhou",
    canceled: "Cancelada",
    timed_out: "Expirou"
};

function statusLabel(s) {
    return STATUS[s] || s;
}

function terminal(s) {
    return s !== "queued" && s !== "running";
}

var KIND = { windows: "PC Windows", vps: "VPS" };

function kindLabel(k) {
    return KIND[k] || k;
}

// differs tells whether a ListModel row has a field that item changes.
function differs(row, item) {
    for (var k in item)
        if (row[k] !== item[k])
            return true;
    return false;
}

// syncModel makes a ListModel match items (same order) keyed by "id",
// updating rows in place so views keep their scroll position and delegates.
// Unchanged rows are left alone: a set() re-lays out the row's text, and the
// chats poll every few seconds.
function syncModel(model, items) {
    if (model.count === 0) {
        if (items.length)
            model.append(items);
        return;
    }
    for (var i = 0; i < items.length; i++) {
        var item = items[i];
        if (i < model.count && model.get(i).id === item.id) {
            if (differs(model.get(i), item))
                model.set(i, item);
            continue;
        }
        var found = -1;
        for (var j = i + 1; j < model.count; j++) {
            if (model.get(j).id === item.id) {
                found = j;
                break;
            }
        }
        if (found >= 0) {
            model.remove(i, found - i);
            model.set(i, item);
        } else {
            model.insert(i, item);
        }
    }
    if (model.count > items.length)
        model.remove(items.length, model.count - items.length);
}

var WEEKDAYS = ["dom", "seg", "ter", "qua", "qui", "sex", "sáb"];

// triggerText says what fires a routine on an event.
function triggerText(tr) {
    if (tr.kind === "no")
        return "quando " + (tr.node_name || "o nó") + " ficar " + tr.event;
    var label = tr.tool === "gmail_buscar" ? "e-mail novo" : "tarefa nova no ClickUp";
    var what = [];
    var args = tr.args || {};
    if (args.busca)
        what.push(args.busca);
    if (args.lista_id)
        what.push("lista " + args.lista_id);
    return "quando chegar " + label + (what.length ? " «" + what.join(" · ") + "»" : "");
}

// whenText says when a routine runs: its schedule or its trigger.
function whenText(r) {
    return r.trigger ? triggerText(r.trigger) : scheduleText(r.schedule);
}

// scheduleText says a routine's schedule: "seg, qua às 09:00", "a cada 2 h".
function scheduleText(sc) {
    if (!sc)
        return "";
    if (sc.every)
        return sc.every % 60 === 0 ? "a cada " + sc.every / 60 + " h" : "a cada " + sc.every + " min";
    if (sc.date) {
        var p = sc.date.split("-");
        return "uma vez, " + p[2] + "/" + p[1] + " às " + sc.at;
    }
    var days = sc.days || [];
    if (days.length === 0 || days.length === 7)
        return "todo dia às " + sc.at;
    var weekdays = [1, 2, 3, 4, 5];
    if (days.length === 5 && weekdays.every(function (d) { return days.indexOf(d) >= 0; }))
        return "dias úteis às " + sc.at;
    var names = [];
    for (var i = 1; i <= 7; i++)
        if (days.indexOf(i % 7) >= 0)
            names.push(WEEKDAYS[i % 7]);
    return names.join(", ") + " às " + sc.at;
}

// when says a future time: "hoje 08:00", "amanhã 08:00", "qui 07:30".
function when(iso) {
    if (!iso)
        return "";
    var d = new Date(iso);
    var today = new Date();
    var hm = pad(d.getHours()) + ":" + pad(d.getMinutes());
    var days = Math.round((new Date(d.getFullYear(), d.getMonth(), d.getDate()) - new Date(today.getFullYear(), today.getMonth(), today.getDate())) / 86400000);
    if (days === 0)
        return "hoje " + hm;
    if (days === 1)
        return "amanhã " + hm;
    if (days > 1 && days < 7)
        return WEEKDAYS[d.getDay()] + " " + hm;
    return day(iso) + " " + hm;
}

// esc makes text safe inside a StyledText string.
function esc(t) {
    return String(t).replace(/&/g, "&amp;").replace(/</g, "&lt;").replace(/>/g, "&gt;");
}

function tableCells(line) {
    // Mask escaped characters before counting pipes, including escaped pipes.
    const row = line.trim().replace(/\\./g, "x");
    return row.replace(/^\||\|$/g, "").split("|").map(cell => cell.trim());
}

// Repair presentation without changing the Markdown stored in chat history.
// Images in a message: ![alt](avora:file/<id>) (a print, a file) or
// ![alt](https://...). imagesOf lists them; stripImages takes them out of
// the text, which shows them as a gallery instead.
var IMAGE_RE = /!\[([^\]]*)\]\((avora:file\/[0-9a-f]+|https?:\/\/[^\s)]+)\)/g;
function imagesOf(text) {
    const out = [];
    if (!text || text.indexOf("![") < 0)
        return out;
    IMAGE_RE.lastIndex = 0;
    let m;
    while ((m = IMAGE_RE.exec(text)) !== null && out.length < 12) {
        const src = m[2].startsWith("avora:file/") ? "image://server/v1/files/" + m[2].slice(11) : m[2];
        const id = m[2].startsWith("avora:file/") ? m[2].slice(11).split("#")[0] : "";
        out.push({ alt: m[1], src: src, web: m[2].startsWith("http") ? m[2] : "", id: id });
    }
    return out;
}
function stripImages(text) {
    if (!text || text.indexOf("![") < 0)
        return text;
    return text.replace(IMAGE_RE, "").replace(/\n{3,}/g, "\n\n").trim();
}

function chatMarkdown(text) {
    if (!text.includes("|") && !text.includes("\uFFFD"))
        return text;
    const lines = text.split("\n");
    let fence = "";
    for (let i = 0; i < lines.length; ++i) {
        const marker = /^ {0,3}(`{3,}|~{3,})(.*)$/.exec(lines[i].replace(/\s+$/, ""));
        if (fence) {
            if (marker && marker[1][0] === fence[0] && marker[1].length >= fence.length && marker[2] === "")
                fence = "";
            continue;
        }
        if (marker) {
            fence = marker[1];
            continue;
        }
        // A regional indicator followed by replacement characters is a
        // broken flag. Omit the decoration at the start of headings.
        lines[i] = lines[i].replace(/^( {0,3}#{1,6}[ \t]+)\uD83C[\uDDE6-\uDDFF]\uFFFD+[ \t]+/, "$1");
        if (i < 2 || !lines[i - 2].trim() || !lines[i].includes("|") || !lines[i - 1].includes("|"))
            continue;
        if (/^(?: {4}|\s*\t)/.test(lines[i]) || /^(?: {4}|\s*\t| {0,3}(?:[#>]|[-+*]\s|\d+[.)]\s))/.test(lines[i - 1]))
            continue;
        const divider = tableCells(lines[i]);
        // Qt needs a blank line to separate a table from the paragraph above.
        if (divider.every(cell => /^:?-+:?$/.test(cell)) && divider.length === tableCells(lines[i - 1]).length)
            lines[i - 1] = "\n" + lines[i - 1];
    }
    return lines.join("\n");
}

// What an agent did, for the chat's action cards: its own actions (asking
// Claude Code, creating a routine...). Readings and Claude Code's inner
// steps are not actions and return null. A consulted colleague's action
// comes as "Cora · criar_rotina".
// photo is an agent's picture for Avatar.photo ("" when it has none). The
// version makes a new upload load again.
function photo(a) {
    return a && a.avatar_at ? "image://avatar/" + a.id + "?v=" + encodeURIComponent(a.avatar_at) : "";
}

var ACTIONS = {
    claude: { icon: "claude", label: "Pediu ao Claude Code" },
    codex: { icon: "openai", label: "Pediu ao Codex" },
    pesquisar: { icon: "openai", label: "Pesquisou a fundo" },
    missao_iniciar: { icon: "claude", label: "Começou a missão" },
    missao_atualizar: { icon: "claude", label: "Atualizou a missão" },
    objetivo_criar: { icon: "target", label: "Criou o objetivo" },
    objetivo_item: { icon: "target", label: "Funil" },
    objetivo_atualizar: { icon: "target", label: "Atualizou o objetivo" },
    mandato: { icon: "check", label: "Fez dentro do mandato" },
    criar_rotina: { icon: "clock", label: "Criou a rotina" },
    alterar_rotina: { icon: "clock", label: "Alterou a rotina" },
    apagar_rotina: { icon: "clock", label: "Apagou a rotina" },
    rodar_rotina: { icon: "clock", label: "Rodou a rotina" },
    definir_responsabilidade: { icon: "target", label: "Responsabilidade" },
    remover_responsabilidade: { icon: "target", label: "Removeu a responsabilidade" },
    consultar_colega: { icon: "users", label: "Consultou" },
    avisar_colega: { icon: "users", label: "Passou um recado" },
    escrever_arquivo: { icon: "file", label: "Escreveu na pasta" },
    apagar_arquivo: { icon: "trash", label: "Apagou da pasta" },
    executar: { icon: "terminal", label: "Executou" },
    lembrar: { icon: "bookmark", label: "Guardou na memória" },
    esquecer: { icon: "bookmark", label: "Esqueceu" },
    aprovacao: { icon: "lock", label: "Aprovação" },
    gmail_rascunho: { icon: "mail", label: "Rascunho no Gmail" },
    gmail_enviar: { icon: "mail", label: "Enviou e-mail" },
    drive_criar: { icon: "folder", label: "Drive" },
    agenda_criar: { icon: "calendar", label: "Agenda" },
    clickup_criar_tarefa: { icon: "tasks", label: "Criou tarefa no ClickUp" },
    clickup_atualizar_tarefa: { icon: "tasks", label: "Atualizou tarefa no ClickUp" },
    clickup_comentar: { icon: "tasks", label: "Comentou no ClickUp" },
    elevenlabs_falar: { icon: "wave", label: "Gerou áudio" },
    discord_enviar: { icon: "chat", label: "Enviou no Discord" },
    whatsapp_enviar: { icon: "chat", label: "Enviou no WhatsApp" },
    instagram_publicar: { icon: "image", label: "Publicou no Instagram" },
    instagram_responder_comentario: { icon: "chat", label: "Respondeu no Instagram" },
    instagram_dm: { icon: "chat", label: "Mandou DM no Instagram" },
    tinder_curtir: { icon: "spark", label: "Curtiu no Tinder" },
    tinder_enviar: { icon: "chat", label: "Mandou mensagem no Tinder" },
    tinder_passar: { icon: "close", label: "Passou no Tinder" },
    ifood_criar_sacola: { icon: "tasks", label: "Montou uma sacola no iFood" },
    ifood_adicionar_item: { icon: "tasks", label: "Pôs na sacola do iFood" },
    ifood_alterar_item: { icon: "tasks", label: "Mudou a sacola do iFood" },
    ifood_remover_item: { icon: "close", label: "Tirou da sacola do iFood" },
    ifood_usar_cupom: { icon: "spark", label: "Usou cupom no iFood" },
    ifood_escolher_entrega: { icon: "tasks", label: "Escolheu a entrega no iFood" },
    ifood_escolher_pagamento: { icon: "tasks", label: "Escolheu o pagamento no iFood" },
    ifood_fazer_pedido: { icon: "bolt", label: "Fez o pedido no iFood" },
    nfse_emitir: { icon: "file", label: "Emitiu a NFS-e" },
    nfse_cancelar: { icon: "close", label: "Cancelou a NFS-e" }
};
var READS = {
    gerar_imagem: "Imagem",
    docs_buscar: "Buscou nos Docs",
    docs_ler: "Leu um doc",
    docs_criar: "Escreveu um doc",
    docs_editar: "Editou um doc",
    docs_lixeira: "Moveu um doc para a lixeira",
    docs_guardar_arquivo: "Guardou nos Docs",
    perguntar: "Fez uma pergunta",
    ver_rotinas: "Viu as rotinas",
    estado_do_avora: "Olhou o estado do Avora",
    buscar_na_web: "Pesquisou na web",
    navegador: "Usou o navegador",
    objetivo_ver: "Olhou o objetivo",
    ler_pagina: "Leu a página",
    listar_arquivos: "Olhou a pasta",
    ler_arquivo: "Leu o arquivo",
    recordar: "Consultou a memória",
    gmail_buscar: "Buscou no Gmail",
    gmail_sem_resposta: "Viu os e-mails sem resposta",
    deixar_para_noite: "Deixou para o turno da noite",
    gmail_ler: "Leu um e-mail",
    drive_buscar: "Buscou no Drive",
    drive_ler: "Leu do Drive",
    agenda_eventos: "Olhou a agenda",
    clickup_espacos: "Olhou o ClickUp",
    clickup_tarefas: "Viu as tarefas do ClickUp",
    clickup_tarefa: "Leu uma tarefa do ClickUp",
    elevenlabs_vozes: "Viu as vozes",
    elevenlabs_uso: "Viu o uso do ElevenLabs",
    discord_canais: "Viu os canais do Discord",
    discord_ler: "Leu o Discord",
    whatsapp_conversas: "Viu as conversas do WhatsApp",
    whatsapp_ler: "Leu o WhatsApp",
    whatsapp_midia: "Abriu uma mídia do WhatsApp",
    instagram_perfil: "Olhou o perfil do Instagram",
    instagram_posts: "Viu os posts do Instagram",
    instagram_metricas: "Viu as métricas do Instagram",
    instagram_comentarios: "Leu comentários do Instagram",
    instagram_conversas: "Viu as DMs do Instagram",
    instagram_mensagens: "Leu uma DM do Instagram",
    tinder_perfil: "Olhou o perfil do Tinder",
    tinder_sugestoes: "Viu as sugestões do Tinder",
    tinder_matches: "Viu os matches do Tinder",
    tinder_mensagens: "Leu uma conversa do Tinder",
    ifood_buscar: "Buscou no iFood",
    ifood_explorar: "Explorou o iFood",
    ifood_loja: "Viu o cardápio no iFood",
    ifood_item: "Viu um item no iFood",
    ifood_sacola: "Olhou a sacola do iFood",
    ifood_enderecos: "Viu os endereços do iFood",
    ifood_acompanhar: "Acompanhou o pedido no iFood",
    ifood_pedido: "Viu o pedido do iFood",
    nfse_notas: "Viu as notas emitidas",
    nfse_consultar: "Consultou uma NFS-e",
    nfse_pdf: "Baixou o PDF da NFS-e",
    pedir_conexao: "Pediu para conectar um app"
};

// connectorIcon is the badge of a Plugin.
function connectorIcon(kind) {
    return { google: "mail", "clickup-token": "tasks", elevenlabs: "wave", discord: "chat" }[kind] || "";
}

// escape makes text safe inside StyledText.
function escape(s) {
    return String(s || "").replace(/&/g, "&amp;").replace(/</g, "&lt;").replace(/>/g, "&gt;");
}

function action(st) {
    let tool = st.tool || "", who = "";
    const cut = tool.indexOf(" · ");
    if (cut > 0 && ACTIONS[tool.slice(cut + 3)]) {
        who = tool.slice(0, cut);
        tool = tool.slice(cut + 3);
    }
    const a = ACTIONS[tool];
    if (!a)
        return null;
    let summary = st.summary || "", label = a.label;
    let failed = summary.startsWith("erro: ");
    const note = failed ? " · não deu" : "";
    if (failed)
        summary = summary.slice(6);
    if (tool === "aprovacao") {
        // "permitida: …", "sempre permitida: …", "negada: …", "expirou: …"
        const cut = summary.indexOf(": ");
        const how = cut > 0 ? summary.slice(0, cut) : "";
        label = { "permitida": "Você permitiu", "sempre permitida": "Você permitiu sempre", "negada": "Você negou", "expirou": "Aprovação expirou" }[how] || label;
        summary = cut > 0 ? summary.slice(cut + 2) : summary;
        failed = how === "negada" || how === "expirou";
    }
    // A note opens the colleague's conversation: detail is "agent/conversation".
    const conv = tool === "avisar_colega" && st.detail ? st.detail : "";
    return { icon: a.icon, label: (who ? who + ": " : "") + label, summary: summary, failed: failed, note: note, job: st.job || "", conv: conv };
}

// stepLabel names a step for the live view.
function stepLabel(tool) {
    if (ACTIONS[tool])
        return ACTIONS[tool].label;
    return READS[tool] || tool;
}
