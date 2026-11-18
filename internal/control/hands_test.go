package control

import (
	"context"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/GuardaChuva22/avora/internal/llm"
	"github.com/GuardaChuva22/avora/internal/store"
	"github.com/GuardaChuva22/avora/internal/web"
)

func resultsOf(e *toolEngine) []string {
	e.mu.Lock()
	defer e.mu.Unlock()
	return append([]string{}, e.results...)
}

func TestAgentSearchesAndReadsTheWeb(t *testing.T) {
	site := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/busca" {
			fmt.Fprint(w, `<div class="result"><a class="result__a" href="https://loja.example/n">Notebook X</a><a class="result__snippet">R$ 3.999</a></div>`)
			return
		}
		w.Header().Set("Content-Type", "text/html")
		fmt.Fprint(w, `<html><head><title>Notebook X</title></head><body><main><h1>Notebook X</h1><p>Preço: R$ 3.999</p><a href="/frete">Frete</a></main></body></html>`)
	}))
	defer site.Close()
	e := &toolEngine{calls: map[string][]llm.ToolCall{"Iris": {
		call("buscar_na_web", map[string]any{"busca": "notebook x preço"}),
		call("ler_pagina", map[string]any{"url": site.URL + "/produto"}),
		call("ler_pagina", map[string]any{"url": "http://127.0.0.1:3190/v1/agents"}),
	}}}
	s := chatServer(t, e)
	s.SetWeb(web.New(web.Options{AllowPrivate: true, SearchURL: site.URL + "/busca"}))
	c := agentConversation(t, s, "iris")
	r := <-sendAsync(t, s, c.ID, "quanto custa o notebook x?")
	res := resultsOf(e)
	if len(res) != 3 || !strings.Contains(res[0], "1. Notebook X\n   https://loja.example/n\n   R$ 3.999") {
		t.Fatalf("search: %q", res)
	}
	if !strings.Contains(res[1], "# Notebook X") || !strings.Contains(res[1], "Preço: R$ 3.999") || !strings.Contains(res[1], "- Frete: "+site.URL+"/frete") {
		t.Fatalf("page: %q", res[1])
	}
	// localhost is refused even when the test client could reach it.
	if !strings.HasPrefix(res[2], "Erro: ") {
		t.Fatalf("localhost read: %q", res[2])
	}
	if !strings.Contains(string(r.Assistant.Steps), "buscar_na_web") {
		t.Fatalf("steps: %s", string(r.Assistant.Steps))
	}
}

func TestAgentFilesStayInItsFolder(t *testing.T) {
	dir := t.TempDir()
	e := &toolEngine{calls: map[string][]llm.ToolCall{"Atlas": {
		call("escrever_arquivo", map[string]any{"caminho": "/notas/orcamentos.md", "conteudo": "# Orçamentos\n"}),
		call("escrever_arquivo", map[string]any{"caminho": "notas/orcamentos.md", "conteudo": "- Loja A: 3.999\n", "anexar": true}),
		call("listar_arquivos", map[string]any{}),
		call("ler_arquivo", map[string]any{"caminho": "notas/orcamentos.md"}),
		call("escrever_arquivo", map[string]any{"caminho": "../../fora.txt", "conteudo": "x"}),
		call("ler_arquivo", map[string]any{"caminho": "nada.txt"}),
	}}}
	s := chatServer(t, e)
	if err := s.SetWorkspace(dir); err != nil {
		t.Fatal(err)
	}
	c := agentConversation(t, s, "atlas")
	<-sendAsync(t, s, c.ID, "anota os orçamentos")
	res := resultsOf(e)
	if res[0] != "notas/orcamentos.md salvo (14 B)." || !strings.HasPrefix(res[1], "notas/orcamentos.md atualizado") {
		t.Fatalf("writes: %q", res[:2])
	}
	if !strings.Contains(res[2], "notas/\nnotas/orcamentos.md  (30 B,") || !strings.Contains(res[2], "Total: 30 B de 200.0 MB") {
		t.Fatalf("list: %q", res[2])
	}
	if res[3] != "notas/orcamentos.md (30 B):\n# Orçamentos\n- Loja A: 3.999\n" {
		t.Fatalf("read: %q", res[3])
	}
	if res[4] != "Erro: o caminho sai da sua pasta" || !strings.HasPrefix(res[5], "Erro: arquivo não encontrado") {
		t.Fatalf("outside / missing: %q", res[4:])
	}
	if b, err := os.ReadFile(filepath.Join(dir, "agents", "atlas", "notas", "orcamentos.md")); err != nil || string(b) != "# Orçamentos\n- Loja A: 3.999\n" {
		t.Fatalf("on disk: %q %v", b, err)
	}
	if _, err := os.Stat(filepath.Join(dir, "fora.txt")); err == nil {
		t.Fatal("wrote outside the folder")
	}
}

func TestDeletingAFileAsksTheOwner(t *testing.T) {
	dir := t.TempDir()
	e := &toolEngine{calls: map[string][]llm.ToolCall{"Atlas": {
		call("apagar_arquivo", map[string]any{"caminho": "velho.txt"}),
	}}}
	s := chatServer(t, e)
	_ = s.SetWorkspace(dir)
	os.MkdirAll(filepath.Join(dir, "agents", "atlas"), 0o700)
	os.WriteFile(filepath.Join(dir, "agents", "atlas", "velho.txt"), []byte("x"), 0o600)
	c := agentConversation(t, s, "atlas")
	answer := sendAsync(t, s, c.ID, "limpa a pasta")
	ap := waitApproval(t, s)
	if ap.Action != "apagar_arquivo" || ap.Summary != "Apagar velho.txt da pasta de Atlas" {
		t.Fatalf("approval: %+v", ap)
	}
	request(t, s, "POST", "/v1/approvals/"+ap.ID, adminTestToken, ApprovalDecision{Decision: "once"})
	<-answer
	if _, err := os.Stat(filepath.Join(dir, "agents", "atlas", "velho.txt")); err == nil {
		t.Fatal("file not deleted")
	}
}

func TestFileToolsNeedAWorkspace(t *testing.T) {
	e := &toolEngine{answers: map[string]string{"Atlas": "oi"}}
	s := chatServer(t, e)
	c := agentConversation(t, s, "atlas")
	<-sendAsync(t, s, c.ID, "oi")
	e.mu.Lock()
	tools := strings.Join(e.tools, ",")
	e.mu.Unlock()
	if strings.Contains(tools, "escrever_arquivo") || strings.Contains(tools, "executar") || !strings.Contains(tools, "buscar_na_web") || !strings.Contains(tools, "lembrar") {
		t.Fatalf("tools without a workspace: %s", tools)
	}
}

func TestAgentMemory(t *testing.T) {
	e := &toolEngine{calls: map[string][]llm.ToolCall{"Cora": {
		call("lembrar", map[string]any{"conteudo": "O dono recebe   no dia 5."}),
		call("lembrar", map[string]any{"conteudo": "o dono recebe no dia 5."}),
		call("recordar", map[string]any{"busca": "dia 5"}),
		call("esquecer", map[string]any{"id": "999"}),
	}}}
	s := chatServer(t, e)
	c := agentConversation(t, s, "cora")
	<-sendAsync(t, s, c.ID, "anota que recebo dia 5")
	res := resultsOf(e)
	if !strings.HasPrefix(res[0], "Guardado na memória (id=") || !strings.HasPrefix(res[1], "Já estava na memória") ||
		!strings.Contains(res[2], "O dono recebe no dia 5.") || !strings.HasPrefix(res[3], "Erro: ") {
		t.Fatalf("results: %q", res)
	}
	if block := s.memoryBlock(context.Background(), "cora"); !strings.Contains(block, "] O dono recebe no dia 5.") {
		t.Fatalf("prompt block: %q", block)
	}
	if s.memoryBlock(context.Background(), "atlas") != "" {
		t.Fatal("memories leak to other agents")
	}
	list := decode[[]store.AgentMemory](t, request(t, s, "GET", "/v1/agents/cora/memories", adminTestToken, nil))
	if len(list) != 1 || list[0].Source != "conversa" {
		t.Fatalf("api list: %+v", list)
	}
	id := fmt.Sprint(list[0].ID)
	if w := request(t, s, "DELETE", "/v1/agents/atlas/memories/"+id, adminTestToken, nil); w.Code != 404 {
		t.Fatalf("delete through another agent: %d", w.Code)
	}
	if w := request(t, s, "PUT", "/v1/agents/cora/memories/"+id, adminTestToken, MemoryRequest{Content: "Recebe dia 6."}); w.Code != 200 {
		t.Fatalf("edit: %d", w.Code)
	}
	if w := request(t, s, "POST", "/v1/agents/cora/memories", adminTestToken, MemoryRequest{Content: "Prefere Pix."}); w.Code != 201 {
		t.Fatalf("add: %d", w.Code)
	}
	if w := request(t, s, "DELETE", "/v1/agents/cora/memories/"+id, adminTestToken, nil); w.Code != 204 {
		t.Fatalf("delete: %d", w.Code)
	}
	if list = decode[[]store.AgentMemory](t, request(t, s, "GET", "/v1/agents/cora/memories", adminTestToken, nil)); len(list) != 1 || list[0].Source != "dono" {
		t.Fatalf("after: %+v", list)
	}
}

func TestExecutarRunsInTheVPSSandbox(t *testing.T) {
	e := &toolEngine{calls: map[string][]llm.ToolCall{"Atlas": {
		call("executar", map[string]any{"comando": "python3 -c 'print(2**10)'", "segundos": 90}),
		call("executar", map[string]any{"comando": "pip install requests", "rede": true}),
	}}}
	s := chatServer(t, e)
	_ = s.SetWorkspace(t.TempDir())
	pc := pairNode(t, s, "pc", "windows")
	request(t, s, "POST", "/v1/runtime/heartbeat", pc.Token, nil)
	vps := pairNode(t, s, "vps", "vps")
	request(t, s, "POST", "/v1/runtime/heartbeat", vps.Token, nil)
	c := agentConversation(t, s, "atlas")
	answer := sendAsync(t, s, c.ID, "quanto é 2 elevado a 10?")

	job := waitClaim(t, s, vps.Token)
	if job.Agent.Engine != "shell" || job.Agent.Mode != "offline" || job.Agent.Workdir != "agents/atlas" ||
		job.Agent.Prompt != "python3 -c 'print(2**10)'" || job.Agent.MaxMinutes != 2 {
		t.Fatalf("job: %+v", job.Agent)
	}
	request(t, s, "POST", "/v1/runtime/jobs/"+job.ID+"/complete", vps.Token, JobResult{Success: true, Output: "código de saída 0, 0.1s\n1024"})

	// With network it waits for the owner.
	ap := waitApproval(t, s)
	if ap.Action != "executar.rede" || !strings.Contains(ap.Summary, "pip install requests") {
		t.Fatalf("approval: %+v", ap)
	}
	request(t, s, "POST", "/v1/approvals/"+ap.ID, adminTestToken, ApprovalDecision{Decision: "deny"})
	<-answer
	res := resultsOf(e)
	if res[0] != "código de saída 0, 0.1s\n1024" || !strings.HasPrefix(res[1], "O dono negou") {
		t.Fatalf("results: %q", res)
	}
	if w := request(t, s, "POST", "/v1/runtime/claim", pc.Token, ClaimRequest{Agents: true}); w.Code != 204 {
		t.Fatalf("the PC got a shell job: %d", w.Code)
	}
}
