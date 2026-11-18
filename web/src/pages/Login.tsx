import { useState, type FormEvent } from "react";
import { api, ApiError } from "../api";
import { ErrorBanner } from "../ui";

export function Login({ onToken }: { onToken: (token: string) => void }) {
  const [token, setToken] = useState("");
  const [busy, setBusy] = useState(false);
  const [error, setError] = useState("");

  async function submit(e: FormEvent) {
    e.preventDefault();
    const t = token.trim();
    if (!t) return;
    setBusy(true);
    setError("");
    try {
      await api.verify(t);
      onToken(t);
    } catch (err) {
      setError(err instanceof ApiError && err.status === 401 ? "Esse token não abre o Avora." : "Não consegui falar com o servidor. O Tailscale está ligado?");
    } finally {
      setBusy(false);
    }
  }

  return (
    <div className="login">
      <div className="card login-card">
        <div className="orb" />
        <h1>Avora</h1>
        <p>Seu funcionário 24h. Entre com o token de admin.</p>
        <form onSubmit={submit}>
          <label className="field">
            <span className="label">Token</span>
            <input
              className="input mono"
              type="password"
              autoComplete="current-password"
              autoFocus
              value={token}
              onChange={(e) => setToken(e.target.value)}
              placeholder="••••••••••••••••••••••••"
            />
          </label>
          {error && <ErrorBanner>{error}</ErrorBanner>}
          <button className="btn btn-primary" disabled={busy || !token.trim()}>
            {busy ? "Conferindo…" : "Entrar"}
          </button>
        </form>
        <div className="hint">
          No PC, copie o token sem mostrar na tela:
          <code>bash scripts/admin-token.sh</code>
        </div>
      </div>
    </div>
  );
}
