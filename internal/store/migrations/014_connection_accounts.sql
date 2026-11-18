-- Several accounts per app (three Gmails, two ClickUps): each connection
-- has its own id. The first account of an app keeps the app's kind as id,
-- so existing connections and their sealed credentials stay as they are.
ALTER TABLE connections ADD COLUMN id text;
UPDATE connections SET id = kind;
ALTER TABLE connections ALTER COLUMN id SET NOT NULL;
ALTER TABLE connections DROP CONSTRAINT connections_pkey;
ALTER TABLE connections ADD PRIMARY KEY (id);
CREATE INDEX connections_kind ON connections (kind, created_at);
