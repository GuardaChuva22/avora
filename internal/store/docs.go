package store

import (
	"context"
	"errors"
	"fmt"
	"path"
	"sort"
	"strconv"
	"strings"
	"time"
	"unicode"
	"unicode/utf8"

	"github.com/jackc/pgx/v5"
)

// Doc is a document of the team's Docs (D60): a Markdown document, an HTML
// page or an uploaded file. Content is the current version's text (for a
// file, the text read out of it), sent only when one doc is asked for.
type Doc struct {
	ID          string `json:"id"`
	WorkspaceID string `json:"workspace_id"`
	Title       string `json:"title"`
	Kind      string `json:"kind"` // DocKindDoc, DocKindHTML, DocKindFile
	Summary   string `json:"summary"`
	Folder    string `json:"folder"`
	ProjectID string `json:"project_id"`
	Pinned    bool   `json:"pinned"`
	Version   int    `json:"version"`
	FileID    string `json:"file_id,omitempty"`
	Mime      string `json:"mime,omitempty"`
	Size      int    `json:"size"`
	// Author made the doc, Editor saved the latest version: "admin" or
	// "agent:<id>".
	Author         string     `json:"author"`
	Editor         string     `json:"editor"`
	ConversationID string     `json:"conversation_id"`
	CreatedAt      time.Time  `json:"created_at"`
	UpdatedAt      time.Time  `json:"updated_at"`
	DeletedAt      *time.Time `json:"deleted_at,omitempty"`

	Content string `json:"content,omitempty"`
	// Shown is the version Content is from, when an older one was asked.
	Shown int `json:"shown,omitempty"`
	// Snippet is the text around the first word of a search.
	Snippet string `json:"snippet,omitempty"`
	search  string
	links   []DocLink
}

// DocLink is one [[Title]] or [[Title|label]] reference extracted from a doc.
type DocLink struct {
	FromID  string `json:"from_id"`
	ToTitle string `json:"to_title"`
	ToID    string `json:"to_id,omitempty"`
}

// DocBacklink is a live doc that links to another doc via [[Title]].
type DocBacklink struct {
	ID        string    `json:"id"`
	Title     string    `json:"title"`
	Author    string    `json:"author"`
	UpdatedAt time.Time `json:"updated_at"`
	Snippet   string    `json:"snippet"`
}

// DocLinkStore extends Store with wiki links, backlinks, title resolution and
// version restore without conflicting with concurrent edits to Store.
type DocLinkStore interface {
	DocLinks(ctx context.Context, fromID string) ([]DocLink, error)
	DocBacklinks(ctx context.Context, id string, scope *string) ([]DocBacklink, error)
	ResolveDoc(ctx context.Context, title, folder string, scope *string) (Doc, error)
	RestoreDocVersion(ctx context.Context, id string, version int, actor string, now time.Time) (Doc, error)
}

func DocLinks(ctx context.Context, s Store, fromID string) ([]DocLink, error) {
	if ls, ok := s.(DocLinkStore); ok {
		return ls.DocLinks(ctx, fromID)
	}
	return []DocLink{}, nil
}

func DocBacklinks(ctx context.Context, s Store, id string, scope *string) ([]DocBacklink, error) {
	if ls, ok := s.(DocLinkStore); ok {
		return ls.DocBacklinks(ctx, id, scope)
	}
	return []DocBacklink{}, nil
}

func ResolveDoc(ctx context.Context, s Store, title, folder string, scope *string) (Doc, error) {
	if ls, ok := s.(DocLinkStore); ok {
		return ls.ResolveDoc(ctx, title, folder, scope)
	}
	return Doc{}, ErrNotFound
}

func RestoreDocVersion(ctx context.Context, s Store, id string, version int, actor string, now time.Time) (Doc, error) {
	if ls, ok := s.(DocLinkStore); ok {
		return ls.RestoreDocVersion(ctx, id, version, actor, now)
	}
	return Doc{}, ErrNotFound
}

// DocVersion is one saved version of a doc (Content only when asked).
type DocVersion struct {
	DocID     string    `json:"doc_id"`
	Version   int       `json:"version"`
	Content   string    `json:"content,omitempty"`
	FileID    string    `json:"file_id,omitempty"`
	Mime      string    `json:"mime,omitempty"`
	Size      int       `json:"size"`
	Title     string    `json:"title"`
	Note      string    `json:"note"`
	Author    string    `json:"author"`
	CreatedAt time.Time `json:"created_at"`
}

const (
	DocKindDoc  = "doc"
	DocKindHTML = "html"
	DocKindFile = "file"

	EventDocSaved    = "doc.saved"
	EventDocTrashed  = "doc.trashed"
	EventDocRestored = "doc.restored"
	EventDocDeleted  = "doc.deleted"

	// DocTrashDays is how long a trashed doc can be restored.
	DocTrashDays = 30
	// MaxDocVersions bounds the saved version history kept per doc.
	MaxDocVersions = 50
)

// ErrStale: someone saved a newer version since the one being edited.
var ErrStale = errors.New("a newer version was saved")

// DocFilter narrows ListDocs. Scope, when set, keeps the docs of that
// project and the universal ones (an agent of a project).
type DocFilter struct {
	Query  string
	Folder string // this folder and the ones inside it
	Kind   string
	Pinned bool
	Trash  bool // only trashed docs; otherwise only live ones
	Scope  *string
	// Actor keeps the docs this actor ("agent:<id>") created or last edited.
	Actor       string
	WorkspaceID string
	Limit       int
}

// DocChange edits a doc. Nil fields stay; Content or FileID make a new
// version (unless they equal the current one).
type DocChange struct {
	Title       *string
	Summary     *string
	Folder      *string
	ProjectID   *string
	Pinned      *bool
	Content     *string
	FileID      string
	Mime        string
	Size        int
	Note        string
	BaseVersion int // >0: ErrStale unless it is still the current version
}

// FoldRunes lowers and strips accents rune by rune, so positions in the
// folded text match the original's runes.
func FoldRunes(s string) []rune {
	out := []rune(s)
	for i, r := range out {
		out[i] = foldRune(r)
	}
	return out
}

// Fold is FoldRunes as a string: what searches compare.
func Fold(s string) string { return string(FoldRunes(s)) }

func foldRune(r rune) rune {
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

// SearchWords are the folded words of a query.
func SearchWords(q string) []string {
	return strings.FieldsFunc(Fold(q), func(r rune) bool {
		return !unicode.IsLetter(r) && !unicode.IsDigit(r) && r != '-' && r != '_' && r != '.'
	})
}

func docSearchText(d Doc) string {
	return Fold(d.Title + "\n" + d.Summary + "\n" + d.Folder + "\n" + d.Content)
}

// docSnippet is about 200 characters of content around the first word found.
func docSnippet(content string, words []string) string {
	if len(words) == 0 || content == "" {
		return ""
	}
	orig := []rune(content)
	folded := FoldRunes(content)
	text := string(folded)
	at := -1
	for _, w := range words {
		if i := strings.Index(text, w); i >= 0 {
			at = utf8.RuneCountInString(text[:i])
			break
		}
	}
	if at < 0 {
		return ""
	}
	from, to := max(0, at-70), min(len(orig), at+150)
	out := strings.Join(strings.Fields(string(orig[from:to])), " ")
	if from > 0 {
		out = "…" + out
	}
	if to < len(orig) {
		out += "…"
	}
	return out
}

// docRank orders search hits: words in the title, then the summary, pinned
// docs, then the newest.
func docRank(list []Doc, words []string) {
	score := func(d Doc) int {
		n, title, sum := 0, Fold(d.Title), Fold(d.Summary)
		for _, w := range words {
			if strings.Contains(title, w) {
				n += 4
			}
			if strings.Contains(sum, w) {
				n += 2
			}
		}
		if d.Pinned {
			n++
		}
		return n
	}
	sort.SliceStable(list, func(i, j int) bool {
		si, sj := score(list[i]), score(list[j])
		if si != sj {
			return si > sj
		}
		return list[i].UpdatedAt.After(list[j].UpdatedAt)
	})
}

func inFolder(folder, want string) bool {
	return want == "" || folder == want || strings.HasPrefix(folder, want+"/")
}

func docEvent(d Doc) map[string]any {
	return map[string]any{"id": d.ID, "title": d.Title, "kind": d.Kind, "version": d.Version, "folder": d.Folder, "editor": d.Editor}
}

// applyChange puts a change on d and says whether it makes a new version.
func applyChange(d *Doc, c DocChange) bool {
	if c.Title != nil {
		d.Title = *c.Title
	}
	if c.Summary != nil {
		d.Summary = *c.Summary
	}
	if c.Folder != nil {
		d.Folder = *c.Folder
	}
	if c.ProjectID != nil {
		d.ProjectID = *c.ProjectID
	}
	if c.Pinned != nil {
		d.Pinned = *c.Pinned
	}
	changed := false
	if c.FileID != "" && c.FileID != d.FileID {
		d.FileID, d.Mime, d.Size, changed = c.FileID, c.Mime, c.Size, true
	}
	if c.Content != nil && *c.Content != d.Content {
		d.Content, changed = *c.Content, true
		if d.Kind != DocKindFile {
			d.Size = len(d.Content)
		}
	}
	return changed
}

func newDocVersion(d Doc, note, actor string, now time.Time) DocVersion {
	return DocVersion{DocID: d.ID, Version: d.Version, Content: d.Content, FileID: d.FileID, Mime: d.Mime, Size: d.Size,
		Title: d.Title, Note: note, Author: actor, CreatedAt: ts(now)}
}

func filterDocs(all []Doc, f DocFilter) []Doc {
	words := SearchWords(f.Query)
	out := []Doc{}
	for _, d := range all {
		if (d.DeletedAt != nil) != f.Trash || !inFolder(d.Folder, f.Folder) || (f.Kind != "" && d.Kind != f.Kind) || (f.Pinned && !d.Pinned) {
			continue
		}
		if f.Scope != nil && d.ProjectID != "" && d.ProjectID != *f.Scope {
			continue
		}
		if f.Actor != "" && d.Author != f.Actor && d.Editor != f.Actor {
			continue
		}
		if f.WorkspaceID != "" && effectiveWorkspace(d.WorkspaceID) != f.WorkspaceID {
			continue
		}
		ok := true
		for _, w := range words {
			if !strings.Contains(d.search, w) {
				ok = false
				break
			}
		}
		if ok {
			out = append(out, d)
		}
	}
	return out
}

func finishList(list []Doc, f DocFilter) []Doc {
	words := SearchWords(f.Query)
	if len(words) > 0 {
		docRank(list, words)
	}
	limit := f.Limit
	if limit <= 0 || limit > 500 {
		limit = 200
	}
	if len(list) > limit {
		list = list[:limit]
	}
	for i := range list {
		if len(words) > 0 {
			list[i].Snippet = docSnippet(list[i].Content, words)
		}
		list[i].Content, list[i].search, list[i].links = "", "", nil
	}
	return list
}

func trimDocVersions(vs []DocVersion) []DocVersion {
	if len(vs) <= MaxDocVersions {
		return vs
	}
	return append([]DocVersion(nil), vs[len(vs)-MaxDocVersions:]...)
}

func fenceOpen(line string) (byte, int) {
	i := 0
	for i < len(line) && i < 3 && line[i] == ' ' {
		i++
	}
	if i >= len(line) || (line[i] != '`' && line[i] != '~') {
		return 0, 0
	}
	ch := line[i]
	n := 0
	for i+n < len(line) && line[i+n] == ch {
		n++
	}
	if n < 3 {
		return 0, 0
	}
	if ch == '`' && strings.ContainsRune(line[i+n:], '`') {
		return 0, 0
	}
	return ch, n
}

func fenceClose(line string, ch byte, want int) bool {
	i := 0
	for i < len(line) && i < 3 && line[i] == ' ' {
		i++
	}
	n := 0
	for i+n < len(line) && line[i+n] == ch {
		n++
	}
	if n < want {
		return false
	}
	return strings.TrimSpace(line[i+n:]) == ""
}

func stripInlineCode(line string) string {
	if !strings.ContainsRune(line, '`') {
		return line
	}
	b := []byte(line)
	for i := 0; i < len(b); {
		if b[i] != '`' {
			i++
			continue
		}
		n := 0
		for i+n < len(b) && b[i+n] == '`' {
			n++
		}
		end := -1
		for j := i + n; j < len(b); {
			if b[j] != '`' {
				j++
				continue
			}
			m := 0
			for j+m < len(b) && b[j+m] == '`' {
				m++
			}
			if m == n {
				end = j + m
				break
			}
			j += m
		}
		if end < 0 {
			i += n
			continue
		}
		for k := i; k < end; k++ {
			b[k] = ' '
		}
		i = end
	}
	return string(b)
}

// ExtractDocLinks returns the distinct target titles referenced via [[Title]]
// or [[Title|label]] in Markdown content, ignoring code spans and code blocks.
func ExtractDocLinks(content string) []string {
	if !strings.Contains(content, "[[") {
		return []string{}
	}
	lines := strings.Split(strings.ReplaceAll(content, "\r\n", "\n"), "\n")
	out := []string{}
	seen := map[string]bool{}
	var fch byte
	var flen int
	for _, rawLine := range lines {
		if fch != 0 {
			if fenceClose(rawLine, fch, flen) {
				fch, flen = 0, 0
			}
			continue
		}
		if ch, n := fenceOpen(rawLine); ch != 0 {
			fch, flen = ch, n
			continue
		}
		line := stripInlineCode(rawLine)
		for i := 0; i+3 < len(line); {
			open := strings.Index(line[i:], "[[")
			if open < 0 {
				break
			}
			start := i + open + 2
			closeRel := strings.Index(line[start:], "]]")
			if closeRel < 0 {
				break
			}
			inner := line[start : start+closeRel]
			if nextOpen := strings.Index(inner, "[["); nextOpen >= 0 {
				i = start + nextOpen
				continue
			}
			i = start + closeRel + 2
			if strings.ContainsAny(inner, "[]") {
				continue
			}
			rawTitle, _, _ := strings.Cut(inner, "|")
			title := strings.Join(strings.Fields(rawTitle), " ")
			if title == "" || utf8.RuneCountInString(title) > 200 {
				continue
			}
			key := Fold(title)
			if key == "" || seen[key] {
				continue
			}
			seen[key] = true
			out = append(out, title)
		}
	}
	return out
}

func cleanWikiText(s string) string {
	if !strings.Contains(s, "[[") {
		return s
	}
	var b strings.Builder
	for i := 0; i < len(s); {
		open := strings.Index(s[i:], "[[")
		if open < 0 {
			b.WriteString(s[i:])
			break
		}
		b.WriteString(s[i : i+open])
		start := i + open + 2
		closeRel := strings.Index(s[start:], "]]")
		if closeRel < 0 || strings.ContainsRune(s[start:start+closeRel], '\n') {
			b.WriteString("[[")
			i = start
			continue
		}
		inner := s[start : start+closeRel]
		target, label, hasPipe := strings.Cut(inner, "|")
		shown := strings.TrimSpace(target)
		if hasPipe && strings.TrimSpace(label) != "" {
			shown = strings.TrimSpace(label)
		}
		b.WriteString(shown)
		i = start + closeRel + 2
	}
	return b.String()
}

func backlinkSnippet(content, linkTitle, targetTitle string) string {
	if content == "" {
		return ""
	}
	wantA, wantB := Fold(strings.Join(strings.Fields(linkTitle), " ")), Fold(strings.Join(strings.Fields(targetTitle), " "))
	at := -1
	for i := 0; i+3 < len(content); {
		open := strings.Index(content[i:], "[[")
		if open < 0 {
			break
		}
		pos := i + open
		start := pos + 2
		closeRel := strings.Index(content[start:], "]]")
		if closeRel < 0 {
			break
		}
		inner := content[start : start+closeRel]
		if !strings.ContainsRune(inner, '\n') {
			rawTitle, _, _ := strings.Cut(inner, "|")
			got := Fold(strings.Join(strings.Fields(rawTitle), " "))
			if got != "" && (got == wantA || (wantB != "" && got == wantB)) {
				at = utf8.RuneCountInString(content[:pos])
				break
			}
		}
		i = start + closeRel + 2
	}
	if at < 0 {
		q := linkTitle
		if q == "" {
			q = targetTitle
		}
		return docSnippet(cleanWikiText(content), SearchWords(q))
	}
	orig := []rune(content)
	from, to := max(0, at-70), min(len(orig), at+150)
	out := strings.Join(strings.Fields(cleanWikiText(string(orig[from:to]))), " ")
	if from > 0 {
		out = "…" + out
	}
	if to < len(orig) {
		out += "…"
	}
	return out
}

func pickResolvedDoc(candidates []Doc, wantTitle, fromFolder, fromProject string, scope *string, excludeID string) (Doc, bool) {
	wantTitle = strings.Join(strings.Fields(wantTitle), " ")
	wantFold := Fold(wantTitle)
	if wantFold == "" {
		return Doc{}, false
	}
	slashFolder, slashBase := "", ""
	if strings.Contains(wantTitle, "/") {
		dir, base := path.Split(wantTitle)
		slashFolder = strings.Trim(dir, "/")
		slashBase = strings.TrimSpace(base)
	}
	var best Doc
	bestScore := -1
	for _, d := range candidates {
		if d.DeletedAt != nil || (excludeID != "" && d.ID == excludeID) {
			continue
		}
		if scope != nil {
			if d.ProjectID != "" && d.ProjectID != *scope {
				continue
			}
		} else if fromProject != "" && d.ProjectID != "" && d.ProjectID != fromProject {
			continue
		}
		exactFold := Fold(d.Title) == wantFold
		pathMatch := !exactFold && slashBase != "" && Fold(d.Title) == Fold(slashBase) && Fold(d.Folder) == Fold(slashFolder)
		if !exactFold && !pathMatch {
			continue
		}
		score := 0
		if pathMatch {
			score += 16
		}
		if fromFolder != "" && Fold(d.Folder) == Fold(fromFolder) {
			score += 8
		} else if fromFolder == "" && d.Folder == "" {
			score += 2
		}
		if fromProject != "" && d.ProjectID == fromProject {
			score += 4
		}
		if strings.EqualFold(d.Title, wantTitle) {
			score++
		}
		if bestScore < 0 || score > bestScore ||
			(score == bestScore && (d.UpdatedAt.After(best.UpdatedAt) || (d.UpdatedAt.Equal(best.UpdatedAt) && d.ID < best.ID))) {
			best, bestScore = d, score
		}
	}
	return best, bestScore >= 0
}

// ── Memory ──────────────────────────────────────────────────────

func (m *Memory) syncDocLinks() {
	live := make([]Doc, 0, len(m.docs))
	for _, d := range m.docs {
		if d.DeletedAt == nil {
			live = append(live, d)
		}
	}
	for id, d := range m.docs {
		if d.DeletedAt != nil || d.Kind != DocKindDoc {
			d.links = nil
			m.docs[id] = d
			continue
		}
		titles := ExtractDocLinks(d.Content)
		links := make([]DocLink, 0, len(titles))
		for _, title := range titles {
			toID := ""
			if target, ok := pickResolvedDoc(live, title, d.Folder, d.ProjectID, nil, d.ID); ok {
				toID = target.ID
			}
			links = append(links, DocLink{FromID: d.ID, ToTitle: title, ToID: toID})
		}
		d.links = links
		m.docs[id] = d
	}
}

func (m *Memory) ListDocs(ctx context.Context, f DocFilter) ([]Doc, error) {
	if f.WorkspaceID == "" {
		f.WorkspaceID = WorkspaceFromContext(ctx)
	}
	m.mu.Lock()
	all := make([]Doc, 0, len(m.docs))
	for _, d := range m.docs {
		d.WorkspaceID = effectiveWorkspace(d.WorkspaceID)
		all = append(all, d)
	}
	m.mu.Unlock()
	sort.Slice(all, func(i, j int) bool { return all[i].UpdatedAt.After(all[j].UpdatedAt) })
	return finishList(filterDocs(all, f), f), nil
}

func (m *Memory) GetDoc(_ context.Context, id string, version int) (Doc, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	d, ok := m.docs[id]
	if !ok {
		return Doc{}, ErrNotFound
	}
	d.WorkspaceID = effectiveWorkspace(d.WorkspaceID)
	d.search, d.links = "", nil
	if version > 0 && version != d.Version {
		for _, v := range m.docVersions[id] {
			if v.Version == version {
				d.Content, d.FileID, d.Mime, d.Size, d.Shown = v.Content, v.FileID, v.Mime, v.Size, v.Version
				return d, nil
			}
		}
		return Doc{}, ErrNotFound
	}
	return d, nil
}

func (m *Memory) DocVersions(_ context.Context, id string) ([]DocVersion, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	if _, ok := m.docs[id]; !ok {
		return nil, ErrNotFound
	}
	out := []DocVersion{}
	vs := m.docVersions[id]
	for i := len(vs) - 1; i >= 0; i-- {
		v := vs[i]
		v.Content = ""
		out = append(out, v)
	}
	return out, nil
}

func (m *Memory) CreateDoc(_ context.Context, d Doc, note, actor string, now time.Time) (Doc, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	if m.docs == nil {
		m.docs, m.docVersions = map[string]Doc{}, map[string][]DocVersion{}
	}
	if _, ok := m.docs[d.ID]; ok {
		return Doc{}, ErrConflict
	}
	d = newDoc(d, actor, now)
	m.docs[d.ID] = d
	m.docVersions[d.ID] = []DocVersion{newDocVersion(d, note, actor, now)}
	m.syncDocLinks()
	m.emit(now, EventDocSaved, actor, "", "", docEvent(d))
	d.search, d.links = "", nil
	return d, nil
}

func newDoc(d Doc, actor string, now time.Time) Doc {
	d.WorkspaceID = effectiveWorkspace(d.WorkspaceID)
	d.Version, d.Author, d.Editor = 1, actor, actor
	d.CreatedAt, d.UpdatedAt, d.DeletedAt = ts(now), ts(now), nil
	if d.Kind != DocKindFile {
		d.Size = len(d.Content)
	}
	d.search = docSearchText(d)
	return d
}

func (m *Memory) UpdateDoc(_ context.Context, id string, c DocChange, actor string, now time.Time) (Doc, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	d, ok := m.docs[id]
	if !ok || d.DeletedAt != nil {
		return Doc{}, ErrNotFound
	}
	if c.BaseVersion > 0 && c.BaseVersion != d.Version {
		return Doc{}, ErrStale
	}
	if applyChange(&d, c) {
		d.Version++
		m.docVersions[id] = trimDocVersions(append(m.docVersions[id], newDocVersion(d, c.Note, actor, now)))
	}
	d.Editor, d.UpdatedAt, d.search = actor, ts(now), docSearchText(d)
	m.docs[id] = d
	m.syncDocLinks()
	m.emit(now, EventDocSaved, actor, "", "", docEvent(d))
	d.search, d.links = "", nil
	return d, nil
}

func (m *Memory) RestoreDocVersion(_ context.Context, id string, version int, actor string, now time.Time) (Doc, error) {
	if version < 1 {
		return Doc{}, ErrNotFound
	}
	m.mu.Lock()
	defer m.mu.Unlock()
	d, ok := m.docs[id]
	if !ok || d.DeletedAt != nil {
		return Doc{}, ErrNotFound
	}
	if version == d.Version {
		d.search, d.links = "", nil
		return d, nil
	}
	var found *DocVersion
	for i := range m.docVersions[id] {
		if m.docVersions[id][i].Version == version {
			found = &m.docVersions[id][i]
			break
		}
	}
	if found == nil {
		return Doc{}, ErrNotFound
	}
	d.Content, d.FileID, d.Mime, d.Size = found.Content, found.FileID, found.Mime, found.Size
	if found.Title != "" {
		d.Title = found.Title
	}
	if d.Kind != DocKindFile {
		d.Size = len(d.Content)
	}
	d.Version++
	d.Editor, d.UpdatedAt, d.search = actor, ts(now), docSearchText(d)
	note := fmt.Sprintf("restaurado da v%d", version)
	m.docVersions[id] = trimDocVersions(append(m.docVersions[id], newDocVersion(d, note, actor, now)))
	m.docs[id] = d
	m.syncDocLinks()
	m.emit(now, EventDocSaved, actor, "", "", docEvent(d))
	d.search, d.links = "", nil
	return d, nil
}

func (m *Memory) DocLinks(_ context.Context, fromID string) ([]DocLink, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	d, ok := m.docs[fromID]
	if !ok {
		return nil, ErrNotFound
	}
	out := make([]DocLink, len(d.links))
	copy(out, d.links)
	return out, nil
}

func (m *Memory) DocBacklinks(_ context.Context, id string, scope *string) ([]DocBacklink, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	target, ok := m.docs[id]
	if !ok {
		return nil, ErrNotFound
	}
	type hit struct {
		doc     Doc
		toTitle string
	}
	hits := []hit{}
	for _, d := range m.docs {
		if d.DeletedAt != nil || d.ID == id {
			continue
		}
		if scope != nil && d.ProjectID != "" && d.ProjectID != *scope {
			continue
		}
		for _, l := range d.links {
			if l.ToID == id {
				hits = append(hits, hit{doc: d, toTitle: l.ToTitle})
				break
			}
		}
	}
	sort.Slice(hits, func(i, j int) bool {
		if !hits[i].doc.UpdatedAt.Equal(hits[j].doc.UpdatedAt) {
			return hits[i].doc.UpdatedAt.After(hits[j].doc.UpdatedAt)
		}
		return hits[i].doc.ID < hits[j].doc.ID
	})
	out := make([]DocBacklink, 0, len(hits))
	for _, h := range hits {
		out = append(out, DocBacklink{
			ID:        h.doc.ID,
			Title:     h.doc.Title,
			Author:    h.doc.Author,
			UpdatedAt: h.doc.UpdatedAt,
			Snippet:   backlinkSnippet(h.doc.Content, h.toTitle, target.Title),
		})
	}
	return out, nil
}

func (m *Memory) ResolveDoc(_ context.Context, title, folder string, scope *string) (Doc, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	live := make([]Doc, 0, len(m.docs))
	for _, d := range m.docs {
		if d.DeletedAt == nil {
			live = append(live, d)
		}
	}
	d, ok := pickResolvedDoc(live, title, folder, "", scope, "")
	if !ok {
		return Doc{}, ErrNotFound
	}
	d.search, d.links = "", nil
	return d, nil
}

func (m *Memory) TrashDoc(_ context.Context, id string, trash bool, actor string, now time.Time) (Doc, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	d, ok := m.docs[id]
	if !ok || (d.DeletedAt != nil) == trash {
		return Doc{}, ErrNotFound
	}
	typ := EventDocRestored
	if trash {
		at := ts(now)
		d.DeletedAt, typ = &at, EventDocTrashed
	} else {
		d.DeletedAt = nil
	}
	m.docs[id] = d
	m.syncDocLinks()
	m.emit(now, typ, actor, "", "", docEvent(d))
	d.search, d.Content, d.links = "", "", nil
	return d, nil
}

func (m *Memory) PurgeDocs(_ context.Context, id string, before time.Time, actor string, now time.Time) (int, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	n := 0
	for k, d := range m.docs {
		if d.DeletedAt == nil || (id != "" && k != id) || (id == "" && !d.DeletedAt.Before(before)) {
			continue
		}
		delete(m.docs, k)
		delete(m.docVersions, k)
		m.emit(now, EventDocDeleted, actor, "", "", docEvent(d))
		n++
	}
	if n > 0 {
		m.syncDocLinks()
	}
	if id != "" && n == 0 {
		return 0, ErrNotFound
	}
	return n, nil
}

// ── Postgres ────────────────────────────────────────────────────

const docColumns = `id, title, kind, summary, folder, project_id, pinned, version, content, file_id, mime, size,
	author, editor, conversation_id, search, created_at, updated_at, deleted_at, workspace_id`

func scanDoc(row pgx.Row) (Doc, error) {
	var d Doc
	err := row.Scan(&d.ID, &d.Title, &d.Kind, &d.Summary, &d.Folder, &d.ProjectID, &d.Pinned, &d.Version, &d.Content,
		&d.FileID, &d.Mime, &d.Size, &d.Author, &d.Editor, &d.ConversationID, &d.search, &d.CreatedAt, &d.UpdatedAt, &d.DeletedAt, &d.WorkspaceID)
	if errors.Is(err, pgx.ErrNoRows) {
		return Doc{}, ErrNotFound
	}
	d.CreatedAt, d.UpdatedAt = d.CreatedAt.UTC(), d.UpdatedAt.UTC()
	if d.DeletedAt != nil {
		t := d.DeletedAt.UTC()
		d.DeletedAt = &t
	}
	return d, err
}

func likeEscape(s string) string {
	return strings.NewReplacer(`\`, `\\`, `%`, `\%`, `_`, `\_`).Replace(s)
}

func (p *Postgres) ListDocs(ctx context.Context, f DocFilter) ([]Doc, error) {
	where := []string{"deleted_at IS NULL"}
	if f.Trash {
		where[0] = "deleted_at IS NOT NULL"
	}
	args := []any{}
	arg := func(v any) string {
		args = append(args, v)
		return "$" + strconv.Itoa(len(args))
	}
	if f.Folder != "" {
		where = append(where, "(folder = "+arg(f.Folder)+" OR folder LIKE "+arg(likeEscape(f.Folder)+"/%")+")")
	}
	if f.Kind != "" {
		where = append(where, "kind = "+arg(f.Kind))
	}
	if f.Pinned {
		where = append(where, "pinned")
	}
	if f.Scope != nil {
		where = append(where, "(project_id = '' OR project_id = "+arg(*f.Scope)+")")
	}
	if f.Actor != "" {
		a := arg(f.Actor)
		where = append(where, "(author = "+a+" OR editor = "+a+")")
	}
	if ws := firstWorkspace(f.WorkspaceID, WorkspaceFromContext(ctx)); ws != "" {
		where = append(where, "workspace_id = "+arg(ws))
	}
	words := SearchWords(f.Query)
	for _, w := range words {
		where = append(where, "search LIKE "+arg("%"+likeEscape(w)+"%"))
	}
	limit := f.Limit
	if limit <= 0 || limit > 500 {
		limit = 200
	}
	content := "''"
	if len(words) > 0 {
		content = "content"
		limit = 1000 // ranked in Go, then cut
	}
	cols := strings.Replace(docColumns, "version, content,", "version, "+content+",", 1)
	cols = strings.Replace(cols, "conversation_id, search,", "conversation_id, '',", 1)
	rows, err := p.pool.Query(ctx, `SELECT `+cols+` FROM docs WHERE `+strings.Join(where, " AND ")+
		` ORDER BY updated_at DESC, id LIMIT `+strconv.Itoa(limit), args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []Doc{}
	for rows.Next() {
		d, err := scanDoc(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, d)
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}
	return finishList(out, f), nil
}

func (p *Postgres) GetDoc(ctx context.Context, id string, version int) (Doc, error) {
	d, err := scanDoc(p.pool.QueryRow(ctx, `SELECT `+docColumns+` FROM docs WHERE id = $1`, id))
	if err != nil {
		return Doc{}, err
	}
	d.search = ""
	if version > 0 && version != d.Version {
		err := p.pool.QueryRow(ctx, `SELECT content, file_id, mime, size FROM doc_versions WHERE doc_id = $1 AND version = $2`, id, version).
			Scan(&d.Content, &d.FileID, &d.Mime, &d.Size)
		if errors.Is(err, pgx.ErrNoRows) {
			return Doc{}, ErrNotFound
		}
		if err != nil {
			return Doc{}, err
		}
		d.Shown = version
	}
	return d, nil
}

func (p *Postgres) DocVersions(ctx context.Context, id string) ([]DocVersion, error) {
	var exists bool
	if err := p.pool.QueryRow(ctx, `SELECT EXISTS (SELECT 1 FROM docs WHERE id = $1)`, id).Scan(&exists); err != nil {
		return nil, err
	}
	if !exists {
		return nil, ErrNotFound
	}
	rows, err := p.pool.Query(ctx, `SELECT doc_id, version, file_id, mime, size, title, note, author, created_at
		FROM doc_versions WHERE doc_id = $1 ORDER BY version DESC`, id)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []DocVersion{}
	for rows.Next() {
		var v DocVersion
		if err := rows.Scan(&v.DocID, &v.Version, &v.FileID, &v.Mime, &v.Size, &v.Title, &v.Note, &v.Author, &v.CreatedAt); err != nil {
			return nil, err
		}
		v.CreatedAt = v.CreatedAt.UTC()
		out = append(out, v)
	}
	return out, rows.Err()
}

func insertDocVersion(ctx context.Context, tx pgx.Tx, v DocVersion) error {
	if _, err := tx.Exec(ctx, `INSERT INTO doc_versions (doc_id, version, content, file_id, mime, size, title, note, author, created_at)
		VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9, $10)`,
		v.DocID, v.Version, v.Content, v.FileID, v.Mime, v.Size, v.Title, v.Note, v.Author, v.CreatedAt); err != nil {
		return err
	}
	_, err := tx.Exec(ctx, `DELETE FROM doc_versions WHERE doc_id = $1 AND version <= $2`, v.DocID, v.Version-MaxDocVersions)
	return err
}

func firstWorkspace(a, b string) string {
	if a != "" {
		return a
	}
	return b
}

func liveDocHeadsTx(ctx context.Context, tx pgx.Tx) ([]Doc, error) {
	rows, err := tx.Query(ctx, `SELECT id, title, folder, project_id, updated_at FROM docs WHERE deleted_at IS NULL`)
	if err != nil {
		return nil, err
	}
	live := []Doc{}
	for rows.Next() {
		var d Doc
		if err := rows.Scan(&d.ID, &d.Title, &d.Folder, &d.ProjectID, &d.UpdatedAt); err != nil {
			rows.Close()
			return nil, err
		}
		d.UpdatedAt = d.UpdatedAt.UTC()
		live = append(live, d)
	}
	rows.Close()
	return live, rows.Err()
}

func rebindDocLinksTx(ctx context.Context, tx pgx.Tx, live []Doc, skipFrom string) error {
	rows, err := tx.Query(ctx, `SELECT dl.from_id, dl.to_title, dl.to_id, s.folder, s.project_id
		FROM doc_links dl
		JOIN docs s ON s.id = dl.from_id
		WHERE s.deleted_at IS NULL AND ($1 = '' OR dl.from_id <> $1)`, skipFrom)
	if err != nil {
		return err
	}
	type linkRow struct {
		fromID, toTitle, toID, folder, projectID string
	}
	existing := []linkRow{}
	for rows.Next() {
		var r linkRow
		if err := rows.Scan(&r.fromID, &r.toTitle, &r.toID, &r.folder, &r.projectID); err != nil {
			rows.Close()
			return err
		}
		existing = append(existing, r)
	}
	rows.Close()
	if err := rows.Err(); err != nil {
		return err
	}
	for _, r := range existing {
		wantID := ""
		if target, ok := pickResolvedDoc(live, r.toTitle, r.folder, r.projectID, nil, r.fromID); ok {
			wantID = target.ID
		}
		if wantID != r.toID {
			if _, err := tx.Exec(ctx, `UPDATE doc_links SET to_id = $3 WHERE from_id = $1 AND to_title = $2`,
				r.fromID, r.toTitle, wantID); err != nil {
				return err
			}
		}
	}
	return nil
}

func syncDocLinksTx(ctx context.Context, tx pgx.Tx, d Doc) error {
	live, err := liveDocHeadsTx(ctx, tx)
	if err != nil {
		return err
	}
	if _, err := tx.Exec(ctx, `DELETE FROM doc_links WHERE from_id = $1`, d.ID); err != nil {
		return err
	}
	if d.DeletedAt == nil && d.Kind == DocKindDoc {
		for _, title := range ExtractDocLinks(d.Content) {
			toID := ""
			if target, ok := pickResolvedDoc(live, title, d.Folder, d.ProjectID, nil, d.ID); ok {
				toID = target.ID
			}
			if _, err := tx.Exec(ctx, `INSERT INTO doc_links (from_id, to_title, to_id) VALUES ($1, $2, $3)`,
				d.ID, title, toID); err != nil {
				return err
			}
		}
	}
	return rebindDocLinksTx(ctx, tx, live, d.ID)
}

func (p *Postgres) CreateDoc(ctx context.Context, d Doc, note, actor string, now time.Time) (Doc, error) {
	d = newDoc(d, actor, now)
	err := p.inTx(ctx, func(tx pgx.Tx) error {
		tag, err := tx.Exec(ctx, `INSERT INTO docs (`+docColumns+`)
			VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9, $10, $11, $12, $13, $14, $15, $16, $17, $18, NULL, $19)
			ON CONFLICT (id) DO NOTHING`,
			d.ID, d.Title, d.Kind, d.Summary, d.Folder, d.ProjectID, d.Pinned, d.Version, d.Content, d.FileID, d.Mime, d.Size,
			d.Author, d.Editor, d.ConversationID, d.search, d.CreatedAt, d.UpdatedAt, d.WorkspaceID)
		if err != nil {
			return err
		}
		if tag.RowsAffected() == 0 {
			return ErrConflict
		}
		if err := insertDocVersion(ctx, tx, newDocVersion(d, note, actor, now)); err != nil {
			return err
		}
		if err := syncDocLinksTx(ctx, tx, d); err != nil {
			return err
		}
		return emit(ctx, tx, now, EventDocSaved, actor, "", "", docEvent(d))
	})
	d.search = ""
	return d, err
}

func (p *Postgres) UpdateDoc(ctx context.Context, id string, c DocChange, actor string, now time.Time) (Doc, error) {
	var d Doc
	err := p.inTx(ctx, func(tx pgx.Tx) error {
		var err error
		d, err = scanDoc(tx.QueryRow(ctx, `SELECT `+docColumns+` FROM docs WHERE id = $1 AND deleted_at IS NULL FOR UPDATE`, id))
		if err != nil {
			return err
		}
		if c.BaseVersion > 0 && c.BaseVersion != d.Version {
			return ErrStale
		}
		if applyChange(&d, c) {
			d.Version++
			if err := insertDocVersion(ctx, tx, newDocVersion(d, c.Note, actor, now)); err != nil {
				return err
			}
		}
		d.Editor, d.UpdatedAt, d.search = actor, ts(now), docSearchText(d)
		if _, err := tx.Exec(ctx, `UPDATE docs SET title = $2, summary = $3, folder = $4, project_id = $5, pinned = $6,
			version = $7, content = $8, file_id = $9, mime = $10, size = $11, editor = $12, search = $13, updated_at = $14
			WHERE id = $1`, d.ID, d.Title, d.Summary, d.Folder, d.ProjectID, d.Pinned, d.Version, d.Content, d.FileID,
			d.Mime, d.Size, d.Editor, d.search, d.UpdatedAt); err != nil {
			return err
		}
		if err := syncDocLinksTx(ctx, tx, d); err != nil {
			return err
		}
		return emit(ctx, tx, now, EventDocSaved, actor, "", "", docEvent(d))
	})
	d.search = ""
	return d, err
}

func (p *Postgres) RestoreDocVersion(ctx context.Context, id string, version int, actor string, now time.Time) (Doc, error) {
	if version < 1 {
		return Doc{}, ErrNotFound
	}
	var d Doc
	err := p.inTx(ctx, func(tx pgx.Tx) error {
		var err error
		d, err = scanDoc(tx.QueryRow(ctx, `SELECT `+docColumns+` FROM docs WHERE id = $1 AND deleted_at IS NULL FOR UPDATE`, id))
		if err != nil {
			return err
		}
		if version == d.Version {
			return nil
		}
		var v DocVersion
		err = tx.QueryRow(ctx, `SELECT content, file_id, mime, size, title FROM doc_versions WHERE doc_id = $1 AND version = $2`,
			id, version).Scan(&v.Content, &v.FileID, &v.Mime, &v.Size, &v.Title)
		if errors.Is(err, pgx.ErrNoRows) {
			return ErrNotFound
		}
		if err != nil {
			return err
		}
		d.Content, d.FileID, d.Mime, d.Size = v.Content, v.FileID, v.Mime, v.Size
		if v.Title != "" {
			d.Title = v.Title
		}
		if d.Kind != DocKindFile {
			d.Size = len(d.Content)
		}
		d.Version++
		d.Editor, d.UpdatedAt, d.search = actor, ts(now), docSearchText(d)
		if err := insertDocVersion(ctx, tx, newDocVersion(d, fmt.Sprintf("restaurado da v%d", version), actor, now)); err != nil {
			return err
		}
		if _, err := tx.Exec(ctx, `UPDATE docs SET title = $2, version = $3, content = $4, file_id = $5, mime = $6,
			size = $7, editor = $8, search = $9, updated_at = $10 WHERE id = $1`,
			d.ID, d.Title, d.Version, d.Content, d.FileID, d.Mime, d.Size, d.Editor, d.search, d.UpdatedAt); err != nil {
			return err
		}
		if err := syncDocLinksTx(ctx, tx, d); err != nil {
			return err
		}
		return emit(ctx, tx, now, EventDocSaved, actor, "", "", docEvent(d))
	})
	d.search = ""
	return d, err
}

func (p *Postgres) DocLinks(ctx context.Context, fromID string) ([]DocLink, error) {
	var exists bool
	if err := p.pool.QueryRow(ctx, `SELECT EXISTS (SELECT 1 FROM docs WHERE id = $1)`, fromID).Scan(&exists); err != nil {
		return nil, err
	}
	if !exists {
		return nil, ErrNotFound
	}
	rows, err := p.pool.Query(ctx, `SELECT from_id, to_title, to_id FROM doc_links WHERE from_id = $1 ORDER BY to_title`, fromID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []DocLink{}
	for rows.Next() {
		var l DocLink
		if err := rows.Scan(&l.FromID, &l.ToTitle, &l.ToID); err != nil {
			return nil, err
		}
		out = append(out, l)
	}
	return out, rows.Err()
}

func (p *Postgres) DocBacklinks(ctx context.Context, id string, scope *string) ([]DocBacklink, error) {
	var targetTitle string
	err := p.pool.QueryRow(ctx, `SELECT title FROM docs WHERE id = $1`, id).Scan(&targetTitle)
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, ErrNotFound
	}
	if err != nil {
		return nil, err
	}
	q := `SELECT d.id, d.title, d.author, d.updated_at, d.content, dl.to_title
		FROM doc_links dl
		JOIN docs d ON d.id = dl.from_id
		WHERE dl.to_id = $1 AND dl.from_id <> $1 AND d.deleted_at IS NULL`
	args := []any{id}
	if scope != nil {
		q += ` AND (d.project_id = '' OR d.project_id = $2)`
		args = append(args, *scope)
	}
	q += ` ORDER BY d.updated_at DESC, d.id`
	rows, err := p.pool.Query(ctx, q, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []DocBacklink{}
	seen := map[string]bool{}
	for rows.Next() {
		var b DocBacklink
		var content, toTitle string
		if err := rows.Scan(&b.ID, &b.Title, &b.Author, &b.UpdatedAt, &content, &toTitle); err != nil {
			return nil, err
		}
		if seen[b.ID] {
			continue
		}
		seen[b.ID] = true
		b.UpdatedAt = b.UpdatedAt.UTC()
		b.Snippet = backlinkSnippet(content, toTitle, targetTitle)
		out = append(out, b)
	}
	return out, rows.Err()
}

func (p *Postgres) ResolveDoc(ctx context.Context, title, folder string, scope *string) (Doc, error) {
	rows, err := p.pool.Query(ctx, `SELECT id, title, folder, project_id, updated_at FROM docs WHERE deleted_at IS NULL`)
	if err != nil {
		return Doc{}, err
	}
	live := []Doc{}
	for rows.Next() {
		var d Doc
		if err := rows.Scan(&d.ID, &d.Title, &d.Folder, &d.ProjectID, &d.UpdatedAt); err != nil {
			rows.Close()
			return Doc{}, err
		}
		d.UpdatedAt = d.UpdatedAt.UTC()
		live = append(live, d)
	}
	rows.Close()
	if err := rows.Err(); err != nil {
		return Doc{}, err
	}
	picked, ok := pickResolvedDoc(live, title, folder, "", scope, "")
	if !ok {
		return Doc{}, ErrNotFound
	}
	return p.GetDoc(ctx, picked.ID, 0)
}

func (p *Postgres) TrashDoc(ctx context.Context, id string, trash bool, actor string, now time.Time) (Doc, error) {
	var d Doc
	err := p.inTx(ctx, func(tx pgx.Tx) error {
		var err error
		q := `UPDATE docs SET deleted_at = $2 WHERE id = $1 AND deleted_at IS NULL RETURNING ` + docColumns
		var at any = ts(now)
		typ := EventDocTrashed
		if !trash {
			q = `UPDATE docs SET deleted_at = NULL WHERE id = $1 AND deleted_at IS NOT NULL AND $2::timestamptz IS NULL RETURNING ` + docColumns
			at, typ = nil, EventDocRestored
		}
		d, err = scanDoc(tx.QueryRow(ctx, q, id, at))
		if err != nil {
			return err
		}
		if err := syncDocLinksTx(ctx, tx, d); err != nil {
			return err
		}
		return emit(ctx, tx, now, typ, actor, "", "", docEvent(d))
	})
	d.search, d.Content = "", ""
	return d, err
}

func (p *Postgres) PurgeDocs(ctx context.Context, id string, before time.Time, actor string, now time.Time) (int, error) {
	n := 0
	err := p.inTx(ctx, func(tx pgx.Tx) error {
		q, args := `DELETE FROM docs WHERE deleted_at IS NOT NULL AND deleted_at < $1 RETURNING `+docColumns, []any{ts(before)}
		if id != "" {
			q, args = `DELETE FROM docs WHERE deleted_at IS NOT NULL AND id = $1 RETURNING `+docColumns, []any{id}
		}
		rows, err := tx.Query(ctx, q, args...)
		if err != nil {
			return err
		}
		gone := []Doc{}
		for rows.Next() {
			d, err := scanDoc(rows)
			if err != nil {
				rows.Close()
				return err
			}
			gone = append(gone, d)
		}
		rows.Close()
		if err := rows.Err(); err != nil {
			return err
		}
		for _, d := range gone {
			if err := emit(ctx, tx, now, EventDocDeleted, actor, "", "", docEvent(d)); err != nil {
				return err
			}
		}
		if len(gone) > 0 {
			live, err := liveDocHeadsTx(ctx, tx)
			if err != nil {
				return err
			}
			if err := rebindDocLinksTx(ctx, tx, live, ""); err != nil {
				return err
			}
		}
		n = len(gone)
		if id != "" && n == 0 {
			return ErrNotFound
		}
		return nil
	})
	return n, err
}
