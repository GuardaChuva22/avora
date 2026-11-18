package control

import (
	"context"
	"net/http"
	"strings"

	"github.com/GuardaChuva22/avora/internal/store"
)

// Notice is one actionable strip shown at the top of the workspace.
type Notice struct {
	Kind   string `json:"kind"`
	Text   string `json:"text"`
	Action string `json:"action"`
}

func (s *Server) registerNotices(mux *http.ServeMux) {
	mux.HandleFunc("GET /v1/notices", s.admin(s.listNotices))
}

func joinNoticeNames(names []string) string {
	switch len(names) {
	case 0:
		return ""
	case 1:
		return names[0]
	case 2:
		return names[0] + " e " + names[1]
	default:
		return strings.Join(names[:len(names)-1], ", ") + " e " + names[len(names)-1]
	}
}

func (s *Server) hasConnectedAI(ctx context.Context) bool {
	s.models.mu.Lock()
	hasBuiltin := s.models.builtin != nil
	active := s.models.active
	s.models.mu.Unlock()
	if hasBuiltin {
		return true
	}
	// Tests and embedded callers may wire a ChatEngine directly without a provider id.
	if s.currentEngine() != nil && active == "" {
		return true
	}
	list, err := s.store.ListProviders(ctx)
	if err != nil {
		return s.currentEngine() != nil
	}
	views := make([]ProviderView, 0, len(list))
	for _, p := range list {
		views = append(views, ProviderView{Provider: p, HasKey: len(p.Secret) > 0})
	}
	views = s.codexViews(ctx, views, active)
	views = s.agyViews(ctx, views)
	for _, v := range views {
		if v.ID == agyBrain {
			if v.CodexMain {
				return true
			}
			continue
		}
		if v.Status == "ok" {
			return true
		}
	}
	for _, q := range s.collectQuotas(ctx) {
		if q.Error == "" {
			return true
		}
	}
	return false
}

func (s *Server) listNotices(w http.ResponseWriter, r *http.Request) {
	wsID, ok := s.requireWorkspace(w, r)
	if !ok {
		return
	}
	ctx := store.WithWorkspace(r.Context(), wsID)

	nodes, err := s.store.ListNodes(ctx)
	if err != nil {
		storeError(w, err)
		return
	}
	agents, err := s.store.ListAgents(ctx)
	if err != nil {
		storeError(w, err)
		return
	}

	out := []Notice{}

	if !s.hasConnectedAI(ctx) {
		out = append(out, Notice{
			Kind:   "no_ai",
			Text:   "Nenhuma IA conectada",
			Action: "modelos:connect",
		})
	}

	byHome := map[string][]string{}
	paused := []string{}
	for _, a := range agents {
		name := firstNonEmpty(strings.TrimSpace(a.Name), a.ID)
		if homeID := strings.TrimSpace(a.HomeNodeID); homeID != "" {
			byHome[homeID] = append(byHome[homeID], name)
		}
		if !a.Autonomy.AllowsStartAlone() {
			paused = append(paused, name)
		}
	}

	for _, n := range nodes {
		if n.RevokedAt != nil || s.online(n) {
			continue
		}
		waiting := byHome[n.ID]
		if len(waiting) == 0 {
			continue
		}
		nodeName := firstNonEmpty(strings.TrimSpace(n.Name), n.ID)
		text := "O computador " + nodeName + " está desligado: " + joinNoticeNames(waiting)
		if len(waiting) == 1 {
			text += " espera por ele"
		} else {
			text += " esperam por ele"
		}
		out = append(out, Notice{
			Kind:   "node_offline",
			Text:   text,
			Action: "nos",
		})
	}

	if len(paused) == 1 {
		out = append(out, Notice{
			Kind:   "agents_paused",
			Text:   paused[0] + " está pausado e não começa tarefas sozinho",
			Action: "team",
		})
	} else if len(paused) > 1 {
		out = append(out, Notice{
			Kind:   "agents_paused",
			Text:   joinNoticeNames(paused) + " estão pausados e não começam tarefas sozinhos",
			Action: "team",
		})
	}

	writeJSON(w, http.StatusOK, out)
}
