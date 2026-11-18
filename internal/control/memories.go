package control

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"strconv"
	"strings"
	"unicode"
	"unicode/utf8"

	"github.com/GuardaChuva22/avora/internal/store"
)

// MemoryRequest creates or updates an agent or team memory from the app.
type MemoryRequest struct {
	AgentID  string `json:"agent_id"`
	Content  string `json:"content"`
	Kind     string `json:"kind"`
	Scope    string `json:"scope"`
	Source   string `json:"source"`
	Verified *bool  `json:"verified"`
	Pinned   *bool  `json:"pinned"`
	Archived *bool  `json:"archived"`
}

type rememberToolArgs struct {
	Conteudo string `json:"conteudo"`
	Content  string `json:"content"`
	Kind     string `json:"kind"`
	Tipo     string `json:"tipo"`
	Scope    string `json:"scope"`
	Escopo   string `json:"escopo"`
}

func (s *Server) registerMemories(mux *http.ServeMux) {
	mux.HandleFunc("GET /v1/memories", s.admin(s.searchMemories))
	mux.HandleFunc("POST /v1/memories", s.admin(s.createMemory))
	mux.HandleFunc("PATCH /v1/memories/{mid}", s.admin(s.patchMemoryByID))
	mux.HandleFunc("PUT /v1/memories/{mid}", s.admin(s.patchMemoryByID))
	mux.HandleFunc("DELETE /v1/memories/{mid}", s.admin(s.deleteMemoryByID))

	mux.HandleFunc("GET /v1/agents/{id}/memories", s.admin(s.listMemories))
	mux.HandleFunc("POST /v1/agents/{id}/memories", s.admin(s.addMemory))
	mux.HandleFunc("PUT /v1/agents/{id}/memories/{mid}", s.admin(s.editMemory))
	mux.HandleFunc("PATCH /v1/agents/{id}/memories/{mid}", s.admin(s.editMemory))
	mux.HandleFunc("DELETE /v1/agents/{id}/memories/{mid}", s.admin(s.deleteMemory))

	s.registerSectors(mux)
	s.registerProposals(mux)
}

// ── Agent memory tools & prompt block ────────────────────────────

func memorySource(t *turn) string {
	if t.channel {
		return "#time"
	}
	return "conversa"
}

func pickStr(vals ...string) string {
	for _, v := range vals {
		if strings.TrimSpace(v) != "" {
			return v
		}
	}
	return ""
}

func (s *Server) toolRemember(ctx context.Context, t *turn, a toolArgs) (string, error) {
	var extra rememberToolArgs
	_ = json.Unmarshal([]byte(a.raw), &extra)
	rawContent := pickStr(extra.Conteudo, extra.Content, a.Conteudo)
	content := strings.Join(strings.Fields(rawContent), " ")
	if content == "" {
		return "", errors.New("diga o que lembrar")
	}
	if utf8.RuneCountInString(content) > MaxMemoryChars {
		return "", fmt.Errorf("memória longa demais (máx. %d caracteres); resuma ou guarde num arquivo", MaxMemoryChars)
	}
	kind := strings.ToLower(strings.TrimSpace(pickStr(extra.Kind, extra.Tipo)))
	if kind == "" {
		kind = "fact"
	} else if !store.ValidMemoryKind(kind) {
		return "", errors.New("kind inválido; use decision, fact, preference, lesson ou rule")
	}
	scope := strings.ToLower(strings.TrimSpace(pickStr(extra.Scope, extra.Escopo)))
	if scope == "" {
		scope = "agent"
	} else if !store.ValidMemoryScope(scope) {
		return "", errors.New("scope inválido; use agent ou team")
	}

	wsID := t.agent.WorkspaceID
	if wsID == "" {
		wsID = "pessoal"
	}
	existing, err := s.store.SearchMemories(ctx, store.MemoryFilter{
		WorkspaceID: wsID,
		AgentID:      mapScopeAgent(scope, t.agent.ID),
		Scope:       scope,
		Limit:       MaxMemoriesPerAgent + 10,
	})
	if err != nil {
		return "", err
	}
	for _, m := range existing {
		if strings.EqualFold(m.Content, content) || (kind == "rule" && m.Kind == "rule" && lessonKey(m.Content) == lessonKey(content)) {
			if kind == "rule" && (!m.Verified || !m.Pinned || m.Kind != "rule") {
				vTrue, pTrue := true, true
				_, _ = s.store.PatchMemory(ctx, m.ID, store.MemoryPatch{
					Kind: &kind, Verified: &vTrue, Pinned: &pTrue,
				}, "agent:"+t.agent.ID, s.now())
			}
			return fmt.Sprintf("Já estava na memória (id=%d).", m.ID), nil
		}
	}
	if len(existing) >= MaxMemoriesPerAgent {
		return "", fmt.Errorf("sua memória está cheia (%d); apague as velhas com esquecer", MaxMemoriesPerAgent)
	}

	src := memorySource(t)
	verified, pinned := false, false
	if kind == "rule" {
		verified, pinned = true, true
		if !t.channel {
			src = "dono"
		}
	}
	agentID := t.agent.ID
	if scope == "team" {
		agentID = ""
	}
	m, err := s.store.AddMemory(ctx, store.AgentMemory{
		WorkspaceID: wsID,
		AgentID:     agentID,
		Scope:       scope,
		Kind:        kind,
		Content:     content,
		Source:      src,
		Verified:    verified,
		Pinned:      pinned,
	}, "agent:"+t.agent.ID, s.now())
	if err != nil {
		return "", err
	}
	s.step(t, AgentStep{Tool: "lembrar", Summary: content})
	return fmt.Sprintf("Guardado na memória (id=%d).", m.ID), nil
}

func mapScopeAgent(scope, agentID string) string {
	if scope == "team" {
		return ""
	}
	return agentID
}

func (s *Server) toolRecall(ctx context.Context, t *turn, a toolArgs) (string, error) {
	wsID := t.agent.WorkspaceID
	if wsID == "" {
		wsID = "pessoal"
	}
	list, err := s.store.SearchMemories(ctx, store.MemoryFilter{
		WorkspaceID: wsID,
		AgentID:     t.agent.ID,
		AgentOrTeam: true,
		Query:       a.Busca,
		Limit:       30,
	})
	if err != nil {
		return "", err
	}
	s.step(t, AgentStep{Tool: "recordar", Summary: fmt.Sprintf("«%s» · %d memórias", strings.TrimSpace(a.Busca), len(list))})
	if len(list) == 0 {
		return "Nada na memória com isso.", nil
	}
	var b strings.Builder
	for _, m := range list {
		tag := m.Kind
		if m.Scope == "team" {
			tag = "time/" + tag
		}
		note := ""
		if !m.Verified {
			note = " (dica, não confirmada)"
		}
		fmt.Fprintf(&b, "- id=%d [%s, %s]: %s%s\n", m.ID, tag, m.UpdatedAt.In(s.zone()).Format("02/01/06"), m.Content, note)
	}
	return b.String(), nil
}

func (s *Server) toolForget(ctx context.Context, t *turn, a toolArgs) (string, error) {
	id, err := strconv.ParseInt(strings.TrimSpace(a.ID), 10, 64)
	if err != nil {
		return "", errors.New("id inválido; use recordar para ver os ids")
	}
	list, err := s.store.ListMemories(ctx, t.agent.ID, "", 0)
	if err != nil {
		return "", err
	}
	for _, m := range list {
		if m.ID == id {
			if _, err := s.store.DeleteMemory(ctx, id, "agent:"+t.agent.ID, s.now()); err != nil {
				return "", err
			}
			s.step(t, AgentStep{Tool: "esquecer", Summary: m.Content})
			return "Memória apagada: " + m.Content, nil
		}
	}
	return "", errors.New("essa memória não é sua ou não existe")
}

// memoryBlock formats the agent's active memories (and its workspace's team memories)
// for the system prompt: pinned always enter, unverified are marked "(dica, não confirmada)",
// archived never enter.
func (s *Server) memoryBlock(ctx context.Context, agentID string) string {
	wsID := "pessoal"
	if ag, err := s.store.GetAgent(ctx, agentID); err == nil && ag.WorkspaceID != "" {
		wsID = ag.WorkspaceID
	}
	list, err := s.store.SearchMemories(ctx, store.MemoryFilter{
		WorkspaceID:     wsID,
		AgentID:         agentID,
		AgentOrTeam:     true,
		IncludeArchived: false,
		Limit:           500,
	})
	if err != nil || len(list) == 0 {
		return ""
	}
	var pinned, regular []store.AgentMemory
	for _, m := range list {
		if m.ArchivedAt != nil {
			continue
		}
		if m.Pinned {
			pinned = append(pinned, m)
		} else {
			regular = append(regular, m)
		}
	}
	if len(pinned) == 0 && len(regular) == 0 {
		return ""
	}
	var b strings.Builder
	b.WriteString("\n\nSua memória (fixadas sempre presentes; dicas não confirmadas devem ser tratadas com cautela):")
	for _, m := range pinned {
		b.WriteString(formatPromptMemoryLine(m))
	}
	shown := 0
	for _, m := range regular {
		if shown >= promptMemories {
			b.WriteString("\n- … (mais antigas: use recordar)")
			break
		}
		line := formatPromptMemoryLine(m)
		if b.Len()+len(line) > promptMemoryChars && len(pinned) == 0 {
			b.WriteString("\n- … (mais antigas: use recordar)")
			break
		}
		if b.Len()+len(line) > promptMemoryChars+3000 {
			b.WriteString("\n- … (mais antigas: use recordar)")
			break
		}
		b.WriteString(line)
		shown++
	}
	return b.String()
}

func formatPromptMemoryLine(m store.AgentMemory) string {
	kind := m.Kind
	if kind == "" {
		kind = "fact"
	}
	tag := fmt.Sprintf("%d · %s", m.ID, kind)
	if m.Scope == "team" {
		tag = fmt.Sprintf("%d · time · %s", m.ID, kind)
	}
	if m.Pinned {
		tag += " · fixada"
	}
	suffix := ""
	if !m.Verified {
		suffix = " (dica, não confirmada)"
	}
	return fmt.Sprintf("\n- [%s] %s%s", tag, m.Content, suffix)
}

// ── Auto-learning: owner rules, corrections, denied approvals ────

// observeOwnerMessage inspects a direct message from a human owner before the turn runs:
// explicit rules become verified+pinned rules; corrections become unverified lessons.
func (s *Server) observeOwnerMessage(ctx context.Context, convID string, agent store.Agent, prior []store.ChatMessage, content, actor string) {
	if actor != store.ActorAdmin && !strings.HasPrefix(actor, "person:") {
		return
	}
	trimmed := strings.Join(strings.Fields(content), " ")
	if trimmed == "" || utf8.RuneCountInString(trimmed) > MaxMemoryChars {
		return
	}
	if isOwnerRule(trimmed) {
		_, _ = s.recordOwnerRule(ctx, agent, trimmed)
		return
	}
	hasPriorAssistant := false
	for _, m := range prior {
		if m.Role == "assistant" {
			hasPriorAssistant = true
			break
		}
	}
	if isOwnerCorrection(trimmed, hasPriorAssistant) {
		_, _, _ = s.learnLesson(ctx, agent.ID, convID, trimmed, "dono", "")
	}
}

func (s *Server) recordOwnerRule(ctx context.Context, agent store.Agent, text string) (store.AgentMemory, error) {
	wsID := agent.WorkspaceID
	if wsID == "" {
		wsID = "pessoal"
	}
	existing, err := s.store.SearchMemories(ctx, store.MemoryFilter{
		WorkspaceID: wsID,
		AgentID:     agent.ID,
		Scope:       "agent",
		Limit:       MaxMemoriesPerAgent,
	})
	if err == nil {
		key := lessonKey(text)
		for _, m := range existing {
			if lessonKey(m.Content) == key {
				kind := "rule"
				vTrue, pTrue := true, true
				src := "dono"
				return s.store.PatchMemory(ctx, m.ID, store.MemoryPatch{
					Kind: &kind, Verified: &vTrue, Pinned: &pTrue, Source: &src,
				}, store.ActorAdmin, s.now())
			}
		}
	}
	return s.store.AddMemory(ctx, store.AgentMemory{
		WorkspaceID: wsID,
		AgentID:     agent.ID,
		Scope:       "agent",
		Kind:        "rule",
		Content:     text,
		Source:      "dono",
		Verified:    true,
		Pinned:      true,
	}, store.ActorAdmin, s.now())
}

func (s *Server) recordDeniedApprovalLesson(ctx context.Context, ap store.Approval) {
	if ap.AgentID == "" {
		return
	}
	summary := strings.TrimSpace(firstLine(ap.Summary))
	text := "Ação recusada (" + ap.Action + ")"
	if summary != "" {
		text += ": " + truncate(summary, 200)
	}
	groupPrefix := "acao recusada " + lessonKey(ap.Action)
	_, _, _ = s.learnLesson(ctx, ap.AgentID, ap.ConversationID, text, "aprovação recusada", groupPrefix)
}

// learnLesson records an unverified lesson for agentID; once the same lesson appears
// 3 times, it creates a pending improvement proposal on the agent's instructions.
func (s *Server) learnLesson(ctx context.Context, agentID, convID, rawText, source, groupKey string) (store.AgentMemory, *store.Proposal, error) {
	text := strings.Join(strings.Fields(rawText), " ")
	if text == "" {
		return store.AgentMemory{}, nil, errors.New("empty lesson")
	}
	if utf8.RuneCountInString(text) > MaxMemoryChars {
		text = string([]rune(text)[:MaxMemoryChars])
	}
	ag, err := s.store.GetAgent(ctx, agentID)
	if err != nil {
		return store.AgentMemory{}, nil, err
	}
	wsID := ag.WorkspaceID
	if wsID == "" {
		wsID = "pessoal"
	}
	lessons, err := s.store.SearchMemories(ctx, store.MemoryFilter{
		WorkspaceID: wsID,
		AgentID:     agentID,
		Scope:       "agent",
		Kind:        "lesson",
		Limit:       MaxMemoriesPerAgent,
	})
	if err != nil {
		return store.AgentMemory{}, nil, err
	}
	key := lessonKey(text)
	var mem store.AgentMemory
	found := false
	for _, cur := range lessons {
		curKey := lessonKey(cur.Content)
		if curKey == key || (groupKey != "" && strings.HasPrefix(curKey, groupKey)) {
			occ := cur.Occurrences + 1
			mem, err = s.store.PatchMemory(ctx, cur.ID, store.MemoryPatch{Occurrences: &occ}, store.ActorSystem, s.now())
			if err != nil {
				return store.AgentMemory{}, nil, err
			}
			found = true
			break
		}
	}
	if !found {
		mem, err = s.store.AddMemory(ctx, store.AgentMemory{
			WorkspaceID: wsID,
			AgentID:     agentID,
			Scope:       "agent",
			Kind:        "lesson",
			Content:     text,
			Source:      source,
			Verified:    false,
			Pinned:      false,
			Occurrences: 1,
		}, store.ActorSystem, s.now())
		if err != nil {
			return store.AgentMemory{}, nil, err
		}
	}
	if mem.Occurrences < 3 {
		return mem, nil, nil
	}
	pending, _ := s.store.ListProposals(ctx, store.ProposalFilter{
		WorkspaceID: wsID,
		TargetAgent: agentID,
		Status:      "pending",
		Limit:       100,
	})
	for i := range pending {
		if pending[i].Field == "instructions" && strings.Contains(lessonKey(pending[i].After), lessonKey(mem.Content)) {
			return mem, &pending[i], nil
		}
	}
	after := appendInstructionLesson(ag.Persona, mem.Content)
	if after == ag.Persona {
		return mem, nil, nil
	}
	prop, err := s.store.CreateProposal(ctx, store.Proposal{
		ID:             newID()[:12],
		WorkspaceID:    wsID,
		ConversationID: convID,
		FromAgent:      "",
		TargetAgent:    ag.ID,
		Field:          "instructions",
		Before:         ag.Persona,
		After:          after,
		Reason:         fmt.Sprintf("A mesma lição apareceu %d vezes: %s", mem.Occurrences, truncate(mem.Content, 160)),
		Status:         "pending",
	}, store.ActorSystem, s.now())
	if err != nil {
		return mem, nil, err
	}
	return mem, &prop, nil
}

func appendInstructionLesson(persona, lesson string) string {
	bullet := "- " + strings.TrimSpace(lesson)
	if strings.Contains(lessonKey(persona), lessonKey(lesson)) {
		return persona
	}
	base := strings.TrimRight(persona, "\n ")
	if base == "" {
		return bullet
	}
	return base + "\n" + bullet
}

func lessonKey(s string) string {
	folded := foldText(strings.ToLower(s))
	var b strings.Builder
	space := false
	for _, r := range folded {
		if unicode.IsLetter(r) || unicode.IsDigit(r) {
			b.WriteRune(r)
			space = false
		} else if !space && b.Len() > 0 {
			b.WriteByte(' ')
			space = true
		}
	}
	return strings.TrimSpace(b.String())
}

func isOwnerRule(text string) bool {
	trimmed := strings.TrimSpace(text)
	if len(trimmed) < 8 || strings.HasSuffix(trimmed, "?") {
		return false
	}
	low := foldText(strings.ToLower(trimmed))
	for _, p := range []string{
		"nunca ", "jamais ", "regra:", "regra -", "proibido ",
		"sempre peca ", "sempre me mostre", "sempre me avise", "sempre confirme",
	} {
		if strings.HasPrefix(low, p) {
			return true
		}
	}
	if (strings.HasPrefix(low, "nao mande ") || strings.HasPrefix(low, "nao envie ")) && strings.Contains(low, " sem ") {
		return true
	}
	if strings.Contains(low, " nunca ") && (strings.Contains(low, " sem ") || strings.Contains(low, " antes de ")) {
		return true
	}
	return false
}

func isOwnerCorrection(text string, hasPriorAssistant bool) bool {
	trimmed := strings.TrimSpace(text)
	if len(trimmed) < 6 || strings.HasSuffix(trimmed, "?") || isOwnerRule(trimmed) {
		return false
	}
	low := foldText(strings.ToLower(trimmed))
	for _, p := range []string{
		"correcao:", "corrigindo:", "corrigindo,", "voce errou", "isso esta errado",
		"nao faca mais isso", "pare de ",
	} {
		if strings.HasPrefix(low, p) {
			return true
		}
	}
	if !hasPriorAssistant {
		return false
	}
	for _, p := range []string{
		"nao, ", "nao! ", "errado,", "errado.", "errado:", "na verdade, ",
		"nao use ", "nao faca ", "nao quero que ",
	} {
		if strings.HasPrefix(low, p) {
			return true
		}
	}
	return false
}

// ── HTTP API ────────────────────────────────────────────────────

func parseBoolParam(v string) (*bool, bool) {
	switch strings.ToLower(strings.TrimSpace(v)) {
	case "":
		return nil, true
	case "1", "true":
		b := true
		return &b, true
	case "0", "false":
		b := false
		return &b, true
	}
	return nil, false
}

func (s *Server) searchMemories(w http.ResponseWriter, r *http.Request) {
	wsID, ok := s.requireWorkspace(w, r)
	if !ok {
		return
	}
	q := r.URL.Query()
	scope := strings.TrimSpace(q.Get("scope"))
	if scope != "" && !store.ValidMemoryScope(scope) {
		http.Error(w, "invalid scope", http.StatusBadRequest)
		return
	}
	kind := strings.TrimSpace(q.Get("kind"))
	if kind != "" && !store.ValidMemoryKind(kind) {
		http.Error(w, "invalid kind", http.StatusBadRequest)
		return
	}
	verified, ok := parseBoolParam(q.Get("verified"))
	if !ok {
		http.Error(w, "invalid verified", http.StatusBadRequest)
		return
	}
	pinned, ok := parseBoolParam(q.Get("pinned"))
	if !ok {
		http.Error(w, "invalid pinned", http.StatusBadRequest)
		return
	}
	limit := 300
	if v := q.Get("limit"); v != "" {
		n, err := strconv.Atoi(v)
		if err != nil || n < 1 || n > 1000 {
			http.Error(w, "invalid limit", http.StatusBadRequest)
			return
		}
		limit = n
	}
	arch := strings.ToLower(strings.TrimSpace(q.Get("archived")))
	f := store.MemoryFilter{
		WorkspaceID:     wsID,
		AgentID:         strings.TrimSpace(q.Get("agent")),
		Scope:           scope,
		Kind:            kind,
		Query:           q.Get("q"),
		Verified:        verified,
		Pinned:          pinned,
		IncludeArchived: arch == "all",
		ArchivedOnly:    arch == "1" || arch == "true",
		Limit:           limit,
	}
	list, err := s.store.SearchMemories(store.WithWorkspace(r.Context(), wsID), f)
	if err != nil {
		storeError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, list)
}

func (s *Server) createMemory(w http.ResponseWriter, r *http.Request) {
	wsID, ok := s.requireWorkspace(w, r)
	if !ok {
		return
	}
	var req MemoryRequest
	if !readJSON(w, r, &req) {
		return
	}
	content := strings.Join(strings.Fields(req.Content), " ")
	if content == "" || utf8.RuneCountInString(content) > MaxMemoryChars {
		http.Error(w, fmt.Sprintf("content must have 1 to %d characters", MaxMemoryChars), http.StatusBadRequest)
		return
	}
	scope := strings.ToLower(strings.TrimSpace(req.Scope))
	if scope == "" {
		if strings.TrimSpace(req.AgentID) == "" {
			scope = "team"
		} else {
			scope = "agent"
		}
	}
	if !store.ValidMemoryScope(scope) {
		http.Error(w, "invalid scope", http.StatusBadRequest)
		return
	}
	kind := strings.ToLower(strings.TrimSpace(req.Kind))
	if kind == "" {
		kind = "fact"
	} else if !store.ValidMemoryKind(kind) {
		http.Error(w, "invalid kind", http.StatusBadRequest)
		return
	}
	agentID := strings.TrimSpace(req.AgentID)
	if scope == "team" {
		agentID = ""
	} else {
		ag, err := s.store.GetAgent(r.Context(), agentID)
		if err != nil || ag.WorkspaceID != wsID {
			storeError(w, store.ErrNotFound)
			return
		}
	}
	source := strings.TrimSpace(req.Source)
	if source == "" {
		source = "dono"
	}
	verified := source == "dono" || kind == "rule"
	if req.Verified != nil {
		verified = *req.Verified
	}
	pinned := kind == "rule"
	if req.Pinned != nil {
		pinned = *req.Pinned
	}
	m, err := s.store.AddMemory(store.WithWorkspace(r.Context(), wsID), store.AgentMemory{
		WorkspaceID: wsID,
		AgentID:     agentID,
		Scope:       scope,
		Kind:        kind,
		Content:     content,
		Source:      source,
		Verified:    verified,
		Pinned:      pinned,
	}, actorFrom(r), s.now())
	if err != nil {
		storeError(w, err)
		return
	}
	writeJSON(w, http.StatusCreated, m)
}

func (s *Server) patchMemoryByID(w http.ResponseWriter, r *http.Request) {
	wsID, ok := s.requireWorkspace(w, r)
	if !ok {
		return
	}
	mid, err := strconv.ParseInt(r.PathValue("mid"), 10, 64)
	if err != nil {
		http.Error(w, "invalid memory id", http.StatusBadRequest)
		return
	}
	cur, err := s.store.GetMemory(r.Context(), mid)
	if err != nil || cur.WorkspaceID != wsID {
		storeError(w, store.ErrNotFound)
		return
	}
	s.applyMemoryUpdate(w, r, mid)
}

func (s *Server) deleteMemoryByID(w http.ResponseWriter, r *http.Request) {
	wsID, ok := s.requireWorkspace(w, r)
	if !ok {
		return
	}
	mid, err := strconv.ParseInt(r.PathValue("mid"), 10, 64)
	if err != nil {
		http.Error(w, "invalid memory id", http.StatusBadRequest)
		return
	}
	cur, err := s.store.GetMemory(r.Context(), mid)
	if err != nil || cur.WorkspaceID != wsID {
		storeError(w, store.ErrNotFound)
		return
	}
	if _, err := s.store.DeleteMemory(r.Context(), mid, actorFrom(r), s.now()); err != nil {
		storeError(w, err)
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

func (s *Server) listMemories(w http.ResponseWriter, r *http.Request) {
	wsID, ok := s.requireWorkspace(w, r)
	if !ok {
		return
	}
	ag, err := s.store.GetAgent(r.Context(), r.PathValue("id"))
	if err != nil || ag.WorkspaceID != wsID {
		storeError(w, store.ErrNotFound)
		return
	}
	q := r.URL.Query()
	kind := strings.TrimSpace(q.Get("kind"))
	if kind != "" && !store.ValidMemoryKind(kind) {
		http.Error(w, "invalid kind", http.StatusBadRequest)
		return
	}
	verified, ok := parseBoolParam(q.Get("verified"))
	if !ok {
		http.Error(w, "invalid verified", http.StatusBadRequest)
		return
	}
	pinned, ok := parseBoolParam(q.Get("pinned"))
	if !ok {
		http.Error(w, "invalid pinned", http.StatusBadRequest)
		return
	}
	arch := strings.ToLower(strings.TrimSpace(q.Get("archived")))
	if kind == "" && verified == nil && pinned == nil && arch == "" {
		list, err := s.store.ListMemories(r.Context(), ag.ID, q.Get("q"), MaxMemoriesPerAgent)
		if err != nil {
			storeError(w, err)
			return
		}
		writeJSON(w, http.StatusOK, list)
		return
	}
	list, err := s.store.SearchMemories(store.WithWorkspace(r.Context(), wsID), store.MemoryFilter{
		WorkspaceID:     wsID,
		AgentID:         ag.ID,
		Scope:           "agent",
		Kind:            kind,
		Query:           q.Get("q"),
		Verified:        verified,
		Pinned:          pinned,
		IncludeArchived: arch == "all",
		ArchivedOnly:    arch == "1" || arch == "true",
		Limit:           MaxMemoriesPerAgent,
	})
	if err != nil {
		storeError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, list)
}

func (s *Server) addMemory(w http.ResponseWriter, r *http.Request) {
	wsID, ok := s.requireWorkspace(w, r)
	if !ok {
		return
	}
	var req MemoryRequest
	if !readJSON(w, r, &req) {
		return
	}
	content := strings.Join(strings.Fields(req.Content), " ")
	if content == "" || utf8.RuneCountInString(content) > MaxMemoryChars {
		http.Error(w, fmt.Sprintf("content must have 1 to %d characters", MaxMemoryChars), http.StatusBadRequest)
		return
	}
	id := r.PathValue("id")
	ag, err := s.store.GetAgent(r.Context(), id)
	if err != nil || ag.WorkspaceID != wsID {
		storeError(w, store.ErrNotFound)
		return
	}
	if list, err := s.store.ListMemories(r.Context(), id, "", 0); err == nil && len(list) >= MaxMemoriesPerAgent {
		http.Error(w, "memory full", http.StatusConflict)
		return
	}
	kind := strings.ToLower(strings.TrimSpace(req.Kind))
	if kind == "" {
		kind = "fact"
	} else if !store.ValidMemoryKind(kind) {
		http.Error(w, "invalid kind", http.StatusBadRequest)
		return
	}
	scope := strings.ToLower(strings.TrimSpace(req.Scope))
	if scope == "" {
		scope = "agent"
	} else if !store.ValidMemoryScope(scope) {
		http.Error(w, "invalid scope", http.StatusBadRequest)
		return
	}
	source := strings.TrimSpace(req.Source)
	if source == "" {
		source = "dono"
	}
	verified := source == "dono" || kind == "rule"
	if req.Verified != nil {
		verified = *req.Verified
	}
	pinned := kind == "rule"
	if req.Pinned != nil {
		pinned = *req.Pinned
	}
	agentID := id
	if scope == "team" {
		agentID = ""
	}
	m, err := s.store.AddMemory(store.WithWorkspace(r.Context(), wsID), store.AgentMemory{
		WorkspaceID: wsID,
		AgentID:     agentID,
		Scope:       scope,
		Kind:        kind,
		Content:     content,
		Source:      source,
		Verified:    verified,
		Pinned:      pinned,
	}, actorFrom(r), s.now())
	if err != nil {
		storeError(w, err)
		return
	}
	writeJSON(w, http.StatusCreated, m)
}

func (s *Server) ownMemory(w http.ResponseWriter, r *http.Request) (int64, bool) {
	wsID, ok := s.requireWorkspace(w, r)
	if !ok {
		return 0, false
	}
	mid, err := strconv.ParseInt(r.PathValue("mid"), 10, 64)
	if err != nil {
		http.Error(w, "invalid memory id", http.StatusBadRequest)
		return 0, false
	}
	mem, err := s.store.GetMemory(r.Context(), mid)
	if err != nil || mem.AgentID != r.PathValue("id") || mem.WorkspaceID != wsID {
		http.Error(w, "not found", http.StatusNotFound)
		return 0, false
	}
	return mid, true
}

func (s *Server) editMemory(w http.ResponseWriter, r *http.Request) {
	mid, ok := s.ownMemory(w, r)
	if !ok {
		return
	}
	s.applyMemoryUpdate(w, r, mid)
}

func (s *Server) applyMemoryUpdate(w http.ResponseWriter, r *http.Request, mid int64) {
	var raw map[string]json.RawMessage
	if !readJSON(w, r, &raw) {
		return
	}
	// A client that sends the whole struct (empty strings, nulls) means
	// "leave as is" for those fields: content, kind and scope can't be blank.
	for k, v := range raw {
		if string(v) == "null" || (string(v) == `""` && (k == "content" || k == "kind" || k == "scope")) {
			delete(raw, k)
		}
	}
	var req MemoryRequest
	b, _ := json.Marshal(raw)
	_ = json.Unmarshal(b, &req)

	var patch store.MemoryPatch
	hasAny := false
	if _, ok := raw["content"]; ok {
		content := strings.Join(strings.Fields(req.Content), " ")
		if content == "" || utf8.RuneCountInString(content) > MaxMemoryChars {
			http.Error(w, fmt.Sprintf("content must have 1 to %d characters", MaxMemoryChars), http.StatusBadRequest)
			return
		}
		patch.Content = &content
		hasAny = true
	}
	if _, ok := raw["kind"]; ok {
		kind := strings.ToLower(strings.TrimSpace(req.Kind))
		if !store.ValidMemoryKind(kind) {
			http.Error(w, "invalid kind", http.StatusBadRequest)
			return
		}
		patch.Kind = &kind
		hasAny = true
	}
	if _, ok := raw["scope"]; ok {
		scope := strings.ToLower(strings.TrimSpace(req.Scope))
		if !store.ValidMemoryScope(scope) {
			http.Error(w, "invalid scope", http.StatusBadRequest)
			return
		}
		patch.Scope = &scope
		hasAny = true
	}
	if _, ok := raw["source"]; ok {
		src := strings.TrimSpace(req.Source)
		patch.Source = &src
		hasAny = true
	}
	if req.Verified != nil {
		patch.Verified = req.Verified
		hasAny = true
	}
	if req.Pinned != nil {
		patch.Pinned = req.Pinned
		hasAny = true
	}
	if req.Archived != nil {
		patch.Archived = req.Archived
		hasAny = true
	}
	if !hasAny {
		http.Error(w, "nothing to update", http.StatusBadRequest)
		return
	}
	m, err := s.store.PatchMemory(r.Context(), mid, patch, actorFrom(r), s.now())
	if err != nil {
		storeError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, m)
}

func (s *Server) deleteMemory(w http.ResponseWriter, r *http.Request) {
	mid, ok := s.ownMemory(w, r)
	if !ok {
		return
	}
	if _, err := s.store.DeleteMemory(r.Context(), mid, actorFrom(r), s.now()); err != nil {
		storeError(w, err)
		return
	}
	w.WriteHeader(http.StatusNoContent)
}
