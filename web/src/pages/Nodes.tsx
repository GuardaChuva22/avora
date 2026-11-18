import { useState } from "react";
import { api, type Node } from "../api";
import { IconMonitor, IconServer } from "../icons";
import { PageHead } from "../ui";
import { ago, nodeKindLabel, usePoll } from "../util";

export function Nodes() {
  const [nodes, setNodes] = useState<Node[]>([]);
  const [confirm, setConfirm] = useState("");

  usePoll(async () => {
    const n = await api.nodes().catch(() => null);
    if (n) setNodes(n);
  }, 5000);

  async function revoke(id: string) {
    if (confirm !== id) {
      setConfirm(id);
      window.setTimeout(() => setConfirm((c) => (c === id ? "" : c)), 4000);
      return;
    }
    setConfirm("");
    await api.revokeNode(id);
    setNodes(await api.nodes());
  }

  const active = nodes.filter((n) => !n.revoked_at);
  const revoked = nodes.filter((n) => n.revoked_at);

  return (
    <div className="page">
      <PageHead eyebrow="Infraestrutura" title="Nós" sub="As máquinas onde o Avora trabalha. Só pelo Tailscale, nenhuma porta pública." />
      <div className="container">
        <div className="nodes">
          {active.map((n) => (
            <article key={n.id} className={`card node ${n.online ? "online" : ""}`}>
              <div className="node-head">
                <div className="node-icon">{n.kind === "windows" ? <IconMonitor /> : <IconServer />}</div>
                <div>
                  <div className="node-name">{n.name}</div>
                  <div className="node-kind">{nodeKindLabel[n.kind] ?? n.kind}</div>
                </div>
                <span className="node-state pill" style={n.online ? { color: "var(--white)" } : undefined}>
                  <span className={`dot ${n.online ? "live" : ""}`} />
                  {n.online ? "Online" : "Offline"}
                </span>
              </div>
              <dl className="kv">
                <dt>Último sinal</dt>
                <dd>{ago(n.last_seen)}</dd>
                <dt>Pareado</dt>
                <dd>{ago(n.created_at)}</dd>
                <dt>ID</dt>
                <dd>{n.id}</dd>
              </dl>
              <div style={{ display: "flex", justifyContent: "flex-end" }}>
                <button className="btn btn-danger btn-sm" onClick={() => revoke(n.id)}>
                  {confirm === n.id ? "Confirmar: cortar acesso" : "Revogar"}
                </button>
              </div>
            </article>
          ))}
        </div>
        {revoked.length > 0 && (
          <>
            <div className="label" style={{ margin: "32px 4px 12px" }}>
              Revogados
            </div>
            <div className="card events">
              {revoked.map((n) => (
                <div key={n.id} className="event" style={{ gridTemplateColumns: "1fr auto" }}>
                  <span className="what">
                    <span className="dot bad" />
                    <span>
                      {n.name} · {nodeKindLabel[n.kind] ?? n.kind}
                    </span>
                  </span>
                  <span className="when">{ago(n.revoked_at)}</span>
                </div>
              ))}
            </div>
          </>
        )}
      </div>
    </div>
  );
}
