package control

import (
	"strings"
	"testing"

	"github.com/GuardaChuva22/avora/internal/store"
)

func agentLines(msgs []store.ChatMessage) []string {
	out := []string{}
	for _, m := range msgs {
		if m.Role == "assistant" {
			out = append(out, m.AgentID)
		}
	}
	return out
}

// Colleagues called together answer one by one, each reading the one before (D57).
func TestColleaguesCalledTogetherAnswerInOrder(t *testing.T) {
	engine := &teamEngine{lines: map[string]string{
		"Nico":  "@Iris, puxa três tendências. @Atlas e @Cora, depois comentem.",
		"Iris":  "Tendência um: comunidades pequenas.",
		"Atlas": "Comunidade dá pra testar com pouco código.",
		"Cora":  "Fecha na conta se o piloto custar pouco.",
	}}
	s := chatServer(t, engine)
	request(t, s, "POST", "/v1/conversations/time/messages", adminTestToken, MessageRequest{Content: "@Nico puxa o assunto de marketing"})
	settle(t, s)
	got := strings.Join(agentLines(channelMessages(t, s)), ",")
	if got != "nico,iris,atlas,cora,nico" {
		t.Fatalf("order: %s", got)
	}
	if !strings.Contains(engine.prompts["Atlas"], "comunidades pequenas") {
		t.Fatalf("Atlas did not read Iris: %q", engine.prompts["Atlas"])
	}
	if !strings.Contains(engine.prompts["Cora"], "pouco código") {
		t.Fatalf("Cora did not read Atlas: %q", engine.prompts["Cora"])
	}
	// Then the work comes back to Nico, who does not call them again.
	if !strings.Contains(engine.prompts["Nico"], "Iris e Atlas e Cora já responderam") {
		t.Fatalf("Nico's hand-back: %q", engine.prompts["Nico"])
	}
}

// "Conversem entre si" keeps the team talking, one at a time, until the
// last answer wraps up for the owner (D57).
func TestDiscussionKeepsGoing(t *testing.T) {
	engine := &teamEngine{lines: map[string]string{
		"router": `{"agentes": ["nico", "iris", "vega"], "motivo": "para o time"}`,
		"Vega":   "Eu foco no que dá retorno rápido.",
		"Atlas":  "Automação com IA é barata de testar.",
		"Iris":   "Comunidades de nicho estão crescendo.",
		"Cora":   "Só se o custo for baixo.",
		"Nico":   "Criadores ligados a vendas funcionam.",
	}}
	s := chatServer(t, engine)
	request(t, s, "POST", "/v1/conversations/time/messages", adminTestToken, MessageRequest{Content: "pessoal, conversem entre si sobre as tendências de marketing"})
	settle(t, s)
	lines := agentLines(channelMessages(t, s))
	if len(lines) != discussTurns || lines[0] != "nico" {
		t.Fatalf("discussion: %v", lines)
	}
	for i := 1; i < len(lines); i++ {
		if lines[i] == lines[i-1] {
			t.Fatalf("someone answered themselves: %v", lines)
		}
	}
	spoke := map[string]bool{}
	for _, id := range lines {
		spoke[id] = true
	}
	if len(spoke) != 5 {
		t.Fatalf("not everyone took part: %v", lines)
	}
	closing := false
	for _, p := range engine.prompts {
		closing = closing || strings.Contains(p, "Você fecha a discussão")
	}
	if !closing {
		t.Fatal("nobody wrapped up")
	}
	if s.discussing(TeamChannelID) {
		t.Fatal("the discussion did not end")
	}
	for _, m := range channelMessages(t, s) {
		if m.Role == "system" {
			t.Fatalf("lock notice in a discussion: %q", m.Content)
		}
	}
}

// Two passes in a row end a discussion.
func TestDiscussionEndsOnPasses(t *testing.T) {
	engine := &teamEngine{lines: map[string]string{
		"router": `{"agentes": ["nico"], "motivo": "para o time"}`,
		"Nico":   "Bora falar de preço.",
		"Vega":   quietMark, "Atlas": quietMark, "Iris": quietMark, "Cora": quietMark,
	}}
	s := chatServer(t, engine)
	request(t, s, "POST", "/v1/conversations/time/messages", adminTestToken, MessageRequest{Content: "time, debatam o preço novo"})
	settle(t, s)
	if lines := agentLines(channelMessages(t, s)); len(lines) != 1 {
		t.Fatalf("after passes: %v", lines)
	}
	if s.discussing(TeamChannelID) {
		t.Fatal("still discussing")
	}
}

func TestWantsDiscussion(t *testing.T) {
	for _, yes := range []string{"pessoal conversem entre si ai", "discutam isso", "quero que vocês debatam", "conversa entre vocês sobre X", "troquem ideias"} {
		if !wantsDiscussion(yes) {
			t.Errorf("missed %q", yes)
		}
	}
	for _, no := range []string{"vamos conversar amanhã", "olá time", "discutimos ontem"} {
		if wantsDiscussion(no) {
			t.Errorf("false hit %q", no)
		}
	}
}

// "Continuem" after the team talked picks the talk up again, among the ones
// who were talking, instead of a lone reaction.
func TestContinueResumesTheTalk(t *testing.T) {
	engine := &teamEngine{lines: map[string]string{
		"Nico":  "@Iris, puxa as tendências. @Cora, comenta o custo.",
		"Iris":  "Comunidades de nicho.",
		"Cora":  "Cabe no orçamento.",
		"Atlas": "Dá pra automatizar.",
		"Vega":  "👍",
	}}
	s := chatServer(t, engine)
	request(t, s, "POST", "/v1/conversations/time/messages", adminTestToken, MessageRequest{Content: "@Nico abre o papo de marketing"})
	settle(t, s)
	before := len(agentLines(channelMessages(t, s)))
	if before != 4 {
		t.Fatalf("first talk: %v", agentLines(channelMessages(t, s)))
	}
	w := request(t, s, "POST", "/v1/conversations/time/messages", adminTestToken, MessageRequest{Content: "continuem trabalhando ai"})
	if !strings.Contains(w.Body.String(), `"discussing":true`) {
		t.Fatalf("not resumed: %s", w.Body)
	}
	settle(t, s)
	msgs := channelMessages(t, s)
	after := agentLines(msgs)[before:]
	if len(after) != discussTurns {
		t.Fatalf("resumed talk: %v", after)
	}
	for _, id := range after {
		if id != "nico" && id != "iris" && id != "cora" {
			t.Fatalf("%s joined a talk it was not in: %v", id, after)
		}
	}
	// The one who picks it up reads that the owner asked to go on.
	d := &discussion{topic: "x", resumed: true}
	if !strings.Contains(discussPrompt(d, 0), "pediu para continuarem") || strings.Contains(discussPrompt(d, 1), "pediu para continuarem") {
		t.Fatal("resumed prompt")
	}
}

// The agent the router picks for the owner's message answers in words.
func TestRoutedAgentAnswersInWords(t *testing.T) {
	engine := &teamEngine{lines: map[string]string{"router": `{"agentes": ["cora"], "motivo": "finanças"}`, "Cora": "Sobra pouco."}}
	s := chatServer(t, engine)
	request(t, s, "POST", "/v1/conversations/time/messages", adminTestToken, MessageRequest{Content: "como tá o caixa"})
	settle(t, s)
	if !strings.Contains(engine.prompts["Cora"], "responda com texto, não só com um emoji") {
		t.Fatalf("Cora's prompt: %q", engine.prompts["Cora"])
	}
}

// An agent that promises and stops is called back once to do it (D59).
func TestPromiseIsKept(t *testing.T) {
	engine := &teamEngine{lines: map[string]string{
		"router": `{"agentes": ["iris"], "motivo": "pesquisa"}`,
		"Iris":   "Boa, vou pesquisar isso e já te falo.",
	}}
	s := chatServer(t, engine)
	request(t, s, "POST", "/v1/conversations/time/messages", adminTestToken, MessageRequest{Content: "quais as tendências?"})
	settle(t, s)
	if lines := agentLines(channelMessages(t, s)); strings.Join(lines, ",") != "iris,iris" {
		t.Fatalf("promise: %v", lines)
	}
	if !strings.Contains(engine.prompts["Iris"], "e parou. Não prometa: faça agora") {
		t.Fatalf("Iris's nudge: %q", engine.prompts["Iris"])
	}
	if got := promised("Ok. Já estou olhando a viabilidade; quando a Iris trouxer, comento."); got == "" {
		t.Fatal("missed a promise")
	}
	if got := promised("Sobram R$ 800 no mês."); got != "" {
		t.Fatalf("false promise: %q", got)
	}
}

func TestResumeSkipsAnAnswerThatWasOnlyAReaction(t *testing.T) {
	h := []store.ChatMessage{
		{Role: "user", Content: "conversem sobre marketing"},
		{Role: "assistant", AgentID: "nico", Content: "a"},
		{Role: "assistant", AgentID: "iris", Content: "b"},
		{Role: "user", Content: "continuem trabalhando ai"},
		{Role: "user", Content: "continuem pesquisando ai pesosal"},
	}
	topic, people := resumeTalk(h)
	if topic != "conversem sobre marketing" || strings.Join(people, ",") != "iris,nico" {
		t.Fatalf("resume: %q %v", topic, people)
	}
}
