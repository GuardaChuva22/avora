package control

import (
	"context"
	"errors"
	"net/http"
	"strings"
	"unicode"

	"github.com/GuardaChuva22/avora/internal/store"
)

// The owner's channels (D54): create, rename, give a purpose, put in a
// project, pick the members and archive. #time keeps everyone and cannot be
// archived.

const (
	maxChannels       = 60
	maxChannelName    = 40
	maxChannelPurpose = 600
)

// ChannelRequest creates or changes a channel; a nil field is left as it is.
type ChannelRequest struct {
	Name      *string                `json:"name"`
	Purpose   *string                `json:"purpose"`
	ProjectID *string                `json:"project_id"`
	Everyone  *bool                  `json:"everyone"`
	Members   *[]store.ChannelMember `json:"members"`
	Position  *int                   `json:"position"`
	Archived  *bool                  `json:"archived"`
}

func (s *Server) registerChannels(mux *http.ServeMux) {
	mux.HandleFunc("GET /v1/channels", s.admin(s.listChannels))
	mux.HandleFunc("POST /v1/channels", s.admin(s.createChannel))
	mux.HandleFunc("PATCH /v1/channels/{id}", s.admin(s.patchChannel))
	mux.HandleFunc("GET /v1/channels/{id}/status", s.admin(s.channelStatus))
}

// channelName is a name as channels write it: lowercase, words joined by
// "-", no "#" ("Lançamento App" → "lançamento-app").
func channelName(raw string) string {
	raw = strings.TrimSpace(strings.TrimPrefix(strings.TrimSpace(raw), "#"))
	var b strings.Builder
	dash := false
	for _, r := range strings.ToLower(raw) {
		switch {
		case unicode.IsLetter(r) || unicode.IsDigit(r):
			b.WriteRune(r)
			dash = false
		case r == '-' || r == '_' || unicode.IsSpace(r):
			if b.Len() > 0 && !dash {
				b.WriteRune('-')
				dash = true
			}
		}
	}
	name := strings.Trim(b.String(), "-")
	if r := []rune(name); len(r) > maxChannelName {
		name = strings.Trim(string(r[:maxChannelName]), "-")
	}
	return name
}

// ChannelView is a channel as the app lists it.
type ChannelView struct {
	store.Channel
	// Agents are the ids of its members now (every agent when Everyone).
	Agents []string `json:"agents"`
	// LastAt and Preview come from its conversation.
	Preview string `json:"preview"`
	LastAt  string `json:"last_at"`
}

func (s *Server) channelViews(ctx context.Context, archived bool) ([]ChannelView, error) {
	list, err := s.store.ListChannels(ctx)
	if err != nil {
		return nil, err
	}
	all, err := s.store.ListAgents(ctx)
	if err != nil {
		return nil, err
	}
	out := []ChannelView{}
	for _, c := range list {
		if (c.ArchivedAt != nil) != archived {
			continue
		}
		v := ChannelView{Channel: c, Agents: []string{}}
		for _, a := range all {
			if _, ok := c.Member(a.ID); ok {
				v.Agents = append(v.Agents, a.ID)
			}
		}
		if conv, err := s.store.GetConversation(ctx, c.ID); err == nil {
			v.Preview, v.LastAt = conv.Preview, conv.UpdatedAt.Format("2006-01-02T15:04:05Z07:00")
		}
		out = append(out, v)
	}
	return out, nil
}

func (s *Server) listChannels(w http.ResponseWriter, r *http.Request) {
	wsID, ok := s.requireWorkspace(w, r)
	if !ok {
		return
	}
	ctx := store.WithWorkspace(r.Context(), wsID)
	out, err := s.channelViews(ctx, r.URL.Query().Get("archived") == "1")
	if err != nil {
		storeError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, out)
}

// applyChannel validates a request onto a channel.
func (s *Server) applyChannel(ctx context.Context, c store.Channel, req ChannelRequest) (store.Channel, error) {
	list, err := s.store.ListChannels(ctx)
	if err != nil {
		return c, err
	}
	if req.Name != nil {
		name := channelName(*req.Name)
		if name == "" {
			return c, errors.New("dê um nome ao canal")
		}
		if c.ID == TeamChannelID && name != c.Name {
			return c, errors.New("o #time não muda de nome")
		}
		for _, o := range list {
			if o.ID != c.ID && o.ArchivedAt == nil && o.Name == name {
				return c, errors.New("já existe um canal #" + name)
			}
		}
		c.Name = name
	}
	if req.Purpose != nil {
		p := strings.TrimSpace(*req.Purpose)
		if len([]rune(p)) > maxChannelPurpose {
			return c, errors.New("o propósito passou de 600 caracteres")
		}
		c.Purpose = p
	}
	if req.ProjectID != nil {
		if *req.ProjectID != "" {
			p, err := s.store.GetProject(ctx, *req.ProjectID)
			if err != nil || (c.WorkspaceID != "" && p.WorkspaceID != c.WorkspaceID) {
				return c, errors.New("esse projeto não existe")
			}
		}
		c.ProjectID = *req.ProjectID
	}
	if req.Everyone != nil {
		if c.ID == TeamChannelID && !*req.Everyone {
			return c, errors.New("o #time tem todo mundo")
		}
		c.Everyone = *req.Everyone
	}
	if req.Members != nil {
		agents, err := s.store.ListAgents(ctx)
		if err != nil {
			return c, err
		}
		known := map[string]bool{}
		for _, a := range agents {
			known[a.ID] = true
		}
		members := []store.ChannelMember{}
		seen := map[string]bool{}
		for _, m := range *req.Members {
			if !known[m.AgentID] {
				return c, errors.New("agente desconhecido: " + m.AgentID)
			}
			if !seen[m.AgentID] {
				seen[m.AgentID] = true
				members = append(members, m)
			}
		}
		c.Members = members
	}
	if !c.Everyone && len(c.Members) == 0 {
		return c, errors.New("ponha pelo menos um agente no canal")
	}
	if req.Position != nil {
		c.Position = *req.Position
	}
	if req.Archived != nil {
		if c.ID == TeamChannelID && *req.Archived {
			return c, errors.New("o #time não pode ser arquivado")
		}
		if *req.Archived && c.ArchivedAt == nil {
			now := s.now()
			c.ArchivedAt = &now
		} else if !*req.Archived {
			c.ArchivedAt = nil
		}
	}
	return c, nil
}

func (s *Server) createChannel(w http.ResponseWriter, r *http.Request) {
	wsID, ok := s.requireWorkspace(w, r)
	if !ok {
		return
	}
	var req ChannelRequest
	if !readJSON(w, r, &req) {
		return
	}
	ctx := store.WithWorkspace(r.Context(), wsID)
	list, err := s.store.ListChannels(ctx)
	if err != nil {
		storeError(w, err)
		return
	}
	if len(list) >= maxChannels {
		http.Error(w, "limite de canais atingido", http.StatusConflict)
		return
	}
	if req.Name == nil {
		http.Error(w, "dê um nome ao canal", http.StatusBadRequest)
		return
	}
	c := store.Channel{ID: "canal-" + newID(), WorkspaceID: wsID}
	for _, o := range list {
		if o.Position >= c.Position {
			c.Position = o.Position + 1
		}
	}
	c, err = s.applyChannel(ctx, c, req)
	if err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}
	saved, err := s.store.SaveChannel(ctx, c, actorFrom(r), s.now())
	if err != nil {
		storeError(w, err)
		return
	}
	writeJSON(w, http.StatusCreated, saved)
}

func (s *Server) patchChannel(w http.ResponseWriter, r *http.Request) {
	wsID, ok := s.requireWorkspace(w, r)
	if !ok {
		return
	}
	var req ChannelRequest
	if !readJSON(w, r, &req) {
		return
	}
	ctx := store.WithWorkspace(r.Context(), wsID)
	c, err := s.store.GetChannel(ctx, r.PathValue("id"))
	if err != nil {
		storeError(w, err)
		return
	}
	if c.WorkspaceID != wsID {
		storeError(w, store.ErrNotFound)
		return
	}
	c, err = s.applyChannel(ctx, c, req)
	if err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}
	if c.ArchivedAt != nil {
		s.stopChannel(ctx, c.ID)
	}
	saved, err := s.store.SaveChannel(ctx, c, actorFrom(r), s.now())
	if err != nil {
		storeError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, saved)
}
