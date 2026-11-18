package store

import (
	"context"
	"database/sql"
	"embed"
	"encoding/json"
	"errors"
	"fmt"
	"io/fs"
	"sort"
	"strconv"
	"strings"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/jackc/pgx/v5/stdlib"
)

//go:embed migrations/*.sql
var migrationFiles embed.FS

// migrationLockID serializes migrations when several processes start at once.
const migrationLockID = 7_406_172_001

type Postgres struct {
	pool *pgxpool.Pool
}

// OpenPostgres connects and applies pending migrations.
func OpenPostgres(ctx context.Context, url string) (*Postgres, error) {
	cfg, err := pgxpool.ParseConfig(url)
	if err != nil {
		return nil, fmt.Errorf("invalid database URL: %w", err)
	}
	return OpenPostgresConfig(ctx, cfg)
}

func OpenPostgresConfig(ctx context.Context, cfg *pgxpool.Config) (*Postgres, error) {
	pool, err := pgxpool.NewWithConfig(ctx, cfg)
	if err != nil {
		return nil, err
	}
	if err := pool.Ping(ctx); err != nil {
		pool.Close()
		return nil, fmt.Errorf("database unreachable: %w", err)
	}
	if err := migrate(ctx, pool); err != nil {
		pool.Close()
		return nil, err
	}
	return &Postgres{pool: pool}, nil
}

func (p *Postgres) Durable() bool { return true }
func (p *Postgres) Close()        { p.pool.Close() }

func migrate(ctx context.Context, pool *pgxpool.Pool) error {
	entries, err := fs.ReadDir(migrationFiles, "migrations")
	if err != nil {
		return err
	}
	type migration struct {
		version int
		name    string
	}
	var pending []migration
	for _, e := range entries {
		prefix, _, ok := strings.Cut(e.Name(), "_")
		version, convErr := strconv.Atoi(prefix)
		if !ok || convErr != nil {
			return fmt.Errorf("migration %q must start with a number and an underscore", e.Name())
		}
		pending = append(pending, migration{version, e.Name()})
	}
	sort.Slice(pending, func(i, j int) bool { return pending[i].version < pending[j].version })

	tx, err := pool.Begin(ctx)
	if err != nil {
		return err
	}
	defer tx.Rollback(ctx)
	if _, err := tx.Exec(ctx, `SELECT pg_advisory_xact_lock($1)`, migrationLockID); err != nil {
		return err
	}
	if _, err := tx.Exec(ctx, `CREATE TABLE IF NOT EXISTS schema_migrations (
		version integer PRIMARY KEY, name text NOT NULL, applied_at timestamptz NOT NULL DEFAULT now())`); err != nil {
		return err
	}
	for _, m := range pending {
		var applied bool
		if err := tx.QueryRow(ctx, `SELECT EXISTS (SELECT 1 FROM schema_migrations WHERE version = $1)`, m.version).Scan(&applied); err != nil {
			return err
		}
		if applied {
			continue
		}
		sql, err := migrationFiles.ReadFile("migrations/" + m.name)
		if err != nil {
			return err
		}
		if _, err := tx.Exec(ctx, string(sql)); err != nil {
			return fmt.Errorf("migration %s: %w", m.name, err)
		}
		if _, err := tx.Exec(ctx, `INSERT INTO schema_migrations (version, name) VALUES ($1, $2)`, m.version, m.name); err != nil {
			return err
		}
	}
	return tx.Commit(ctx)
}

const jobColumns = `id, action, target, device_id, status, output, error, attempts,
	created_at, started_at, lease_expires_at, completed_at, agent`

func scanJob(row pgx.Row) (Job, error) {
	var j Job
	var agent []byte
	err := row.Scan(&j.ID, &j.Action, &j.Target, &j.DeviceID, &j.Status, &j.Output, &j.Error, &j.Attempts,
		&j.CreatedAt, &j.StartedAt, &j.LeaseExpiresAt, &j.CompletedAt, &agent)
	if errors.Is(err, pgx.ErrNoRows) {
		return Job{}, ErrNotFound
	}
	if err != nil {
		return Job{}, err
	}
	if len(agent) > 0 {
		j.Agent = &AgentSpec{}
		if err := json.Unmarshal(agent, j.Agent); err != nil {
			return Job{}, fmt.Errorf("job %s agent: %w", j.ID, err)
		}
	}
	j.CreatedAt = j.CreatedAt.UTC()
	for _, t := range []**time.Time{&j.StartedAt, &j.LeaseExpiresAt, &j.CompletedAt} {
		if *t != nil {
			utc := (**t).UTC()
			*t = &utc
		}
	}
	return j, nil
}

func emit(ctx context.Context, tx pgx.Tx, at time.Time, typ, actor, jobID, nodeID string, data any) error {
	_, err := tx.Exec(ctx, `INSERT INTO events (at, type, actor, job_id, node_id, payload)
		VALUES ($1, $2, $3, NULLIF($4, ''), NULLIF($5, ''), $6)`,
		ts(at), typ, actor, jobID, nodeID, []byte(payload(data)))
	return err
}

// inTx runs fn in a transaction and commits it when fn succeeds.
func (p *Postgres) inTx(ctx context.Context, fn func(pgx.Tx) error) error {
	tx, err := p.pool.Begin(ctx)
	if err != nil {
		return err
	}
	defer tx.Rollback(ctx)
	if err := fn(tx); err != nil {
		return err
	}
	return tx.Commit(ctx)
}

func (p *Postgres) CreateJob(ctx context.Context, job Job, reason, actor string) (Job, error) {
	var created Job
	err := p.inTx(ctx, func(tx pgx.Tx) error {
		var err error
		var agent []byte
		if job.Agent != nil {
			if agent, err = json.Marshal(job.Agent); err != nil {
				return err
			}
		}
		created, err = scanJob(tx.QueryRow(ctx, `INSERT INTO jobs (id, action, target, device_id, status, created_at, agent)
			VALUES ($1, $2, $3, $4, 'queued', $5, $6) ON CONFLICT (id) DO NOTHING RETURNING `+jobColumns,
			job.ID, job.Action, job.Target, job.DeviceID, ts(job.CreatedAt), agent))
		if errors.Is(err, ErrNotFound) {
			return ErrConflict
		}
		if err != nil {
			return err
		}
		return emit(ctx, tx, job.CreatedAt, EventJobCreated, actor, job.ID, job.DeviceID,
			map[string]any{"action": job.Action, "target": job.Target, "reason": reason})
	})
	return created, err
}

func (p *Postgres) GetJob(ctx context.Context, id string) (Job, error) {
	return scanJob(p.pool.QueryRow(ctx, `SELECT `+jobColumns+` FROM jobs WHERE id = $1`, id))
}

func (p *Postgres) ListJobs(ctx context.Context, limit int) ([]Job, error) {
	rows, err := p.pool.Query(ctx, `SELECT `+jobColumns+` FROM jobs ORDER BY created_at DESC, id DESC LIMIT $1`, limit)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []Job{}
	for rows.Next() {
		job, err := scanJob(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, job)
	}
	return out, rows.Err()
}

func (p *Postgres) ClaimJob(ctx context.Context, nodeID string, now time.Time, lease time.Duration, slots ClaimSlots) (*Job, error) {
	var claimed *Job
	err := p.inTx(ctx, func(tx pgx.Tx) error {
		job, err := scanJob(tx.QueryRow(ctx, `UPDATE jobs
			SET status = 'running', attempts = attempts + 1, started_at = $2, lease_expires_at = $3
			WHERE id = (
				SELECT id FROM jobs WHERE device_id = $1 AND status = 'queued'
					AND ($4 OR action <> $5) AND ($6 OR action <> $7)
					AND (action <> $8 OR CASE WHEN agent->>'mode' IN ($9, $12) THEN $10::boolean ELSE $11::boolean END)
				ORDER BY created_at, id LIMIT 1 FOR UPDATE SKIP LOCKED)
			RETURNING `+jobColumns, nodeID, ts(now), ts(now.Add(lease)), slots.Agents, ActionAgentRun, slots.MCP, ActionMCPRun,
			ActionCodexMain, CodexConsultMode, slots.CodexNested, slots.CodexMain, CodexModelsMode))
		if errors.Is(err, ErrNotFound) {
			return nil
		}
		if err != nil {
			return err
		}
		claimed = &job
		return emit(ctx, tx, now, EventJobClaimed, NodeActor(nodeID), job.ID, nodeID, map[string]any{"attempt": job.Attempts})
	})
	return claimed, err
}

func lockJob(ctx context.Context, tx pgx.Tx, id string) (Job, error) {
	return scanJob(tx.QueryRow(ctx, `SELECT `+jobColumns+` FROM jobs WHERE id = $1 FOR UPDATE`, id))
}

func (p *Postgres) CompleteJob(ctx context.Context, id, nodeID string, result Result, now time.Time) (Job, error) {
	var done Job
	err := p.inTx(ctx, func(tx pgx.Tx) error {
		job, err := lockJob(ctx, tx, id)
		if err != nil {
			return err
		}
		if job.DeviceID != nodeID {
			return ErrForbidden
		}
		if job.Status != StatusRunning {
			return ErrConflict
		}
		status := StatusFailed
		if result.Success {
			status = StatusSucceeded
		}
		done, err = scanJob(tx.QueryRow(ctx, `UPDATE jobs
			SET status = $2, output = $3, error = $4, completed_at = $5, lease_expires_at = NULL
			WHERE id = $1 RETURNING `+jobColumns, id, status, result.Output, result.Error, ts(now)))
		if err != nil {
			return err
		}
		return emit(ctx, tx, now, completionEvent(result.Success), NodeActor(nodeID), id, nodeID, completionPayload(result))
	})
	return done, err
}

func (p *Postgres) CancelJob(ctx context.Context, id, actor string, now time.Time) (Job, error) {
	var canceled Job
	err := p.inTx(ctx, func(tx pgx.Tx) error {
		job, err := lockJob(ctx, tx, id)
		if err != nil {
			return err
		}
		if job.Status.Terminal() {
			return ErrConflict
		}
		canceled, err = scanJob(tx.QueryRow(ctx, `UPDATE jobs
			SET status = 'canceled', completed_at = $2, lease_expires_at = NULL
			WHERE id = $1 RETURNING `+jobColumns, id, ts(now)))
		if err != nil {
			return err
		}
		return emit(ctx, tx, now, EventJobCanceled, actor, id, job.DeviceID, map[string]any{"from": job.Status})
	})
	return canceled, err
}

func (p *Postgres) ExpireLeases(ctx context.Context, now time.Time, maxAttempts int) ([]Job, error) {
	changed := []Job{}
	err := p.inTx(ctx, func(tx pgx.Tx) error {
		rows, err := tx.Query(ctx, `SELECT id FROM jobs
			WHERE status = 'running' AND lease_expires_at < $1
			ORDER BY created_at, id FOR UPDATE SKIP LOCKED`, ts(now))
		if err != nil {
			return err
		}
		ids, err := pgx.CollectRows(rows, pgx.RowTo[string])
		if err != nil {
			return err
		}
		for _, id := range ids {
			job, err := scanJob(tx.QueryRow(ctx, `UPDATE jobs SET
				status = CASE WHEN attempts < $2 AND action NOT IN ($4, $5, $6) THEN 'queued' ELSE 'timed_out' END,
				started_at = CASE WHEN attempts < $2 AND action NOT IN ($4, $5, $6) THEN NULL ELSE started_at END,
				completed_at = CASE WHEN attempts < $2 AND action NOT IN ($4, $5, $6) THEN NULL ELSE $3::timestamptz END,
				lease_expires_at = NULL
				WHERE id = $1 RETURNING `+jobColumns, id, maxAttempts, ts(now), ActionAgentRun, ActionMCPRun, ActionCodexMain))
			if err != nil {
				return err
			}
			typ := EventJobRequeued
			if job.Status == StatusTimedOut {
				typ = EventJobTimedOut
			}
			if err := emit(ctx, tx, now, typ, ActorSystem, id, job.DeviceID, map[string]any{"attempts": job.Attempts}); err != nil {
				return err
			}
			changed = append(changed, job)
		}
		return nil
	})
	if err != nil {
		return nil, err
	}
	return changed, nil
}

func (p *Postgres) RenewLeases(ctx context.Context, nodeID string, ids []string, now time.Time, lease time.Duration) ([]string, error) {
	stop := []string{}
	if len(ids) == 0 {
		return stop, nil
	}
	rows, err := p.pool.Query(ctx, `UPDATE jobs SET lease_expires_at = $3
		WHERE id = ANY($2) AND device_id = $1 AND status = 'running' RETURNING id`, nodeID, ids, ts(now.Add(lease)))
	if err != nil {
		return nil, err
	}
	renewed, err := pgx.CollectRows(rows, pgx.RowTo[string])
	if err != nil {
		return nil, err
	}
	still := map[string]bool{}
	for _, id := range renewed {
		still[id] = true
	}
	for _, id := range ids {
		if !still[id] {
			stop = append(stop, id)
		}
	}
	return stop, nil
}

func (p *Postgres) RecordHeartbeat(ctx context.Context, nodeID string, now time.Time, onlineTTL time.Duration) error {
	return p.inTx(ctx, func(tx pgx.Tx) error {
		var previous *time.Time
		err := tx.QueryRow(ctx, `SELECT last_seen FROM node_presence WHERE node_id = $1 FOR UPDATE`, nodeID).Scan(&previous)
		if err != nil && !errors.Is(err, pgx.ErrNoRows) {
			return err
		}
		if _, err := tx.Exec(ctx, `INSERT INTO node_presence (node_id, last_seen) VALUES ($1, $2)
			ON CONFLICT (node_id) DO UPDATE SET last_seen = EXCLUDED.last_seen`, nodeID, ts(now)); err != nil {
			return err
		}
		if previous == nil || ts(now).Sub(*previous) >= onlineTTL {
			return emit(ctx, tx, now, EventNodeOnline, NodeActor(nodeID), "", nodeID, nil)
		}
		return nil
	})
}

const nodeSelect = `SELECT n.id, n.workspace_id, n.name, n.kind, n.created_at, n.revoked_at, p.last_seen
	FROM nodes n LEFT JOIN node_presence p ON p.node_id = n.id`

func scanNode(row pgx.Row) (Node, error) {
	var n Node
	err := row.Scan(&n.ID, &n.WorkspaceID, &n.Name, &n.Kind, &n.CreatedAt, &n.RevokedAt, &n.LastSeen)
	if errors.Is(err, pgx.ErrNoRows) {
		return Node{}, ErrNotFound
	}
	if err != nil {
		return Node{}, err
	}
	n.WorkspaceID = effectiveWorkspace(n.WorkspaceID)
	n.CreatedAt = n.CreatedAt.UTC()
	for _, t := range []**time.Time{&n.RevokedAt, &n.LastSeen} {
		if *t != nil {
			utc := (**t).UTC()
			*t = &utc
		}
	}
	return n, nil
}

func (p *Postgres) CreatePairingCode(ctx context.Context, code PairingCode, actor string) error {
	ws := effectiveWorkspace(code.WorkspaceID)
	return p.inTx(ctx, func(tx pgx.Tx) error {
		tag, err := tx.Exec(ctx, `INSERT INTO pairing_codes (code_hash, workspace_id, name, kind, created_at, expires_at)
			VALUES ($1, $2, $3, $4, $5, $6) ON CONFLICT (code_hash) DO NOTHING`,
			code.Hash, ws, code.Name, code.Kind, ts(code.CreatedAt), ts(code.ExpiresAt))
		if err != nil {
			return err
		}
		if tag.RowsAffected() == 0 {
			return ErrConflict
		}
		return emit(ctx, tx, code.CreatedAt, EventPairingCodeCreated, actor, "", "",
			map[string]any{"name": code.Name, "kind": code.Kind, "workspace_id": ws, "expires_at": ts(code.ExpiresAt)})
	})
}

func (p *Postgres) RedeemPairingCode(ctx context.Context, codeHash []byte, nodeID string, tokenHash []byte, now time.Time) (Node, error) {
	var node Node
	err := p.inTx(ctx, func(tx pgx.Tx) error {
		var ws, name, kind string
		err := tx.QueryRow(ctx, `UPDATE pairing_codes SET used_at = $2, node_id = $3
			WHERE code_hash = $1 AND used_at IS NULL AND expires_at > $2
			RETURNING workspace_id, name, kind`, codeHash, ts(now), nodeID).Scan(&ws, &name, &kind)
		if errors.Is(err, pgx.ErrNoRows) {
			return ErrNotFound
		}
		if err != nil {
			return err
		}
		ws = effectiveWorkspace(ws)
		tag, err := tx.Exec(ctx, `INSERT INTO nodes (id, workspace_id, name, kind, token_hash, created_at)
			VALUES ($1, $2, $3, $4, $5, $6) ON CONFLICT DO NOTHING`, nodeID, ws, name, kind, tokenHash, ts(now))
		if err != nil {
			return err
		}
		if tag.RowsAffected() == 0 {
			return ErrConflict
		}
		node, err = scanNode(tx.QueryRow(ctx, nodeSelect+` WHERE n.id = $1`, nodeID))
		if err != nil {
			return err
		}
		return emit(ctx, tx, now, EventNodePaired, NodeActor(nodeID), "", nodeID, map[string]any{"name": name, "kind": kind, "workspace_id": ws})
	})
	return node, err
}

func (p *Postgres) NodeByToken(ctx context.Context, tokenHash []byte) (Node, error) {
	return scanNode(p.pool.QueryRow(ctx, nodeSelect+` WHERE n.token_hash = $1 AND n.revoked_at IS NULL`, tokenHash))
}

func (p *Postgres) ListNodes(ctx context.Context) ([]Node, error) {
	ws := WorkspaceFromContext(ctx)
	var (
		rows pgx.Rows
		err  error
	)
	if ws != "" {
		rows, err = p.pool.Query(ctx, nodeSelect+` WHERE n.workspace_id = $1 ORDER BY n.created_at, n.id`, ws)
	} else {
		rows, err = p.pool.Query(ctx, nodeSelect+` ORDER BY n.created_at, n.id`)
	}
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []Node{}
	for rows.Next() {
		n, err := scanNode(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, n)
	}
	return out, rows.Err()
}

func (p *Postgres) RevokeNode(ctx context.Context, id, actor string, now time.Time) (Node, error) {
	var node Node
	err := p.inTx(ctx, func(tx pgx.Tx) error {
		var revoked *time.Time
		err := tx.QueryRow(ctx, `SELECT revoked_at FROM nodes WHERE id = $1 FOR UPDATE`, id).Scan(&revoked)
		if errors.Is(err, pgx.ErrNoRows) {
			return ErrNotFound
		}
		if err != nil {
			return err
		}
		if revoked != nil {
			return ErrConflict
		}
		if _, err := tx.Exec(ctx, `UPDATE nodes SET revoked_at = $2 WHERE id = $1`, id, ts(now)); err != nil {
			return err
		}
		if err := emit(ctx, tx, now, EventNodeRevoked, actor, "", id, nil); err != nil {
			return err
		}
		rows, err := tx.Query(ctx, `SELECT id, status FROM jobs
			WHERE device_id = $1 AND status IN ('queued', 'running') ORDER BY created_at, id FOR UPDATE`, id)
		if err != nil {
			return err
		}
		type open struct {
			id     string
			status string
		}
		jobs, err := pgx.CollectRows(rows, func(r pgx.CollectableRow) (open, error) {
			var o open
			return o, r.Scan(&o.id, &o.status)
		})
		if err != nil {
			return err
		}
		for _, j := range jobs {
			if _, err := tx.Exec(ctx, `UPDATE jobs SET status = 'canceled', completed_at = $2, lease_expires_at = NULL
				WHERE id = $1`, j.id, ts(now)); err != nil {
				return err
			}
			if err := emit(ctx, tx, now, EventJobCanceled, actor, j.id, id,
				map[string]any{"from": j.status, "reason": "node_revoked"}); err != nil {
				return err
			}
		}
		node, err = scanNode(tx.QueryRow(ctx, nodeSelect+` WHERE n.id = $1`, id))
		return err
	})
	return node, err
}

func (p *Postgres) Events(ctx context.Context, filter EventFilter) ([]Event, error) {
	order := "id"
	if filter.Latest {
		order = "id DESC"
	}
	rows, err := p.pool.Query(ctx, `SELECT id, at, type, actor, COALESCE(job_id, ''), COALESCE(node_id, ''), payload
		FROM events WHERE id > $1 AND ($2 = '' OR job_id = $2) ORDER BY `+order+` LIMIT $3`,
		filter.AfterID, filter.JobID, filter.limit())
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []Event{}
	for rows.Next() {
		var e Event
		var raw []byte
		if err := rows.Scan(&e.ID, &e.At, &e.Type, &e.Actor, &e.JobID, &e.NodeID, &raw); err != nil {
			return nil, err
		}
		e.At, e.Payload = e.At.UTC(), raw
		out = append(out, e)
	}
	return out, rows.Err()
}

// SQLDB is the same pool as a database/sql handle, for libraries that need
// one (the WhatsApp device store).
func (p *Postgres) SQLDB() *sql.DB { return stdlib.OpenDBFromPool(p.pool) }
