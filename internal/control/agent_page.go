package control

import (
	"net/http"
	"sort"
	"strconv"
	"time"

	"github.com/GuardaChuva22/avora/internal/store"
)

// The agent page (D62): what an agent spent, and archiving it.

func (s *Server) registerAgentPage(mux *http.ServeMux) {
	mux.HandleFunc("GET /v1/agents/{id}/usage", s.admin(s.agentUsage))
	mux.HandleFunc("POST /v1/agents/{id}/archive", s.admin(s.archiveAgent))
	mux.HandleFunc("GET /v1/agents/archived", s.admin(s.archivedAgents))
}

// UsageView is an agent's use over the last days: totals, per day (only
// days with use, oldest first) and per model (most tokens first).
type UsageView struct {
	Days       int              `json:"days"`
	Totals     UsageTotals      `json:"totals"`
	ByDay      []UsageTotalsDay `json:"by_day"`
	ByModel    []UsageModel     `json:"by_model"`
	Runs       int              `json:"runs"`
	RunsFailed int              `json:"runs_failed"`
}

type UsageTotals struct {
	Replies          int     `json:"replies"`
	PromptTokens     int     `json:"prompt_tokens"`
	CompletionTokens int     `json:"completion_tokens"`
	CostUSD          float64 `json:"cost_usd,omitempty"`
}

type UsageTotalsDay struct {
	Day string `json:"day"`
	UsageTotals
}

type UsageModel struct {
	Model string `json:"model"`
	UsageTotals
}

func (t *UsageTotals) add(r store.UsageRow) { t.addWithPrice(r, store.ModelMeta{}) }

func (t *UsageTotals) addWithPrice(r store.UsageRow, p store.ModelMeta) {
	t.Replies += r.Replies
	t.PromptTokens += r.PromptTokens
	t.CompletionTokens += r.CompletionTokens
	if p.InputPrice > 0 || p.OutputPrice > 0 {
		t.CostUSD += (float64(r.PromptTokens)*p.InputPrice + float64(r.CompletionTokens)*p.OutputPrice) / 1_000_000.0
	}
}

func (s *Server) agentUsage(w http.ResponseWriter, r *http.Request) {
	wsID, ok := s.requireWorkspace(w, r)
	if !ok {
		return
	}
	id := r.PathValue("id")
	if a, err := s.store.GetAgent(r.Context(), id); err != nil {
		storeError(w, err)
		return
	} else if a.WorkspaceID != wsID {
		storeError(w, store.ErrNotFound)
		return
	}
	days, _ := strconv.Atoi(r.URL.Query().Get("days"))
	if days <= 0 || days > 365 {
		days = 30
	}
	now := s.now().In(s.zone())
	y, m, d := now.Date()
	since := time.Date(y, m, d, 0, 0, 0, 0, s.zone()).AddDate(0, 0, 1-days)
	rows, err := s.store.AgentUsage(r.Context(), id, since, s.zone())
	if err != nil {
		storeError(w, err)
		return
	}
	ctx := store.WithWorkspace(r.Context(), wsID)
	prices := map[string]store.ModelMeta{}
	if provs, perr := s.store.ListProviders(ctx); perr == nil {
		for _, pr := range provs {
			for m, meta := range pr.ModelMeta {
				if meta.InputPrice > 0 || meta.OutputPrice > 0 {
					prices[m] = meta
				}
			}
		}
	}
	v := UsageView{Days: days, ByDay: []UsageTotalsDay{}, ByModel: []UsageModel{}}
	dayAt, modelAt := map[string]int{}, map[string]int{}
	for _, row := range rows {
		meta := prices[row.Model]
		v.Totals.addWithPrice(row, meta)
		i, ok := dayAt[row.Day]
		if !ok {
			i = len(v.ByDay)
			dayAt[row.Day] = i
			v.ByDay = append(v.ByDay, UsageTotalsDay{Day: row.Day})
		}
		v.ByDay[i].addWithPrice(row, meta)
		model := row.Model
		if model == "" {
			model = "padrão"
		}
		j, ok := modelAt[model]
		if !ok {
			j = len(v.ByModel)
			modelAt[model] = j
			v.ByModel = append(v.ByModel, UsageModel{Model: model})
		}
		v.ByModel[j].addWithPrice(row, meta)
	}
	sortByTokens(v.ByModel)
	if jobs, err := s.store.ListJobs(r.Context(), 200); err == nil {
		for _, j := range jobs {
			if !isRun(j) || j.Agent.Owner != id || j.CreatedAt.Before(since) {
				continue
			}
			v.Runs++
			if j.Status == store.StatusFailed || j.Status == store.StatusTimedOut {
				v.RunsFailed++
			}
		}
	}
	writeJSON(w, http.StatusOK, v)
}

func sortByTokens(list []UsageModel) {
	tokens := func(m UsageModel) int { return m.PromptTokens + m.CompletionTokens }
	sort.SliceStable(list, func(i, j int) bool { return tokens(list[i]) > tokens(list[j]) })
}

// ArchiveRequest archives an agent (true) or brings it back (false).
type ArchiveRequest struct {
	Archived bool `json:"archived"`
}

func (s *Server) archiveAgent(w http.ResponseWriter, r *http.Request) {
	wsID, ok := s.requireWorkspace(w, r)
	if !ok {
		return
	}
	var req ArchiveRequest
	if !readJSON(w, r, &req) {
		return
	}
	cur, err := s.store.GetAgent(r.Context(), r.PathValue("id"))
	if err != nil {
		storeError(w, err)
		return
	}
	if cur.WorkspaceID != wsID {
		storeError(w, store.ErrNotFound)
		return
	}
	a, err := s.store.ArchiveAgent(r.Context(), cur.ID, req.Archived, actorFrom(r), s.now())
	if err != nil {
		storeError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, a)
}

func (s *Server) archivedAgents(w http.ResponseWriter, r *http.Request) {
	wsID, ok := s.requireWorkspace(w, r)
	if !ok {
		return
	}
	ctx := store.WithWorkspace(r.Context(), wsID)
	list, err := s.store.ListArchivedAgents(ctx)
	if err != nil {
		storeError(w, err)
		return
	}
	out := make([]store.Agent, 0, len(list))
	for _, a := range list {
		ws := a.WorkspaceID
		if ws == "" {
			ws = store.DefaultWorkspaceID
		}
		if ws == wsID {
			a.WorkspaceID = ws
			out = append(out, a)
		}
	}
	writeJSON(w, http.StatusOK, out)
}
