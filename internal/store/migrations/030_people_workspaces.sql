-- People, sessions, workspaces, members and invites (Phase D).
CREATE TABLE people (
    id            text PRIMARY KEY,
    email         text NOT NULL UNIQUE,
    name          text NOT NULL,
    avatar        text NOT NULL DEFAULT '',
    presence      text NOT NULL DEFAULT 'auto',
    status_text   text NOT NULL DEFAULT '',
    password_hash text NOT NULL,
    last_seen     timestamptz,
    created_at    timestamptz NOT NULL
);

CREATE TABLE sessions (
    token_hash bytea PRIMARY KEY,
    person_id  text NOT NULL REFERENCES people(id) ON DELETE CASCADE,
    expires_at timestamptz NOT NULL,
    created_at timestamptz NOT NULL
);
CREATE INDEX sessions_person ON sessions (person_id);

CREATE TABLE workspaces (
    id            text PRIMARY KEY,
    name          text NOT NULL,
    icon          text NOT NULL DEFAULT '',
    mode          text NOT NULL DEFAULT 'team',
    owner_id      text NOT NULL DEFAULT '',
    approve_joins boolean NOT NULL DEFAULT false,
    created_at    timestamptz NOT NULL
);

INSERT INTO workspaces (id, name, icon, mode, owner_id, approve_joins, created_at)
VALUES ('pessoal', 'Pessoal', '', 'personal', '', false, now())
ON CONFLICT (id) DO NOTHING;

CREATE TABLE workspace_members (
    workspace_id text NOT NULL REFERENCES workspaces(id) ON DELETE CASCADE,
    person_id    text NOT NULL REFERENCES people(id) ON DELETE CASCADE,
    role         text NOT NULL DEFAULT 'member',
    status       text NOT NULL DEFAULT 'active',
    joined_at    timestamptz NOT NULL,
    PRIMARY KEY (workspace_id, person_id)
);
CREATE INDEX workspace_members_person ON workspace_members (person_id);

CREATE TABLE invites (
    id           text PRIMARY KEY,
    workspace_id text NOT NULL REFERENCES workspaces(id) ON DELETE CASCADE,
    code_hash    bytea NOT NULL UNIQUE,
    role         text NOT NULL DEFAULT 'member',
    approval     boolean NOT NULL DEFAULT false,
    expires_at   timestamptz NOT NULL,
    uses         integer NOT NULL DEFAULT 0,
    created_by   text NOT NULL DEFAULT '',
    created_at   timestamptz NOT NULL
);
CREATE INDEX invites_workspace ON invites (workspace_id);

ALTER TABLE agents ADD COLUMN workspace_id text NOT NULL DEFAULT 'pessoal';
CREATE INDEX agents_workspace ON agents (workspace_id);

ALTER TABLE conversations ADD COLUMN workspace_id text NOT NULL DEFAULT 'pessoal';
CREATE INDEX conversations_workspace ON conversations (workspace_id);

ALTER TABLE channels ADD COLUMN workspace_id text NOT NULL DEFAULT 'pessoal';
CREATE INDEX channels_workspace ON channels (workspace_id);

ALTER TABLE projects ADD COLUMN workspace_id text NOT NULL DEFAULT 'pessoal';
CREATE INDEX projects_workspace ON projects (workspace_id);

ALTER TABLE docs ADD COLUMN workspace_id text NOT NULL DEFAULT 'pessoal';
CREATE INDEX docs_workspace ON docs (workspace_id);

ALTER TABLE providers ADD COLUMN workspace_id text NOT NULL DEFAULT 'pessoal';
CREATE INDEX providers_workspace ON providers (workspace_id);
