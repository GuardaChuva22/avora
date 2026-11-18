CREATE TABLE nodes (
    id         text PRIMARY KEY,
    name       text NOT NULL,
    kind       text NOT NULL CHECK (kind IN ('windows', 'vps')),
    token_hash bytea NOT NULL UNIQUE,
    created_at timestamptz NOT NULL,
    revoked_at timestamptz
);

-- One-time codes that let a new node pair itself. Only the hash is kept.
CREATE TABLE pairing_codes (
    code_hash  bytea PRIMARY KEY,
    name       text NOT NULL,
    kind       text NOT NULL CHECK (kind IN ('windows', 'vps')),
    created_at timestamptz NOT NULL,
    expires_at timestamptz NOT NULL,
    used_at    timestamptz,
    node_id    text
);
