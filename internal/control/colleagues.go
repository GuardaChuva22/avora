package control

import (
	"context"
	"encoding/hex"
	"errors"
	"fmt"
	"strings"

	"github.com/GuardaChuva22/avora/internal/store"
)

// Notes between colleagues (D48). In a direct conversation an agent can
// pass work to a colleague without waiting (avisar_colega): the colleague
// gets a conversation of its own, started by the note, and works there at
// once, answering the owner as in any conversation of hers; the owner sees
// it in the colleague's list, working live. The agent that passed the note
// gets a card that opens that conversation. A conversation started by a
// note cannot pass notes in turn, so two agents never bounce work around.

// notePrefix starts the first message of a conversation opened by a note.
const notePrefix = "📨"

// noteContext is how many recent messages the colleague reads.
const noteContext = 10

func (s *Server) note(ctx context.Context, t *turn, a toolArgs) (string, error) {
	col, err := s.who(ctx, t, a.Funcionario)
	if err != nil {
		return "", err
	}
	if col.ID == t.agent.ID || strings.TrimSpace(a.Funcionario) == "" {
		return "", errors.New("diga para qual colega (não você mesmo)")
	}
	msg := strings.TrimSpace(a.Recado)
	if msg == "" {
		return "", errors.New("diga o recado")
	}
	if c, err := s.store.GetConversation(ctx, t.conv); err == nil && strings.HasPrefix(c.Title, notePrefix) {
		return "", errors.New("esta conversa já veio de um recado: faça você mesmo ou use consultar_colega")
	}

	raw, err := randomBytes(12)
	if err != nil {
		return "", err
	}
	model := ""
	if eng := s.chatEngine(); eng != nil {
		model = eng.Model()
	}
	title := notePrefix + " " + truncate(strings.Join(strings.Fields(firstLine(msg)), " "), 60)
	conv, err := s.store.CreateConversation(ctx, store.Conversation{
		ID: hex.EncodeToString(raw), AgentID: col.ID, Title: title, Engine: "native", Model: model, CreatedAt: s.now(),
	}, "agent:"+t.agent.ID)
	if err != nil {
		return "", err
	}
	all, _ := s.store.ListAgents(ctx)
	history, _ := s.store.Messages(ctx, t.conv)
	content := notePrefix + " Recado de " + t.agent.Name + "\n" + msg
	if _, err := s.startTurn(ctx, newID(), conv, col, content, 0, noteContextBlock(t.agent, history, all), "agent:"+t.agent.ID, nil); err != nil {
		return "", err
	}
	s.step(t, AgentStep{Tool: "avisar_colega", Summary: col.Name + ": " + msg, Detail: col.ID + "/" + conv.ID})
	return fmt.Sprintf("Recado passado: %s já está trabalhando nisso numa conversa própria («%s»), e responde ao dono lá. O dono abre essa conversa pelo cartão do recado. Diga isso em uma frase e não faça o trabalho dela.", col.Name, strings.TrimPrefix(title, notePrefix+" ")), nil
}

func firstLine(s string) string {
	if i := strings.IndexByte(s, '\n'); i >= 0 {
		return s[:i]
	}
	return s
}

// noteContextBlock is what the colleague reads besides the note (the model
// only): the conversation it came from and how to answer.
func noteContextBlock(from store.Agent, history []store.ChatMessage, all []store.Agent) string {
	names := map[string]string{}
	for _, a := range all {
		names[a.ID] = a.Name
	}
	if len(history) > noteContext {
		history = history[len(history)-noteContext:]
	}
	var conv strings.Builder
	for _, m := range history {
		if strings.TrimSpace(m.Content) == "" || m.Role == "system" {
			continue
		}
		who := ownerName
		if m.Role == "assistant" {
			who = firstNonEmptyStr(names[m.AgentID], from.Name)
		}
		fmt.Fprintf(&conv, "%s: %s\n", who, truncate(strings.TrimSpace(m.Content), 1200))
	}
	return fmt.Sprintf("--- De onde veio: a conversa do dono com %s, mais recente por último ---\n%s---\n"+
		"%s, seu colega no time, passou isto para você. Esta conversa é sua com o dono: faça o trabalho e responda a ele aqui, direto, com o resultado.",
		from.Name, firstNonEmptyStr(conv.String(), "(nada)\n"), from.Name)
}

// colleaguesSigned marks, for the conversation's own agent, the answers a
// colleague wrote in it, so it does not take them for its own.
func colleaguesSigned(history []store.ChatMessage, agent store.Agent, all []store.Agent) []store.ChatMessage {
	names := map[string]string{}
	for _, a := range all {
		names[a.ID] = a.Name
	}
	out := make([]store.ChatMessage, len(history))
	copy(out, history)
	for i, m := range out {
		if m.Role == "assistant" && m.AgentID != "" && m.AgentID != agent.ID {
			out[i].Content = "(Mensagem de " + firstNonEmptyStr(names[m.AgentID], "um ex-colega") + ", colega do time, nesta conversa)\n" + m.Content
		}
	}
	return out
}
