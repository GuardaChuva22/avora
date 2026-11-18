package control

import (
	"context"
	"encoding/json"
	"errors"
	"strings"

	"github.com/GuardaChuva22/avora/internal/store"
)

// CodeResult holds the git branch, worktree path and commit recorded in a
// finished agent run's Stats payload.
type CodeResult struct {
	Branch       string `json:"branch,omitempty"`
	Worktree     string `json:"worktree,omitempty"`
	Commit       string `json:"commit,omitempty"`
	FilesChanged int    `json:"files_changed,omitempty"`
}

// CodeAgentConfig stores optional git e-mail and worktree preference for an agent.
type CodeAgentConfig struct {
	Email    string `json:"email,omitempty"`
	Worktree *bool  `json:"worktree,omitempty"`
}

// CodeProjectConfig stores optional worktree preference for a project.
type CodeProjectConfig struct {
	Worktree *bool `json:"worktree,omitempty"`
}

// ParseCodeStats extracts git branch and commit metadata from a JobResult.Stats JSON blob.
func ParseCodeStats(stats json.RawMessage) CodeResult {
	if len(stats) == 0 {
		return CodeResult{}
	}
	var out CodeResult
	_ = json.Unmarshal(stats, &out)
	return out
}

// CodeResultFromEvents reads the git result stored in a job's completion event.
func CodeResultFromEvents(events []store.Event) CodeResult {
	for i := len(events) - 1; i >= 0; i-- {
		e := events[i]
		if e.Type != store.EventJobSucceeded && e.Type != store.EventJobFailed {
			continue
		}
		var env struct {
			Stats json.RawMessage `json:"stats"`
		}
		if json.Unmarshal(e.Payload, &env) == nil && len(env.Stats) > 0 {
			if r := ParseCodeStats(env.Stats); r.Branch != "" || r.Commit != "" {
				return r
			}
		}
	}
	return CodeResult{}
}

// RunCodeResult returns the branch and commit saved for a finished run job.
func (s *Server) RunCodeResult(ctx context.Context, jobID string) (CodeResult, error) {
	if strings.TrimSpace(jobID) == "" {
		return CodeResult{}, store.ErrNotFound
	}
	if _, err := s.store.GetJob(ctx, jobID); err != nil {
		return CodeResult{}, err
	}
	events, err := s.store.Events(ctx, store.EventFilter{JobID: jobID, Limit: store.MaxEventLimit})
	if err != nil {
		return CodeResult{}, err
	}
	return CodeResultFromEvents(events), nil
}

// AgentGitAuthor returns the commit author name and e-mail for an agent.
func AgentGitAuthor(a store.Agent, configuredEmail string) (name, email string) {
	name = strings.TrimSpace(a.Name)
	idSlug := slug(a.ID)
	if idSlug == "" {
		idSlug = slug(name)
	}
	if idSlug == "" {
		idSlug = "agent"
	}
	if name == "" {
		name = idSlug
	}
	email = strings.TrimSpace(configuredEmail)
	if email == "" {
		email = idSlug + "@avora.local"
	}
	return name, email
}

func (s *Server) GetCodeAgentConfig(ctx context.Context, agentID string) (CodeAgentConfig, error) {
	raw, err := s.store.GetSetting(ctx, "code.agent."+strings.TrimSpace(agentID))
	if errors.Is(err, store.ErrNotFound) {
		return CodeAgentConfig{}, nil
	}
	if err != nil {
		return CodeAgentConfig{}, err
	}
	var cfg CodeAgentConfig
	_ = json.Unmarshal(raw, &cfg)
	return cfg, nil
}

func (s *Server) SetCodeAgentConfig(ctx context.Context, agentID string, cfg CodeAgentConfig, actor string) error {
	raw, err := json.Marshal(cfg)
	if err != nil {
		return err
	}
	return s.store.SetSetting(ctx, "code.agent."+strings.TrimSpace(agentID), raw, actor, s.now())
}

func (s *Server) GetCodeProjectConfig(ctx context.Context, projectID string) (CodeProjectConfig, error) {
	raw, err := s.store.GetSetting(ctx, "code.project."+strings.TrimSpace(projectID))
	if errors.Is(err, store.ErrNotFound) {
		return CodeProjectConfig{}, nil
	}
	if err != nil {
		return CodeProjectConfig{}, err
	}
	var cfg CodeProjectConfig
	_ = json.Unmarshal(raw, &cfg)
	return cfg, nil
}

func (s *Server) SetCodeProjectConfig(ctx context.Context, projectID string, cfg CodeProjectConfig, actor string) error {
	raw, err := json.Marshal(cfg)
	if err != nil {
		return err
	}
	return s.store.SetSetting(ctx, "code.project."+strings.TrimSpace(projectID), raw, actor, s.now())
}

// WorktreeEnabledFor reports whether isolated git worktrees are enabled for a
// project and agent (default true; either switch can turn it off).
func (s *Server) WorktreeEnabledFor(ctx context.Context, projectID, agentID string) bool {
	if projectID != "" {
		if cfg, err := s.GetCodeProjectConfig(ctx, projectID); err == nil && cfg.Worktree != nil && !*cfg.Worktree {
			return false
		}
	}
	if agentID != "" {
		if cfg, err := s.GetCodeAgentConfig(ctx, agentID); err == nil && cfg.Worktree != nil && !*cfg.Worktree {
			return false
		}
	}
	return true
}
