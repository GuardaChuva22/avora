package control

import (
	"context"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/GuardaChuva22/avora/internal/llm"
	"github.com/GuardaChuva22/avora/internal/store"
)

// StepTellAgent marks a non-blocking message sent to another agent.
const StepTellAgent = "tell_agent"

type tellToolArgs struct {
	Agent       string `json:"agent"`
	Funcionario string `json:"funcionario"`
	Alvo        string `json:"alvo"`
	Message     string `json:"message"`
	Recado      string `json:"recado"`
	Mensagem    string `json:"mensagem"`
}

func tellAgentTool() llm.Tool {
	return tool("tell_agent",
		"Manda um recado ou passa uma tarefa para outro agente sem esperar resposta (aparece no chat dele como 'Recado de <você>'). Quando a tarefa passada terminar, o resultado volta automaticamente para esta conversa.",
		`{"type": "object", "properties": {
	"agent": {"type": "string", "description": "Nome ou id do colega destinatário."},
	"message": {"type": "string", "description": "O recado ou a tarefa completa, com todo o contexto necessário."}
}, "required": ["agent", "message"]}`)
}

func (s *Server) toolTellAgent(ctx context.Context, t *turn, a toolArgs) (string, error) {
	var args tellToolArgs
	if err := json.Unmarshal([]byte(a.raw), &args); err != nil {
		return "", err
	}
	targetRef := strings.TrimSpace(pickStr(args.Agent, args.Funcionario, args.Alvo))
	if targetRef == "" {
		return "", errors.New("diga para qual colega mandar o recado em agent")
	}
	col, err := s.who(ctx, t, targetRef)
	if err != nil {
		return "", err
	}
	if col.ID == t.agent.ID {
		return "", errors.New("diga para qual colega (não você mesmo)")
	}
	msg := strings.TrimSpace(pickStr(args.Message, args.Recado, args.Mensagem))
	if msg == "" {
		return "", errors.New("diga a mensagem em message")
	}
	if c, err := s.store.GetConversation(ctx, t.conv); err == nil && strings.HasPrefix(c.Title, notePrefix) {
		return "", errors.New("esta conversa já veio de um recado: conclua aqui sem repassar outro recado")
	}

	raw, err := randomBytes(12)
	if err != nil {
		return "", err
	}
	model := ""
	if eng := s.chatEngine(); eng != nil {
		model = eng.Model()
	}
	wsID := col.WorkspaceID
	if wsID == "" {
		wsID = "pessoal"
	}
	shortTitle := truncate(strings.Join(strings.Fields(firstLine(msg)), " "), 52)
	title := fmt.Sprintf("%s Recado de %s: %s", notePrefix, t.agent.Name, shortTitle)
	conv, err := s.store.CreateConversation(store.WithWorkspace(ctx, wsID), store.Conversation{
		ID:          hex.EncodeToString(raw),
		WorkspaceID: wsID,
		AgentID:     col.ID,
		Title:       title,
		Engine:      "native",
		Model:       model,
		CreatedAt:   s.now(),
	}, "agent:"+t.agent.ID)
	if err != nil {
		return "", err
	}

	content := notePrefix + " Recado de " + t.agent.Name + "\n" + msg
	fromConv := t.conv
	fromAgent := t.agent

	if eng, _ := s.brainFor(ctx, col); eng == nil {
		if _, err := s.store.AppendMessage(ctx, store.ChatMessage{
			ConversationID: conv.ID,
			Role:           "user",
			Content:        content,
			CreatedAt:      s.now(),
		}, "agent:"+t.agent.ID); err != nil {
			return "", err
		}
	} else {
		all, _ := s.store.ListAgents(store.WithWorkspace(ctx, wsID))
		var history []store.ChatMessage
		if fromConv != "" {
			history, _ = s.store.Messages(ctx, fromConv)
		}
		extra := tellContextBlock(fromAgent, history, all)
		_, err := s.startTurn(context.WithoutCancel(ctx), newID(), conv, col, content, 0, extra, "agent:"+t.agent.ID, func(answer store.ChatMessage) {
			if fromConv == "" {
				return
			}
			go s.deliverTellResult(fromConv, col, answer)
		})
		if err != nil {
			return "", err
		}
	}

	s.step(t, AgentStep{
		Tool:    StepTellAgent,
		Summary: fmt.Sprintf("Recado de %s para %s: %s", t.agent.Name, col.Name, truncate(msg, 120)),
		Detail:  col.ID + "/" + conv.ID,
	})
	return fmt.Sprintf("Recado enviado para %s («Recado de %s»); %s já recebeu e, quando terminar, o resultado volta para esta conversa.", col.Name, t.agent.Name, col.Name), nil
}

func tellContextBlock(from store.Agent, history []store.ChatMessage, all []store.Agent) string {
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
			if n := names[m.AgentID]; n != "" {
				who = n
			} else {
				who = from.Name
			}
		}
		fmt.Fprintf(&conv, "%s: %s\n", who, truncate(strings.TrimSpace(m.Content), 1000))
	}
	ctxText := conv.String()
	if ctxText == "" {
		ctxText = "(sem histórico anterior)\n"
	}
	return fmt.Sprintf("--- Contexto da conversa de %s ---\n%s---\n"+
		"%s te mandou este recado. Se for apenas um aviso, registre/confirme de forma objetiva; se for uma tarefa, execute-a e escreva o resultado claro: a sua resposta será entregue de volta para %s.",
		from.Name, ctxText, from.Name, from.Name)
}

func (s *Server) deliverTellResult(fromConvID string, col store.Agent, answer store.ChatMessage) {
	bctx := context.Background()
	body := strings.TrimSpace(answer.Content)
	if body == "" && answer.Error != "" {
		body = "Não consegui concluir: " + answer.Error
	}
	if body == "" {
		return
	}
	// Wait briefly if the requester's turn is still wrapping up so the return lands right after it.
	deadline := time.Now().Add(5 * time.Second)
	for time.Now().Before(deadline) {
		fc, err := s.store.GetConversation(bctx, fromConvID)
		if err != nil {
			return
		}
		if fc.JobID == "" {
			break
		}
		time.Sleep(15 * time.Millisecond)
	}
	_, _ = s.store.AppendMessage(bctx, store.ChatMessage{
		ConversationID: fromConvID,
		Role:           "assistant",
		AgentID:        col.ID,
		Content:        fmt.Sprintf("%s Retorno de %s:\n%s", notePrefix, col.Name, body),
		Model:          answer.Model,
		CreatedAt:      s.now(),
	}, "agent:"+col.ID)
}

// ── Autonomy brakes for the scheduler ────────────────────────────

// agentActiveCount counts how many turns or channel runs an agent currently has in flight.
func (s *Server) agentActiveCount(ctx context.Context, agentID string) int {
	n := 0
	if convs, err := s.store.ListConversations(ctx, agentID, 200); err == nil {
		for _, c := range convs {
			if c.AgentID == agentID && c.JobID != "" {
				n++
			}
		}
	}
	if s.workingInChannel(agentID) {
		n++
	}
	return n
}

// schedulerAllows enforces agents.autonomy (can_start_alone and max_parallel 1..5).
// Manual runs triggered explicitly by the owner bypass can_start_alone, while still respecting max_parallel.
func (s *Server) schedulerAllows(ctx context.Context, agent store.Agent, manual bool) (bool, string) {
	aut := store.NormalizeAutonomy(agent.Autonomy)
	if !manual && !aut.AllowsStartAlone() {
		return false, "autonomia: este funcionário não inicia tarefas sozinho"
	}
	if s.agentActiveCount(ctx, agent.ID) >= aut.LimitParallel() {
		return false, fmt.Sprintf("autonomia: limite de %d execuções em paralelo atingido", aut.LimitParallel())
	}
	return true, ""
}
