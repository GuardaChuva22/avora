package control

import (
	"context"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"log"
	"net/http"
	"regexp"
	"strings"
	"sync"
	"time"
	_ "time/tzdata" // the VPS image may have no zoneinfo
	"unicode/utf8"

	"github.com/GuardaChuva22/avora/internal/store"
)

// Rotinas (spec M5) and responsabilidades (spec M1).
//
// A routine is a prompt an agent runs on a schedule: at a time of day on some
// weekdays, or every N minutes (at least 15). Its answer goes to #time or to
// the routine's own conversation with the agent. Every run gets a snapshot
// of the Avora's state (nodes, recent jobs, the team's routines and
// responsibilities), so a briefing is about what actually happened.
//
// Ten failures in a row disable the routine and say so in #time. Routines
// run with the agent's own permission mode in its own folder; nothing needs
// an approval that nobody is there to give.

const (
	MinRoutineEvery     = 15
	maxRoutineEvery     = 7 * 24 * 60
	RoutineMaxFailures  = 10
	maxRoutinesPerAgent = 20
	maxRoutinePrompt    = 8000
	maxResponsibilities = 10
	// RoutineTick is how often the scheduler looks for due routines.
	RoutineTick = 30 * time.Second
	// lockPrefix starts the loop-lock notice in #time.
	lockPrefix    = "Trava do canal"
	routinePrefix = "⏰ Rotina"
)

var timeOfDay = regexp.MustCompile(`^([01]\d|2[0-3]):([0-5]\d)$`)

var weekdays = []string{"dom", "seg", "ter", "qua", "qui", "sex", "sáb"}

// routineState tracks runs in flight, which the store does not keep.
type routineState struct {
	mu      sync.Mutex
	running map[string]bool   // routine id
	jobs    map[string]string // agent job id -> routine id
}

func (rs *routineState) start(id string) bool {
	rs.mu.Lock()
	defer rs.mu.Unlock()
	if rs.running == nil {
		rs.running, rs.jobs = map[string]bool{}, map[string]string{}
	}
	if rs.running[id] {
		return false
	}
	rs.running[id] = true
	return true
}

func (rs *routineState) end(id string) {
	rs.mu.Lock()
	defer rs.mu.Unlock()
	delete(rs.running, id)
}

func (rs *routineState) addJob(jobID, routineID string) {
	rs.mu.Lock()
	defer rs.mu.Unlock()
	rs.jobs[jobID] = routineID
}

func (rs *routineState) takeJob(jobID string) string {
	rs.mu.Lock()
	defer rs.mu.Unlock()
	id := rs.jobs[jobID]
	delete(rs.jobs, jobID)
	return id
}

// SetTimeZone sets the clock routine times are read in.
func (s *Server) SetTimeZone(name string) error {
	loc, err := time.LoadLocation(name)
	if err != nil {
		return err
	}
	s.loc = loc
	return nil
}

func (s *Server) zone() *time.Location {
	if s.loc == nil {
		// SetTimeZone runs at start (AVORA_TIMEZONE or the machine's zone).
		return time.UTC
	}
	return s.loc
}

// nextRun is the first time after `after` the schedule fires.
func nextRun(sc store.Schedule, after time.Time, loc *time.Location) time.Time {
	if sc.Every > 0 {
		return after.Add(time.Duration(sc.Every) * time.Minute).Truncate(time.Minute)
	}
	m := timeOfDay.FindStringSubmatch(sc.At)
	if m == nil {
		return after.Add(24 * time.Hour)
	}
	hour, minute := int(m[1][0]-'0')*10+int(m[1][1]-'0'), int(m[2][0]-'0')*10+int(m[2][1]-'0')
	if sc.Date != "" {
		// Once: that moment, even if already past (a reminder missed while
		// the server was down still runs, late).
		d, _ := time.ParseInLocation(time.DateOnly, sc.Date, loc)
		return time.Date(d.Year(), d.Month(), d.Day(), hour, minute, 0, 0, loc).UTC()
	}
	local := after.In(loc)
	for i := 0; i <= 7; i++ {
		d := local.AddDate(0, 0, i)
		at := time.Date(d.Year(), d.Month(), d.Day(), hour, minute, 0, 0, loc)
		if !at.After(after) {
			continue
		}
		if len(sc.Days) == 0 || containsInt(sc.Days, int(at.Weekday())) {
			return at.UTC()
		}
	}
	return after.Add(7 * 24 * time.Hour)
}

func containsInt(list []int, v int) bool {
	for _, x := range list {
		if x == v {
			return true
		}
	}
	return false
}

// scheduleText says a schedule in Portuguese: "seg, qua às 09:00".
func scheduleText(sc store.Schedule) string {
	if sc.Every > 0 {
		if sc.Every%60 == 0 {
			return fmt.Sprintf("a cada %d h", sc.Every/60)
		}
		return fmt.Sprintf("a cada %d min", sc.Every)
	}
	if sc.Date != "" {
		if d, err := time.Parse(time.DateOnly, sc.Date); err == nil {
			return "uma vez, " + d.Format("02/01/2006") + " às " + sc.At
		}
	}
	if len(sc.Days) == 0 || len(sc.Days) == 7 {
		return "todo dia às " + sc.At
	}
	names := []string{}
	for d := 0; d < 7; d++ {
		if containsInt(sc.Days, d) {
			names = append(names, weekdays[d])
		}
	}
	return strings.Join(names, ", ") + " às " + sc.At
}

func validSchedule(sc store.Schedule) error {
	switch {
	case sc.Every != 0 && sc.At != "":
		return errors.New("schedule takes either every or at, not both")
	case sc.Every != 0:
		if sc.Every < MinRoutineEvery || sc.Every > maxRoutineEvery {
			return fmt.Errorf("every must be between %d and %d minutes", MinRoutineEvery, maxRoutineEvery)
		}
		if len(sc.Days) > 0 {
			return errors.New("days only go with at")
		}
	case !timeOfDay.MatchString(sc.At):
		return errors.New(`at must be a time like "08:00"`)
	case sc.Date != "":
		if _, err := time.Parse(time.DateOnly, sc.Date); err != nil {
			return errors.New(`date must be a day like "2026-11-12"`)
		}
		if len(sc.Days) > 0 {
			return errors.New("date runs once; it takes no days")
		}
	}
	seen := map[int]bool{}
	for _, d := range sc.Days {
		if d < 0 || d > 6 || seen[d] {
			return errors.New("days must be distinct weekdays from 0 (Sunday) to 6")
		}
		seen[d] = true
	}
	return nil
}

// validResponsibilities checks an agent's goals and gives new ones an id.
func validResponsibilities(list []store.Responsibility) ([]store.Responsibility, error) {
	if len(list) > maxResponsibilities {
		return nil, fmt.Errorf("an agent has at most %d responsibilities", maxResponsibilities)
	}
	out := make([]store.Responsibility, 0, len(list))
	ids := map[string]bool{}
	for _, r := range list {
		r.Goal, r.Metric, r.Cadence = strings.TrimSpace(r.Goal), strings.TrimSpace(r.Metric), strings.TrimSpace(r.Cadence)
		switch {
		case r.Goal == "" || utf8.RuneCountInString(r.Goal) > 200:
			return nil, errors.New("a responsibility's goal must have 1 to 200 characters")
		case utf8.RuneCountInString(r.Metric) > 200 || utf8.RuneCountInString(r.Cadence) > 100:
			return nil, errors.New("metric has at most 200 characters and cadence 100")
		}
		if r.ID == "" {
			raw, err := randomBytes(4)
			if err != nil {
				return nil, err
			}
			r.ID = hex.EncodeToString(raw)
		}
		if !agentIDPattern.MatchString(r.ID) || ids[r.ID] {
			return nil, errors.New("responsibility ids must be distinct short lowercase names")
		}
		ids[r.ID] = true
		out = append(out, r)
	}
	return out, nil
}

func responsibilityText(r store.Responsibility) string {
	text := r.Goal
	extra := []string{}
	if r.Metric != "" {
		extra = append(extra, "métrica: "+r.Metric)
	}
	if r.Cadence != "" {
		extra = append(extra, "cadência: "+r.Cadence)
	}
	if len(extra) > 0 {
		text += " (" + strings.Join(extra, "; ") + ")"
	}
	return text
}

// ── API ─────────────────────────────────────────────────────────

// RoutineRequest creates or edits a routine.
type RoutineRequest struct {
	AgentID          string         `json:"agent_id"`
	ResponsibilityID string         `json:"responsibility_id"`
	Title            string         `json:"title"`
	Prompt           string         `json:"prompt"`
	Schedule         store.Schedule `json:"schedule"`
	Deliver          string         `json:"deliver"`
	Enabled          *bool          `json:"enabled"`
	// Skill is the skill the routine follows; nil keeps the current one.
	Skill *string `json:"skill"`
	// Trigger runs the routine on an event instead of the schedule.
	Trigger *store.Trigger `json:"trigger"`
}

func (s *Server) registerRoutines(mux *http.ServeMux) {
	mux.HandleFunc("GET /v1/routines", s.admin(s.listRoutines))
	mux.HandleFunc("POST /v1/routines", s.admin(s.createRoutine))
	mux.HandleFunc("PUT /v1/routines/{id}", s.admin(s.updateRoutine))
	mux.HandleFunc("DELETE /v1/routines/{id}", s.admin(s.deleteRoutine))
	mux.HandleFunc("POST /v1/routines/{id}/run", s.admin(s.runRoutineNow))
}

func (s *Server) listRoutines(w http.ResponseWriter, r *http.Request) {
	list, err := s.store.ListRoutines(r.Context(), r.URL.Query().Get("agent"))
	if err != nil {
		storeError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, list)
}

// toRoutine validates a request onto base (the current routine on edit).
func (s *Server) toRoutine(ctx context.Context, req RoutineRequest, base store.Routine) (store.Routine, int, error) {
	rt := base
	rt.Title, rt.Prompt = strings.TrimSpace(req.Title), strings.TrimSpace(req.Prompt)
	rt.Schedule, rt.Deliver, rt.ResponsibilityID = req.Schedule, req.Deliver, req.ResponsibilityID
	if rt.Deliver == "" {
		rt.Deliver = "time"
	}
	if req.Enabled != nil {
		if *req.Enabled && !rt.Enabled {
			rt.Failures = 0 // turning it back on starts a new count
		}
		rt.Enabled = *req.Enabled
	}
	switch {
	case rt.Title == "" || utf8.RuneCountInString(rt.Title) > 80:
		return rt, 400, errors.New("title must have 1 to 80 characters")
	case rt.Prompt == "" || len(rt.Prompt) > maxRoutinePrompt:
		return rt, 400, fmt.Errorf("prompt must have 1 to %d bytes", maxRoutinePrompt)
	case rt.Deliver != "time" && rt.Deliver != "conversation":
		return rt, 400, errors.New(`deliver must be "time" or "conversation"`)
	}
	if req.Trigger != nil && req.Trigger.Kind != "" {
		tr, err := s.cleanTrigger(ctx, *req.Trigger, base.Trigger)
		if err != nil {
			return rt, 400, err
		}
		rt.Trigger, rt.Schedule = tr, store.Schedule{}
	} else {
		rt.Trigger = nil
		if err := validSchedule(rt.Schedule); err != nil {
			return rt, 400, err
		}
	}
	agent, err := s.store.GetAgent(ctx, req.AgentID)
	if err != nil {
		return rt, 400, errors.New("unknown agent")
	}
	if rt.AgentID != agent.ID {
		rt.ConversationID = "" // a conversation belongs to one agent
	}
	if req.Skill != nil {
		rt.Skill = *req.Skill
	}
	if rt.Skill != "" {
		id, err := s.routineSkill(ctx, agent, rt.Skill)
		if err != nil {
			return rt, 400, err
		}
		rt.Skill = id
	}
	rt.AgentID = agent.ID
	if rt.ResponsibilityID != "" {
		found := false
		for _, resp := range agent.Responsibilities {
			found = found || resp.ID == rt.ResponsibilityID
		}
		if !found {
			return rt, 400, errors.New("the agent has no such responsibility")
		}
	}
	rt.NextRun = nil
	if rt.Enabled && rt.Trigger == nil {
		next := nextRun(rt.Schedule, s.now(), s.zone())
		if rt.Schedule.Date != "" && !next.After(s.now()) {
			return rt, 400, errors.New("esse dia e horário já passaram")
		}
		rt.NextRun = &next
	}
	return rt, 0, nil
}

func (s *Server) createRoutine(w http.ResponseWriter, r *http.Request) {
	var req RoutineRequest
	if !readJSONMax(w, r, &req, maxRoutinePrompt*2+4096) {
		return
	}
	if req.Enabled == nil {
		on := true
		req.Enabled = &on
	}
	rt, code, err := s.toRoutine(r.Context(), req, store.Routine{})
	if err != nil {
		http.Error(w, err.Error(), code)
		return
	}
	existing, err := s.store.ListRoutines(r.Context(), rt.AgentID)
	if err != nil {
		storeError(w, err)
		return
	}
	if len(existing) >= maxRoutinesPerAgent {
		http.Error(w, fmt.Sprintf("an agent has at most %d routines", maxRoutinesPerAgent), http.StatusConflict)
		return
	}
	raw, err := randomBytes(8)
	if err != nil {
		http.Error(w, "failed to create routine", http.StatusInternalServerError)
		return
	}
	rt.ID = hex.EncodeToString(raw)
	saved, err := s.store.SaveRoutine(r.Context(), rt, store.ActorAdmin, s.now())
	if err != nil {
		storeError(w, err)
		return
	}
	writeJSON(w, http.StatusCreated, saved)
}

func (s *Server) updateRoutine(w http.ResponseWriter, r *http.Request) {
	current, err := s.store.GetRoutine(r.Context(), r.PathValue("id"))
	if err != nil {
		storeError(w, err)
		return
	}
	var req RoutineRequest
	if !readJSONMax(w, r, &req, maxRoutinePrompt*2+4096) {
		return
	}
	rt, code, err := s.toRoutine(r.Context(), req, current)
	if err != nil {
		http.Error(w, err.Error(), code)
		return
	}
	saved, err := s.store.SaveRoutine(r.Context(), rt, store.ActorAdmin, s.now())
	if err != nil {
		storeError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, saved)
}

func (s *Server) deleteRoutine(w http.ResponseWriter, r *http.Request) {
	if err := s.store.DeleteRoutine(r.Context(), r.PathValue("id"), store.ActorAdmin, s.now()); err != nil {
		storeError(w, err)
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

func (s *Server) runRoutineNow(w http.ResponseWriter, r *http.Request) {
	rt, err := s.store.GetRoutine(r.Context(), r.PathValue("id"))
	if err != nil {
		storeError(w, err)
		return
	}
	if !s.startRoutine(context.WithoutCancel(r.Context()), rt, true, "") {
		http.Error(w, "the routine is already running", http.StatusConflict)
		return
	}
	rt, _ = s.store.GetRoutine(r.Context(), rt.ID)
	writeJSON(w, http.StatusAccepted, rt)
}

// ── Scheduler ───────────────────────────────────────────────────

// RunRoutines starts due routines until ctx ends.
func (s *Server) RunRoutines(ctx context.Context) {
	ticker := time.NewTicker(RoutineTick)
	defer ticker.Stop()
	for {
		s.TickRoutines(ctx)
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
		}
	}
}

// TickRoutines starts every enabled routine that is due. A routine missed
// while the server was down runs once, not once per missed slot.
func (s *Server) TickRoutines(ctx context.Context) {
	list, err := s.store.ListRoutines(ctx, "")
	if err != nil {
		if ctx.Err() == nil {
			log.Printf("routines: %v", err)
		}
		return
	}
	now := s.now()
	for _, rt := range list {
		switch {
		case !rt.Enabled || rt.Trigger != nil:
		case rt.NextRun == nil:
			next := nextRun(rt.Schedule, now, s.zone())
			rt.NextRun = &next
			_, _ = s.store.SaveRoutine(ctx, rt, "", now)
		case !rt.NextRun.After(now):
			s.startRoutine(ctx, rt, false, "")
		}
	}
}

// startRoutine runs a routine now. It returns false if it is still running.
// event is what fired a triggered routine, for the agent.
func (s *Server) startRoutine(ctx context.Context, rt store.Routine, manual bool, event string) bool {
	now := s.now()
	if rt.Enabled && rt.Trigger == nil {
		next := nextRun(rt.Schedule, now, s.zone())
		rt.NextRun = &next
		if rt.Schedule.Date != "" { // a reminder runs once
			rt.Enabled, rt.NextRun = false, nil
		}
	}
	if a, err := s.store.GetAgent(ctx, rt.AgentID); err == nil {
		if a.ArchivedAt != nil {
			// An archived agent's routines wait until it comes back.
			_, _ = s.store.SaveRoutine(ctx, rt, "", now)
			return true
		}
		if ok, reason := s.schedulerAllows(ctx, a, manual); !ok {
			if !manual {
				rt.LastRun, rt.LastStatus, rt.LastPreview = &now, "skipped", reason
				_, _ = s.store.SaveRoutine(ctx, rt, "", now)
			}
			return false
		}
	}
	if !s.routines.start(rt.ID) {
		if !manual { // a slow run: skip this slot
			_, _ = s.store.SaveRoutine(ctx, rt, "", now)
		}
		return false
	}
	rt.LastRun, rt.LastStatus = &now, "running"
	rt, err := s.store.SaveRoutine(ctx, rt, "", now)
	if err != nil {
		s.routines.end(rt.ID)
		log.Printf("routine %s: %v", rt.ID, err)
		return true
	}
	payload, _ := json.Marshal(map[string]any{"routine_id": rt.ID, "agent_id": rt.AgentID, "manual": manual, "deliver": rt.Deliver})
	_ = s.store.RecordEvent(ctx, store.Event{At: now, Type: store.EventRoutineRun, Actor: store.ActorSystem, Payload: payload})

	agent, err := s.store.GetAgent(ctx, rt.AgentID)
	if err != nil {
		s.routineFinished(rt.ID, store.ChatMessage{Error: "funcionário não encontrado"})
		return true
	}
	snapshot := s.stateSnapshot(ctx, rt, agent)
	if event != "" {
		snapshot = event + "\n\n" + snapshot
	}
	if rt.Deliver == "time" {
		s.routineInChannel(ctx, rt, agent, snapshot)
	} else {
		s.routineInConversation(ctx, rt, agent, snapshot)
	}
	return true
}

func (s *Server) routineInChannel(ctx context.Context, rt store.Routine, agent store.Agent, snapshot string) {
	if _, err := s.store.AppendMessage(ctx, store.ChatMessage{
		ConversationID: TeamChannelID, Role: "system", CreatedAt: s.now(),
		Content: fmt.Sprintf("%s «%s» · %s\n%s", routinePrefix, rt.Title, agent.Name, rt.Prompt),
	}, store.ActorSystem); err != nil {
		s.routineFinished(rt.ID, store.ChatMessage{Error: "não consegui escrever no #time"})
		return
	}
	s.channel.mu.Lock()
	start := s.claimLocked(TeamChannelID, agent.ID, "a rotina «"+rt.Title+"»")
	if start != nil {
		run := s.runsLocked(TeamChannelID)[agent.ID]
		run.routine, run.extra = rt.ID, snapshot
	}
	s.channel.mu.Unlock()
	if start == nil {
		s.routineSkipped(rt.ID, agent.Name+" estava ocupado no #time")
		return
	}
	start()
}

func (s *Server) routineInConversation(ctx context.Context, rt store.Routine, agent store.Agent, snapshot string) {
	conv, err := s.store.GetConversation(ctx, rt.ConversationID)
	if err != nil || conv.AgentID != agent.ID {
		raw, rerr := randomBytes(12)
		if rerr != nil {
			s.routineFinished(rt.ID, store.ChatMessage{Error: "não consegui abrir a conversa"})
			return
		}
		model := ""
		if eng := s.chatEngine(); eng != nil {
			model = eng.Model()
		}
		conv, err = s.store.CreateConversation(ctx, store.Conversation{
			ID: hex.EncodeToString(raw), AgentID: agent.ID, Title: "⏰ " + rt.Title, Engine: "native", Model: model, CreatedAt: s.now(),
		}, store.ActorSystem)
		if err != nil {
			s.routineFinished(rt.ID, store.ChatMessage{Error: "não consegui abrir a conversa"})
			return
		}
		if cur, err := s.store.GetRoutine(ctx, rt.ID); err == nil {
			cur.ConversationID = conv.ID
			_, _ = s.store.SaveRoutine(ctx, cur, "", s.now())
		}
	}

	id := newID()
	_, err = s.startTurn(ctx, id, conv, agent, s.routinePrompt(ctx, rt), 0, snapshot, store.ActorSystem, func(msg store.ChatMessage) {
		s.routineFinished(rt.ID, msg)
	})
	switch {
	case errors.Is(err, store.ErrConflict):
		s.routineSkipped(rt.ID, "a conversa da rotina ainda estava respondendo")
	case err != nil:
		s.routineFinished(rt.ID, store.ChatMessage{Error: "não consegui começar: " + err.Error()})
	}
}

// routineFinished records how a run ended; ten failures in a row turn the
// routine off and say so in #time.
func (s *Server) routineFinished(id string, msg store.ChatMessage) {
	defer s.routines.end(id)
	ctx := context.Background()
	rt, err := s.store.GetRoutine(ctx, id)
	if err != nil {
		return
	}
	rt.LastPreview = preview(msg.Content)
	if msg.Error != "" {
		rt.LastStatus, rt.LastPreview = "failed", preview(msg.Error)
		rt.Failures++
	} else {
		rt.LastStatus, rt.Failures = "ok", 0
	}
	if rt.Enabled && rt.Failures >= RoutineMaxFailures {
		rt.Enabled, rt.NextRun = false, nil
		name := rt.AgentID
		if a, err := s.store.GetAgent(ctx, rt.AgentID); err == nil {
			name = a.Name
		}
		_, _ = s.store.AppendMessage(ctx, store.ChatMessage{
			ConversationID: TeamChannelID, Role: "system", CreatedAt: s.now(),
			Content: fmt.Sprintf("%s «%s» · %s desativada depois de %d falhas seguidas. Último erro: %s",
				routinePrefix, rt.Title, name, RoutineMaxFailures, msg.Error),
		}, store.ActorSystem)
	}
	if _, err := s.store.SaveRoutine(ctx, rt, "", s.now()); err != nil {
		log.Printf("routine %s: storing the result failed: %v", id, err)
	}
}

func (s *Server) routineSkipped(id, why string) {
	defer s.routines.end(id)
	ctx := context.Background()
	if rt, err := s.store.GetRoutine(ctx, id); err == nil {
		rt.LastStatus, rt.LastPreview = "skipped", why
		_, _ = s.store.SaveRoutine(ctx, rt, "", s.now())
	}
}

func preview(text string) string {
	p := strings.Join(strings.Fields(text), " ")
	if utf8.RuneCountInString(p) > 140 {
		p = string([]rune(p)[:140])
	}
	return p
}

// stateSnapshot is what a routine run knows about the Avora right now.
func (s *Server) stateSnapshot(ctx context.Context, rt store.Routine, agent store.Agent) string {
	loc := s.zone()
	now := s.now().In(loc)
	var b strings.Builder
	fmt.Fprintf(&b, "--- Rotina «%s» (%s) · agora é %s %s ---\n", rt.Title, whenOf(rt),
		weekdays[now.Weekday()], now.Format("02/01 15:04"))
	for _, resp := range agent.Responsibilities {
		if resp.ID == rt.ResponsibilityID {
			b.WriteString("Responsabilidade desta rotina: " + responsibilityText(resp) + "\n")
		}
	}
	b.WriteString(s.stateText(ctx, rt.ID))
	b.WriteString("Você está rodando sozinho, sem o dono por perto: não peça confirmação, entregue o resultado.")
	return truncate(b.String(), 6000)
}

// stateText is what the Avora looks like now: nodes, the last day's work and
// the team's routines and responsibilities (skip leaves one routine out).
func (s *Server) stateText(ctx context.Context, skip string) string {
	loc := s.zone()
	var b strings.Builder
	if nodes, err := s.store.ListNodes(ctx); err == nil {
		parts := []string{}
		for _, n := range nodes {
			if n.RevokedAt != nil {
				continue
			}
			state := "offline"
			if s.online(n) {
				state = "online"
			}
			parts = append(parts, fmt.Sprintf("%s (%s) %s", n.Name, n.Kind, state))
		}
		if len(parts) > 0 {
			b.WriteString("Nós: " + strings.Join(parts, ", ") + ".\n")
		}
	}

	if jobs, err := s.store.ListJobs(ctx, 200); err == nil {
		since := s.now().Add(-24 * time.Hour)
		count := map[store.Status]int{}
		failed := []string{}
		for _, j := range jobs {
			if j.CreatedAt.Before(since) || j.Action != store.ActionAgentRun {
				continue
			}
			count[j.Status]++
			if j.Status == store.StatusFailed && len(failed) < 3 && j.Agent != nil {
				failed = append(failed, strings.TrimPrefix(j.Agent.Workdir, "agents/"))
			}
		}
		fmt.Fprintf(&b, "Tarefas de agentes nas últimas 24 h: %d concluídas, %d falharam, %d em andamento.",
			count[store.StatusSucceeded], count[store.StatusFailed]+count[store.StatusTimedOut], count[store.StatusQueued]+count[store.StatusRunning])
		if len(failed) > 0 {
			b.WriteString(" Falhas de: " + strings.Join(failed, ", ") + ".")
		}
		b.WriteString("\n")
	}

	team, _ := s.store.ListAgents(ctx)
	names := map[string]string{}
	for _, a := range team {
		names[a.ID] = a.Name
	}
	if routines, err := s.store.ListRoutines(ctx, ""); err == nil && len(routines) > 0 {
		b.WriteString("Rotinas do time:\n")
		for _, other := range routines {
			if other.ID == skip {
				continue
			}
			line := fmt.Sprintf("- id=%s «%s» de %s, %s", other.ID, other.Title, names[other.AgentID], whenOf(other))
			if !other.Enabled {
				line += ", desligada"
			}
			if other.LastRun != nil && other.LastStatus != "" && other.LastStatus != "running" {
				line += fmt.Sprintf("; última (%s, %s): %s", other.LastStatus, other.LastRun.In(loc).Format("02/01 15:04"), other.LastPreview)
			}
			b.WriteString(truncate(line, 300) + "\n")
		}
	}
	lines := []string{}
	for _, a := range team {
		for _, resp := range a.Responsibilities {
			lines = append(lines, "- "+a.Name+": "+responsibilityText(resp))
		}
	}
	if len(lines) > 0 {
		b.WriteString("Responsabilidades do time:\n" + strings.Join(lines, "\n") + "\n")
	}
	return truncate(b.String(), 5000)
}

// seedRoutines are the starting routines (spec M5): the morning briefing and
// the weekly review, both by the chief of staff in #time.
func seedRoutines() []store.Routine {
	return []store.Routine{
		{ID: "briefing-manha", AgentID: DefaultAgentID, Title: "Briefing da manhã", Deliver: "time", Enabled: true,
			Schedule: store.Schedule{At: "08:00", Days: []int{1, 2, 3, 4, 5}},
			Prompt: `Bom dia! Faça o briefing da manhã para o seu dono, em até 8 linhas: o que o time entregou nas últimas 24 h, o que falhou ou travou, as rotinas e responsabilidades que pedem atenção e uma sugestão de foco para hoje.
Só chame alguém com @ se houver algo concreto para essa pessoa fazer agora.`},
		{ID: "revisao-semanal", AgentID: DefaultAgentID, Title: "Revisão semanal", Deliver: "time", Enabled: true,
			Schedule: store.Schedule{At: "09:00", Days: []int{1}},
			Prompt: `Faça a revisão semanal das responsabilidades do time: para cada funcionário com responsabilidades, diga se está no caminho (ok, atenção ou fora) com base nas checagens da semana, o que travou e uma mudança concreta para a próxima semana.
Termine com as 3 prioridades da semana. Só chame alguém com @ se houver algo concreto para fazer agora.`},
	}
}
