-- A mission is long work an agent runs in the background: an objective, a
-- living checklist, and runs of Claude Code or Codex on one node that
-- continue the same CLI session. When a run ends the agent is woken in the
-- mission's conversation to update the plan and continue or finish.
CREATE TABLE missions (
    id              text PRIMARY KEY,
    agent_id        text NOT NULL,
    conversation_id text NOT NULL,
    title           text NOT NULL,
    objective       text NOT NULL,
    plan            jsonb NOT NULL DEFAULT '[]',
    status          text NOT NULL,
    spec            jsonb NOT NULL,
    target          text NOT NULL,
    node_id         text NOT NULL,
    brief           text NOT NULL DEFAULT '',
    session         text NOT NULL DEFAULT '',
    job_id          text NOT NULL DEFAULT '',
    runs            integer NOT NULL DEFAULT 0,
    last_result     text NOT NULL DEFAULT '',
    summary         text NOT NULL DEFAULT '',
    wake_pending    boolean NOT NULL DEFAULT false,
    created_at      timestamptz NOT NULL,
    updated_at      timestamptz NOT NULL,
    finished_at     timestamptz
);

CREATE INDEX missions_conversation ON missions (conversation_id, created_at);
CREATE INDEX missions_job ON missions (job_id) WHERE job_id <> '';
