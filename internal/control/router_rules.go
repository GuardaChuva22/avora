package control

import (
	"math/rand/v2"
	"strings"
	"time"
	"unicode"

	"github.com/GuardaChuva22/avora/internal/store"
)

// ruleRoute decides who answers in #time without a model (D52): when the
// main model is a Codex account (it only runs whole chat turns, no quick
// calls) or the router call fails. It reads the owner's words:
//   - only an ack ("valeu", "ok", "kkkk", an emoji): nobody, a reaction;
//   - names an agent: that agent (and the others named);
//   - speaks to the team or greets ("olá time", "bom dia", "pessoal"): 2 to 4
//     agents, whoever was talking first, the rest drawn by how much each
//     likes to talk;
//   - anything else: whoever the owner was talking to, or the chief.
func ruleRoute(team []store.Agent, history []store.ChatMessage, msgs []string, now time.Time) route {
	text := strings.Join(msgs, "\n")
	words := ruleWords(text)
	if len(team) == 0 {
		return route{}
	}
	if len(team) == 1 {
		return route{agents: []string{team[0].ID}, why: "só um agente"}
	}

	if emoji, ok := ackOnly(text, words); ok {
		return route{reaction: emoji, why: "não pede resposta"}
	}

	named := []string{}
	for _, a := range team {
		for _, w := range ruleWords(a.Name) {
			if contains(words, w) && !contains(named, a.ID) {
				named = append(named, a.ID)
			}
		}
	}
	if len(named) > 0 {
		if len(named) > routerMaxAgents {
			named = named[:routerMaxAgents]
		}
		return route{agents: named, why: "chamou pelo nome"}
	}

	last := lastSpeaker(team, history, now)
	if toTeam(words) {
		first := firstNonEmptyStr(last, chief(team))
		n := 2 + rand.IntN(2) // 2 or 3, sometimes 4 when the team is big
		if len(team) >= 5 && rand.IntN(4) == 0 {
			n = 4
		}
		if n > len(team) {
			n = len(team)
		}
		picked := []string{first}
		pool := []store.Agent{}
		for _, a := range team {
			if a.ID != first {
				pool = append(pool, a)
			}
		}
		for len(picked) < n && len(pool) > 0 {
			i := drawByTalk(pool)
			picked = append(picked, pool[i].ID)
			pool = append(pool[:i], pool[i+1:]...)
		}
		return route{agents: picked, why: "mensagem para o time"}
	}
	if last != "" {
		return route{agents: []string{last}, why: "continua a conversa"}
	}
	return route{agents: []string{chief(team)}, why: "sem dono claro"}
}

// ruleWords is the text folded (lowercase, no accents) and split in words.
func ruleWords(s string) []string {
	return strings.FieldsFunc(foldText(s), func(r rune) bool { return !unicode.IsLetter(r) && !unicode.IsDigit(r) })
}

var (
	teamWords  = []string{"time", "pessoal", "galera", "voces", "vcs", "todos", "todas", "equipe", "turma", "povo", "alguem", "geral", "gente"}
	greetWords = []string{"oi", "ola", "oie", "eai", "eae", "salve", "opa", "fala", "bom", "boa", "hello", "hey", "coe"}
	ackWords   = map[string]string{
		"valeu": "🙌", "vlw": "🙌", "obrigado": "❤️", "obrigada": "❤️", "obg": "❤️", "brigado": "❤️",
		"ok": "👍", "okay": "👍", "blz": "👍", "beleza": "👍", "show": "👍", "top": "🔥", "massa": "🔥",
		"fechou": "👍", "fechado": "👍", "perfeito": "👍", "certo": "👍", "boa": "👍", "tmj": "🙌",
		"kkk": "😂", "kkkk": "😂", "kkkkk": "😂", "haha": "😂", "hahaha": "😂", "rs": "😂", "rsrs": "😂",
		"tchau": "👋", "falou": "👋", "flw": "👋", "ate": "👋", "mais": "", "amanha": "", "logo": "",
		"entendi": "👍", "demais": "🔥", "otimo": "👍", "legal": "👍", "dahora": "🔥",
	}
)

// toTeam says the owner speaks to the whole group or just greets it.
func toTeam(words []string) bool {
	for _, w := range words {
		if contains(teamWords, w) {
			return true
		}
	}
	// A short greeting to nobody in particular ("olá!", "bom dia") is for
	// everyone, as in a group.
	if len(words) > 0 && len(words) <= 4 && contains(greetWords, words[0]) {
		return true
	}
	return false
}

// ackOnly says a message needs no answer, and the emoji that fits.
func ackOnly(text string, words []string) (string, bool) {
	if e, ok := onlyEmoji(text); ok {
		return e, true
	}
	if len(words) == 0 || len(words) > 4 || strings.Contains(text, "?") {
		return "", false
	}
	emoji := ""
	for _, w := range words {
		e, ok := ackWords[w]
		if !ok && strings.HasPrefix(w, "kkk") {
			e, ok = "😂", true
		}
		if !ok {
			return "", false
		}
		if emoji == "" {
			emoji = e
		}
	}
	return firstNonEmptyStr(emoji, "👍"), true
}

// lastSpeaker is the agent who spoke last in the channel, when it was
// recent enough to still be the conversation.
func lastSpeaker(team []store.Agent, history []store.ChatMessage, now time.Time) string {
	for i := len(history) - 1; i >= 0; i-- {
		m := history[i]
		if m.Role != "assistant" || m.AgentID == "" {
			continue
		}
		if now.Sub(m.CreatedAt) > 30*time.Minute {
			return ""
		}
		for _, a := range team {
			if a.ID == m.AgentID {
				return a.ID
			}
		}
		return ""
	}
	return ""
}

// drawByTalk picks one agent, those who like to talk more often.
func drawByTalk(pool []store.Agent) int {
	weight := func(a store.Agent) int {
		switch a.Style.Talk {
		case "muito":
			return 4
		case "pouco":
			return 1
		}
		return 2
	}
	total := 0
	for _, a := range pool {
		total += weight(a)
	}
	r := rand.IntN(total)
	for i, a := range pool {
		if r -= weight(a); r < 0 {
			return i
		}
	}
	return len(pool) - 1
}
