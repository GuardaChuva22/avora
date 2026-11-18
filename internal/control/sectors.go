package control

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"strings"
	"unicode/utf8"

	"github.com/GuardaChuva22/avora/internal/store"
)

// MaxPinnedAgents bounds how many agents may be pinned at the top of a workspace.
const MaxPinnedAgents = 5

var errTooManyPinned = fmt.Errorf("at most %d agents can be pinned", MaxPinnedAgents)

// SectorRequest creates or updates a sector.
type SectorRequest struct {
	ID          string  `json:"id"`
	Name        *string `json:"name"`
	Color       *string `json:"color"`
	Icon        *string `json:"icon"`
	LeadAgentID *string `json:"lead_agent_id"`
	Position    *int    `json:"position"`
	// Order / IDs allow PATCH /v1/sectors to reorder sectors directly.
	Order []string `json:"order"`
	IDs   []string `json:"ids"`
}

func (s *Server) registerSectors(mux *http.ServeMux) {
	mux.HandleFunc("GET /v1/sectors", s.admin(s.listSectors))
	mux.HandleFunc("POST /v1/sectors", s.admin(s.createSector))
	mux.HandleFunc("PATCH /v1/sectors", s.admin(s.reorderSectors))
	mux.HandleFunc("POST /v1/sectors/reorder", s.admin(s.reorderSectors))
	mux.HandleFunc("PATCH /v1/sectors/reorder", s.admin(s.reorderSectors))
	mux.HandleFunc("GET /v1/sectors/{id}", s.admin(s.getSector))
	mux.HandleFunc("PATCH /v1/sectors/{id}", s.admin(s.patchSector))
	mux.HandleFunc("PUT /v1/sectors/{id}", s.admin(s.patchSector))
	mux.HandleFunc("DELETE /v1/sectors/{id}", s.admin(s.deleteSector))

	mux.HandleFunc("PATCH /v1/agents/{id}", s.admin(s.patchAgent))
}

func (s *Server) listSectors(w http.ResponseWriter, r *http.Request) {
	wsID, ok := s.requireWorkspace(w, r)
	if !ok {
		return
	}
	list, err := s.store.ListSectors(store.WithWorkspace(r.Context(), wsID))
	if err != nil {
		storeError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, list)
}

func (s *Server) getSector(w http.ResponseWriter, r *http.Request) {
	wsID, ok := s.requireWorkspace(w, r)
	if !ok {
		return
	}
	sec, err := s.store.GetSector(r.Context(), r.PathValue("id"))
	if err != nil || sec.WorkspaceID != wsID {
		storeError(w, store.ErrNotFound)
		return
	}
	writeJSON(w, http.StatusOK, sec)
}

func (s *Server) createSector(w http.ResponseWriter, r *http.Request) {
	wsID, ok := s.requireWorkspace(w, r)
	if !ok {
		return
	}
	ctx := store.WithWorkspace(r.Context(), wsID)
	var req SectorRequest
	if !readJSON(w, r, &req) {
		return
	}
	name := ""
	if req.Name != nil {
		name = strings.TrimSpace(*req.Name)
	}
	if name == "" || utf8.RuneCountInString(name) > 60 {
		http.Error(w, "name must have 1 to 60 characters", http.StatusBadRequest)
		return
	}
	id := strings.TrimSpace(req.ID)
	if id == "" {
		id = slug(name)
	}
	if len(id) < 2 {
		id = "setor-" + newID()[:6]
	}
	if !agentIDPattern.MatchString(id) {
		http.Error(w, "id must be 2 to 32 lowercase letters, digits or dashes", http.StatusBadRequest)
		return
	}
	if existing, err := s.store.GetSector(ctx, id); err == nil {
		if req.ID != "" || existing.WorkspaceID == wsID {
			http.Error(w, "a sector with this id already exists", http.StatusConflict)
			return
		}
		base := id
		if len(base) > 27 {
			base = strings.TrimSuffix(base[:27], "-")
		}
		for i := 2; ; i++ {
			cand := fmt.Sprintf("%s-%d", base, i)
			if _, gerr := s.store.GetSector(ctx, cand); errors.Is(gerr, store.ErrNotFound) {
				id = cand
				break
			} else if gerr != nil {
				storeError(w, gerr)
				return
			}
		}
	} else if !errors.Is(err, store.ErrNotFound) {
		storeError(w, err)
		return
	}

	sec := store.Sector{
		ID:          id,
		WorkspaceID: wsID,
		Name:        name,
	}
	if req.Color != nil {
		c := strings.ToLower(strings.TrimSpace(*req.Color))
		if c != "" && !colorPattern.MatchString(c) {
			http.Error(w, "color must look like #7fd1ff", http.StatusBadRequest)
			return
		}
		sec.Color = c
	}
	if req.Icon != nil {
		icon := strings.TrimSpace(*req.Icon)
		if utf8.RuneCountInString(icon) > 40 {
			http.Error(w, "icon must have at most 40 characters", http.StatusBadRequest)
			return
		}
		sec.Icon = icon
	}
	if req.LeadAgentID != nil {
		lead := strings.TrimSpace(*req.LeadAgentID)
		if lead != "" {
			ag, err := s.store.GetAgent(ctx, lead)
			if err != nil || ag.WorkspaceID != wsID {
				http.Error(w, "unknown lead agent", http.StatusBadRequest)
				return
			}
		}
		sec.LeadAgentID = lead
	}
	if req.Position != nil {
		sec.Position = *req.Position
	} else {
		existingList, _ := s.store.ListSectors(ctx)
		for _, other := range existingList {
			if other.Position >= sec.Position {
				sec.Position = other.Position + 1
			}
		}
	}
	saved, err := s.store.SaveSector(ctx, sec, actorFrom(r), s.now())
	if err != nil {
		storeError(w, err)
		return
	}
	writeJSON(w, http.StatusCreated, saved)
}

func (s *Server) patchSector(w http.ResponseWriter, r *http.Request) {
	wsID, ok := s.requireWorkspace(w, r)
	if !ok {
		return
	}
	ctx := store.WithWorkspace(r.Context(), wsID)
	cur, err := s.store.GetSector(ctx, r.PathValue("id"))
	if err != nil || cur.WorkspaceID != wsID {
		storeError(w, store.ErrNotFound)
		return
	}
	var req SectorRequest
	if !readJSON(w, r, &req) {
		return
	}
	if req.Name != nil {
		name := strings.TrimSpace(*req.Name)
		if name == "" || utf8.RuneCountInString(name) > 60 {
			http.Error(w, "name must have 1 to 60 characters", http.StatusBadRequest)
			return
		}
		cur.Name = name
	}
	if req.Color != nil {
		c := strings.ToLower(strings.TrimSpace(*req.Color))
		if c != "" && !colorPattern.MatchString(c) {
			http.Error(w, "color must look like #7fd1ff", http.StatusBadRequest)
			return
		}
		cur.Color = c
	}
	if req.Icon != nil {
		icon := strings.TrimSpace(*req.Icon)
		if utf8.RuneCountInString(icon) > 40 {
			http.Error(w, "icon must have at most 40 characters", http.StatusBadRequest)
			return
		}
		cur.Icon = icon
	}
	if req.LeadAgentID != nil {
		lead := strings.TrimSpace(*req.LeadAgentID)
		if lead != "" {
			ag, err := s.store.GetAgent(ctx, lead)
			if err != nil || ag.WorkspaceID != wsID {
				http.Error(w, "unknown lead agent", http.StatusBadRequest)
				return
			}
		}
		cur.LeadAgentID = lead
	}
	if req.Position != nil {
		if *req.Position < 0 || *req.Position > 1000 {
			http.Error(w, "position must be between 0 and 1000", http.StatusBadRequest)
			return
		}
		cur.Position = *req.Position
	}
	saved, err := s.store.SaveSector(ctx, cur, actorFrom(r), s.now())
	if err != nil {
		storeError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, saved)
}

func (s *Server) deleteSector(w http.ResponseWriter, r *http.Request) {
	wsID, ok := s.requireWorkspace(w, r)
	if !ok {
		return
	}
	cur, err := s.store.GetSector(r.Context(), r.PathValue("id"))
	if err != nil || cur.WorkspaceID != wsID {
		storeError(w, store.ErrNotFound)
		return
	}
	if err := s.store.DeleteSector(r.Context(), cur.ID, actorFrom(r), s.now()); err != nil {
		storeError(w, err)
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

func (s *Server) reorderSectors(w http.ResponseWriter, r *http.Request) {
	wsID, ok := s.requireWorkspace(w, r)
	if !ok {
		return
	}
	var req SectorRequest
	if !readJSON(w, r, &req) {
		return
	}
	ids := req.IDs
	if len(ids) == 0 {
		ids = req.Order
	}
	if len(ids) == 0 {
		http.Error(w, "ids required", http.StatusBadRequest)
		return
	}
	list, err := s.store.ReorderSectors(store.WithWorkspace(r.Context(), wsID), wsID, ids, actorFrom(r), s.now())
	if err != nil {
		storeError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, list)
}

// ── Agent partial update & org validation ────────────────────────

func applyAgentOrgFields(req AgentRequest, a *store.Agent) error {
	if req.Sector != nil {
		a.Sector = strings.TrimSpace(*req.Sector)
	}
	if req.ReportsTo != nil {
		a.ReportsTo = strings.TrimSpace(*req.ReportsTo)
	}
	if req.Pinned != nil {
		a.Pinned = *req.Pinned
	}
	if req.Autonomy != nil {
		if req.Autonomy.MaxParallel != 0 && (req.Autonomy.MaxParallel < 1 || req.Autonomy.MaxParallel > 5) {
			return errors.New("autonomy.max_parallel must be between 1 and 5")
		}
		cur := store.NormalizeAutonomy(a.Autonomy)
		if req.Autonomy.CanStartAlone != nil {
			cur.CanStartAlone = store.Bool(*req.Autonomy.CanStartAlone)
		}
		if req.Autonomy.MaxParallel != 0 {
			cur.MaxParallel = req.Autonomy.MaxParallel
		}
		a.Autonomy = store.NormalizeAutonomy(cur)
	} else {
		a.Autonomy = store.NormalizeAutonomy(a.Autonomy)
	}
	if req.Apps != nil {
		cleaned := []string{}
		seen := map[string]bool{}
		for _, item := range *req.Apps {
			item = strings.TrimSpace(item)
			if item != "" && !seen[item] {
				seen[item] = true
				cleaned = append(cleaned, item)
			}
		}
		if len(cleaned) == 0 {
			a.Apps = nil
		} else {
			a.Apps = cleaned
		}
	}
	return nil
}

// validateAgentOrg checks sector membership, reports_to hierarchy, and max 5 pinned agents.
func (s *Server) validateAgentOrg(ctx context.Context, wsID string, a *store.Agent) (int, error) {
	ctx = store.WithWorkspace(ctx, wsID)
	if a.Sector != "" {
		sec, err := s.store.GetSector(ctx, a.Sector)
		if err != nil || sec.WorkspaceID != wsID {
			return http.StatusBadRequest, errors.New("unknown sector")
		}
	}
	agents, err := s.store.ListAgents(ctx)
	if err != nil {
		return http.StatusInternalServerError, err
	}
	byID := map[string]store.Agent{}
	pinnedCount := 0
	for _, other := range agents {
		byID[other.ID] = other
		if other.ID != a.ID && other.Pinned {
			pinnedCount++
		}
	}
	if a.Pinned && pinnedCount >= MaxPinnedAgents {
		return http.StatusConflict, errTooManyPinned
	}
	if a.ReportsTo != "" {
		if a.ReportsTo == a.ID {
			return http.StatusBadRequest, errors.New("agent cannot report to itself")
		}
		mgr, ok := byID[a.ReportsTo]
		if !ok {
			return http.StatusBadRequest, errors.New("unknown reports_to agent")
		}
		visited := map[string]bool{a.ID: true}
		cur := mgr.ID
		for hops := 0; cur != "" && hops < 64; hops++ {
			if visited[cur] {
				return http.StatusBadRequest, errors.New("reports_to cycle is not allowed")
			}
			visited[cur] = true
			next, ok := byID[cur]
			if !ok {
				break
			}
			cur = next.ReportsTo
		}
	}
	return 0, nil
}

func (s *Server) patchAgent(w http.ResponseWriter, r *http.Request) {
	wsID, ok := s.requireWorkspace(w, r)
	if !ok {
		return
	}
	ctx := store.WithWorkspace(r.Context(), wsID)
	current, err := s.store.GetAgent(ctx, r.PathValue("id"))
	if err != nil || current.WorkspaceID != wsID {
		storeError(w, store.ErrNotFound)
		return
	}
	var raw map[string]json.RawMessage
	if !readJSONMax(w, r, &raw, maxPersona*4+4096) {
		return
	}
	// Blank required fields and nulls mean "unchanged" (a client may send
	// the whole AgentRequest with only a few fields set).
	for k, v := range raw {
		if string(v) == "null" || (string(v) == `""` && (k == "name" || k == "role" || k == "persona" || k == "color")) {
			delete(raw, k)
		}
	}
	var req AgentRequest
	b, _ := json.Marshal(raw)
	if err := json.Unmarshal(b, &req); err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}
	if _, ok := raw["name"]; !ok {
		req.Name = current.Name
	}
	if _, ok := raw["role"]; !ok {
		req.Role = current.Role
	}
	if _, ok := raw["persona"]; !ok {
		req.Persona = current.Persona
	}
	if _, ok := raw["color"]; !ok {
		req.Color = current.Color
	}
	a, err := toAgent(req, current)
	if err == nil && req.Brain != nil {
		a.Brain, err = s.validBrain(ctx, *req.Brain)
	}
	if err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}
	if code, err := s.validateAgentOrg(ctx, wsID, &a); err != nil {
		http.Error(w, err.Error(), code)
		return
	}
	saved, err := s.store.SaveAgent(ctx, a, actorFrom(r), s.now())
	if err != nil {
		storeError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, saved)
}

// orgBlock tells the agent its sector, sector leader, and reporting lines in the workspace.
func (s *Server) orgBlock(ctx context.Context, a store.Agent) string {
	wsID := a.WorkspaceID
	if wsID == "" {
		wsID = "pessoal"
	}
	wctx := store.WithWorkspace(ctx, wsID)
	var parts []string
	if a.Sector != "" {
		if sec, err := s.store.GetSector(wctx, a.Sector); err == nil {
			line := "Setor: " + sec.Name
			if sec.LeadAgentID != "" {
				if sec.LeadAgentID == a.ID {
					line += " (você lidera este setor)"
				} else if lead, err := s.store.GetAgent(wctx, sec.LeadAgentID); err == nil {
					line += " (líder: " + lead.Name + ")"
				}
			}
			parts = append(parts, line)
		}
	}
	if a.ReportsTo != "" {
		if mgr, err := s.store.GetAgent(wctx, a.ReportsTo); err == nil {
			parts = append(parts, "Responde a: "+mgr.Name)
		}
	}
	if agents, err := s.store.ListAgents(wctx); err == nil {
		var directs []string
		for _, other := range agents {
			if other.ReportsTo == a.ID {
				directs = append(directs, other.Name)
			}
		}
		if len(directs) > 0 {
			parts = append(parts, "Respondem a você: "+strings.Join(directs, ", "))
		}
	}
	if len(parts) == 0 {
		return ""
	}
	return "\n\nOrganização no time: " + strings.Join(parts, " · ") + "."
}
