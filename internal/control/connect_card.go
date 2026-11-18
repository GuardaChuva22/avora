package control

import (
	"context"
	"encoding/json"
	"errors"
	"strings"

	"github.com/GuardaChuva22/avora/internal/connect"
	"github.com/GuardaChuva22/avora/internal/llm"
)

// Connect from the chat (D37): when a request needs an app that is not
// connected, the agent calls pedir_conexao and the conversation shows a
// card with the app's logo and a Conectar button (one click for a login,
// the token box for the others). Once the owner connects, the app says so
// in the chat and the agent carries on.

const connectTool = "pedir_conexao"

func (s *Server) connectTools() []llm.Tool {
	if s.connectors == nil && s.wa == nil {
		return nil
	}
	return []llm.Tool{tool(connectTool, "Quando um pedido precisa de um app que não está conectado (Gmail, Instagram, WhatsApp, ClickUp...), mostra ao dono um cartão para conectar ali mesmo na conversa. Depois de chamar, diga em uma frase para que vai usar e pare: o dono avisa quando conectar.",
		`{"type": "object", "properties": {
	"app": {"type": "string", "description": "O nome do app, como aparece em Plugins (ex.: Gmail, Instagram, WhatsApp)."}
}, "required": ["app"]}`)}
}

// connectorFor finds a Plugins app by its name, one of its apps or its kind.
func (s *Server) connectorFor(name string) (kind, label string, ok bool) {
	want := appKey(foldText(name))
	if want == "" {
		return "", "", false
	}
	if want == "whatsapp" && s.wa != nil {
		return "whatsapp", "WhatsApp", true
	}
	if s.connectors == nil {
		return "", "", false
	}
	for _, con := range s.connectors.All() {
		info := con.Info()
		if appKey(info.Kind) == want || appKey(foldText(info.Name)) == want {
			return info.Kind, info.Name, true
		}
		for _, app := range info.Apps {
			if appKey(foldText(app)) == want {
				return info.Kind, info.Name, true
			}
		}
	}
	return "", "", false
}

func (s *Server) runConnectTool(ctx context.Context, t *turn, name, raw string) (string, error, bool) {
	if name != connectTool {
		return "", nil, false
	}
	var a struct {
		App string `json:"app"`
	}
	_ = json.Unmarshal([]byte(orDefault(raw, "{}")), &a)
	kind, label, ok := s.connectorFor(a.App)
	if !ok {
		return "", errors.New("não há app " + strings.TrimSpace(a.App) + " em Plugins; para outro serviço com servidor MCP, use instalar_mcp"), true
	}
	if via := connect.Backed[kind]; via != "" {
		// Shopee and the like connect through their Apify account.
		if c, ok := s.connectors.Get(via); ok {
			label += " (pelo " + c.Info().Name + ")"
		}
		kind = via
	}
	if kind == "whatsapp" {
		if s.whatsappReady() {
			return "O WhatsApp já está conectado: use as ferramentas whatsapp_*.", nil, true
		}
	} else {
		conns, _ := s.store.ListConnections(ctx)
		_, by := accountsOf(conns)
		if len(usableOnly(by[kind], t.agent.ID)) > 0 {
			return "O " + label + " já está conectado para você: use as ferramentas dele.", nil, true
		}
		if len(by[kind]) > 0 && by[kind][0].Status == "reconnect" {
			label += " (reconectar)"
		}
	}
	// The step is the card: the app draws it from the tool and the kind.
	s.step(t, AgentStep{Tool: connectTool, Summary: kind})
	return "Cartão para conectar o " + label + " mostrado ao dono na conversa. Diga em uma frase para que vai usar e espere ele avisar que conectou.", nil, true
}
