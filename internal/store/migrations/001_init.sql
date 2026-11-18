CREATE TABLE jobs (
    id               text PRIMARY KEY,
    action           text NOT NULL,
    target           text NOT NULL,
    device_id        text NOT NULL,
    status           text NOT NULL CHECK (status IN ('queued', 'running', 'succeeded', 'failed', 'canceled', 'timed_out')),
    output           text NOT NULL DEFAULT '',
    error            text NOT NULL DEFAULT '',
    attempts         integer NOT NULL DEFAULT 0,
    created_at       timestamptz NOT NULL,
    started_at       timestamptz,
    lease_expires_at timestamptz,
    completed_at     timestamptz
);

CREATE INDEX jobs_queue_idx ON jobs (device_id, created_at, id) WHERE status = 'queued';
CREATE INDEX jobs_lease_idx ON jobs (lease_expires_at) WHERE status = 'running';

CREATE TABLE node_presence (
    node_id   text PRIMARY KEY,
    last_seen timestamptz NOT NULL
);

-- Append-only audit log. No foreign keys: the history must survive any
-- future cleanup of the tables it describes.
CREATE TABLE events (
    id      bigserial PRIMARY KEY,
    at      timestamptz NOT NULL,
    type    text NOT NULL,
    actor   text NOT NULL,
    job_id  text,
    node_id text,
    payload jsonb NOT NULL DEFAULT '{}'
);

CREATE INDEX events_job_idx ON events (job_id, id) WHERE job_id IS NOT NULL;

CREATE FUNCTION events_append_only() RETURNS trigger LANGUAGE plpgsql AS $$
BEGIN
    RAISE EXCEPTION 'events is append-only';
END
$$;

CREATE TRIGGER events_append_only
    BEFORE UPDATE OR DELETE ON events
    FOR EACH ROW EXECUTE FUNCTION events_append_only();
