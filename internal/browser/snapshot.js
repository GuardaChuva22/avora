function (max, part) {
  // Reads the page as text in reading order, numbering what can be clicked
  // or typed in: "[12] link «Casa 4 quartos» → /imovel/123". The numbered
  // elements stay in window.__avoraRefs for clicar/digitar.
  const refs = [null];
  const elements = [];
  const out = [];
  const visCache = new WeakMap();
  const SKIP = new Set(["SCRIPT", "STYLE", "NOSCRIPT", "TEMPLATE", "HEAD", "SVG", "PATH", "IFRAME", "CANVAS", "VIDEO", "AUDIO", "OBJECT"]);
  const ROLES = /^(button|link|tab|menuitem|menuitemcheckbox|option|checkbox|radio|switch|textbox|combobox|searchbox|slider)$/;
  const BLOCK = /^(block|flex|grid|list-item|table|table-row|table-cell|flow-root)$/;
  const clip = (s, n) => (s.length > n ? s.slice(0, n - 1) + "…" : s);
  const clean = s => (s || "").replace(/\s+/g, " ").trim();

  function visible(el) {
    if (visCache.has(el)) return visCache.get(el);
    let v = true;
    const s = getComputedStyle(el);
    if (s.display === "none" || s.visibility === "hidden" || s.visibility === "collapse" || +s.opacity < 0.05) v = false;
    else if (el.getClientRects().length === 0 && s.display !== "contents") v = false;
    visCache.set(el, v);
    return v;
  }
  function interactive(el) {
    const t = el.tagName;
    if (t === "A") return el.hasAttribute("href") || el.hasAttribute("onclick") || el.getAttribute("role") === "button";
    if (t === "BUTTON" || t === "SELECT" || t === "TEXTAREA" || t === "SUMMARY") return !el.disabled;
    if (t === "INPUT") return el.type !== "hidden" && !el.disabled;
    const role = el.getAttribute("role");
    if (role && ROLES.test(role)) return el.getAttribute("aria-disabled") !== "true";
    if (el.isContentEditable && el.getAttribute("contenteditable") !== null) return true;
    return false;
  }
  function label(el) {
    let s = el.getAttribute("aria-label") || "";
    if (!s) {
      const by = el.getAttribute("aria-labelledby");
      if (by) s = by.split(/\s+/).map(id => (document.getElementById(id) || {}).innerText || "").join(" ");
    }
    if (!s && (el.tagName === "INPUT" || el.tagName === "TEXTAREA" || el.tagName === "SELECT")) {
      if (el.labels && el.labels.length) s = el.labels[0].innerText;
      s = s || el.placeholder || el.name || el.title || "";
    }
    if (!s) s = el.innerText || el.textContent || "";
    if (!s) {
      const img = el.querySelector && el.querySelector("img[alt]");
      s = (img && img.alt) || el.title || "";
    }
    return clip(clean(s), 160);
  }
  function shortHref(href) {
    try {
      const u = new URL(href, location.href);
      if (u.protocol === "javascript:") return "";
      const same = u.host === location.host;
      return clip((same ? "" : u.host) + u.pathname + (u.search.length < 60 ? u.search : ""), 110);
    } catch (e) {
      return "";
    }
  }
  function describe(el) {
    const t = el.tagName;
    const role = el.getAttribute("role");
    const lab = label(el);
    if (t === "INPUT") {
      const type = (el.type || "text").toLowerCase();
      if (type === "checkbox" || type === "radio") return [type === "checkbox" ? "caixa" : "opção", (el.checked ? "[x] " : "[ ] ") + lab];
      if (type === "submit" || type === "button" || type === "reset") return ["botão", clip(clean(el.value) || lab, 160)];
      if (type === "password") return ["campo de senha", lab];
      const val = clean(el.value);
      return ["campo " + type, lab + (val ? " = «" + clip(val, 80) + "»" : "")];
    }
    if (t === "TEXTAREA") return ["caixa de texto", lab + (el.value ? " = «" + clip(clean(el.value), 80) + "»" : "")];
    if (t === "SELECT") {
      const opts = Array.from(el.options).slice(0, 12).map(o => clean(o.text));
      return ["lista", lab + " (escolhida: " + clean((el.selectedOptions[0] || {}).text || "") + "; opções: " + opts.join(" | ") + ")"];
    }
    if (el.isContentEditable && el.getAttribute("contenteditable") !== null) return ["caixa de texto", lab];
    if (t === "A" && !role) return ["link", lab, el.href ? shortHref(el.href) : ""];
    if (role === "link") return ["link", lab];
    if (role === "textbox" || role === "searchbox" || role === "combobox") return ["campo", lab];
    if (role === "checkbox" || role === "switch") return ["caixa", (el.getAttribute("aria-checked") === "true" ? "[x] " : "[ ] ") + lab];
    if (role === "tab") return ["aba", lab];
    if (role === "option" || role === "menuitem") return ["item", lab];
    return ["botão", lab];
  }
  let line = [];
  function flush() {
    const s = line.join(" ").replace(/\s+/g, " ").trim();
    if (s) out.push(s);
    line = [];
  }
  function walk(node, depth) {
    if (depth > 60) return;
    if (node.nodeType === 3) {
      const t = node.textContent;
      if (t && t.trim() && node.parentElement && visible(node.parentElement)) line.push(clean(t));
      return;
    }
    if (node.nodeType !== 1) return;
    const el = node;
    if (SKIP.has(el.tagName.toUpperCase())) return;
    if (el.getAttribute("aria-hidden") === "true") return;
    if (!visible(el)) return;
    if (interactive(el)) {
      const [kind, lab, to] = describe(el);
      if (!lab && kind === "botão" && !el.querySelector("img")) return;
      const ref = refs.length;
      refs.push(el);
      elements.push({ kind, label: lab });
      // Its text is in the label: no need to walk inside.
      line.push("[" + ref + "] " + kind + " «" + lab + "»" + (to ? " → " + to : ""));
      return;
    }
    if (el.tagName === "IMG") {
      const alt = clean(el.alt);
      if (alt && alt.length > 3) line.push("(imagem: " + clip(alt, 120) + ")");
      return;
    }
    const s = getComputedStyle(el);
    const block = BLOCK.test(s.display) || /^H[1-6]$/.test(el.tagName) || el.tagName === "BR";
    if (block) flush();
    if (/^H[1-6]$/.test(el.tagName)) line.push("#".repeat(+el.tagName[1]));
    for (const c of el.childNodes) walk(c, depth + 1);
    if (el.shadowRoot) for (const c of el.shadowRoot.childNodes) walk(c, depth + 1);
    if (block) flush();
  }
  walk(document.body || document.documentElement, 0);
  flush();
  window.__avoraRefs = refs;

  // Drop repeated lines (menus repeated by responsive layouts).
  const seen = new Set();
  const lines = [];
  for (const l of out) {
    if (l.length < 40 && seen.has(l)) continue;
    seen.add(l);
    lines.push(l);
  }
  const body = lines.join("\n");
  const parts = Math.max(1, Math.ceil(body.length / max));
  const p = Math.min(part, parts);
  let chunk = body.slice((p - 1) * max, p * max);
  const sy = Math.round(window.scrollY), h = Math.max(document.documentElement.scrollHeight, 1), vh = window.innerHeight;
  const head = "Página: " + clip(document.title, 120) + "\nEndereço: " + location.href +
    "\nRolagem: " + Math.min(100, Math.round(100 * (sy + vh) / h)) + "% da página visto" +
    (parts > 1 ? "\nParte " + p + " de " + parts + " do texto" : "") + "\n\n";
  if (p < parts) chunk += "\n\n(continua: leia a parte " + (p + 1) + ")";
  return JSON.stringify({ text: head + chunk, parts, elements });
}
