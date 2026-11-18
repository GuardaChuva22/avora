-- A goal is an outcome an agent pursues for days under a mandate: what it
-- may do without asking and what it must never do. It keeps a funnel of
-- items (prospects, listings...), a strategy it revises daily, lessons it
-- learned from the owner, pages it watches, and decides when to wake up.
CREATE TABLE goals (
    id              text PRIMARY KEY,
    agent_id        text NOT NULL,
    conversation_id text NOT NULL,
    title           text NOT NULL,
    outcome         text NOT NULL,
    deadline        timestamptz,
    mandate         text NOT NULL DEFAULT '',
    never           text NOT NULL DEFAULT '',
    stages          jsonb NOT NULL DEFAULT '[]',
    strategy        text NOT NULL DEFAULT '',
    lessons         jsonb NOT NULL DEFAULT '[]',
    items           jsonb NOT NULL DEFAULT '[]',
    watches         jsonb NOT NULL DEFAULT '[]',
    status          text NOT NULL,
    next_wake       timestamptz,
    wake_reason     text NOT NULL DEFAULT '',
    last_review     timestamptz,
    created_at      timestamptz NOT NULL,
    updated_at      timestamptz NOT NULL
);

CREATE INDEX goals_conversation ON goals (conversation_id);
CREATE INDEX goals_agent ON goals (agent_id, status);
