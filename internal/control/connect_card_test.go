package control

import (
	"strings"
	"testing"

	"github.com/GuardaChuva22/avora/internal/connect"
	"github.com/GuardaChuva22/avora/internal/llm"
	"github.com/GuardaChuva22/avora/internal/vault"
)

func TestConnectCard(t *testing.T) {
	e := &toolEngine{calls: map[string][]llm.ToolCall{}, answers: map[string]string{}}
	s := chatServer(t, e)
	s.SetWhatsApp(&fakeWA{}) // linked
	c := agentConversation(t, s, "nico")

	setCalls(e, "Nico", call(connectTool, map[string]any{"app": "whatsapp"}), call(connectTool, map[string]any{"app": "app que não existe"}))
	r := send(t, s, c.ID, "manda pro cliente no whats")
	if strings.Contains(string(r.Assistant.Steps), `"summary":"whatsapp"`) {
		t.Fatalf("WhatsApp is linked, no card: %s", r.Assistant.Steps)
	}
	if !strings.Contains(r.Assistant.Content, "já está conectado") || !strings.Contains(r.Assistant.Content, "instalar_mcp") {
		t.Fatalf("answer: %q", r.Assistant.Content)
	}
	// Gmail is not connected: the card shows (the step is the card).
	s.SetConnectors(connect.NewRegistry(&connect.Google{ClientID: "cid", ClientSecret: "cs"}, &connect.Discord{}), vault.Random())
	setCalls(e, "Nico", call(connectTool, map[string]any{"app": "Gmail"}))
	r = send(t, s, c.ID, "lê meus e-mails")
	if !strings.Contains(string(r.Assistant.Steps), `"tool":"pedir_conexao","summary":"google"`) {
		t.Fatalf("no card: %s", r.Assistant.Steps)
	}
	if kind, _, ok := s.connectorFor("whatsapp"); !ok || kind != "whatsapp" {
		t.Fatalf("connectorFor: %q %v", kind, ok)
	}
}
