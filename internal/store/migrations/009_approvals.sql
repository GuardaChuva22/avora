-- Portão de políticas (M6): agent actions waiting for the owner, and the
-- standing "sempre permitir" grants they can turn into.
CREATE TABLE approvals (
    id              text PRIMARY KEY,
    agent_id        text NOT NULL,
    conversation_id text NOT NULL DEFAULT '',
    turn_id         text NOT NULL DEFAULT '',
    action          text NOT NULL,
    summary         text NOT NULL,
    status          text NOT NULL CHECK (status IN ('pending', 'approved', 'denied', 'expired')),
    always          boolean NOT NULL DEFAULT false,
    created_at      timestamptz NOT NULL,
    expires_at      timestamptz NOT NULL,
    decided_at      timestamptz,
    decided_by      text NOT NULL DEFAULT ''
);

CREATE INDEX approvals_status_idx ON approvals (status, created_at DESC);

CREATE TABLE grants (
    id         text PRIMARY KEY,
    agent_id   text NOT NULL REFERENCES agents (id) ON DELETE CASCADE,
    action     text NOT NULL,
    created_at timestamptz NOT NULL,
    created_by text NOT NULL,
    UNIQUE (agent_id, action)
);
