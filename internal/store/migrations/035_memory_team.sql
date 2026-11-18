-- Phase 4 (proven & team memory) and Phase 8 (sectors, hierarchy, proposals, autonomy).

DO $$
DECLARE
    r record;
BEGIN
    FOR r IN
        SELECT conname FROM pg_constraint
        WHERE conrelid = 'agent_memories'::regclass AND contype = 'f'
    LOOP
        EXECUTE 'ALTER TABLE agent_memories DROP CONSTRAINT ' || quote_ident(r.conname);
    END LOOP;
END $$;

ALTER TABLE agent_memories
    ALTER COLUMN agent_id SET DEFAULT '',
    ADD COLUMN IF NOT EXISTS workspace_id text NOT NULL DEFAULT 'pessoal',
    ADD COLUMN IF NOT EXISTS scope text NOT NULL DEFAULT 'agent' CHECK (scope IN ('agent', 'team')),
    ADD COLUMN IF NOT EXISTS kind text NOT NULL DEFAULT 'fact' CHECK (kind IN ('decision', 'fact', 'preference', 'lesson', 'rule')),
    ADD COLUMN IF NOT EXISTS verified boolean NOT NULL DEFAULT false,
    ADD COLUMN IF NOT EXISTS pinned boolean NOT NULL DEFAULT false,
    ADD COLUMN IF NOT EXISTS archived_at timestamptz,
    ADD COLUMN IF NOT EXISTS occurrences integer NOT NULL DEFAULT 1;

UPDATE agent_memories m
SET workspace_id = a.workspace_id
FROM agents a
WHERE m.agent_id = a.id AND m.agent_id <> '';

UPDATE agent_memories
SET verified = true
WHERE source = 'dono';

CREATE INDEX IF NOT EXISTS agent_memories_workspace_idx ON agent_memories (workspace_id, scope, updated_at DESC);
CREATE INDEX IF NOT EXISTS agent_memories_agent_active_idx ON agent_memories (agent_id, updated_at DESC) WHERE archived_at IS NULL;

ALTER TABLE agents
    ADD COLUMN IF NOT EXISTS sector     text NOT NULL DEFAULT '',
    ADD COLUMN IF NOT EXISTS reports_to text NOT NULL DEFAULT '',
    ADD COLUMN IF NOT EXISTS pinned     boolean NOT NULL DEFAULT false,
    ADD COLUMN IF NOT EXISTS autonomy   jsonb NOT NULL DEFAULT '{"can_start_alone":true,"max_parallel":1}',
    ADD COLUMN IF NOT EXISTS apps       jsonb NOT NULL DEFAULT '[]';

CREATE TABLE IF NOT EXISTS sectors (
    id            text PRIMARY KEY,
    workspace_id  text NOT NULL DEFAULT 'pessoal',
    name          text NOT NULL,
    color         text NOT NULL DEFAULT '',
    icon          text NOT NULL DEFAULT '',
    lead_agent_id text NOT NULL DEFAULT '',
    position      integer NOT NULL DEFAULT 0,
    created_at    timestamptz NOT NULL DEFAULT now(),
    updated_at    timestamptz NOT NULL DEFAULT now()
);
CREATE INDEX IF NOT EXISTS sectors_workspace_idx ON sectors (workspace_id, position, created_at);

CREATE TABLE IF NOT EXISTS proposals (
    id              text PRIMARY KEY,
    workspace_id    text NOT NULL DEFAULT 'pessoal',
    conversation_id text NOT NULL DEFAULT '',
    from_agent      text NOT NULL DEFAULT '',
    target_agent    text NOT NULL,
    field           text NOT NULL CHECK (field IN ('instructions', 'role', 'apps')),
    before_value    text NOT NULL DEFAULT '',
    after_value     text NOT NULL DEFAULT '',
    reason          text NOT NULL DEFAULT '',
    status          text NOT NULL DEFAULT 'pending' CHECK (status IN ('pending', 'applied', 'rejected', 'undone')),
    pre_version     integer NOT NULL DEFAULT 0,
    applied_version integer NOT NULL DEFAULT 0,
    created_at      timestamptz NOT NULL DEFAULT now(),
    decided_at      timestamptz
);
CREATE INDEX IF NOT EXISTS proposals_workspace_idx ON proposals (workspace_id, status, created_at DESC);
CREATE INDEX IF NOT EXISTS proposals_target_idx ON proposals (target_agent, created_at DESC);
