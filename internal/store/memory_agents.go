package store

import (
	"context"
	"sort"
	"time"
)

func cloneAgent(a Agent) Agent {
	a.Brain = a.Brain.Clone()
	a.WorkspaceID = effectiveWorkspace(a.WorkspaceID)
	if a.Responsibilities == nil {
		a.Responsibilities = []Responsibility{}
	} else {
		a.Responsibilities = append([]Responsibility{}, a.Responsibilities...)
	}
	a.Autonomy = NormalizeAutonomy(a.Autonomy)
	if len(a.Apps) > 0 {
		a.Apps = append([]string{}, a.Apps...)
	} else {
		a.Apps = nil
	}
	return a
}

func (m *Memory) ListAgents(ctx context.Context) ([]Agent, error) {
	ws := WorkspaceFromContext(ctx)
	m.mu.Lock()
	defer m.mu.Unlock()
	out := make([]Agent, 0, len(m.agents))
	for _, a := range m.agents {
		cp := cloneAgent(*a)
		if cp.ArchivedAt == nil && (ws == "" || cp.WorkspaceID == ws) {
			out = append(out, cp)
		}
	}
	sortAgents(out)
	return out, nil
}

func sortAgents(out []Agent) {
	sort.Slice(out, func(i, j int) bool {
		if out[i].Position != out[j].Position {
			return out[i].Position < out[j].Position
		}
		return out[i].ID < out[j].ID
	})
}

func (m *Memory) GetAgent(_ context.Context, id string) (Agent, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	a, ok := m.agents[id]
	if !ok {
		return Agent{}, ErrNotFound
	}
	return cloneAgent(*a), nil
}

func (m *Memory) SaveAgent(_ context.Context, a Agent, actor string, now time.Time) (Agent, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	if m.agents == nil {
		m.agents = map[string]*Agent{}
		m.agentVersions = map[string][]AgentVersion{}
	}
	now = ts(now)
	a = cloneAgent(a)
	if prev, ok := m.agents[a.ID]; ok {
		a.CreatedAt, a.Version, a.ProjectID, a.ArchivedAt = prev.CreatedAt, prev.Version+1, prev.ProjectID, prev.ArchivedAt
		a.WorkspaceID = effectiveWorkspace(prev.WorkspaceID)
	} else {
		a.CreatedAt, a.Version = now, 1
	}
	a.UpdatedAt = now
	stored := cloneAgent(a)
	m.agents[a.ID] = &stored
	m.agentVersions[a.ID] = append(m.agentVersions[a.ID], AgentVersion{AgentID: a.ID, Version: a.Version, Agent: cloneAgent(stored), CreatedAt: now})
	m.emit(now, EventAgentSaved, actor, "", "", map[string]any{"agent_id": a.ID, "version": a.Version, "engine": a.Engine})
	return cloneAgent(stored), nil
}

func (m *Memory) AgentVersions(_ context.Context, id string) ([]AgentVersion, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	if _, ok := m.agents[id]; !ok {
		return nil, ErrNotFound
	}
	vs := m.agentVersions[id]
	out := make([]AgentVersion, len(vs))
	for i := range vs {
		v := vs[i]
		v.Agent = cloneAgent(v.Agent)
		out[len(vs)-1-i] = v
	}
	return out, nil
}

func (m *Memory) StartConversationJob(_ context.Context, convID, jobID string) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	c, ok := m.conversations[convID]
	if !ok {
		return ErrNotFound
	}
	if c.JobID != "" {
		return ErrConflict
	}
	c.JobID = jobID
	return nil
}

func (m *Memory) FinishConversationJob(_ context.Context, convID, jobID string, answer ChatMessage, sessionID, sessionNode, actor string) (ChatMessage, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	c, ok := m.conversations[convID]
	if !ok {
		return ChatMessage{}, ErrNotFound
	}
	if jobID == "" || c.JobID != jobID {
		return ChatMessage{}, ErrConflict
	}
	c.JobID = ""
	if sessionID != "" {
		c.SessionID, c.SessionNode = sessionID, sessionNode
	}
	answer.ConversationID, answer.JobID = convID, jobID
	return m.appendLocked(c, answer, actor), nil
}
