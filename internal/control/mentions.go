package control

import (
	"context"
	"net/http"
	"regexp"
	"sort"
	"strings"
	"unicode"

	"github.com/GuardaChuva22/avora/internal/store"
)

// Mentions (D35): the owner types @ in the chat and picks an app, a skill
// or a built-in tool; the turn then tells the agent to use exactly that
// (the app's tools, the skill's steps) instead of guessing or going
// through the browser.

// Mention is something the owner can mark with @.
type Mention struct {
	Name  string   `json:"name"`
	Kind  string   `json:"kind"` // app, skill, tool
	Hint  string   `json:"hint"`
	Icon  string   `json:"icon,omitempty"` // connector kind, for the logo
	Tools []string `json:"-"`
	Skill string   `json:"-"` // skill id
}

var builtinMentions = []Mention{
	{Name: "Navegador", Kind: "tool", Hint: "o navegador dentro do app, logado nos seus sites", Tools: []string{"navegador"}},
	{Name: "Pesquisa", Kind: "tool", Hint: "pesquisa a fundo na web (Codex com busca ao vivo)", Tools: []string{"pesquisar"}},
	{Name: "Web", Kind: "tool", Hint: "busca rápida na web", Tools: []string{"buscar_na_web", "ler_pagina"}},
}

// appKey turns an app name into its tools' prefix (Gmail → gmail_).
func appKey(name string) string {
	var b strings.Builder
	for _, r := range strings.ToLower(name) {
		if r < unicode.MaxASCII && (unicode.IsLetter(r) || unicode.IsDigit(r)) {
			b.WriteRune(r)
		}
	}
	return b.String()
}

// mentionables are what the agent can be pointed at: the apps connected
// for it, WhatsApp, its skills and the built-in tools.
func (s *Server) mentionables(ctx context.Context, a store.Agent) []Mention {
	out := []Mention{}
	if s.connectors != nil {
		conns, _ := s.store.ListConnections(ctx)
		_, by := accountsOf(conns)
		for _, con := range s.connectors.All() {
			info := con.Info()
			if len(usableOnly(by[info.Kind], a.ID)) == 0 {
				continue
			}
			names := []string{}
			for _, t := range con.Tools() {
				names = append(names, t.Name)
			}
			apps := info.Apps
			if len(apps) <= 1 {
				out = append(out, Mention{Name: info.Name, Kind: "app", Hint: info.Description, Icon: info.Kind, Tools: names})
				continue
			}
			// One connector, several apps (Google: Gmail, Drive, Agenda).
			for _, app := range apps {
				own := []string{}
				for _, n := range names {
					if strings.HasPrefix(n, appKey(app)+"_") {
						own = append(own, n)
					}
				}
				if len(own) == 0 {
					own = names
				}
				out = append(out, Mention{Name: app, Kind: "app", Hint: info.Name, Icon: info.Kind, Tools: own})
			}
		}
	}
	if s.whatsappReady() {
		out = append(out, Mention{Name: "WhatsApp", Kind: "app", Hint: "suas conversas do WhatsApp", Icon: "whatsapp",
			Tools: []string{"whatsapp_conversas", "whatsapp_ler", "whatsapp_midia", "whatsapp_enviar"}})
	}
	for _, sk := range s.visibleSkills(ctx, a) {
		out = append(out, Mention{Name: sk.Name, Kind: "skill", Hint: sk.Description, Skill: sk.ID})
	}
	out = append(out, builtinMentions...)
	return out
}

// mentioned finds @Name in text (case and accents ignored), the longest
// name first so "@Google Drive" wins over "@Google".
func mentioned(text string, list []Mention) []Mention {
	low := foldText(text)
	sorted := append([]Mention{}, list...)
	sort.SliceStable(sorted, func(i, j int) bool { return len(sorted[i].Name) > len(sorted[j].Name) })
	out := []Mention{}
	seen := map[string]bool{}
	for _, m := range sorted {
		key := m.Kind + ":" + m.Name
		if seen[key] {
			continue
		}
		re := regexp.MustCompile(`(^|[\s(])@` + regexp.QuoteMeta(foldText(m.Name)) + `($|[^\pL\pN_])`)
		if re.MatchString(low) {
			seen[key] = true
			out = append(out, m)
			// Taken: "@Google Drive" is not also "@Google".
			low = re.ReplaceAllString(low, "${1} ${2}")
		}
	}
	return out
}

var accentFold = strings.NewReplacer("á", "a", "à", "a", "â", "a", "ã", "a", "é", "e", "ê", "e", "í", "i", "ó", "o", "ô", "o", "õ", "o", "ú", "u", "ç", "c")

func foldText(s string) string { return accentFold.Replace(strings.ToLower(s)) }

// mentionBlock tells the agent what the owner marked in the message it is
// answering.
func (s *Server) mentionBlock(ctx context.Context, a store.Agent, history []store.ChatMessage) string {
	msg, ok := lastOwnerMessage(history)
	if !ok || !strings.Contains(msg.Content, "@") {
		return ""
	}
	marks := mentioned(msg.Content, s.mentionables(ctx, a))
	if len(marks) == 0 {
		return ""
	}
	var b strings.Builder
	b.WriteString("\n\nO dono marcou com @ o que quer que você use nesta mensagem; use exatamente isso:")
	for _, m := range marks {
		switch m.Kind {
		case "skill":
			if sk, err := s.store.GetSkill(ctx, m.Skill); err == nil {
				b.WriteString("\n- @" + m.Name + ": siga esta skill (já carregada, não precisa de skill_ler):\n<<<\n" + truncate(sk.Body, 12000) + "\n>>>")
			}
		case "app":
			b.WriteString("\n- @" + m.Name + ": use as ferramentas do conector (" + strings.Join(m.Tools, ", ") +
				"), com a conta do dono já conectada; não use o navegador nem a busca para isso.")
		default:
			b.WriteString("\n- @" + m.Name + ": use " + strings.Join(m.Tools, " / ") + ".")
		}
	}
	return b.String()
}

// listMentions is GET /v1/mentions?agent=: what the chat suggests after @.
func (s *Server) listMentions(w http.ResponseWriter, r *http.Request) {
	agent := store.Agent{ID: r.URL.Query().Get("agent")}
	if agent.ID != "" {
		if a, err := s.store.GetAgent(r.Context(), agent.ID); err == nil {
			agent = a
		}
	}
	writeJSON(w, http.StatusOK, s.mentionables(r.Context(), agent))
}
