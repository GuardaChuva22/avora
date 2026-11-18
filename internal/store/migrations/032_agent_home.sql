-- Computers per workspace, each agent's home computer/folder, and shared local providers (Phase F).
ALTER TABLE nodes ADD COLUMN IF NOT EXISTS workspace_id text NOT NULL DEFAULT 'pessoal';
CREATE INDEX IF NOT EXISTS nodes_workspace ON nodes (workspace_id);

ALTER TABLE pairing_codes ADD COLUMN IF NOT EXISTS workspace_id text NOT NULL DEFAULT 'pessoal';

ALTER TABLE agents ADD COLUMN IF NOT EXISTS home_node_id text NOT NULL DEFAULT '';
ALTER TABLE agents ADD COLUMN IF NOT EXISTS home_dir text NOT NULL DEFAULT '';

ALTER TABLE providers ADD COLUMN IF NOT EXISTS node_id text NOT NULL DEFAULT '';
ALTER TABLE providers ALTER COLUMN node_id SET DEFAULT '';
ALTER TABLE providers ADD COLUMN IF NOT EXISTS shared_with text NOT NULL DEFAULT '[]';
