package control

import (
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path"
	"path/filepath"
	"sort"
	"strings"
	"unicode/utf8"
)

// Each agent's own folder on the VPS: <workspace>/agents/<id>, the same
// folder Claude Code and Codex work in when the VPS runs them, and the one
// mounted at /work for executar. The agent reads and writes it directly,
// without a job. Every access goes through os.Root, so no path (or symlink)
// leaves the folder.

const (
	// AgentQuotaBytes caps one agent's folder.
	AgentQuotaBytes = 200 << 20
	// MaxWriteBytes caps one escrever_arquivo call.
	MaxWriteBytes = 1 << 20
	// maxReadBytes is the most of a file ler_arquivo loads.
	maxReadBytes = 2 << 20
	// partRunes is how much text one tool result carries; longer texts are
	// read in parts.
	partRunes = 6500
	maxListed = 200
)

// SetWorkspace enables the agents' folders under dir (the VPS runtime's
// AVORA_WORKSPACE). Empty disables the file tools.
func (s *Server) SetWorkspace(dir string) error {
	if dir == "" {
		s.workspace = ""
		return nil
	}
	abs, err := filepath.Abs(dir)
	if err != nil {
		return err
	}
	if err := os.MkdirAll(abs, 0o700); err != nil {
		return err
	}
	s.workspace = abs
	return nil
}

// agentRoot opens an agent's folder, creating it on first use.
func (s *Server) agentRoot(agentID string) (*os.Root, error) {
	if s.workspace == "" {
		return nil, errors.New("as pastas dos agentes não estão configuradas neste servidor")
	}
	if _, ok := cleanWorkdir("agents/" + agentID); !ok {
		return nil, errors.New("agente inválido")
	}
	dir := filepath.Join(s.workspace, "agents", agentID)
	if err := os.MkdirAll(dir, 0o700); err != nil {
		return nil, err
	}
	return os.OpenRoot(dir)
}

// relPath turns what the model wrote ("/notas/a.md", "./a.md") into a path
// inside the folder.
func relPath(p string, allowRoot bool) (string, error) {
	p = strings.TrimSpace(strings.ReplaceAll(p, "\\", "/"))
	p = strings.TrimPrefix(p, "/work")
	p = strings.TrimLeft(p, "/")
	for _, part := range strings.Split(p, "/") {
		if part == ".." {
			return "", errors.New("o caminho sai da sua pasta")
		}
	}
	clean := path.Clean("/" + p)[1:]
	if clean == "" {
		if allowRoot {
			return ".", nil
		}
		return "", errors.New("diga o caminho do arquivo")
	}
	if len(clean) > 300 || strings.ContainsAny(clean, "\x00:") {
		return "", errors.New("caminho inválido")
	}
	for _, part := range strings.Split(clean, "/") {
		if part == ".." {
			return "", errors.New("o caminho sai da sua pasta")
		}
	}
	return clean, nil
}

func folderSize(root *os.Root) int64 {
	var total int64
	_ = fs.WalkDir(root.FS(), ".", func(_ string, d fs.DirEntry, err error) error {
		if err == nil && !d.IsDir() {
			if info, err := d.Info(); err == nil {
				total += info.Size()
			}
		}
		return nil
	})
	return total
}

func sizeText(n int64) string {
	switch {
	case n >= 1<<20:
		return fmt.Sprintf("%.1f MB", float64(n)/(1<<20))
	case n >= 1<<10:
		return fmt.Sprintf("%.1f KB", float64(n)/(1<<10))
	}
	return fmt.Sprintf("%d B", n)
}

// part returns part n (1-based) of text, cut at partRunes, and how many
// parts there are.
func part(text string, n int) (string, int) {
	runes := []rune(text)
	total := (len(runes) + partRunes - 1) / partRunes
	if total == 0 {
		return "", 1
	}
	if n < 1 {
		n = 1
	}
	if n > total {
		return "", total
	}
	end := min(n*partRunes, len(runes))
	return string(runes[(n-1)*partRunes : end]), total
}

func (s *Server) listFiles(agentID, dir string) (string, error) {
	root, err := s.agentRoot(agentID)
	if err != nil {
		return "", err
	}
	defer root.Close()
	rel, err := relPath(dir, true)
	if err != nil {
		return "", err
	}
	lines := []string{}
	var total int64
	err = fs.WalkDir(root.FS(), rel, func(p string, d fs.DirEntry, err error) error {
		if err != nil {
			if p == rel {
				return err
			}
			return nil
		}
		if p == rel {
			return nil
		}
		if strings.HasPrefix(d.Name(), ".") && d.IsDir() {
			return fs.SkipDir // CLI state (.claude, .git...)
		}
		if len(lines) >= maxListed {
			return fs.SkipAll
		}
		if d.IsDir() {
			lines = append(lines, p+"/")
			return nil
		}
		info, err := d.Info()
		if err != nil {
			return nil
		}
		total += info.Size()
		lines = append(lines, fmt.Sprintf("%s  (%s, %s)", p, sizeText(info.Size()), info.ModTime().In(s.zone()).Format("02/01 15:04")))
		return nil
	})
	if errors.Is(err, fs.ErrNotExist) {
		return "", errors.New("essa pasta não existe")
	}
	if err != nil {
		return "", err
	}
	if len(lines) == 0 {
		return "A pasta está vazia.", nil
	}
	sort.Strings(lines)
	out := strings.Join(lines, "\n")
	if len(lines) >= maxListed {
		out += fmt.Sprintf("\n… (mostrando os primeiros %d)", maxListed)
	}
	return out + fmt.Sprintf("\nTotal: %s de %s.", sizeText(folderSize(root)), sizeText(AgentQuotaBytes)), nil
}

func (s *Server) readFile(agentID, file string, n int) (string, error) {
	root, err := s.agentRoot(agentID)
	if err != nil {
		return "", err
	}
	defer root.Close()
	rel, err := relPath(file, false)
	if err != nil {
		return "", err
	}
	info, err := root.Stat(rel)
	if errors.Is(err, fs.ErrNotExist) {
		return "", errors.New("arquivo não encontrado; use listar_arquivos")
	}
	if err != nil {
		return "", err
	}
	if info.IsDir() {
		return "", errors.New("é uma pasta; use listar_arquivos")
	}
	f, err := root.Open(rel)
	if err != nil {
		return "", err
	}
	defer f.Close()
	buf := make([]byte, min(info.Size(), maxReadBytes))
	read, _ := f.Read(buf)
	buf = buf[:read]
	if !utf8.Valid(buf) || strings.ContainsRune(string(buf), 0) {
		return fmt.Sprintf("%s é um arquivo binário (%s); não dá para ler como texto.", rel, sizeText(info.Size())), nil
	}
	text, total := part(string(buf), n)
	if text == "" && total > 0 && info.Size() > 0 {
		return "", fmt.Errorf("o arquivo tem só %d partes", total)
	}
	head := fmt.Sprintf("%s (%s)", rel, sizeText(info.Size()))
	if total > 1 {
		head += fmt.Sprintf(", parte %d de %d", max(n, 1), total)
	}
	if info.Size() > maxReadBytes {
		head += fmt.Sprintf("; só os primeiros %s foram carregados", sizeText(maxReadBytes))
	}
	return head + ":\n" + text, nil
}

func (s *Server) writeFile(agentID, file, content string, appendTo bool) (string, error) {
	if len(content) > MaxWriteBytes {
		return "", fmt.Errorf("conteúdo grande demais (máx. %s por escrita)", sizeText(MaxWriteBytes))
	}
	root, err := s.agentRoot(agentID)
	if err != nil {
		return "", err
	}
	defer root.Close()
	rel, err := relPath(file, false)
	if err != nil {
		return "", err
	}
	old := int64(0)
	if info, err := root.Stat(rel); err == nil {
		if info.IsDir() {
			return "", errors.New("já existe uma pasta com esse nome")
		}
		if !appendTo {
			old = info.Size()
		}
	}
	if folderSize(root)-old+int64(len(content)) > AgentQuotaBytes {
		return "", fmt.Errorf("sua pasta passaria de %s; apague o que não precisa antes", sizeText(AgentQuotaBytes))
	}
	if dir := path.Dir(rel); dir != "." {
		if err := root.MkdirAll(dir, 0o700); err != nil {
			return "", err
		}
	}
	flags := os.O_WRONLY | os.O_CREATE | os.O_TRUNC
	if appendTo {
		flags = os.O_WRONLY | os.O_CREATE | os.O_APPEND
	}
	f, err := root.OpenFile(rel, flags, 0o600)
	if err != nil {
		return "", err
	}
	if _, err := f.WriteString(content); err != nil {
		f.Close()
		return "", err
	}
	if err := f.Close(); err != nil {
		return "", err
	}
	info, _ := root.Stat(rel)
	verb := "salvo"
	if appendTo {
		verb = "atualizado"
	}
	return fmt.Sprintf("%s %s (%s).", rel, verb, sizeText(info.Size())), nil
}

func (s *Server) removeFile(agentID, file string) (string, error) {
	root, err := s.agentRoot(agentID)
	if err != nil {
		return "", err
	}
	defer root.Close()
	rel, err := relPath(file, false)
	if err != nil {
		return "", err
	}
	if _, err := root.Stat(rel); err != nil {
		return "", errors.New("não encontrado; use listar_arquivos")
	}
	if err := root.RemoveAll(rel); err != nil {
		return "", err
	}
	return rel + " apagado.", nil
}

// MaxSavedBytes caps one file a connector saves (an audio, an export).
const MaxSavedBytes = 25 << 20

// saveBytes writes a file a tool produced into the agent's folder, under the
// same quota, and returns its path there.
func (s *Server) saveBytes(agentID, file string, data []byte) (string, error) {
	if len(data) > MaxSavedBytes {
		return "", fmt.Errorf("arquivo grande demais (máx. %s)", sizeText(MaxSavedBytes))
	}
	root, err := s.agentRoot(agentID)
	if err != nil {
		return "", err
	}
	defer root.Close()
	rel, err := relPath(file, false)
	if err != nil {
		return "", err
	}
	if folderSize(root)+int64(len(data)) > AgentQuotaBytes {
		return "", fmt.Errorf("sua pasta passaria de %s; apague o que não precisa antes", sizeText(AgentQuotaBytes))
	}
	if dir := path.Dir(rel); dir != "." {
		if err := root.MkdirAll(dir, 0o700); err != nil {
			return "", err
		}
	}
	if err := root.WriteFile(rel, data, 0o600); err != nil {
		return "", err
	}
	return rel, nil
}
