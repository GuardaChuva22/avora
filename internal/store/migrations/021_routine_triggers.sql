-- A routine may run on an event instead of a time: new items from a
-- connector's read tool (new e-mail, new ClickUp task) or a node going
-- online/offline. seen keeps the items already handled.
ALTER TABLE routines ADD COLUMN trigger jsonb;
