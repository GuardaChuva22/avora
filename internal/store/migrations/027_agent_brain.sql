-- What each agent thinks with (D55): a provider, its model and effort; {} = the main model.
ALTER TABLE agents ADD COLUMN brain jsonb NOT NULL DEFAULT '{}';
-- When it asks the owner before acting (D56): '' (locked actions), 'always' or 'never'.
ALTER TABLE agents ADD COLUMN approvals text NOT NULL DEFAULT '';
