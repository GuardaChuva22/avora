-- Canal #time (M9): one conversation shared by the owner and the whole team.
-- Each answer there records which agent wrote it.
ALTER TABLE messages ADD COLUMN agent_id text NOT NULL DEFAULT '';
