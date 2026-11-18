import { useEffect, useRef } from "react";

const rtf = new Intl.RelativeTimeFormat("pt-BR", { numeric: "auto" });

export function ago(iso?: string, now = Date.now()): string {
  if (!iso) return "nunca";
  const s = Math.round((new Date(iso).getTime() - now) / 1000);
  const a = Math.abs(s);
  if (a < 5) return "agora";
  if (a < 60) return rtf.format(s, "second");
  if (a < 3600) return rtf.format(Math.round(s / 60), "minute");
  if (a < 86400) return rtf.format(Math.round(s / 3600), "hour");
  return rtf.format(Math.round(s / 86400), "day");
}

export function clock(iso: string): string {
  return new Date(iso).toLocaleTimeString("pt-BR", { hour: "2-digit", minute: "2-digit", second: "2-digit" });
}

export function day(iso: string): string {
  return new Date(iso).toLocaleDateString("pt-BR", { day: "2-digit", month: "short" });
}

export function seconds(ms?: number): string {
  if (!ms) return "";
  return ms < 10_000 ? `${(ms / 1000).toFixed(1)} s` : `${Math.round(ms / 1000)} s`;
}

export function tokens(n?: number): string {
  if (!n) return "0";
  return n >= 1000 ? `${(n / 1000).toFixed(n >= 10_000 ? 0 : 1)}k` : String(n);
}

// usePoll runs fn now and every ms while enabled; fn may be async.
export function usePoll(fn: () => unknown, ms: number, enabled = true, deps: unknown[] = []) {
  const saved = useRef(fn);
  saved.current = fn;
  useEffect(() => {
    if (!enabled) return;
    let alive = true;
    let timer: number | undefined;
    const tick = async () => {
      try {
        await saved.current();
      } finally {
        if (alive) timer = window.setTimeout(tick, document.hidden ? ms * 4 : ms);
      }
    };
    tick();
    return () => {
      alive = false;
      window.clearTimeout(timer);
    };
    // eslint-disable-next-line react-hooks/exhaustive-deps
  }, [ms, enabled, ...deps]);
}

export const statusLabel: Record<string, string> = {
  queued: "Na fila",
  running: "Executando",
  succeeded: "Concluída",
  failed: "Falhou",
  canceled: "Cancelada",
  timed_out: "Expirou",
};

export const nodeKindLabel: Record<string, string> = { windows: "PC Windows", vps: "VPS" };
