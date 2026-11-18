package control

import (
	"context"
	"errors"
	"fmt"
	"log"
	"net/http"
	"regexp"
	"strings"
	"sync"
	"time"
	"unicode"
	"unicode/utf8"

	"github.com/GuardaChuva22/avora/internal/store"
)

// Channels (spec M9, D54): group conversations of the owner with some of the
// agents, like Discord channels. #time is the first one, with everyone in it;
// the owner creates others, each with a name, a purpose and its members.
//
//   - The owner's message goes to the agents it @mentions, or to the router,
//     which picks among the members that answer on their own.
//   - An agent that @mentions colleagues in its answer calls them in turn, so
//     the team can hand work around.
//   - The loop lock: after ChannelLoopLimit agent answers in a row without a
//     message from the owner, nobody else is called until the owner writes.
//   - Each agent answers one call at a time per channel; a call that arrives
//     while it is working runs once it finishes.

const (
	// TeamChannelID is the id of #time, the channel with everyone.
	TeamChannelID = "time"
	// ChannelLoopLimit is how many agent answers can follow one owner message.
	// ChannelLoopLimit is how many answers the team gives on its own since
	// the owner last wrote (a "stretch", as agentes-chat calls it, D59).
	ChannelLoopLimit = 20
	// channelContext is how many recent messages an agent reads.
	channelContext = 24
	ownerName      = "Dono"
)

// mentionPattern finds @Name in a message; names may carry accents.
var mentionPattern = regexp.MustCompile(`@([\p{L}\p{N}][\p{L}\p{N}_-]*)`)

// everyone are the mentions that call the whole channel.
var everyone = map[string]bool{"todos": true, "time": true, "equipe": true, "everyone": true, "canal": true}

type channelRun struct {
	JobID string    `json:"job_id,omitempty"`
	By    string    `json:"by"`
	Since time.Time `json:"since"`
	// again is a call that arrived while the agent was working.
	again  string
	cancel context.CancelFunc
	turn   bool // answered here; JobID is the turn's id
	// routine is the routine this answer runs, with its state snapshot.
	routine, extra string
	// forOwner: the owner's message is for this agent (the router's first
	// pick); it answers in words, not with a reaction.
	forOwner bool
	// nudged: it was called back because it promised and stopped (D59).
	nudged bool
	// heard are the colleagues it just heard back from: its mentions of
	// them wake nobody (ADR 0089 of agentes-chat), so two agents never
	// bounce the same request.
	heard []string
	// quiet says the agent may stay quiet ("routed": the router brought it
	// in with others; "agent": a colleague called it). Empty: it answers.
	quiet string
}

type channelState struct {
	mu   sync.Mutex
	runs map[string]map[string]*channelRun // channel id -> agent id -> its answer in flight
	// queue holds colleagues called together, answering one by one (D57).
	queue map[string][]queuedCall
	// discuss is a channel's discussion among the agents (D57).
	discuss map[string]*discussion
	// back are agents waiting for the colleagues they called (D59).
	back map[string][]handBack
}

// runsLocked are a channel's answers in flight; s.channel.mu must be held.
func (s *Server) runsLocked(ch string) map[string]*channelRun {
	if s.channel.runs == nil {
		s.channel.runs = map[string]map[string]*channelRun{}
	}
	runs := s.channel.runs[ch]
	if runs == nil {
		runs = map[string]*channelRun{}
		s.channel.runs[ch] = runs
	}
	return runs
}

// channelRoom is a channel with its agents resolved.
type channelRoom struct {
	ch       store.Channel
	all      []store.Agent // every agent
	members  []store.Agent // the agents in the channel
	routable []store.Agent // members the router may pick (not @ only)
}

func (s *Server) loadRoom(ctx context.Context, id string) (channelRoom, error) {
	ch, err := s.store.GetChannel(ctx, id)
	if err != nil {
		return channelRoom{}, err
	}
	all, err := s.store.ListAgents(ctx)
	if err != nil {
		return channelRoom{}, err
	}
	r := channelRoom{ch: ch, all: all}
	for _, a := range all {
		if m, ok := ch.Member(a.ID); ok {
			r.members = append(r.members, a)
			if !m.MentionOnly {
				r.routable = append(r.routable, a)
			}
		}
	}
	return r, nil
}

// ChannelStatus is who is working in a channel and how close the lock is.
type ChannelStatus struct {
	Conversation store.Conversation     `json:"conversation"`
	Channel      store.Channel          `json:"channel"`
	Working      map[string]*channelRun `json:"working"`
	Streak       int                    `json:"streak"`
	Limit        int                    `json:"limit"`
	Locked       bool                   `json:"locked"`
	// Routing: an owner message waits for the router to pick who answers.
	Routing bool `json:"routing"`
	// Next are agents lined up to answer after the ones working now.
	Next []string `json:"next"`
	// Discussing: the agents are talking among themselves (D57).
	Discussing bool `json:"discussing"`
}

// ensureChannel creates #time once, with every agent in it.
func (s *Server) ensureChannel(ctx context.Context) error {
	if _, err := s.store.GetChannel(ctx, TeamChannelID); err == nil {
		return nil
	} else if !errors.Is(err, store.ErrNotFound) {
		return err
	}
	_, err := s.store.SaveChannel(ctx, store.Channel{ID: TeamChannelID, Name: "time", Everyone: true,
		Purpose: "O grupo do time todo: papo, avisos e pedidos rápidos."}, store.ActorSystem, s.now())
	return err
}

// isChannel says whether a conversation is a channel.
func (s *Server) isChannel(ctx context.Context, id string) bool {
	if id == TeamChannelID {
		return true
	}
	_, err := s.store.GetChannel(ctx, id)
	return err == nil
}

// mentions are the agents a text calls, in order, without repeats: @todos
// calls the channel's members, @Nome any agent (being named beats membership).
func mentions(text string, members, all []store.Agent) []string {
	out := []string{}
	seen := map[string]bool{}
	add := func(id string) {
		if !seen[id] {
			seen[id] = true
			out = append(out, id)
		}
	}
	for _, m := range mentionPattern.FindAllStringSubmatch(text, -1) {
		name := slug(m[1])
		if everyone[name] {
			for _, a := range members {
				add(a.ID)
			}
			continue
		}
		for _, a := range all {
			if name == a.ID || name == slug(a.Name) {
				add(a.ID)
			}
		}
	}
	return out
}

// streak counts the agent answers since the owner last wrote.
func streak(history []store.ChatMessage) int {
	n := 0
	for i := len(history) - 1; i >= 0; i-- {
		switch history[i].Role {
		case "user":
			return n
		case "assistant":
			n++
		}
	}
	return n
}

// lockNoticed says whether the lock notice was posted since the owner wrote.
func lockNoticed(history []store.ChatMessage) bool {
	for i := len(history) - 1; i >= 0 && history[i].Role != "user"; i-- {
		if history[i].Role == "system" && strings.HasPrefix(history[i].Content, lockPrefix) {
			return true
		}
	}
	return false
}

// channelRules is how an agent behaves in a channel: the channel's purpose
// and members, then the group's manners.
func channelRules(a store.Agent, room channelRoom, project string) string {
	where := "Agora você está no #" + room.ch.Name + ", um canal de conversa do seu dono com agentes do time, como um grupo de WhatsApp do trabalho. Todo mundo no canal lê tudo."
	if room.ch.Purpose != "" {
		where += "\nPropósito do canal: " + room.ch.Purpose + ". Fique nesse assunto; o que fugir dele, leve para uma conversa direta com o dono."
	}
	if project != "" {
		where += "\nO canal é do projeto " + project + "."
	}
	names := []string{}
	for _, m := range room.members {
		if m.ID != a.ID {
			names = append(names, m.Name+" ("+m.Role+")")
		}
	}
	if len(names) > 0 {
		where += "\nNo canal com você: " + strings.Join(names, ", ") + "."
	}
	return fmt.Sprintf(`

%s
Aqui valem estas regras, acima das de formato:
- Escreva como um colega mandando mensagem no grupo, não como relatório: uma a três frases curtas, no seu jeito, em tom de conversa. Sem títulos, sem negrito, sem listas e sem tabelas, a não ser que peçam algo que precise disso (um passo a passo, uma lista de itens).
- Cumprimento e papo leve ("fala time", "bom dia", "como estão?"): responda como gente, curto e com a sua personalidade (como você está, no que anda mexendo, uma brincadeira se for o seu jeito). Não ofereça ajuda, não puxe trabalho e não fale pelos colegas.
- Leia o que os colegas acabaram de escrever: não repita, complemente, concorde ou discorde em uma frase, e chame o colega pelo nome quando fizer sentido.
- Quando a mensagem só pede um ok, agradece, é uma piada ou você só concorda, reaja em vez de escrever: responda só com um emoji (👍, 😂, ❤️, 🔥, 👀, 🙌) e ele vira uma reação na mensagem, sem texto.
- Trabalho de verdade você faz com as suas ferramentas e conta o resultado em poucas linhas; o que for longo vai numa conversa direta com o dono, não no grupo.
- Para passar trabalho a um colega, escreva @Nome dele (por exemplo @Atlas) dizendo exatamente o que precisa: ele lê e responde em seguida. Para só citar um colega, escreva o nome sem @ ("como a Cora calculou").
- Se um colega ainda vai trazer algo, não escreva só para dizer que vai esperar ou comentar depois: fale quando tiver o que dizer.
- Só chame alguém quando precisar de verdade. Nunca chame a si mesmo (%s) nem quem já está cuidando do assunto.
- Depois de %d respostas seguidas do time sem o dono escrever, o canal trava até ele voltar.`, where, a.Name, ChannelLoopLimit)
}

// channelPrompt is what an agent reads when called in a channel.
func channelPrompt(chName string, a store.Agent, team []store.Agent, history []store.ChatMessage, by string) string {
	names := map[string]string{}
	for _, t := range team {
		names[t.ID] = t.Name
	}
	byID := make(map[int64]store.ChatMessage, len(history))
	for _, m := range history {
		byID[m.ID] = m
	}
	author := func(m store.ChatMessage) string {
		switch {
		case m.Role == "system":
			return "[Avora]"
		case m.Role == "assistant":
			if n := names[m.AgentID]; n != "" {
				return n
			}
			return "Ex-funcionário"
		}
		return ownerName
	}
	if len(history) > channelContext {
		history = history[len(history)-channelContext:]
	}
	var b strings.Builder
	b.WriteString("--- Canal #" + chName + ", mensagens mais recentes por último ---\n")
	for _, m := range history {
		who := author(m)
		if q, ok := byID[m.ReplyTo]; ok && m.ReplyTo != 0 {
			who += " (respondendo a " + author(q) + ": «" + truncate(strings.TrimSpace(q.Content), 300) + "»)"
		}
		text := m.Content
		if text == "" && m.Error != "" {
			text = "(falhou: " + m.Error + ")"
		}
		if text == "" {
			continue
		}
		b.WriteString(who + ": " + truncate(text, 1500) + "\n")
	}
	fmt.Fprintf(&b, "\n--- %s chamou você. Responda agora no canal como %s. ---", by, a.Name)
	return b.String()
}

// sendChannel stores the owner's message in a channel and calls the agents.
func (s *Server) sendChannel(w http.ResponseWriter, r *http.Request, ch, content string, replyTo int64) {
	ctx := r.Context()
	room, err := s.loadRoom(ctx, ch)
	if err != nil {
		storeError(w, err)
		return
	}
	if room.ch.ArchivedAt != nil {
		http.Error(w, "o canal está arquivado", http.StatusConflict)
		return
	}
	user, err := s.store.AppendMessage(ctx, store.ChatMessage{
		ConversationID: ch, Role: "user", Content: content, ReplyTo: replyTo, CreatedAt: s.now(),
	}, store.ActorAdmin)
	if err != nil {
		storeError(w, err)
		return
	}
	called := mentions(content, room.members, room.all)
	// "Conversem entre si": a discussion among the ones named, or the
	// members that answer on their own (D57). Any other message ends one.
	if people := called; wantsDiscussion(content) && (len(people) >= 2 || len(people) == 0 && len(room.routable) >= 2) {
		if len(people) == 0 {
			people = agentIDs(room.routable)
		}
		s.startDiscussion(ch, content, people, false)
		if len(called) > 0 {
			called = called[:1] // the others come in as the discussion goes
		}
	} else if history, err := s.store.Messages(ctx, ch); err == nil && len(called) == 0 && morePattern.MatchString(content) {
		// "Continuem": the agents who were talking pick it up again.
		if topic, people := resumeTalk(history); len(people) >= 2 {
			s.startDiscussion(ch, topic, people, true)
			s.routeCancel(ch)
			s.callRouted(ch, nextSpeaker(s.discussionOf(ch), history, people[0], nil), false)
			writeJSON(w, http.StatusAccepted, map[string]any{"user": user, "called": called, "discussing": true})
			return
		}
		s.endDiscussion(ch)
	} else {
		s.endDiscussion(ch)
	}
	// A reply to an agent's message, without @, calls that agent.
	if len(called) == 0 && replyTo != 0 {
		if q, err := s.messageIn(ctx, ch, replyTo); err == nil && q.Role == "assistant" && q.AgentID != "" {
			for _, a := range room.all {
				if a.ID == q.AgentID {
					called = []string{a.ID}
				}
			}
		}
	}
	if len(called) == 0 && len(room.routable) > 0 {
		// Nobody named: the router decides who it is for (D48).
		s.routeLater(ch, content)
		writeJSON(w, http.StatusAccepted, map[string]any{"user": user, "called": called, "routing": true})
		return
	}
	s.routeCancel(ch) // messages still waiting are answered by this call
	for _, id := range called {
		s.callInChannel(ch, id, ownerName)
	}
	writeJSON(w, http.StatusAccepted, map[string]any{"user": user, "called": called})
}

// callInChannel has an agent answer in a channel, now or after its current
// answer there.
func (s *Server) callInChannel(ch, agentID, by string) {
	s.channel.mu.Lock()
	start := s.claimLocked(ch, agentID, by)
	s.channel.mu.Unlock()
	if start != nil {
		start()
	}
}

// claimLocked books the agent's next answer in a channel and returns what
// starts it, or nil when the agent is busy there (the call then runs after
// the current answer).
func (s *Server) claimLocked(ch, agentID, by string) func() {
	runs := s.runsLocked(ch)
	if run := runs[agentID]; run != nil {
		if run.again == "" {
			run.again = by
		}
		return nil
	}
	ctx, cancel := context.WithTimeout(context.Background(), TurnMinutes*time.Minute)
	run := &channelRun{By: by, Since: s.now(), cancel: cancel}
	runs[agentID] = run
	return func() {
		s.channelEvent(EventChannelWorking, ch, agentID, map[string]any{"by": by})
		go s.answerInChannel(ctx, ch, agentID, run)
	}
}

// answerInChannel produces one agent answer in a channel.
func (s *Server) answerInChannel(ctx context.Context, ch, agentID string, run *channelRun) {
	fail := func(why string) {
		s.channelAnswered(ch, agentID, run, store.ChatMessage{Role: "assistant", Error: why, CreatedAt: s.now()})
	}
	agent, err := s.store.GetAgent(ctx, agentID)
	if err != nil {
		s.endRun(ch, agentID, run)
		return
	}
	room, err := s.loadRoom(ctx, ch)
	if err != nil {
		fail("não consegui ler o canal")
		return
	}
	team := visibleTeam(agent, room.all)
	history, err := s.store.Messages(ctx, ch)
	if err != nil {
		fail("não consegui ler o canal")
		return
	}
	prompt := channelPrompt(room.ch.Name, agent, room.all, history, run.By)
	if run.extra != "" {
		prompt += "\n\n" + run.extra
	}
	s.channel.mu.Lock()
	if d := s.discussLocked(ch); d != nil {
		prompt += discussPrompt(d, streak(history))
	}
	s.channel.mu.Unlock()
	if run.quiet != "" && run.quiet != "discuss" && run.quiet != "back" {
		prompt += quietRule(run.quiet == "agent", agent.Style.Talk)
	}
	if run.forOwner {
		prompt += "\n\nA mensagem do dono é para você: responda com texto, não só com um emoji. Se ele pediu para fazer ou continuar algo, faça e conte."
	}
	project := ""
	if room.ch.ProjectID != "" {
		if p, err := s.store.GetProject(ctx, room.ch.ProjectID); err == nil {
			project = p.Name
		}
	}

	defer run.cancel()
	id := newID()
	s.channel.mu.Lock()
	run.JobID, run.turn = id, true
	s.channel.mu.Unlock()
	t := &turn{agent: agent, team: team, id: id, conv: ch, channel: true}
	// In the group the conversation's layout rule (headings, bold, tables)
	// gives way to texting like a colleague.
	system := strings.Replace(nativeSystem(agent, team), formatRule+"\n", "", 1) + channelRules(agent, room, project)
	answer := s.runTurn(ctx, t, system,
		[]store.ChatMessage{{ConversationID: ch, Role: "user", Content: prompt}})
	s.channelAnswered(ch, agentID, run, answer)
}

// finishChannelJob stores the answer of a CLI agent's job in its channel. The
// job ends only once, so this runs once per job.
func (s *Server) finishChannelJob(job Job, answer store.ChatMessage) {
	ch := job.Agent.ConversationID
	agentID := strings.TrimPrefix(job.Agent.Workdir, "agents/")
	answer.JobID = job.ID
	s.channel.mu.Lock()
	run := s.runsLocked(ch)[agentID]
	if run != nil && run.JobID != job.ID {
		run = nil
	}
	s.channel.mu.Unlock()
	s.channelAnswered(ch, agentID, run, answer)
}

// channelAnswered stores an agent's answer in a channel and calls whoever it
// mentions, unless the loop lock holds. run is nil when the server restarted
// while the agent worked.
func (s *Server) channelAnswered(ch, agentID string, run *channelRun, answer store.ChatMessage) {
	ctx := context.Background()
	answer.ConversationID, answer.AgentID = ch, agentID
	if run != nil && run.quiet != "" && answer.Error == "" && passed(answer.Content) {
		// Nothing to add: no message, and the channel sees it stopped.
		s.channelEvent(EventChannelPassed, ch, agentID, map[string]any{"by": run.By})
		s.afterAnswer(ch, agentID, run, nil, "", true)
		return
	}
	if emoji, ok := onlyEmoji(answer.Content); ok && answer.Error == "" && s.reactInChannel(ctx, ch, agentID, run, emoji) {
		// Just an emoji: a reaction on the message, no bubble.
		s.afterAnswer(ch, agentID, run, nil, "", true)
		return
	}
	actor := store.ActorSystem
	if answer.JobID != "" {
		actor = "agent:" + agentID
	} else if s.chatEngine() != nil && answer.Error == "" {
		actor = s.engineActor()
	}
	stored, err := s.store.AppendMessage(ctx, answer, actor)
	if err != nil {
		log.Printf("#%s: storing %s's answer failed: %v", ch, agentID, err)
		stored = answer
	}
	if run != nil && run.routine != "" {
		s.routineFinished(run.routine, stored)
	}
	called, promise := []string{}, ""
	if answer.Error == "" {
		for _, id := range s.calledBy(ctx, ch, agentID, answer.Content) {
			if run == nil || !contains(run.heard, id) {
				called = append(called, id)
			}
		}
		if run != nil && !run.nudged {
			promise = promised(answer.Content)
		}
	}
	s.afterAnswer(ch, agentID, run, called, promise, false)
}

// calledBy are the colleagues an agent's message calls in a channel, in the
// order named.
func (s *Server) calledBy(ctx context.Context, ch, agentID, content string) []string {
	room, _ := s.loadRoom(ctx, ch)
	team := room.all
	for _, a := range room.all {
		if a.ID == agentID {
			team = visibleTeam(a, room.all) // a project agent only calls its own
		}
	}
	members := []store.Agent{}
	for _, m := range room.members {
		if contains(agentIDs(team), m.ID) {
			members = append(members, m)
		}
	}
	called := []string{}
	for _, id := range mentions(content, members, team) {
		if id != agentID {
			called = append(called, id)
		}
	}
	return called
}

// afterAnswer frees the agent and books the next answers at once, so the
// channel never looks idle in between (D57: one colleague at a time, then a
// discussion's next speaker). The lock counts stored answers plus the ones
// still being written.
func (s *Server) afterAnswer(ch, agentID string, run *channelRun, called []string, promise string, passed bool) {
	ctx := context.Background()
	name := agentID
	if a, err := s.store.GetAgent(ctx, agentID); err == nil {
		name = a.Name
	}
	history, err := s.store.Messages(ctx, ch)
	s.channel.mu.Lock()
	runs := s.runsLocked(ch)
	again := ""
	if run != nil {
		run.cancel()
		if runs[agentID] == run {
			delete(runs, agentID)
		}
		again = run.again
	}
	var starts []func()
	if err == nil {
		starts = s.nextCallsLocked(ch, agentID, name, called, again, promise, passed, history)
	}
	s.channel.mu.Unlock()
	for _, start := range starts {
		start()
	}
}

func agentIDs(list []store.Agent) []string {
	out := make([]string, len(list))
	for i, a := range list {
		out[i] = a.ID
	}
	return out
}

// onlyEmoji says whether an answer is nothing but emoji ("👍", "😂🔥"): the
// agent reacts instead of writing (D50).
func onlyEmoji(content string) (string, bool) {
	c := strings.Join(strings.Fields(content), "")
	n := utf8.RuneCountInString(c)
	if n == 0 || n > 12 {
		return "", false
	}
	symbol := false
	for _, r := range c {
		switch {
		case unicode.IsLetter(r) || unicode.IsDigit(r) || unicode.IsPunct(r) && r != '‼' && r != '⁉':
			return "", false
		case r >= 0x2100:
			symbol = true
		}
	}
	return c, symbol
}

// reactInChannel puts an agent's emoji on the message it was called for: the
// owner's latest, or the latest of the colleague who called it.
func (s *Server) reactInChannel(ctx context.Context, ch, agentID string, run *channelRun, emoji string) bool {
	history, err := s.store.Messages(ctx, ch)
	if err != nil {
		return false
	}
	byOwner := run == nil || run.By == ownerName
	for i := len(history) - 1; i >= 0; i-- {
		m := history[i]
		if byOwner && m.Role != "user" || !byOwner && (m.Role != "assistant" || m.AgentID == agentID) {
			continue
		}
		_, err := s.store.React(ctx, ch, m.ID, store.Reaction{AgentID: agentID, Emoji: emoji}, "agent:"+agentID, s.now())
		return err == nil
	}
	return false
}

// endRun frees the agent for the next call and returns a call that waited.
func (s *Server) endRun(ch, agentID string, run *channelRun) string {
	if run == nil {
		return ""
	}
	run.cancel()
	s.channel.mu.Lock()
	defer s.channel.mu.Unlock()
	if runs := s.runsLocked(ch); runs[agentID] == run {
		delete(runs, agentID)
	}
	return run.again
}

// channelStatus is GET /v1/channel (#time) and GET /v1/channels/{id}/status.
func (s *Server) channelStatus(w http.ResponseWriter, r *http.Request) {
	ch := r.PathValue("id")
	if ch == "" {
		ch = TeamChannelID
	}
	conv, err := s.store.GetConversation(r.Context(), ch)
	if err != nil {
		storeError(w, err)
		return
	}
	c, err := s.store.GetChannel(r.Context(), ch)
	if err != nil {
		storeError(w, err)
		return
	}
	history, err := s.store.Messages(r.Context(), ch)
	if err != nil {
		storeError(w, err)
		return
	}
	st := ChannelStatus{Conversation: conv, Channel: c, Working: map[string]*channelRun{}, Streak: streak(history), Limit: ChannelLoopLimit}
	st.Locked = lockNoticed(history)
	st.Routing = s.routing(ch)
	st.Next = s.nextUp(ch)
	if st.Discussing = s.discussing(ch); st.Discussing {
		st.Limit = discussTurns
	}
	s.channel.mu.Lock()
	for id, run := range s.runsLocked(ch) {
		copy := *run
		st.Working[id] = &copy
	}
	s.channel.mu.Unlock()
	writeJSON(w, http.StatusOK, st)
}

// stopChannel stops every answer in flight in a channel.
func (s *Server) stopChannel(ctx context.Context, ch string) {
	s.routeCancel(ch)
	s.channel.mu.Lock()
	delete(s.channel.queue, ch)
	delete(s.channel.discuss, ch)
	delete(s.channel.back, ch)
	jobs, natives := []string{}, []*channelRun{}
	for _, run := range s.runsLocked(ch) {
		run.again = ""
		if run.JobID == "" || run.turn {
			natives = append(natives, run)
		} else {
			jobs = append(jobs, run.JobID)
		}
	}
	s.channel.mu.Unlock()
	for _, run := range natives {
		run.cancel() // the answer ends as "parado por você"
	}
	for _, id := range jobs {
		if job, err := s.store.CancelJob(ctx, id, store.ActorAdmin, s.now()); err == nil {
			s.finishAgentReply(job, nil)
		}
	}
}

// workingIn reports whether an agent is answering in a channel.
func (s *Server) workingIn(ch, agentID string) bool {
	s.channel.mu.Lock()
	defer s.channel.mu.Unlock()
	return s.runsLocked(ch)[agentID] != nil
}

// workingInChannel reports whether an agent is answering in any channel.
func (s *Server) workingInChannel(agentID string) bool {
	s.channel.mu.Lock()
	defer s.channel.mu.Unlock()
	for _, runs := range s.channel.runs {
		if runs[agentID] != nil {
			return true
		}
	}
	return false
}
