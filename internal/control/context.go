package control

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"log"
	"net/http"
	"strings"
	"time"
	"unicode/utf8"

	"github.com/GuardaChuva22/avora/internal/store"
)

// Long conversations. Every turn sends the agent's instructions, its tools
// and the conversation; past CompactAtTokens the oldest messages are
// summarized and leave the model's context (the owner still sees them).
// The summary goes into the instructions; the newest compactKeep messages
// stay as they are. The app shows how full the context is and can compact
// on demand.

const (
	ContextWindow   = 128000
	CompactAtTokens = 64000
	compactKeep     = 12
	maxSummaryChars = 7000
	compactChunk    = 180000 // characters of old messages per summarizing call
	compactRounds   = 6
	// codexCompactAt is the room for messages when Codex is the main model
	// (about half of maxCodexPrompt).
	codexCompactAt = 14000
)

// estTokens guesses the tokens of a text (about 3.6 characters each in
// Portuguese with code and JSON mixed in).
func estTokens(s string) int {
	return (utf8.RuneCountInString(s)*10 + 35) / 36
}

// turnBlocks are what runTurn adds to an agent's instructions.
func (s *Server) turnBlocks(ctx context.Context, a store.Agent) string {
	return s.ownerBlock(ctx, a) + s.projectBlock(ctx, a) + s.orgBlock(ctx, a) + s.skillsBlock(ctx, a) + s.docsBlock(ctx, a) + s.missionsBlock(ctx, a) + s.goalsBlock(ctx, a) + s.connectionsBlock(ctx, a.ID) + s.memoryBlock(ctx, a.ID)
}

func summaryBlock(sum store.ConversationSummary) string {
	if sum.Summary == "" {
		return ""
	}
	return "\n\nResumo da conversa até aqui (as mensagens mais antigas saíram do seu contexto; o dono ainda as vê):\n" + sum.Summary
}

// compacted drops the messages a summary stands for.
func compacted(history []store.ChatMessage, sum store.ConversationSummary) []store.ChatMessage {
	if sum.Upto == 0 {
		return history
	}
	out := []store.ChatMessage{}
	for _, m := range history {
		if m.ID > sum.Upto {
			out = append(out, m)
		}
	}
	if len(out) == 0 && len(history) > 0 {
		out = history[len(history)-1:]
	}
	return out
}

func messageTokens(list []store.ChatMessage) int {
	n := 0
	for _, m := range list {
		n += estTokens(m.Content) + 4
	}
	return n
}

// agentContextWindow returns the effective token window for agent a, capped at
// what the configured model accepts (defaulting to ContextWindow when unset).
func (s *Server) agentContextWindow(ctx context.Context, a store.Agent) int {
	if a.Brain.Context <= 0 {
		return ContextWindow
	}
	if maxWin := s.maxBrainContext(ctx, a.Brain); maxWin > 0 && a.Brain.Context > maxWin {
		return maxWin
	}
	return a.Brain.Context
}

// trimHistoryToWindow drops the oldest messages when history exceeds maxTokens,
// always keeping at least the newest message.
func trimHistoryToWindow(history []store.ChatMessage, maxTokens int) []store.ChatMessage {
	if len(history) <= 1 || maxTokens <= 0 || messageTokens(history) <= maxTokens {
		return history
	}
	kept := 0
	start := len(history) - 1
	for i := len(history) - 1; i >= 0; i-- {
		cost := estTokens(history[i].Content) + 4
		if kept+cost > maxTokens && i < len(history)-1 {
			break
		}
		kept += cost
		start = i
	}
	return history[start:]
}

// ContextView is how full a conversation's context is, for the app.
type ContextView struct {
	Window     int                        `json:"window"`
	Used       int                        `json:"used"`
	CompactAt  int                        `json:"compact_at"`
	Parts      []ContextPart              `json:"parts"`
	Messages   int                        `json:"messages"` // in the context
	Hidden     int                        `json:"hidden"`   // summarized away
	Summary    *store.ConversationSummary `json:"summary,omitempty"`
	CanCompact bool                       `json:"can_compact"`
	Compacting bool                       `json:"compacting"`
	Note       string                     `json:"note,omitempty"`
}

type ContextPart struct {
	Name   string `json:"name"`
	Tokens int    `json:"tokens"`
}

func (s *Server) contextOf(ctx context.Context, conv store.Conversation) (ContextView, error) {
	v := ContextView{Window: ContextWindow, CompactAt: CompactAtTokens, Parts: []ContextPart{}}
	_, v.Compacting = s.compacting.Load(conv.ID)
	history, err := s.store.Messages(ctx, conv.ID)
	if err != nil {
		return v, err
	}
	sum, _ := s.store.GetSummary(ctx, conv.ID)
	kept := compacted(history, sum)
	v.Messages, v.Hidden = len(kept), len(history)-len(kept)
	if sum.Summary != "" {
		v.Summary = &sum
	}
	agent, err := s.store.GetAgent(ctx, conv.AgentID)
	if err != nil {
		return v, err
	}
	v.Window = s.agentContextWindow(ctx, agent)
	v.CompactAt = v.Window / 2
	all, _ := s.store.ListAgents(ctx)
	team := visibleTeam(agent, all)
	tools, _ := json.Marshal(s.toolsFor(&turn{agent: agent, team: team, conv: conv.ID}))
	v.Parts = append(v.Parts,
		ContextPart{"Instruções e memória", estTokens(nativeSystem(agent, team) + s.turnBlocks(ctx, agent))},
		ContextPart{"Ferramentas", estTokens(string(tools))},
		ContextPart{"Resumo", estTokens(summaryBlock(sum))},
		ContextPart{"Mensagens", messageTokens(kept)})
	for _, p := range v.Parts {
		v.Used += p.Tokens
	}
	// Codex/Antigravity read the conversation inside one prompt; scale room when
	// the agent picked a larger context window.
	eng, _ := s.brainFor(ctx, agent)
	if eng == nil {
		eng = s.chatEngine()
	}
	if wholeTurns(eng) {
		room := codexCompactAt
		if agent.Brain.Context > 0 {
			room = max(codexCompactAt, codexCompactAt*v.Window/200000)
		}
		v.CompactAt = min(v.Window/2, v.Used-messageTokens(kept)+room)
	}
	switch {
	case len(kept) <= compactKeep:
		v.Note = fmt.Sprintf("Compacta sozinho perto de %dk; as %d mensagens mais novas ficam sempre inteiras.", v.CompactAt/1000, compactKeep)
	default:
		v.CanCompact = true
	}
	return v, nil
}

func roleName(m store.ChatMessage, agent string) string {
	switch {
	case m.Role == "assistant":
		return agent
	case strings.HasPrefix(m.Content, missionPrefix), strings.HasPrefix(m.Content, routinePrefix):
		return "Avora"
	case m.Role == "user":
		return "Dono"
	}
	return "Sistema"
}

const compactSystem = `Você resume conversas entre o dono e um funcionário digital (um agente de IA), para o agente continuar o trabalho sem as mensagens antigas.
Escreva em português do Brasil, em tópicos curtos, nesta ordem:
- Quem é o dono e o que ele quer nesta conversa.
- Fatos, decisões e preferências do dono.
- O que já foi feito, com os nomes, números, ids, caminhos, datas e links exatos.
- O que está em aberto ou combinado para depois.
Junte o resumo anterior (se houver) com as mensagens novas num resumo só. Não invente nada. No máximo 900 palavras.`

// compact summarizes the oldest messages of a conversation. Without force it
// does so only when the context passed CompactAtTokens.
func (s *Server) compact(ctx context.Context, convID string, force bool) error {
	if _, busy := s.compacting.LoadOrStore(convID, true); busy {
		return errors.New("a conversa já está sendo compactada")
	}
	defer s.compacting.Delete(convID)
	return s.compactLocked(ctx, convID, force)
}

func (s *Server) compactLocked(ctx context.Context, convID string, force bool) error {
	eng := s.chatEngine()
	if eng == nil {
		return errors.New("nenhuma IA conectada")
	}
	// One summarizing call reads up to chunk characters of old messages;
	// a Codex job gets one prompt, so less.
	chunk := compactChunk
	if wholeTurns(eng) {
		chunk = maxCodexPrompt * 2 / 3
	}
	conv, err := s.store.GetConversation(ctx, convID)
	if err != nil {
		return err
	}
	agentName := conv.AgentID
	if a, err := s.store.GetAgent(ctx, conv.AgentID); err == nil {
		agentName = a.Name
	}
	for round := 0; round < compactRounds; round++ {
		v, err := s.contextOf(ctx, conv)
		if err != nil {
			return err
		}
		if !force && round == 0 && v.Used < v.CompactAt {
			return nil
		}
		// After a round, stop once the messages use half of their room.
		msgs := v.Parts[len(v.Parts)-1].Tokens
		if round > 0 && msgs < (v.CompactAt-(v.Used-msgs))/2 {
			return nil
		}
		history, err := s.store.Messages(ctx, convID)
		if err != nil {
			return err
		}
		sum, _ := s.store.GetSummary(ctx, convID)
		kept := compacted(history, sum)
		if len(kept) <= compactKeep {
			if force && round == 0 {
				return errors.New("a conversa ainda é curta: as mensagens mais novas ficam sempre inteiras")
			}
			return nil
		}
		old := kept[:len(kept)-compactKeep]
		var b strings.Builder
		var upto int64
		for _, m := range old {
			if b.Len() > chunk {
				break
			}
			upto = m.ID
			text := strings.TrimSpace(m.Content)
			if text == "" {
				continue
			}
			fmt.Fprintf(&b, "%s: %s\n\n", roleName(m, agentName), truncate(text, 4000))
		}
		input := "Mensagens:\n\n" + b.String()
		if sum.Summary != "" {
			input = "Resumo anterior:\n" + sum.Summary + "\n\n" + input
		}
		rctx, cancel := context.WithTimeout(ctx, 4*time.Minute)
		answer, err := s.quickReply(rctx, compactSystem, input, "Resumo de uma conversa longa")
		cancel()
		if err != nil {
			return fmt.Errorf("o modelo não conseguiu resumir: %w", err)
		}
		text := truncate(strings.TrimSpace(answer), maxSummaryChars)
		if text == "" {
			return errors.New("o modelo devolveu um resumo vazio")
		}
		if _, err := s.store.SaveSummary(ctx, store.ConversationSummary{ConversationID: convID, Summary: text, Upto: upto,
			Count: sum.Count + 1}, store.ActorSystem, s.now()); err != nil {
			return err
		}
	}
	return nil
}

// maybeCompact compacts a conversation in the background after a turn.
func (s *Server) maybeCompact(convID string) {
	ctx, cancel := context.WithTimeout(context.Background(), 15*time.Minute)
	defer cancel()
	if err := s.compact(ctx, convID, false); err != nil && !strings.Contains(err.Error(), "Codex") &&
		!strings.Contains(err.Error(), "já está") {
		log.Printf("conversation %s: compacting failed: %v", convID, err)
	}
}

// ── API ─────────────────────────────────────────────────────────

func (s *Server) registerContext(mux *http.ServeMux) {
	mux.HandleFunc("GET /v1/conversations/{id}/context", s.admin(s.getContext))
	mux.HandleFunc("POST /v1/conversations/{id}/compact", s.admin(s.compactAPI))
}

func (s *Server) getContext(w http.ResponseWriter, r *http.Request) {
	conv, err := s.store.GetConversation(r.Context(), r.PathValue("id"))
	if err != nil {
		storeError(w, err)
		return
	}
	v, err := s.contextOf(r.Context(), conv)
	if err != nil {
		storeError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, v)
}

// compactAPI starts compacting now; the app follows it through the context.
func (s *Server) compactAPI(w http.ResponseWriter, r *http.Request) {
	conv, err := s.store.GetConversation(r.Context(), r.PathValue("id"))
	if err != nil {
		storeError(w, err)
		return
	}
	v, err := s.contextOf(r.Context(), conv)
	if err != nil {
		storeError(w, err)
		return
	}
	if !v.CanCompact {
		http.Error(w, v.Note, http.StatusConflict)
		return
	}
	if _, busy := s.compacting.LoadOrStore(conv.ID, true); busy {
		http.Error(w, "already compacting", http.StatusConflict)
		return
	}
	go func() {
		defer s.compacting.Delete(conv.ID)
		ctx, cancel := context.WithTimeout(context.Background(), 15*time.Minute)
		defer cancel()
		if err := s.compactLocked(ctx, conv.ID, true); err != nil {
			log.Printf("conversation %s: compacting on request failed: %v", conv.ID, err)
		}
	}()
	v.Compacting = true
	writeJSON(w, http.StatusAccepted, v)
}
