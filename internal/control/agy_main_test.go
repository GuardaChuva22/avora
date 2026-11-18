package control

import (
	"strings"
	"testing"

	"github.com/GuardaChuva22/avora/internal/store"
)

// agyPoolServer has a PC with three signed-in Antigravity accounts:
// a (90% used), b (10%) and c (out of quota).
func agyPoolServer(t *testing.T) (*Server, PairResponse) {
	t.Helper()
	s := modelsServer(t)
	pc := pairNode(t, s, "pc", "windows")
	for _, id := range []string{"antigravity.aaaaaa", "antigravity.bbbbbb", "antigravity.cccccc"} {
		if _, err := s.store.SaveProvider(t.Context(), store.Provider{ID: id, Kind: "antigravity", Name: id[12:] + "@gmail.com",
			Account: id[12:] + "@gmail.com", NodeID: pc.NodeID, Status: "ok"}, store.ActorAdmin, s.now()); err != nil {
			t.Fatal(err)
		}
	}
	now := s.now()
	w := request(t, s, "POST", "/v1/runtime/heartbeat", pc.Token, HeartbeatRequest{Quotas: []Quota{
		{Engine: "antigravity", Account: "antigravity.aaaaaa", FiveHour: &QuotaWindow{UsedPercent: 90}, Weekly: &QuotaWindow{UsedPercent: 20}, CheckedAt: now},
		{Engine: "antigravity", Account: "antigravity.bbbbbb", FiveHour: &QuotaWindow{UsedPercent: 10}, Weekly: &QuotaWindow{UsedPercent: 5}, CheckedAt: now},
		{Engine: "antigravity", Account: "antigravity.cccccc", FiveHour: &QuotaWindow{UsedPercent: 100}, Limited: true, CheckedAt: now},
	}})
	if w.Code != 200 {
		t.Fatalf("heartbeat: %d %s", w.Code, w.Body)
	}
	return s, pc
}

func agyIDs(list []store.Provider) string {
	out := []string{}
	for _, p := range list {
		out = append(out, p.ID[12:13])
	}
	return strings.Join(out, "")
}

func TestAgyPoolPicksTheFreestAccount(t *testing.T) {
	s, _ := agyPoolServer(t)
	list, err := s.agyAccounts(t.Context())
	if err != nil || agyIDs(list) != "bac" {
		t.Fatalf("order: %s %v", agyIDs(list), err)
	}
	// A busy account goes behind an idle one.
	done := s.agyJobs.take("antigravity.bbbbbb")
	if list, _ := s.agyAccounts(t.Context()); agyIDs(list) != "abc" {
		t.Fatalf("busy b: %s", agyIDs(list))
	}
	done()
	done() // releasing twice is harmless
	// One that ran out mid-turn waits a while.
	s.markAgyLimited(store.Provider{ID: "antigravity.bbbbbb"})
	if list, _ := s.agyAccounts(t.Context()); agyIDs(list) != "abc" && agyIDs(list) != "acb" {
		t.Fatalf("b out: %s", agyIDs(list))
	}
	// A delegation without a named account goes to the freest one.
	p, err := s.accountFor(t.Context(), "antigravity", "", "")
	if err != nil || p.ID != "antigravity.aaaaaa" {
		t.Fatalf("delegation account: %+v %v", p, err)
	}
	if p, err := s.accountFor(t.Context(), "antigravity", "cccccc@gmail.com", ""); err != nil || p.ID != "antigravity.cccccc" {
		t.Fatalf("named account: %+v %v", p, err)
	}
}

func TestAgyPoolAsMainModel(t *testing.T) {
	s, pc := agyPoolServer(t)
	_, list := listModelsOf(t, s)
	var pool *ProviderView
	for i := range list {
		if list[i].ID == agyBrain {
			pool = &list[i]
		}
	}
	if pool == nil || !pool.CodexMain || pool.Account != "3 contas prontas" {
		t.Fatalf("pool view: %+v", pool)
	}
	// Models come from `agy models` on the runner (cached for 1 hour), not a hardcoded list.
	refreshed := make(chan ProviderView, 1)
	go func() {
		w := request(t, s, "POST", "/v1/models/providers/antigravity/refresh", adminTestToken, nil)
		refreshed <- decode[ProviderView](t, w)
	}()
	mjob := claimCodex(t, s, pc.Token)
	if mjob.Action != store.ActionCodexMain || mjob.Agent.Engine != "antigravity" || mjob.Agent.Mode != store.CodexModelsMode {
		t.Fatalf("models job: %+v %+v", mjob, mjob.Agent)
	}
	request(t, s, "POST", "/v1/runtime/jobs/"+mjob.ID+"/complete", pc.Token, JobResult{
		Success: true,
		Output:  `[{"id":"gemini-4-carbon-high","name":"Gemini 4 Carbon (High)"},{"id":"gemini-4-carbon-medium","name":"Gemini 4 Carbon (Medium)"}]`,
	})
	rv := <-refreshed
	if len(rv.Models) != 2 || rv.Models[0] != "gemini-4-carbon-high" {
		t.Fatalf("refreshed pool models: %+v", rv)
	}
	// A second refresh within 1 hour hits the cache without dispatching another runner job.
	if w := request(t, s, "POST", "/v1/models/providers/antigravity/refresh", adminTestToken, nil); w.Code != 200 {
		t.Fatalf("cached refresh: %d %s", w.Code, w.Body)
	}
	if w := request(t, s, "PUT", "/v1/models/main", adminTestToken, MainModel{Provider: agyBrain, Model: "gemini-4-carbon-high"}); w.Code != 200 {
		t.Fatalf("pick the pool: %d %s", w.Code, w.Body)
	}
	if !wholeTurns(s.chatEngine()) {
		t.Fatal("the pool should run whole turns")
	}

	// A turn runs on the freest account; when it runs out, the next one
	// takes the same turn.
	c := agentConversation(t, s, "atlas")
	answer := sendAsync(t, s, c.ID, "resume o dia")
	job := claimCodex(t, s, pc.Token)
	spec := job.Agent
	if job.Action != store.ActionCodexMain || spec.Engine != "antigravity" || spec.Account != "antigravity.bbbbbb" ||
		spec.Model != "gemini-4-carbon-high" || spec.Bridge == "" || spec.Mode != store.CodexChatMode || !strings.Contains(spec.Prompt, "resume o dia") {
		t.Fatalf("first job: %+v %+v", job, spec)
	}
	request(t, s, "POST", "/v1/runtime/jobs/"+job.ID+"/complete", pc.Token, JobResult{Error: "RESOURCE_EXHAUSTED: quota exceeded"})
	job = claimCodex(t, s, pc.Token)
	if job.Agent.Account != "antigravity.aaaaaa" || job.Agent.Bridge != spec.Bridge {
		t.Fatalf("second job: %+v", job.Agent)
	}
	request(t, s, "POST", "/v1/runtime/jobs/"+job.ID+"/complete", pc.Token,
		JobResult{Success: true, Output: "Dia tranquilo.", Stats: []byte(`{"input_tokens":10,"output_tokens":3}`)})
	r := <-answer
	if r.Assistant.Content != "Dia tranquilo." || r.Assistant.Error != "" || r.Assistant.Model != "gemini-4-carbon-high" {
		t.Fatalf("answer: %+v", r.Assistant)
	}
	if !strings.Contains(string(r.Assistant.Steps), "sem cota") {
		t.Fatalf("steps: %s", r.Assistant.Steps)
	}
}

func TestAgyAccountLoginModes(t *testing.T) {
	s := modelsServer(t)
	pc := pairNode(t, s, "pc", "windows")
	request(t, s, "POST", "/v1/runtime/heartbeat", pc.Token, nil)
	// A new account signs in to a Google login of its own.
	w := request(t, s, "POST", "/v1/models/providers", adminTestToken, ProviderRequest{Kind: "antigravity", NodeID: pc.NodeID})
	acc := decode[ProviderView](t, w)
	job := waitClaim(t, s, pc.Token)
	if job.Action != store.ActionAgentLogin || job.Agent.Mode != "login" || job.Agent.Account != acc.ID {
		t.Fatalf("login job: %+v", job.Agent)
	}
	request(t, s, "POST", "/v1/runtime/jobs/"+job.ID+"/complete", pc.Token, JobResult{Success: true, Output: `{"account":"nova@gmail.com"}`})
	for i := 0; i < 200; i++ {
		if v := loginView(t, s, acc.ID); v.State == "done" {
			break
		}
	}
	p, _ := s.store.GetProvider(t.Context(), acc.ID)
	if p.Status != "ok" || p.Name != "nova@gmail.com" {
		t.Fatalf("after login: %+v", p)
	}
	// The PC's own agy account, added before the pool, is only checked.
	old, _ := s.store.SaveProvider(t.Context(), store.Provider{ID: "antigravity.pc0000", Kind: "antigravity", Name: "Antigravity",
		Account: agyPCAccount, NodeID: pc.NodeID, Status: "ok"}, store.ActorAdmin, s.now())
	if w := request(t, s, "POST", "/v1/models/providers/"+old.ID+"/login", adminTestToken, nil); w.Code != 200 {
		t.Fatalf("relogin: %d %s", w.Code, w.Body)
	}
	job = waitClaim(t, s, pc.Token)
	if job.Agent.Mode != store.AgyCheckMode {
		t.Fatalf("check job: %+v", job.Agent)
	}
}
