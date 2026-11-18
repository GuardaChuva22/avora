-- Emoji agents put on a message instead of answering (D50), one per agent.
ALTER TABLE messages ADD COLUMN reactions jsonb NOT NULL DEFAULT '[]';
