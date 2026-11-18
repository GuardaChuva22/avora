-- Model providers the owner sets up in Settings: an API (NVIDIA, any
-- OpenAI-compatible service), a local llama.cpp server, or a subscription
-- CLI account (Claude, Codex) logged in on a node. An API key is sealed
-- with the vault key, like a Plugin's credential.
CREATE TABLE providers (
    id         text PRIMARY KEY,
    kind       text NOT NULL,
    name       text NOT NULL,
    base_url   text NOT NULL DEFAULT '',
    account    text NOT NULL DEFAULT '',
    node_id    text NOT NULL DEFAULT '',
    status     text NOT NULL,
    last_error text NOT NULL DEFAULT '',
    models     jsonb NOT NULL DEFAULT '[]',
    secret     bytea NOT NULL DEFAULT '',
    created_at timestamptz NOT NULL,
    updated_at timestamptz NOT NULL
);

-- Small owner choices that outlive a restart (the main model).
CREATE TABLE settings (
    key        text PRIMARY KEY,
    value      jsonb NOT NULL,
    updated_at timestamptz NOT NULL
);
