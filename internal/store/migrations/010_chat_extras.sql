-- Replies to a specific message, archived conversations and agent photos.
ALTER TABLE messages ADD COLUMN reply_to bigint NOT NULL DEFAULT 0;
ALTER TABLE conversations ADD COLUMN archived_at timestamptz;

CREATE TABLE agent_avatars (
    agent_id     text PRIMARY KEY REFERENCES agents (id) ON DELETE CASCADE,
    data         bytea NOT NULL,
    content_type text NOT NULL,
    updated_at   timestamptz NOT NULL
);
