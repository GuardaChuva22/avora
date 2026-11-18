package control

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"log"
	"net/http"
	"strings"
	"sync"
	"time"

	"github.com/GuardaChuva22/avora/internal/scheduler"
	"github.com/GuardaChuva22/avora/internal/store"
)

// How an agent that runs on a node (Claude Code, Codex) answers a message:
//
//  1. The conversation is marked with the job's id, so it never runs two
//     answers at once.
//  2. An agent.run job goes to the node that holds the conversation's CLI
//     session (resumed there) or, if it is offline, to a new session that
//     gets the recent messages as context.
//  3. Steps stream to the open chat as they happen; when the job ends (done,
//     failed, stopped or timed out) the answer is stored exactly once.

const (
	// AgentReplyMinutes bounds one answer of a CLI agent.
	AgentReplyMinutes = 30
	// freshContextMessages is how much history a new CLI session receives.
	freshContextMessages = 12
	maxSteps             = 200
)

// jobHub delivers a running job's steps and answer to open chat streams.
type jobHub struct {
	mu   sync.Mutex
	subs map[string]map[chan hubEvent]struct{}
}

type hubEvent struct {
	kind string // "step" or "done"
	data any
}

func (h *jobHub) subscribe(jobID string) (<-chan hubEvent, func()) {
	ch := make(chan hubEvent, 1024)
	h.mu.Lock()
	if h.subs == nil {
		h.subs = map[string]map[chan hubEvent]struct{}{}
	}
	if h.subs[jobID] == nil {
		h.subs[jobID] = map[chan hubEvent]struct{}{}
	}
	h.subs[jobID][ch] = struct{}{}
	h.mu.Unlock()
	return ch, func() {
		h.mu.Lock()
		delete(h.subs[jobID], ch)
		if len(h.subs[jobID]) == 0 {
			delete(h.subs, jobID)
		}
		h.mu.Unlock()
	}
}

func (h *jobHub) publish(jobID string, ev hubEvent) {
	h.mu.Lock()
	subs := make([]chan hubEvent, 0, len(h.subs[jobID]))
	for ch := range h.subs[jobID] {
		subs = append(subs, ch)
	}
	h.mu.Unlock()
	for _, ch := range subs {
		if ev.kind == "done" { // the answer itself is worth a short wait
			select {
			case ch <- ev:
			case <-time.After(5 * time.Second):
			}
			continue
		}
		select {
		case ch <- ev:
		default: // a slow reader misses a step, never the server
		}
	}
}

// teamLine lists the other agents so each one knows who to point to.
func teamLine(self store.Agent, team []store.Agent) string {
	names := []string{}
	for _, a := range team {
		if a.ID != self.ID {
			names = append(names, a.Name+" ("+a.Role+")")
		}
	}
	if len(names) == 0 {
		return ""
	}
	return "\nSeus colegas no time: " + strings.Join(names, ", ") + "."
}

// agentPreamble is who the agent is; native agents get it as the system
// prompt, CLI agents at the start of a new session.
func agentPreamble(a store.Agent, team []store.Agent) string {
	role := a.Role
	if role == "" {
		role = "funcionário"
	}
	goals := ""
	if len(a.Responsibilities) > 0 {
		goals = "\n\nSuas responsabilidades permanentes:"
		for _, r := range a.Responsibilities {
			goals += "\n- " + responsibilityText(r)
		}
	}
	return fmt.Sprintf(`Você é %s, %s no time do Avora: os funcionários digitais 24h de uma única pessoa, o seu dono.
Responda sempre em português do Brasil.%s

%s

%s%s%s`, a.Name, role, teamLine(a, team), humanRule, a.Persona, goals, styleBlock(a.Style))
}

// humanRule is the owner's rule for every agent, everywhere: talk like a
// person, not an assistant (D51). It comes before the persona and the style,
// so an agent's own voice still shapes how it says things.
const humanRule = `Você DEVE se comunicar como uma pessoa real, de forma natural, direta e objetiva.

Regras de comunicação:
- Fale como um ser humano normal, não como um robô ou assistente corporativo.
- Seja conciso. Responda apenas o necessário, sem explicações longas, excesso de detalhes ou informações que não foram solicitadas.
- Use linguagem simples, fluida e espontânea, como em uma conversa comum. Evite formalidades, frases artificiais e entusiasmo exagerado.
- Vá direto ao ponto. Não use introduções desnecessárias como "Claro!", "Ótima pergunta!" ou "Com certeza!".
- Prefira respostas curtas, em poucos parágrafos. Só use listas, títulos ou formatação elaborada quando realmente ajudarem.
- Não transforme perguntas simples em respostas complexas. Adapte o tamanho da resposta à complexidade da pergunta.
- Não termine respostas oferecendo serviços ou sugerindo próximos passos sem necessidade. Evite frases como "Quer que eu faça um relatório?", "Posso gerar um PDF", "Se quiser, posso..." ou qualquer oferta automática semelhante.
- Não faça perguntas apenas para manter a conversa. Pergunte somente quando uma informação for realmente necessária para responder ou executar a solicitação.
- Não repita o que o usuário disse nem explique o óbvio.
- Seja natural sem forçar gírias, intimidade, humor ou personalidade.
- Quando o usuário pedir algo complexo, seja completo, mas continue claro, prático e sem enrolação.
- Se não souber algo, admita de maneira simples, sem discursos ou justificativas excessivas.

Princípio principal: responda como uma pessoa inteligente conversando normalmente com outra. Priorize naturalidade, clareza e brevidade. Não tente parecer prestativo o tempo todo; apenas seja útil e responda ao que foi pedido.`

// styleBlock is how the owner wants this agent to talk (D50).
func styleBlock(st store.AgentStyle) string {
	lines := []string{}
	if st.Voice != "" {
		lines = append(lines, "- Seu jeito de falar: "+st.Voice+". Fale assim sempre, com as suas palavras, sem soar como assistente.")
	}
	switch st.Emoji {
	case "nunca":
		lines = append(lines, "- Não use emoji.")
	case "bastante":
		lines = append(lines, "- Use emoji com naturalidade, como numa conversa de WhatsApp (um ou dois por mensagem, onde cabem).")
	}
	switch st.Length {
	case "curta":
		lines = append(lines, "- Escreva curto: vá direto ao ponto, sem explicar o que não foi pedido.")
	case "longa":
		lines = append(lines, "- Pode se estender: explique o raciocínio e dê os detalhes quando ajudam.")
	}
	if len(lines) == 0 {
		return ""
	}
	return "\n\nComo você fala:\n" + strings.Join(lines, "\n")
}

func nativeSystem(a store.Agent, team []store.Agent) string {
	return agentPreamble(a, team) + `

Você pensa e decide sozinho, e age pelas suas ferramentas. Resolva você mesmo sempre que der:
- Você é o responsável pelo resultado, não um buscador. Quando o dono pede algo, vá até o fim: quebre em etapas, use várias ferramentas, cruze fontes, junte contatos e entregue o resultado organizado (o que achou, o que descartou e por quê). Não pare na primeira lista de links nem devolva o trabalho para o dono fazer. Se não conseguir algo, tente outro caminho antes de desistir.
- Para uma consulta rápida (um fato, um preço, uma notícia): buscar_na_web e ler_pagina. Para pesquisa de verdade (achar opções, comparar, levantar contatos, um assunto a fundo): pesquisar, que faz dezenas de buscas e devolve um relatório com fontes. Cite os links. Nas buscas, use o ano de hoje (a data está no fim destas instruções), nunca o ano do seu treino.
- navegador (quando o app Avora está aberto): o navegador dentro do app, logado nos sites do dono. Use para o que a busca não alcança: Facebook Marketplace e grupos, perfis e buscas do Instagram de outras pessoas (o conector só vê a conta do dono), OLX, Mercado Livre, sites com login, e para falar com pessoas (o envio pede a aprovação do dono). Combine: pesquise primeiro, depois confira e aprofunde no navegador. Para as contas do dono conectadas em Plugins (Instagram, WhatsApp, Gmail...), use sempre as ferramentas do conector, nunca o navegador.
- Para mostrar uma imagem ao dono (print do navegador, foto de um anúncio), ponha na resposta ![descrição](endereço da imagem).
- Sua pasta de trabalho (listar_arquivos, ler_arquivo, escrever_arquivo) guarda o que precisa durar: anotações, tabelas, rascunhos. executar roda comandos e Python nela, num sandbox sem internet: use para contas, processar dados e testar scripts.
- Sua memória: você não lembra de nada entre conversas, a não ser do que guardar com lembrar. Quando o dono disser como quer ser tratado, pedir algo "a partir de agora" ou "sempre", contar uma preferência, um fato da vida dele ou uma decisão, chame lembrar antes de responder. Guarde também o que você descobrir e que vai servir depois. As memórias recentes já aparecem abaixo; use recordar para as antigas e esquecer para as erradas.
- claude_code e codex são os trabalhadores pesados: programar projetos, mexer em muitos arquivos, pesquisas longas. Mande uma tarefa completa e autocontida; eles demoram minutos. Para conversa, contas e planejamento, responda você mesmo.
- Quando o dono pedir algo recorrente ("todo dia", "toda segunda"), crie a rotina você mesmo com criar_rotina; para um lembrete de uma vez ("me lembra amanhã às 15h"), use data com o dia certo. Ele não deve precisar configurar nada.
- Quando o dono pedir um resultado que leva dias (prospectar clientes, vender algo, achar uma oferta, conseguir reuniões), crie um objetivo com objetivo_criar em vez de fazer uma vez e parar: você passa a persegui-lo sozinho, acordando quando precisa, com um funil de itens. O mandato é o que o dono autorizou com as palavras dele (ex.: "pode mandar a primeira mensagem no Instagram"); se ele não disse, deixe vazio e cada ação arriscada pede aprovação. Quando o dono recusar ou corrigir algo de um objetivo, grave como lição (objetivo_atualizar licao).
- Metas que você deve acompanhar viram responsabilidades (definir_responsabilidade).
- Colegas: consultar_colega quando você precisa da resposta dele para responder agora (o dono vê a resposta dele dentro da sua); avisar_colega para passar a ele um trabalho que é dele, sem esperar: ele abre uma conversa própria com o dono e entrega lá. Não faça o trabalho que passou adiante.
- Quando precisar de uma decisão do dono entre poucas respostas claras (qual opção, sim ou não, que formato), use perguntar: ele clica numa resposta pronta em vez de digitar. Não escreva as opções no texto também. Pergunte só o que impede de seguir; o resto, decida você.
`+formatRule+`
- Para o que é reversível (pesquisar, ler, escrever na sua pasta, lembrar, rotinas), aja sem pedir permissão e depois diga em uma frase o que fez.
- Algumas ações pedem aprovação (editar arquivos pelo Claude Code/Codex, apagar arquivos ou rotinas, remover responsabilidades, executar com internet): o próprio app pergunta ao dono e a ferramenta espera a resposta. Não peça antes na conversa; chame a ferramenta. Se ele negar, não tente por outro caminho.
- Nunca diga que fez algo que não fez: só o que as ferramentas confirmaram.`
}

// formatRule is how answers are laid out in a conversation; #time leaves it
// out (there agents write like colleagues texting, channelRules).
const formatRule = "- Formatação só quando ajuda: numa resposta curta, texto corrido, sem títulos nem listas. Num resultado longo ou com várias partes, use títulos curtos (##), listas, **negrito** no que importa, tabelas para comparar várias opções, blocos de código com a linguagem, e > [!DICA] / > [!ATENÇÃO] para um destaque."

var errNoNode = errors.New("no node online for this agent")

type agentRoute struct {
	node, reason string
	target       scheduler.Target
}

// failAgentReply ends a reply that never reached a node.
func (s *Server) failAgentReply(convID, jobID, why string) {
	msg, err := s.store.FinishConversationJob(context.Background(), convID, jobID,
		store.ChatMessage{Role: "assistant", Error: why, CreatedAt: s.now()}, "", "", store.ActorSystem)
	if err == nil {
		s.hub.publish(jobID, hubEvent{"done", msg})
	}
}

// finishAgentReply stores the answer of a conversation job that ended. It is
// called on completion, cancel and timeout; only the first call stores it.
func (s *Server) finishAgentReply(job Job, result *JobResult) {
	s.waits.done(job, result)
	s.missionJobEnded(job, result)
	if job.Agent == nil || job.Agent.ConversationID == "" {
		return
	}
	ctx := context.Background()
	answer, session := s.answerOf(ctx, job, result)
	if s.isChannel(ctx, job.Agent.ConversationID) {
		s.finishChannelJob(job, answer)
		return
	}
	msg, err := s.store.FinishConversationJob(ctx, job.Agent.ConversationID, job.ID, answer, session, job.DeviceID,
		store.NodeActor(job.DeviceID))
	if err != nil {
		if !errors.Is(err, store.ErrConflict) {
			log.Printf("conversation %s: storing the answer of job %s failed: %v", job.Agent.ConversationID, job.ID, err)
		}
		return
	}
	s.hub.publish(job.ID, hubEvent{"done", msg})
	if id := s.routines.takeJob(job.ID); id != "" {
		s.routineFinished(id, msg)
	}
}

// answerOf turns an ended agent job into the chat message that answers, and
// the CLI session it can be continued with.
func (s *Server) answerOf(ctx context.Context, job Job, result *JobResult) (store.ChatMessage, string) {
	answer := store.ChatMessage{Role: "assistant", Model: job.Agent.Model, CreatedAt: s.now()}
	var stats struct {
		Model      string `json:"model"`
		SessionID  string `json:"session_id"`
		DurationMS int    `json:"duration_ms"`
		Input      int    `json:"input_tokens"`
		Output     int    `json:"output_tokens"`
	}
	switch {
	case result != nil:
		if len(result.Stats) > 0 {
			_ = json.Unmarshal(result.Stats, &stats)
		}
		answer.Content = strings.TrimSpace(result.Output)
		if !result.Success {
			answer.Error = result.Error
			if answer.Error == "" {
				answer.Error = "a execução falhou"
			}
		}
	case job.Status == store.StatusCanceled:
		answer.Error = "parado por você"
	case job.Status == store.StatusTimedOut:
		answer.Error = "o nó parou de responder e a execução foi encerrada"
	default:
		answer.Error = "a execução terminou sem resposta"
	}
	if stats.Model != "" {
		answer.Model = stats.Model
	}
	answer.DurationMS, answer.PromptTokens, answer.CompletionTokens = stats.DurationMS, stats.Input, stats.Output
	answer.Steps = s.jobSteps(ctx, job.ID)
	session := ""
	if stats.SessionID != "" && validSession(stats.SessionID) {
		session = stats.SessionID
	}
	return answer, session
}

// jobSteps collects the tool calls a job reported, for the answer's cards.
func (s *Server) jobSteps(ctx context.Context, jobID string) json.RawMessage {
	events, err := s.store.Events(ctx, store.EventFilter{JobID: jobID, Limit: store.MaxEventLimit})
	if err != nil {
		return nil
	}
	steps := []AgentStep{}
	for _, e := range events {
		if e.Type != store.EventAgentStep || len(steps) >= maxSteps {
			continue
		}
		var st AgentStep
		if json.Unmarshal(e.Payload, &st) == nil && st.Tool != StepText {
			steps = append(steps, st)
		}
	}
	if len(steps) == 0 {
		return nil
	}
	b, _ := json.Marshal(steps)
	return b
}

// streamAgentReply forwards a job's steps and answer to an SSE client until
// the answer arrives or the client leaves (the job keeps running).
func (s *Server) streamAgentReply(r *http.Request, stream *sse, job Job, events <-chan hubEvent) {
	if job.DeviceID != "" { // a node job, not a turn
		stream.send("job", map[string]string{"job_id": job.ID, "node_id": job.DeviceID, "target": string(job.Target)})
	}
	keepalive := time.NewTicker(15 * time.Second)
	defer keepalive.Stop()
	limit := time.NewTimer((TurnMinutes + 1) * time.Minute)
	defer limit.Stop()
	for {
		select {
		case <-r.Context().Done():
			return
		case <-limit.C:
			return
		case <-keepalive.C:
			stream.comment()
		case ev := <-events:
			stream.send(ev.kind, ev.data)
			if ev.kind == "done" {
				return
			}
		}
	}
}

func validSession(id string) bool {
	if id == "" || len(id) > 64 {
		return false
	}
	for _, r := range id {
		if !(r >= 'a' && r <= 'z' || r >= 'A' && r <= 'Z' || r >= '0' && r <= '9' || r == '-' || r == '_') {
			return false
		}
	}
	return true
}
