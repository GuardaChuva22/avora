package control

import (
	"context"
	"errors"
	"fmt"
	"path"
	"strings"
	"unicode/utf8"

	"github.com/GuardaChuva22/avora/internal/store"
)

func offlineNodeMessage(nodeName string) string {
	return fmt.Sprintf("O computador %s está desligado; a tarefa começa quando ele voltar", nodeName)
}

func validSpecHomeDir(raw string) (string, error) {
	dir := strings.TrimSpace(raw)
	if dir == "" {
		return "", nil
	}
	if len(dir) > 260 || !utf8.ValidString(dir) || strings.ContainsRune(dir, 0) {
		return "", errors.New("home_dir inválido")
	}
	slash := strings.ReplaceAll(dir, `\`, "/")
	for _, seg := range strings.Split(slash, "/") {
		if seg == ".." {
			return "", errors.New("home_dir não pode conter ..")
		}
	}
	// Windows absolute path (e.g. C:\Agentes\atlas).
	win := strings.TrimRight(strings.ReplaceAll(dir, "/", `\`), `\`)
	if len(win) == 2 && win[1] == ':' {
		win += `\`
	}
	if len(win) >= 3 && win[1] == ':' && win[2] == '\\' {
		if !windowsPath.MatchString(win) {
			return "", errors.New("home_dir inválido para Windows")
		}
		return win, nil
	}
	// Unix absolute path (e.g. /var/lib/avora/agents/atlas).
	if strings.HasPrefix(dir, "/") {
		clean := path.Clean(slash)
		if clean == "/" || clean == "." {
			return "", errors.New("home_dir não pode ser a raiz do sistema")
		}
		return clean, nil
	}
	// Relative path inside the computer's workspace.
	clean, ok := cleanWorkdir(dir)
	if !ok || clean == "" {
		return "", errors.New("home_dir relativo inválido")
	}
	return clean, nil
}

func (s *Server) validateAgentHome(_ context.Context, wsID string, a *store.Agent) error {
	a.HomeNodeID = strings.TrimSpace(a.HomeNodeID)
	a.HomeDir = strings.TrimSpace(a.HomeDir)
	if a.HomeNodeID != "" {
		n, err := s.nodeByID(context.Background(), a.HomeNodeID)
		if err != nil || n.RevokedAt != nil || n.WorkspaceID != wsID {
			return errors.New("computador não encontrado neste espaço")
		}
	}
	if a.HomeDir != "" {
		clean, err := validSpecHomeDir(a.HomeDir)
		if err != nil {
			return err
		}
		a.HomeDir = clean
	}
	return nil
}

func (s *Server) resolveHomeNode(_ context.Context, a store.Agent) (store.Node, bool, error) {
	id := strings.TrimSpace(a.HomeNodeID)
	if id == "" {
		return store.Node{}, false, nil
	}
	n, err := s.nodeByID(context.Background(), id)
	if err != nil || n.RevokedAt != nil {
		return store.Node{}, false, errors.New("o computador configurado para este agente não está disponível")
	}
	ws := a.WorkspaceID
	if ws == "" {
		ws = store.DefaultWorkspaceID
	}
	if n.WorkspaceID != "" && n.WorkspaceID != ws {
		return store.Node{}, false, errors.New("o computador configurado pertence a outro espaço")
	}
	return n, true, nil
}

func applyAgentIdentity(spec *store.AgentSpec, a store.Agent) {
	if spec == nil {
		return
	}
	if spec.Owner == "" {
		spec.Owner = a.ID
	}
	if spec.AgentName == "" {
		spec.AgentName = a.Name
	}
	if spec.AgentEmail == "" {
		s := slug(firstNonEmpty(a.ID, a.Name))
		if s == "" {
			s = "agent"
		}
		spec.AgentEmail = s + "@avora.local"
	}
}

func applyAgentHome(spec *store.AgentSpec, a store.Agent) {
	if spec == nil {
		return
	}
	applyAgentIdentity(spec, a)
	if spec.Folder != "" {
		return
	}
	agentSlug := slug(firstNonEmpty(a.ID, a.Name))
	if agentSlug == "" {
		agentSlug = "agent"
	}
	if strings.TrimSpace(a.HomeDir) != "" {
		spec.HomeDir = strings.TrimSpace(a.HomeDir)
		spec.Workdir = ""
	} else if spec.Workdir == "" {
		spec.Workdir = "agents/" + agentSlug
	}
}
