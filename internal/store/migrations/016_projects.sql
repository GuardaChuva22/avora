-- Projects group agents around one piece of the owner's life (work, a
-- product): their own instructions (the AGENTS.md of the main folder) and
-- folders on a node. An agent in a project only sees that project's agents
-- and the universal ones (no project).
CREATE TABLE projects (
    id           text PRIMARY KEY,
    name         text NOT NULL,
    color        text NOT NULL DEFAULT '',
    instructions text NOT NULL DEFAULT '',
    folders      jsonb NOT NULL DEFAULT '[]',
    position     integer NOT NULL DEFAULT 0,
    created_at   timestamptz NOT NULL,
    updated_at   timestamptz NOT NULL
);

ALTER TABLE agents ADD COLUMN project_id text NOT NULL DEFAULT '';
