-- Brain options on agents live inside agents.brain (jsonb). Providers gain an
-- optional API wire format ("openai" or "anthropic" for custom endpoints) and
-- per-model metadata (tool support probe result and token pricing in USD/1M).
ALTER TABLE providers ADD COLUMN IF NOT EXISTS api_format text NOT NULL DEFAULT '';
ALTER TABLE providers ADD COLUMN IF NOT EXISTS model_meta jsonb NOT NULL DEFAULT '{}';
