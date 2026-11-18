import { useState } from "react";
import { api, type AvoraEvent } from "../api";
import { PageHead, Segmented } from "../ui";
import { clock, day, seconds, usePoll } from "../util";

type Filter = "all" | "jobs" | "agent" | "chat" | "nodes";

const GROUPS: Record<Filter, (t: string) => boolean> = {
  all: () => true,
  jobs: (t) => t.startsWith("job."),
  agent: (t) => t.startsWith("agent."),
  chat: (t) => t.startsWith("conversation.") || t.startsWith("message.") || t.startsWith("llm."),
  nodes: (t) => t.startsWith("node."),
};

const LABELS: Record<string, string> = {
  "job.created": "Tarefa criada",
  "job.claimed": "Tarefa iniciada",
  "job.succeeded": "Tarefa concluída",
  "job.failed": "Tarefa falhou",
  "job.canceled": "Tarefa cancelada",
  "job.requeued": "Tarefa voltou à fila",
  "job.timed_out": "Tarefa expirou",
  "agent.step": "Passo do agente",
  "node.online": "Nó online",
  "node.paired": "Nó pareado",
  "node.revoked": "Nó revogado",
  "node.pairing_code_created": "Código de pareamento",
  "conversation.created": "Conversa criada",
  "message.created": "Mensagem",
  "llm.failed": "Modelo falhou",
};

function dotFor(t: string) {
  if (t.endsWith("failed") || t.endsWith("timed_out") || t.endsWith("revoked")) return "bad";
  if (t.endsWith("succeeded") || t === "node.online" || t === "node.paired") return "ok";
  if (t === "agent.step" || t === "job.claimed") return "live";
  return "";
}

function describe(e: AvoraEvent): string {
  const p = (e.payload ?? {}) as Record<string, any>;
  switch (e.type) {
    case "job.created":
      return `${p.action} → ${p.target} · ${p.reason ?? ""}`;
    case "agent.step":
      return `${p.tool} ${p.summary ?? ""}`;
    case "job.succeeded":
    case "job.failed": {
      const s = p.stats;
      const parts = [p.error, s && `${s.engine} ${s.model ?? ""}`, s && seconds(s.duration_ms), s && `${s.turns} turnos`];
      return parts.filter(Boolean).join(" · ");
    }
    case "message.created":
      return `${p.role === "user" ? "Você" : "Avora"} · ${p.chars} caracteres${p.completion_tokens ? ` · ${p.completion_tokens} tokens · ${seconds(p.duration_ms)}` : ""}`;
    case "llm.failed":
      return `${p.status ?? ""} ${p.error ?? ""}`;
    case "node.pairing_code_created":
      return `${p.name ?? ""} · ${p.kind ?? ""} · vale até ${p.expires_at ? clock(p.expires_at) : "?"}`;
    case "node.paired":
    case "node.online":
      return [p.name, p.kind].filter(Boolean).join(" · ");
    case "conversation.created":
      return `${p.engine} · ${p.model}`;
    default:
      return Object.entries(p)
        .filter(([, v]) => v !== "" && v !== null && typeof v !== "object")
        .map(([k, v]) => `${k}: ${v}`)
        .join(" · ");
  }
}

export function Activity() {
  const [events, setEvents] = useState<AvoraEvent[]>([]);
  const [filter, setFilter] = useState<Filter>("all");

  usePollEvents(setEvents);
  const shown = events.filter((e) => GROUPS[filter](e.type));
  let lastDay = "";

  return (
    <div className="page">
      <PageHead eyebrow="Auditoria" title="Atividade" sub="Tudo o que o Avora fez, em ordem. O registro não pode ser apagado nem editado." />
      <div className="container">
        <div className="filters">
          <Segmented
            value={filter}
            onChange={setFilter}
            options={[
              { value: "all", label: "Tudo" },
              { value: "jobs", label: "Tarefas" },
              { value: "agent", label: "Agentes" },
              { value: "chat", label: "Chat" },
              { value: "nodes", label: "Nós" },
            ]}
          />
          <span className="chip">
            <span className="dot live" /> ao vivo
          </span>
        </div>
        <div className="card events">
          {shown.length === 0 && <div className="empty">Nada por aqui ainda.</div>}
          {shown.map((e) => {
            const d = day(e.at);
            const header = d !== lastDay;
            lastDay = d;
            return (
              <div key={e.id} style={{ display: "contents" }}>
                {header && (
                  <div className="label" style={{ padding: "14px 20px 6px" }}>
                    {d}
                  </div>
                )}
                <div className="event">
                  <span className="when">{clock(e.at)}</span>
                  <span className="what">
                    <span className={`dot ${dotFor(e.type)}`} />
                    <span>{LABELS[e.type] ?? e.type}</span>
                  </span>
                  <span className="detail-text" title={describe(e)}>
                    {describe(e)}
                  </span>
                  <span className="who">{e.actor.replace("node:", "")}</span>
                </div>
              </div>
            );
          })}
        </div>
      </div>
    </div>
  );
}

function usePollEvents(set: (e: AvoraEvent[]) => void) {
  // Newest 300, refreshed every 4 s; cheap enough for one operator.
  usePoll(async () => {
    const latest = await api.events({ latest: true, limit: 300 }).catch(() => null);
    if (latest) set(latest);
  }, 4000);
}

