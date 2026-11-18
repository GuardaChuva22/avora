package control

import (
	"bytes"
	"fmt"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/GuardaChuva22/avora/internal/store"
)

func claimCodex(t *testing.T, s *Server, token string) Job {
	t.Helper()
	for i := 0; i < 400; i++ {
		w := request(t, s, "POST", "/v1/runtime/claim", token, ClaimRequest{CodexMain: true, CodexNested: true})
		if w.Code == 200 {
			return decode[Job](t, w)
		}
		time.Sleep(5 * time.Millisecond)
	}
	t.Fatal("no codex job to claim")
	return Job{}
}

func TestCodexAsMainModel(t *testing.T) {
	s := modelsServer(t)
	pc := pairNode(t, s, "pc", "windows")
	now := s.now()
	beat := func() {
		request(t, s, "POST", "/v1/runtime/heartbeat", pc.Token, HeartbeatRequest{Quotas: []Quota{{Engine: "codex", Plan: "plus", CheckedAt: now}}})
	}
	beat()

	// The node's own Codex login shows up as a provider that can be picked.
	_, list := listModelsOf(t, s)
	var codex *ProviderView
	for i := range list {
		if list[i].ID == "codex@"+pc.NodeID {
			codex = &list[i]
		}
	}
	if codex == nil || !codex.CodexMain || !codex.Builtin || codex.Kind != "codex" {
		t.Fatalf("codex provider: %+v", list)
	}

	// Refreshing asks the node for the account's models.
	refreshed := make(chan ProviderView, 1)
	go func() {
		refreshed <- decode[ProviderView](t, request(t, s, "POST", "/v1/models/providers/codex@"+pc.NodeID+"/refresh", adminTestToken, nil))
	}()
	job := claimCodex(t, s, pc.Token)
	if job.Action != store.ActionCodexMain || job.Agent.Mode != store.CodexModelsMode || job.Agent.Account != "" {
		t.Fatalf("models job: %+v %+v", job, job.Agent)
	}
	request(t, s, "POST", "/v1/runtime/jobs/"+job.ID+"/complete", pc.Token,
		JobResult{Success: true, Output: `[{"id":"gpt-6.1-sol","name":"GPT-6.1-Sol"},{"id":"gpt-6-astra","name":"GPT-6-Astra"}]`})
	view := <-refreshed
	if len(view.Models) != 2 || view.ModelNames["gpt-6-astra"] != "GPT-6-Astra" || !view.CodexMain {
		t.Fatalf("refreshed: %+v", view)
	}

	if w := request(t, s, "PUT", "/v1/models/main", adminTestToken, MainModel{Provider: "codex@" + pc.NodeID, Model: "nope"}); w.Code != 400 {
		t.Fatalf("unknown model accepted: %d", w.Code)
	}
	if w := request(t, s, "PUT", "/v1/models/main", adminTestToken, MainModel{Provider: "codex@" + pc.NodeID, Model: "gpt-6-astra"}); w.Code != 200 {
		t.Fatalf("pick codex: %d %s", w.Code, w.Body)
	}
	if main, _ := listModelsOf(t, s); main.Model != "gpt-6-astra" || main.Provider != "codex@"+pc.NodeID {
		t.Fatalf("main: %+v", main)
	}

	// A chat turn runs on the node, with the agent's tools on the bridge.
	c := agentConversation(t, s, "atlas")
	answer := sendAsync(t, s, c.ID, "cria uma rotina pra mim")
	job = claimCodex(t, s, pc.Token)
	spec := job.Agent
	if spec.Mode != store.CodexChatMode || spec.Model != "gpt-6-astra" || spec.Bridge == "" || spec.Workdir != "agents/atlas" ||
		!strings.Contains(spec.Prompt, "cria uma rotina pra mim") || !strings.Contains(spec.Prompt, "servidor MCP \"avora\"") {
		t.Fatalf("turn job: %+v", spec)
	}
	code, out := rpc(t, s, spec.Bridge, 1, "tools/list", nil)
	if code != 200 || !strings.Contains(fmt.Sprint(out), "criar_rotina") {
		t.Fatalf("bridge tools: %d %v", code, out)
	}
	request(t, s, "POST", "/v1/runtime/jobs/"+job.ID+"/steps", pc.Token, AgentStep{Tool: "WebSearch", Summary: "query: x"})
	// An image Codex drew goes up while the job runs and comes back as a file.
	up := func(token string, body []byte) *httptest.ResponseRecorder {
		r := httptest.NewRequest("POST", "/v1/runtime/jobs/"+job.ID+"/files?name=exec-1.png", bytes.NewReader(body))
		r.Header.Set("Authorization", "Bearer "+token)
		w := httptest.NewRecorder()
		s.Handler().ServeHTTP(w, r)
		return w
	}
	png := append([]byte("\x89PNG\r\n\x1a\n"), make([]byte, 64)...)
	if w := up(pc.Token, []byte("not an image")); w.Code != 400 {
		t.Fatalf("text upload: %d", w.Code)
	}
	w := up(pc.Token, png)
	if w.Code != 201 {
		t.Fatalf("upload: %d %s", w.Code, w.Body.String())
	}
	fid := decode[map[string]string](t, w)["id"]
	if f, err := s.store.GetFile(t.Context(), fid); err != nil || f.Mime != "image/png" || f.AgentID != "atlas" || f.Name != "exec-1.png" {
		t.Fatalf("file: %+v %v", f, err)
	}
	request(t, s, "POST", "/v1/runtime/jobs/"+job.ID+"/complete", pc.Token,
		JobResult{Success: true, Output: "Pronto, criei.", Stats: []byte(`{"input_tokens":10,"output_tokens":3}`)})
	if w := up(pc.Token, png); w.Code != 409 {
		t.Fatalf("upload after the job: %d", w.Code)
	}
	r := <-answer
	if r.Assistant.Content != "Pronto, criei." || r.Assistant.Model != "gpt-6-astra" || r.Assistant.Error != "" {
		t.Fatalf("answer: %+v", r.Assistant)
	}
	if !strings.Contains(string(r.Assistant.Steps), "Codex · WebSearch") {
		t.Fatalf("steps: %s", r.Assistant.Steps)
	}
	// The bridge key dies with the turn.
	if code, _ := rpc(t, s, spec.Bridge, 2, "tools/list", nil); code != 401 {
		t.Fatalf("bridge after the turn: %d", code)
	}

	// A failure the owner can act on is shown as is.
	failed := make(chan string, 1)
	go func() {
		w := request(t, s, "POST", "/v1/conversations/"+c.ID+"/messages", adminTestToken, MessageRequest{Content: "e agora?"})
		failed <- w.Body.String()
	}()
	job = claimCodex(t, s, pc.Token)
	request(t, s, "POST", "/v1/runtime/jobs/"+job.ID+"/complete", pc.Token, JobResult{Error: "You've hit your usage limit."})
	if body := <-failed; !strings.Contains(body, "limite de uso") {
		t.Fatalf("limit error: %s", body)
	}

	// A quick question (summary, goal overseer) runs as a short nested job
	// with no tools, instead of failing on Codex (D53).
	allowed := make(chan bool, 1)
	go func() {
		allowed <- s.mandateAllows(t.Context(), store.Goal{Outcome: "vender a bike", Mandate: "pode mandar a primeira mensagem"}, "mandar oi ao vendedor")
	}()
	job = claimCodex(t, s, pc.Token)
	if job.Agent.Mode != store.CodexConsultMode || job.Agent.Bridge != "" || job.Agent.Workdir != "avora-quick" ||
		!strings.Contains(job.Agent.Prompt, "PERMITIDO ou PERGUNTAR") || !strings.Contains(job.Agent.Prompt, "mandar oi ao vendedor") {
		t.Fatalf("quick job: %+v", job.Agent)
	}
	request(t, s, "POST", "/v1/runtime/jobs/"+job.ID+"/complete", pc.Token, JobResult{Success: true, Output: "**PERMITIDO**"})
	if !<-allowed {
		t.Fatal("the overseer's answer was not read")
	}
}

func TestCodexTurnPromptKeepsTheLatestMessages(t *testing.T) {
	hist := []store.ChatMessage{}
	for i := 0; i < 30; i++ {
		hist = append(hist, store.ChatMessage{ID: int64(i + 1), Role: "user", Content: strings.Repeat("x", 8000)})
	}
	hist = append(hist, store.ChatMessage{ID: 99, Role: "user", Content: "a última"})
	p := codexTurnPrompt(time.Now(), "Você é o Atlas.", hist)
	if len(p) > maxCodexPrompt || !strings.Contains(p, "a última") || !strings.Contains(p, "Você é o Atlas.") {
		t.Fatalf("prompt: %d bytes", len(p))
	}
}

func TestQuotasListExtraAccountsByName(t *testing.T) {
	s := modelsServer(t)
	pc := pairNode(t, s, "pc", "windows")
	now := s.now()
	ctx := t.Context()
	if _, err := s.store.SaveProvider(ctx, store.Provider{ID: "claude.abc123", Kind: "claude", Name: "Trabalho", NodeID: pc.NodeID, Status: "ok"}, store.ActorAdmin, now); err != nil {
		t.Fatal(err)
	}
	w := request(t, s, "POST", "/v1/runtime/heartbeat", pc.Token, HeartbeatRequest{Quotas: []Quota{
		{Engine: "claude", CheckedAt: now, Weekly: &QuotaWindow{UsedPercent: 10}},
		{Engine: "claude", Account: "claude.abc123", CheckedAt: now, Weekly: &QuotaWindow{UsedPercent: 50}, Limited: true},
		{Engine: "claude", Account: "claude.gone00", CheckedAt: now},
	}})
	if w.Code != 200 {
		t.Fatalf("heartbeat: %d %s", w.Code, w.Body)
	}
	got := decode[[]QuotaStatus](t, request(t, s, "GET", "/v1/quotas", adminTestToken, nil))
	if len(got) != 2 || got[0].Account != "" || got[1].Name != "Trabalho" || got[1].Weekly.UsedPercent != 50 {
		t.Fatalf("quotas: %+v", got)
	}
	if !s.quotaLimited(pc.NodeID, "claude", "claude.abc123") || s.quotaLimited(pc.NodeID, "claude") {
		t.Fatal("limits are per login")
	}
	bad := request(t, s, "POST", "/v1/runtime/heartbeat", pc.Token, HeartbeatRequest{Quotas: []Quota{{Engine: "claude", Account: "codex.abc123"}}})
	if bad.Code != 400 {
		t.Fatalf("mismatched account accepted: %d", bad.Code)
	}
}

func TestWantsImage(t *testing.T) {
	img := store.ChatMessage{Role: "assistant", Content: "Aqui.\n\n![imagem gerada](avora:file/abc123)"}
	cases := []struct {
		h    []store.ChatMessage
		want bool
	}{
		{[]store.ChatMessage{{Role: "user", Content: "gere uma imagem de uma maçã"}}, true},
		{[]store.ChatMessage{{Role: "user", Content: "Cria um logo pra minha loja"}}, true},
		{[]store.ChatMessage{{Role: "user", Content: "faça um desenho de gato"}}, true},
		{[]store.ChatMessage{{Role: "user", Content: "como gerenciar minhas fotos?"}}, false},
		{[]store.ChatMessage{{Role: "user", Content: "gere um relatório"}}, false},
		{[]store.ChatMessage{{Role: "user", Content: "a"}, img, {Role: "user", Content: "gere outra"}}, true},
		{[]store.ChatMessage{{Role: "user", Content: "a"}, {Role: "assistant", Content: "texto"}, {Role: "user", Content: "gere outra"}}, false},
	}
	for _, c := range cases {
		if got := wantsImage(c.h); got != c.want {
			t.Errorf("%q: %v", c.h[len(c.h)-1].Content, got)
		}
	}
}
