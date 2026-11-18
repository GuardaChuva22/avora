-- Keep custom MCP apps in the catalog while they await a login or token.
ALTER TABLE connections DROP CONSTRAINT connections_status_check;
ALTER TABLE connections ADD CONSTRAINT connections_status_check
    CHECK (status IN ('ok', 'error', 'reconnect', 'pending'));
