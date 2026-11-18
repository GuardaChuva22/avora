package store

import (
	"bytes"
	"context"
	"encoding/json"
	"sort"
	"sync"
	"time"
)

// Memory is a non-durable Store for tests and local experiments.
type Memory struct {
	mu        sync.Mutex
	jobs      map[string]*Job
	order     []string
	lastSeen  map[string]time.Time
	events    []Event
	nodes     map[string]*memoryNode
	codes     map[string]*memoryCode
	nodeOrder []string

	agents        map[string]*Agent
	agentVersions map[string][]AgentVersion
	routines      map[string]*Routine
	approvals     map[string]*Approval
	grants        map[string]*Grant
	conversations map[string]*Conversation
	avatars       map[string]Avatar
	memories      []AgentMemory
	connections   map[string]Connection
	providers     map[string]Provider
	projects      map[string]Project
	channels      map[string]Channel
	skills        map[string]Skill
	missions      map[string]Mission
	goals         map[string]Goal
	summaries     map[string]ConversationSummary
	files         map[string]File
	docs          map[string]Doc
	docVersions   map[string][]DocVersion
	settings      map[string]json.RawMessage
	people        map[string]*Person
	sessions      map[string]Session
	workspaces    map[string]*Workspace
	members       map[string]map[string]workspaceMemberRow
	invites       map[string]*Invite
	sectors       map[string]*Sector
	proposals     map[string]*Proposal
	nextMemory    int64
	messages      []ChatMessage
	nextMessage   int64
}

type memoryNode struct {
	Node
	tokenHash []byte
}

type memoryCode struct {
	PairingCode
	used bool
}

func NewMemory() *Memory {
	return &Memory{
		jobs: map[string]*Job{}, lastSeen: map[string]time.Time{},
		nodes: map[string]*memoryNode{}, codes: map[string]*memoryCode{},
	}
}

func (m *Memory) CreatePairingCode(_ context.Context, code PairingCode, actor string) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	key := string(code.Hash)
	if _, exists := m.codes[key]; exists {
		return ErrConflict
	}
	code.WorkspaceID = effectiveWorkspace(code.WorkspaceID)
	code.CreatedAt, code.ExpiresAt = ts(code.CreatedAt), ts(code.ExpiresAt)
	m.codes[key] = &memoryCode{PairingCode: code}
	m.emit(code.CreatedAt, EventPairingCodeCreated, actor, "", "",
		map[string]any{"name": code.Name, "kind": code.Kind, "workspace_id": code.WorkspaceID, "expires_at": code.ExpiresAt})
	return nil
}

func (m *Memory) RedeemPairingCode(_ context.Context, codeHash []byte, nodeID string, tokenHash []byte, now time.Time) (Node, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	code, ok := m.codes[string(codeHash)]
	if !ok || code.used || !ts(now).Before(code.ExpiresAt) {
		return Node{}, ErrNotFound
	}
	if _, exists := m.nodes[nodeID]; exists {
		return Node{}, ErrConflict
	}
	for _, n := range m.nodes {
		if bytes.Equal(n.tokenHash, tokenHash) {
			return Node{}, ErrConflict
		}
	}
	code.used = true
	node := Node{ID: nodeID, WorkspaceID: effectiveWorkspace(code.WorkspaceID), Name: code.Name, Kind: code.Kind, CreatedAt: ts(now)}
	m.nodes[nodeID] = &memoryNode{Node: node, tokenHash: append([]byte(nil), tokenHash...)}
	m.nodeOrder = append(m.nodeOrder, nodeID)
	m.emit(now, EventNodePaired, NodeActor(nodeID), "", nodeID, map[string]any{"name": node.Name, "kind": node.Kind, "workspace_id": node.WorkspaceID})
	return node, nil
}

func (m *Memory) NodeByToken(_ context.Context, tokenHash []byte) (Node, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	for _, n := range m.nodes {
		if n.RevokedAt == nil && bytes.Equal(n.tokenHash, tokenHash) {
			return m.withPresence(n.Node), nil
		}
	}
	return Node{}, ErrNotFound
}

func (m *Memory) withPresence(n Node) Node {
	n.WorkspaceID = effectiveWorkspace(n.WorkspaceID)
	if seen, ok := m.lastSeen[n.ID]; ok {
		n.LastSeen = &seen
	}
	return n
}

func (m *Memory) ListNodes(ctx context.Context) ([]Node, error) {
	ws := WorkspaceFromContext(ctx)
	m.mu.Lock()
	defer m.mu.Unlock()
	out := make([]Node, 0, len(m.nodes))
	for _, id := range m.nodeOrder {
		n := m.withPresence(m.nodes[id].Node)
		if ws != "" && n.WorkspaceID != ws {
			continue
		}
		out = append(out, n)
	}
	sort.SliceStable(out, func(i, j int) bool { return out[i].CreatedAt.Before(out[j].CreatedAt) })
	return out, nil
}

func (m *Memory) RevokeNode(_ context.Context, id, actor string, now time.Time) (Node, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	n, ok := m.nodes[id]
	if !ok {
		return Node{}, ErrNotFound
	}
	if n.RevokedAt != nil {
		return Node{}, ErrConflict
	}
	revoked := ts(now)
	n.RevokedAt = &revoked
	m.emit(now, EventNodeRevoked, actor, "", id, nil)
	for _, jobID := range m.order {
		job := m.jobs[jobID]
		if job.DeviceID != id || job.Status.Terminal() {
			continue
		}
		previous := job.Status
		job.Status, job.CompletedAt, job.LeaseExpiresAt = StatusCanceled, &revoked, nil
		m.emit(now, EventJobCanceled, actor, jobID, id, map[string]any{"from": previous, "reason": "node_revoked"})
	}
	return m.withPresence(n.Node), nil
}

func (m *Memory) Durable() bool { return false }
func (m *Memory) Close()        {}

func (m *Memory) emit(at time.Time, typ, actor, jobID, nodeID string, data any) {
	m.events = append(m.events, Event{
		ID: int64(len(m.events) + 1), At: ts(at), Type: typ, Actor: actor,
		JobID: jobID, NodeID: nodeID, Payload: payload(data),
	})
}

func (m *Memory) CreateJob(_ context.Context, job Job, reason, actor string) (Job, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	if _, exists := m.jobs[job.ID]; exists {
		return Job{}, ErrConflict
	}
	job.Status = StatusQueued
	job.CreatedAt = ts(job.CreatedAt)
	job.Attempts = 0
	stored := job
	m.jobs[job.ID] = &stored
	m.order = append(m.order, job.ID)
	m.emit(job.CreatedAt, EventJobCreated, actor, job.ID, job.DeviceID,
		map[string]any{"action": job.Action, "target": job.Target, "reason": reason})
	return stored, nil
}

func (m *Memory) GetJob(_ context.Context, id string) (Job, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	job, ok := m.jobs[id]
	if !ok {
		return Job{}, ErrNotFound
	}
	return *job, nil
}

func (m *Memory) ListJobs(_ context.Context, limit int) ([]Job, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	out := []Job{}
	for i := len(m.order) - 1; i >= 0 && len(out) < limit; i-- {
		out = append(out, *m.jobs[m.order[i]])
	}
	return out, nil
}

func (m *Memory) ClaimJob(_ context.Context, nodeID string, now time.Time, lease time.Duration, slots ClaimSlots) (*Job, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	for _, id := range m.order {
		job := m.jobs[id]
		if job.Status != StatusQueued || job.DeviceID != nodeID ||
			job.Action == ActionAgentRun && !slots.Agents || job.Action == ActionMCPRun && !slots.MCP {
			continue
		}
		if job.Action == ActionCodexMain {
			nested := job.Agent != nil && codexNested(job.Agent.Mode)
			if nested && !slots.CodexNested || !nested && !slots.CodexMain {
				continue
			}
		}
		started, expires := ts(now), ts(now.Add(lease))
		job.Status = StatusRunning
		job.Attempts++
		job.StartedAt, job.LeaseExpiresAt = &started, &expires
		m.emit(now, EventJobClaimed, NodeActor(nodeID), job.ID, nodeID, map[string]any{"attempt": job.Attempts})
		claimed := *job
		return &claimed, nil
	}
	return nil, nil
}

func (m *Memory) CompleteJob(_ context.Context, id, nodeID string, result Result, now time.Time) (Job, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	job, ok := m.jobs[id]
	if !ok {
		return Job{}, ErrNotFound
	}
	if job.DeviceID != nodeID {
		return Job{}, ErrForbidden
	}
	if job.Status != StatusRunning {
		return Job{}, ErrConflict
	}
	job.Status = StatusFailed
	if result.Success {
		job.Status = StatusSucceeded
	}
	done := ts(now)
	job.Output, job.Error, job.CompletedAt, job.LeaseExpiresAt = result.Output, result.Error, &done, nil
	m.emit(now, completionEvent(result.Success), NodeActor(nodeID), id, nodeID, completionPayload(result))
	return *job, nil
}

func (m *Memory) CancelJob(_ context.Context, id, actor string, now time.Time) (Job, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	job, ok := m.jobs[id]
	if !ok {
		return Job{}, ErrNotFound
	}
	if job.Status.Terminal() {
		return Job{}, ErrConflict
	}
	previous := job.Status
	done := ts(now)
	job.Status, job.CompletedAt, job.LeaseExpiresAt = StatusCanceled, &done, nil
	m.emit(now, EventJobCanceled, actor, id, job.DeviceID, map[string]any{"from": previous})
	return *job, nil
}

func (m *Memory) ExpireLeases(_ context.Context, now time.Time, maxAttempts int) ([]Job, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	changed := []Job{}
	for _, id := range m.order {
		job := m.jobs[id]
		if job.Status != StatusRunning || job.LeaseExpiresAt == nil || !job.LeaseExpiresAt.Before(ts(now)) {
			continue
		}
		if job.Attempts < maxAttempts && job.Action != ActionAgentRun && job.Action != ActionMCPRun && job.Action != ActionCodexMain {
			job.Status, job.StartedAt, job.LeaseExpiresAt = StatusQueued, nil, nil
			m.emit(now, EventJobRequeued, ActorSystem, id, job.DeviceID, map[string]any{"attempts": job.Attempts})
		} else {
			done := ts(now)
			job.Status, job.CompletedAt, job.LeaseExpiresAt = StatusTimedOut, &done, nil
			m.emit(now, EventJobTimedOut, ActorSystem, id, job.DeviceID, map[string]any{"attempts": job.Attempts})
		}
		changed = append(changed, *job)
	}
	return changed, nil
}

func (m *Memory) RenewLeases(_ context.Context, nodeID string, ids []string, now time.Time, lease time.Duration) ([]string, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	stop := []string{}
	for _, id := range ids {
		job, ok := m.jobs[id]
		if !ok || job.DeviceID != nodeID || job.Status != StatusRunning {
			stop = append(stop, id)
			continue
		}
		expires := ts(now.Add(lease))
		job.LeaseExpiresAt = &expires
	}
	return stop, nil
}

func (m *Memory) RecordHeartbeat(_ context.Context, nodeID string, now time.Time, onlineTTL time.Duration) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	previous, seen := m.lastSeen[nodeID]
	m.lastSeen[nodeID] = ts(now)
	if !seen || ts(now).Sub(previous) >= onlineTTL {
		m.emit(now, EventNodeOnline, NodeActor(nodeID), "", nodeID, nil)
	}
	return nil
}

func (m *Memory) Events(_ context.Context, filter EventFilter) ([]Event, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	out := []Event{}
	for i := range m.events {
		e := m.events[i]
		if filter.Latest {
			e = m.events[len(m.events)-1-i]
		}
		if e.ID <= filter.AfterID || (filter.JobID != "" && e.JobID != filter.JobID) {
			continue
		}
		out = append(out, e)
		if len(out) == filter.limit() {
			break
		}
	}
	return out, nil
}
