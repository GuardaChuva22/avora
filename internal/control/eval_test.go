package control

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/GuardaChuva22/avora/internal/engine"
	"github.com/GuardaChuva22/avora/internal/llm"
	"github.com/GuardaChuva22/avora/internal/store"
)

// The eval suite: real requests sent to the real brain (the native model),
// checking which tool it reaches for first and with what. It never runs a
// tool; the first call ends the scenario. Run it where the model key lives:
//
//	AVORA_EVAL=1 go test ./internal/control -run TestEval -v
//
// AVORA_EVAL_ONLY=name1,name2 runs a few; AVORA_EVAL_MIN=0.8 fails below that
// pass rate.

const noTool = "-" // the model should answer without tools

type evalCase struct {
	name     string
	agent    string
	msg      string
	memories []string
	want     []string // acceptable first tools; noTool = a direct answer
	check    func(args map[string]any, answer string) string
}

func argHas(key string, words ...string) func(map[string]any, string) string {
	return func(a map[string]any, _ string) string {
		v := strings.ToLower(fmt.Sprint(a[key]))
		for _, w := range words {
			if !strings.Contains(v, strings.ToLower(w)) {
				return fmt.Sprintf("%s=%q sem %q", key, v, w)
			}
		}
		return ""
	}
}

func answerHas(words ...string) func(map[string]any, string) string {
	return func(_ map[string]any, ans string) string {
		low := strings.ToLower(ans)
		for _, w := range words {
			if !strings.Contains(low, strings.ToLower(w)) {
				return fmt.Sprintf("resposta sem %q", w)
			}
		}
		return ""
	}
}

var evalCases = []evalCase{
	// Web
	{name: "preco-atual", agent: "iris", msg: "quanto tá custando um PS5 slim hoje no Brasil?", want: []string{"buscar_na_web"}, check: argHas("busca", "ps5")},
	{name: "noticia", agent: "vega", msg: "quais as notícias de hoje sobre a Selic?", want: []string{"buscar_na_web"}, check: argHas("busca", "selic")},
	{name: "le-link", agent: "iris", msg: "resume esse artigo pra mim: https://go.dev/blog/routing-enhancements", want: []string{"ler_pagina"}, check: argHas("url", "go.dev/blog/routing-enhancements")},
	{name: "clima", agent: "vega", msg: "vai chover amanhã em Curitiba?", want: []string{"buscar_na_web"}},
	{name: "comparativo", agent: "iris", msg: "compara os planos pós-pago de 50GB da Claro, Vivo e Tim", want: []string{"buscar_na_web"}},
	{name: "le-e-salva", agent: "iris", msg: "lê https://example.com e salva um resumo na sua pasta", want: []string{"ler_pagina"}, check: argHas("url", "example.com")},

	// Answers without tools
	{name: "bom-dia", agent: "vega", msg: "bom dia! tudo certo por aí?", want: []string{noTool}},
	{name: "conhecimento", agent: "iris", msg: "o que é uma função hash, em duas frases?", want: []string{noTool}},
	{name: "conta-simples", agent: "cora", msg: "quanto é 15% de 3.420?", want: []string{noTool, "executar"}},
	{name: "planejar", agent: "vega", msg: "me ajuda a planejar a semana: tenho prova quinta e entrega de projeto sexta", want: []string{noTool, "lembrar"}},
	{name: "sem-email", agent: "vega", msg: "manda um e-mail pro meu chefe dizendo que vou me atrasar amanhã", want: []string{noTool},
		check: func(_ map[string]any, ans string) string {
			low := strings.ToLower(ans)
			for _, lie := range []string{"enviei", "e-mail enviado", "email enviado", "mandei o e-mail", "mandei o email"} {
				if strings.Contains(low, lie) {
					return "disse que enviou: " + lie
				}
			}
			return ""
		}},

	// Memory
	{name: "lembra-preferencia", agent: "vega", msg: "a partir de agora me chama de Gui e responde sempre curto", want: []string{"lembrar"}, check: argHas("conteudo", "gui")},
	{name: "lembra-fato", agent: "cora", msg: "meu salário cai todo dia 5 e o aluguel é 2.100", want: []string{"lembrar"}, check: argHas("conteudo", "5")},
	{name: "usa-memoria", agent: "vega", msg: "onde eu moro mesmo?", memories: []string{"O dono se chama Gui e mora em Curitiba."}, want: []string{noTool}, check: answerHas("curitiba")},
	{name: "recorda-antiga", agent: "vega", msg: "o que eu te falei sobre o meu carro uns meses atrás?", want: []string{"recordar"}, check: argHas("busca", "carro")},
	{name: "corrige-memoria", agent: "vega", msg: "me mudei, não moro mais em Curitiba, agora é Florianópolis", memories: []string{"O dono mora em Curitiba."}, want: []string{"esquecer", "lembrar"}},

	// Files
	{name: "anota", agent: "atlas", msg: "anota numa nota na sua pasta essas ideias: app de receitas, bot de cripto", want: []string{"escrever_arquivo"}, check: argHas("conteudo", "receitas")},
	{name: "lista-pasta", agent: "atlas", msg: "o que tem na sua pasta?", want: []string{"listar_arquivos"}},
	{name: "le-arquivo", agent: "atlas", msg: "lê o arquivo notas/ideias.md e me diz o que tem", want: []string{"ler_arquivo"}, check: argHas("caminho", "ideias.md")},
	{name: "apaga-arquivo", agent: "atlas", msg: "apaga o rascunho.txt da sua pasta", want: []string{"apagar_arquivo", "listar_arquivos"}},

	// Sandbox
	{name: "roda-python", agent: "atlas", msg: "roda um python que gere 5 senhas aleatórias de 16 caracteres", want: []string{"executar"}, check: argHas("comando", "python")},
	{name: "juros-tabela", agent: "cora", msg: "calcula mês a mês os juros compostos de R$ 10.000 a 1,1% ao mês por 37 meses e me dá a tabela", want: []string{"executar"}},
	{name: "pip-rede", agent: "atlas", msg: "instala a lib requests no seu sandbox e testa um GET no google.com", want: []string{"executar"},
		check: func(a map[string]any, _ string) string {
			if a["rede"] != true {
				return "sem rede=true"
			}
			return ""
		}},

	// Heavy work goes to the CLIs
	{name: "delega-projeto", agent: "atlas", msg: "cria um projeto Next.js completo de landing page pra minha barbearia, com testes, no meu PC", want: []string{"claude_code", "codex"}},

	// Routines, responsibilities, team
	{name: "rotina-diaria", agent: "vega", msg: "todo dia às 8h me manda um resumo das notícias de tecnologia", want: []string{"criar_rotina"}, check: argHas("horario", "08:00")},
	{name: "lembrete", agent: "vega", msg: "me lembra amanhã às 15h de ligar pro dentista", want: []string{"criar_rotina"}, check: func(a map[string]any, _ string) string {
		tomorrow := time.Now().In(time.FixedZone("BRT", -3*3600)).AddDate(0, 0, 1).Format(time.DateOnly)
		if why := argHas("horario", "15:00")(a, ""); why != "" {
			return why
		}
		if a["data"] != tomorrow {
			return fmt.Sprintf("data=%v, esperava %s (uma vez só)", a["data"], tomorrow)
		}
		return ""
	}},
	{name: "responsabilidade", agent: "cora", msg: "quero que você cuide pra eu não gastar mais de 3 mil por mês no cartão", want: []string{"definir_responsabilidade"}, check: argHas("objetivo", "3")},
	{name: "consulta-colega", agent: "vega", msg: "pergunta pra Cora quanto eu já gastei esse mês", want: []string{"consultar_colega"}, check: argHas("funcionario", "cora")},
	{name: "estado", agent: "vega", msg: "como tá o Avora? algum nó offline?", want: []string{"estado_do_avora"}},
	{name: "ver-rotinas", agent: "vega", msg: "quais rotinas eu tenho ligadas?", want: []string{"ver_rotinas"}},
	{name: "pausa-rotina", agent: "vega", msg: "pausa a rotina do resumo de notícias", want: []string{"ver_rotinas", "alterar_rotina"}},
}

func evalEngine(t *testing.T) *engine.Native {
	key := os.Getenv("AVORA_NATIVE_API_KEY")
	if key == "" {
		key = os.Getenv("NVIDIA_API_KEY")
	}
	if key == "" {
		t.Fatal("no model key (AVORA_NATIVE_API_KEY or NVIDIA_API_KEY)")
	}
	or := func(name, def string) string {
		if v := os.Getenv(name); v != "" {
			return v
		}
		return def
	}
	rpm, _ := strconv.Atoi(or("AVORA_NATIVE_RPM", "30"))
	p, err := llm.NewProvider(or("AVORA_NATIVE_PROVIDER", "nvidia"), or("AVORA_NATIVE_BASE_URL", "https://integrate.api.nvidia.com/v1"), key, rpm)
	if err != nil {
		t.Fatal(err)
	}
	return engine.NewNative(p, or("AVORA_NATIVE_MODEL", "nvidia/nemotron-3-ultra-550b-a55b"))
}

func TestEval(t *testing.T) {
	if os.Getenv("AVORA_EVAL") == "" {
		t.Skip("AVORA_EVAL=1 runs the suite against the real model")
	}
	only := map[string]bool{}
	for _, n := range strings.Split(os.Getenv("AVORA_EVAL_ONLY"), ",") {
		if n = strings.TrimSpace(n); n != "" {
			only[n] = true
		}
	}
	eng := evalEngine(t)
	s := chatServer(t, nil)
	if err := s.SetWorkspace(t.TempDir()); err != nil {
		t.Fatal(err)
	}
	ctx := context.Background()
	team, err := s.store.ListAgents(ctx)
	if err != nil {
		t.Fatal(err)
	}
	byID := map[string]store.Agent{}
	for _, a := range team {
		byID[a.ID] = a
	}

	passed, ran := 0, 0
	var report []string
	for _, c := range evalCases {
		if len(only) > 0 && !only[c.name] {
			continue
		}
		ran++
		a := byID[c.agent]
		var saved []store.AgentMemory
		for _, m := range c.memories {
			mem, err := s.store.AddMemory(ctx, store.AgentMemory{AgentID: a.ID, Content: m, Source: "conversa"}, "eval", s.now())
			if err != nil {
				t.Fatal(err)
			}
			saved = append(saved, mem)
		}
		system := nativeSystem(a, team) + s.memoryBlock(ctx, a.ID)
		tr := &turn{agent: a, team: team}
		history := []store.ChatMessage{{ID: 1, Role: "user", Content: c.msg}}

		cctx, cancel := context.WithTimeout(ctx, 3*time.Minute)
		var first *llm.ToolCall
		started := time.Now()
		resp, err := eng.Run(cctx, system, history, s.toolsFor(tr), func(_ context.Context, call llm.ToolCall) string {
			if first == nil {
				first = &call
			}
			cancel() // the first call is what we grade
			return ""
		}, nil)
		cancel()
		took := time.Since(started).Round(100 * time.Millisecond)
		for _, m := range saved {
			_, _ = s.store.DeleteMemory(ctx, m.ID, "eval", s.now())
		}

		got, args := noTool, map[string]any{}
		if first != nil {
			got = first.Function.Name
			_ = json.Unmarshal([]byte(first.Function.Arguments), &args)
		} else if err != nil {
			report = append(report, fmt.Sprintf("ERRO  %-20s %v", c.name, err))
			continue
		}
		why := ""
		ok := false
		for _, w := range c.want {
			ok = ok || w == got
		}
		if !ok {
			why = "esperava " + strings.Join(c.want, " ou ")
		} else if c.check != nil {
			why = c.check(args, resp.Content)
		}
		mark := "ok   "
		if why == "" {
			passed++
		} else {
			mark = "FALHA"
		}
		detail := got
		if first != nil {
			detail += " " + truncate(first.Function.Arguments, 140)
		} else {
			detail += " «" + truncate(strings.ReplaceAll(resp.Content, "\n", " "), 140) + "»"
		}
		line := fmt.Sprintf("%s %-20s %6s  %s", mark, c.name, took, detail)
		if why != "" {
			line += "\n      → " + why
		}
		report = append(report, line)
		t.Log(line)
	}
	rate := float64(passed) / float64(max(ran, 1))
	summary := fmt.Sprintf("\n%s\n\n%d de %d passaram (%.0f%%)", strings.Join(report, "\n"), passed, ran, rate*100)
	t.Log(summary)
	if min, err := strconv.ParseFloat(os.Getenv("AVORA_EVAL_MIN"), 64); err == nil && rate < min {
		t.Fatalf("pass rate %.2f below %.2f", rate, min)
	}
}
