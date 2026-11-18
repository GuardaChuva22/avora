package control

import (
	"encoding/json"
	"errors"
	"net/http"
	"path"
	"strings"
	"unicode/utf8"

	"github.com/GuardaChuva22/avora/internal/store"
)

// Limits for agent.run jobs.
const (
	MaxAgentPrompt      = 32 << 10
	DefaultAgentMinutes = 20
	MaxAgentMinutes     = 120
	// MaxJobOutput bounds what a node may report back as a job's output.
	MaxJobOutput = 64 << 10
	maxStepField = 300
)

// AgentEngines are the agent CLIs a node can run.
var AgentEngines = []string{"claude", "codex", "antigravity"}

// AgentModes are the permission levels a run may get. There is no mode with
// shell access until the policy gate (M6) exists.
var AgentModes = []string{"read", "edit"}

// HeartbeatRequest lists the jobs a node is running so their leases are renewed.
type HeartbeatRequest struct {
	Running []string `json:"running"`
	// Quotas are the node's latest readings of its agent subscriptions.
	Quotas []Quota `json:"quotas,omitempty"`
	// Tools are the node tools (yt-dlp, ffmpeg...) installed on the node.
	Tools []ToolInfo `json:"tools,omitempty"`
	// LocalModels are the local model servers (Ollama, LM Studio) detected on this computer.
	LocalModels []LocalModelReport `json:"local_models,omitempty"`
	// SharedDir is the shared folder (<workspace>/shared) on this computer.
	SharedDir string `json:"shared_dir,omitempty"`
}

// HeartbeatResponse tells the node which of its running jobs to stop.
type HeartbeatResponse struct {
	Status string   `json:"status"`
	NodeID string   `json:"node_id"`
	Stop   []string `json:"stop"`
}

// ClaimRequest says which kinds of background work the node can take.
type ClaimRequest = store.ClaimSlots

// AgentStep is one thing an agent did, reported while it runs (a tool call).
type AgentStep struct {
	Tool    string `json:"tool"`
	Summary string `json:"summary"`
	// Job is the run a step started (a Claude Code or Codex delegation):
	// the app opens its thread from the step.
	Job string `json:"job,omitempty"`
	// Detail is what a colleague answered (StepColleague): the app shows it
	// as that colleague's message inside the answer.
	Detail string `json:"detail,omitempty"`
}

// StepColleague is a colleague's answer to a consultation; Summary holds
// the colleague's id.
const (
	StepColleague = "colega_respondeu"
	maxStepDetail = 2000
	// StepImage is Codex drawing an image ("gerando", then "pronta").
	StepImage = "gerar_imagem"
)

// StepText is a step that is what a CLI run said (not a tool it used): it
// shows in the run's thread, not among the agent's steps.
const (
	StepText    = "texto"
	MaxStepText = 2000
)

// validateAgent normalizes an agent spec or explains why it is refused.
func validateAgent(spec *store.AgentSpec) error {
	if spec == nil {
		return errors.New("agent.run requires an agent spec")
	}
	if spec.Engine != "claude" && spec.Engine != "codex" && spec.Engine != "antigravity" {
		return errors.New(`agent engine must be "claude", "codex" or "antigravity"`)
	}
	if !validModel(spec.Model) {
		return errors.New("model must be a plain name like haiku or gpt-5.5-mini")
	}
	switch spec.Effort {
	case "", "low", "medium", "high", "xhigh", "max":
	default:
		return errors.New(`effort must be low, medium, high, xhigh or max`)
	}
	if spec.Search && spec.Engine != "codex" {
		return errors.New("search is a Codex option")
	}
	if spec.Effort != "" && spec.Engine == "codex" {
		spec.Effort = codexEffort(spec.Effort)
	}
	spec.Prompt = strings.TrimSpace(spec.Prompt)
	if spec.Prompt == "" || len(spec.Prompt) > MaxAgentPrompt || !utf8.ValidString(spec.Prompt) {
		return errors.New("prompt must be non-empty UTF-8 up to 32 KB")
	}
	if spec.Mode == "" {
		spec.Mode = "read"
	}
	if spec.Mode != "read" && spec.Mode != "edit" {
		return errors.New(`mode must be "read" or "edit"`)
	}
	if spec.MaxMinutes == 0 {
		spec.MaxMinutes = DefaultAgentMinutes
	}
	if spec.MaxMinutes < 1 || spec.MaxMinutes > MaxAgentMinutes {
		return errors.New("max_minutes must be between 1 and 120")
	}
	if spec.Resume != "" && !validSession(spec.Resume) {
		return errors.New("resume must be a session id")
	}
	if spec.HomeDir != "" {
		if spec.Folder != "" || spec.Workdir != "" {
			return errors.New("home_dir cannot be combined with folder or workdir")
		}
		clean, err := validSpecHomeDir(spec.HomeDir)
		if err != nil {
			return err
		}
		spec.HomeDir = clean
	}
	if spec.Folder != "" {
		if spec.Workdir != "" || len(spec.Folder) > 260 || !windowsPath.MatchString(spec.Folder) || strings.Contains(spec.Folder, `\..`) {
			return errors.New("folder must be an absolute Windows folder, without workdir")
		}
	}
	if spec.Workdir != "" {
		clean, ok := cleanWorkdir(spec.Workdir)
		if !ok {
			return errors.New("workdir must be a relative path inside the node workspace")
		}
		spec.Workdir = clean
	}
	return nil
}

func validModel(m string) bool {
	if len(m) > 64 || strings.HasPrefix(m, "-") {
		return false
	}
	for _, r := range m {
		if !(r >= 'a' && r <= 'z' || r >= 'A' && r <= 'Z' || r >= '0' && r <= '9' || strings.ContainsRune("._:-[]", r)) {
			return false
		}
	}
	return true
}

// cleanWorkdir accepts a relative path made of plain names, so a run can never
// leave the node's workspace.
func cleanWorkdir(dir string) (string, bool) {
	dir = strings.ReplaceAll(dir, "\\", "/")
	if len(dir) > 200 || strings.HasPrefix(dir, "/") || strings.Contains(dir, ":") {
		return "", false
	}
	clean := path.Clean(dir)
	if clean == "." {
		return "", true
	}
	for _, part := range strings.Split(clean, "/") {
		if part == ".." || part == "" || strings.HasPrefix(part, ".") {
			return "", false
		}
	}
	return clean, true
}

func truncate(s string, n int) string {
	if len(s) <= n {
		return s
	}
	for n > 0 && !utf8.RuneStart(s[n]) {
		n--
	}
	return s[:n] + "…"
}

// agentStep records a step of a running agent job in the audit log.
func (s *Server) agentStep(w http.ResponseWriter, r *http.Request, node store.Node) {
	var step AgentStep
	if !readJSON(w, r, &step) {
		return
	}
	step.Tool = strings.TrimSpace(step.Tool)
	if step.Tool == "" {
		http.Error(w, "tool required", http.StatusBadRequest)
		return
	}
	job, err := s.store.GetJob(r.Context(), r.PathValue("id"))
	if err != nil {
		storeError(w, err)
		return
	}
	if job.DeviceID != node.ID {
		storeError(w, store.ErrForbidden)
		return
	}
	if job.Status != store.StatusRunning || job.Action != store.ActionAgentRun && job.Action != store.ActionAgentLogin && job.Action != store.ActionCodexMain {
		storeError(w, store.ErrConflict)
		return
	}
	limit := maxStepField
	if step.Tool == StepText {
		limit = MaxStepText
	}
	if job.Action == store.ActionAgentLogin {
		limit = 2048 // a login link is long; it is not logged
	}
	step.Summary = truncate(step.Summary, limit)
	if job.Action == store.ActionAgentLogin {
		s.hub.publish(job.ID, hubEvent{"step", AgentStep{Tool: truncate(step.Tool, 80), Summary: step.Summary}})
		w.WriteHeader(http.StatusNoContent)
		return
	}
	payload, _ := json.Marshal(map[string]string{"tool": truncate(step.Tool, 80), "summary": step.Summary})
	err = s.store.RecordEvent(r.Context(), store.Event{
		At: s.now(), Type: store.EventAgentStep, Actor: store.NodeActor(node.ID), JobID: job.ID, NodeID: node.ID,
		Payload: payload,
	})
	if err != nil {
		storeError(w, err)
		return
	}
	{ // a waiting turn (delegation) or open chat follows it
		s.hub.publish(job.ID, hubEvent{"step", AgentStep{Tool: truncate(step.Tool, 80), Summary: step.Summary}})
	}
	w.WriteHeader(http.StatusNoContent)
}
