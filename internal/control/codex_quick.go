package control

import (
	"context"
	"errors"
	"strings"
	"time"

	"github.com/GuardaChuva22/avora/internal/store"
)

// quickReply is one question to the main model with no tools and no chat:
// summarizing a conversation, the goal overseer. A native model answers it
// directly; a Codex account (no quick API, D53) runs it as a short job on
// its node, in the nested slot so a turn waiting for it never blocks it,
// with no bridge (no Avora tools) and a read-only sandbox.
func (s *Server) quickReply(ctx context.Context, system, question, reason string) (string, error) {
	eng := s.chatEngine()
	if eng == nil {
		return "", errors.New("nenhuma IA conectada")
	}
	if ae, ok := eng.(*agyEngine); ok {
		return s.agyQuick(ctx, ae, system, question, reason)
	}
	e, codex := eng.(*codexEngine)
	if !codex {
		resp, err := eng.Reply(ctx, system, []store.ChatMessage{{Role: "user", Content: question, CreatedAt: s.now()}}, nil)
		return resp.Content, err
	}
	n, account, err := s.codexTarget(ctx, e.provider, true)
	if err != nil {
		return "", err
	}
	minutes := 5
	if d, ok := ctx.Deadline(); ok {
		minutes = max(1, int(time.Until(d).Minutes())+1)
	}
	prompt := "Siga as instruções abaixo e responda só o que elas pedem, sem usar ferramentas nem ler arquivos.\n\n<instrucoes>\n" +
		system + "\n</instrucoes>\n\n" + truncate(question, maxCodexPrompt-len(system)-512)
	spec := &store.AgentSpec{Engine: "codex", Model: e.model, Mode: store.CodexConsultMode, MaxMinutes: minutes,
		Account: account, Workdir: "avora-quick", Prompt: prompt}
	end, err := s.runNodeJobAs(ctx, nil, spec, n.Kind, n.ID, reason, store.ActorSystem, "")
	if err != nil {
		return "", err
	}
	switch r := end.result; {
	case r != nil && r.Success:
		return strings.TrimSpace(r.Output), nil
	case r != nil:
		return "", publicError{codexProblem(r.Error)}
	case ctx.Err() != nil:
		return "", ctx.Err()
	default:
		return "", errors.New("o Codex não respondeu")
	}
}
