package control

import (
	"strings"
	"testing"

	"github.com/GuardaChuva22/avora/internal/llm"
	"github.com/GuardaChuva22/avora/internal/store"
)

func TestToolCallsFollowTheCatalog(t *testing.T) {
	c := ToolCall{Tool: "yt-dlp", Action: "baixar_video", Args: map[string]any{"url": "https://youtu.be/x{saida}", "saida": "videos/aula 1.mp4"}}
	argv, ins, outs, err := c.Argv()
	if err != nil {
		t.Fatal(err)
	}
	joined := strings.Join(argv, " ")
	if !strings.Contains(joined, "-o videos/aula 1.mp4 https://youtu.be/x{saida}") || !strings.HasPrefix(joined, "--ignore-config") ||
		len(ins) != 0 || len(outs) != 1 {
		t.Fatalf("argv: %q ins=%v outs=%v", argv, ins, outs)
	}
	// Optional values take their defaults.
	c = ToolCall{Tool: "tesseract", Action: "ocr", Args: map[string]any{"entrada": "print.png"}}
	if argv, _, _, err := c.Argv(); err != nil || strings.Join(argv, " ") != "print.png stdout -l por" {
		t.Fatalf("defaults: %q %v", argv, err)
	}
	c = ToolCall{Tool: "qpdf", Action: "juntar", Args: map[string]any{"entradas": []any{"a.pdf", "b.pdf"}, "saida": "ab.pdf"}}
	if argv, ins, _, err := c.Argv(); err != nil || strings.Join(argv, " ") != "--empty --pages a.pdf b.pdf -- ab.pdf" || len(ins) != 2 {
		t.Fatalf("list: %q %v", argv, err)
	}
	for _, bad := range []ToolCall{
		{Tool: "ffmpeg", Action: "converter", Args: map[string]any{"entrada": "../segredo.mp4", "saida": "x.mp4"}},
		{Tool: "ffmpeg", Action: "converter", Args: map[string]any{"entrada": "-i", "saida": "x.mp4"}},
		{Tool: "ffmpeg", Action: "converter", Args: map[string]any{"entrada": "C:/x.mp4", "saida": "x.mp4"}},
		{Tool: "ffmpeg", Action: "converter", Args: map[string]any{"entrada": "concat:a|b", "saida": "x.mp4"}},
		{Tool: "ffmpeg", Action: "converter", Args: map[string]any{"entrada": "/etc/passwd", "saida": "x.mp4"}},
		{Tool: "ffmpeg", Action: "converter", Args: map[string]any{"entrada": "a.mp4"}},
		{Tool: "ffmpeg", Action: "converter", Args: map[string]any{"entrada": "a.mp4", "saida": "b.mp4", "extra": "-y"}},
		{Tool: "ffmpeg", Action: "cortar", Args: map[string]any{"entrada": "a.mp4", "inicio": "1; rm", "fim": "2", "saida": "b.mp4"}},
		{Tool: "yt-dlp", Action: "info", Args: map[string]any{"url": "file:///etc/passwd"}},
		{Tool: "yt-dlp", Action: "baixar_video", Args: map[string]any{"url": "https://x.com/v", "saida": "%(title)s.mp4"}},
		{Tool: "yt-dlp", Action: "rodar", Args: map[string]any{}},
		{Tool: "bash", Action: "x"},
	} {
		if _, _, _, err := bad.Argv(); err == nil {
			t.Fatalf("accepted %+v", bad)
		}
	}
}

func TestSkillMarkdownImport(t *testing.T) {
	name, desc, body := parseSkillMarkdown("\ufeff---\nname: relatorio-mensal\ndescription: \"Fecha o mês\"\n---\n\n# Passos\n1. Some.\n")
	if name != "relatorio-mensal" || desc != "Fecha o mês" || body != "# Passos\n1. Some." {
		t.Fatalf("parsed: %q %q %q", name, desc, body)
	}
	if _, _, body := parseSkillMarkdown("só texto"); body != "só texto" {
		t.Fatalf("no frontmatter: %q", body)
	}
}

func TestSkillsAndNodeTools(t *testing.T) {
	e := &toolEngine{calls: map[string][]llm.ToolCall{
		"Atlas": {
			call("skill_ler", map[string]any{"nome": "Fechar Mês"}),
			call("skill_ler", map[string]any{"nome": "lado-secreto"}),
			call("skill_criar", map[string]any{"nome": "Resumo semanal", "descricao": "Resumir a semana do projeto", "instrucoes": "1. Leia.\n2. Resuma."}),
		},
		"Cora": {
			call("ferramenta", map[string]any{"ferramenta": "yt-dlp", "acao": "baixar_audio", "args": map[string]any{"url": "https://youtu.be/abc"}}),
			call("ferramenta", map[string]any{"ferramenta": "ffmpeg", "acao": "converter", "args": map[string]any{"entrada": "a.mp4", "saida": "a.webm"}}),
		},
	}}
	s := chatServer(t, e)
	pc := pairNode(t, s, "pc", "windows")
	vps := pairNode(t, s, "vps", "vps")
	request(t, s, "POST", "/v1/runtime/heartbeat", vps.Token, HeartbeatRequest{Tools: []ToolInfo{{ID: "yt-dlp", Version: "2026.10.26"}}})
	request(t, s, "POST", "/v1/runtime/heartbeat", pc.Token, HeartbeatRequest{Tools: []ToolInfo{{ID: "yt-dlp", Version: "2026.10.01"}}})
	if w := request(t, s, "POST", "/v1/runtime/heartbeat", pc.Token, HeartbeatRequest{Tools: []ToolInfo{{ID: "bash"}}}); w.Code != 400 {
		t.Fatalf("unknown tool: %d", w.Code)
	}
	request(t, s, "POST", "/v1/runtime/heartbeat", pc.Token, HeartbeatRequest{Tools: []ToolInfo{{ID: "yt-dlp", Version: "2026.10.01"}}})

	decode[store.Project](t, request(t, s, "POST", "/v1/projects", adminTestToken, ProjectRequest{Name: "Trabalho"}))
	decode[store.Project](t, request(t, s, "POST", "/v1/projects", adminTestToken, ProjectRequest{Name: "Lado"}))
	request(t, s, "PUT", "/v1/agents/atlas/project", adminTestToken, MoveRequest{ProjectID: "trabalho"})
	lado := "lado"
	w := request(t, s, "POST", "/v1/skills", adminTestToken, SkillRequest{Markdown: "---\nname: Fechar Mês\ndescription: Fecha as contas do mês\n---\n1. Some os gastos."})
	if w.Code != 201 {
		t.Fatalf("create: %d %s", w.Code, w.Body.String())
	}
	if sk := decode[store.Skill](t, w); sk.ID != "fechar-mes" || sk.Body != "1. Some os gastos." || sk.Author != "admin" {
		t.Fatalf("skill: %+v", sk)
	}
	decode[store.Skill](t, request(t, s, "POST", "/v1/skills", adminTestToken, SkillRequest{Name: "lado-secreto", Description: "Só do Lado", Body: "x", ProjectID: &lado}))
	if w := request(t, s, "POST", "/v1/skills", adminTestToken, SkillRequest{Name: "fechar mes", Description: "d", Body: "b"}); w.Code != 409 {
		t.Fatalf("duplicate: %d", w.Code)
	}
	if w := request(t, s, "POST", "/v1/skills", adminTestToken, SkillRequest{Name: "x", Description: "", Body: "b"}); w.Code != 400 {
		t.Fatalf("no description: %d", w.Code)
	}

	atlas, _ := s.store.GetAgent(t.Context(), "atlas")
	if block := s.skillsBlock(t.Context(), atlas); !strings.Contains(block, "- fechar-mes: Fecha as contas do mês") || strings.Contains(block, "lado-secreto") {
		t.Fatalf("atlas's skills: %s", block)
	}
	cora, _ := s.store.GetAgent(t.Context(), "cora")
	if block := s.skillsBlock(t.Context(), cora); !strings.Contains(block, "- lado-secreto: Só do Lado (projeto Lado)") {
		t.Fatalf("universal skills: %s", block)
	}

	// Atlas reads a skill, cannot read another project's, and saving a
	// skill waits for the owner; it lands in his project.
	c := agentConversation(t, s, "atlas")
	answer := sendAsync(t, s, c.ID, "guarda isso")
	ap := waitApproval(t, s)
	if ap.Action != "skill_criar" || !strings.Contains(ap.Summary, "Guardar a skill «resumo-semanal» no projeto Trabalho") {
		t.Fatalf("approval: %+v", ap)
	}
	request(t, s, "POST", "/v1/approvals/"+ap.ID, adminTestToken, ApprovalDecision{Decision: "once"})
	r := <-answer
	if !strings.Contains(r.Assistant.Content, "1. Some os gastos.") || !strings.Contains(r.Assistant.Content, `não há skill "lado-secreto"`) ||
		!strings.Contains(r.Assistant.Content, "Skill resumo-semanal criada") {
		t.Fatalf("answer: %s", r.Assistant.Content)
	}
	if sk, err := s.store.GetSkill(t.Context(), "resumo-semanal"); err != nil || sk.ProjectID != "trabalho" || sk.Author != "agent:atlas" {
		t.Fatalf("saved: %+v %v", sk, err)
	}

	// A routine follows a skill: the steps go with its prompt.
	sk := "fechar-mes"
	rt := decode[store.Routine](t, request(t, s, "POST", "/v1/routines", adminTestToken, RoutineRequest{
		AgentID: "atlas", Title: "Mês", Prompt: "Feche o mês.", Schedule: store.Schedule{At: "08:00"}, Skill: &sk}))
	if rt.Skill != "fechar-mes" || !strings.Contains(s.routinePrompt(t.Context(), rt), "Siga a skill fechar-mes") {
		t.Fatalf("routine: %+v", rt)
	}
	secret := "lado-secreto"
	if w := request(t, s, "POST", "/v1/routines", adminTestToken, RoutineRequest{
		AgentID: "atlas", Title: "X", Prompt: "x", Schedule: store.Schedule{At: "08:00"}, Skill: &secret}); w.Code != 400 {
		t.Fatalf("other project's skill: %d", w.Code)
	}

	// Cora (universal, no project) runs yt-dlp on the PC, the only node
	// with it; ffmpeg is nowhere.
	c = agentConversation(t, s, "cora")
	answer = sendAsync(t, s, c.ID, "baixa o áudio")
	job := waitClaim(t, s, pc.Token)
	got, err := DecodeToolCall(job.Agent.Prompt)
	if err != nil || job.Agent.Engine != "tool" || got.Tool != "yt-dlp" || got.Action != "baixar_audio" || job.Agent.Workdir != "agents/cora" ||
		job.Agent.Mode != "net" {
		t.Fatalf("job: %+v %+v", job.Agent, got)
	}
	request(t, s, "POST", "/v1/runtime/jobs/"+job.ID+"/complete", pc.Token, JobResult{Success: true, Output: "código de saída 0\nArquivos novos: a.mp3"})
	r = <-answer
	if !strings.Contains(r.Assistant.Content, "Rodou em sua pasta agents/cora (PC)") || !strings.Contains(r.Assistant.Content, "nenhum nó online tem ffmpeg") {
		t.Fatalf("answer: %s", r.Assistant.Content)
	}
	if !strings.Contains(strings.Join(e.tools, ","), "ferramenta") {
		t.Fatalf("tools offered: %v", e.tools)
	}
	list := decode[[]nodeToolStatus](t, request(t, s, "GET", "/v1/tools", adminTestToken, nil))
	if len(list) != len(NodeToolCatalog) || !list[0].Nodes[0].Has || list[0].Nodes[0].Version != "2026.10.01" || list[1].Nodes[0].Has {
		t.Fatalf("tools: %+v", list)
	}
}
