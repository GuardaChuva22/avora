-- An archived agent leaves the team (lists, routines, #time) but its
-- conversations, docs and versions stay, and it can come back.
ALTER TABLE agents ADD COLUMN archived_at timestamptz;
