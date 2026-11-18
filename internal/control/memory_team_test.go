package control

import (
	"context"
	"fmt"
	"strings"
	"testing"
	"time"

	"github.com/GuardaChuva22/avora/internal/llm"
	"github.com/GuardaChuva22/avora/internal/store"
)

func TestMemoryContextAndSearchAPI(t *testing.T) {
	s, _ := makeServer(t)
	ctx := context.Background()

	// Fill > promptMemories regular memories so we prove pinned memories ALWAYS enter context.
	for i := 0; i < promptMemories+5; i++ {
		w := request(t, s, "POST", "/v1/agents/cora/memories", adminTestToken, MemoryRequest{
			Content: fmt.Sprintf("Nota antiga número %d.", i),
			Kind:    "fact",
		})
		if w.Code != 201 {
			t.Fatalf("seed memory %d: %d %s", i, w.Code, w.Body.String())
		}
	}

	// Add an older-timestamped pinned memory directly via store and an unverified hint via API.
	pinnedMem, err := s.store.AddMemory(ctx, store.AgentMemory{
		WorkspaceID: "pessoal",
		AgentID:     "cora",
		Scope:       "agent",
		Kind:        "rule",
		Content:     "Sempre mostrar a memória de cálculo.",
		Source:      "dono",
		Verified:    true,
		Pinned:      true,
	}, store.ActorAdmin, time.Date(2026, 1, 1, 10, 0, 0, 0, time.UTC))
	if err != nil {
		t.Fatal(err)
	}

	vFalse := false
	hint := decode[store.AgentMemory](t, request(t, s, "POST", "/v1/agents/cora/memories", adminTestToken, MemoryRequest{
		Content:  "O cliente prefere boleto bancário.",
		Kind:     "preference",
		Source:   "conversa",
		Verified: &vFalse,
	}))

	// Add a team memory: must appear for every agent in workspace "pessoal", not in another workspace.
	teamW := request(t, s, "POST", "/v1/memories", adminTestToken, MemoryRequest{
		Scope:   "team",
		Kind:    "decision",
		Content: "O fechamento mensal acontece todo dia 28.",
	})
	if teamW.Code != 201 {
		t.Fatalf("create team memory: %d %s", teamW.Code, teamW.Body.String())
	}
	teamMem := decode[store.AgentMemory](t, teamW)

	coraBlock := s.memoryBlock(ctx, "cora")
	if !strings.Contains(coraBlock, pinnedMem.Content) {
		t.Fatalf("pinned memory missing from context: %s", coraBlock)
	}
	if !strings.Contains(coraBlock, "O cliente prefere boleto bancário. (dica, não confirmada)") {
		t.Fatalf("unverified memory must be marked as hint: %s", coraBlock)
	}
	if !strings.Contains(coraBlock, teamMem.Content) {
		t.Fatalf("team memory missing from cora context: %s", coraBlock)
	}
	atlasBlock := s.memoryBlock(ctx, "atlas")
	if !strings.Contains(atlasBlock, teamMem.Content) {
		t.Fatalf("team memory must be shared with atlas in same workspace: %s", atlasBlock)
	}

	// Confirm the hint -> "(dica, não confirmada)" disappears; then archive -> disappears completely.
	vTrue := true
	if w := request(t, s, "PATCH", fmt.Sprintf("/v1/memories/%d", hint.ID), adminTestToken, MemoryRequest{Verified: &vTrue}); w.Code != 200 {
		t.Fatalf("verify memory: %d %s", w.Code, w.Body.String())
	}
	coraBlock = s.memoryBlock(ctx, "cora")
	if !strings.Contains(coraBlock, "O cliente prefere boleto bancário.") || strings.Contains(coraBlock, "boleto bancário. (dica, não confirmada)") {
		t.Fatalf("verified memory should no longer carry unverified tag: %s", coraBlock)
	}

	archTrue := true
	if w := request(t, s, "PATCH", fmt.Sprintf("/v1/memories/%d", hint.ID), adminTestToken, MemoryRequest{Archived: &archTrue}); w.Code != 200 {
		t.Fatalf("archive memory: %d %s", w.Code, w.Body.String())
	}
	if strings.Contains(s.memoryBlock(ctx, "cora"), "boleto bancário") {
		t.Fatalf("archived memory must never enter context")
	}

	// Search API: GET /v1/memories?q=&scope=&kind=
	found := decode[[]store.AgentMemory](t, request(t, s, "GET", "/v1/memories?q=fechamento&scope=team&kind=decision", adminTestToken, nil))
	if len(found) != 1 || found[0].ID != teamMem.ID {
		t.Fatalf("unexpected search result: %+v", found)
	}
}

func TestMemoryAutoLearningRulesAndLessonsToProposal(t *testing.T) {
	e := &toolEngine{
		calls: map[string][]llm.ToolCall{
			"Atlas": {
				call("remember", map[string]any{"content": "Nunca fazer push direto na main.", "kind": "rule"}),
			},
		},
		answers: map[string]string{
			"Iris": "Entendido, vou ajustar.",
		},
	}
	s := chatServer(t, e)

	// 1. Owner rule stated in chat + remember tool with kind="rule" -> both verified & pinned rules.
	atlasConv := agentConversation(t, s, "atlas")
	<-sendAsync(t, s, atlasConv.ID, "Nunca mande e-mail sem me mostrar")

	rules := decode[[]store.AgentMemory](t, request(t, s, "GET", "/v1/memories?agent=atlas&kind=rule", adminTestToken, nil))
	if len(rules) < 2 {
		t.Fatalf("expected both owner chat rule and remember(kind=rule), got: %+v", rules)
	}
	for _, r := range rules {
		if !r.Verified || !r.Pinned || r.Kind != "rule" {
			t.Fatalf("rule must be verified and pinned: %+v", r)
		}
	}

	// 2. Owner corrects Iris 3 times -> unverified lesson becomes an instructions Proposal on 3rd occurrence.
	irisConv := agentConversation(t, s, "iris")
	<-sendAsync(t, s, irisConv.ID, "Liste os preços.")
	for i := 1; i <= 3; i++ {
		<-sendAsync(t, s, irisConv.ID, "Não, coloque sempre os links oficiais no final.")
	}
	lessons := decode[[]store.AgentMemory](t, request(t, s, "GET", "/v1/memories?agent=iris&kind=lesson", adminTestToken, nil))
	if len(lessons) != 1 || lessons[0].Verified || lessons[0].Occurrences != 3 {
		t.Fatalf("expected 1 unverified lesson with 3 occurrences, got: %+v", lessons)
	}
	props := decode[[]ProposalView](t, request(t, s, "GET", "/v1/proposals?target_agent=iris&status=pending", adminTestToken, nil))
	if len(props) != 1 || props[0].Field != "instructions" || !strings.Contains(props[0].After, "links oficiais") {
		t.Fatalf("expected pending instructions proposal after 3x lesson, got: %+v", props)
	}

	// 3. Denying the same approval 3 times -> unverified lesson + instructions Proposal for Cora.
	for i := 0; i < 3; i++ {
		ap, err := s.store.CreateApproval(context.Background(), store.Approval{
			ID:        fmt.Sprintf("ap-cora-%d", i),
			AgentID:   "cora",
			Action:    "gmail.send",
			Summary:   "Enviar cobrança para cliente sem revisão",
			Status:    "pending",
			CreatedAt: s.now(),
			ExpiresAt: s.now().Add(10 * time.Minute),
		}, "agent:cora")
		if err != nil {
			t.Fatal(err)
		}
		if w := request(t, s, "POST", "/v1/approvals/"+ap.ID, adminTestToken, ApprovalDecision{Decision: "deny"}); w.Code != 200 {
			t.Fatalf("deny approval %d: %d %s", i, w.Code, w.Body.String())
		}
	}
	coraProps := decode[[]ProposalView](t, request(t, s, "GET", "/v1/proposals?target_agent=cora&status=pending", adminTestToken, nil))
	if len(coraProps) != 1 || coraProps[0].Field != "instructions" {
		t.Fatalf("expected pending proposal for cora after 3 denied approvals, got: %+v", coraProps)
	}
}

func TestSectorsAndPinnedLimit(t *testing.T) {
	s, _ := makeServer(t)

	nameEng, colorEng, iconEng, leadEng := "Engenharia", "#7fd1ff", "code", "atlas"
	w := request(t, s, "POST", "/v1/sectors", adminTestToken, SectorRequest{
		Name: &nameEng, Color: &colorEng, Icon: &iconEng, LeadAgentID: &leadEng,
	})
	if w.Code != 201 {
		t.Fatalf("create sector: %d %s", w.Code, w.Body.String())
	}
	secEng := decode[store.Sector](t, w)

	nameFin := "Finanças"
	secFin := decode[store.Sector](t, request(t, s, "POST", "/v1/sectors", adminTestToken, SectorRequest{Name: &nameFin}))

	// Reorder Finanças first, Engenharia second.
	reordered := decode[[]store.Sector](t, request(t, s, "POST", "/v1/sectors/reorder", adminTestToken, SectorRequest{
		IDs: []string{secFin.ID, secEng.ID},
	}))
	if len(reordered) != 2 || reordered[0].ID != secFin.ID || reordered[1].ID != secEng.ID {
		t.Fatalf("unexpected sector reorder: %+v", reordered)
	}

	// Assign sector & reports_to; check cycle protection.
	repVega := "vega"
	if w := request(t, s, "PATCH", "/v1/agents/atlas", adminTestToken, AgentRequest{
		Sector: &secEng.ID, ReportsTo: &repVega,
	}); w.Code != 200 {
		t.Fatalf("patch atlas sector/reports_to: %d %s", w.Code, w.Body.String())
	}
	repAtlas := "atlas"
	if w := request(t, s, "PATCH", "/v1/agents/vega", adminTestToken, AgentRequest{ReportsTo: &repAtlas}); w.Code != 400 {
		t.Fatalf("expected 400 on reports_to cycle, got %d: %s", w.Code, w.Body.String())
	}
	atlas, _ := s.store.GetAgent(context.Background(), "atlas")
	org := s.orgBlock(context.Background(), atlas)
	if !strings.Contains(org, "Engenharia") || !strings.Contains(org, "Vega") {
		t.Fatalf("unexpected orgBlock: %q", org)
	}

	// Pinning up to 5 agents succeeds; 6th returns 409 Conflict.
	if w := request(t, s, "POST", "/v1/agents", adminTestToken, AgentRequest{
		Name: "Bia", Role: "Design", Persona: "Cuida da marca.", Color: "#ffc48a",
	}); w.Code != 201 {
		t.Fatalf("create 6th agent: %d %s", w.Code, w.Body.String())
	}
	pinTrue := true
	for _, id := range []string{"vega", "atlas", "iris", "cora", "nico"} {
		if w := request(t, s, "PATCH", "/v1/agents/"+id, adminTestToken, AgentRequest{Pinned: &pinTrue}); w.Code != 200 {
			t.Fatalf("pin %s: %d %s", id, w.Code, w.Body.String())
		}
	}
	if w := request(t, s, "PATCH", "/v1/agents/bia", adminTestToken, AgentRequest{Pinned: &pinTrue}); w.Code != 409 {
		t.Fatalf("expected 409 when pinning 6th agent, got %d: %s", w.Code, w.Body.String())
	}

	// Deleting sector clears agents.sector.
	if w := request(t, s, "DELETE", "/v1/sectors/"+secEng.ID, adminTestToken, nil); w.Code != 204 {
		t.Fatalf("delete sector: %d", w.Code)
	}
	atlasAfter := decode[store.Agent](t, request(t, s, "GET", "/v1/agents/atlas", adminTestToken, nil))
	if atlasAfter.Sector != "" {
		t.Fatalf("expected sector cleared after delete, got %q", atlasAfter.Sector)
	}
}

func TestProposalsApplyUndoAndReject(t *testing.T) {
	newPersona := "Você pesquisa com fontes primárias e sempre inclui tabela comparativa."
	e := &toolEngine{calls: map[string][]llm.ToolCall{"Atlas": {
		call("propose_change", map[string]any{
			"target_agent": "Iris",
			"field":        "instructions",
			"after":        newPersona,
			"reason":       "Padronizar entregas com tabela comparativa.",
		}),
		call("propose_change", map[string]any{
			"target_agent": "Iris",
			"field":        "role",
			"after":        "Pesquisadora Sênior",
			"reason":       "Escopo ampliado.",
		}),
	}}}
	s := chatServer(t, e)

	origIris := decode[store.Agent](t, request(t, s, "GET", "/v1/agents/iris", adminTestToken, nil))
	conv := agentConversation(t, s, "atlas")
	<-sendAsync(t, s, conv.ID, "proponha melhorias na Iris")

	props := decode[[]ProposalView](t, request(t, s, "GET", "/v1/proposals?target_agent=iris", adminTestToken, nil))
	if len(props) != 2 {
		t.Fatalf("expected 2 proposals, got: %+v", props)
	}
	var instProp, roleProp ProposalView
	for _, p := range props {
		if p.Field == "instructions" {
			instProp = p
		} else if p.Field == "role" {
			roleProp = p
		}
	}
	if instProp.Headline != "Atlas propõe mudar as instruções de Iris" {
		t.Fatalf("unexpected headline: %q", instProp.Headline)
	}

	// Apply instructions proposal -> creates new AgentVersion.
	applied := decode[ProposalView](t, request(t, s, "POST", "/v1/proposals/"+instProp.ID+"/apply", adminTestToken, nil))
	if applied.Status != "applied" || applied.PreVersion != origIris.Version || applied.AppliedVersion <= origIris.Version {
		t.Fatalf("unexpected applied proposal: %+v", applied)
	}
	irisNow := decode[store.Agent](t, request(t, s, "GET", "/v1/agents/iris", adminTestToken, nil))
	if irisNow.Persona != newPersona || irisNow.Version != applied.AppliedVersion {
		t.Fatalf("persona not updated on apply: %+v", irisNow)
	}

	// Undo restores original Persona via AgentVersions.
	undone := decode[ProposalView](t, request(t, s, "POST", "/v1/proposals/"+instProp.ID+"/undo", adminTestToken, nil))
	if undone.Status != "undone" {
		t.Fatalf("expected undone status, got: %+v", undone)
	}
	irisRestored := decode[store.Agent](t, request(t, s, "GET", "/v1/agents/iris", adminTestToken, nil))
	if irisRestored.Persona != origIris.Persona {
		t.Fatalf("undo did not restore original persona: got %q, want %q", irisRestored.Persona, origIris.Persona)
	}

	// Reject role proposal.
	rejected := decode[ProposalView](t, request(t, s, "POST", "/v1/proposals/"+roleProp.ID+"/reject", adminTestToken, nil))
	if rejected.Status != "rejected" {
		t.Fatalf("expected rejected status, got: %+v", rejected)
	}
}

func TestTellAgentDeliversNoteAndReturnsResult(t *testing.T) {
	e := &toolEngine{
		calls: map[string][]llm.ToolCall{
			"Atlas": {
				call("tell_agent", map[string]any{
					"agent":   "Iris",
					"message": "Levante os 3 principais concorrentes e me mande um resumo.",
				}),
			},
		},
		answers: map[string]string{
			"Iris": "Os 3 concorrentes mapeados são Alfa, Beta e Gama.",
		},
	}
	s := chatServer(t, e)
	atlasConv := agentConversation(t, s, "atlas")
	<-sendAsync(t, s, atlasConv.ID, "peça para a Iris mapear os concorrentes")

	// Verify Iris received "Recado de Atlas" in her own conversation.
	irisConvs := decode[[]store.Conversation](t, request(t, s, "GET", "/v1/conversations?agent=iris", adminTestToken, nil))
	if len(irisConvs) != 1 {
		t.Fatalf("expected 1 conversation for Iris, got: %+v", irisConvs)
	}
	irisMsgs := decode[[]store.ChatMessage](t, request(t, s, "GET", "/v1/conversations/"+irisConvs[0].ID+"/messages", adminTestToken, nil))
	if len(irisMsgs) == 0 || !strings.Contains(irisMsgs[0].Content, "Recado de Atlas") {
		t.Fatalf("expected 'Recado de Atlas' in Iris chat, got: %+v", irisMsgs)
	}

	// Wait briefly for Iris's async turn to finish and deliver its result back to Atlas's conversation.
	deadline := time.Now().Add(3 * time.Second)
	var atlasMsgs []store.ChatMessage
	foundReturn := false
	for time.Now().Before(deadline) {
		atlasMsgs = decode[[]store.ChatMessage](t, request(t, s, "GET", "/v1/conversations/"+atlasConv.ID+"/messages", adminTestToken, nil))
		for _, m := range atlasMsgs {
			if m.AgentID == "iris" && strings.Contains(m.Content, "Alfa, Beta e Gama") {
				foundReturn = true
				break
			}
		}
		if foundReturn {
			break
		}
		time.Sleep(15 * time.Millisecond)
	}
	if !foundReturn {
		t.Fatalf("expected Iris's result to return to Atlas's conversation, got: %+v", atlasMsgs)
	}
}

type blockingChatEngine struct {
	started chan struct{}
	release chan struct{}
}

func (b *blockingChatEngine) Name() string     { return "native" }
func (b *blockingChatEngine) Model() string    { return "fake-model" }
func (b *blockingChatEngine) Provider() string { return "fake" }
func (b *blockingChatEngine) Reply(ctx context.Context, _ string, _ []store.ChatMessage, _ func(llm.Delta)) (llm.Response, error) {
	select {
	case b.started <- struct{}{}:
	default:
	}
	select {
	case <-b.release:
		return llm.Response{Content: "concluído"}, nil
	case <-ctx.Done():
		return llm.Response{}, ctx.Err()
	}
}

func TestAutonomyBrakesInScheduler(t *testing.T) {
	eng := &blockingChatEngine{
		started: make(chan struct{}, 10),
		release: make(chan struct{}),
	}
	s := chatServer(t, eng)
	ctx := context.Background()

	// 1. can_start_alone = false blocks scheduled routine & heartbeat, but manual run still starts.
	canAlone := false
	if w := request(t, s, "PATCH", "/v1/agents/cora", adminTestToken, AgentRequest{
		Autonomy: &store.AgentAutonomy{CanStartAlone: &canAlone, MaxParallel: 1},
	}); w.Code != 200 {
		t.Fatalf("patch cora autonomy: %d %s", w.Code, w.Body.String())
	}

	past := s.now().Add(-time.Minute)
	rt1, err := s.store.SaveRoutine(ctx, store.Routine{
		ID: "rt-cora-1", AgentID: "cora", Title: "Caixa", Prompt: "Checar caixa",
		Schedule: store.Schedule{Every: 30}, Deliver: "conversation", Enabled: true, NextRun: &past,
	}, store.ActorAdmin, s.now())
	if err != nil {
		t.Fatal(err)
	}

	if s.startRoutine(ctx, rt1, false, "") {
		t.Fatal("scheduled routine must not start when can_start_alone is false")
	}
	rt1After, _ := s.store.GetRoutine(ctx, rt1.ID)
	if rt1After.LastStatus != "skipped" {
		t.Fatalf("expected routine LastStatus='skipped', got %q", rt1After.LastStatus)
	}

	// Heartbeat agent (vega) also respects can_start_alone = false.
	if w := request(t, s, "PATCH", "/v1/agents/vega", adminTestToken, AgentRequest{
		Autonomy: &store.AgentAutonomy{CanStartAlone: &canAlone, MaxParallel: 1},
	}); w.Code != 200 {
		t.Fatalf("patch vega autonomy: %d", w.Code)
	}
	if dec := s.Beat(ctx); dec != "bloqueado" {
		t.Fatalf("expected heartbeat decision 'bloqueado' when can_start_alone=false, got %q", dec)
	}

	// Manual run is allowed while max_parallel (1) is free.
	if !s.startRoutine(ctx, rt1, true, "") {
		t.Fatal("manual routine run should succeed even when can_start_alone is false")
	}
	<-eng.started

	// 2. While rt1 is running on cora and max_parallel == 1, a second routine on cora is blocked.
	rt2, err := s.store.SaveRoutine(ctx, store.Routine{
		ID: "rt-cora-2", AgentID: "cora", Title: "Boletos", Prompt: "Checar boletos",
		Schedule: store.Schedule{Every: 30}, Deliver: "conversation", Enabled: true, NextRun: &past,
	}, store.ActorAdmin, s.now())
	if err != nil {
		t.Fatal(err)
	}
	canAlone = true
	if w := request(t, s, "PATCH", "/v1/agents/cora", adminTestToken, AgentRequest{
		Autonomy: &store.AgentAutonomy{CanStartAlone: &canAlone, MaxParallel: 1},
	}); w.Code != 200 {
		t.Fatalf("enable can_start_alone: %d", w.Code)
	}
	if s.startRoutine(ctx, rt2, false, "") {
		t.Fatal("second concurrent routine must be blocked when max_parallel=1")
	}

	// 3. Raising max_parallel to 2 allows rt2 to run alongside rt1, while blocking a 3rd!
	if w := request(t, s, "PATCH", "/v1/agents/cora", adminTestToken, AgentRequest{
		Autonomy: &store.AgentAutonomy{CanStartAlone: &canAlone, MaxParallel: 2},
	}); w.Code != 200 {
		t.Fatalf("set max_parallel=2: %d", w.Code)
	}
	if !s.startRoutine(ctx, rt2, false, "") {
		t.Fatal("second routine should start when max_parallel=2")
	}
	<-eng.started

	rt3, _ := s.store.SaveRoutine(ctx, store.Routine{
		ID: "rt-cora-3", AgentID: "cora", Title: "Impostos", Prompt: "Checar impostos",
		Schedule: store.Schedule{Every: 30}, Deliver: "conversation", Enabled: true, NextRun: &past,
	}, store.ActorAdmin, s.now())
	if s.startRoutine(ctx, rt3, false, "") {
		t.Fatal("third routine must be blocked when max_parallel=2")
	}

	close(eng.release)
	for deadline := time.Now().Add(2 * time.Second); time.Now().Before(deadline); {
		if s.agentActiveCount(ctx, "cora") == 0 {
			break
		}
		time.Sleep(10 * time.Millisecond)
	}
}
