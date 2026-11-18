package control

import (
	"context"
	"fmt"
	"net/http"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/GuardaChuva22/avora/internal/connect"
	"github.com/GuardaChuva22/avora/internal/store"
)

// Portão de políticas v1 (M6). Every tool call goes through gate before it
// runs. The rules live here, in code, so there is nothing to configure:
//
//   - Free ("faz e conta"): reading the state and the routines, consulting a
//     colleague, delegating read-only work, creating or changing routines
//     and responsibilities. They are reversible and show as cards.
//   - Locked (🔒, asks the owner): delegating work that creates or edits
//     files, deleting a routine, removing a responsibility.
//
// A locked action waits for "Permitir", "Sempre permitir" or "Negar" in the
// app; "Sempre permitir" becomes a grant the owner can revoke. Without an
// answer it expires and is not done. The model reads why, so it can tell
// the owner.

// How long a locked action waits for the owner, by default and at most.
const (
	DefaultApprovalMinutes = 15
	MaxApprovalMinutes     = 60
)

// SetApprovalMinutes changes how long a locked action waits for an answer.
func (s *Server) SetApprovalMinutes(n int) error {
	if n < 1 || n > MaxApprovalMinutes {
		return fmt.Errorf("approval minutes must be between 1 and %d", MaxApprovalMinutes)
	}
	s.approvalTTL = time.Duration(n) * time.Minute
	return nil
}

func (s *Server) approvalWait() time.Duration {
	if s.approvalTTL > 0 {
		return s.approvalTTL
	}
	return DefaultApprovalMinutes * time.Minute
}

// policyOf is the action key of a tool call and whether it needs the owner.
func policyOf(tool string, a toolArgs) (string, bool) {
	switch tool {
	case "claude_code", "codex", "antigravity":
		if a.PodeEditar {
			return tool + ".edit", true
		}
		return tool + ".read", false
	case "missao_iniciar":
		tool = "claude_code"
		if missionEngine(a.Executor) == "codex" {
			tool = "codex"
		}
		if a.PodeEditar {
			return tool + ".edit", true
		}
		return tool + ".read", false
	case "apagar_rotina", "remover_responsabilidade", "apagar_arquivo", "skill_criar", "docs_lixeira":
		return tool, true
	case "whatsapp_enviar":
		return "whatsapp.enviar", true
	case installMCPTool:
		return "instalar_mcp.codigo", strings.TrimSpace(a.mcp.URL) == ""
	case createMCPTool:
		return tool, true
	case "executar":
		if a.Rede {
			return "executar.rede", true
		}
		return "executar", false
	}
	return tool, false
}

// ActionLabels name the locked actions for the app.
var ActionLabels = map[string]string{
	"claude_code.edit":         "Claude Code criar e editar arquivos",
	"codex.edit":               "Codex criar e editar arquivos",
	"antigravity.edit":         "Antigravity criar e editar arquivos",
	"apagar_rotina":            "apagar rotinas",
	"remover_responsabilidade": "remover responsabilidades",
	"apagar_arquivo":           "apagar arquivos da própria pasta",
	"executar.rede":            "executar comandos com internet",
	"navegador.enviar":         "enviar, publicar, comprar ou apagar pelo navegador",
	"whatsapp.enviar":          "mandar mensagens pelo seu WhatsApp",
	"skill_criar":              "criar e alterar skills",
	"docs_lixeira":             "mover docs para a lixeira",
	"instalar_mcp.codigo":      "instalar servidores MCP de repositórios no sandbox da VPS",
	createMCPTool:              "criar servidores MCP no sandbox da VPS",
	// Asked only of an agent whose approvals are "always" (D56).
	"criar_rotina":             "criar rotinas",
	"alterar_rotina":           "alterar rotinas",
	"rodar_rotina":             "rodar rotinas na hora",
	"definir_responsabilidade": "definir responsabilidades",
	"escrever_arquivo":         "escrever arquivos da própria pasta",
	"executar":                 "executar comandos",
	"missao_iniciar":           "começar missões",
	"docs_criar":               "criar docs",
	"docs_editar":              "editar docs",
	"docs_guardar_arquivo":     "guardar arquivos nos Docs",
}

// changesSomething says whether a free call changes anything, for an agent
// that asks before everything (D56): writing in an app, its routines, its
// files, a command, a mission, or a tool whose effect Avora cannot tell.
func changesSomething(tool string, risk connect.Risk) bool {
	if risk != "" {
		return risk != connect.Read
	}
	switch tool {
	case "criar_rotina", "alterar_rotina", "rodar_rotina", "definir_responsabilidade", "escrever_arquivo", "executar", "missao_iniciar",
		"docs_criar", "docs_editar", "docs_guardar_arquivo":
		return true
	}
	return isMCPTool(tool)
}

// describe says in words what a locked call would do. ok is false when the
// target does not exist: the tool then fails on its own, with nothing to ask.
func (s *Server) describe(ctx context.Context, t *turn, tool string, a toolArgs) (string, bool) {
	if !isMCPTool(tool) {
		if ct, args, ok := s.connectorCall(tool, a.raw); ok {
			return ct.SummaryFor(args), true
		}
	}
	if strings.HasPrefix(tool, "docs_") {
		return s.docDescribe(ctx, t, tool, a.raw)
	}
	switch tool {
	case "navegador":
		return s.browserDescribe(a.raw), true
	case "whatsapp_enviar":
		return s.whatsappDescribe(ctx, a.raw)
	case "claude_code", "codex", "antigravity":
		return fmt.Sprintf("%s vai criar e editar arquivos na pasta agents/%s: %s", engineLabel(engineOf(tool)), t.agent.ID,
			truncate(strings.TrimSpace(a.Tarefa), 400)), strings.TrimSpace(a.Tarefa) != ""
	case "missao_iniciar":
		return fmt.Sprintf("Missão «%s»: %s vai criar e editar arquivos (%s). Plano: %s", strings.TrimSpace(a.Titulo),
			engineLabel(missionEngine(a.Executor)), orDefault(strings.TrimSpace(a.Projeto), "pasta do funcionário"),
			truncate(strings.Join(a.Passos, "; "), 400)), strings.TrimSpace(a.Objetivo) != ""
	case "apagar_rotina":
		rt, err := s.store.GetRoutine(ctx, a.ID)
		if err != nil {
			return "", false
		}
		return fmt.Sprintf("Apagar a rotina «%s» de %s (%s)", rt.Title, agentName(t.team, rt.AgentID), whenOf(rt)), true
	case "remover_responsabilidade":
		owner, err := s.who(ctx, t, a.Funcionario)
		if err != nil {
			return "", false
		}
		for _, r := range owner.Responsibilities {
			if r.ID == a.ID {
				return fmt.Sprintf("Remover a responsabilidade «%s» de %s", r.Goal, owner.Name), true
			}
		}
		return "", false
	case "apagar_arquivo":
		rel, err := relPath(a.Caminho, false)
		if err != nil {
			return "", false
		}
		return fmt.Sprintf("Apagar %s da pasta de %s", rel, t.agent.Name), true
	case "skill_criar":
		sk, replaces, err := s.skillDraft(ctx, t, a)
		if err != nil {
			return "", false
		}
		verb := "Guardar a skill"
		if replaces {
			verb = "Alterar a skill"
		}
		where := "para todo o time"
		if sk.ProjectID != "" {
			if p, err := s.store.GetProject(ctx, sk.ProjectID); err == nil {
				where = "no projeto " + p.Name
			}
		}
		return fmt.Sprintf("%s «%s» %s: %s\n\n%s", verb, sk.Name, where, sk.Description, truncate(sk.Body, 1500)), true
	case "executar":
		return fmt.Sprintf("Executar com internet, na pasta de %s: %s", t.agent.Name, truncate(strings.TrimSpace(a.Comando), 400)),
			strings.TrimSpace(a.Comando) != ""
	case installMCPTool, createMCPTool:
		cfg, name, err := localMCPDraft(tool, a.mcp)
		if err != nil || s.workspace == "" || s.connectors == nil || s.vault == nil {
			return "", false
		}
		if cfg.Repository != "" {
			revision := orDefault(cfg.Revision, "branch padrão")
			return fmt.Sprintf("Instalar e executar o MCP «%s» no sandbox da VPS. Origem %s (%s). Entrada %s.", name, cfg.Repository, revision, cfg.Entry), true
		}
		network := "sem internet"
		if cfg.Network {
			network = "com internet"
		}
		return fmt.Sprintf("Criar e executar o MCP «%s» em %s no sandbox da VPS, %s.", name, cfg.Runtime, network), true
	}
	return tool, true
}

func engineOf(tool string) string {
	switch tool {
	case "codex", "antigravity":
		return tool
	}
	return "claude"
}

// gate lets a tool call run, asking the owner first when it is locked. When
// it may not run, refusal is what the model reads instead of the result.
func (s *Server) gate(ctx context.Context, t *turn, tool string, a toolArgs) (refusal string, ok bool) {
	action, locked := policyOf(tool, a)
	if tool == "navegador" {
		action, locked = s.browserRisk(a.raw)
	}
	var risk connect.Risk
	if !isMCPTool(tool) {
		if ct, args, ok := s.connectorCall(tool, a.raw); ok {
			risk = ct.RiskFor(args)
			action, locked = tool, risk.Locked()
		}
	}
	strict := false
	if !locked && t.agent.Approvals == "always" && changesSomething(tool, risk) {
		action, locked, strict = tool, true, true
	}
	if !locked {
		return "", true
	}
	// An agent that never asks acts on its own, except to pay and to send or
	// buy through the browser.
	if t.agent.Approvals == "never" && risk != connect.Spend && tool != "navegador" {
		if summary, exists := s.describe(ctx, t, tool, a); exists {
			s.step(t, AgentStep{Tool: "aprovacao", Summary: "sem perguntar (" + t.agent.Name + " age sozinho): " + summary})
		}
		return "", true
	}
	if grants, err := s.store.ListGrants(ctx, t.agent.ID); err == nil {
		for _, g := range grants {
			if g.Action == action {
				return "", true
			}
		}
	}
	summary, exists := s.describe(ctx, t, tool, a)
	if !exists {
		return "", true
	}
	if strict && summary == tool {
		summary = s.actionLabel(action) + ": " + truncate(strings.TrimSpace(a.raw), 300)
	}
	// Inside a goal, what the owner's mandate clearly covers runs on its own.
	goal, inGoal := s.goalOfConversation(ctx, t.agent.ID, t.conv)
	if inGoal && s.mandateAllows(ctx, goal, summary) {
		s.step(t, AgentStep{Tool: "mandato", Summary: "dentro do mandato: " + summary})
		return "", true
	}
	root := t
	for root.parent != nil {
		root = root.parent
	}
	// The night shift: nobody is there to approve.
	if s.night.inShift(root.conv) || s.night.inShift(t.conv) {
		s.step(t, AgentStep{Tool: "aprovacao", Summary: "turno da noite, não rodou: " + summary})
		return "Turno da noite: ações travadas não rodam sem o dono (" + summary + "). Deixe tudo pronto e termine com ESPERANDO APROVAÇÃO dizendo o que ele precisa aprovar.", false
	}
	now := s.now()
	ttl := s.approvalWait()
	ap := store.Approval{ID: newID()[:16], AgentID: t.agent.ID, ConversationID: t.conv, TurnID: root.id,
		Action: action, Summary: truncate(summary, 600), CreatedAt: now, ExpiresAt: now.Add(ttl)}
	decided := s.approvals.add(ap.ID)
	defer s.approvals.drop(ap.ID)
	if _, err := s.store.CreateApproval(ctx, ap, "agent:"+t.agent.ID); err != nil {
		return "Não consegui pedir a aprovação do dono; a ação não foi feita.", false
	}
	s.hub.publish(root.id, hubEvent{"approval", ap})

	timer := time.NewTimer(ttl)
	defer timer.Stop()
	var final store.Approval
	select {
	case final = <-decided:
	case <-timer.C:
		final = s.expireApproval(ap.ID, store.ActorSystem)
	case <-ctx.Done():
		final = s.expireApproval(ap.ID, store.ActorSystem)
	}
	switch final.Status {
	case "approved":
		how := "permitida"
		if final.Always {
			how = "sempre permitida"
		}
		s.step(t, AgentStep{Tool: "aprovacao", Summary: how + ": " + summary})
		return "", true
	case "denied":
		s.step(t, AgentStep{Tool: "aprovacao", Summary: "negada: " + summary})
		refusal := "O dono negou esta ação: " + summary + ". Não faça nem tente de novo por outro caminho; pergunte o que ele prefere."
		if inGoal {
			refusal += " Grave o que isso ensina com objetivo_atualizar licao, para não repetir."
		}
		return refusal, false
	default:
		s.step(t, AgentStep{Tool: "aprovacao", Summary: "expirou: " + summary})
		return fmt.Sprintf("O dono não respondeu em %d minutos e a ação não foi feita: %s. Diga isso a ele.", int(ttl.Minutes()), summary), false
	}
}

// expireApproval closes an approval nobody answered; if the owner answered
// at the same moment, that answer wins.
func (s *Server) expireApproval(id, actor string) store.Approval {
	ap, err := s.store.DecideApproval(context.Background(), id, "expired", false, actor, s.now())
	if err == nil {
		return ap
	}
	if got, gerr := s.store.GetApproval(context.Background(), id); gerr == nil {
		return got
	}
	return store.Approval{ID: id, Status: "expired"}
}

// approvalWaits wakes the turn waiting on an approval.
type approvalWaits struct {
	mu sync.Mutex
	m  map[string]chan store.Approval
}

func (aw *approvalWaits) add(id string) <-chan store.Approval {
	aw.mu.Lock()
	defer aw.mu.Unlock()
	if aw.m == nil {
		aw.m = map[string]chan store.Approval{}
	}
	ch := make(chan store.Approval, 1)
	aw.m[id] = ch
	return ch
}

func (aw *approvalWaits) drop(id string) {
	aw.mu.Lock()
	defer aw.mu.Unlock()
	delete(aw.m, id)
}

func (aw *approvalWaits) done(ap store.Approval) {
	aw.mu.Lock()
	ch := aw.m[ap.ID]
	delete(aw.m, ap.ID)
	aw.mu.Unlock()
	if ch != nil {
		ch <- ap
	}
}

// ── API ─────────────────────────────────────────────────────────

// ApprovalDecision is the owner's answer: "once", "always" or "deny".
type ApprovalDecision struct {
	Decision string `json:"decision"`
}

// GrantView is a grant with its action in words.
type GrantView struct {
	store.Grant
	Label string `json:"label"`
}

func (s *Server) registerApprovals(mux *http.ServeMux) {
	mux.HandleFunc("GET /v1/approvals", s.admin(s.listApprovals))
	mux.HandleFunc("POST /v1/approvals/{id}", s.admin(s.decideApproval))
	mux.HandleFunc("GET /v1/grants", s.admin(s.listGrants))
	mux.HandleFunc("DELETE /v1/grants/{id}", s.admin(s.deleteGrant))
}

func (s *Server) listApprovals(w http.ResponseWriter, r *http.Request) {
	status := r.URL.Query().Get("status")
	if status != "" && !contains(store.ApprovalStatuses, status) {
		http.Error(w, "invalid status", http.StatusBadRequest)
		return
	}
	limit := 50
	if v := r.URL.Query().Get("limit"); v != "" {
		n, err := strconv.Atoi(v)
		if err != nil || n < 1 || n > 500 {
			http.Error(w, "invalid limit", http.StatusBadRequest)
			return
		}
		limit = n
	}
	list, err := s.store.ListApprovals(r.Context(), status, limit)
	if err != nil {
		storeError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, list)
}

func (s *Server) decideApproval(w http.ResponseWriter, r *http.Request) {
	var req ApprovalDecision
	if !readJSON(w, r, &req) {
		return
	}
	status, always := "", false
	switch req.Decision {
	case "once":
		status = "approved"
	case "always":
		status, always = "approved", true
	case "deny":
		status = "denied"
	default:
		http.Error(w, `decision must be "once", "always" or "deny"`, http.StatusBadRequest)
		return
	}
	ap, err := s.store.DecideApproval(r.Context(), r.PathValue("id"), status, always, store.ActorAdmin, s.now())
	if err != nil {
		storeError(w, err)
		return
	}
	if status == "denied" {
		s.recordDeniedApprovalLesson(r.Context(), ap)
	}
	s.approvals.done(ap)
	writeJSON(w, http.StatusOK, ap)
}

func (s *Server) listGrants(w http.ResponseWriter, r *http.Request) {
	list, err := s.store.ListGrants(r.Context(), r.URL.Query().Get("agent"))
	if err != nil {
		storeError(w, err)
		return
	}
	out := make([]GrantView, len(list))
	for i, g := range list {
		out[i] = GrantView{Grant: g, Label: s.actionLabel(g.Action)}
		if out[i].Label == "" {
			out[i].Label = g.Action
		}
	}
	writeJSON(w, http.StatusOK, out)
}

func (s *Server) deleteGrant(w http.ResponseWriter, r *http.Request) {
	if err := s.store.DeleteGrant(r.Context(), r.PathValue("id"), store.ActorAdmin, s.now()); err != nil {
		storeError(w, err)
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

// expirePending closes the approvals left pending by a restart: the turns
// waiting on them are gone.
func (s *Server) expirePending(ctx context.Context) error {
	list, err := s.store.ListApprovals(ctx, "pending", 500)
	if err != nil {
		return err
	}
	for _, ap := range list {
		_, _ = s.store.DecideApproval(ctx, ap.ID, "expired", false, store.ActorSystem, s.now())
	}
	return nil
}
