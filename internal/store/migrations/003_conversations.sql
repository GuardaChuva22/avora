CREATE TABLE conversations (
    id         text PRIMARY KEY,
    title      text NOT NULL DEFAULT '',
    engine     text NOT NULL,
    model      text NOT NULL,
    created_at timestamptz NOT NULL,
    updated_at timestamptz NOT NULL
);

CREATE INDEX conversations_updated_idx ON conversations (updated_at DESC, id);

CREATE TABLE messages (
    id                bigserial PRIMARY KEY,
    conversation_id   text NOT NULL REFERENCES conversations (id) ON DELETE CASCADE,
    role              text NOT NULL CHECK (role IN ('system', 'user', 'assistant')),
    content           text NOT NULL,
    model             text NOT NULL DEFAULT '',
    prompt_tokens     integer NOT NULL DEFAULT 0,
    completion_tokens integer NOT NULL DEFAULT 0,
    duration_ms       integer NOT NULL DEFAULT 0,
    created_at        timestamptz NOT NULL
);

CREATE INDEX messages_conversation_idx ON messages (conversation_id, id);
