-- A long conversation is compacted: the messages up to upto leave the
-- model's context and a summary takes their place (the owner still sees
-- every message). count says how many times it was compacted.
CREATE TABLE conversation_summaries (
    conversation_id text PRIMARY KEY,
    summary         text NOT NULL,
    upto            bigint NOT NULL,
    count           integer NOT NULL DEFAULT 1,
    updated_at      timestamptz NOT NULL
);
