package runner

import (
	"bytes"
	"context"
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"unicode"

	"github.com/GuardaChuva22/avora/internal/store"
)

type runMetaKey struct{}

// RunMeta carries per-job git and identity metadata into RunAgent without
// changing the RunAgent call signature.
type RunMeta struct {
	JobID      string
	ProjectID  string
	AgentID    string
	AgentName  string
	AgentEmail string
	CoAuthor   string
	Worktree   *bool
}

func WithRunMeta(ctx context.Context, meta RunMeta) context.Context {
	return context.WithValue(ctx, runMetaKey{}, meta)
}

func runMetaFrom(ctx context.Context) RunMeta {
	if v, ok := ctx.Value(runMetaKey{}).(RunMeta); ok {
		return v
	}
	return RunMeta{}
}

// WorktreePolicy controls isolated git worktrees for code runs. By default
// (the zero value), worktrees are enabled whenever the target folder is a git
// repository.
type WorktreePolicy struct {
	Disabled    bool
	Projects    map[string]bool
	Agents      map[string]bool
	AgentNames  map[string]string
	AgentEmails map[string]string
}

// Enabled reports whether worktrees are active for the given project and agent.
func (p WorktreePolicy) Enabled(projectID, dir, agentSlug string, override *bool) bool {
	if override != nil {
		return *override
	}
	if p.Disabled {
		return false
	}
	switch strings.ToLower(strings.TrimSpace(os.Getenv("AVORA_WORKTREE"))) {
	case "0", "false", "off", "no":
		return false
	}
	if agentSlug != "" && p.Agents != nil {
		if on, ok := p.Agents[agentSlug]; ok && !on {
			return false
		}
	}
	if p.Projects != nil {
		clean := filepath.Clean(dir)
		for _, k := range []string{projectID, clean, filepath.ToSlash(clean), filepath.Base(clean)} {
			if k == "" {
				continue
			}
			if on, ok := p.Projects[k]; ok && !on {
				return false
			}
		}
	}
	if dir != "" {
		if data, err := os.ReadFile(filepath.Join(dir, ".avora", "config.json")); err == nil {
			var cfg struct {
				Worktree *bool `json:"worktree"`
			}
			if json.Unmarshal(data, &cfg) == nil && cfg.Worktree != nil && !*cfg.Worktree {
				return false
			}
		}
	}
	return true
}

// WorktreeResult is what a finished code run in a git worktree reports back.
type WorktreeResult struct {
	Branch       string   `json:"branch,omitempty"`
	Worktree     string   `json:"worktree,omitempty"`
	Commit       string   `json:"commit,omitempty"`
	FilesChanged int      `json:"files_changed,omitempty"`
	Files        []string `json:"-"`
}

// WorktreeOptions configures one isolated worktree execution.
type WorktreeOptions struct {
	ProjectDir string
	RunID      string
	ProjectID  string
	AgentID    string
	AgentName  string
	AgentEmail string
	CoAuthor   string
	CommitMsg  string
	Policy     WorktreePolicy
	Disabled   bool
}

// Worktree is an active git worktree created for one agent execution.
type Worktree struct {
	gitBin     string
	projectDir string
	repoRoot   string
	wtRoot     string
	workDir    string
	branch     string
	baseCommit string
	agentSlug  string
	shortSlug  string
	agentName  string
	agentEmail string
	coAuthor   string
	commitMsg  string
	done       bool
}

// Dir is the working directory the agent process should run in.
func (w *Worktree) Dir() string { return w.workDir }

// Path is the root directory of the created worktree.
func (w *Worktree) Path() string { return w.wtRoot }

// Branch is the git branch created for this execution.
func (w *Worktree) Branch() string { return w.branch }

func gitExec(ctx context.Context, bin, dir string, args ...string) (string, error) {
	cmd := exec.CommandContext(ctx, bin, args...)
	cmd.Dir = dir
	cmd.Env = append(os.Environ(), "GIT_TERMINAL_PROMPT=0", "LC_ALL=C.UTF-8")
	hideWindow(cmd)
	var stdout, stderr bytes.Buffer
	cmd.Stdout = &stdout
	cmd.Stderr = &stderr
	err := cmd.Run()
	if err != nil {
		msg := strings.TrimSpace(stderr.String())
		if msg == "" {
			msg = strings.TrimSpace(stdout.String())
		}
		if msg != "" {
			return "", fmt.Errorf("git %s: %w (%s)", firstWord(args), err, msg)
		}
		return "", fmt.Errorf("git %s: %w", firstWord(args), err)
	}
	return stdout.String(), nil
}

func firstWord(args []string) string {
	if len(args) == 0 {
		return ""
	}
	return args[0]
}

func foldASCII(r rune) rune {
	r = unicode.ToLower(r)
	switch r {
	case 'á', 'à', 'â', 'ã', 'ä':
		return 'a'
	case 'é', 'è', 'ê', 'ë':
		return 'e'
	case 'í', 'ì', 'î', 'ï':
		return 'i'
	case 'ó', 'ò', 'ô', 'õ', 'ö':
		return 'o'
	case 'ú', 'ù', 'û', 'ü':
		return 'u'
	case 'ç':
		return 'c'
	case 'ñ':
		return 'n'
	}
	return r
}

func slugifyPart(s string, maxLen int) string {
	var b strings.Builder
	dash := false
	for _, r := range s {
		r = foldASCII(r)
		if (r >= 'a' && r <= 'z') || (r >= '0' && r <= '9') {
			b.WriteRune(r)
			dash = false
		} else if !dash && b.Len() > 0 {
			b.WriteByte('-')
			dash = true
		}
		if b.Len() >= maxLen {
			break
		}
	}
	return strings.Trim(b.String(), "-")
}

func slugifyAgent(raw string) string {
	if s := slugifyPart(raw, 32); s != "" {
		return s
	}
	return "agent"
}

func slugifyExec(raw string) string {
	if s := slugifyPart(raw, 24); s != "" {
		return s
	}
	buf := make([]byte, 6)
	_, _ = rand.Read(buf)
	return hex.EncodeToString(buf)
}

func humanizeSlug(slug string) string {
	parts := strings.Split(slug, "-")
	for i, p := range parts {
		if p != "" {
			parts[i] = strings.ToUpper(p[:1]) + p[1:]
		}
	}
	out := strings.Join(parts, " ")
	if out == "" {
		return "Agent"
	}
	return out
}

func cleanGitIdent(s string) string {
	s = strings.NewReplacer("<", "", ">", "", "\r", " ", "\n", " ").Replace(s)
	return strings.Join(strings.Fields(s), " ")
}

func cleanCoAuthor(s string) string {
	s = strings.TrimSpace(strings.ReplaceAll(strings.ReplaceAll(s, "\r", " "), "\n", " "))
	if s == "" {
		return ""
	}
	if strings.Contains(s, "<") && strings.Contains(s, ">") {
		return s
	}
	clean := cleanGitIdent(s)
	if clean == "" {
		return ""
	}
	if strings.Contains(clean, "@") && !strings.Contains(clean, " ") {
		local, _, _ := strings.Cut(clean, "@")
		return fmt.Sprintf("%s <%s>", humanizeSlug(slugifyPart(local, 32)), clean)
	}
	return fmt.Sprintf("%s <%s@avora.local>", clean, slugifyAgent(clean))
}

func cleanCommitSubject(msg, agentSlug, shortSlug string) string {
	line, _, _ := strings.Cut(strings.TrimSpace(msg), "\n")
	line = strings.Join(strings.Fields(line), " ")
	if line == "" {
		return fmt.Sprintf("avora: %s (%s)", agentSlug, shortSlug)
	}
	if len(line) > 120 {
		line = clip(line, 120)
	}
	return line
}

func firstNonBlank(vals ...string) string {
	for _, v := range vals {
		if strings.TrimSpace(v) != "" {
			return strings.TrimSpace(v)
		}
	}
	return ""
}

// ensureGitExclude hides /.avora/ in the repository's local info/exclude so
// active worktrees inside <project>/.avora/worktrees never dirty git status.
func ensureGitExclude(ctx context.Context, gitBin, dir string) {
	out, err := gitExec(ctx, gitBin, dir, "rev-parse", "--git-common-dir")
	if err != nil {
		return
	}
	common := strings.TrimSpace(out)
	if common == "" {
		return
	}
	common = filepath.FromSlash(common)
	if !filepath.IsAbs(common) {
		common = filepath.Join(dir, common)
	}
	infoDir := filepath.Join(common, "info")
	excludePath := filepath.Join(infoDir, "exclude")
	cur, _ := os.ReadFile(excludePath)
	for _, line := range strings.Split(strings.ReplaceAll(string(cur), "\r\n", "\n"), "\n") {
		t := strings.TrimSpace(line)
		if t == "/.avora/" || t == ".avora/" || t == ".avora" {
			return
		}
	}
	if err := os.MkdirAll(infoDir, 0o700); err != nil {
		return
	}
	prefix := ""
	if len(cur) > 0 && cur[len(cur)-1] != '\n' {
		prefix = "\n"
	}
	f, err := os.OpenFile(excludePath, os.O_CREATE|os.O_APPEND|os.O_WRONLY, 0o600)
	if err != nil {
		return
	}
	_, _ = f.WriteString(prefix + "/.avora/\n")
	_ = f.Close()
}

// StartWorktree prepares an isolated git worktree when ProjectDir is inside a
// git repository with at least one commit and worktrees are enabled. When
// ProjectDir is not a git repo, git is missing, or the switch is off, it
// returns (nil, nil) so the caller runs directly in ProjectDir.
func StartWorktree(ctx context.Context, opts WorktreeOptions) (*Worktree, error) {
	if opts.ProjectDir == "" {
		return nil, errors.New("project dir required")
	}
	projectDir := filepath.Clean(opts.ProjectDir)
	if strings.Contains(filepath.ToSlash(projectDir), "/.avora/worktrees/") {
		return nil, nil
	}
	agentSlug := slugifyAgent(firstNonBlank(opts.AgentID, opts.AgentName))
	var override *bool
	if opts.Disabled {
		off := false
		override = &off
	}
	if !opts.Policy.Enabled(opts.ProjectID, projectDir, agentSlug, override) {
		return nil, nil
	}
	gitBin, err := exec.LookPath("git")
	if err != nil {
		return nil, nil
	}
	topOut, err := gitExec(ctx, gitBin, projectDir, "rev-parse", "--show-toplevel")
	if err != nil {
		return nil, nil // not a git repository
	}
	repoRoot := filepath.Clean(filepath.FromSlash(strings.TrimSpace(topOut)))
	if cfgOut, err := gitExec(ctx, gitBin, projectDir, "config", "--bool", "avora.worktree"); err == nil && strings.TrimSpace(cfgOut) == "false" {
		return nil, nil
	}
	headOut, err := gitExec(ctx, gitBin, projectDir, "rev-parse", "--verify", "HEAD")
	if err != nil {
		return nil, nil // repository has no commits yet
	}
	baseCommit := strings.TrimSpace(headOut)

	ensureGitExclude(ctx, gitBin, projectDir)
	_, _ = gitExec(ctx, gitBin, projectDir, "worktree", "prune")

	execSlug := slugifyExec(opts.RunID)
	shortSlug := execSlug
	if len(shortSlug) > 8 {
		shortSlug = shortSlug[:8]
	}
	wtRoot := filepath.Join(projectDir, ".avora", "worktrees", agentSlug+"-"+execSlug)
	branch := "avora/" + agentSlug + "/" + shortSlug
	for i := 2; i < 50; i++ {
		if _, err := gitExec(ctx, gitBin, projectDir, "show-ref", "--verify", "--quiet", "refs/heads/"+branch); err != nil {
			break
		}
		branch = fmt.Sprintf("avora/%s/%s-%d", agentSlug, shortSlug, i)
	}
	if _, err := os.Stat(wtRoot); err == nil {
		_ = os.RemoveAll(wtRoot)
		_, _ = gitExec(ctx, gitBin, projectDir, "worktree", "prune")
	}
	if err := os.MkdirAll(filepath.Dir(wtRoot), 0o700); err != nil {
		return nil, fmt.Errorf("cannot create worktree parent: %w", err)
	}
	if _, err := gitExec(ctx, gitBin, projectDir, "worktree", "add", "-b", branch, wtRoot, "HEAD"); err != nil {
		return nil, err
	}

	workDir := wtRoot
	if rel, err := filepath.Rel(repoRoot, projectDir); err == nil && rel != "." && !strings.HasPrefix(rel, "..") {
		sub := filepath.Join(wtRoot, rel)
		if st, err := os.Stat(sub); err == nil && st.IsDir() {
			workDir = sub
		}
	}

	name := firstNonBlank(opts.AgentName, opts.Policy.AgentNames[agentSlug], humanizeSlug(agentSlug))
	email := firstNonBlank(opts.AgentEmail, opts.Policy.AgentEmails[agentSlug], agentSlug+"@avora.local")
	return &Worktree{
		gitBin:     gitBin,
		projectDir: projectDir,
		repoRoot:   repoRoot,
		wtRoot:     wtRoot,
		workDir:    workDir,
		branch:     branch,
		baseCommit: baseCommit,
		agentSlug:  agentSlug,
		shortSlug:  shortSlug,
		agentName:  name,
		agentEmail: email,
		coAuthor:   opts.CoAuthor,
		commitMsg:  opts.CommitMsg,
	}, nil
}

// Cleanup removes the worktree and its branch unconditionally (used when a
// process fails to start).
func (w *Worktree) Cleanup(ctx context.Context) {
	if w == nil || w.done {
		return
	}
	w.done = true
	w.remove(ctx)
}

func (w *Worktree) remove(ctx context.Context) {
	_, _ = gitExec(ctx, w.gitBin, w.projectDir, "worktree", "remove", "--force", w.wtRoot)
	_ = os.RemoveAll(w.wtRoot)
	_, _ = gitExec(ctx, w.gitBin, w.projectDir, "worktree", "prune")
	_, _ = gitExec(ctx, w.gitBin, w.projectDir, "branch", "-D", w.branch)
}

// Finish commits any changes in the worktree under the agent's git identity,
// or removes the worktree and branch when nothing changed.
func (w *Worktree) Finish(ctx context.Context) (WorktreeResult, error) {
	if w == nil || w.done {
		return WorktreeResult{}, nil
	}
	w.done = true

	statusOut, err := gitExec(ctx, w.gitBin, w.wtRoot, "status", "--porcelain")
	if err != nil {
		return WorktreeResult{}, err
	}
	if strings.TrimSpace(statusOut) != "" {
		if _, err := gitExec(ctx, w.gitBin, w.wtRoot, "add", "-A"); err != nil {
			return WorktreeResult{}, err
		}
		stagedOut, err := gitExec(ctx, w.gitBin, w.wtRoot, "status", "--porcelain")
		if err != nil {
			return WorktreeResult{}, err
		}
		if strings.TrimSpace(stagedOut) != "" {
			name := cleanGitIdent(w.agentName)
			if name == "" {
				name = humanizeSlug(w.agentSlug)
			}
			email := cleanGitIdent(w.agentEmail)
			if email == "" {
				email = w.agentSlug + "@avora.local"
			}
			author := fmt.Sprintf("%s <%s>", name, email)
			msg := cleanCommitSubject(w.commitMsg, w.agentSlug, w.shortSlug)
			if co := cleanCoAuthor(w.coAuthor); co != "" {
				msg += "\n\nCo-authored-by: " + co
			}
			if _, err := gitExec(ctx, w.gitBin, w.wtRoot,
				"-c", "user.name="+name,
				"-c", "user.email="+email,
				"-c", "commit.gpgsign=false",
				"commit", "--author="+author, "-m", msg,
			); err != nil {
				return WorktreeResult{}, err
			}
		}
	}

	headOut, err := gitExec(ctx, w.gitBin, w.wtRoot, "rev-parse", "HEAD")
	if err != nil {
		return WorktreeResult{}, err
	}
	headCommit := strings.TrimSpace(headOut)
	if headCommit == w.baseCommit {
		w.remove(ctx)
		return WorktreeResult{}, nil
	}

	diffOut, err := gitExec(ctx, w.gitBin, w.wtRoot, "diff", "--name-only", w.baseCommit, headCommit)
	if err != nil {
		return WorktreeResult{}, err
	}
	files := []string{}
	for _, line := range strings.Split(strings.ReplaceAll(diffOut, "\r\n", "\n"), "\n") {
		if f := strings.TrimSpace(line); f != "" {
			files = append(files, f)
		}
	}
	return WorktreeResult{
		Branch:       w.branch,
		Worktree:     w.wtRoot,
		Commit:       headCommit,
		FilesChanged: len(files),
		Files:        files,
	}, nil
}

// RunInWorktree executes fn inside an isolated worktree (when ProjectDir is a
// git repo and worktrees are enabled) and finishes the worktree afterwards.
func RunInWorktree(ctx context.Context, opts WorktreeOptions, fn func(dir string) error) (WorktreeResult, error) {
	wt, err := StartWorktree(ctx, opts)
	if err != nil {
		return WorktreeResult{}, err
	}
	if wt == nil {
		return WorktreeResult{}, fn(opts.ProjectDir)
	}
	fnErr := fn(wt.Dir())
	res, finErr := wt.Finish(ctx)
	if fnErr != nil {
		return res, fnErr
	}
	return res, finErr
}

// prepareWorkdir chooses the directory a CLI run executes in, creating an
// isolated git worktree when spec runs in edit mode inside a git repository.
func (cfg AgentConfig) prepareWorkdir(ctx context.Context, spec store.AgentSpec) (string, *Worktree, error) {
	dir, err := cfg.dirFor(spec)
	if err != nil {
		return "", nil, err
	}
	if spec.Mode != "edit" {
		return dir, nil, nil
	}
	meta := runMetaFrom(ctx)
	agentID := firstNonBlank(meta.AgentID, spec.Owner, spec.Engine)
	projectID := firstNonBlank(meta.ProjectID, spec.Workdir)
	wt, err := StartWorktree(ctx, WorktreeOptions{
		ProjectDir: dir,
		RunID:      meta.JobID,
		ProjectID:  projectID,
		AgentID:    agentID,
		AgentName:  firstNonBlank(meta.AgentName, spec.AgentName),
		AgentEmail: firstNonBlank(meta.AgentEmail, spec.AgentEmail),
		CoAuthor:   meta.CoAuthor,
		CommitMsg:  spec.Title,
		Policy:     cfg.Worktrees,
		Disabled:   meta.Worktree != nil && !*meta.Worktree,
	})
	if err != nil {
		return "", nil, err
	}
	if wt == nil {
		return dir, nil, nil
	}
	return wt.Dir(), wt, nil
}
