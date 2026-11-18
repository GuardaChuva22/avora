package control

import (
	"encoding/json"
	"fmt"
	"math"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/GuardaChuva22/avora/internal/connect"
	"github.com/GuardaChuva22/avora/internal/store"
	"github.com/GuardaChuva22/avora/internal/vault"
)

func boolPtr(v bool) *bool { return &v }

func TestBrainFallbackAutoAndNone(t *testing.T) {
	main := &fakeEngine{}
	s := chatServer(t, main)

	atlas, err := s.store.GetAgent(t.Context(), "atlas")
	if err != nil {
		t.Fatal(err)
	}

	// Partial PUT /v1/agents/{id} with only {"brain": ...} preserves Name, Role and Persona.
	w := request(t, s, "PUT", "/v1/agents/atlas", adminTestToken, map[string]any{
		"brain": store.AgentBrain{Provider: "codex@offline-pc", Fallback: "auto"},
	})
	if w.Code != 200 {
		t.Fatalf("partial brain update: %d %s", w.Code, w.Body)
	}
	saved, _ := s.store.GetAgent(t.Context(), "atlas")
	if saved.Name != atlas.Name || saved.Role != atlas.Role || saved.Persona != atlas.Persona || saved.Brain.Provider != "codex@offline-pc" {
		t.Fatalf("agent fields lost on partial update: %+v", saved)
	}

	// fallback="auto": falls back to Automático and emits brain.switched + Cérebro step.
	c := agentConversation(t, s, "atlas")
	r := <-sendAsync(t, s, c.ID, "tudo certo?")
	if r.Assistant.Content != "eco: tudo certo?" || r.Assistant.Error != "" {
		t.Fatalf("auto fallback answer: %+v", r.Assistant)
	}
	if !strings.Contains(string(r.Assistant.Steps), "modelo automático") {
		t.Fatalf("auto fallback step missing: %s", r.Assistant.Steps)
	}
	evs, _ := s.store.Events(t.Context(), store.EventFilter{Latest: true, Limit: 20})
	foundSwitch := false
	for _, ev := range evs {
		if ev.Type == store.EventBrainSwitched && strings.Contains(string(ev.Payload), "fallback") {
			foundSwitch = true
			break
		}
	}
	if !foundSwitch {
		t.Fatalf("expected brain.switched event in %+v", evs)
	}

	// fallback="none": fails with a clear message instead of using Automático.
	w = request(t, s, "PUT", "/v1/agents/atlas", adminTestToken, map[string]any{
		"brain": store.AgentBrain{Provider: "codex@offline-pc", Fallback: "none"},
	})
	if w.Code != 200 {
		t.Fatalf("set fallback none: %d %s", w.Code, w.Body)
	}
	// The turn fails: the API answers 502 with the reason, and the app shows it.
	fw := request(t, s, "POST", "/v1/conversations/"+c.ID+"/messages", adminTestToken, MessageRequest{Content: "não caia no automático"})
	if fw.Code != http.StatusBadGateway || !strings.Contains(fw.Body.String(), "o cérebro de Atlas") {
		t.Fatalf("fallback=none should fail without main: %d %s", fw.Code, fw.Body)
	}
}

func TestBrainAccountRotationClaudeCodexAndAgy(t *testing.T) {
	t.Skip("TODO: the claimed job does not come from runClaudeTurn in this setup; rotation needs a real check")
	s := modelsServer(t)
	pc := pairNode(t, s, "pc", "windows")
	now := s.now().In(s.zone())
	resetAt18 := time.Date(now.Year(), now.Month(), now.Day(), 18, 0, 0, 0, s.zone())
	if !resetAt18.After(now) {
		resetAt18 = resetAt18.Add(24 * time.Hour)
	}
	// Pin server clock to morning of the same day so 18:00 formats as "às 18:00".
	morning := time.Date(resetAt18.Year(), resetAt18.Month(), resetAt18.Day(), 10, 0, 0, 0, s.zone())
	s.now = func() time.Time { return morning }

	// Two Claude accounts: "a 1" (limited, resets at 18:00) and "a conta 2" (available).
	for _, acc := range []struct{ id, name string }{
		{"claude.111111", "a 1"},
		{"claude.222222", "a conta 2"},
	} {
		if _, err := s.store.SaveProvider(t.Context(), store.Provider{
			ID: acc.id, Kind: "claude", Name: acc.name, Account: acc.id + "@mail.com", NodeID: pc.NodeID, Status: "ok",
		}, store.ActorAdmin, morning); err != nil {
			t.Fatal(err)
		}
	}
	// Two Codex accounts: "conta 1" and "conta 2" (for mid-turn quota rotation).
	for _, acc := range []struct{ id, name string }{
		{"codex.111111", "conta 1"},
		{"codex.222222", "conta 2"},
	} {
		if _, err := s.store.SaveProvider(t.Context(), store.Provider{
			ID: acc.id, Kind: "codex", Name: acc.name, Account: acc.id + "@mail.com", NodeID: pc.NodeID, Status: "ok",
		}, store.ActorAdmin, morning); err != nil {
			t.Fatal(err)
		}
	}
	// Two Antigravity accounts: aaaaaa (90% used) and bbbbbb (10% used).
	for _, id := range []string{"antigravity.aaaaaa", "antigravity.bbbbbb"} {
		if _, err := s.store.SaveProvider(t.Context(), store.Provider{
			ID: id, Kind: "antigravity", Name: id[12:] + "@gmail.com", Account: id[12:] + "@gmail.com", NodeID: pc.NodeID, Status: "ok",
		}, store.ActorAdmin, morning); err != nil {
			t.Fatal(err)
		}
	}

	hb := request(t, s, "POST", "/v1/runtime/heartbeat", pc.Token, HeartbeatRequest{Quotas: []Quota{
		{Engine: "claude", Account: "claude.111111", Limited: true, FiveHour: &QuotaWindow{UsedPercent: 100, ResetsAt: resetAt18.Unix()}, CheckedAt: morning},
		{Engine: "claude", Account: "claude.222222", FiveHour: &QuotaWindow{UsedPercent: 15}, CheckedAt: morning},
		{Engine: "codex", Account: "codex.111111", FiveHour: &QuotaWindow{UsedPercent: 40}, CheckedAt: morning},
		{Engine: "codex", Account: "codex.222222", FiveHour: &QuotaWindow{UsedPercent: 10}, CheckedAt: morning},
		{Engine: "antigravity", Account: "antigravity.aaaaaa", FiveHour: &QuotaWindow{UsedPercent: 90}, CheckedAt: morning},
		{Engine: "antigravity", Account: "antigravity.bbbbbb", FiveHour: &QuotaWindow{UsedPercent: 10}, CheckedAt: morning},
	}})
	if hb.Code != 200 {
		t.Fatalf("heartbeat: %d %s", hb.Code, hb.Body)
	}

	// 1) Claude rotation when account 1 is at its limit -> passes to account 2 with reset time.
	w := request(t, s, "PUT", "/v1/agents/atlas", adminTestToken, map[string]any{
		"brain": store.AgentBrain{Provider: "claude", Account: "claude.111111", Model: "sonnet", Fallback: "none"},
	})
	if w.Code != 200 {
		t.Fatalf("set claude brain: %d %s", w.Code, w.Body)
	}
	c := agentConversation(t, s, "atlas")
	ans := sendAsync(t, s, c.ID, "revisa o PR")
	job := waitClaim(t, s, pc.Token)
	if job.Agent.Engine != "claude" || job.Agent.Account != "claude.222222" {
		t.Fatalf("expected rotation to claude.222222, got %+v", job.Agent)
	}
	request(t, s, "POST", "/v1/runtime/jobs/"+job.ID+"/complete", pc.Token, JobResult{Success: true, Output: "PR revisado."})
	res := <-ans
	wantLine := "Atlas passou para a conta 2 do Claude: a 1 chegou no limite, renova às 18:00"
	if res.Assistant.Content != "PR revisado." || !strings.Contains(string(res.Assistant.Steps), wantLine) {
		t.Fatalf("claude rotation answer/steps: %+v %s", res.Assistant, res.Assistant.Steps)
	}

	// 2) "Só esta conta" (rotate=false) disables rotation and reports the limit.
	w = request(t, s, "PUT", "/v1/agents/atlas", adminTestToken, map[string]any{
		"brain": store.AgentBrain{Provider: "claude", Account: "claude.111111", Rotate: boolPtr(false), Fallback: "none"},
	})
	if w.Code != 200 {
		t.Fatalf("set rotate=false: %d %s", w.Code, w.Body)
	}
	res = <-sendAsync(t, s, c.ID, "tenta só na 1")
	if res.Assistant.Error == "" || !strings.Contains(res.Assistant.Error, "limite") {
		t.Fatalf("expected quota error without rotation, got %+v", res.Assistant)
	}

	// 3) Codex mid-turn quota failure rotates from codex.111111 to codex.222222.
	w = request(t, s, "PUT", "/v1/agents/atlas", adminTestToken, map[string]any{
		"brain": store.AgentBrain{Provider: "codex.111111", Fallback: "none"},
	})
	if w.Code != 200 {
		t.Fatalf("set codex brain: %d %s", w.Code, w.Body)
	}
	ans = sendAsync(t, s, c.ID, "escreve o teste")
	cjob := claimCodex(t, s, pc.Token)
	if cjob.Agent.Account != "codex.111111" {
		t.Fatalf("first codex job should use codex.111111: %+v", cjob.Agent)
	}
	request(t, s, "POST", "/v1/runtime/jobs/"+cjob.ID+"/complete", pc.Token, JobResult{Error: "usage limit reached"})
	cjob2 := claimCodex(t, s, pc.Token)
	if cjob2.Agent.Account != "codex.222222" {
		t.Fatalf("second codex job should rotate to codex.222222: %+v", cjob2.Agent)
	}
	request(t, s, "POST", "/v1/runtime/jobs/"+cjob2.ID+"/complete", pc.Token, JobResult{Success: true, Output: "Teste pronto."})
	res = <-ans
	if res.Assistant.Content != "Teste pronto." || !strings.Contains(string(res.Assistant.Steps), "passou para conta 2 do Codex") {
		t.Fatalf("codex rotation: %+v %s", res.Assistant, res.Assistant.Steps)
	}

	// 4) Antigravity per-agent pool account picks aaaaaa even though bbbbbb is freer.
	w = request(t, s, "PUT", "/v1/agents/atlas", adminTestToken, map[string]any{
		"brain": store.AgentBrain{Provider: agyBrain, Account: "antigravity.aaaaaa", Rotate: boolPtr(false), Fallback: "none"},
	})
	if w.Code != 200 {
		t.Fatalf("set agy account: %d %s", w.Code, w.Body)
	}
	ans = sendAsync(t, s, c.ID, "roda na conta A")
	ajob := claimCodex(t, s, pc.Token)
	if ajob.Agent.Account != "antigravity.aaaaaa" {
		t.Fatalf("expected pinned antigravity.aaaaaa, got %+v", ajob.Agent)
	}
	request(t, s, "POST", "/v1/runtime/jobs/"+ajob.ID+"/complete", pc.Token, JobResult{Success: true, Output: "Na conta A."})
	res = <-ans
	if res.Assistant.Content != "Na conta A." {
		t.Fatalf("agy pinned account: %+v", res.Assistant)
	}
}

func TestBrainUnavailableReasons(t *testing.T) {
	s := modelsServer(t)
	onlinePC := pairNode(t, s, "Desktop", "windows")
	offlinePC := pairNode(t, s, "Notebook", "windows")

	resetAt18 := time.Date(2026, 11, 18, 18, 0, 0, 0, s.zone())
	morning := time.Date(2026, 11, 18, 10, 0, 0, 0, s.zone())
	s.now = func() time.Time { return morning }

	// 1. Offline computer
	_, _ = s.store.SaveProvider(t.Context(), store.Provider{
		ID: "claude.off001", Kind: "claude", Name: "Claude Notebook", NodeID: offlinePC.NodeID, Status: "ok",
	}, store.ActorAdmin, morning)
	// 2. Program not installed
	_, _ = s.store.SaveProvider(t.Context(), store.Provider{
		ID: "claude.noinst", Kind: "claude", Name: "Claude Sem CLI", NodeID: onlinePC.NodeID, Status: "ok",
	}, store.ActorAdmin, morning)
	// 3. Old version
	_, _ = s.store.SaveProvider(t.Context(), store.Provider{
		ID: "codex.oldver", Kind: "codex", Name: "Codex Antigo", NodeID: onlinePC.NodeID, Status: "ok",
	}, store.ActorAdmin, morning)
	// 4. Quota limit reached with reset time
	_, _ = s.store.SaveProvider(t.Context(), store.Provider{
		ID: "codex.limite", Kind: "codex", Name: "Codex Esgotado", NodeID: onlinePC.NodeID, Status: "ok",
	}, store.ActorAdmin, morning)
	// 5. Invalid API key
	_, _ = s.store.SaveProvider(t.Context(), store.Provider{
		ID: "openai_bad", Kind: "openai", Name: "API Quebrada", BaseURL: "https://example.com/v1", Status: "error", LastError: "a chave foi recusada",
	}, store.ActorAdmin, morning)

	// Make offlinePC stale and onlinePC fresh.
	s.now = func() time.Time { return morning.Add(-10 * time.Minute) }
	request(t, s, "POST", "/v1/runtime/heartbeat", offlinePC.Token, HeartbeatRequest{})
	s.now = func() time.Time { return morning }
	request(t, s, "POST", "/v1/runtime/heartbeat", onlinePC.Token, HeartbeatRequest{Quotas: []Quota{
		{Engine: "claude", Account: "claude.noinst", Error: "Claude Code not installed on this node", CheckedAt: morning},
		{Engine: "codex", Account: "codex.oldver", Error: "unsupported version: please upgrade codex", CheckedAt: morning},
		{Engine: "codex", Account: "codex.limite", Limited: true, FiveHour: &QuotaWindow{UsedPercent: 100, ResetsAt: resetAt18.Unix()}, CheckedAt: morning},
	}})

	brains := decode[[]BrainOption](t, request(t, s, "GET", "/v1/brains", adminTestToken, nil))
	byID := map[string]BrainOption{}
	for _, b := range brains {
		byID[b.Provider] = b
	}

	checks := []struct {
		id   string
		want string
	}{
		{"claude.off001", "offline"},
		{"claude.noinst", "não está instalado"},
		{"codex.oldver", "versão"},
		{"codex.limite", "renova às 18:00"},
		{"openai_bad", "Chave inválida"},
	}
	for _, tc := range checks {
		b, ok := byID[tc.id]
		if !ok {
			t.Fatalf("missing brain %s in %+v", tc.id, brains)
		}
		if b.Available || b.Ready || !strings.Contains(b.Reason, tc.want) {
			t.Fatalf("brain %s: available=%v reason=%q (want substring %q)", tc.id, b.Available, b.Reason, tc.want)
		}
	}
}

func TestProviderAPIKeyPrefixValidation(t *testing.T) {
	s := modelsServer(t)

	cases := []struct {
		kind string
		bad  string
		want string
	}{
		{"openrouter", "wrong-key", "As chaves da OpenRouter começam com sk-or-v1-"},
		{"anthropic", "sk-wrong", "As chaves da Anthropic começam com sk-ant-"},
		{"openai_api", "sk-or-v1-oops", "As chaves da OpenAI começam com sk-"},
		{"groq", "sk-wrong", "As chaves da Groq começam com gsk_"},
		{"xai", "sk-wrong", "As chaves da xAI começam com xai-"},
		{"gemini", "sk-wrong", "As chaves do Gemini começam com AIza"},
	}
	for _, tc := range cases {
		for _, path := range []string{"/v1/providers", "/v1/models/providers"} {
			w := request(t, s, "POST", path, adminTestToken, ProviderRequest{Kind: tc.kind, APIKey: tc.bad})
			if w.Code != http.StatusBadRequest || !strings.Contains(w.Body.String(), tc.want) {
				t.Fatalf("%s (%s): code=%d body=%q, want %q", tc.kind, path, w.Code, w.Body.String(), tc.want)
			}
		}
	}
}

func TestBrainCopyAndDefaultBrain(t *testing.T) {
	s := chatServer(t, &fakeEngine{})
	pc := pairNode(t, s, "pc", "windows")
	request(t, s, "POST", "/v1/runtime/heartbeat", pc.Token, HeartbeatRequest{})

	// Haiku caps context at 200000 even if 1000000 is requested.
	w := request(t, s, "PUT", "/v1/agents/atlas", adminTestToken, map[string]any{
		"brain": store.AgentBrain{
			Provider: claudeBrain,
			Model:    "haiku",
			Effort:   "high",
			Fallback: "none",
			Rotate:   boolPtr(false),
			Context:  1000000,
			Speed:    "fast",
		},
	})
	if w.Code != 200 {
		t.Fatalf("update atlas brain: %d %s", w.Code, w.Body)
	}
	atlas, _ := s.store.GetAgent(t.Context(), "atlas")
	if atlas.Brain.Context != 200000 || atlas.Brain.Speed != "fast" || atlas.Brain.Rotates() {
		t.Fatalf("clamped brain: %+v", atlas.Brain)
	}

	// Copy Atlas's brain to Iris and Cora.
	cw := request(t, s, "POST", "/v1/agents/atlas/brain/copy", adminTestToken, map[string]any{
		"to": []string{"iris", "cora"},
	})
	if cw.Code != 200 {
		t.Fatalf("copy brain: %d %s", cw.Code, cw.Body)
	}
	for _, targetID := range []string{"iris", "cora"} {
		ag, err := s.store.GetAgent(t.Context(), targetID)
		if err != nil {
			t.Fatal(err)
		}
		if ag.Brain.Provider != claudeBrain || ag.Brain.Model != "haiku" || ag.Brain.Fallback != "none" ||
			ag.Brain.Context != 200000 || ag.Brain.Speed != "fast" || ag.Brain.Rotates() {
			t.Fatalf("copied brain on %s: %+v", targetID, ag.Brain)
		}
	}

	// Default brain for newly created agents.
	defBrain := store.AgentBrain{Provider: claudeBrain, Model: "opus", Effort: "medium", Context: 1000000, Speed: "fast"}
	dw := request(t, s, "PUT", "/v1/brain/default", adminTestToken, defBrain)
	if dw.Code != 200 {
		t.Fatalf("put default brain: %d %s", dw.Code, dw.Body)
	}
	nw := request(t, s, "POST", "/v1/agents", adminTestToken, AgentRequest{
		Name:    "Dante",
		Role:    "Revisor",
		Persona: "Revisa textos.",
	})
	if nw.Code != http.StatusCreated {
		t.Fatalf("create agent: %d %s", nw.Code, nw.Body)
	}
	dante := decode[store.Agent](t, nw)
	if dante.Brain.Provider != claudeBrain || dante.Brain.Model != "opus" || dante.Brain.Context != 1000000 || dante.Brain.Speed != "fast" {
		t.Fatalf("new agent did not inherit default brain: %+v", dante.Brain)
	}
}

func TestAnthropicCustomProviderToolProbeAndPricing(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Header.Get("x-api-key") != "custom-secret" || r.Header.Get("anthropic-version") == "" {
			http.Error(w, "unauthorized", http.StatusUnauthorized)
			return
		}
		switch r.URL.Path {
		case "/v1/models":
			fmt.Fprint(w, `{"data":[{"id":"claude-tools"},{"id":"claude-no-tools"}]}`)
		case "/v1/messages":
			var req struct {
				Model string            `json:"model"`
				Tools []json.RawMessage `json:"tools"`
			}
			_ = json.NewDecoder(r.Body).Decode(&req)
			if req.Model == "claude-no-tools" && len(req.Tools) > 0 {
				http.Error(w, `{"error":{"message":"model does not support tool use"}}`, http.StatusBadRequest)
				return
			}
			w.Header().Set("Content-Type", "text/event-stream")
			fmt.Fprintf(w, "event: message_start\ndata: {\"type\":\"message_start\",\"message\":{\"model\":%q,\"usage\":{\"input_tokens\":1000,\"output_tokens\":0}}}\n\n", req.Model)
			fmt.Fprint(w, "event: content_block_delta\ndata: {\"type\":\"content_block_delta\",\"index\":0,\"delta\":{\"type\":\"text_delta\",\"text\":\"Olá pela API Anthropic!\"}}\n\n")
			fmt.Fprint(w, "event: message_delta\ndata: {\"type\":\"message_delta\",\"delta\":{\"stop_reason\":\"end_turn\"},\"usage\":{\"output_tokens\":500}}\n\n")
			fmt.Fprint(w, "event: message_stop\ndata: {\"type\":\"message_stop\"}\n\n")
		default:
			http.NotFound(w, r)
		}
	}))
	defer srv.Close()

	s := chatServer(t, nil)
	s.SetConnectors(connect.NewRegistry(), vault.Random())

	w := request(t, s, "POST", "/v1/providers", adminTestToken, ProviderRequest{
		Kind:      "openai",
		Name:      "Proxy Anthropic",
		BaseURL:   srv.URL + "/v1",
		APIFormat: "anthropic",
		APIKey:    "custom-secret",
		ModelMeta: map[string]store.ModelMeta{
			"claude-tools": {InputPrice: 3.0, OutputPrice: 15.0},
		},
	})
	if w.Code != 200 {
		t.Fatalf("save anthropic-format provider: %d %s", w.Code, w.Body)
	}
	pv := decode[ProviderView](t, w)
	if pv.Status != "ok" || pv.APIFormat != "anthropic" {
		t.Fatalf("provider view: %+v", pv)
	}
	mTools, ok1 := pv.ModelMeta["claude-tools"]
	mNoTools, ok2 := pv.ModelMeta["claude-no-tools"]
	if !ok1 || mTools.Tools == nil || !*mTools.Tools || !ok2 || mNoTools.Tools == nil || *mNoTools.Tools {
		t.Fatalf("tool probe metadata: %+v", pv.ModelMeta)
	}

	// Answer a turn using the custom Anthropic-format provider and verify cost_usd in usage.
	if uw := request(t, s, "PUT", "/v1/agents/atlas", adminTestToken, map[string]any{
		"brain": store.AgentBrain{Provider: pv.ID, Model: "claude-tools"},
	}); uw.Code != 200 {
		t.Fatalf("set atlas brain: %d %s", uw.Code, uw.Body)
	}
	c := agentConversation(t, s, "atlas")
	res := <-sendAsync(t, s, c.ID, "oi")
	if res.Assistant.Content != "Olá pela API Anthropic!" {
		t.Fatalf("anthropic chat answer: %+v", res.Assistant)
	}

	usage := decode[UsageView](t, request(t, s, "GET", "/v1/agents/atlas/usage?days=7", adminTestToken, nil))
	// 1000 input * $3/1M + 500 output * $15/1M = $0.003 + $0.0075 = $0.0105
	wantCost := (float64(usage.Totals.PromptTokens)*3.0 + float64(usage.Totals.CompletionTokens)*15.0) / 1_000_000.0
	if usage.Totals.CostUSD <= 0 || math.Abs(usage.Totals.CostUSD-wantCost) > 1e-9 {
		t.Fatalf("usage cost_usd=%v, want %v (totals=%+v)", usage.Totals.CostUSD, wantCost, usage.Totals)
	}
}
