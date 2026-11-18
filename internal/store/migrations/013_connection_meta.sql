-- What a connection knows besides its credential: for an MCP app, its
-- address and the tools it offered at the last check. Not secret.
ALTER TABLE connections ADD COLUMN meta jsonb NOT NULL DEFAULT '{}';
