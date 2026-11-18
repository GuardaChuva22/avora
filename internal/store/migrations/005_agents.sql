-- Funcionários (M1): each agent is a versioned record; conversations belong
-- to one agent and remember the CLI session they continue.
CREATE TABLE agents (
    id         text PRIMARY KEY,
    name       text NOT NULL,
    role       text NOT NULL DEFAULT '',
    persona    text NOT NULL DEFAULT '',
    engine     text NOT NULL CHECK (engine IN ('native', 'claude', 'codex')),
    model      text NOT NULL DEFAULT '',
    mode       text NOT NULL DEFAULT 'read' CHECK (mode IN ('read', 'edit')),
    color      text NOT NULL DEFAULT '',
    position   integer NOT NULL DEFAULT 0,
    version    integer NOT NULL,
    created_at timestamptz NOT NULL,
    updated_at timestamptz NOT NULL
);

CREATE TABLE agent_versions (
    agent_id   text NOT NULL REFERENCES agents (id) ON DELETE CASCADE,
    version    integer NOT NULL,
    snapshot   jsonb NOT NULL,
    created_at timestamptz NOT NULL,
    PRIMARY KEY (agent_id, version)
);

ALTER TABLE conversations
    ADD COLUMN agent_id     text NOT NULL DEFAULT 'vega',
    ADD COLUMN session_id   text NOT NULL DEFAULT '',
    ADD COLUMN session_node text NOT NULL DEFAULT '',
    ADD COLUMN job_id       text NOT NULL DEFAULT '',
    ADD COLUMN preview      text NOT NULL DEFAULT '',
    ADD COLUMN last_role    text NOT NULL DEFAULT '';

CREATE INDEX conversations_agent_idx ON conversations (agent_id, updated_at DESC, id);

ALTER TABLE messages
    ADD COLUMN job_id text NOT NULL DEFAULT '',
    ADD COLUMN steps  jsonb,
    ADD COLUMN error  text NOT NULL DEFAULT '';

UPDATE conversations c
SET preview = left(regexp_replace(m.content, '\s+', ' ', 'g'), 140), last_role = m.role
FROM (SELECT DISTINCT ON (conversation_id) conversation_id, content, role
      FROM messages ORDER BY conversation_id, id DESC) m
WHERE m.conversation_id = c.id;
