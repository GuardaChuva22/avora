// Typed client for the Avora control plane. Same origin: the app is served
// by avora-server itself, so there is no CORS and no API base URL.

export type Conversation = {
  id: string;
  title: string;
  engine: string;
  model: string;
  created_at: string;
  updated_at: string;
};

export type ChatMessage = {
  id: number;
  conversation_id: string;
  role: "user" | "assistant" | "system";
  content: string;
  model?: string;
  prompt_tokens?: number;
  completion_tokens?: number;
  duration_ms?: number;
  created_at: string;
};

export type AgentSpec = {
  engine: "claude" | "codex";
  model?: string;
  prompt: string;
  mode: "read" | "edit";
  workdir?: string;
  max_minutes: number;
};

export type JobStatus = "queued" | "running" | "succeeded" | "failed" | "canceled" | "timed_out";

export type Job = {
  id: string;
  action: string;
  target: "windows" | "vps" | string;
  device_id: string;
  status: JobStatus;
  output?: string;
  error?: string;
  attempts: number;
  created_at: string;
  started_at?: string;
  completed_at?: string;
  agent?: AgentSpec;
};

export type AvoraEvent = {
  id: number;
  at: string;
  type: string;
  actor: string;
  job_id?: string;
  node_id?: string;
  payload: Record<string, unknown> | null;
};

export type Node = {
  id: string;
  name: string;
  kind: "windows" | "vps" | string;
  created_at: string;
  revoked_at?: string;
  last_seen?: string;
  online: boolean;
};

export type Capabilities = {
  chat: { enabled: boolean; engine?: string; provider?: string; model?: string };
  agent: { engines: string[]; modes: string[]; max_minutes: number };
  persistence: boolean;
};

export type AgentStats = {
  engine: string;
  model?: string;
  turns: number;
  duration_ms: number;
  input_tokens: number;
  output_tokens: number;
  cache_read_tokens: number;
  steps: number;
};

const TOKEN_KEY = "avora.token";

export function getToken(): string | null {
  try {
    return localStorage.getItem(TOKEN_KEY);
  } catch {
    return null;
  }
}

export function setToken(token: string | null) {
  try {
    if (token) localStorage.setItem(TOKEN_KEY, token);
    else localStorage.removeItem(TOKEN_KEY);
  } catch {
    /* private mode: the token lives only for this tab */
  }
}

export class ApiError extends Error {
  constructor(
    public status: number,
    message: string,
  ) {
    super(message);
  }
}

let onUnauthorized: () => void = () => {};
export function handleUnauthorized(fn: () => void) {
  onUnauthorized = fn;
}

async function call<T>(method: string, path: string, body?: unknown, token = getToken()): Promise<T> {
  const res = await fetch(path, {
    method,
    headers: {
      Authorization: `Bearer ${token ?? ""}`,
      ...(body !== undefined ? { "Content-Type": "application/json" } : {}),
    },
    body: body !== undefined ? JSON.stringify(body) : undefined,
  });
  if (res.status === 401) {
    onUnauthorized();
    throw new ApiError(401, "Token recusado");
  }
  if (!res.ok) {
    throw new ApiError(res.status, (await res.text()).trim() || res.statusText);
  }
  if (res.status === 204) return undefined as T;
  return (await res.json()) as T;
}

export const api = {
  verify: (token: string) => call<Capabilities>("GET", "/v1/capabilities", undefined, token),
  capabilities: () => call<Capabilities>("GET", "/v1/capabilities"),
  nodes: () => call<Node[]>("GET", "/v1/nodes"),
  revokeNode: (id: string) => call<Node>("POST", `/v1/nodes/${id}/revoke`),
  jobs: (limit = 50) => call<Job[]>("GET", `/v1/jobs?limit=${limit}`),
  job: (id: string) => call<Job>("GET", `/v1/jobs/${id}`),
  submitAgent: (target: string, agent: AgentSpec) =>
    call<Job>("POST", "/v1/jobs", { target, action: "agent.run", agent }),
  cancelJob: (id: string) => call<Job>("POST", `/v1/jobs/${id}/cancel`),
  events: (q: { job?: string; after?: number; limit?: number; latest?: boolean }) => {
    const p = new URLSearchParams();
    if (q.job) p.set("job", q.job);
    if (q.after) p.set("after", String(q.after));
    if (q.limit) p.set("limit", String(q.limit));
    if (q.latest) p.set("order", "latest");
    return call<AvoraEvent[]>("GET", `/v1/events?${p}`);
  },
  conversations: () => call<Conversation[]>("GET", "/v1/conversations?limit=100"),
  createConversation: (title: string) => call<Conversation>("POST", "/v1/conversations", { title }),
  messages: (id: string) => call<ChatMessage[]>("GET", `/v1/conversations/${id}/messages`),
};

export type StreamHandlers = {
  onUser?: (m: ChatMessage) => void;
  onReasoning?: (text: string) => void;
  onDelta?: (text: string) => void;
  onDone?: (m: ChatMessage) => void;
};

// sendMessage posts a message and reads the SSE answer. EventSource cannot
// send an Authorization header, so the stream is parsed from fetch.
export async function sendMessage(conversationId: string, content: string, h: StreamHandlers, signal?: AbortSignal) {
  const res = await fetch(`/v1/conversations/${conversationId}/messages`, {
    method: "POST",
    headers: {
      Authorization: `Bearer ${getToken() ?? ""}`,
      "Content-Type": "application/json",
      Accept: "text/event-stream",
    },
    body: JSON.stringify({ content }),
    signal,
  });
  if (res.status === 401) {
    onUnauthorized();
    throw new ApiError(401, "Token recusado");
  }
  if (!res.ok || !res.body) throw new ApiError(res.status, (await res.text()).trim() || res.statusText);

  const reader = res.body.pipeThrough(new TextDecoderStream()).getReader();
  let buffer = "";
  for (;;) {
    const { value, done } = await reader.read();
    if (done) break;
    buffer += value;
    let cut: number;
    while ((cut = buffer.indexOf("\n\n")) >= 0) {
      const block = buffer.slice(0, cut);
      buffer = buffer.slice(cut + 2);
      let event = "message";
      let data = "";
      for (const line of block.split("\n")) {
        if (line.startsWith("event: ")) event = line.slice(7);
        else if (line.startsWith("data: ")) data += line.slice(6);
      }
      if (!data) continue;
      const parsed = JSON.parse(data);
      if (event === "user") h.onUser?.(parsed);
      else if (event === "reasoning") h.onReasoning?.(parsed.content);
      else if (event === "delta") h.onDelta?.(parsed.content);
      else if (event === "done") h.onDone?.(parsed);
      else if (event === "error") throw new ApiError(502, parsed.error);
    }
  }
}
