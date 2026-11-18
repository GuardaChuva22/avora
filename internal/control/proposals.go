package control

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"strconv"
	"strings"
	"unicode/utf8"

	"github.com/GuardaChuva22/avora/internal/llm"
	"github.com/GuardaChuva22/avora/internal/store"
)

// StepProposal marks a proposal created during an agent turn.
const StepProposal = "propose_change"

// ProposalView enriches Proposal with display names and a Portuguese headline.
type ProposalView struct {
	store.Proposal
	FromName   string `json:"from_name"`
	TargetName string `json:"target_name"`
	Headline   string `json:"headline"`
}

// ProposalRequest creates a proposal over HTTP.
type ProposalRequest struct {
	ConversationID string          `json:"conversation_id"`
	FromAgent      string          `json:"from_agent"`
	TargetAgent    string          `json:"target_agent"`
	Field          string          `json:"field"`
	After          json.RawMessage `json:"after"`
	Reason         string          `json:"reason"`
}

type proposeToolArgs struct {
	TargetAgent string          `json:"target_agent"`
	Funcionario string          `json:"funcionario"`
	Alvo        string          `json:"alvo"`
	Field       string          `json:"field"`
	Campo       string          `json:"campo"`
	After       json.RawMessage `json:"after"`
	Depois      json.RawMessage `json:"depois"`
	Valor       json.RawMessage `json:"valor"`
	Reason      string          `json:"reason"`
	Motivo      string          `json:"motivo"`
}

func proposeChangeTool() llm.Tool {
	return tool("propose_change",
		"Propõe uma mudança nas instruções, no cargo ou nos apps de um funcionário (ou seus próprios). A proposta aparece como um cartão no chat para o dono aplicar ou recusar; aplicar guarda uma nova versão com Desfazer.",
		`{"type": "object", "properties": {
	"target_agent": {"type": "string", "description": "Nome ou id do funcionário alvo (vazio = você mesmo)."},
	"field": {"type": "string", "enum": ["instructions", "role", "apps"], "description": "Qual campo mudar: instructions, role ou apps."},
	"after": {"type": "string", "description": "O novo valor completo proposto (para apps: nomes separados por vírgula)."},
	"reason": {"type": "string", "description": "Por que essa mudança melhora o time, numa frase curta."}
}, "required": ["target_agent", "field", "after", "reason"]}`)
}

func (s *Server) registerProposals(mux *http.ServeMux) {
	mux.HandleFunc("GET /v1/proposals", s.admin(s.listProposals))
	mux.HandleFunc("POST /v1/proposals", s.admin(s.createProposalHTTP))
	mux.HandleFunc("GET /v1/proposals/{id}", s.admin(s.getProposal))
	mux.HandleFunc("POST /v1/proposals/{id}/apply", s.admin(s.applyProposal))
	mux.HandleFunc("POST /v1/proposals/{id}/reject", s.admin(s.rejectProposal))
	mux.HandleFunc("POST /v1/proposals/{id}/undo", s.admin(s.undoProposal))
}

func decodeAfterValue(raw json.RawMessage) (string, error) {
	raw = json.RawMessage(strings.TrimSpace(string(raw)))
	if len(raw) == 0 || string(raw) == "null" {
		return "", nil
	}
	var str string
	if err := json.Unmarshal(raw, &str); err == nil {
		return strings.TrimSpace(str), nil
	}
	var arr []string
	if err := json.Unmarshal(raw, &arr); err == nil {
		cleaned := parseAppsList(strings.Join(arr, ","))
		return strings.Join(cleaned, ", "), nil
	}
	return "", errors.New("after must be a string or string array")
}

func parseAppsList(v string) []string {
	v = strings.TrimSpace(v)
	if v == "" {
		return nil
	}
	var arr []string
	if strings.HasPrefix(v, "[") {
		if json.Unmarshal([]byte(v), &arr) == nil {
			v = strings.Join(arr, ",")
		}
	}
	parts := strings.FieldsFunc(v, func(r rune) bool {
		return r == ',' || r == ';' || r == '\n'
	})
	out := []string{}
	seen := map[string]bool{}
	for _, p := range parts {
		p = strings.TrimSpace(p)
		if p != "" && !seen[p] {
			seen[p] = true
			out = append(out, p)
		}
	}
	return out
}

func formatAppsValue(apps []string) string {
	return strings.Join(apps, ", ")
}

func normalizeProposalField(raw string) (string, bool) {
	switch strings.ToLower(strings.TrimSpace(raw)) {
	case "instructions", "instrucoes", "instruções", "persona":
		return "instructions", true
	case "role", "cargo":
		return "role", true
	case "apps", "aplicativos", "conectores":
		return "apps", true
	}
	return "", false
}

func proposalHeadline(fromName, targetName, field string) string {
	actor := fromName
	if actor == "" {
		actor = "Avora"
	}
	target := targetName
	if target == "" {
		target = "funcionário"
	}
	switch field {
	case "role":
		return fmt.Sprintf("%s propõe mudar o cargo de %s", actor, target)
	case "apps":
		return fmt.Sprintf("%s propõe mudar os apps de %s", actor, target)
	default:
		return fmt.Sprintf("%s propõe mudar as instruções de %s", actor, target)
	}
}

func (s *Server) effectiveAgentApps(ctx context.Context, a store.Agent) []string {
	if len(a.Apps) > 0 {
		return append([]string{}, a.Apps...)
	}
	conns, err := s.store.ListConnections(ctx)
	if err != nil {
		return nil
	}
	var out []string
	seen := map[string]bool{}
	for _, c := range conns {
		if len(c.Agents) == 0 {
			continue
		}
		for _, id := range c.Agents {
			if id == a.ID && !seen[c.ID] {
				seen[c.ID] = true
				out = append(out, c.ID)
			}
		}
	}
	return out
}

func (s *Server) proposalBeforeValue(ctx context.Context, target store.Agent, field string) string {
	switch field {
	case "role":
		return target.Role
	case "apps":
		return formatAppsValue(s.effectiveAgentApps(ctx, target))
	default:
		return target.Persona
	}
}

func validateProposalAfter(field, after string) (string, error) {
	switch field {
	case "instructions":
		after = strings.TrimSpace(after)
		if after == "" || utf8.RuneCountInString(after) > maxPersona {
			return "", fmt.Errorf("instructions must have 1 to %d characters", maxPersona)
		}
		return after, nil
	case "role":
		after = strings.TrimSpace(after)
		if after == "" || utf8.RuneCountInString(after) > 60 {
			return "", errors.New("role must have 1 to 60 characters")
		}
		return after, nil
	case "apps":
		return formatAppsValue(parseAppsList(after)), nil
	default:
		return "", errors.New("field must be instructions, role or apps")
	}
}

func (s *Server) toolProposeChange(ctx context.Context, t *turn, a toolArgs) (string, error) {
	var args proposeToolArgs
	if err := json.Unmarshal([]byte(a.raw), &args); err != nil {
		return "", err
	}
	targetRef := pickStr(args.TargetAgent, args.Funcionario, args.Alvo)
	target, err := s.who(ctx, t, targetRef)
	if err != nil {
		return "", err
	}
	field, ok := normalizeProposalField(pickStr(args.Field, args.Campo))
	if !ok {
		return "", errors.New("field inválido; use instructions, role ou apps")
	}
	rawAfter := args.After
	if len(rawAfter) == 0 {
		rawAfter = args.Depois
	}
	if len(rawAfter) == 0 {
		rawAfter = args.Valor
	}
	afterRawStr, err := decodeAfterValue(rawAfter)
	if err != nil {
		return "", err
	}
	after, err := validateProposalAfter(field, afterRawStr)
	if err != nil {
		return "", err
	}
	reason := strings.TrimSpace(pickStr(args.Reason, args.Motivo))
	if reason == "" {
		return "", errors.New("diga o motivo da proposta em reason")
	}
	before := s.proposalBeforeValue(ctx, target, field)
	if before == after {
		return "Nada mudou: o valor proposto já é o atual.", nil
	}
	wsID := target.WorkspaceID
	if wsID == "" {
		wsID = "pessoal"
	}
	prop, err := s.store.CreateProposal(ctx, store.Proposal{
		ID:             newID()[:12],
		WorkspaceID:    wsID,
		ConversationID: t.conv,
		FromAgent:      t.agent.ID,
		TargetAgent:    target.ID,
		Field:          field,
		Before:         before,
		After:          after,
		Reason:         truncate(reason, 500),
		Status:         "pending",
	}, "agent:"+t.agent.ID, s.now())
	if err != nil {
		return "", err
	}
	headline := proposalHeadline(t.agent.Name, target.Name, field)
	s.step(t, AgentStep{Tool: StepProposal, Summary: headline, Detail: prop.ID})
	return fmt.Sprintf("Proposta criada (id=%s): %s. O dono pode clicar em Aplicar ou Recusar no cartão da conversa.", prop.ID, headline), nil
}

func (s *Server) decorateProposal(ctx context.Context, p store.Proposal, names map[string]string) ProposalView {
	fromName := names[p.FromAgent]
	if fromName == "" && p.FromAgent != "" {
		if ag, err := s.store.GetAgent(ctx, p.FromAgent); err == nil {
			fromName = ag.Name
		} else {
			fromName = p.FromAgent
		}
	}
	targetName := names[p.TargetAgent]
	if targetName == "" && p.TargetAgent != "" {
		if ag, err := s.store.GetAgent(ctx, p.TargetAgent); err == nil {
			targetName = ag.Name
		} else {
			targetName = p.TargetAgent
		}
	}
	return ProposalView{
		Proposal:   p,
		FromName:   fromName,
		TargetName: targetName,
		Headline:   proposalHeadline(fromName, targetName, p.Field),
	}
}

func (s *Server) agentNamesMap(ctx context.Context) map[string]string {
	out := map[string]string{}
	if agents, err := s.store.ListAgents(ctx); err == nil {
		for _, a := range agents {
			out[a.ID] = a.Name
		}
	}
	return out
}

func (s *Server) listProposals(w http.ResponseWriter, r *http.Request) {
	wsID, ok := s.requireWorkspace(w, r)
	if !ok {
		return
	}
	ctx := store.WithWorkspace(r.Context(), wsID)
	q := r.URL.Query()
	status := strings.TrimSpace(q.Get("status"))
	if status != "" && !store.ValidProposalStatus(status) {
		http.Error(w, "invalid status", http.StatusBadRequest)
		return
	}
	limit := 100
	if v := q.Get("limit"); v != "" {
		n, err := strconv.Atoi(v)
		if err != nil || n < 1 || n > 500 {
			http.Error(w, "invalid limit", http.StatusBadRequest)
			return
		}
		limit = n
	}
	target := pickStr(q.Get("target_agent"), q.Get("agent"))
	list, err := s.store.ListProposals(ctx, store.ProposalFilter{
		WorkspaceID:    wsID,
		ConversationID: strings.TrimSpace(q.Get("conversation")),
		TargetAgent:    strings.TrimSpace(target),
		Status:         status,
		Limit:          limit,
	})
	if err != nil {
		storeError(w, err)
		return
	}
	names := s.agentNamesMap(ctx)
	out := make([]ProposalView, len(list))
	for i, p := range list {
		out[i] = s.decorateProposal(ctx, p, names)
	}
	writeJSON(w, http.StatusOK, out)
}

func (s *Server) getProposal(w http.ResponseWriter, r *http.Request) {
	wsID, ok := s.requireWorkspace(w, r)
	if !ok {
		return
	}
	ctx := store.WithWorkspace(r.Context(), wsID)
	prop, err := s.store.GetProposal(ctx, r.PathValue("id"))
	if err != nil || prop.WorkspaceID != wsID {
		storeError(w, store.ErrNotFound)
		return
	}
	writeJSON(w, http.StatusOK, s.decorateProposal(ctx, prop, s.agentNamesMap(ctx)))
}

func (s *Server) createProposalHTTP(w http.ResponseWriter, r *http.Request) {
	wsID, ok := s.requireWorkspace(w, r)
	if !ok {
		return
	}
	ctx := store.WithWorkspace(r.Context(), wsID)
	var req ProposalRequest
	if !readJSONMax(w, r, &req, maxPersona*2+4096) {
		return
	}
	target, err := s.store.GetAgent(ctx, strings.TrimSpace(req.TargetAgent))
	if err != nil || target.WorkspaceID != wsID {
		storeError(w, store.ErrNotFound)
		return
	}
	field, ok := normalizeProposalField(req.Field)
	if !ok {
		http.Error(w, "field must be instructions, role or apps", http.StatusBadRequest)
		return
	}
	afterRaw, err := decodeAfterValue(req.After)
	if err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}
	after, err := validateProposalAfter(field, afterRaw)
	if err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}
	reason := strings.TrimSpace(req.Reason)
	if reason == "" {
		http.Error(w, "reason required", http.StatusBadRequest)
		return
	}
	fromID := strings.TrimSpace(req.FromAgent)
	if fromID != "" {
		if fromAg, err := s.store.GetAgent(ctx, fromID); err != nil || fromAg.WorkspaceID != wsID {
			http.Error(w, "unknown from_agent", http.StatusBadRequest)
			return
		}
	}
	before := s.proposalBeforeValue(ctx, target, field)
	prop, err := s.store.CreateProposal(ctx, store.Proposal{
		ID:             newID()[:12],
		WorkspaceID:    wsID,
		ConversationID: strings.TrimSpace(req.ConversationID),
		FromAgent:      fromID,
		TargetAgent:    target.ID,
		Field:          field,
		Before:         before,
		After:          after,
		Reason:         truncate(reason, 500),
		Status:         "pending",
	}, actorFrom(r), s.now())
	if err != nil {
		storeError(w, err)
		return
	}
	writeJSON(w, http.StatusCreated, s.decorateProposal(ctx, prop, s.agentNamesMap(ctx)))
}

func (s *Server) applyProposal(w http.ResponseWriter, r *http.Request) {
	wsID, ok := s.requireWorkspace(w, r)
	if !ok {
		return
	}
	ctx := store.WithWorkspace(r.Context(), wsID)
	prop, err := s.store.GetProposal(ctx, r.PathValue("id"))
	if err != nil || prop.WorkspaceID != wsID {
		storeError(w, store.ErrNotFound)
		return
	}
	if prop.Status != "pending" {
		http.Error(w, "proposal is not pending", http.StatusConflict)
		return
	}
	target, err := s.store.GetAgent(ctx, prop.TargetAgent)
	if err != nil || target.WorkspaceID != wsID {
		storeError(w, store.ErrNotFound)
		return
	}
	preVersion := target.Version
	prop.Before = s.proposalBeforeValue(ctx, target, prop.Field)
	switch prop.Field {
	case "instructions":
		target.Persona = prop.After
	case "role":
		target.Role = prop.After
	case "apps":
		apps := parseAppsList(prop.After)
		if len(apps) == 0 {
			target.Apps = nil
		} else {
			target.Apps = apps
		}
		s.syncAgentConnections(ctx, target.ID, apps, actorFrom(r))
	default:
		http.Error(w, "invalid proposal field", http.StatusBadRequest)
		return
	}
	now := s.now()
	saved, err := s.store.SaveAgent(ctx, target, actorFrom(r), now)
	if err != nil {
		storeError(w, err)
		return
	}
	prop.Status = "applied"
	prop.PreVersion = preVersion
	prop.AppliedVersion = saved.Version
	prop.DecidedAt = &now
	updated, err := s.store.UpdateProposal(ctx, prop, actorFrom(r), now)
	if err != nil {
		storeError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, s.decorateProposal(ctx, updated, s.agentNamesMap(ctx)))
}

func (s *Server) rejectProposal(w http.ResponseWriter, r *http.Request) {
	wsID, ok := s.requireWorkspace(w, r)
	if !ok {
		return
	}
	ctx := store.WithWorkspace(r.Context(), wsID)
	prop, err := s.store.GetProposal(ctx, r.PathValue("id"))
	if err != nil || prop.WorkspaceID != wsID {
		storeError(w, store.ErrNotFound)
		return
	}
	if prop.Status != "pending" {
		http.Error(w, "proposal is not pending", http.StatusConflict)
		return
	}
	now := s.now()
	prop.Status = "rejected"
	prop.DecidedAt = &now
	updated, err := s.store.UpdateProposal(ctx, prop, actorFrom(r), now)
	if err != nil {
		storeError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, s.decorateProposal(ctx, updated, s.agentNamesMap(ctx)))
}

// undoProposal restores the target agent's snapshot from AgentVersions matching PreVersion.
func (s *Server) undoProposal(w http.ResponseWriter, r *http.Request) {
	wsID, ok := s.requireWorkspace(w, r)
	if !ok {
		return
	}
	ctx := store.WithWorkspace(r.Context(), wsID)
	prop, err := s.store.GetProposal(ctx, r.PathValue("id"))
	if err != nil || prop.WorkspaceID != wsID {
		storeError(w, store.ErrNotFound)
		return
	}
	if prop.Status != "applied" {
		http.Error(w, "only an applied proposal can be undone", http.StatusConflict)
		return
	}
	cur, err := s.store.GetAgent(ctx, prop.TargetAgent)
	if err != nil || cur.WorkspaceID != wsID {
		storeError(w, store.ErrNotFound)
		return
	}
	versions, err := s.store.AgentVersions(ctx, cur.ID)
	if err != nil {
		storeError(w, err)
		return
	}
	var snapshot *store.Agent
	for i := range versions {
		if versions[i].Version == prop.PreVersion {
			cp := versions[i].Agent
			snapshot = &cp
			break
		}
	}
	if snapshot == nil {
		http.Error(w, "previous agent version not found", http.StatusNotFound)
		return
	}
	snapshot.WorkspaceID = cur.WorkspaceID
	now := s.now()
	if _, err := s.store.SaveAgent(ctx, *snapshot, actorFrom(r), now); err != nil {
		storeError(w, err)
		return
	}
	if prop.Field == "apps" {
		s.syncAgentConnections(ctx, cur.ID, snapshot.Apps, actorFrom(r))
	}
	prop.Status = "undone"
	prop.DecidedAt = &now
	updated, err := s.store.UpdateProposal(ctx, prop, actorFrom(r), now)
	if err != nil {
		storeError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, s.decorateProposal(ctx, updated, s.agentNamesMap(ctx)))
}

func (s *Server) syncAgentConnections(ctx context.Context, agentID string, wantApps []string, actor string) {
	conns, err := s.store.ListConnections(ctx)
	if err != nil || len(conns) == 0 {
		return
	}
	want := map[string]bool{}
	for _, id := range wantApps {
		want[id] = true
	}
	for _, c := range conns {
		if len(c.Agents) == 0 {
			continue
		}
		has := false
		filtered := make([]string, 0, len(c.Agents))
		for _, id := range c.Agents {
			if id == agentID {
				has = true
			} else {
				filtered = append(filtered, id)
			}
		}
		shouldHave := want[c.ID] || want[c.Kind]
		if shouldHave && !has {
			c.Agents = append(filtered, agentID)
			_, _ = s.store.SaveConnection(ctx, c, actor, s.now())
		} else if !shouldHave && has && len(filtered) > 0 {
			c.Agents = filtered
			_, _ = s.store.SaveConnection(ctx, c, actor, s.now())
		}
	}
}
