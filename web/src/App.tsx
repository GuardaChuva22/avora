import { useCallback, useEffect, useState } from "react";
import { api, getToken, handleUnauthorized, setToken, type Node } from "./api";
import { BrandMark, IconBolt, IconChat, IconLogout, IconPulse, IconServer } from "./icons";
import { Activity } from "./pages/Activity";
import { Chat } from "./pages/Chat";
import { Login } from "./pages/Login";
import { Nodes } from "./pages/Nodes";
import { Tasks } from "./pages/Tasks";
import { usePoll } from "./util";

const NAV = [
  { path: "/chat", label: "Chat", icon: IconChat },
  { path: "/tarefas", label: "Tarefas", icon: IconBolt },
  { path: "/atividade", label: "Atividade", icon: IconPulse },
  { path: "/nos", label: "Nós", icon: IconServer },
];

function usePath() {
  const [path, setPath] = useState(() => location.pathname);
  useEffect(() => {
    const on = () => setPath(location.pathname);
    addEventListener("popstate", on);
    return () => removeEventListener("popstate", on);
  }, []);
  const navigate = useCallback((to: string) => {
    if (to !== location.pathname) history.pushState(null, "", to);
    setPath(to);
  }, []);
  return [path, navigate] as const;
}

export function App() {
  const [token, setTok] = useState(getToken);
  const [path, navigate] = usePath();

  useEffect(() => {
    handleUnauthorized(() => {
      setToken(null);
      setTok(null);
    });
  }, []);

  useEffect(() => {
    if (path === "/") navigate("/chat");
  }, [path, navigate]);

  if (!token) {
    return (
      <>
        <Backdrop />
        <Login
          onToken={(t) => {
            setToken(t);
            setTok(t);
          }}
        />
      </>
    );
  }

  const [, section, id] = path.split("/");
  const logout = () => {
    setToken(null);
    setTok(null);
  };

  let page;
  if (section === "tarefas") page = <Tasks jobId={id} navigate={navigate} />;
  else if (section === "atividade") page = <Activity />;
  else if (section === "nos") page = <Nodes />;
  else page = <Chat conversationId={id} navigate={navigate} />;

  return (
    <>
      <Backdrop />
      <div className="shell">
        <aside className="sidebar">
          <div className="brand">
            <BrandMark className="brand-mark" />
            <span className="wordmark">Avora</span>
          </div>
          <div className="nav-label">Espaço</div>
          {NAV.map(({ path: p, label, icon: Icon }) => (
            <button key={p} className={`nav-item ${`/${section}` === p ? "active" : ""}`} onClick={() => navigate(p)}>
              <Icon />
              {label}
            </button>
          ))}
          <div className="sidebar-foot">
            <Presence />
            <button className="nav-item" onClick={logout}>
              <IconLogout />
              Sair
            </button>
          </div>
        </aside>

        <header className="topbar">
          <div className="brand">
            <BrandMark className="brand-mark" />
            <span className="wordmark">Avora</span>
          </div>
          <span className="spacer" />
          <button className="btn btn-ghost btn-sm btn-icon" onClick={logout} aria-label="Sair">
            <IconLogout />
          </button>
        </header>

        <main className="main" key={section}>
          {page}
        </main>

        <nav className="tabbar">
          {NAV.map(({ path: p, label, icon: Icon }) => (
            <button key={p} className={`/${section}` === p ? "active" : ""} onClick={() => navigate(p)}>
              <Icon />
              {label}
            </button>
          ))}
        </nav>
      </div>
    </>
  );
}

function Backdrop() {
  return (
    <>
      <div className="atmosphere" />
      <div className="grain" />
    </>
  );
}

function Presence() {
  const [nodes, setNodes] = useState<Node[]>([]);
  usePoll(async () => {
    const n = await api.nodes().catch(() => null);
    if (n) setNodes(n.filter((x) => !x.revoked_at));
  }, 10000);
  if (nodes.length === 0) return null;
  return (
    <div className="presence">
      {nodes.map((n) => (
        <div key={n.id} className="presence-row">
          <span className={`dot ${n.online ? "live" : ""}`} />
          <span>{n.name}</span>
          <span>{n.online ? "online" : "offline"}</span>
        </div>
      ))}
    </div>
  );
}
