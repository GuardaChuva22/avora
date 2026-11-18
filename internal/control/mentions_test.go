package control

import (
	"strings"
	"testing"

	"github.com/GuardaChuva22/avora/internal/llm"
	"github.com/GuardaChuva22/avora/internal/store"
)

func TestMentioned(t *testing.T) {
	list := []Mention{{Name: "Google", Kind: "app"}, {Name: "Google Drive", Kind: "app"}, {Name: "Relatório do mês", Kind: "skill"}, {Name: "Navegador", Kind: "tool"}}
	got := mentioned("faz o @relatorio do mês com os dados do @Google Drive, sem @navegadores", list)
	names := []string{}
	for _, m := range got {
		names = append(names, m.Name)
	}
	if strings.Join(names, "|") != "Relatório do mês|Google Drive" {
		t.Fatalf("mentioned: %v", names)
	}
	if len(mentioned("mande para ana@exemplo.com", list)) != 0 {
		t.Fatal("an e-mail address is not a mention")
	}
}

func TestMentionBlock(t *testing.T) {
	e := &toolEngine{calls: map[string][]llm.ToolCall{}, answers: map[string]string{}}
	s := chatServer(t, e)
	s.SetWhatsApp(&fakeWA{})
	if _, err := s.store.SaveSkill(t.Context(), store.Skill{ID: "proposta", Name: "Proposta", Description: "monta proposta",
		Body: "1. Leia o briefing.\n2. Use o modelo da muun."}, store.ActorAdmin, s.now()); err != nil {
		t.Fatal(err)
	}
	agent := store.Agent{ID: "nico"}
	history := []store.ChatMessage{{Role: "user", Content: "@Proposta para a clínica, e responde no @whatsapp"}}
	block := s.mentionBlock(t.Context(), agent, history)
	if !strings.Contains(block, "Use o modelo da muun") || !strings.Contains(block, "whatsapp_enviar") || !strings.Contains(block, "não use o navegador") {
		t.Fatalf("block: %s", block)
	}
	if s.mentionBlock(t.Context(), agent, []store.ChatMessage{{Role: "user", Content: "oi"}}) != "" {
		t.Fatal("no mention, no block")
	}
}
