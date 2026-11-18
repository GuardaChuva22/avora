package control

import (
	"context"
	"errors"
	"fmt"
	"log"
	"net/http"
	"strings"
	"sync"
	"time"
	"unicode/utf8"

	"github.com/GuardaChuva22/avora/internal/store"
)

// Missions: long work an agent runs in the background. The agent stays the
// owner of the work; Claude Code or Codex is the hand that does each run.
// A run is a node job nobody waits on; when it ends (on completion, cancel
// or timeout, even after a server restart) the agent is woken in the
// mission's conversation with the result, updates the checklist, and starts
// the next run (the same CLI session, on the same node) or finishes.

const (
	MaxMissionRuns      = 8
	MaxActiveMissions   = 6
	maxMissionSteps     = 12
	maxMissionStepChars = 200
	missionResultChars  = 6000
	missionWakeRetry    = 20 * time.Second
	missionWakeTries    = 90 // 30 minutes of a busy conversation
	missionPrefix       = "🎯 Missão"
)

var stepStatuses = map[string]string{
	"pendente": store.StepTodo, "fazendo": store.StepDoing, "feito": store.StepDone, "pulado": store.StepDropped,
	"todo": store.StepTodo, "doing": store.StepDoing, "done": store.StepDone, "dropped": store.StepDropped,
}

// missionBridges closes the bridge of each running mission when its run ends.
type missionBridges struct {
	mu sync.Mutex
	m  map[string]func()
}

func (b *missionBridges) set(id string, close func()) {
	b.mu.Lock()
	defer b.mu.Unlock()
	if b.m == nil {
		b.m = map[string]func(){}
	}
	if old := b.m[id]; old != nil {
		old()
	}
	b.m[id] = close
}

func (b *missionBridges) close(id string) {
	b.mu.Lock()
	c := b.m[id]
	delete(b.m, id)
	b.mu.Unlock()
	if c != nil {
		c()
	}
}

const missionStartParams = `{"type": "object", "properties": {
	"titulo": {"type": "string", "description": "Nome curto da missão, ex.: Refatorar o login."},
	"objetivo": {"type": "string", "description": "O objetivo completo e autocontido: o que fazer, com todo o contexto, e o que entregar no fim."},
	"passos": {"type": "array", "items": {"type": "string"}, "description": "O plano: 2 a 12 passos curtos, na ordem."},
	"executor": {"type": "string", "enum": ["claude_code", "codex"], "description": "Quem executa; padrão claude_code."},
	"pode_editar": {"type": "boolean", "description": "true se ele precisa criar ou editar arquivos; false (padrão) para só ler e pesquisar."},
	"conta": {"type": "string", "description": "Opcional: a conta da assinatura a usar."},
	"projeto": {"type": "string", "description": "Opcional, para universais: o projeto em cuja pasta ele trabalha."},
	"pasta": {"type": "string", "description": "Opcional: outra pasta do projeto (o nome dela)."}
}, "required": ["titulo", "objetivo", "passos"]}`

const missionUpdateParams = `{"type": "object", "properties": {
	"missao": {"type": "string", "description": "O id da missão; vazio = a sua única missão em andamento."},
	"plano": {"type": "array", "items": {"type": "object", "properties": {
		"texto": {"type": "string"}, "status": {"type": "string", "enum": ["pendente", "fazendo", "feito", "pulado"]}},
		"required": ["texto", "status"]}, "description": "O plano inteiro atualizado (pode acrescentar ou reescrever passos)."},
	"continuar": {"type": "string", "description": "Instrução da próxima rodada para o executor (ele lembra da rodada anterior). Começa uma nova rodada em segundo plano."},
	"status": {"type": "string", "enum": ["concluida", "aguardando_dono", "cancelada"], "description": "Para encerrar a missão ou pausar até o dono responder."},
	"resumo": {"type": "string", "description": "Com concluida: o resultado final para o dono. Com aguardando_dono: o que você precisa dele."}
}}`

type missionStepInput struct {
	Texto  string `json:"texto"`
	Status string `json:"status"`
}

func missionEngine(executor string) string {
	if strings.TrimSpace(strings.ToLower(executor)) == "codex" {
		return "codex"
	}
	return "claude"
}

func cleanStepText(s string) string {
	s = strings.Join(strings.Fields(s), " ")
	if utf8.RuneCountInString(s) > maxMissionStepChars {
		s = string([]rune(s)[:maxMissionStepChars])
	}
	return s
}

// planText is the checklist as the agent and the executor read it.
func planText(plan []store.MissionStep) string {
	marks := map[string]string{store.StepTodo: "[ ]", store.StepDoing: "[~]", store.StepDone: "[x]", store.StepDropped: "[-]"}
	var b strings.Builder
	for i, st := range plan {
		fmt.Fprintf(&b, "%d. %s %s\n", i+1, marks[st.Status], st.Text)
	}
	return strings.TrimRight(b.String(), "\n")
}

func (s *Server) toolStartMission(ctx context.Context, t *turn, a toolArgs) (string, error) {
	title, objective := strings.TrimSpace(a.Titulo), strings.TrimSpace(a.Objetivo)
	if title == "" || objective == "" {
		return "", errors.New("diga o título e o objetivo da missão")
	}
	if t.conv == "" || t.channel {
		return "", errors.New("missões começam numa conversa direta com o dono, não num canal")
	}
	plan := []store.MissionStep{}
	for _, p := range a.Passos {
		if p = cleanStepText(p); p != "" && len(plan) < maxMissionSteps {
			plan = append(plan, store.MissionStep{Text: p, Status: store.StepTodo})
		}
	}
	if len(plan) == 0 {
		return "", errors.New("escreva o plano em passos")
	}
	if active, err := s.store.ListMissions(ctx, store.MissionFilter{ActiveOnly: true}); err == nil && len(active) >= MaxActiveMissions {
		return "", fmt.Errorf("já há %d missões em andamento; conclua uma antes", len(active))
	}
	engine := missionEngine(a.Executor)
	a.Tarefa = objective
	d, err := s.planDelegation(ctx, t, engine, a)
	if err != nil {
		return "", err
	}
	d.spec.MaxMinutes = MaxAgentMinutes
	m := store.Mission{
		ID: newID(), AgentID: t.agent.ID, ConversationID: t.conv, Title: truncate(title, 120), Objective: truncate(objective, 8000),
		Plan: plan, Spec: *d.spec, Target: d.route.target, NodeID: d.route.node,
	}
	plan[0].Status = store.StepDoing
	task := fmt.Sprintf("Missão: %s\n\nObjetivo:\n%s\n\nPlano:\n%s\n\nFaça o máximo do plano que conseguir nesta rodada. "+
		"No fim, diga quais passos (pelos números) você concluiu, o que ficou faltando e qualquer dúvida para o dono.",
		m.Title, m.Objective, planText(plan))
	m.Brief = truncate(task, MaxAgentPrompt/2)
	if err := s.launchMission(ctx, t.agent, &m, d.project, d.folder, task, "agent:"+t.agent.ID); err != nil {
		return "", err
	}
	s.step(t, AgentStep{Tool: "missao_iniciar", Summary: m.Title + " · " + d.label, Job: m.JobID})
	return fmt.Sprintf("Missão «%s» começou em segundo plano (%s, id %s). Conte ao dono que você avisa quando a rodada terminar; não espere por ela.",
		m.Title, d.label, m.ID), nil
}

// launchMission starts one run of m without waiting for it and saves m.
func (s *Server) launchMission(ctx context.Context, agent store.Agent, m *store.Mission, project *store.Project, folder *store.ProjectFolder, task, actor string) error {
	spec := m.Spec
	apps := s.bridgeApps(ctx, agent.ID)
	switch {
	case m.Runs == 0:
		spec.Prompt = delegatePrompt(agent, task, apps, project, folder)
	case m.Session == "":
		// The last run left no session to continue: start over with
		// the whole story.
		spec.Prompt = delegatePrompt(agent, m.Brief+"\n\nAndamento até aqui:\n"+planText(m.Plan)+
			"\n\nResultado da rodada anterior:\n"+truncate(m.LastResult, 3000)+"\n\nNesta rodada:\n"+task, apps, nil, nil)
	default:
		spec.Resume = m.Session
		spec.Prompt = "Continuação da missão «" + m.Title + "».\n\nPlano agora:\n" + planText(m.Plan) +
			"\n\nNesta rodada:\n" + task + "\n\nNo fim, diga quais passos você concluiu e o que falta."
	}
	spec.Prompt = truncate(spec.Prompt, MaxAgentPrompt-8)
	spec.Owner, spec.Mission = agent.ID, m.ID
	spec.Title = fmt.Sprintf("Missão «%s» · rodada %d", m.Title, m.Runs+1)
	if apps != "" {
		all, _ := s.store.ListAgents(ctx)
		mt := &turn{agent: agent, team: visibleTeam(agent, all), id: m.ID, conv: m.ConversationID}
		key, closeBridge := s.bridges.open(mt, time.Duration(spec.MaxMinutes)*time.Minute+s.approvalWait()+5*time.Minute, s.now())
		spec.Bridge = key
		s.missionBridges.set(m.ID, closeBridge)
	}
	if err := validateAgent(&spec); err != nil {
		return err
	}
	jobID := newID()
	m.Status, m.JobID, m.WakePending = store.MissionRunning, jobID, false
	m.Runs++
	// Saved before the job exists, so a run that ends at once finds it.
	saved, err := s.store.SaveMission(ctx, *m, actor, s.now())
	if err != nil {
		return errors.New("não consegui guardar a missão")
	}
	*m = saved
	reason := fmt.Sprintf("%s: missão «%s», rodada %d", agent.Name, m.Title, m.Runs)
	if _, err := s.store.CreateJob(ctx, Job{ID: jobID, Action: store.ActionAgentRun, Target: m.Target, DeviceID: m.NodeID,
		CreatedAt: s.now(), Agent: &spec}, reason, actor); err != nil {
		s.missionBridges.close(m.ID)
		m.Status, m.JobID, m.Runs = store.MissionReview, "", m.Runs-1
		_, _ = s.store.SaveMission(ctx, *m, actor, s.now())
		return errors.New("não consegui criar a rodada no nó")
	}
	return nil
}

// missionJobEnded records the end of a mission's run and wakes its agent.
// It is called for every job that ends; most are not missions.
func (s *Server) missionJobEnded(job Job, result *JobResult) {
	if job.Agent == nil || job.Agent.ConversationID != "" {
		return
	}
	ctx := context.Background()
	m, err := s.store.MissionByJob(ctx, job.ID)
	if err != nil {
		return
	}
	s.missionBridges.close(m.ID)
	answer, session := s.answerOf(ctx, job, result)
	out := strings.TrimSpace(answer.Content)
	if answer.Error != "" {
		out = "A rodada falhou: " + answer.Error + "\n" + out
	} else if session != "" {
		m.Session = session
	}
	m.LastResult, m.JobID = truncate(strings.TrimSpace(out), missionResultChars), ""
	if m.Status == store.MissionRunning {
		m.Status, m.WakePending = store.MissionReview, true
	}
	saved, err := s.store.SaveMission(ctx, m, store.NodeActor(job.DeviceID), s.now())
	if err != nil {
		log.Printf("mission %s: storing the run's end failed: %v", m.ID, err)
		return
	}
	if saved.WakePending {
		go s.wakeMission(saved.ID, 0)
	}
}

// missionReport is what the agent reads when a run ends.
func missionReport(m store.Mission) string {
	return fmt.Sprintf("%s «%s» · a rodada %d de %s terminou.\n\nResultado:\n%s\n\nPlano (id %s):\n%s\n\n"+
		"Use missao_atualizar agora: marque os passos e continue (continuar) se falta algo que o executor pode fazer; "+
		"conclua (status concluida, com resumo) se terminou; ou pergunte ao dono (aguardando_dono). Depois conte ao dono, em poucas linhas, como está.",
		missionPrefix, m.Title, m.Runs, engineLabel(m.Spec.Engine), orDefault(m.LastResult, "(sem resposta)"), m.ID, planText(m.Plan))
}

// wakeMission starts the agent's turn about a run that ended, retrying
// while the conversation is busy answering something else.
func (s *Server) wakeMission(id string, try int) {
	ctx := context.Background()
	m, err := s.store.GetMission(ctx, id)
	if err != nil || !m.WakePending {
		return
	}
	agent, aerr := s.store.GetAgent(ctx, m.AgentID)
	conv, cerr := s.store.GetConversation(ctx, m.ConversationID)
	if aerr != nil || cerr != nil {
		m.Status, m.WakePending, m.Summary = store.MissionFailed, false, "a conversa ou o funcionário da missão não existe mais"
		_, _ = s.store.SaveMission(ctx, m, store.ActorSystem, s.now())
		return
	}
	_, err = s.startTurn(ctx, newID(), conv, agent, missionReport(m), 0, "", store.ActorSystem, nil)
	switch {
	case err == nil:
		if cur, gerr := s.store.GetMission(ctx, id); gerr == nil && cur.WakePending {
			cur.WakePending = false
			_, _ = s.store.SaveMission(ctx, cur, store.ActorSystem, s.now())
		}
	case errors.Is(err, store.ErrConflict) && try < missionWakeTries:
		time.AfterFunc(missionWakeRetry, func() { s.wakeMission(id, try+1) })
	default:
		log.Printf("mission %s: waking %s failed: %v", id, agent.Name, err)
	}
}

// RecoverMissions wakes agents whose run ended while the server was down
// (or whose wake was pending) after a restart.
func (s *Server) RecoverMissions(ctx context.Context) {
	list, err := s.store.ListMissions(ctx, store.MissionFilter{ActiveOnly: true})
	if err != nil {
		return
	}
	for _, m := range list {
		switch {
		case m.Status == store.MissionRunning && m.JobID != "":
			if job, err := s.store.GetJob(ctx, m.JobID); err == nil && job.Status.Terminal() {
				result := &JobResult{Success: job.Status == store.StatusSucceeded, Output: job.Output, Error: job.Error}
				if job.Status != store.StatusSucceeded && job.Status != store.StatusFailed {
					result = nil
				}
				go s.missionJobEnded(job, result)
			} else if errors.Is(err, store.ErrNotFound) {
				m.Status, m.JobID, m.WakePending, m.LastResult = store.MissionReview, "", true, "A rodada se perdeu (o servidor reiniciou)."
				_, _ = s.store.SaveMission(ctx, m, store.ActorSystem, s.now())
				go s.wakeMission(m.ID, 0)
			}
		case m.WakePending:
			go s.wakeMission(m.ID, 0)
		}
	}
}

// pickMission finds the agent's mission a call is about.
func (s *Server) pickMission(ctx context.Context, t *turn, id string) (store.Mission, error) {
	if id = strings.TrimSpace(id); id != "" {
		m, err := s.store.GetMission(ctx, id)
		if err != nil || m.AgentID != t.agent.ID {
			return store.Mission{}, fmt.Errorf("não há missão %q sua", id)
		}
		return m, nil
	}
	list, err := s.store.ListMissions(ctx, store.MissionFilter{AgentID: t.agent.ID, ActiveOnly: true})
	if err != nil {
		return store.Mission{}, err
	}
	switch len(list) {
	case 0:
		return store.Mission{}, errors.New("você não tem missão em andamento")
	case 1:
		return list[0], nil
	}
	return store.Mission{}, errors.New("você tem mais de uma missão: diga o id em missao")
}

func (s *Server) toolUpdateMission(ctx context.Context, t *turn, a toolArgs) (string, error) {
	ma := a
	m, err := s.pickMission(ctx, t, ma.Missao)
	if err != nil {
		return "", err
	}
	if !m.Active() {
		return "", fmt.Errorf("a missão «%s» já terminou", m.Title)
	}
	if len(ma.Plano) > 0 {
		plan := []store.MissionStep{}
		for _, p := range ma.Plano {
			text, status := cleanStepText(p.Texto), stepStatuses[strings.ToLower(strings.TrimSpace(p.Status))]
			if text == "" || len(plan) == maxMissionSteps {
				continue
			}
			plan = append(plan, store.MissionStep{Text: text, Status: orDefault(status, store.StepTodo)})
		}
		if len(plan) > 0 {
			m.Plan = plan
		}
	}
	actor, summary := "agent:"+t.agent.ID, strings.TrimSpace(ma.Resumo)
	next := strings.TrimSpace(ma.Continuar)
	var did string
	switch strings.ToLower(strings.TrimSpace(ma.Status)) {
	case "concluida", "concluída":
		m.Status, m.Summary, m.WakePending = store.MissionDone, truncate(summary, 4000), false
		now := s.now()
		m.FinishedAt = &now
		did = "concluída"
	case "cancelada":
		did = "cancelada"
		s.cancelMission(ctx, &m, actor)
	case "aguardando_dono":
		if m.Status == store.MissionRunning {
			return "", errors.New("a rodada ainda está rodando; espere ela terminar para pausar")
		}
		m.Status, m.Summary, m.WakePending = store.MissionWaiting, truncate(summary, 4000), false
		did = "aguardando o dono"
	case "":
		if next == "" {
			if m.Status == store.MissionReview {
				m.Status = store.MissionWaiting
			}
			did = "plano atualizado"
		}
	default:
		return "", fmt.Errorf("status %q não existe (concluida, aguardando_dono, cancelada)", ma.Status)
	}
	if next != "" && did == "" {
		switch {
		case m.Status == store.MissionRunning:
			return "", errors.New("a rodada atual ainda está rodando; você é acordado quando ela terminar")
		case m.Runs >= MaxMissionRuns:
			return "", fmt.Errorf("a missão chegou ao limite de %d rodadas: conclua ou pergunte ao dono", MaxMissionRuns)
		}
		node, nerr := s.nodeByID(ctx, m.NodeID)
		if nerr != nil || !s.online(node) {
			return "", errors.New("o nó da missão está offline agora; pergunte ao dono (aguardando_dono) ou tente depois")
		}
		for i := range m.Plan {
			if m.Plan[i].Status == store.StepTodo {
				m.Plan[i].Status = store.StepDoing
				break
			}
		}
		if err := s.launchMission(ctx, t.agent, &m, nil, nil, next, actor); err != nil {
			return "", err
		}
		s.step(t, AgentStep{Tool: "missao_atualizar", Summary: m.Title + " · rodada " + fmt.Sprint(m.Runs), Job: m.JobID})
		return fmt.Sprintf("Rodada %d da missão «%s» começou em segundo plano. Você é acordado quando ela terminar.", m.Runs, m.Title), nil
	}
	if _, err := s.store.SaveMission(ctx, m, actor, s.now()); err != nil {
		return "", errors.New("não consegui guardar a missão")
	}
	s.step(t, AgentStep{Tool: "missao_atualizar", Summary: m.Title + " · " + did})
	return fmt.Sprintf("Missão «%s»: %s.\n%s", m.Title, did, planText(m.Plan)), nil
}

// cancelMission ends m and stops its run, if any (m is saved by the caller
// or here when a run was stopped).
func (s *Server) cancelMission(ctx context.Context, m *store.Mission, actor string) {
	m.Status, m.WakePending = store.MissionCanceled, false
	now := s.now()
	m.FinishedAt = &now
	jobID := m.JobID
	m.JobID = ""
	if _, err := s.store.SaveMission(ctx, *m, actor, s.now()); err != nil {
		return
	}
	s.missionBridges.close(m.ID)
	if jobID != "" {
		if job, err := s.store.CancelJob(ctx, jobID, actor, s.now()); err == nil {
			s.finishAgentReply(job, nil)
		}
	}
}

// missionsBlock tells the agent about its missions in progress.
func (s *Server) missionsBlock(ctx context.Context, a store.Agent) string {
	list, err := s.store.ListMissions(ctx, store.MissionFilter{AgentID: a.ID, ActiveOnly: true})
	if err != nil || len(list) == 0 {
		return ""
	}
	words := map[string]string{store.MissionRunning: "rodando", store.MissionReview: "rodada terminou, decida",
		store.MissionWaiting: "esperando o dono"}
	var b strings.Builder
	b.WriteString("\n\nSuas missões em andamento (atualize com missao_atualizar):")
	for _, m := range list {
		fmt.Fprintf(&b, "\n- «%s» (id %s, %s, rodada %d/%d)\n%s", m.Title, m.ID, words[m.Status], m.Runs, MaxMissionRuns, planText(m.Plan))
		if m.Status == store.MissionWaiting && m.Summary != "" {
			b.WriteString("\n  Você perguntou ao dono: " + truncate(m.Summary, 300))
		}
	}
	return b.String()
}

// ── API ─────────────────────────────────────────────────────────

func (s *Server) registerMissions(mux *http.ServeMux) {
	mux.HandleFunc("GET /v1/missions", s.admin(s.listMissions))
	mux.HandleFunc("POST /v1/missions/{id}/cancel", s.admin(s.cancelMissionAPI))
}

func (s *Server) listMissions(w http.ResponseWriter, r *http.Request) {
	q := r.URL.Query()
	list, err := s.store.ListMissions(r.Context(), store.MissionFilter{
		AgentID: q.Get("agent"), ConversationID: q.Get("conversation"), ActiveOnly: q.Get("active") == "1"})
	if err != nil {
		storeError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, list)
}

func (s *Server) cancelMissionAPI(w http.ResponseWriter, r *http.Request) {
	m, err := s.store.GetMission(r.Context(), r.PathValue("id"))
	if err != nil {
		storeError(w, err)
		return
	}
	if !m.Active() {
		http.Error(w, "mission is over", http.StatusConflict)
		return
	}
	s.cancelMission(r.Context(), &m, store.ActorAdmin)
	writeJSON(w, http.StatusOK, m)
}
