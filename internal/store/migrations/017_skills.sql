-- Skills are written know-how (the SKILL.md format of Agent Skills): a name
-- and a description every agent sees, and instructions it loads when the
-- task matches. A skill of a project is seen only by that project's agents
-- and the universal ones. A routine may follow a skill (a workflow).
CREATE TABLE skills (
    id          text PRIMARY KEY,
    name        text NOT NULL,
    description text NOT NULL,
    body        text NOT NULL,
    project_id  text NOT NULL DEFAULT '',
    author      text NOT NULL DEFAULT '',
    created_at  timestamptz NOT NULL,
    updated_at  timestamptz NOT NULL
);

ALTER TABLE routines ADD COLUMN skill text NOT NULL DEFAULT '';
