-- What each agent learned and keeps across conversations.
CREATE TABLE agent_memories (
    id         bigserial PRIMARY KEY,
    agent_id   text NOT NULL REFERENCES agents (id) ON DELETE CASCADE,
    content    text NOT NULL,
    source     text NOT NULL DEFAULT '',
    created_at timestamptz NOT NULL,
    updated_at timestamptz NOT NULL
);
CREATE INDEX agent_memories_agent ON agent_memories (agent_id, updated_at DESC);
