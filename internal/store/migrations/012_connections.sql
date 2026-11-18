-- Connected apps (Plugins): one account per connector, its state and its
-- credential sealed with the vault key (AES-GCM, the key lives in
-- /etc/avora, never in the database).
CREATE TABLE connections (
    kind       text PRIMARY KEY,
    account    text NOT NULL DEFAULT '',
    status     text NOT NULL CHECK (status IN ('ok', 'error', 'reconnect')),
    last_error text NOT NULL DEFAULT '',
    last_ok    timestamptz,
    agents     text[] NOT NULL DEFAULT '{}',
    secret     bytea NOT NULL,
    created_at timestamptz NOT NULL,
    updated_at timestamptz NOT NULL
);
