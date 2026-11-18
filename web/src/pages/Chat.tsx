import { useCallback, useEffect, useLayoutEffect, useRef, useState, type KeyboardEvent } from "react";
import { api, ApiError, sendMessage, type ChatMessage, type Conversation } from "../api";
import { IconBack, IconPlus, IconSend, IconStop } from "../icons";
import { ErrorBanner, Markdown } from "../ui";
import { ago, seconds, tokens } from "../util";

const SUGGESTIONS = [
  { t: "Planeje minha semana", s: "Prioridades, blocos de foco e pausas" },
  { t: "Me dê 3 ideias de renda extra", s: "Concretas, com primeiro passo" },
  { t: "Resuma um assunto para mim", s: "Do básico ao que importa" },
  { t: "Revise um texto meu", s: "Clareza, tom e erros" },
];

type Draft = { content: string; reasoning: string };

export function Chat({ conversationId, navigate }: { conversationId?: string; navigate: (path: string) => void }) {
  const [conversations, setConversations] = useState<Conversation[]>([]);
  const [messages, setMessages] = useState<ChatMessage[]>([]);
  const [draft, setDraft] = useState<Draft | null>(null);
  const [input, setInput] = useState("");
  const [error, setError] = useState("");
  const [model, setModel] = useState("");
  const [showList, setShowList] = useState(false);
  const abort = useRef<AbortController | null>(null);
  const scroller = useRef<HTMLDivElement>(null);
  const box = useRef<HTMLTextAreaElement>(null);
  const stick = useRef(true);
  const busy = draft !== null;

  const loadConversations = useCallback(() => api.conversations().then(setConversations).catch(() => {}), []);

  useEffect(() => {
    loadConversations();
    api.capabilities().then((c) => setModel(c.chat.model ?? "")).catch(() => {});
  }, [loadConversations]);

  useEffect(() => {
    setError("");
    if (!conversationId) {
      setMessages([]);
      return;
    }
    if (busy) return; // the stream is filling this conversation
    api.messages(conversationId).then(setMessages).catch((e) => setError(String(e.message ?? e)));
    // eslint-disable-next-line react-hooks/exhaustive-deps
  }, [conversationId]);

  // Follow the answer while it streams, unless the reader scrolled up.
  useLayoutEffect(() => {
    const el = scroller.current;
    // The welcome screen has nothing to follow; keep its orb in view.
    if (el && stick.current && (messages.length > 0 || draft !== null)) el.scrollTop = el.scrollHeight;
  }, [messages, draft]);

  useLayoutEffect(() => {
    const el = box.current;
    if (!el) return;
    el.style.height = "0px";
    el.style.height = `${Math.min(el.scrollHeight, 220)}px`;
  }, [input]);

  async function send(text = input) {
    const content = text.trim();
    if (!content || busy) return;
    setInput("");
    setError("");
    stick.current = true;
    let id = conversationId;
    try {
      if (!id) {
        const c = await api.createConversation(content.slice(0, 60));
        id = c.id;
        setConversations((cs) => [c, ...cs]);
        navigate(`/chat/${c.id}`);
      }
      const ctrl = new AbortController();
      abort.current = ctrl;
      setDraft({ content: "", reasoning: "" });
      setMessages((m) => [...m, { id: -1, conversation_id: id!, role: "user", content, created_at: new Date().toISOString() }]);
      await sendMessage(
        id,
        content,
        {
          onUser: (u) => setMessages((m) => m.map((x) => (x.id === -1 ? u : x))),
          onReasoning: (r) => setDraft((d) => d && { ...d, reasoning: d.reasoning + r }),
          onDelta: (c) => setDraft((d) => d && { ...d, content: d.content + c }),
          onDone: (a) => setMessages((m) => [...m, a]),
        },
        ctrl.signal,
      );
    } catch (e) {
      if ((e as Error).name !== "AbortError") setError(e instanceof ApiError ? e.message : "A conexão caiu no meio da resposta.");
    } finally {
      setDraft(null);
      abort.current = null;
      loadConversations();
    }
  }

  function onKey(e: KeyboardEvent<HTMLTextAreaElement>) {
    if (e.key === "Enter" && !e.shiftKey && !e.nativeEvent.isComposing) {
      e.preventDefault();
      send();
    }
  }

  const empty = messages.length === 0 && !busy;

  return (
    <div className={`chat ${showList ? "show-threads" : ""}`}>
      <aside className="threads">
        <div className="threads-head">
          <h2>Conversas</h2>
          <button
            className="btn btn-ghost btn-sm"
            onClick={() => {
              setShowList(false);
              navigate("/chat");
            }}
          >
            <IconPlus /> Nova
          </button>
        </div>
        <div className="threads-list">
          {conversations.length === 0 && <div className="empty">Nenhuma conversa ainda.</div>}
          {conversations.map((c) => (
            <button key={c.id} className={`thread-item ${c.id === conversationId ? "active" : ""}`} onClick={() => {
                setShowList(false);
                navigate(`/chat/${c.id}`);
              }}
            >
              <span className="t">{c.title || "Sem título"}</span>
              <span className="d">{ago(c.updated_at)}</span>
            </button>
          ))}
        </div>
      </aside>

      <section className="thread">
        <div
          className="thread-scroll"
          ref={scroller}
          onScroll={(e) => {
            const el = e.currentTarget;
            stick.current = el.scrollHeight - el.scrollTop - el.clientHeight < 80;
          }}
        >
          <div className="thread-inner">
            <button className="btn btn-ghost btn-sm mobile-only" style={{ alignSelf: "flex-start" }} onClick={() => setShowList(true)}>
              <IconBack /> Conversas
            </button>
            {empty ? (
              <div className="welcome">
                <div className="orb" />
                <h1>Como posso ajudar?</h1>
                <p>Converse com o Avora. Para ele agir no PC ou na VPS, use Tarefas.</p>
                <div className="suggestions">
                  {SUGGESTIONS.map((s) => (
                    <button key={s.t} className="suggestion sheen" onClick={() => send(s.t)}>
                      {s.t}
                      <small>{s.s}</small>
                    </button>
                  ))}
                </div>
              </div>
            ) : (
              messages.map((m) => <Message key={`${m.id}-${m.role}`} m={m} />)
            )}
            {draft && (
              <div className="msg assistant">
                <div className="orb sm busy" />
                <div className="body">
                  {draft.reasoning && !draft.content && <div className="reasoning">{draft.reasoning}</div>}
                  {draft.content ? (
                    <div className="caret-wrap">
                      <Markdown text={draft.content} />
                      <span className="caret" />
                    </div>
                  ) : (
                    <span className="thinking shimmer">{draft.reasoning ? "Raciocinando…" : "Pensando…"}</span>
                  )}
                </div>
              </div>
            )}
            {error && <ErrorBanner>{error}</ErrorBanner>}
          </div>
        </div>

        <div className="composer-wrap">
          <div className="composer">
            <textarea
              ref={box}
              rows={1}
              value={input}
              placeholder="Mensagem para o Avora…"
              onChange={(e) => setInput(e.target.value)}
              onKeyDown={onKey}
            />
            {busy ? (
              <button className="send" onClick={() => abort.current?.abort()} aria-label="Parar">
                <IconStop />
              </button>
            ) : (
              <button className="send" disabled={!input.trim()} onClick={() => send()} aria-label="Enviar">
                <IconSend />
              </button>
            )}
          </div>
          <div className="composer-note">{model ? `Motor nativo · ${model}` : "Motor nativo"} · Enter envia, Shift+Enter quebra linha</div>
        </div>
      </section>
    </div>
  );
}

function Message({ m }: { m: ChatMessage }) {
  if (m.role === "user") {
    return (
      <div className="msg user">
        <div className="bubble">{m.content}</div>
      </div>
    );
  }
  return (
    <div className="msg assistant">
      <div className="orb sm" />
      <div className="body">
        <Markdown text={m.content} />
        {m.model && (
          <div className="meta">
            <span className="chip">{m.model.split("/").pop()}</span>
            {!!m.completion_tokens && (
              <span className="chip">
                {tokens(m.prompt_tokens)} → {tokens(m.completion_tokens)} tokens
              </span>
            )}
            {!!m.duration_ms && <span className="chip">{seconds(m.duration_ms)}</span>}
          </div>
        )}
      </div>
    </div>
  );
}
