-- Channels (D54): group conversations of the owner with some agents. The
-- messages stay in the conversation with the same id (engine 'team').
CREATE TABLE channels (
    id          text PRIMARY KEY REFERENCES conversations(id) ON DELETE CASCADE,
    name        text NOT NULL,
    purpose     text NOT NULL DEFAULT '',
    project_id  text NOT NULL DEFAULT '',
    everyone    boolean NOT NULL DEFAULT false,
    members     jsonb NOT NULL DEFAULT '[]',
    position    integer NOT NULL DEFAULT 0,
    archived_at timestamptz,
    created_at  timestamptz NOT NULL,
    updated_at  timestamptz NOT NULL
);

-- #time becomes the first channel, with every agent in it.
INSERT INTO channels (id, name, purpose, everyone, created_at, updated_at)
SELECT id, 'time', 'O grupo do time todo: papo, avisos e pedidos rápidos.', true, created_at, now()
FROM conversations WHERE id = 'time'
ON CONFLICT (id) DO NOTHING;
