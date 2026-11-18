package control

import (
	"context"
	"fmt"
	"regexp"
	"strings"

	"github.com/GuardaChuva22/avora/internal/store"
)

// Talking among themselves in a channel (D57). Two things keep a group
// conversation going like people do:
//
//   - Several colleagues called at once (@Iris … @Atlas e @Cora) answer one
//     after another, in the order they were named, each reading what the
//     one before said. Before, they all started together and the later ones
//     answered "when Iris brings it…" without having read Iris.
//   - When the owner asks the team to talk among themselves ("conversem",
//     "discutam", "debatam"…), the channel holds a discussion: after each
//     answer that calls nobody, the member who spoke least takes the next
//     turn, until discussTurns answers (the last one wraps up for the
//     owner), two passes in a row, or the owner writes again.

// discussTurns is how many answers a discussion runs; the last one closes.
const discussTurns = 10

var discussPattern = regexp.MustCompile(`(?i)(^|[^\p{L}])(conversem|conversa(r|em)? entre (si|voc[eê]s)|falem entre (si|voc[eê]s)|discutam|discutir entre|discuss[ãa]o entre|debatam|debater entre|debate entre|troquem (uma )?ideias?|trocar ideias? entre|batam um papo|bat[ae]r um papo entre|brainstorm)([^\p{L}]|$)`)

// morePattern is the owner asking the team to keep going ("continuem",
// "podem seguir").
var morePattern = regexp.MustCompile(`(?i)(^|[^\p{L}])(continuem|continuar|continua[ií]|sigam|prossigam|podem seguir|segue[mn]? a[ií]|bora continuar|n[ãa]o parem)([^\p{L}]|$)`)

// resumeTalk finds what to resume when the owner says "continuem": the
// owner's message before this one and the agents who talked after it, when
// at least two did. history ends with the new message.
func resumeTalk(history []store.ChatMessage) (string, []string) {
	// Owner messages nobody answered in words ("continuem" that only got a
	// reaction) are skipped, up to a few back.
	people, asks := []string{}, 0
	for i := len(history) - 2; i >= 0 && asks < 4; i-- {
		m := history[i]
		switch {
		case m.Role == "assistant" && m.AgentID != "" && m.Content != "" && !contains(people, m.AgentID):
			people = append(people, m.AgentID)
		case m.Role == "user":
			asks++
			if len(people) >= 2 {
				return m.Content, people
			}
			if len(people) == 1 {
				return "", nil
			}
		}
	}
	return "", nil
}

// wantsDiscussion says whether the owner asks the team to talk among
// themselves.
func wantsDiscussion(text string) bool { return discussPattern.MatchString(text) }

// discussion is one discussion in a channel; s.channel.mu guards it.
type discussion struct {
	topic string
	// resumed: the owner asked them to go on with an earlier talk.
	resumed bool
	people []string // who takes part (agent ids)
	passes int      // passes in a row
}

// queuedCall is a colleague's call waiting for the one before to answer.
type queuedCall struct {
	id, by string
}

func (s *Server) discussLocked(ch string) *discussion {
	return s.channel.discuss[ch]
}

// startDiscussion opens a discussion among people (the ones the owner named,
// or the channel's members that answer on their own).
func (s *Server) startDiscussion(ch, topic string, people []string, resumed bool) {
	s.channel.mu.Lock()
	defer s.channel.mu.Unlock()
	if s.channel.discuss == nil {
		s.channel.discuss = map[string]*discussion{}
	}
	s.channel.discuss[ch] = &discussion{topic: truncate(strings.TrimSpace(topic), 600), people: people, resumed: resumed}
}

// endDiscussion closes the channel's discussion, if any.
func (s *Server) endDiscussion(ch string) {
	s.channel.mu.Lock()
	defer s.channel.mu.Unlock()
	delete(s.channel.discuss, ch)
}

func (s *Server) discussing(ch string) bool {
	s.channel.mu.Lock()
	defer s.channel.mu.Unlock()
	return s.channel.discuss[ch] != nil
}

// discussPrompt is what an agent reads on its turn in a discussion; done is
// how many answers the team gave since the owner wrote.
func discussPrompt(d *discussion, done int) string {
	head := "O dono pediu que o time converse entre si sobre isto: «" + d.topic + "»."
	switch {
	case done+1 >= discussTurns:
		return "\n\n" + head + " Você fecha a discussão: em três a cinco frases, diga ao dono o que o time concluiu, onde discordou e o próximo passo que sugere. Não chame ninguém."
	case done == 0 && d.resumed:
		return "\n\n" + head + " Vocês já começaram e o dono pediu para continuarem. Retome de onde pararam: aprofunde um ponto, discorde de algo ou proponha um próximo passo concreto (sem repetir o que já foi dito) e passe a palavra a um colega com @Nome."
	case done == 0:
		return "\n\n" + head + " Você abre a conversa: dê a sua visão com um ponto concreto (um exemplo, um número, uma aposta) e termine passando a palavra a um colega com @Nome."
	}
	return "\n\n" + head + " É a sua vez. Responda ao que o colega anterior disse: concorde ou discorde com um porquê, e traga algo novo (um exemplo, um risco, um número, uma ideia prática). Duas a quatro frases, como conversa. Pode terminar com uma pergunta a um colega pelo @Nome. Não diga que vai comentar depois nem que espera alguém trazer algo: fale agora com o que já tem. Se não tiver nada novo a dizer, responda exatamente " + quietMark + "."
}

// nextSpeaker picks who talks next in a discussion: someone other than the
// last to speak, not busy, who spoke least since the owner wrote (and, on a
// tie, longest ago).
func nextSpeaker(d *discussion, history []store.ChatMessage, last string, busy map[string]*channelRun) string {
	count, at := map[string]int{}, map[string]int{}
	for i := len(history) - 1; i >= 0 && history[i].Role != "user"; i-- {
		if m := history[i]; m.Role == "assistant" && m.AgentID != "" {
			count[m.AgentID]++
			if _, ok := at[m.AgentID]; !ok {
				at[m.AgentID] = i
			}
		}
	}
	best := ""
	for _, id := range d.people {
		if id == last || busy[id] != nil {
			continue
		}
		if best == "" || count[id] < count[best] || count[id] == count[best] && at[id] < at[best] {
			best = id
		}
	}
	return best
}

// nextCallsLocked books who answers after agentID in a channel, in this
// order: the agent itself again when it promised work and stopped (ADR 0097
// of agentes-chat: it does it now, once); the colleagues it called (the first
// now, the rest queued in order); the next queued call; the agent that
// handed work out, once everyone it called answered (ADR 0095: hand-offs
// come back); the discussion's next speaker. promise is what it promised
// (""= nothing); passed says the answer was a pass. It returns what starts
// the calls. s.channel.mu must be held.
func (s *Server) nextCallsLocked(ch, agentID, name string, called []string, again, promise string, passed bool, history []store.ChatMessage) []func() {
	runs := s.runsLocked(ch)
	if s.channel.queue == nil {
		s.channel.queue = map[string][]queuedCall{}
	}
	if s.channel.back == nil {
		s.channel.back = map[string][]handBack{}
	}
	d := s.channel.discuss[ch]
	done := streak(history)
	if d != nil {
		switch {
		case done >= discussTurns:
			// It closed: the wrap-up is the last word.
			delete(s.channel.discuss, ch)
			delete(s.channel.queue, ch)
			delete(s.channel.back, ch)
			return nil
		case passed:
			if d.passes++; d.passes >= 2 {
				delete(s.channel.discuss, ch)
				d = nil
			}
		default:
			d.passes = 0
		}
	}
	calls := []channelCall{}
	if again != "" {
		calls = append(calls, channelCall{id: agentID, by: again})
	}
	switch {
	case promise != "":
		calls = append(calls, channelCall{id: agentID, by: name, nudged: true, extra: promiseNudge(promise)})
		// Whoever it called still answers after it, and it hears back.
		for _, id := range called {
			s.channel.queue[ch] = append(s.channel.queue[ch], queuedCall{id, name})
		}
		if len(called) > 0 {
			s.channel.back[ch] = append(s.channel.back[ch], handBack{asker: agentID, ids: called, names: s.namesLocked(ch, called)})
		}
	case len(called) > 0:
		calls = append(calls, channelCall{id: called[0], by: name, quiet: "agent"})
		for _, id := range called[1:] {
			s.channel.queue[ch] = append(s.channel.queue[ch], queuedCall{id, name})
		}
		s.channel.back[ch] = append(s.channel.back[ch], handBack{asker: agentID, ids: called, names: s.namesLocked(ch, called)})
	case len(runs) == 0 && len(s.channel.queue[ch]) > 0:
		q := s.channel.queue[ch][0]
		s.channel.queue[ch] = s.channel.queue[ch][1:]
		calls = append(calls, channelCall{id: q.id, by: q.by, quiet: "agent"})
	case len(runs) == 0 && len(s.channel.back[ch]) > 0:
		backs := s.channel.back[ch]
		b := backs[len(backs)-1]
		s.channel.back[ch] = backs[:len(backs)-1]
		if b.asker != agentID {
			calls = append(calls, channelCall{id: b.asker, by: strings.Join(b.names, " e "), quiet: "back", extra: handBackPrompt(b.names), heard: b.ids})
		}
	case len(runs) == 0 && d != nil:
		if id := nextSpeaker(d, history, agentID, runs); id != "" {
			calls = append(calls, channelCall{id: id, by: name, quiet: "discuss"})
		}
	}
	starts := []func(){}
	for _, c := range calls {
		if done+len(runs) >= ChannelLoopLimit {
			if !lockNoticed(history) {
				_, _ = s.store.AppendMessage(context.Background(), store.ChatMessage{
					ConversationID: ch, Role: "system", CreatedAt: s.now(),
					Content: fmt.Sprintf(lockPrefix+": o time trabalhou %d respostas seguidas sem você. Escreva \"continuem\" para deixar seguir.", ChannelLoopLimit),
				}, store.ActorSystem)
			}
			delete(s.channel.queue, ch)
			delete(s.channel.back, ch)
			break
		}
		s.dropFromRouter(ch, c.id)
		if start := s.claimLocked(ch, c.id, c.by); start != nil {
			run := runs[c.id]
			run.quiet, run.nudged, run.heard = c.quiet, c.nudged, c.heard
			if c.extra != "" {
				run.extra = c.extra
			}
			starts = append(starts, start)
		}
	}
	return starts
}

// channelCall is one answer to book in a channel.
type channelCall struct {
	id, by, quiet, extra string
	nudged               bool
	heard                []string
}

// handBack is an agent waiting for the colleagues it called (D59).
type handBack struct {
	asker string
	ids   []string
	names []string
}

func (s *Server) namesLocked(ch string, ids []string) []string {
	out := []string{}
	for _, id := range ids {
		if a, err := s.store.GetAgent(context.Background(), id); err == nil {
			out = append(out, a.Name)
		} else {
			out = append(out, id)
		}
	}
	return out
}

func handBackPrompt(names []string) string {
	return "\n\n" + strings.Join(names, " e ") + " já responderam o que você pediu (acima). Continue o trabalho: junte o que trouxeram, decida, faça a próxima parte com as suas ferramentas, passe o próximo passo a alguém com @Nome ou feche para o dono com o resultado. Não agradeça nem repita o que disseram, e não peça de novo a eles o que já responderam. Se não houver mais nada a fazer, responda exatamente " + quietMark + "."
}

// promisePattern finds an agent saying it will do something later instead
// of doing it ("vou pesquisar", "já volto", "quando a Iris trouxer…").
var promisePattern = regexp.MustCompile(`(?i)(^|[^\p{L}])(vou (pesquisar|ver|olhar|checar|verificar|analisar|levantar|buscar|montar|preparar|fazer|calcular|investigar|procurar|escrever|criar|organizar|revisar|testar|mapear|comparar)|já volto|j[áa] te (falo|aviso|trago|retorno)|te (aviso|retorno|trago) (quando|assim)|assim que (eu )?(tiver|terminar|souber)|quando (a|o) [\p{L}]+ (trouxer|mandar|terminar|responder)|estou (olhando|vendo|pesquisando|levantando|analisando)|já estou (olhando|vendo|pesquisando|levantando|analisando))([^\p{L}]|$)`)

// promised is the promise in an answer, or "".
func promised(content string) string {
	loc := promisePattern.FindStringIndex(content)
	if loc == nil {
		return ""
	}
	// The sentence around it.
	start := strings.LastIndexAny(content[:loc[0]], ".!?\n") + 1
	end := len(content)
	if i := strings.IndexAny(content[loc[1]:], ".!?\n"); i >= 0 {
		end = loc[1] + i + 1
	}
	return truncate(strings.TrimSpace(content[start:end]), 300)
}

func promiseNudge(promise string) string {
	return "\n\nVocê escreveu no canal «" + promise + "» e parou. Não prometa: faça agora com as suas ferramentas (pesquisar, buscar na web, ler, calcular, criar) e conte no canal o que encontrou ou fez. Se depende de um colega, peça a ele com @Nome dizendo exatamente o quê. Se já está feito, responda exatamente " + quietMark + "."
}

// channelBusy says whether anyone is answering or waiting to in a channel.
func (s *Server) channelBusy(ch string) bool {
	s.channel.mu.Lock()
	defer s.channel.mu.Unlock()
	return len(s.runsLocked(ch)) > 0 || len(s.channel.queue[ch]) > 0
}

// dropFromRouter takes an agent out of the router's line: being called by
// name answers the same message.
func (s *Server) dropFromRouter(ch, id string) {
	s.router.mu.Lock()
	defer s.router.mu.Unlock()
	r := s.roomLocked(ch)
	for i, x := range r.next {
		if x == id {
			r.next = append(r.next[:i], r.next[i+1:]...)
			return
		}
	}
}

func (s *Server) discussionOf(ch string) *discussion {
	s.channel.mu.Lock()
	defer s.channel.mu.Unlock()
	return s.channel.discuss[ch]
}
