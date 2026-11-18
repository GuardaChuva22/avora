package control

import (
	"context"
	"errors"
	"fmt"
	"io"
	"net/http"
	"os"
	"path"
	"path/filepath"
	"regexp"
	"strings"
	"unicode/utf8"

	"github.com/GuardaChuva22/avora/internal/scheduler"
	"github.com/GuardaChuva22/avora/internal/store"
)

// Projects (D25) group agents around one piece of the owner's life. Each
// has instructions (the AGENTS.md of its main folder) and folders on a
// node: an absolute path on the PC, or a path inside the VPS workspace.
// An agent in a project sees only that project's agents and the
// universal ones (no project); a universal agent sees everyone.

const (
	maxInstructions   = 20000
	maxProjectFolders = 8
	maxProjects       = 50
	// AgentsFile is the instructions file of a project's main folder, the
	// one Codex (and other CLIs) read on their own.
	AgentsFile = "AGENTS.md"
)

// windowsPath is an absolute Windows folder, like C:\Trabalho\site.
var windowsPath = regexp.MustCompile(`^[A-Za-z]:\\[^<>"|?*\x00-\x1f]*$`)

type ProjectRequest struct {
	Name         string                `json:"name"`
	Color        string                `json:"color"`
	Instructions *string               `json:"instructions"`
	Folders      []store.ProjectFolder `json:"folders"`
	Position     *int                  `json:"position"`
}

type MoveRequest struct {
	ProjectID string `json:"project_id"`
}

func (s *Server) registerProjects(mux *http.ServeMux) {
	mux.HandleFunc("GET /v1/projects", s.admin(s.listProjects))
	mux.HandleFunc("POST /v1/projects", s.admin(s.createProject))
	mux.HandleFunc("GET /v1/projects/{id}", s.admin(s.getProject))
	mux.HandleFunc("PUT /v1/projects/{id}", s.admin(s.updateProject))
	mux.HandleFunc("DELETE /v1/projects/{id}", s.admin(s.deleteProject))
	mux.HandleFunc("PUT /v1/agents/{id}/project", s.admin(s.moveAgent))
}

// cleanFolder checks a folder against the node it is on.
func cleanFolder(f store.ProjectFolder, nodes []store.Node) (store.ProjectFolder, error) {
	f.Label = strings.TrimSpace(f.Label)
	f.Path = strings.TrimSpace(f.Path)
	if utf8.RuneCountInString(f.Label) > 40 {
		return f, errors.New("o nome de uma pasta tem até 40 caracteres")
	}
	var node *store.Node
	for i := range nodes {
		if nodes[i].ID == f.Node && nodes[i].RevokedAt == nil {
			node = &nodes[i]
		}
	}
	if node == nil {
		return f, fmt.Errorf("nó %q não existe", f.Node)
	}
	switch node.Kind {
	case scheduler.Windows:
		p := strings.TrimRight(strings.ReplaceAll(f.Path, "/", `\`), `\`)
		if len(p) == 2 {
			p += `\`
		}
		if len(p) > 260 || !windowsPath.MatchString(p) || strings.Contains(p, `\..`) {
			return f, fmt.Errorf("pasta inválida no PC: %q (use o caminho completo, como C:\\Projetos\\site)", f.Path)
		}
		f.Path = p
	case scheduler.VPS:
		clean, ok := cleanWorkdir(f.Path)
		if !ok || clean == "" {
			return f, fmt.Errorf("pasta inválida na VPS: %q (um caminho dentro do workspace, como projects/site)", f.Path)
		}
		f.Path = clean
	default:
		return f, fmt.Errorf("o nó %s não guarda pastas de projeto", node.Name)
	}
	return f, nil
}

func (s *Server) toProject(ctx context.Context, req ProjectRequest, base store.Project) (store.Project, error) {
	p := base
	p.Name = strings.TrimSpace(req.Name)
	p.Color = strings.ToLower(strings.TrimSpace(req.Color))
	if req.Instructions != nil {
		p.Instructions = strings.TrimSpace(*req.Instructions)
	}
	if req.Position != nil {
		p.Position = *req.Position
	}
	switch {
	case p.Name == "" || utf8.RuneCountInString(p.Name) > 40:
		return p, errors.New("o nome do projeto tem de 1 a 40 caracteres")
	case p.Color != "" && !colorPattern.MatchString(p.Color):
		return p, errors.New("cor deve ser como #7fd1ff")
	case utf8.RuneCountInString(p.Instructions) > maxInstructions || !utf8.ValidString(p.Instructions):
		return p, fmt.Errorf("as instruções têm até %d caracteres", maxInstructions)
	case len(req.Folders) > maxProjectFolders:
		return p, fmt.Errorf("até %d pastas por projeto", maxProjectFolders)
	case p.Position < -1000 || p.Position > 1000:
		return p, errors.New("posição inválida")
	}
	if req.Folders != nil {
		nodes, err := s.store.ListNodes(ctx)
		if err != nil {
			return p, err
		}
		p.Folders = []store.ProjectFolder{}
		seen := map[string]bool{}
		for _, f := range req.Folders {
			f, err := cleanFolder(f, nodes)
			if err != nil {
				return p, err
			}
			key := f.Node + "|" + strings.ToLower(f.Path)
			if seen[key] {
				continue
			}
			seen[key] = true
			p.Folders = append(p.Folders, f)
		}
	}
	return p, nil
}

// syncInstructions keeps AGENTS.md in a VPS main folder: the file the
// owner (or Claude Code) edits wins over the stored copy, and a save from
// the app writes it. PC folders are kept by the desktop app.
func (s *Server) syncInstructions(ctx context.Context, p *store.Project, write bool) {
	dir, ok := s.vpsFolder(ctx, *p)
	if !ok {
		return
	}
	if err := os.MkdirAll(dir, 0o700); err != nil {
		return
	}
	root, err := os.OpenRoot(dir)
	if err != nil {
		return
	}
	defer root.Close()
	if write {
		if p.Instructions != "" {
			if f, err := root.OpenFile(AgentsFile, os.O_WRONLY|os.O_CREATE|os.O_TRUNC, 0o600); err == nil {
				_, _ = f.WriteString(p.Instructions + "\n")
				_ = f.Close()
			}
		}
		return
	}
	f, err := root.Open(AgentsFile)
	if err != nil {
		return
	}
	defer f.Close()
	data, err := io.ReadAll(io.LimitReader(f, 4*maxInstructions))
	if err == nil && utf8.Valid(data) {
		p.Instructions = truncate(strings.TrimSpace(string(data)), 4*maxInstructions)
	}
}

// vpsFolder is the local path of a project's main folder when it is on
// the VPS (where the server keeps the agents' workspace).
func (s *Server) vpsFolder(ctx context.Context, p store.Project) (string, bool) {
	if s.workspace == "" || len(p.Folders) == 0 {
		return "", false
	}
	n, err := s.nodeByID(ctx, p.Folders[0].Node)
	if err != nil || n.Kind != scheduler.VPS {
		return "", false
	}
	clean, ok := cleanWorkdir(p.Folders[0].Path)
	if !ok || clean == "" {
		return "", false
	}
	return filepath.Join(s.workspace, filepath.FromSlash(clean)), true
}

func (s *Server) listProjects(w http.ResponseWriter, r *http.Request) {
	wsID, ok := s.requireWorkspace(w, r)
	if !ok {
		return
	}
	ctx := store.WithWorkspace(r.Context(), wsID)
	list, err := s.store.ListProjects(ctx)
	if err != nil {
		storeError(w, err)
		return
	}
	for i := range list {
		s.syncInstructions(ctx, &list[i], false)
	}
	writeJSON(w, http.StatusOK, list)
}

func (s *Server) getProject(w http.ResponseWriter, r *http.Request) {
	wsID, ok := s.requireWorkspace(w, r)
	if !ok {
		return
	}
	p, err := s.store.GetProject(r.Context(), r.PathValue("id"))
	if err != nil {
		storeError(w, err)
		return
	}
	if p.WorkspaceID != wsID {
		storeError(w, store.ErrNotFound)
		return
	}
	s.syncInstructions(r.Context(), &p, false)
	writeJSON(w, http.StatusOK, p)
}

func (s *Server) createProject(w http.ResponseWriter, r *http.Request) {
	wsID, ok := s.requireWorkspace(w, r)
	if !ok {
		return
	}
	ctx := store.WithWorkspace(r.Context(), wsID)
	var req ProjectRequest
	if !readJSONMax(w, r, &req, 4*maxInstructions+8192) {
		return
	}
	p, err := s.toProject(ctx, req, store.Project{WorkspaceID: wsID})
	if err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}
	list, err := s.store.ListProjects(ctx)
	if err != nil {
		storeError(w, err)
		return
	}
	if len(list) >= maxProjects {
		http.Error(w, "limite de projetos atingido", http.StatusConflict)
		return
	}
	base := slug(p.Name)
	if base == "" || len(base) < 2 {
		base = "projeto"
	}
	p.ID = base
	for i := 2; ; i++ {
		if _, err := s.store.GetProject(ctx, p.ID); errors.Is(err, store.ErrNotFound) {
			break
		} else if err != nil {
			storeError(w, err)
			return
		}
		p.ID = fmt.Sprintf("%s-%d", base, i)
	}
	if req.Position == nil {
		for _, o := range list {
			if o.Position >= p.Position {
				p.Position = o.Position + 1
			}
		}
	}
	saved, err := s.store.SaveProject(ctx, p, actorFrom(r), s.now())
	if err != nil {
		storeError(w, err)
		return
	}
	s.syncInstructions(ctx, &saved, true)
	writeJSON(w, http.StatusCreated, saved)
}

func (s *Server) updateProject(w http.ResponseWriter, r *http.Request) {
	wsID, ok := s.requireWorkspace(w, r)
	if !ok {
		return
	}
	ctx := store.WithWorkspace(r.Context(), wsID)
	current, err := s.store.GetProject(ctx, r.PathValue("id"))
	if err != nil {
		storeError(w, err)
		return
	}
	if current.WorkspaceID != wsID {
		storeError(w, store.ErrNotFound)
		return
	}
	var req ProjectRequest
	if !readJSONMax(w, r, &req, 4*maxInstructions+8192) {
		return
	}
	p, err := s.toProject(ctx, req, current)
	if err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}
	saved, err := s.store.SaveProject(ctx, p, actorFrom(r), s.now())
	if err != nil {
		storeError(w, err)
		return
	}
	if req.Instructions != nil {
		s.syncInstructions(ctx, &saved, true)
	}
	writeJSON(w, http.StatusOK, saved)
}

// deleteProject forgets the project; its agents become universal. The
// folders stay where they are.
func (s *Server) deleteProject(w http.ResponseWriter, r *http.Request) {
	wsID, ok := s.requireWorkspace(w, r)
	if !ok {
		return
	}
	cur, err := s.store.GetProject(r.Context(), r.PathValue("id"))
	if err != nil {
		storeError(w, err)
		return
	}
	if cur.WorkspaceID != wsID {
		storeError(w, store.ErrNotFound)
		return
	}
	if err := s.store.DeleteProject(r.Context(), cur.ID, actorFrom(r), s.now()); err != nil {
		storeError(w, err)
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

func (s *Server) moveAgent(w http.ResponseWriter, r *http.Request) {
	wsID, ok := s.requireWorkspace(w, r)
	if !ok {
		return
	}
	var req MoveRequest
	if !readJSONMax(w, r, &req, 1024) {
		return
	}
	agent, err := s.store.GetAgent(r.Context(), r.PathValue("id"))
	if err != nil {
		storeError(w, err)
		return
	}
	if agent.WorkspaceID != wsID {
		storeError(w, store.ErrNotFound)
		return
	}
	projectID := strings.TrimSpace(req.ProjectID)
	if projectID != "" {
		proj, err := s.store.GetProject(r.Context(), projectID)
		if err != nil {
			storeError(w, err)
			return
		}
		if proj.WorkspaceID != wsID {
			storeError(w, store.ErrNotFound)
			return
		}
	}
	a, err := s.store.MoveAgent(r.Context(), agent.ID, projectID, actorFrom(r), s.now())
	if err != nil {
		storeError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, a)
}

// ── Who sees whom ───────────────────────────────────────────────

// sees reports whether self may talk to other: a universal agent sees
// everyone; a project agent sees its project and the universal agents.
func sees(self, other store.Agent) bool {
	return self.ProjectID == "" || other.ProjectID == "" || other.ProjectID == self.ProjectID
}

// visibleTeam is the part of the team self may talk to.
func visibleTeam(self store.Agent, team []store.Agent) []store.Agent {
	out := make([]store.Agent, 0, len(team))
	for _, a := range team {
		if a.ID == self.ID || sees(self, a) {
			out = append(out, a)
		}
	}
	return out
}

func projectByID(list []store.Project, id string) (store.Project, bool) {
	for _, p := range list {
		if p.ID == id {
			return p, true
		}
	}
	return store.Project{}, false
}

// folderLine describes a folder for the model.
func (s *Server) folderLine(ctx context.Context, f store.ProjectFolder) string {
	where := f.Node
	if n, err := s.nodeByID(ctx, f.Node); err == nil {
		where = n.Name
		if n.Kind == scheduler.VPS {
			where = "VPS"
		}
	}
	line := f.Path + " (" + where + ")"
	if f.Label != "" {
		line = f.Label + ": " + line
	}
	return line
}

// projectBlock tells the agent about its project, or, for a universal
// agent, which projects exist and who works in each.
func (s *Server) projectBlock(ctx context.Context, a store.Agent) string {
	projects, err := s.store.ListProjects(ctx)
	if err != nil || len(projects) == 0 {
		return ""
	}
	if a.ProjectID == "" {
		team, _ := s.store.ListAgents(ctx)
		var b strings.Builder
		b.WriteString("\n\nProjetos do dono (você é universal: pode consultar os funcionários de qualquer projeto e delegar ao claude_code/codex na pasta de um projeto com o argumento projeto):")
		for _, p := range projects {
			names := []string{}
			for _, m := range team {
				if m.ProjectID == p.ID {
					names = append(names, m.Name)
				}
			}
			who := "ninguém ainda"
			if len(names) > 0 {
				who = strings.Join(names, ", ")
			}
			fmt.Fprintf(&b, "\n- %s: %s", p.Name, who)
		}
		return b.String()
	}
	p, ok := projectByID(projects, a.ProjectID)
	if !ok {
		return ""
	}
	s.syncInstructions(ctx, &p, false)
	var b strings.Builder
	fmt.Fprintf(&b, "\n\n## Projeto: %s\nVocê trabalha no projeto %s. Os funcionários de outros projetos não fazem parte do seu trabalho; você só fala com os colegas deste projeto e com os universais.", p.Name, p.Name)
	if len(p.Folders) > 0 {
		b.WriteString("\nPastas do projeto (claude_code e codex trabalham na principal; use o argumento pasta para outra):")
		for i, f := range p.Folders {
			line := s.folderLine(ctx, f)
			if i == 0 {
				line = "principal: " + line
			}
			b.WriteString("\n- " + line)
		}
	}
	if p.Instructions != "" {
		b.WriteString("\n\nInstruções do projeto (" + AgentsFile + "), siga-as:\n" + truncate(p.Instructions, maxInstructions))
	}
	return b.String()
}

// projectFolder picks where a delegated run works: a project's folder
// (by label or path) or nothing (the agent's own folder).
func (s *Server) projectFolder(ctx context.Context, self store.Agent, project, folder string) (*store.Project, *store.ProjectFolder, error) {
	id := self.ProjectID
	if project = strings.TrimSpace(project); project != "" {
		projects, err := s.store.ListProjects(ctx)
		if err != nil {
			return nil, nil, err
		}
		key := slug(project)
		found := ""
		names := []string{}
		for _, p := range projects {
			if p.ID == key || slug(p.Name) == key {
				found = p.ID
			}
			names = append(names, p.Name)
		}
		if found == "" {
			return nil, nil, fmt.Errorf("não há projeto %q (os projetos: %s)", project, strings.Join(names, ", "))
		}
		if self.ProjectID != "" && found != self.ProjectID {
			return nil, nil, errors.New("você só trabalha no seu projeto")
		}
		id = found
	}
	if id == "" {
		return nil, nil, nil
	}
	p, err := s.store.GetProject(ctx, id)
	if err != nil {
		return nil, nil, err
	}
	if len(p.Folders) == 0 {
		if strings.TrimSpace(folder) != "" {
			return nil, nil, errors.New("o projeto não tem pastas")
		}
		return &p, nil, nil
	}
	f := p.Folders[0]
	if want := strings.TrimSpace(folder); want != "" {
		found := false
		for _, c := range p.Folders {
			if strings.EqualFold(c.Label, want) || strings.EqualFold(c.Path, want) || strings.EqualFold(path.Base(strings.ReplaceAll(c.Path, `\`, "/")), want) {
				f, found = c, true
				break
			}
		}
		if !found {
			return nil, nil, fmt.Errorf("o projeto não tem a pasta %q", want)
		}
	}
	return &p, &f, nil
}
