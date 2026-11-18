package control

import (
	"encoding/json"
	"errors"
	"net/http"
	"strings"
	"time"

	"github.com/GuardaChuva22/avora/internal/store"
)

// Runs: the Claude Code and Codex runs an agent delegated (claude_code,
// codex, mission rounds). The app shows a running one as a thread under its
// agent and any one from the step that started it: the task, what the CLI
// said and did, and its answer.

// RunView is a run without its secrets (the bridge key stays out).
type RunView struct {
	ID          string       `json:"id"`
	AgentID     string       `json:"agent_id"`
	Engine      string       `json:"engine"`
	Title       string       `json:"title"`
	Mission     string       `json:"mission,omitempty"`
	Status      store.Status `json:"status"`
	Active      bool         `json:"active"`
	Node        string       `json:"node_id"`
	CreatedAt   time.Time    `json:"created_at"`
	StartedAt   *time.Time   `json:"started_at,omitempty"`
	CompletedAt *time.Time   `json:"completed_at,omitempty"`
	Prompt      string       `json:"prompt,omitempty"`
	Output      string       `json:"output,omitempty"`
	Error       string       `json:"error,omitempty"`
	Steps       []AgentStep  `json:"steps,omitempty"`
}

// runTitle is a run's name: the first line of its task.
func runTitle(task string) string {
	line, _, _ := strings.Cut(strings.TrimSpace(task), "\n")
	return truncate(strings.TrimSpace(line), 100)
}

func isRun(j Job) bool {
	return j.Action == store.ActionAgentRun && j.Agent != nil && j.Agent.Owner != "" &&
		(j.Agent.Engine == "claude" || j.Agent.Engine == "codex" || j.Agent.Engine == "antigravity")
}

func runView(j Job) RunView {
	return RunView{ID: j.ID, AgentID: j.Agent.Owner, Engine: j.Agent.Engine, Title: orDefault(j.Agent.Title, engineLabel(j.Agent.Engine)),
		Mission: j.Agent.Mission, Status: j.Status, Active: !j.Status.Terminal(), Node: j.DeviceID, CreatedAt: j.CreatedAt,
		StartedAt: j.StartedAt, CompletedAt: j.CompletedAt}
}

func (s *Server) registerRuns(mux *http.ServeMux) {
	mux.HandleFunc("GET /v1/runs", s.admin(s.listRuns))
	mux.HandleFunc("GET /v1/runs/{id}", s.admin(s.getRun))
	mux.HandleFunc("POST /v1/runs/{id}/stop", s.admin(s.stopRun))
}

func (s *Server) listRuns(w http.ResponseWriter, r *http.Request) {
	q := r.URL.Query()
	jobs, err := s.store.ListJobs(r.Context(), 200)
	if err != nil {
		storeError(w, err)
		return
	}
	out := []RunView{}
	for _, j := range jobs {
		if !isRun(j) || (q.Get("active") == "1" && j.Status.Terminal()) || (q.Get("agent") != "" && j.Agent.Owner != q.Get("agent")) {
			continue
		}
		out = append(out, runView(j))
	}
	writeJSON(w, http.StatusOK, out)
}

func (s *Server) getRun(w http.ResponseWriter, r *http.Request) {
	j, err := s.store.GetJob(r.Context(), r.PathValue("id"))
	if err != nil {
		storeError(w, err)
		return
	}
	if !isRun(j) {
		storeError(w, store.ErrNotFound)
		return
	}
	v := runView(j)
	v.Prompt, v.Output, v.Error = j.Agent.Prompt, j.Output, j.Error
	events, err := s.store.Events(r.Context(), store.EventFilter{JobID: j.ID, Limit: store.MaxEventLimit})
	if err == nil {
		for _, e := range events {
			var st AgentStep
			if e.Type == store.EventAgentStep && json.Unmarshal(e.Payload, &st) == nil {
				v.Steps = append(v.Steps, st)
			}
		}
	}
	writeJSON(w, http.StatusOK, v)
}

func (s *Server) stopRun(w http.ResponseWriter, r *http.Request) {
	wsID, ok := s.requireWorkspace(w, r)
	if !ok {
		return
	}
	j, err := s.store.GetJob(r.Context(), r.PathValue("id"))
	if err != nil {
		storeError(w, err)
		return
	}
	if !isRun(j) {
		storeError(w, store.ErrNotFound)
		return
	}
	if a, aerr := s.store.GetAgent(r.Context(), j.Agent.Owner); aerr == nil && a.WorkspaceID != "" && a.WorkspaceID != wsID {
		storeError(w, store.ErrNotFound)
		return
	}
	if j.Status.Terminal() {
		writeJSON(w, http.StatusOK, runView(j))
		return
	}
	actor := actorFrom(r)
	job, err := s.store.CancelJob(r.Context(), j.ID, actor, s.now())
	if errors.Is(err, store.ErrConflict) {
		if cur, gerr := s.store.GetJob(r.Context(), j.ID); gerr == nil {
			writeJSON(w, http.StatusOK, runView(cur))
			return
		}
	}
	if err != nil {
		storeError(w, err)
		return
	}
	st := AgentStep{Tool: StepText, Summary: "Execução parada por você."}
	if payload, merr := json.Marshal(st); merr == nil {
		_ = s.store.RecordEvent(r.Context(), store.Event{
			At: s.now(), Type: store.EventAgentStep, Actor: actor, JobID: job.ID, NodeID: job.DeviceID, Payload: payload,
		})
		s.hub.publish(job.ID, hubEvent{"step", st})
	}
	s.finishAgentReply(job, nil)
	writeJSON(w, http.StatusOK, runView(job))
}
