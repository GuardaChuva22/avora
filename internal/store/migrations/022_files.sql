-- Files in the chat: browser prints, images an agent shows, attachments.
CREATE TABLE files (
    id text PRIMARY KEY,
    conversation_id text NOT NULL DEFAULT '',
    agent_id text NOT NULL DEFAULT '',
    name text NOT NULL DEFAULT '',
    mime text NOT NULL,
    data bytea NOT NULL,
    created_at timestamptz NOT NULL
);
CREATE INDEX files_conversation ON files (conversation_id);
