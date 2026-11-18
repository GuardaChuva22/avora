package control

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/GuardaChuva22/avora/internal/llm"
	"github.com/GuardaChuva22/avora/internal/store"
)

func TestProjectsIsolateTheirAgents(t *testing.T) {
	e := &toolEngine{
		calls: map[string][]llm.ToolCall{"Atlas": {
			call("consultar_colega", map[string]any{"funcionario": "Iris", "pergunta": "oi?"}),
			call("consultar_colega", map[string]any{"funcionario": "Cora", "pergunta": "quanto custa?"}),
			call("claude_code", map[string]any{"tarefa": "Leia o README", "pasta": "docs"}),
		}},
		answers: map[string]string{"Cora": "R$ 10.", "Iris": "olá"},
	}
	s := chatServer(t, e)
	ws := t.TempDir()
	if err := s.SetWorkspace(ws); err != nil {
		t.Fatal(err)
	}
	pc := pairNode(t, s, "pc", "windows")
	vps := pairNode(t, s, "vps", "vps")
	request(t, s, "POST", "/v1/runtime/heartbeat", pc.Token, nil)

	rules := "# Trabalho\nSempre em TypeScript."
	w := request(t, s, "POST", "/v1/projects", adminTestToken, ProjectRequest{Name: "Trabalho", Color: "#7FD1FF", Instructions: &rules,
		Folders: []store.ProjectFolder{{Node: pc.NodeID, Path: `C:\Trabalho\`}, {Node: pc.NodeID, Path: `C:/Trabalho/docs`, Label: "docs"}}})
	if w.Code != 201 {
		t.Fatalf("create: %d %s", w.Code, w.Body.String())
	}
	work := decode[store.Project](t, w)
	if work.ID != "trabalho" || work.Color != "#7fd1ff" || work.Folders[0].Path != `C:\Trabalho` || work.Folders[1].Path != `C:\Trabalho\docs` {
		t.Fatalf("project: %+v", work)
	}
	// A VPS project keeps AGENTS.md in its folder, and the file wins.
	decode[store.Project](t, request(t, s, "POST", "/v1/projects", adminTestToken, ProjectRequest{Name: "Lado", Instructions: &rules,
		Folders: []store.ProjectFolder{{Node: vps.NodeID, Path: "projects/lado"}}}))
	file := filepath.Join(ws, "projects", "lado", AgentsFile)
	if data, err := os.ReadFile(file); err != nil || !strings.Contains(string(data), "TypeScript") {
		t.Fatalf("AGENTS.md: %q %v", data, err)
	}
	_ = os.WriteFile(file, []byte("# Lado\nEditado à mão."), 0o600)
	if got := decode[store.Project](t, request(t, s, "GET", "/v1/projects/lado", adminTestToken, nil)); got.Instructions != "# Lado\nEditado à mão." {
		t.Fatalf("file edits: %q", got.Instructions)
	}
	for _, bad := range []ProjectRequest{
		{Name: ""},
		{Name: "X", Folders: []store.ProjectFolder{{Node: pc.NodeID, Path: `relativo\pasta`}}},
		{Name: "X", Folders: []store.ProjectFolder{{Node: vps.NodeID, Path: "../etc"}}},
		{Name: "X", Folders: []store.ProjectFolder{{Node: "nada", Path: `C:\x`}}},
	} {
		if w := request(t, s, "POST", "/v1/projects", adminTestToken, bad); w.Code != 400 {
			t.Fatalf("accepted %+v: %d", bad, w.Code)
		}
	}

	for agent, project := range map[string]string{"atlas": "trabalho", "iris": "lado"} {
		if w := request(t, s, "PUT", "/v1/agents/"+agent+"/project", adminTestToken, MoveRequest{ProjectID: project}); w.Code != 200 {
			t.Fatalf("move %s: %d %s", agent, w.Code, w.Body.String())
		}
	}
	if w := request(t, s, "PUT", "/v1/agents/atlas/project", adminTestToken, MoveRequest{ProjectID: "nada"}); w.Code != 404 {
		t.Fatalf("unknown project: %d", w.Code)
	}
	created := decode[store.Agent](t, request(t, s, "POST", "/v1/agents", adminTestToken, AgentRequest{Name: "Lia", ProjectID: "trabalho"}))
	if created.ProjectID != "trabalho" {
		t.Fatalf("hired into a project: %+v", created)
	}

	atlas, _ := s.store.GetAgent(t.Context(), "atlas")
	block := s.projectBlock(t.Context(), atlas)
	if !strings.Contains(block, "## Projeto: Trabalho") || !strings.Contains(block, `principal: C:\Trabalho (pc)`) ||
		!strings.Contains(block, "Sempre em TypeScript.") || !strings.Contains(block, `docs: C:\Trabalho\docs`) {
		t.Fatalf("project block: %s", block)
	}
	all, _ := s.store.ListAgents(t.Context())
	if line := teamLine(atlas, visibleTeam(atlas, all)); strings.Contains(line, "Iris") || !strings.Contains(line, "Cora") || !strings.Contains(line, "Lia") {
		t.Fatalf("atlas's team: %s", line)
	}
	vega, _ := s.store.GetAgent(t.Context(), "vega")
	if block := s.projectBlock(t.Context(), vega); !strings.Contains(block, "- Trabalho: Atlas, Lia") || !strings.Contains(block, "- Lado: Iris") {
		t.Fatalf("universal block: %s", block)
	}

	// Atlas cannot reach Iris (another project) but reaches Cora
	// (universal), and Claude Code runs in the project's folder on the PC.
	c := agentConversation(t, s, "atlas")
	answer := sendAsync(t, s, c.ID, "faz aí")
	job := waitClaim(t, s, pc.Token)
	if job.Agent.Folder != `C:\Trabalho\docs` || job.Agent.Workdir != "" || !strings.Contains(job.Agent.Prompt, "A tarefa é do projeto Trabalho") ||
		!strings.Contains(job.Agent.Prompt, "Sempre em TypeScript.") {
		t.Fatalf("job: %+v", job.Agent)
	}
	request(t, s, "POST", "/v1/runtime/jobs/"+job.ID+"/complete", pc.Token, JobResult{Success: true, Output: "Li."})
	r := <-answer
	if !strings.Contains(r.Assistant.Content, "Iris é de outro projeto") || !strings.Contains(r.Assistant.Content, "Cora respondeu:\nR$ 10.") ||
		!strings.Contains(r.Assistant.Content, "Li.") {
		t.Fatalf("answer: %s", r.Assistant.Content)
	}

	if w := request(t, s, "DELETE", "/v1/projects/trabalho", adminTestToken, nil); w.Code != 204 {
		t.Fatalf("delete: %d", w.Code)
	}
	if a, _ := s.store.GetAgent(t.Context(), "atlas"); a.ProjectID != "" {
		t.Fatalf("atlas after delete: %+v", a)
	}
}
