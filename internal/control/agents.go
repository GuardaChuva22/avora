package control

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"regexp"
	"strconv"
	"strings"
	"time"
	"unicode/utf8"

	"github.com/GuardaChuva22/avora/internal/store"
)

// Funcionários (M1). Each agent has a name, a role, a persona and
// responsibilities. Every one thinks with the native model and acts through
// tools; Claude Code and Codex are tools it delegates to, never its engine.

const (
	maxPersona = 8000
	// DefaultAgentID is the agent that gets conversations with no agent.
	DefaultAgentID = "vega"
)

var (
	agentIDPattern = regexp.MustCompile(`^[a-z0-9][a-z0-9-]{1,31}$`)
	colorPattern   = regexp.MustCompile(`^#[0-9a-fA-F]{6}$`)
)

// AgentRequest creates or edits an agent. On edit, the id comes from the path.
type AgentRequest struct {
	ID       string `json:"id"`
	Name     string `json:"name"`
	Role     string `json:"role"`
	Persona  string `json:"persona"`
	Engine   string `json:"engine"`
	Model    string `json:"model"`
	Mode     string `json:"mode"`
	Color    string `json:"color"`
	Position *int   `json:"position"`
	// Responsibilities replace the agent's list when set; omitted, they stay.
	Responsibilities *[]store.Responsibility `json:"responsibilities"`
	// ProjectID puts a new agent in a project (on create only; moving is
	// PUT /v1/agents/{id}/project).
	ProjectID string `json:"project_id"`
	// Style replaces how the agent talks when set; omitted, it stays.
	Style *store.AgentStyle `json:"style"`
	// Brain replaces what the agent thinks with when set (D55).
	Brain *store.AgentBrain `json:"brain"`
	// Approvals replaces when it asks the owner when set (D56).
	Approvals *string `json:"approvals"`
	// HomeNodeID and HomeDir set the agent's home computer and folder when present.
	HomeNodeID *string `json:"home_node_id"`
	HomeDir    *string `json:"home_dir"`
	// Sector, ReportsTo, Pinned, Autonomy and Apps organize the team and bound autonomy.
	Sector    *string              `json:"sector"`
	ReportsTo *string              `json:"reports_to"`
	Pinned    *bool                `json:"pinned"`
	Autonomy  *store.AgentAutonomy `json:"autonomy"`
	Apps      *[]string            `json:"apps"`
}

// AgentStatus is an agent as the app lists it: with its latest conversation
// and whether it is working on an answer right now.
type AgentStatus struct {
	store.Agent
	Working       bool       `json:"working"`
	Status        string     `json:"status"`
	StatusChannel string     `json:"status_channel,omitempty"`
	Conversations int        `json:"conversations"`
	LastAt        *time.Time `json:"last_at,omitempty"`
	Preview       string     `json:"preview,omitempty"`
	LastRole      string     `json:"last_role,omitempty"`
	// AvatarAt is when the photo last changed (none: no photo), so the app
	// knows when to fetch it again.
	AvatarAt *time.Time `json:"avatar_at,omitempty"`
}

// teamTemplates is the starting team (spec M1). Every field is editable.
var teamTemplates = []store.Agent{
	{ID: "vega", Name: "Vega", Role: "Chefe de Gabinete", Engine: "native", Color: "#c9b8ff", Position: 0,
		Persona: `Você coordena o time e é quem mais conversa com o seu dono.
- Entenda o que ele quer, organize em passos e diga quem do time cuida de cada parte (Atlas para código, Iris para pesquisa, Cora para dinheiro, Nico para crescimento).
- Responda curto e direto. Termine com o próximo passo concreto quando houver um.
- Se faltar informação para decidir, faça uma pergunta só.`},
	{ID: "atlas", Name: "Atlas", Role: "Dev", Engine: "native", Color: "#7fd1ff", Position: 1,
		Persona: `Você é o desenvolvedor do time. Para escrever ou mudar código, delegue ao Claude Code (ou ao Codex) com pode_editar=true; os arquivos ficam na sua pasta de trabalho.
- Antes de algo grande, diga em uma linha o plano e então delegue com uma tarefa completa.
- Prefira mudanças pequenas e testáveis. No fim, resuma o que mudou e onde, com base no que o Claude Code respondeu.
- Ele ainda não tem terminal: não diga que comandos ou testes rodaram.`},
	{ID: "iris", Name: "Iris", Role: "Pesquisadora", Engine: "native", Color: "#ffc48a", Position: 2,
		Persona: `Você pesquisa a fundo e responde com fatos verificados. Para buscar na web, delegue ao Claude Code uma pesquisa com as perguntas exatas e peça os links das fontes.
- Compare fontes, aponte divergências e diga o grau de confiança.
- Entregue em tópicos curtos, com os links das fontes no fim.
- Não invente dados: se não achar, diga que não achou.`},
	{ID: "cora", Name: "Cora", Role: "Financeiro", Engine: "native", Color: "#86e3bd", Position: 3,
		Persona: `Você cuida do dinheiro do seu dono: orçamento, gastos, metas e decisões de compra.
- Faça as contas explicitamente e mostre os números.
- Quando o Omniarch estiver ligado, os dados de finanças virão de lá; até lá, trabalhe com o que ele te contar.
- Não dê recomendação de investimento: você não é consultor licenciado.`},
	{ID: "nico", Name: "Nico", Role: "Growth", Engine: "native", Color: "#ff9db8", Position: 4,
		Persona: `Você procura formas concretas de o seu dono crescer: renda, audiência e oportunidades ligadas às habilidades reais dele.
- Proponha ideias com passo a passo, custo e tempo estimados.
- Seja específico: nomes de plataformas, números, prazos. Nada de conselho genérico.`},
}

// EnsureTeam creates the #time channel and, when there are no agents yet,
// the starting team.
func (s *Server) EnsureTeam(ctx context.Context) error {
	if err := s.ensureChannel(ctx); err != nil {
		return err
	}
	agents, err := s.store.ListAgents(ctx)
	if err != nil || len(agents) > 0 {
		return err
	}
	// An owner who archived the whole team keeps it archived.
	if archived, err := s.store.ListArchivedAgents(ctx); err != nil || len(archived) > 0 {
		return err
	}
	for _, a := range teamTemplates {
		if _, err := s.store.SaveAgent(ctx, a, store.ActorSystem, s.now()); err != nil {
			return err
		}
	}
	// Existing installs got these from migration 007.
	for _, rt := range seedRoutines() {
		if _, err := s.store.SaveRoutine(ctx, rt, store.ActorSystem, s.now()); err != nil {
			return err
		}
	}
	return nil
}

func (s *Server) registerAgents(mux *http.ServeMux) {
	mux.HandleFunc("GET /v1/agents", s.admin(s.listAgents))
	mux.HandleFunc("GET /v1/brains", s.admin(s.listBrains))
	mux.HandleFunc("GET /v1/brain/default", s.admin(s.getDefaultBrain))
	mux.HandleFunc("PUT /v1/brain/default", s.admin(s.setDefaultBrain))
	mux.HandleFunc("POST /v1/agents", s.admin(s.createAgent))
	mux.HandleFunc("GET /v1/agents/{id}", s.admin(s.getAgent))
	mux.HandleFunc("PUT /v1/agents/{id}", s.admin(s.updateAgent))
	mux.HandleFunc("POST /v1/agents/{id}/brain/copy", s.admin(s.copyAgentBrain))
	mux.HandleFunc("GET /v1/agents/{id}/versions", s.admin(s.agentVersions))
	mux.HandleFunc("POST /v1/agents/{id}/versions/{version}/restore", s.admin(s.restoreAgent))
	mux.HandleFunc("PUT /v1/agents/{id}/avatar", s.admin(s.putAvatar))
	mux.HandleFunc("GET /v1/agents/{id}/avatar", s.admin(s.getAvatar))
	mux.HandleFunc("DELETE /v1/agents/{id}/avatar", s.admin(s.deleteAvatar))
}

func (s *Server) listAgents(w http.ResponseWriter, r *http.Request) {
	wsID, ok := s.requireWorkspace(w, r)
	if !ok {
		return
	}
	ctx := store.WithWorkspace(r.Context(), wsID)
	agents, err := s.store.ListAgents(ctx)
	if err != nil {
		storeError(w, err)
		return
	}
	convs, err := s.store.ListConversations(ctx, "", 1000)
	if err != nil {
		storeError(w, err)
		return
	}
	avatars, err := s.store.AvatarTimes(ctx)
	if err != nil {
		storeError(w, err)
		return
	}
	out := make([]AgentStatus, len(agents))
	index := map[string]int{}
	for i, a := range agents {
		out[i] = AgentStatus{Agent: a, Working: s.workingInChannel(a.ID)}
		if at, ok := avatars[a.ID]; ok {
			out[i].AvatarAt = &at
		}
		index[a.ID] = i
	}
	for _, c := range convs { // newest first
		i, ok := index[c.AgentID]
		if !ok {
			continue
		}
		st := &out[i]
		st.Conversations++
		if c.JobID != "" {
			st.Working = true
		}
		if st.LastAt == nil {
			at := c.UpdatedAt
			st.LastAt, st.Preview, st.LastRole = &at, c.Preview, c.LastRole
		}
	}
	s.fillAgentStatuses(ctx, out, convs)
	writeJSON(w, http.StatusOK, out)
}

func (s *Server) getAgent(w http.ResponseWriter, r *http.Request) {
	wsID, ok := s.requireWorkspace(w, r)
	if !ok {
		return
	}
	a, err := s.store.GetAgent(r.Context(), r.PathValue("id"))
	if err != nil {
		storeError(w, err)
		return
	}
	if a.WorkspaceID != wsID {
		storeError(w, store.ErrNotFound)
		return
	}
	writeJSON(w, http.StatusOK, a)
}

// slug turns a name into an agent id: "Dra. Ana Lú" -> "dra-ana-lu".
func slug(name string) string {
	repl := strings.NewReplacer("á", "a", "à", "a", "â", "a", "ã", "a", "é", "e", "ê", "e", "í", "i",
		"ó", "o", "ô", "o", "õ", "o", "ú", "u", "ü", "u", "ç", "c")
	var b strings.Builder
	dash := false
	for _, r := range repl.Replace(strings.ToLower(name)) {
		if r >= 'a' && r <= 'z' || r >= '0' && r <= '9' {
			b.WriteRune(r)
			dash = false
		} else if !dash && b.Len() > 0 {
			b.WriteByte('-')
			dash = true
		}
	}
	out := strings.TrimSuffix(b.String(), "-")
	if len(out) > 32 {
		out = strings.TrimSuffix(out[:32], "-")
	}
	return out
}

// toAgent validates a request into an agent, starting from base (the current
// version on edit).
func toAgent(req AgentRequest, base store.Agent) (store.Agent, error) {
	a := base
	a.Name = strings.TrimSpace(req.Name)
	a.Role = strings.TrimSpace(req.Role)
	a.Persona = strings.TrimSpace(req.Persona)
	// Engine, model and mode are gone from the agent: older apps still send
	// them and they are ignored.
	a.Engine, a.Model, a.Mode = "native", "", "read"
	a.Color = strings.ToLower(strings.TrimSpace(req.Color))
	if req.Position != nil {
		a.Position = *req.Position
	}
	if req.Responsibilities != nil {
		list, err := validResponsibilities(*req.Responsibilities)
		if err != nil {
			return a, err
		}
		a.Responsibilities = list
	}
	if req.Approvals != nil {
		switch v := strings.TrimSpace(*req.Approvals); v {
		case "", "risky":
			a.Approvals = ""
		case "always", "never":
			a.Approvals = v
		default:
			return a, errors.New("approvals must be always, risky or never")
		}
	}
	if req.Style != nil {
		st, err := validStyle(*req.Style)
		if err != nil {
			return a, err
		}
		a.Style = st
	}
	if req.HomeNodeID != nil {
		a.HomeNodeID = strings.TrimSpace(*req.HomeNodeID)
	}
	if req.HomeDir != nil {
		a.HomeDir = strings.TrimSpace(*req.HomeDir)
	}
	if err := applyAgentOrgFields(req, &a); err != nil {
		return a, err
	}
	switch {
	case a.Name == "" || utf8.RuneCountInString(a.Name) > 40:
		return a, errors.New("name must have 1 to 40 characters")
	case utf8.RuneCountInString(a.Role) > 60:
		return a, errors.New("role must have at most 60 characters")
	case utf8.RuneCountInString(a.Persona) > maxPersona || !utf8.ValidString(a.Persona):
		return a, fmt.Errorf("persona must have at most %d characters", maxPersona)
	case a.Color != "" && !colorPattern.MatchString(a.Color):
		return a, errors.New("color must look like #7fd1ff")
	case a.Position < 0 || a.Position > 1000:
		return a, errors.New("position must be between 0 and 1000")
	}
	return a, nil
}

// validStyle checks how an agent talks: a short voice and known choices
// (empty means the default).
func validStyle(st store.AgentStyle) (store.AgentStyle, error) {
	st.Voice = strings.TrimSpace(st.Voice)
	if utf8.RuneCountInString(st.Voice) > 400 {
		return st, errors.New("style.voice must have at most 400 characters")
	}
	check := func(field, v string, ok ...string) (string, error) {
		v = strings.TrimSpace(v)
		if v == "" || v == "normal" || v == "as_vezes" {
			return "", nil
		}
		if !contains(ok, v) {
			return "", fmt.Errorf("style.%s must be one of %s", field, strings.Join(ok, ", "))
		}
		return v, nil
	}
	var err error
	if st.Talk, err = check("talk", st.Talk, "pouco", "muito"); err != nil {
		return st, err
	}
	if st.Emoji, err = check("emoji", st.Emoji, "nunca", "bastante"); err != nil {
		return st, err
	}
	if st.Length, err = check("length", st.Length, "curta", "longa"); err != nil {
		return st, err
	}
	return st, nil
}

func contains(list []string, v string) bool {
	for _, x := range list {
		if x == v {
			return true
		}
	}
	return false
}

func (s *Server) createAgent(w http.ResponseWriter, r *http.Request) {
	wsID, ok := s.requireWorkspace(w, r)
	if !ok {
		return
	}
	ctx := store.WithWorkspace(r.Context(), wsID)
	var req AgentRequest
	if !readJSONMax(w, r, &req, maxPersona*4+4096) {
		return
	}
	a, err := toAgent(req, store.Agent{WorkspaceID: wsID})
	if err == nil {
		if req.Brain != nil {
			if accessErr := s.checkBrainProviderAccess(ctx, wsID, req.Brain.Provider); accessErr != nil {
				http.Error(w, publicLLMError(accessErr), http.StatusForbidden)
				return
			}
			a.Brain, err = s.validBrain(ctx, *req.Brain)
		} else {
			a.Brain = s.defaultBrain(ctx, wsID)
		}
	}
	if err == nil {
		err = s.validateAgentHome(ctx, wsID, &a)
	}
	if err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}
	a.ID = req.ID
	if a.ID == "" {
		a.ID = slug(a.Name)
	}
	if !agentIDPattern.MatchString(a.ID) {
		http.Error(w, "id must be 2 to 32 lowercase letters, digits or dashes", http.StatusBadRequest)
		return
	}
	if existing, err := s.store.GetAgent(ctx, a.ID); err == nil {
		if req.ID != "" || existing.WorkspaceID == wsID {
			http.Error(w, "an agent with this id already exists", http.StatusConflict)
			return
		}
		base := a.ID
		if len(base) > 27 {
			base = strings.TrimSuffix(base[:27], "-")
		}
		for i := 2; ; i++ {
			cand := fmt.Sprintf("%s-%d", base, i)
			if _, gerr := s.store.GetAgent(ctx, cand); errors.Is(gerr, store.ErrNotFound) {
				a.ID = cand
				break
			} else if gerr != nil {
				storeError(w, gerr)
				return
			}
		}
	} else if !errors.Is(err, store.ErrNotFound) {
		storeError(w, err)
		return
	}
	if req.Position == nil {
		agents, err := s.store.ListAgents(ctx)
		if err != nil {
			storeError(w, err)
			return
		}
		for _, other := range agents {
			if other.Position >= a.Position {
				a.Position = other.Position + 1
			}
		}
	}
	if req.ProjectID != "" {
		if p, err := s.store.GetProject(ctx, req.ProjectID); err != nil || p.WorkspaceID != wsID {
			http.Error(w, "unknown project", http.StatusBadRequest)
			return
		}
	}
	if code, err := s.validateAgentOrg(ctx, wsID, &a); err != nil {
		http.Error(w, err.Error(), code)
		return
	}
	actor := actorFrom(r)
	saved, err := s.store.SaveAgent(ctx, a, actor, s.now())
	if err != nil {
		storeError(w, err)
		return
	}
	if req.ProjectID != "" {
		if saved, err = s.store.MoveAgent(ctx, saved.ID, req.ProjectID, actor, s.now()); err != nil {
			storeError(w, err)
			return
		}
	}
	writeJSON(w, http.StatusCreated, saved)
}

func readAgentUpdate(w http.ResponseWriter, r *http.Request, max int64) (AgentRequest, map[string]json.RawMessage, bool) {
	r.Body = http.MaxBytesReader(w, r.Body, max)
	defer r.Body.Close()
	raw, err := io.ReadAll(r.Body)
	if err != nil {
		http.Error(w, "invalid JSON payload", http.StatusBadRequest)
		return AgentRequest{}, nil, false
	}
	var req AgentRequest
	dec := json.NewDecoder(bytes.NewReader(raw))
	dec.DisallowUnknownFields()
	if err := dec.Decode(&req); err != nil {
		http.Error(w, "invalid JSON payload", http.StatusBadRequest)
		return AgentRequest{}, nil, false
	}
	var extra any
	if err := dec.Decode(&extra); err != io.EOF {
		http.Error(w, "unexpected content after JSON", http.StatusBadRequest)
		return AgentRequest{}, nil, false
	}
	var fields map[string]json.RawMessage
	_ = json.Unmarshal(raw, &fields)
	return req, fields, true
}

func (s *Server) updateAgent(w http.ResponseWriter, r *http.Request) {
	wsID, ok := s.requireWorkspace(w, r)
	if !ok {
		return
	}
	ctx := store.WithWorkspace(r.Context(), wsID)
	current, err := s.store.GetAgent(ctx, r.PathValue("id"))
	if err != nil {
		storeError(w, err)
		return
	}
	if current.WorkspaceID != wsID {
		storeError(w, store.ErrNotFound)
		return
	}
	req, fields, ok := readAgentUpdate(w, r, maxPersona*4+4096)
	if !ok {
		return
	}
	if _, has := fields["name"]; !has {
		req.Name = current.Name
	}
	if _, has := fields["role"]; !has {
		req.Role = current.Role
	}
	if _, has := fields["persona"]; !has {
		req.Persona = current.Persona
	}
	if _, has := fields["color"]; !has {
		req.Color = current.Color
	}
	a, err := toAgent(req, current)
	if err == nil && req.Brain != nil {
		if accessErr := s.checkBrainProviderAccess(ctx, wsID, req.Brain.Provider); accessErr != nil {
			http.Error(w, publicLLMError(accessErr), http.StatusForbidden)
			return
		}
		a.Brain, err = s.validBrain(ctx, *req.Brain)
	}
	if err == nil {
		err = s.validateAgentHome(ctx, wsID, &a)
	}
	if err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}
	if code, err := s.validateAgentOrg(ctx, wsID, &a); err != nil {
		http.Error(w, err.Error(), code)
		return
	}
	saved, err := s.store.SaveAgent(ctx, a, actorFrom(r), s.now())
	if err != nil {
		storeError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, saved)
}

func (s *Server) agentVersions(w http.ResponseWriter, r *http.Request) {
	wsID, ok := s.requireWorkspace(w, r)
	if !ok {
		return
	}
	a, err := s.store.GetAgent(r.Context(), r.PathValue("id"))
	if err != nil {
		storeError(w, err)
		return
	}
	if a.WorkspaceID != wsID {
		storeError(w, store.ErrNotFound)
		return
	}
	versions, err := s.store.AgentVersions(r.Context(), a.ID)
	if err != nil {
		storeError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, versions)
}

// restoreAgent saves an old version again, as the newest one.
func (s *Server) restoreAgent(w http.ResponseWriter, r *http.Request) {
	wsID, ok := s.requireWorkspace(w, r)
	if !ok {
		return
	}
	cur, err := s.store.GetAgent(r.Context(), r.PathValue("id"))
	if err != nil {
		storeError(w, err)
		return
	}
	if cur.WorkspaceID != wsID {
		storeError(w, store.ErrNotFound)
		return
	}
	want, err := strconv.Atoi(r.PathValue("version"))
	if err != nil {
		http.Error(w, "invalid version", http.StatusBadRequest)
		return
	}
	versions, err := s.store.AgentVersions(r.Context(), cur.ID)
	if err != nil {
		storeError(w, err)
		return
	}
	for _, v := range versions {
		if v.Version == want {
			v.Agent.WorkspaceID = cur.WorkspaceID
			saved, err := s.store.SaveAgent(r.Context(), v.Agent, actorFrom(r), s.now())
			if err != nil {
				storeError(w, err)
				return
			}
			writeJSON(w, http.StatusOK, saved)
			return
		}
	}
	http.Error(w, "version not found", http.StatusNotFound)
}
