import { useEffect, useMemo, useRef, useState, type FormEvent } from "react";
import { api, ApiError, type AgentStats, type AvoraEvent, type Job } from "../api";
import { IconBolt, IconCode, IconEye, IconMonitor, IconPen, IconServer, IconSpark } from "../icons";
import { ErrorBanner, Markdown, PageHead, Segmented, Status } from "../ui";
import { ago, seconds, tokens, usePoll } from "../util";

type Engine = "claude" | "codex";

// Lightest model first: it is the default, so routine tasks spend the least quota.
const MODELS: Record<Engine, { value: string; label: string }[]> = {
  claude: [
    { value: "haiku", label: "Haiku" },
    { value: "sonnet", label: "Sonnet" },
    { value: "opus", label: "Opus" },
  ],
  codex: [
    { value: "gpt-6-luna", label: "Luna" },
    { value: "gpt-6.1-sol", label: "Sol" },
    { value: "gpt-6-astra", label: "Astra" },
  ],
};

const terminal = (s: string) => s !== "queued" && s !== "running";

export function Tasks({ jobId, navigate }: { jobId?: string; navigate: (path: string) => void }) {
  const [jobs, setJobs] = useState<Job[]>([]);
  const [loaded, setLoaded] = useState(false);

  usePoll(
    async () => {
      const all = await api.jobs(60).catch(() => null);
      if (all) {
        setJobs(all.filter((j) => j.action === "agent.run"));
        setLoaded(true);
      }
    },
    3000,
  );

  const selected = jobs.find((j) => j.id === jobId);

  return (
    <div className="page">
      <PageHead eyebrow="Agentes" title="Tarefas" sub="Claude Code e Codex trabalhando pela sua assinatura, no PC ou na VPS." />
      <div className="container tasks">
        <NewTask
          onCreated={(job) => {
            setJobs((js) => [job, ...js]);
            navigate(`/tarefas/${job.id}`);
          }}
        />
        <div className="detail">
          {selected && <TaskDetail key={selected.id} job={selected} onChange={(j) => setJobs((js) => js.map((x) => (x.id === j.id ? j : x)))} />}
          <div className="card">
            <div className="card-pad" style={{ paddingBottom: 6 }}>
              <h2 className="card-title">
                <IconBolt /> Recentes
              </h2>
            </div>
            <div className="task-list">
              {loaded && jobs.length === 0 && (
                <div className="empty">
                  <b>Nenhuma tarefa ainda</b>
                  Descreva uma ao lado e o Avora entrega.
                </div>
              )}
              {jobs.map((j) => (
                <button key={j.id} className={`task-row ${j.id === jobId ? "active" : ""}`} onClick={() => navigate(`/tarefas/${j.id}`)}>
                  <span className="engine">{j.agent?.engine === "codex" ? <IconCode /> : <IconSpark />}</span>
                  <span style={{ minWidth: 0 }}>
                    <div className="p">{j.agent?.prompt ?? j.action}</div>
                    <div className="s">
                      <span>{j.agent?.engine === "codex" ? "Codex" : "Claude"}</span>
                      {j.agent?.model && <span>{j.agent.model}</span>}
                      <span>{j.target === "windows" ? "PC" : "VPS"}</span>
                      <span>{ago(j.created_at)}</span>
                    </div>
                  </span>
                  <Status status={j.status} />
                </button>
              ))}
            </div>
          </div>
        </div>
      </div>
    </div>
  );
}

function NewTask({ onCreated }: { onCreated: (job: Job) => void }) {
  const [prompt, setPrompt] = useState("");
  const [engine, setEngine] = useState<Engine>("claude");
  const [model, setModel] = useState("haiku");
  const [mode, setMode] = useState<"read" | "edit">("read");
  const [target, setTarget] = useState<"auto" | "windows" | "vps">("auto");
  const [workdir, setWorkdir] = useState("");
  const [minutes, setMinutes] = useState(10);
  const [busy, setBusy] = useState(false);
  const [error, setError] = useState("");

  useEffect(() => setModel(MODELS[engine][0].value), [engine]);

  async function submit(e: FormEvent) {
    e.preventDefault();
    if (!prompt.trim()) return;
    setBusy(true);
    setError("");
    try {
      const job = await api.submitAgent(target, {
        engine,
        model,
        prompt: prompt.trim(),
        mode,
        workdir: workdir.trim() || undefined,
        max_minutes: minutes,
      });
      setPrompt("");
      onCreated(job);
    } catch (err) {
      setError(
        err instanceof ApiError && err.status === 409
          ? target === "auto"
            ? "Nenhum nó online agora."
            : `O ${target === "windows" ? "PC" : "VPS"} não está online.`
          : (err as Error).message,
      );
    } finally {
      setBusy(false);
    }
  }

  return (
    <form className="card card-pad task-form" onSubmit={submit}>
      <h2 className="card-title" style={{ margin: 0 }}>
        <IconSpark /> Nova tarefa
      </h2>
      <label className="field">
        <span className="label">O que fazer</span>
        <textarea
          className="textarea"
          rows={5}
          value={prompt}
          placeholder="Ex.: leia os arquivos da pasta e me diga o que falta para terminar o projeto."
          onChange={(e) => setPrompt(e.target.value)}
          onKeyDown={(e) => {
            if (e.key === "Enter" && (e.ctrlKey || e.metaKey)) submit(e);
          }}
        />
      </label>
      <div className="field">
        <span className="label">Motor</span>
        <Segmented
          value={engine}
          onChange={setEngine}
          options={[
            { value: "claude", label: "Claude Code", icon: <IconSpark /> },
            { value: "codex", label: "Codex", icon: <IconCode /> },
          ]}
        />
      </div>
      <div className="field">
        <span className="label">Modelo</span>
        <Segmented value={model} onChange={setModel} options={MODELS[engine]} />
      </div>
      <div className="field">
        <span className="label">Permissão</span>
        <Segmented
          value={mode}
          onChange={setMode}
          options={[
            { value: "read", label: "Só ler", icon: <IconEye /> },
            { value: "edit", label: "Editar arquivos", icon: <IconPen /> },
          ]}
        />
      </div>
      <div className="field">
        <span className="label">Onde</span>
        <Segmented
          value={target}
          onChange={setTarget}
          options={[
            { value: "auto", label: "Automático" },
            { value: "windows", label: "PC", icon: <IconMonitor /> },
            { value: "vps", label: "VPS", icon: <IconServer /> },
          ]}
        />
      </div>
      <div className="row">
        <label className="field">
          <span className="label">Pasta</span>
          <input className="input mono" value={workdir} placeholder="raiz do workspace" onChange={(e) => setWorkdir(e.target.value)} />
        </label>
        <label className="field" style={{ maxWidth: 120 }}>
          <span className="label">Limite (min)</span>
          <input
            className="input mono"
            type="number"
            min={1}
            max={120}
            value={minutes}
            onChange={(e) => setMinutes(Math.max(1, Math.min(120, Number(e.target.value) || 1)))}
          />
        </label>
      </div>
      {error && <ErrorBanner>{error}</ErrorBanner>}
      <button className="btn btn-primary" disabled={busy || !prompt.trim()} style={{ height: 44 }}>
        <IconBolt /> {busy ? "Enviando…" : "Executar"}
      </button>
    </form>
  );
}

function TaskDetail({ job, onChange }: { job: Job; onChange: (j: Job) => void }) {
  const [events, setEvents] = useState<AvoraEvent[]>([]);
  const [canceling, setCanceling] = useState(false);
  const done = terminal(job.status);
  const card = useRef<HTMLDivElement>(null);

  // On phones the detail sits below the form; bring it into view when opened.
  useEffect(() => {
    if (matchMedia("(max-width: 820px)").matches) card.current?.scrollIntoView({ behavior: "smooth", block: "start" });
  }, []);

  usePoll(
    async () => {
      const [ev, fresh] = await Promise.all([api.events({ job: job.id, limit: 500 }), api.job(job.id)]);
      setEvents(ev);
      if (fresh.status !== job.status || fresh.output !== job.output) onChange(fresh);
    },
    1500,
    !done || events.length === 0,
    [job.id, done],
  );

  const steps = events.filter((e) => e.type === "agent.step");
  const stats = useMemo(() => {
    const end = events.find((e) => e.type.startsWith("job.") && (e.payload as { stats?: AgentStats })?.stats);
    return (end?.payload as { stats?: AgentStats } | undefined)?.stats;
  }, [events]);

  async function cancel() {
    setCanceling(true);
    try {
      onChange(await api.cancelJob(job.id));
    } finally {
      setCanceling(false);
    }
  }

  return (
    <div ref={card} className="card card-pad" style={{ display: "flex", flexDirection: "column", gap: 18, scrollMarginTop: 80 }}>
      <div className="detail-head">
        <Status status={job.status} />
        {!done && (
          <button className="btn btn-danger btn-sm" onClick={cancel} disabled={canceling}>
            Cancelar
          </button>
        )}
      </div>
      <div className="prompt">{job.agent?.prompt}</div>
      <div className="chips">
        <span className="chip">{job.agent?.engine === "codex" ? "Codex" : "Claude Code"}</span>
        <span className="chip">{stats?.model ?? job.agent?.model ?? "padrão"}</span>
        <span className="chip">{job.agent?.mode === "edit" ? "editar" : "só ler"}</span>
        <span className="chip">{job.target === "windows" ? "PC" : "VPS"}</span>
        {job.agent?.workdir && <span className="chip">{job.agent.workdir}</span>}
        {stats && (
          <>
            <span className="chip">{seconds(stats.duration_ms)}</span>
            <span className="chip">{stats.turns} turnos</span>
            <span className="chip">
              {tokens(stats.input_tokens + stats.cache_read_tokens)} → {tokens(stats.output_tokens)} tokens
            </span>
          </>
        )}
      </div>

      {(steps.length > 0 || !done) && (
        <div>
          <div className="label" style={{ marginBottom: 10 }}>
            Passos
          </div>
          <div className="timeline">
            {steps.map((s, i) => {
              const p = s.payload as { tool?: string; summary?: string };
              return (
                <div key={s.id} className={`step ${i === steps.length - 1 && !done ? "last" : ""}`}>
                  <span className="tool">{p.tool}</span>
                  <span className="sum" title={p.summary}>
                    {p.summary}
                  </span>
                </div>
              );
            })}
            {!done && (
              <div className="step last">
                <span className="tool shimmer">{job.status === "queued" ? "Na fila, esperando o nó…" : "Trabalhando…"}</span>
              </div>
            )}
          </div>
        </div>
      )}

      {job.output && (
        <div>
          <div className="label" style={{ marginBottom: 10 }}>
            Resultado
          </div>
          <div className="output">
            <Markdown text={job.output} />
          </div>
        </div>
      )}
      {job.error && <ErrorBanner>{job.error}</ErrorBanner>}
    </div>
  );
}
