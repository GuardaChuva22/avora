package control

import (
	"context"
	"encoding/json"
	"fmt"
	"log"
	"math/rand/v2"
	"regexp"
	"strings"
	"sync"
	"time"

	"github.com/GuardaChuva22/avora/internal/store"
)

// Who answers in a channel (D48, D54). A message of the owner that names
// nobody goes to the router: one quick model call (or the rules, D52) reads
// the recent conversation and picks who it is for, among the channel's
// members that answer on their own. Usually one agent; several only when the
// message speaks to the whole channel; the chief of staff (or the first
// member) when it is unclear. The owner's messages in a row are decided
// together, and an agent the router brought in (not called by name) may stay
// quiet. Each channel has its own router state.

// routerQuiet: the owner's next message within this time joins the
// decision ("bom dia" then "como tá o projeto?").
var routerQuiet = 1500 * time.Millisecond

const (
	// routerTimeout bounds the model call; past it the chief answers.
	routerTimeout = 20 * time.Second
	// routerMaxAgents caps a message to the whole team.
	routerMaxAgents = 4
	// quietMark is how a brought-in agent says it has nothing to add.
	quietMark = "[quieto]"
)

type routerState struct {
	mu    sync.Mutex
	rooms map[string]*routerRoom
}

// routerRoom is one channel's router.
type routerRoom struct {
	pending []string // the owner's messages waiting for the decision
	timer   *time.Timer
	// deciding counts decisions being made (the model call).
	deciding int
	// next are agents picked to answer after the one answering now.
	next []string
}

// roomLocked is a channel's router state; s.router.mu must be held.
func (s *Server) roomLocked(ch string) *routerRoom {
	if s.router.rooms == nil {
		s.router.rooms = map[string]*routerRoom{}
	}
	r := s.router.rooms[ch]
	if r == nil {
		r = &routerRoom{}
		s.router.rooms[ch] = r
	}
	return r
}

// nextUp is who the router has lined up to answer after the current one.
func (s *Server) nextUp(ch string) []string {
	s.router.mu.Lock()
	defer s.router.mu.Unlock()
	return append([]string{}, s.roomLocked(ch).next...)
}

// routing says whether a message in the channel waits for the router.
func (s *Server) routing(ch string) bool {
	s.router.mu.Lock()
	defer s.router.mu.Unlock()
	r := s.roomLocked(ch)
	return len(r.pending) > 0 || r.deciding > 0
}

// routeLater queues an owner message for the router; the decision waits
// routerQuiet for more messages in a row.
func (s *Server) routeLater(ch, content string) {
	s.router.mu.Lock()
	defer s.router.mu.Unlock()
	r := s.roomLocked(ch)
	r.pending = append(r.pending, content)
	if r.timer != nil {
		r.timer.Stop()
	}
	r.timer = time.AfterFunc(routerQuiet, func() { s.routeNow(ch) })
}

// routeCancel drops the queued messages: a message with an @ (or a reply to
// an agent) answers them together with its own call.
func (s *Server) routeCancel(ch string) {
	s.router.mu.Lock()
	defer s.router.mu.Unlock()
	r := s.roomLocked(ch)
	if r.timer != nil {
		r.timer.Stop()
	}
	r.pending = nil
}

func (s *Server) routeNow(ch string) {
	s.router.mu.Lock()
	r := s.roomLocked(ch)
	msgs := r.pending
	r.pending, r.timer = nil, nil
	if len(msgs) == 0 {
		s.router.mu.Unlock()
		return
	}
	r.deciding++
	s.router.mu.Unlock()
	defer func() {
		s.router.mu.Lock()
		s.roomLocked(ch).deciding--
		s.router.mu.Unlock()
	}()
	ctx, cancel := context.WithTimeout(context.Background(), routerTimeout)
	defer cancel()
	room, err := s.loadRoom(ctx, ch)
	if err != nil || len(room.routable) == 0 {
		return
	}
	history, _ := s.store.Messages(ctx, ch)
	d := s.pickAgents(ctx, room, history, msgs)
	s.recordRoute(ch, d.agents, d.why)
	if len(d.agents) == 0 {
		// Nothing to answer: whoever the owner was talking to reacts.
		s.ackReaction(ctx, ch, history, d.reaction)
		return
	}
	// The first pick answers; the others come in after it, in turn, and may
	// pass or just react. In a discussion the first opens and the discussion
	// picks who follows (D57).
	if s.discussing(ch) {
		d.agents = d.agents[:1]
	}
	s.callInTurn(ch, d.agents)
}

// route is the router's decision: who answers, or the emoji to react with
// when nobody needs to.
type route struct {
	agents   []string
	reaction string
	why      string
}

// chief is the agent a message goes to when nobody fits.
func chief(team []store.Agent) string {
	for _, a := range team {
		if a.ID == DefaultAgentID {
			return a.ID
		}
	}
	return team[0].ID
}

var routerJSON = regexp.MustCompile(`(?s)\{.*\}`)

// pickAgents asks the model who the messages are for, among the channel's
// members that answer on their own.
func (s *Server) pickAgents(ctx context.Context, room channelRoom, history []store.ChatMessage, msgs []string) route {
	team := room.routable
	eng := s.chatEngine()
	if len(team) == 1 {
		return route{agents: []string{team[0].ID}, why: "só um agente no canal"}
	}
	// Codex only runs whole chat turns (no quick call), so the router reads
	// the owner's words by rules instead.
	if eng == nil || wholeTurns(eng) {
		return ruleRoute(team, history, msgs, s.now())
	}
	names := map[string]string{}
	for _, a := range room.all {
		names[a.ID] = a.Name
	}
	var who strings.Builder
	for _, a := range team {
		talk := map[string]string{"pouco": " Fala pouco no grupo.", "muito": " Gosta de participar do papo."}[a.Style.Talk]
		fmt.Fprintf(&who, "- %s (id %s): %s.%s %s\n", a.Name, a.ID, a.Role, talk, truncate(strings.ReplaceAll(a.Persona, "\n", " "), 220))
	}
	// The conversation before, newest last, without the messages decided.
	recent := history
	if n := len(recent) - len(msgs); n >= 0 {
		recent = recent[:n]
	}
	if len(recent) > 12 {
		recent = recent[len(recent)-12:]
	}
	var conv strings.Builder
	for _, m := range recent {
		author := ownerName
		switch m.Role {
		case "system":
			continue
		case "assistant":
			author = firstNonEmptyStr(names[m.AgentID], "Ex-funcionário")
		}
		if strings.TrimSpace(m.Content) != "" {
			fmt.Fprintf(&conv, "%s: %s\n", author, truncate(strings.TrimSpace(m.Content), 400))
		}
	}
	system := `Você decide quem responde a uma mensagem do dono num canal de conversa com agentes (como um grupo de WhatsApp do trabalho). Responda só com JSON: {"agentes": ["id", ...], "reacao": "", "motivo": "uma frase"}.
Regras:
- Um pedido ou pergunta de trabalho: um agente só, o que a mensagem pede pela especialidade, pelo nome ou pela função, ou quem estava conversando com o dono sobre esse assunto agora.
- Uma mensagem para o canal todo ("fala time", "bom dia, pessoal", "como vocês estão?", "galera, o que acham?", "alguém sabe...?", "o pessoal não vai responder?"): de 2 a 4 agentes, como num grupo de verdade, onde nem todo mundo responde sempre. Ponha primeiro quem tem mais a ver; quem gosta de participar entra mais, quem fala pouco raramente.
- Mensagem que não pede resposta (um "valeu", "ok", "beleza", "kkkk", uma despedida): nenhum agente ("agentes": []) e um emoji em "reacao" (👍, ❤️, 😂, 🙌), que quem conversava com o dono põe na mensagem.
- Mensagens seguidas do dono são uma conversa só: decida por todas juntas.
- Quando não dá para saber de quem é, o chefe de gabinete (id ` + chief(team) + `).
- Use só ids da lista.`
	prompt := "Canal #" + room.ch.Name
	if room.ch.Purpose != "" {
		prompt += " (" + truncate(room.ch.Purpose, 300) + ")"
	}
	prompt += "\n\nQuem pode responder:\n" + who.String() + "\nConversa antes (mais recente por último):\n" + firstNonEmptyStr(conv.String(), "(nada)\n") +
		"\n<mensagem>\n" + strings.Join(msgs, "\n") + "\n</mensagem>"
	resp, err := eng.Reply(ctx, system, []store.ChatMessage{{Role: "user", Content: prompt}}, nil)
	if err != nil {
		d := ruleRoute(team, history, msgs, s.now())
		d.why = "roteador falhou (" + truncate(err.Error(), 80) + "), pelas regras: " + d.why
		return d
	}
	var out struct {
		Agentes []string `json:"agentes"`
		Reacao  string   `json:"reacao"`
		Motivo  string   `json:"motivo"`
	}
	if raw := routerJSON.FindString(resp.Content); raw == "" || json.Unmarshal([]byte(raw), &out) != nil {
		d := ruleRoute(team, history, msgs, s.now())
		d.why = "roteador não respondeu em JSON, pelas regras: " + d.why
		return d
	}
	picked := []string{}
	for _, id := range out.Agentes {
		id = strings.ToLower(strings.TrimSpace(strings.TrimPrefix(id, "@")))
		for _, a := range team {
			if (id == a.ID || id == slug(a.Name)) && !contains(picked, a.ID) {
				picked = append(picked, a.ID)
			}
		}
	}
	if len(picked) == 0 {
		// Nobody, on purpose: a message that needs no answer gets a reaction.
		if emoji, ok := onlyEmoji(out.Reacao); ok && len(out.Agentes) == 0 {
			return route{reaction: emoji, why: firstNonEmptyStr(out.Motivo, "só uma reação")}
		}
		return route{agents: []string{chief(team)}, why: firstNonEmptyStr(out.Motivo, "ninguém escolhido")}
	}
	if len(picked) > routerMaxAgents {
		picked = picked[:routerMaxAgents]
	}
	// Who follows the first changes from one message to the next, as in a
	// real group.
	if len(picked) > 2 {
		rest := picked[1:]
		rand.Shuffle(len(rest), func(i, j int) { rest[i], rest[j] = rest[j], rest[i] })
	}
	return route{agents: picked, why: truncate(out.Motivo, 200)}
}

// recordRoute keeps the decision in the log (and tells the app at once).
func (s *Server) recordRoute(ch string, picked []string, why string) {
	payload, _ := json.Marshal(map[string]any{"channel_id": ch, "agents": picked, "reason": why})
	if err := s.store.RecordEvent(context.Background(), store.Event{At: s.now(), Type: EventChannelRouted, Actor: store.ActorSystem, Payload: payload}); err != nil {
		log.Printf("#%s: recording the route failed: %v", ch, err)
	}
}

// Channel events the app follows live.
const (
	EventChannelRouted  = "channel.routed"
	EventChannelWorking = "channel.working"
	EventChannelPassed  = "channel.passed"
)

func (s *Server) channelEvent(typ, ch, agentID string, extra map[string]any) {
	p := map[string]any{"channel_id": ch, "agent_id": agentID}
	for k, v := range extra {
		p[k] = v
	}
	payload, _ := json.Marshal(p)
	_ = s.store.RecordEvent(context.Background(), store.Event{At: s.now(), Type: typ, Actor: "agent:" + agentID, Payload: payload})
}

// callRouted calls an agent the router picked.
func (s *Server) callRouted(ch, agentID string, mayPass bool) {
	s.channel.mu.Lock()
	start := s.claimLocked(ch, agentID, ownerName)
	if run := s.runsLocked(ch)[agentID]; run != nil && start != nil {
		if mayPass {
			run.quiet = "routed"
		} else {
			run.forOwner = true
		}
	}
	s.channel.mu.Unlock()
	if start != nil {
		start()
	}
}

// quietRule is what a brought-in agent reads: silence (or just an emoji) is
// fine. talk is how much the agent likes to join in (AgentStyle.Talk).
func quietRule(byAgent bool, talk string) string {
	if byAgent {
		return "\n\nUm colega te chamou. Faça o que ele passou; se a mensagem for só um agradecimento, um ok ou uma despedida, reaja com um emoji ou responda exatamente " + quietMark + " e nada mais."
	}
	rule := "\n\nNinguém te chamou pelo nome: a mensagem é para o canal, e você entra se tiver algo seu a dizer. Num cumprimento, cumprimente do seu jeito, curto. Se você só concorda com o que um colega disse, reaja com um emoji. Se não tiver nada a acrescentar, ou um colega já disse o mesmo, responda exatamente " + quietMark + " e nada mais: ficar quieto no grupo é normal."
	switch talk {
	case "pouco":
		rule += " Você fala pouco no grupo: na dúvida, fique quieto ou só reaja."
	case "muito":
		rule += " Você gosta de participar: entre na conversa sempre que tiver algo, mesmo que pequeno."
	}
	return rule
}

// routerStagger is the pause between agents answering the same message in
// turn, so each reads what the one before said (as people do in a group).
var routerStagger = func() time.Duration { return time.Duration(700+rand.IntN(1500)) * time.Millisecond }

// routerWait bounds how long the next agent waits for the one before.
var routerWait = 45 * time.Second

// callInTurn calls the agents picked for one message one after another:
// each starts after the previous one finished (or passed), and a moment.
func (s *Server) callInTurn(ch string, picked []string) {
	if len(picked) == 0 {
		return
	}
	s.callRouted(ch, picked[0], false)
	if len(picked) == 1 {
		return
	}
	s.router.mu.Lock()
	r := s.roomLocked(ch)
	r.next = append(r.next, picked[1:]...)
	s.router.mu.Unlock()
	go func() {
		prev := picked[0]
		for _, id := range picked[1:] {
			// Wait for the one before (up to routerWait), then for the
			// colleagues it called (D57), up to four times that in all.
			start := time.Now()
			for time.Since(start) < 4*routerWait {
				if s.workingIn(ch, prev) {
					if time.Since(start) > routerWait {
						break
					}
				} else if !s.channelBusy(ch) {
					break
				}
				time.Sleep(50 * time.Millisecond)
			}
			time.Sleep(routerStagger())
			// Called by name meanwhile: that call answered it (D57).
			s.router.mu.Lock()
			r := s.roomLocked(ch)
			still := false
			for i, x := range r.next {
				if x == id {
					r.next = append(r.next[:i], r.next[i+1:]...)
					still = true
					break
				}
			}
			s.router.mu.Unlock()
			if !still {
				continue
			}
			s.callRouted(ch, id, true)
			prev = id
		}
	}()
}

// ackReaction has the agent the owner last talked to react to a message
// that needs no answer ("valeu", "ok"), without a model call of its own.
func (s *Server) ackReaction(ctx context.Context, ch string, history []store.ChatMessage, emoji string) {
	if emoji == "" {
		emoji = "👍"
	}
	for i := len(history) - 1; i >= 0; i-- {
		if m := history[i]; m.Role == "assistant" && m.AgentID != "" {
			_ = s.reactInChannel(ctx, ch, m.AgentID, nil, emoji)
			s.channelEvent(EventChannelPassed, ch, m.AgentID, map[string]any{"by": ownerName, "reaction": emoji})
			return
		}
	}
}

// passed says whether an answer is the agent staying quiet.
func passed(content string) bool {
	c := strings.ToLower(strings.TrimSpace(content))
	return c == quietMark || c == strings.Trim(quietMark, "[]") || strings.HasPrefix(c, quietMark)
}

func firstNonEmptyStr(s ...string) string {
	for _, x := range s {
		if strings.TrimSpace(x) != "" {
			return x
		}
	}
	return ""
}
