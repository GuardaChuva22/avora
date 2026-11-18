package control

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"sort"
	"strings"
	"time"
	"unicode/utf8"

	"github.com/GuardaChuva22/avora/internal/store"
)

const (
	homeDeliveredWindow = 7 * 24 * time.Hour
	operatorProfileKey  = "owner_profile"
)

type operatorProfile struct {
	Name   string `json:"name"`
	Avatar string `json:"avatar,omitempty"`
	About  string `json:"about,omitempty"`
}

// HomeWaitingItem is one approval, question or mission decision waiting for the person.
type HomeWaitingItem struct {
	ID             string     `json:"id"`
	Kind           string     `json:"kind"` // "approval", "question", "decision"
	AgentID        string     `json:"agent_id"`
	AgentName      string     `json:"agent_name"`
	AgentColor     string     `json:"agent_color,omitempty"`
	ConversationID string     `json:"conversation_id,omitempty"`
	Title          string     `json:"title"`
	Summary        string     `json:"summary"`
	Action         string     `json:"action,omitempty"`
	ExpiresAt      *time.Time `json:"expires_at,omitempty"`
	Options        []string   `json:"options,omitempty"`
	Multiple       bool       `json:"multiple,omitempty"`
	Other          bool       `json:"other,omitempty"`
	MissionID      string     `json:"mission_id,omitempty"`
	CreatedAt      time.Time  `json:"created_at"`
}

// HomeWorkingItem is one execution an agent is running right now.
type HomeWorkingItem struct {
	ID             string    `json:"id"`
	Kind           string    `json:"kind"` // "run", "conversation", "channel", "mission"
	RunID          string    `json:"run_id,omitempty"`
	ConversationID string    `json:"conversation_id,omitempty"`
	Channel        string    `json:"channel,omitempty"`
	MissionID      string    `json:"mission_id,omitempty"`
	AgentID        string    `json:"agent_id"`
	AgentName      string    `json:"agent_name"`
	AgentColor     string    `json:"agent_color,omitempty"`
	Engine         string    `json:"engine,omitempty"`
	Title          string    `json:"title"`
	StartedAt      time.Time `json:"started_at"`
	ElapsedSeconds int       `json:"elapsed_seconds"`
}

// HomeDelivered is one finished run, mission or routine since the person's last visit.
type HomeDelivered struct {
	ID             string    `json:"id"`
	Kind           string    `json:"kind"` // "run", "mission", "routine"
	RunID          string    `json:"run_id,omitempty"`
	ConversationID string    `json:"conversation_id,omitempty"`
	AgentID        string    `json:"agent_id"`
	AgentName      string    `json:"agent_name"`
	AgentColor     string    `json:"agent_color,omitempty"`
	Title          string    `json:"title"`
	Summary        string    `json:"summary,omitempty"`
	CompletedAt    time.Time `json:"completed_at"`
}

// HomeRoutine is one upcoming routine shown on Home.
type HomeRoutine struct {
	ID         string         `json:"id"`
	Title      string         `json:"title"`
	AgentID    string         `json:"agent_id"`
	AgentName  string         `json:"agent_name"`
	AgentColor string         `json:"agent_color,omitempty"`
	Schedule   store.Schedule `json:"schedule"`
	Trigger    *store.Trigger `json:"trigger,omitempty"`
	When       string         `json:"when"`
	NextRun    *time.Time     `json:"next_run,omitempty"`
}

// HomeResponse is GET /v1/home.
type HomeResponse struct {
	GreetingName string            `json:"greeting_name"`
	Waiting      []HomeWaitingItem `json:"waiting"`
	Working      []HomeWorkingItem `json:"working"`
	Delivered    []HomeDelivered   `json:"delivered"`
	NextRoutines []HomeRoutine     `json:"next_routines"`
	LastAgent    string            `json:"last_agent"`
	Since        time.Time         `json:"since"`
}

func (s *Server) registerHome(mux *http.ServeMux) {
	mux.HandleFunc("GET /v1/home", s.admin(s.getHome))
	mux.HandleFunc("GET /v1/waiting", s.admin(s.listWaiting))
	mux.HandleFunc("GET /v1/tasks", s.admin(s.listTasks))
}

func (s *Server) patchOperatorProfile(w http.ResponseWriter, r *http.Request, req PatchMeRequest) {
	ctx := r.Context()
	var prof operatorProfile
	if raw, err := s.store.GetSetting(ctx, operatorProfileKey); err == nil {
		_ = json.Unmarshal(raw, &prof)
	}
	if req.Name != nil {
		name := strings.TrimSpace(*req.Name)
		if name == "" || utf8.RuneCountInString(name) > 80 {
			http.Error(w, "o nome tem de 1 a 80 caracteres", http.StatusBadRequest)
			return
		}
		prof.Name = name
	}
	if req.Avatar != nil {
		av := strings.TrimSpace(*req.Avatar)
		if len(av) > 400*1024 {
			http.Error(w, "a foto é grande demais", http.StatusBadRequest)
			return
		}
		prof.Avatar = av
	}
	if req.About != nil {
		about := strings.TrimSpace(*req.About)
		if utf8.RuneCountInString(about) > 2000 {
			http.Error(w, "o texto sobre você tem até 2000 caracteres", http.StatusBadRequest)
			return
		}
		prof.About = about
	}
	raw, _ := json.Marshal(prof)
	if err := s.store.SetSetting(ctx, operatorProfileKey, raw, store.ActorAdmin, s.now()); err != nil {
		storeError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, store.Person{
		Name:      prof.Name,
		Avatar:    prof.Avatar,
		About:     prof.About,
		Presence:  store.PresenceAuto,
		CreatedAt: s.now(),
	})
}

// ownerBlock injects the owner's profile ("Sobre você") into every agent's instructions.
func (s *Server) ownerBlock(ctx context.Context, a store.Agent) string {
	wsID := a.WorkspaceID
	if wsID == "" {
		wsID = store.DefaultWorkspaceID
	}
	var name, about string
	if ws, err := s.store.GetWorkspace(ctx, wsID); err == nil && ws.OwnerID != "" {
		if owner, err := s.store.GetPerson(ctx, ws.OwnerID); err == nil {
			name, about = strings.TrimSpace(owner.Name), strings.TrimSpace(owner.About)
		}
	}
	if name == "" && about == "" {
		if p := personFrom(ctx); p != nil {
			name, about = strings.TrimSpace(p.Name), strings.TrimSpace(p.About)
		}
	}
	if name == "" && about == "" {
		if raw, err := s.store.GetSetting(ctx, operatorProfileKey); err == nil {
			var prof operatorProfile
			if json.Unmarshal(raw, &prof) == nil {
				name, about = strings.TrimSpace(prof.Name), strings.TrimSpace(prof.About)
			}
		}
	}
	if about == "" && name == "" {
		return ""
	}
	var b strings.Builder
	b.WriteString("\n\nSobre quem trabalha com você:")
	if name != "" {
		b.WriteString("\n- Nome: " + name)
	}
	if about != "" {
		b.WriteString("\n- Do que cuida e contexto: " + about)
	}
	return b.String()
}

func (s *Server) greetingNameOf(ctx context.Context, wsID string) string {
	if p := personFrom(ctx); p != nil && strings.TrimSpace(p.Name) != "" {
		return firstWord(p.Name)
	}
	if ws, err := s.store.GetWorkspace(ctx, wsID); err == nil && ws.OwnerID != "" {
		if owner, err := s.store.GetPerson(ctx, ws.OwnerID); err == nil && strings.TrimSpace(owner.Name) != "" {
			return firstWord(owner.Name)
		}
	}
	if raw, err := s.store.GetSetting(ctx, operatorProfileKey); err == nil {
		var prof operatorProfile
		if json.Unmarshal(raw, &prof) == nil && strings.TrimSpace(prof.Name) != "" {
			return firstWord(prof.Name)
		}
	}
	return ""
}

func firstWord(name string) string {
	fields := strings.Fields(strings.TrimSpace(name))
	if len(fields) == 0 {
		return ""
	}
	return fields[0]
}

func oneLine(s string, max int) string {
	clean := strings.Join(strings.Fields(strings.TrimSpace(s)), " ")
	return truncate(clean, max)
}

func (s *Server) readAndBumpLastHomeAt(ctx context.Context, wsID string, now time.Time, peek bool) *time.Time {
	if p := personFrom(ctx); p != nil && p.ID != "" {
		if cur, err := s.store.GetPerson(ctx, p.ID); err == nil {
			prev := cur.LastHomeAt
			if !peek {
				at := now.UTC()
				cur.LastHomeAt = &at
				_, _ = s.store.UpdatePerson(ctx, cur)
			}
			return prev
		}
	}
	key := "last_home_at:" + wsID
	var prev *time.Time
	if raw, err := s.store.GetSetting(ctx, key); err == nil {
		var t time.Time
		if json.Unmarshal(raw, &t) == nil && !t.IsZero() {
			prev = &t
		}
	}
	if !peek {
		if b, err := json.Marshal(now.UTC()); err == nil {
			_ = s.store.SetSetting(ctx, key, b, store.ActorAdmin, now)
		}
	}
	return prev
}

func (s *Server) workingChannelsOf(ctx context.Context, agentByID map[string]store.Agent) map[string]struct {
	chID   string
	chName string
	since  time.Time
} {
	out := map[string]struct {
		chID   string
		chName string
		since  time.Time
	}{}
	channels, _ := s.store.ListChannels(ctx)
	chNames := map[string]string{TeamChannelID: "time"}
	chInWS := map[string]bool{}
	for _, c := range channels {
		chNames[c.ID] = c.Name
		chInWS[c.ID] = true
	}
	s.channel.mu.Lock()
	defer s.channel.mu.Unlock()
	for chID, runs := range s.channel.runs {
		if len(chInWS) > 0 && !chInWS[chID] {
			continue
		}
		for agentID, run := range runs {
			if run == nil {
				continue
			}
			if _, ok := agentByID[agentID]; !ok {
				continue
			}
			name := chNames[chID]
			if name == "" {
				name = chID
			}
			out[agentID] = struct {
				chID   string
				chName string
				since  time.Time
			}{chID: chID, chName: name, since: run.Since}
		}
	}
	return out
}

func (s *Server) collectWaiting(ctx context.Context, agentByID map[string]store.Agent, convs []store.Conversation) []HomeWaitingItem {
	items := []HomeWaitingItem{}
	now := s.now()

	if approvals, err := s.store.ListApprovals(ctx, "pending", 100); err == nil {
		for _, ap := range approvals {
			a, ok := agentByID[ap.AgentID]
			if !ok {
				continue
			}
			if !ap.ExpiresAt.IsZero() && !now.Before(ap.ExpiresAt) {
				continue
			}
			exp := ap.ExpiresAt
			items = append(items, HomeWaitingItem{
				ID:             ap.ID,
				Kind:           "approval",
				AgentID:        a.ID,
				AgentName:      a.Name,
				AgentColor:     a.Color,
				ConversationID: ap.ConversationID,
				Title:          a.Name + " precisa da sua permissão",
				Summary:        ap.Summary,
				Action:         ap.Action,
				ExpiresAt:      &exp,
				CreatedAt:      ap.CreatedAt,
			})
		}
	}

	checkedPerAgent := map[string]int{}
	for _, c := range convs {
		if c.Engine == "team" || c.ArchivedAt != nil || c.LastRole != "assistant" {
			continue
		}
		a, ok := agentByID[c.AgentID]
		if !ok || checkedPerAgent[a.ID] >= 3 {
			continue
		}
		checkedPerAgent[a.ID]++
		msgs, err := s.store.Messages(ctx, c.ID)
		if err != nil || len(msgs) == 0 {
			continue
		}
		last := msgs[len(msgs)-1]
		if last.Role != "assistant" || len(last.Steps) == 0 {
			continue
		}
		var steps []AgentStep
		if json.Unmarshal(last.Steps, &steps) != nil {
			continue
		}
		for i := len(steps) - 1; i >= 0; i-- {
			st := steps[i]
			if st.Tool != StepAsk {
				continue
			}
			var d askDetail
			_ = json.Unmarshal([]byte(st.Detail), &d)
			summary := strings.Join(d.Options, " · ")
			items = append(items, HomeWaitingItem{
				ID:             fmt.Sprintf("ask:%s:%d", c.ID, last.ID),
				Kind:           "question",
				AgentID:        a.ID,
				AgentName:      a.Name,
				AgentColor:     a.Color,
				ConversationID: c.ID,
				Title:          st.Summary,
				Summary:        summary,
				Options:        d.Options,
				Multiple:       d.Multiple,
				Other:          d.Other,
				CreatedAt:      last.CreatedAt,
			})
			break
		}
	}

	if missions, err := s.store.ListMissions(ctx, store.MissionFilter{ActiveOnly: true}); err == nil {
		for _, m := range missions {
			if m.Status != store.MissionWaiting {
				continue
			}
			a, ok := agentByID[m.AgentID]
			if !ok {
				continue
			}
			items = append(items, HomeWaitingItem{
				ID:             "mission:" + m.ID,
				Kind:           "decision",
				AgentID:        a.ID,
				AgentName:      a.Name,
				AgentColor:     a.Color,
				ConversationID: m.ConversationID,
				MissionID:      m.ID,
				Title:          "Missão «" + m.Title + "»",
				Summary:        firstNonEmpty(m.Summary, m.Objective),
				CreatedAt:      m.UpdatedAt,
			})
		}
	}

	sort.SliceStable(items, func(i, j int) bool {
		return items[i].CreatedAt.After(items[j].CreatedAt)
	})
	return items
}

func (s *Server) collectWorking(ctx context.Context, agentByID map[string]store.Agent, convs []store.Conversation, waitingAgents map[string]bool) []HomeWorkingItem {
	items := []HomeWorkingItem{}
	now := s.now()
	agentHasRun := map[string]bool{}
	missionByJob := map[string]store.Mission{}

	missions, _ := s.store.ListMissions(ctx, store.MissionFilter{ActiveOnly: true})
	for _, m := range missions {
		if m.JobID != "" {
			missionByJob[m.JobID] = m
		}
	}

	if jobs, err := s.store.ListJobs(ctx, 200); err == nil {
		for _, j := range jobs {
			if !isRun(j) || j.Status.Terminal() {
				continue
			}
			a, ok := agentByID[j.Agent.Owner]
			if !ok {
				continue
			}
			agentHasRun[a.ID] = true
			started := j.CreatedAt
			if j.StartedAt != nil {
				started = *j.StartedAt
			}
			elapsed := int(now.Sub(started).Seconds())
			if elapsed < 0 {
				elapsed = 0
			}
			rv := runView(j)
			item := HomeWorkingItem{
				ID:             "run:" + j.ID,
				Kind:           "run",
				RunID:          j.ID,
				AgentID:        a.ID,
				AgentName:      a.Name,
				AgentColor:     a.Color,
				Engine:         j.Agent.Engine,
				Title:          rv.Title,
				StartedAt:      started,
				ElapsedSeconds: elapsed,
			}
			if m, hasM := missionByJob[j.ID]; hasM {
				item.MissionID = m.ID
				item.ConversationID = m.ConversationID
			}
			items = append(items, item)
		}
	}

	for _, m := range missions {
		if (m.Status != store.MissionRunning && m.Status != store.MissionReview) || m.JobID != "" {
			continue
		}
		a, ok := agentByID[m.AgentID]
		if !ok {
			continue
		}
		elapsed := int(now.Sub(m.UpdatedAt).Seconds())
		if elapsed < 0 {
			elapsed = 0
		}
		items = append(items, HomeWorkingItem{
			ID:             "mission:" + m.ID,
			Kind:           "mission",
			MissionID:      m.ID,
			ConversationID: m.ConversationID,
			AgentID:        a.ID,
			AgentName:      a.Name,
			AgentColor:     a.Color,
			Engine:         m.Spec.Engine,
			Title:          "Missão «" + m.Title + "»",
			StartedAt:      m.UpdatedAt,
			ElapsedSeconds: elapsed,
		})
	}

	chRuns := s.workingChannelsOf(ctx, agentByID)
	for agentID, cr := range chRuns {
		a := agentByID[agentID]
		elapsed := int(now.Sub(cr.since).Seconds())
		if elapsed < 0 {
			elapsed = 0
		}
		items = append(items, HomeWorkingItem{
			ID:             "channel:" + cr.chID + ":" + a.ID,
			Kind:           "channel",
			ConversationID: cr.chID,
			Channel:        "#" + cr.chName,
			AgentID:        a.ID,
			AgentName:      a.Name,
			AgentColor:     a.Color,
			Engine:         "native",
			Title:          "Respondendo em #" + cr.chName,
			StartedAt:      cr.since,
			ElapsedSeconds: elapsed,
		})
	}

	for _, c := range convs {
		if c.Engine == "team" || c.JobID == "" {
			continue
		}
		a, ok := agentByID[c.AgentID]
		if !ok || agentHasRun[a.ID] || waitingAgents[a.ID] {
			continue
		}
		started := c.UpdatedAt
		if started.IsZero() {
			started = c.CreatedAt
		}
		elapsed := int(now.Sub(started).Seconds())
		if elapsed < 0 {
			elapsed = 0
		}
		title := strings.TrimSpace(c.Title)
		if title == "" {
			title = oneLine(c.Preview, 80)
		}
		if title == "" {
			title = "Respondendo na conversa"
		}
		items = append(items, HomeWorkingItem{
			ID:             "conv:" + c.ID,
			Kind:           "conversation",
			ConversationID: c.ID,
			AgentID:        a.ID,
			AgentName:      a.Name,
			AgentColor:     a.Color,
			Engine:         "native",
			Title:          title,
			StartedAt:      started,
			ElapsedSeconds: elapsed,
		})
	}

	sort.SliceStable(items, func(i, j int) bool {
		return items[i].StartedAt.Before(items[j].StartedAt)
	})
	return items
}

func (s *Server) collectDelivered(ctx context.Context, agentByID map[string]store.Agent, since time.Time) []HomeDelivered {
	items := []HomeDelivered{}

	if jobs, err := s.store.ListJobs(ctx, 200); err == nil {
		for _, j := range jobs {
			if !isRun(j) || j.Status != store.StatusSucceeded || j.CompletedAt == nil || !j.CompletedAt.After(since) {
				continue
			}
			a, ok := agentByID[j.Agent.Owner]
			if !ok {
				continue
			}
			rv := runView(j)
			items = append(items, HomeDelivered{
				ID:          "run:" + j.ID,
				Kind:        "run",
				RunID:       j.ID,
				AgentID:     a.ID,
				AgentName:   a.Name,
				AgentColor:  a.Color,
				Title:       rv.Title,
				Summary:     oneLine(j.Output, 140),
				CompletedAt: *j.CompletedAt,
			})
		}
	}

	if missions, err := s.store.ListMissions(ctx, store.MissionFilter{}); err == nil {
		for _, m := range missions {
			if m.Status != store.MissionDone || m.FinishedAt == nil || !m.FinishedAt.After(since) {
				continue
			}
			a, ok := agentByID[m.AgentID]
			if !ok {
				continue
			}
			items = append(items, HomeDelivered{
				ID:             "mission:" + m.ID,
				Kind:           "mission",
				ConversationID: m.ConversationID,
				AgentID:        a.ID,
				AgentName:      a.Name,
				AgentColor:     a.Color,
				Title:          m.Title,
				Summary:        oneLine(m.Summary, 140),
				CompletedAt:    *m.FinishedAt,
			})
		}
	}

	if routines, err := s.store.ListRoutines(ctx, ""); err == nil {
		for _, rt := range routines {
			if rt.LastStatus != "ok" || rt.LastRun == nil || !rt.LastRun.After(since) {
				continue
			}
			a, ok := agentByID[rt.AgentID]
			if !ok {
				continue
			}
			convID := rt.ConversationID
			if rt.Deliver != "conversation" && convID == "" {
				convID = TeamChannelID
			}
			items = append(items, HomeDelivered{
				ID:             "routine:" + rt.ID,
				Kind:           "routine",
				ConversationID: convID,
				AgentID:        a.ID,
				AgentName:      a.Name,
				AgentColor:     a.Color,
				Title:          rt.Title,
				Summary:        oneLine(rt.LastPreview, 140),
				CompletedAt:    *rt.LastRun,
			})
		}
	}

	sort.SliceStable(items, func(i, j int) bool {
		return items[i].CompletedAt.After(items[j].CompletedAt)
	})
	if len(items) > 20 {
		items = items[:20]
	}
	return items
}

func (s *Server) collectNextRoutines(ctx context.Context, agentByID map[string]store.Agent) []HomeRoutine {
	routines, err := s.store.ListRoutines(ctx, "")
	if err != nil {
		return []HomeRoutine{}
	}
	timed := []store.Routine{}
	triggered := []store.Routine{}
	for _, rt := range routines {
		if !rt.Enabled {
			continue
		}
		if _, ok := agentByID[rt.AgentID]; !ok {
			continue
		}
		if rt.NextRun != nil {
			timed = append(timed, rt)
		} else if rt.Trigger != nil {
			triggered = append(triggered, rt)
		}
	}
	sort.SliceStable(timed, func(i, j int) bool {
		return timed[i].NextRun.Before(*timed[j].NextRun)
	})
	ordered := append(timed, triggered...)
	if len(ordered) > 3 {
		ordered = ordered[:3]
	}
	out := make([]HomeRoutine, 0, len(ordered))
	for _, rt := range ordered {
		a := agentByID[rt.AgentID]
		out = append(out, HomeRoutine{
			ID:         rt.ID,
			Title:      rt.Title,
			AgentID:    a.ID,
			AgentName:  a.Name,
			AgentColor: a.Color,
			Schedule:   rt.Schedule,
			Trigger:    rt.Trigger,
			When:       whenOf(rt),
			NextRun:    rt.NextRun,
		})
	}
	return out
}

func (s *Server) getHome(w http.ResponseWriter, r *http.Request) {
	wsID, ok := s.requireWorkspace(w, r)
	if !ok {
		return
	}
	ctx := store.WithWorkspace(r.Context(), wsID)
	agents, err := s.store.ListAgents(ctx)
	if err != nil {
		storeError(w, err)
		return
	}
	convs, err := s.store.ListConversations(ctx, "", 1000)
	if err != nil {
		storeError(w, err)
		return
	}
	agentByID := make(map[string]store.Agent, len(agents))
	for _, a := range agents {
		agentByID[a.ID] = a
	}

	now := s.now()
	q := r.URL.Query()
	peek := q.Get("peek") == "1"
	prev := s.readAndBumpLastHomeAt(ctx, wsID, now, peek)

	since := now.Add(-homeDeliveredWindow)
	if rawSince := strings.TrimSpace(q.Get("since")); rawSince != "" {
		if parsed, perr := time.Parse(time.RFC3339Nano, rawSince); perr == nil && parsed.After(since) {
			since = parsed
		}
	} else if prev != nil && prev.After(since) {
		since = *prev
	}

	waiting := s.collectWaiting(ctx, agentByID, convs)
	waitingAgents := map[string]bool{}
	for _, wItem := range waiting {
		if wItem.Kind == "approval" {
			waitingAgents[wItem.AgentID] = true
		}
	}
	working := s.collectWorking(ctx, agentByID, convs, waitingAgents)
	delivered := s.collectDelivered(ctx, agentByID, since)
	nextRoutines := s.collectNextRoutines(ctx, agentByID)

	lastAgent := ""
	for _, c := range convs {
		if c.Engine == "team" || c.ArchivedAt != nil {
			continue
		}
		if _, ok := agentByID[c.AgentID]; ok {
			lastAgent = c.AgentID
			break
		}
	}
	if lastAgent == "" && len(agents) > 0 {
		lastAgent = agents[0].ID
	}

	writeJSON(w, http.StatusOK, HomeResponse{
		GreetingName: s.greetingNameOf(ctx, wsID),
		Waiting:      waiting,
		Working:      working,
		Delivered:    delivered,
		NextRoutines: nextRoutines,
		LastAgent:    lastAgent,
		Since:        since.UTC(),
	})
}

func (s *Server) listWaiting(w http.ResponseWriter, r *http.Request) {
	wsID, ok := s.requireWorkspace(w, r)
	if !ok {
		return
	}
	ctx := store.WithWorkspace(r.Context(), wsID)
	agents, err := s.store.ListAgents(ctx)
	if err != nil {
		storeError(w, err)
		return
	}
	convs, err := s.store.ListConversations(ctx, "", 1000)
	if err != nil {
		storeError(w, err)
		return
	}
	agentByID := make(map[string]store.Agent, len(agents))
	for _, a := range agents {
		agentByID[a.ID] = a
	}
	writeJSON(w, http.StatusOK, s.collectWaiting(ctx, agentByID, convs))
}

func (s *Server) listTasks(w http.ResponseWriter, r *http.Request) {
	wsID, ok := s.requireWorkspace(w, r)
	if !ok {
		return
	}
	ctx := store.WithWorkspace(r.Context(), wsID)
	agents, err := s.store.ListAgents(ctx)
	if err != nil {
		storeError(w, err)
		return
	}
	convs, err := s.store.ListConversations(ctx, "", 1000)
	if err != nil {
		storeError(w, err)
		return
	}
	agentByID := make(map[string]store.Agent, len(agents))
	for _, a := range agents {
		agentByID[a.ID] = a
	}
	waiting := s.collectWaiting(ctx, agentByID, convs)
	waitingAgents := map[string]bool{}
	for _, wItem := range waiting {
		if wItem.Kind == "approval" {
			waitingAgents[wItem.AgentID] = true
		}
	}
	writeJSON(w, http.StatusOK, s.collectWorking(ctx, agentByID, convs, waitingAgents))
}

// fillAgentStatuses populates Status ("working", "waiting", "free") and
// StatusChannel ("#canal") on GET /v1/agents items.
func (s *Server) fillAgentStatuses(ctx context.Context, out []AgentStatus, convs []store.Conversation) {
	agentByID := make(map[string]store.Agent, len(out))
	index := make(map[string]int, len(out))
	for i := range out {
		agentByID[out[i].ID] = out[i].Agent
		index[out[i].ID] = i
	}

	chRuns := s.workingChannelsOf(ctx, agentByID)
	for agentID, cr := range chRuns {
		if i, ok := index[agentID]; ok {
			out[i].Working = true
			out[i].StatusChannel = "#" + cr.chName
		}
	}

	if jobs, err := s.store.ListJobs(ctx, 200); err == nil {
		for _, j := range jobs {
			if !isRun(j) || j.Status.Terminal() {
				continue
			}
			if i, ok := index[j.Agent.Owner]; ok {
				out[i].Working = true
			}
		}
	}

	if missions, err := s.store.ListMissions(ctx, store.MissionFilter{ActiveOnly: true}); err == nil {
		for _, m := range missions {
			if m.Status == store.MissionRunning || m.Status == store.MissionReview {
				if i, ok := index[m.AgentID]; ok {
					out[i].Working = true
				}
			}
		}
	}

	waiting := map[string]bool{}
	for _, item := range s.collectWaiting(ctx, agentByID, convs) {
		waiting[item.AgentID] = true
	}

	for i := range out {
		switch {
		case waiting[out[i].ID]:
			out[i].Status = "waiting"
		case out[i].Working:
			out[i].Status = "working"
		default:
			out[i].Status = "free"
		}
	}
}

// collectQuotas gathers the current subscription quota readings across online nodes.
func (s *Server) collectQuotas(ctx context.Context) []QuotaStatus {
	nodes, err := s.store.ListNodes(ctx)
	if err != nil {
		return nil
	}
	providers, err := s.store.ListProviders(ctx)
	if err != nil {
		return nil
	}
	accounts := map[string]string{}
	agyOn := map[string]string{}
	for _, p := range providers {
		if p.Kind == "antigravity" && isAgyPCAccount(p) {
			agyOn[p.NodeID] = p.ID
		}
		if p.Kind == "claude" || p.Kind == "codex" || p.Kind == "antigravity" {
			accounts[p.ID] = p.Name
		}
	}
	best := map[string]QuotaStatus{}
	for _, n := range nodes {
		if !s.online(n) {
			continue
		}
		for _, q := range s.quotaBook.get(n.ID) {
			key := q.Engine
			if q.Engine == "antigravity" && q.Account == "" && agyOn[n.ID] != "" {
				q.Account = agyOn[n.ID]
			}
			if q.Account != "" {
				name, ok := accounts[q.Account]
				if !ok {
					continue
				}
				key = q.Account
				q.Name = name
				if q.Engine == "antigravity" && (name == "" || name == "Principal" || name == "Antigravity") {
					q.Name = "Antigravity"
				}
			}
			if cur, ok := best[key]; !ok || better(q, cur.Quota) {
				best[key] = QuotaStatus{Quota: q, Node: n.Name}
			}
		}
	}
	out := make([]QuotaStatus, 0, len(best))
	for _, q := range best {
		out = append(out, q)
	}
	return out
}

func formatQuotaWindow(label string, w *QuotaWindow, loc *time.Location) string {
	if w == nil {
		return ""
	}
	left := 100 - w.UsedPercent
	if left < 0 {
		left = 0
	}
	line := fmt.Sprintf("%s: %.0f%% usado (%.0f%% disponível)", label, w.UsedPercent, left)
	if w.ResetsAt > 0 {
		reset := time.Unix(w.ResetsAt, 0).In(loc)
		line += " · renova em " + reset.Format("02/01 às 15:04")
	}
	return line
}

// toolGetMyUsage answers how much the agent spent and how the quota of its account stands.
func (s *Server) toolGetMyUsage(ctx context.Context, t *turn) (string, error) {
	loc := s.zone()
	now := s.now().In(loc)
	y, m, d := now.Date()
	todayStr := now.Format("2006-01-02")
	since := time.Date(y, m, d, 0, 0, 0, 0, loc).AddDate(0, 0, -29)

	rows, err := s.store.AgentUsage(ctx, t.agent.ID, since, loc)
	if err != nil && !errors.Is(err, store.ErrNotFound) {
		return "", err
	}

	var monthTotals, todayTotals UsageTotals
	for _, r := range rows {
		monthTotals.add(r)
		if r.Day == todayStr {
			todayTotals.add(r)
		}
	}

	runs30, runsToday := 0, 0
	todayStart := time.Date(y, m, d, 0, 0, 0, 0, loc)
	if jobs, jerr := s.store.ListJobs(ctx, 200); jerr == nil {
		for _, j := range jobs {
			if !isRun(j) || j.Agent.Owner != t.agent.ID || j.CreatedAt.Before(since) {
				continue
			}
			runs30++
			if !j.CreatedAt.Before(todayStart) {
				runsToday++
			}
		}
	}

	brain := t.agent.Brain
	brainLabel := "Automático"
	matchEngine, matchAccount := "", ""
	if brain.Provider != "" {
		brainLabel = s.brainName(ctx, brain)
		if brain.Model != "" {
			brainLabel += " (" + brain.Model + ")"
		}
		switch {
		case brain.Provider == claudeBrain:
			matchEngine = "claude"
		case strings.HasPrefix(brain.Provider, "claude."):
			matchEngine, matchAccount = "claude", brain.Provider
		case strings.HasPrefix(brain.Provider, codexPrincipalPrefix):
			matchEngine = "codex"
		case strings.HasPrefix(brain.Provider, "codex."):
			matchEngine, matchAccount = "codex", brain.Provider
		case brain.Provider == agyBrain:
			matchEngine = "antigravity"
		case strings.HasPrefix(brain.Provider, "antigravity."):
			matchEngine, matchAccount = "antigravity", brain.Provider
		}
	} else if eng := s.chatEngine(); eng != nil {
		brainLabel = "Automático · " + eng.Name()
		if mdl := eng.Model(); mdl != "" {
			brainLabel += " (" + mdl + ")"
		}
		switch te := eng.(type) {
		case *claudeEngine:
			matchEngine = "claude"
			if strings.HasPrefix(te.provider, "claude.") {
				matchAccount = te.provider
			}
		case *codexEngine:
			matchEngine = "codex"
			if strings.HasPrefix(te.provider, "codex.") {
				matchAccount = te.provider
			}
		case *agyEngine:
			matchEngine = "antigravity"
			if strings.HasPrefix(te.provider, "antigravity.") {
				matchAccount = te.provider
			}
		}
	}

	quotas := s.collectQuotas(ctx)
	matched := []QuotaStatus{}
	for _, q := range quotas {
		if matchAccount != "" {
			if q.Account == matchAccount {
				matched = append(matched, q)
			}
			continue
		}
		if matchEngine != "" {
			if q.Engine == matchEngine && (matchEngine == "antigravity" || q.Account == "") {
				matched = append(matched, q)
			}
		}
	}

	var b strings.Builder
	fmt.Fprintf(&b, "Uso de %s (%s):\n", t.agent.Name, brainLabel)
	fmt.Fprintf(&b, "- Hoje: %d respostas, %d tokens (%d entrada + %d saída), %d execuções delegadas.\n",
		todayTotals.Replies, todayTotals.PromptTokens+todayTotals.CompletionTokens,
		todayTotals.PromptTokens, todayTotals.CompletionTokens, runsToday)
	fmt.Fprintf(&b, "- Últimos 30 dias: %d respostas, %d tokens (%d entrada + %d saída), %d execuções delegadas.\n",
		monthTotals.Replies, monthTotals.PromptTokens+monthTotals.CompletionTokens,
		monthTotals.PromptTokens, monthTotals.CompletionTokens, runs30)

	b.WriteString("\nCota da conta usada:\n")
	switch {
	case len(matched) > 0:
		for _, q := range matched {
			accName := firstNonEmpty(q.Name, engineLabel(q.Engine))
			if q.Plan != "" {
				accName += " (" + strings.ToUpper(q.Plan) + ")"
			}
			status := "disponível"
			if q.Limited {
				status = "no limite (esgotada agora)"
			} else if q.Error != "" {
				status = "sem leitura (" + q.Error + ")"
			}
			fmt.Fprintf(&b, "- %s [%s]: %s", accName, q.Node, status)
			parts := []string{}
			if s5 := formatQuotaWindow("janela 5h", q.FiveHour, loc); s5 != "" {
				parts = append(parts, s5)
			}
			if sw := formatQuotaWindow("semana", q.Weekly, loc); sw != "" {
				parts = append(parts, sw)
			}
			if len(parts) > 0 {
				b.WriteString(" — " + strings.Join(parts, "; "))
			}
			b.WriteByte('\n')
		}
	case matchEngine != "":
		fmt.Fprintf(&b, "- Conta de %s sem leitura de cota recente (verifique se o computador com a conta está online).\n", engineLabel(matchEngine))
	default:
		b.WriteString("- Conta por chave de API (não tem limite de assinatura de 5h/semanal medido pelo computador).\n")
	}

	s.step(t, AgentStep{Tool: "get_my_usage", Summary: "consultou o próprio uso e limite"})
	return strings.TrimSpace(b.String()), nil
}
