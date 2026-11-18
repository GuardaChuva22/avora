package mcp

import (
	"bytes"
	"encoding/json"
	"errors"
	"io/fs"
	"regexp"
	"strings"
)

// PluginJob is the request the control plane sends to a sandboxed runtime.
// Entry is relative to the plugin directory or the root of Repository.
type PluginJob struct {
	Mode       string          `json:"mode"`
	Repository string          `json:"repository,omitempty"`
	Revision   string          `json:"revision,omitempty"`
	Runtime    string          `json:"runtime"`
	Entry      string          `json:"entry"`
	Name       string          `json:"name,omitempty"`
	Arguments  json.RawMessage `json:"arguments,omitempty"`
}

type PluginResult struct {
	Server ServerInfo `json:"server,omitempty"`
	Tools  []Tool     `json:"tools,omitempty"`
	Result CallResult `json:"result,omitempty"`
}

var (
	githubRepo = regexp.MustCompile(`(?i)^https://github\.com/[a-z0-9][a-z0-9-]*/[a-z0-9_][a-z0-9_.-]*$`)
	gitSHA     = regexp.MustCompile(`(?i)^[a-f0-9]{40}$`)
	pluginDir  = regexp.MustCompile(`^plugins/[a-zA-Z0-9][a-zA-Z0-9_.-]{0,127}$`)
)

// ValidPluginDir limits stdio jobs to a single plugin folder in the workspace.
func ValidPluginDir(dir string) bool { return pluginDir.MatchString(dir) }

func (j PluginJob) Validate() error {
	if j.Runtime != "node" && j.Runtime != "python3" {
		return errors.New("o MCP precisa usar node ou python3")
	}
	if len(j.Entry) > 200 || !fs.ValidPath(j.Entry) || j.Entry == "." || strings.ContainsAny(j.Entry, "\\:\x00\r\n") {
		return errors.New("informe o caminho relativo do arquivo que inicia o MCP")
	}
	if j.Repository != "" {
		if len(j.Repository) > 300 || !githubRepo.MatchString(j.Repository) {
			return errors.New("informe a URL HTTPS de um repositório público do GitHub")
		}
		if j.Revision != "" && !gitSHA.MatchString(j.Revision) {
			return errors.New("informe o SHA completo do commit do GitHub")
		}
		if j.Runtime != "node" {
			return errors.New("a instalação pelo GitHub requer um projeto Node.js com package-lock.json")
		}
	} else if j.Revision != "" {
		return errors.New("a revisão precisa de um repositório do GitHub")
	}
	switch j.Mode {
	case "install":
		if j.Repository == "" {
			return errors.New("informe o repositório que deseja instalar")
		}
	case "list":
	case "call":
		if j.Name == "" || len(j.Name) > 256 {
			return errors.New("nome inválido da ferramenta MCP")
		}
		if a := bytes.TrimSpace(j.Arguments); len(a) > 0 && (a[0] != '{' || !json.Valid(a)) {
			return errors.New("os argumentos da ferramenta MCP precisam ser um objeto JSON")
		}
	default:
		return errors.New("operação MCP desconhecida")
	}
	return nil
}
