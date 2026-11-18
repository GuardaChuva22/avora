package control

import (
	"context"
	"encoding/json"
	"net/http/httptest"
	"regexp"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/GuardaChuva22/avora/internal/engine"
	"github.com/GuardaChuva22/avora/internal/llm"
	"github.com/GuardaChuva22/avora/internal/store"
)

// toolEngine plays a scripted agent: the agent named in the system prompt
// makes its calls, one round each, then answers with what the tools said.
type toolEngine struct {
	mu      sync.Mutex
	calls   map[string][]llm.ToolCall // agent name -> its calls
	answers map[string]string         // agent name -> plain answer (no calls)
	results []string
	tools   []string
}

func (e *toolEngine) Name() string     { return "native" }
func (e *toolEngine) Model() string    { return "fake-model" }
func (e *toolEngine) Provider() string { return "fake" }
func (e *toolEngine) Reply(_ context.Context, system string, _ []store.ChatMessage, _ func(llm.Delta)) (llm.Response, error) {
	if strings.HasPrefix(system, "Você decide quem") {
		return llm.Response{Content: e.answers["router"]}, nil // the #time router
	}
	panic("a tool engine is called through Run")
}

var agentNamePattern = regexp.MustCompile(`Você é (\pL+),`)

func (e *toolEngine) Run(ctx context.Context, system string, _ []store.ChatMessage, tools []llm.Tool, run engine.ToolRunner, onDelta func(llm.Delta)) (llm.Response, error) {
	name := agentNamePattern.FindStringSubmatch(system)[1]
	e.mu.Lock()
	calls := e.calls[name]
	answer, plain := e.answers[name]
	e.tools = e.tools[:0]
	for _, tl := range tools {
		e.tools = append(e.tools, tl.Function.Name)
	}
	e.mu.Unlock()
	if plain {
		return llm.Response{Content: answer}, nil
	}
	outs := []string{}
	for _, c := range calls {
		out := run(ctx, c)
		if ctx.Err() != nil {
			return llm.Response{}, ctx.Err()
		}
		outs = append(outs, out)
	}
	e.mu.Lock()
	e.results = append(e.results, outs...)
	e.mu.Unlock()
	if onDelta != nil {
		onDelta(llm.Delta{Content: "feito"})
	}
	return llm.Response{Content: "Feito. " + strings.Join(outs, " | ")}, nil
}

func call(name string, args any) llm.ToolCall {
	b, _ := json.Marshal(args)
	return llm.ToolCall{ID: name, Type: "function", Function: llm.FunctionCall{Name: name, Arguments: string(b)}}
}

func agentConversation(t *testing.T, s *Server, agent string) store.Conversation {
	t.Helper()
	return decode[store.Conversation](t, request(t, s, "POST", "/v1/conversations", adminTestToken, ConversationRequest{AgentID: agent}))
}

func TestAgentCreatesAndEditsRoutines(t *testing.T) {
	e := &toolEngine{calls: map[string][]llm.ToolCall{"Cora": {
		call("criar_rotina", map[string]any{"titulo": "Revisão de gastos", "instrucao": "Revise os gastos da semana.", "horario": "09:00", "dias": []int{1}}),
		call("criar_rotina", map[string]any{"titulo": "Sem horário", "instrucao": "x"}),
	}}}
	s := chatServer(t, e)
	c := agentConversation(t, s, "cora")
	resp := decode[MessageResponse](t, request(t, s, "POST", "/v1/conversations/"+c.ID+"/messages", adminTestToken,
		MessageRequest{Content: "toda segunda às 9 revisa meus gastos"}))
	list := decode[[]store.Routine](t, request(t, s, "GET", "/v1/routines?agent=cora", adminTestToken, nil))
	if len(list) != 1 || list[0].Title != "Revisão de gastos" || list[0].Schedule.At != "09:00" || list[0].Deliver != "time" ||
		!list[0].Enabled || list[0].NextRun == nil {
		t.Fatalf("routines: %+v", list)
	}
	// The second call had no schedule: the model reads the error.
	if !strings.Contains(resp.Assistant.Content, "Rotina criada") || !strings.Contains(resp.Assistant.Content, "Erro: diga quando") {
		t.Fatalf("answer: %q", resp.Assistant.Content)
	}
	var steps []AgentStep
	_ = json.Unmarshal(resp.Assistant.Steps, &steps)
	if len(steps) != 2 || steps[0].Tool != "criar_rotina" || !strings.Contains(steps[0].Summary, "«Revisão de gastos» · Cora · seg às 09:00") {
		t.Fatalf("steps: %+v", steps)
	}
	// Steps are in the log under the turn, for the app's live view.
	evs := decode[[]store.Event](t, request(t, s, "GET", "/v1/events?job="+resp.Assistant.JobID, adminTestToken, nil))
	if len(evs) < 2 || evs[0].Type != store.EventAgentStep || evs[1].Type != store.EventAgentStep || evs[0].Actor != "agent:cora" {
		t.Fatalf("events: %+v", evs)
	}

	// Pausing it and moving it to Vega, by name.
	id := list[0].ID
	e.calls["Cora"] = []llm.ToolCall{call("alterar_rotina", map[string]any{"id": id, "ativa": false, "funcionario": "vega", "a_cada_minutos": 60})}
	request(t, s, "POST", "/v1/conversations/"+c.ID+"/messages", adminTestToken, MessageRequest{Content: "pausa e passa pra Vega"})
	rt, _ := s.store.GetRoutine(t.Context(), id)
	if rt.Enabled || rt.AgentID != "vega" || rt.Schedule.Every != 60 || rt.Title != "Revisão de gastos" {
		t.Fatalf("edited: %+v", rt)
	}
	e.calls["Cora"] = []llm.ToolCall{call("ver_rotinas", map[string]any{"funcionario": "Vega"}), call("apagar_rotina", map[string]any{"id": id})}
	answer := sendAsync(t, s, c.ID, "lista e apaga")
	ap := waitApproval(t, s)
	request(t, s, "POST", "/v1/approvals/"+ap.ID, adminTestToken, ApprovalDecision{Decision: "once"})
	resp = <-answer
	if !strings.Contains(resp.Assistant.Content, "id="+id) || !strings.Contains(resp.Assistant.Content, "PAUSADA") {
		t.Fatalf("list: %q", resp.Assistant.Content)
	}
	if _, err := s.store.GetRoutine(t.Context(), id); err == nil {
		t.Fatal("routine not deleted")
	}
}

func TestAgentSetsResponsibilities(t *testing.T) {
	e := &toolEngine{calls: map[string][]llm.ToolCall{"Cora": {
		call("definir_responsabilidade", map[string]any{"objetivo": "Gastos abaixo do orçamento", "metrica": "gasto / orçamento"}),
	}}}
	s := chatServer(t, e)
	c := agentConversation(t, s, "cora")
	request(t, s, "POST", "/v1/conversations/"+c.ID+"/messages", adminTestToken, MessageRequest{Content: "cuida do meu orçamento"})
	cora, _ := s.store.GetAgent(t.Context(), "cora")
	if len(cora.Responsibilities) != 1 || cora.Responsibilities[0].Metric != "gasto / orçamento" {
		t.Fatalf("responsibilities: %+v", cora.Responsibilities)
	}
	rid := cora.Responsibilities[0].ID
	e.calls["Cora"] = []llm.ToolCall{
		call("definir_responsabilidade", map[string]any{"id": rid, "objetivo": "Gastos 10% abaixo do orçamento"}),
		call("criar_rotina", map[string]any{"titulo": "Checar orçamento", "instrucao": "Cheque.", "horario": "08:00", "responsabilidade_id": rid}),
	}
	request(t, s, "POST", "/v1/conversations/"+c.ID+"/messages", adminTestToken, MessageRequest{Content: "aperta a meta"})
	cora, _ = s.store.GetAgent(t.Context(), "cora")
	list, _ := s.store.ListRoutines(t.Context(), "cora")
	if len(cora.Responsibilities) != 1 || cora.Responsibilities[0].Goal != "Gastos 10% abaixo do orçamento" ||
		cora.Responsibilities[0].Metric != "gasto / orçamento" || len(list) != 1 || list[0].ResponsibilityID != rid {
		t.Fatalf("after edit: %+v %+v", cora.Responsibilities, list)
	}
	e.calls["Cora"] = []llm.ToolCall{call("remover_responsabilidade", map[string]any{"id": rid})}
	answer := sendAsync(t, s, c.ID, "esquece")
	ap := waitApproval(t, s)
	if ap.Action != "remover_responsabilidade" || !strings.Contains(ap.Summary, "«Gastos 10% abaixo do orçamento» de Cora") {
		t.Fatalf("approval: %+v", ap)
	}
	request(t, s, "POST", "/v1/approvals/"+ap.ID, adminTestToken, ApprovalDecision{Decision: "once"})
	<-answer
	if cora, _ = s.store.GetAgent(t.Context(), "cora"); len(cora.Responsibilities) != 0 {
		t.Fatalf("not removed: %+v", cora.Responsibilities)
	}
}

// sendAsync posts a message without waiting; the answer comes on the channel.
func sendAsync(t *testing.T, s *Server, convID, content string) <-chan MessageResponse {
	out := make(chan MessageResponse, 1)
	go func() {
		w := request(t, s, "POST", "/v1/conversations/"+convID+"/messages", adminTestToken, MessageRequest{Content: content})
		var r MessageResponse
		_ = json.Unmarshal(w.Body.Bytes(), &r)
		out <- r
	}()
	return out
}

func waitClaim(t *testing.T, s *Server, token string) Job {
	t.Helper()
	for i := 0; i < 400; i++ {
		w := request(t, s, "POST", "/v1/runtime/claim", token, ClaimRequest{Agents: true})
		if w.Code == 200 {
			return decode[Job](t, w)
		}
		time.Sleep(5 * time.Millisecond)
	}
	t.Fatal("no job to claim")
	return Job{}
}

func TestAgentDelegatesToClaudeCode(t *testing.T) {
	e := &toolEngine{calls: map[string][]llm.ToolCall{"Atlas": {
		call("claude_code", map[string]any{"tarefa": "Crie backup.py que copia ~/docs", "pode_editar": true}),
	}}}
	s := chatServer(t, e)
	if err := s.SetDelegateModels("haiku", ""); err != nil {
		t.Fatal(err)
	}
	pc := pairNode(t, s, "pc", "windows")
	request(t, s, "POST", "/v1/runtime/heartbeat", pc.Token, nil)
	c := agentConversation(t, s, "atlas")
	answer := sendAsync(t, s, c.ID, "cria um script de backup")

	// Editing files is locked: nothing reaches the node before the owner says yes.
	ap := waitApproval(t, s)
	if ap.Action != "claude_code.edit" || ap.AgentID != "atlas" || ap.ConversationID != c.ID ||
		!strings.Contains(ap.Summary, "Claude Code vai criar e editar arquivos na pasta agents/atlas: Crie backup.py") {
		t.Fatalf("approval: %+v", ap)
	}
	if w := request(t, s, "POST", "/v1/runtime/claim", pc.Token, ClaimRequest{Agents: true}); w.Code != 204 {
		t.Fatalf("a job before approval: %d", w.Code)
	}
	if w := request(t, s, "POST", "/v1/approvals/"+ap.ID, adminTestToken, ApprovalDecision{Decision: "once"}); w.Code != 200 {
		t.Fatalf("approve: %d %s", w.Code, w.Body.String())
	}
	job := waitClaim(t, s, pc.Token)
	if job.Agent.Engine != "claude" || job.Agent.Model != "haiku" || job.Agent.Mode != "edit" || job.Agent.Workdir != "agents/atlas" ||
		job.Agent.ConversationID != "" || !strings.Contains(job.Agent.Prompt, "trabalhando para Atlas (Dev)") ||
		!strings.HasSuffix(job.Agent.Prompt, "Crie backup.py que copia ~/docs") {
		t.Fatalf("job: %+v", job.Agent)
	}
	// While it works the conversation is busy and Atlas shows as working.
	if w := request(t, s, "POST", "/v1/conversations/"+c.ID+"/messages", adminTestToken, MessageRequest{Content: "e aí?"}); w.Code != 409 {
		t.Fatalf("second message while delegating: %d", w.Code)
	}
	if team := decode[[]AgentStatus](t, request(t, s, "GET", "/v1/agents", adminTestToken, nil)); !team[1].Working {
		t.Fatalf("atlas not working: %+v", team[1])
	}
	request(t, s, "POST", "/v1/runtime/jobs/"+job.ID+"/steps", pc.Token, AgentStep{Tool: "Write", Summary: "file_path: backup.py"})
	request(t, s, "POST", "/v1/runtime/jobs/"+job.ID+"/complete", pc.Token, JobResult{Success: true, Output: "Criei backup.py."})

	r := <-answer
	if !strings.Contains(r.Assistant.Content, "Claude Code terminou") || !strings.Contains(r.Assistant.Content, "Criei backup.py.") {
		t.Fatalf("answer: %+v", r.Assistant)
	}
	var steps []AgentStep
	_ = json.Unmarshal(r.Assistant.Steps, &steps)
	if len(steps) != 3 || steps[0].Tool != "aprovacao" || !strings.HasPrefix(steps[0].Summary, "permitida: ") ||
		steps[1].Tool != "claude" || steps[2].Tool != "Claude Code · Write" {
		t.Fatalf("steps: %+v", steps)
	}
	conv := decode[store.Conversation](t, request(t, s, "GET", "/v1/conversations/"+c.ID, adminTestToken, nil))
	if conv.JobID != "" {
		t.Fatalf("conversation still busy: %+v", conv)
	}
}

func TestDelegationStopAndNoNode(t *testing.T) {
	e := &toolEngine{calls: map[string][]llm.ToolCall{"Nico": {call("codex", map[string]any{"tarefa": "Analise o mercado"})}}}
	s := chatServer(t, e)
	c := agentConversation(t, s, "nico")

	// No node: the model reads why and the turn still answers.
	r := decode[MessageResponse](t, request(t, s, "POST", "/v1/conversations/"+c.ID+"/messages", adminTestToken, MessageRequest{Content: "analisa"}))
	if !strings.Contains(r.Assistant.Content, "Erro: nenhum nó online com o Codex") {
		t.Fatalf("answer: %q", r.Assistant.Content)
	}

	pc := pairNode(t, s, "pc", "windows")
	request(t, s, "POST", "/v1/runtime/heartbeat", pc.Token, nil)
	answer := sendAsync(t, s, c.ID, "de novo")
	job := waitClaim(t, s, pc.Token)
	if job.Agent.Engine != "codex" || job.Agent.Mode != "read" {
		t.Fatalf("job: %+v", job.Agent)
	}
	if w := request(t, s, "POST", "/v1/conversations/"+c.ID+"/stop", adminTestToken, nil); w.Code != 204 {
		t.Fatalf("stop: %d", w.Code)
	}
	<-answer // a stop ends the answer as "parado por você" (502 for JSON clients)
	msgs := decode[[]store.ChatMessage](t, request(t, s, "GET", "/v1/conversations/"+c.ID+"/messages", adminTestToken, nil))
	if last := msgs[len(msgs)-1]; last.Error != "parado por você" {
		t.Fatalf("after stop: %+v", last)
	}
	if j, _ := s.store.GetJob(t.Context(), job.ID); j.Status != store.StatusCanceled {
		t.Fatalf("job after stop: %s", j.Status)
	}
}

func TestAgentConsultsColleague(t *testing.T) {
	e := &toolEngine{
		calls:   map[string][]llm.ToolCall{"Vega": {call("consultar_colega", map[string]any{"funcionario": "Cora", "pergunta": "Quanto sobra no mês?"})}},
		answers: map[string]string{"Cora": "Sobram R$ 800."},
	}
	s := chatServer(t, e)
	c := agentConversation(t, s, "vega")
	r := decode[MessageResponse](t, request(t, s, "POST", "/v1/conversations/"+c.ID+"/messages", adminTestToken, MessageRequest{Content: "dá pra viajar?"}))
	if !strings.Contains(r.Assistant.Content, "Cora respondeu:\nSobram R$ 800.") {
		t.Fatalf("answer: %q", r.Assistant.Content)
	}
	// Cora, consulted, cannot consult in turn.
	for _, name := range e.tools {
		if name == "consultar_colega" || name == "avisar_colega" {
			t.Fatalf("the consulted colleague got %s: %v", name, e.tools)
		}
	}
	// The owner sees Cora's answer as her message inside Vega's.
	var steps []AgentStep
	_ = json.Unmarshal(r.Assistant.Steps, &steps)
	if len(steps) == 0 || steps[len(steps)-1].Tool != StepColleague || steps[len(steps)-1].Summary != "cora" || steps[len(steps)-1].Detail != "Sobram R$ 800." {
		t.Fatalf("steps: %+v", steps)
	}
}

func TestAgentAsksOwner(t *testing.T) {
	e := &toolEngine{
		calls: map[string][]llm.ToolCall{"Vega": {call("perguntar", map[string]any{
			"pergunta": "Qual destino?", "opcoes": []string{"Praia", " ", "Serra\nfria"}, "multipla": true, "outra": true})}},
	}
	s := chatServer(t, e)
	c := agentConversation(t, s, "vega")
	r := decode[MessageResponse](t, request(t, s, "POST", "/v1/conversations/"+c.ID+"/messages", adminTestToken, MessageRequest{Content: "planeja a viagem"}))
	var steps []AgentStep
	_ = json.Unmarshal(r.Assistant.Steps, &steps)
	var got *AgentStep
	for i := range steps {
		if steps[i].Tool == StepAsk {
			got = &steps[i]
		}
	}
	if got == nil || got.Summary != "Qual destino?" || got.Detail != `{"opcoes":["Praia","Serra fria"],"multipla":true,"outra":true}` {
		t.Fatalf("steps: %+v", steps)
	}
	// One option and no free answer is no question.
	if _, err := s.ask(&turn{}, toolArgs{Pergunta: "Ok?", Opcoes: []string{"Sim"}}); err == nil {
		t.Fatal("a single option was accepted")
	}
}

func TestAgentNotesColleague(t *testing.T) {
	e := &toolEngine{
		calls:   map[string][]llm.ToolCall{"Vega": {call("avisar_colega", map[string]any{"funcionario": "Cora", "recado": "Monta o orçamento da viagem."})}},
		answers: map[string]string{"Cora": "Orçamento pronto: R$ 3.200."},
	}
	s := chatServer(t, e)
	c := agentConversation(t, s, "vega")
	r := decode[MessageResponse](t, request(t, s, "POST", "/v1/conversations/"+c.ID+"/messages", adminTestToken, MessageRequest{Content: "planeja a viagem"}))
	if !strings.Contains(r.Assistant.Content, "Recado passado: Cora") {
		t.Fatalf("answer: %q", r.Assistant.Content)
	}
	// Vega's conversation keeps only her answer.
	if msgs, _ := s.store.Messages(t.Context(), c.ID); len(msgs) != 2 {
		t.Fatalf("vega's conversation: %+v", msgs)
	}
	// Cora works in a conversation of her own, opened by the note.
	var steps []AgentStep
	_ = json.Unmarshal(r.Assistant.Steps, &steps)
	var where string
	for _, st := range steps {
		if st.Tool == "avisar_colega" {
			where = st.Detail
		}
	}
	agent, conv, _ := strings.Cut(where, "/")
	if agent != "cora" || conv == "" {
		t.Fatalf("steps: %+v", steps)
	}
	var msgs []store.ChatMessage
	for i := 0; i < 200; i++ {
		if msgs, _ = s.store.Messages(t.Context(), conv); len(msgs) == 2 {
			break
		}
		time.Sleep(5 * time.Millisecond)
	}
	got, _ := s.store.GetConversation(t.Context(), conv)
	if got.AgentID != "cora" || !strings.HasPrefix(got.Title, notePrefix) || len(msgs) != 2 ||
		!strings.HasPrefix(msgs[0].Content, notePrefix+" Recado de Vega") || msgs[1].Content != "Orçamento pronto: R$ 3.200." {
		t.Fatalf("cora's conversation %+v: %+v", got, msgs)
	}
	// From there, Cora cannot pass notes on.
	ct := &turn{agent: store.Agent{ID: "cora", Name: "Cora"}, conv: conv}
	if _, err := s.note(t.Context(), ct, toolArgs{Funcionario: "Vega", Recado: "volta"}); err == nil {
		t.Fatal("a note from a note's conversation")
	}
}

func TestChannelAgentsUseTools(t *testing.T) {
	e := &toolEngine{calls: map[string][]llm.ToolCall{"Vega": {call("estado_do_avora", map[string]any{})}}}
	s := chatServer(t, e)
	say(t, s, "como estamos?")
	settle(t, s)
	msgs := channelMessages(t, s)
	if len(msgs) != 2 || !strings.Contains(msgs[1].Content, "Rotinas do time") || len(msgs[1].Steps) == 0 {
		t.Fatalf("messages: %+v", msgs)
	}
	for _, name := range e.tools {
		if name == "consultar_colega" {
			t.Fatal("in #time colleagues are called with @")
		}
	}
}

func TestChatStreamsToolSteps(t *testing.T) {
	e := &toolEngine{calls: map[string][]llm.ToolCall{"Vega": {call("estado_do_avora", map[string]any{})}}}
	s := chatServer(t, e)
	c := agentConversation(t, s, "vega")
	req := httptest.NewRequest("POST", "/v1/conversations/"+c.ID+"/messages", strings.NewReader(`{"content":"oi"}`))
	req.Header.Set("Authorization", "Bearer "+adminTestToken)
	req.Header.Set("Accept", "text/event-stream")
	w := httptest.NewRecorder()
	s.Handler().ServeHTTP(w, req)
	body := w.Body.String()
	if !strings.Contains(body, "event: step\ndata: {\"tool\":\"estado_do_avora\"") || !strings.Contains(body, "event: done") {
		t.Fatalf("stream: %s", body)
	}
}

func TestRecoverTurns(t *testing.T) {
	s := chatServer(t, &fakeEngine{})
	c := agentConversation(t, s, "vega")
	if err := s.store.StartConversationJob(t.Context(), c.ID, "lost-turn"); err != nil {
		t.Fatal(err)
	}
	if err := s.RecoverTurns(t.Context()); err != nil {
		t.Fatal(err)
	}
	msgs := decode[[]store.ChatMessage](t, request(t, s, "GET", "/v1/conversations/"+c.ID+"/messages", adminTestToken, nil))
	conv := decode[store.Conversation](t, request(t, s, "GET", "/v1/conversations/"+c.ID, adminTestToken, nil))
	if len(msgs) != 1 || !strings.Contains(msgs[0].Error, "reiniciou") || conv.JobID != "" {
		t.Fatalf("after recovery: %+v %+v", msgs, conv)
	}
}
