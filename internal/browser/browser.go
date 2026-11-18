// Package browser drives the agents' browser on a node: a real Chrome (or
// Edge) with a profile of its own, through the DevTools protocol. The owner
// signs in to sites in that window once; the agents then browse with those
// sessions. The page is read as text with numbered elements (not pixels),
// and clicks and keys are real input events, so sites see a person.
package browser

import (
	"context"
	_ "embed"
	"encoding/json"
	"errors"
	"fmt"
	"net/url"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"sync"
	"time"

	"github.com/chromedp/cdproto/input"
	"github.com/chromedp/cdproto/page"
	"github.com/chromedp/cdproto/target"
	"github.com/chromedp/chromedp"
	"github.com/chromedp/chromedp/kb"
)

// Command is one browser action, as the server sends it.
type Command struct {
	Action string `json:"action"` // abrir, ler, clicar, digitar, rolar, tecla, voltar, print, abas, fechar
	URL    string `json:"url,omitempty"`
	Ref    int    `json:"ref,omitempty"`
	Text   string `json:"text,omitempty"`
	Enter  bool   `json:"enter,omitempty"`
	Down   bool   `json:"down,omitempty"`
	Times  int    `json:"times,omitempty"`
	Part   int    `json:"part,omitempty"`
	Tab    int    `json:"tab,omitempty"`
}

// Result is what an action gives back: the page as text (when the action
// changes or reads it) and a screenshot for the owner's live view.
type Result struct {
	OK    bool   `json:"ok"`
	Error string `json:"error,omitempty"`
	Text  string `json:"text,omitempty"`
	URL   string `json:"url,omitempty"`
	Title string `json:"title,omitempty"`
	Shot  []byte `json:"shot,omitempty"` // JPEG
	// Elements are the numbered elements of the read (ref n = index n-1).
	Elements []Element `json:"elements,omitempty"`
}

// Element is a numbered element of the last read, for the server's risk check.
type Element struct {
	Kind  string `json:"kind"`
	Label string `json:"label"`
}

// MaxText bounds one read of the page; longer pages come in parts.
const MaxText = 9000

// IdleClose closes the browser after this long without use (the profile,
// with its sign-ins, stays).
const IdleClose = 30 * time.Minute

// SnapshotJS reads a page: (SnapshotJS)(maxChars, part) returns the JSON of
// the text with numbered elements. The app's browser runs it too.
//
//go:embed snapshot.js
var SnapshotJS string

// Browser is one Chrome with its profile. Safe for concurrent use; actions
// run one at a time.
type Browser struct {
	Exe      string // Chrome or Edge; empty finds one
	Profile  string // the user data dir
	Headless bool

	mu      sync.Mutex
	alloc   context.CancelFunc
	browser context.Context
	stopB   context.CancelFunc
	tab     context.Context
	tabID   target.ID
	lastUse time.Time
}

// FindChrome returns an installed Chrome or Edge, or "".
func FindChrome() string {
	var paths []string
	switch runtime.GOOS {
	case "windows":
		for _, base := range []string{os.Getenv("ProgramFiles"), os.Getenv("ProgramFiles(x86)"), os.Getenv("LOCALAPPDATA")} {
			if base == "" {
				continue
			}
			paths = append(paths, filepath.Join(base, `Google\Chrome\Application\chrome.exe`), filepath.Join(base, `Microsoft\Edge\Application\msedge.exe`))
		}
	case "darwin":
		paths = []string{"/Applications/Google Chrome.app/Contents/MacOS/Google Chrome"}
	default:
		paths = []string{"/usr/bin/google-chrome", "/usr/bin/chromium", "/usr/bin/chromium-browser"}
	}
	for _, p := range paths {
		if st, err := os.Stat(p); err == nil && !st.IsDir() {
			return p
		}
	}
	return ""
}

func (b *Browser) start() error {
	if b.browser != nil && b.browser.Err() == nil {
		return nil
	}
	exe := b.Exe
	if exe == "" {
		exe = FindChrome()
	}
	if exe == "" {
		return errors.New("não achei o Chrome nem o Edge neste computador")
	}
	if err := os.MkdirAll(b.Profile, 0o700); err != nil {
		return err
	}
	opts := []chromedp.ExecAllocatorOption{
		chromedp.ExecPath(exe),
		chromedp.UserDataDir(b.Profile),
		chromedp.NoFirstRun,
		chromedp.NoDefaultBrowserCheck,
		chromedp.WindowSize(1280, 900),
		chromedp.Flag("disable-blink-features", "AutomationControlled"),
		chromedp.Flag("disable-background-timer-throttling", true),
		chromedp.Flag("disable-backgrounding-occluded-windows", true),
		chromedp.Flag("disable-renderer-backgrounding", true),
		chromedp.Flag("lang", "pt-BR"),
	}
	if b.Headless {
		opts = append(opts, chromedp.Headless)
	}
	actx, acancel := chromedp.NewExecAllocator(context.Background(), opts...)
	quiet := func(string, ...any) {} // newer Chrome events the protocol package does not know
	bctx, bcancel := chromedp.NewContext(actx, chromedp.WithErrorf(quiet), chromedp.WithLogf(quiet))
	// The first Run starts Chrome and attaches to its first tab.
	if err := chromedp.Run(bctx); err != nil {
		bcancel()
		acancel()
		return fmt.Errorf("não consegui abrir o navegador: %v", err)
	}
	b.alloc, b.browser, b.stopB = acancel, bctx, bcancel
	b.tab = bctx
	if c := chromedp.FromContext(bctx); c != nil && c.Target != nil {
		b.tabID = c.Target.TargetID
	}
	go b.idle(bctx)
	return nil
}

// idle closes the browser once it sits unused for IdleClose.
func (b *Browser) idle(ctx context.Context) {
	t := time.NewTicker(time.Minute)
	defer t.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-t.C:
			b.mu.Lock()
			if time.Since(b.lastUse) > IdleClose {
				b.closeLocked()
				b.mu.Unlock()
				return
			}
			b.mu.Unlock()
		}
	}
}

func (b *Browser) closeLocked() {
	if b.stopB != nil {
		b.stopB()
		b.alloc()
	}
	b.browser, b.tab, b.stopB, b.alloc = nil, nil, nil, nil
}

// Close shuts the browser down.
func (b *Browser) Close() {
	b.mu.Lock()
	defer b.mu.Unlock()
	b.closeLocked()
}

// Do runs one command.
func (b *Browser) Do(ctx context.Context, cmd Command) Result {
	b.mu.Lock()
	defer b.mu.Unlock()
	b.lastUse = time.Now()
	if cmd.Action == "fechar" {
		b.closeLocked()
		return Result{OK: true, Text: "Navegador fechado."}
	}
	if err := b.start(); err != nil {
		return Result{Error: err.Error()}
	}
	res, err := b.do(ctx, cmd)
	if err != nil && b.browser != nil && b.browser.Err() != nil {
		// The window was closed by hand: start again once.
		b.closeLocked()
		if err2 := b.start(); err2 == nil {
			res, err = b.do(ctx, cmd)
		}
	}
	if err != nil {
		res.Error = err.Error()
		res.OK = false
	} else {
		res.OK = true
	}
	b.decorate(ctx, &res)
	return res
}

// run runs actions on the current tab with a deadline.
func (b *Browser) run(ctx context.Context, d time.Duration, actions ...chromedp.Action) error {
	tctx, cancel := context.WithTimeout(b.tab, d)
	defer cancel()
	stop := context.AfterFunc(ctx, cancel)
	defer stop()
	return chromedp.Run(tctx, actions...)
}

func (b *Browser) eval(ctx context.Context, js string, out any) error {
	return b.run(ctx, 15*time.Second, chromedp.Evaluate(js, out))
}

// settle waits for the page to load after an action.
func (b *Browser) settle(ctx context.Context) {
	time.Sleep(350 * time.Millisecond)
	deadline := time.Now().Add(8 * time.Second)
	for time.Now().Before(deadline) && ctx.Err() == nil {
		var state string
		if err := b.eval(ctx, `document.readyState`, &state); err == nil && state == "complete" {
			break
		}
		time.Sleep(250 * time.Millisecond)
	}
	time.Sleep(400 * time.Millisecond)
}

func (b *Browser) do(ctx context.Context, cmd Command) (Result, error) {
	switch cmd.Action {
	case "abrir":
		u, err := cleanURL(cmd.URL)
		if err != nil {
			return Result{}, err
		}
		if err := b.run(ctx, 45*time.Second, chromedp.Navigate(u)); err != nil && !strings.Contains(err.Error(), "net::ERR_ABORTED") {
			return Result{}, fmt.Errorf("não abriu %s: %v", u, err)
		}
		b.settle(ctx)
		return b.read(ctx, 1)
	case "ler":
		return b.read(ctx, cmd.Part)
	case "clicar":
		return b.click(ctx, cmd.Ref)
	case "digitar":
		return b.typeText(ctx, cmd.Ref, cmd.Text, cmd.Enter)
	case "rolar":
		times := cmd.Times
		if times < 1 {
			times = 1
		}
		if times > 10 {
			times = 10
		}
		dy := 700.0
		if !cmd.Down {
			dy = -dy
		}
		for i := 0; i < times; i++ {
			// A wheel event, so feeds that load on scroll load more.
			if err := b.run(ctx, 10*time.Second, chromedp.ActionFunc(func(c context.Context) error {
				return input.DispatchMouseEvent(input.MouseWheel, 640, 450).WithDeltaX(0).WithDeltaY(dy).Do(c)
			})); err != nil {
				return Result{}, err
			}
			time.Sleep(450 * time.Millisecond)
		}
		b.settle(ctx)
		return b.read(ctx, 1)
	case "tecla":
		key, ok := keys[strings.ToLower(strings.TrimSpace(cmd.Text))]
		if !ok {
			return Result{}, errors.New("tecla não suportada (enter, esc, tab, pagedown, pageup, baixo, cima)")
		}
		if err := b.run(ctx, 10*time.Second, chromedp.KeyEvent(key)); err != nil {
			return Result{}, err
		}
		b.settle(ctx)
		return b.read(ctx, 1)
	case "voltar":
		if err := b.run(ctx, 20*time.Second, chromedp.NavigateBack()); err != nil {
			return Result{}, errors.New("não há página anterior")
		}
		b.settle(ctx)
		return b.read(ctx, 1)
	case "print":
		return Result{Text: "Print tirado."}, nil
	case "abas":
		return b.tabs(ctx, cmd.Tab)
	}
	return Result{}, fmt.Errorf("ação %q não existe", cmd.Action)
}

var keys = map[string]string{
	"enter": kb.Enter, "esc": kb.Escape, "escape": kb.Escape, "tab": kb.Tab,
	"pagedown": kb.PageDown, "pageup": kb.PageUp, "baixo": kb.ArrowDown, "cima": kb.ArrowUp,
	"down": kb.ArrowDown, "up": kb.ArrowUp, "end": kb.End, "home": kb.Home,
}

func cleanURL(raw string) (string, error) {
	raw = strings.TrimSpace(raw)
	if raw == "" {
		return "", errors.New("diga o endereço")
	}
	if !strings.Contains(raw, "://") {
		raw = "https://" + raw
	}
	u, err := url.Parse(raw)
	if err != nil || (u.Scheme != "http" && u.Scheme != "https") || u.Host == "" {
		return "", errors.New("endereço inválido (só http e https)")
	}
	return u.String(), nil
}

type snapshot struct {
	Text     string    `json:"text"`
	Parts    int       `json:"parts"`
	Elements []Element `json:"elements"`
}

// read numbers the page's elements and returns part of its text.
func (b *Browser) read(ctx context.Context, part int) (Result, error) {
	if part < 1 {
		part = 1
	}
	var raw string
	var err error
	for i := 0; i < 3; i++ { // a navigation may swap the page under us
		err = b.eval(ctx, fmt.Sprintf("(%s)(%d, %d)", SnapshotJS, MaxText, part), &raw)
		if err == nil {
			break
		}
		time.Sleep(600 * time.Millisecond)
	}
	if err != nil {
		return Result{}, fmt.Errorf("não consegui ler a página: %v", err)
	}
	var s snapshot
	if err := json.Unmarshal([]byte(raw), &s); err != nil {
		return Result{}, errors.New("leitura da página ilegível")
	}
	return Result{Text: s.Text, Elements: s.Elements}, nil
}

// point finds element ref, scrolls it into view and returns its center.
func (b *Browser) point(ctx context.Context, ref int) (x, y float64, kind string, err error) {
	var p struct {
		X, Y  float64
		Kind  string
		Error string
	}
	js := fmt.Sprintf(`(() => {
  const el = (window.__avoraRefs || [])[%d];
  if (!el || !el.isConnected) return {error: "o elemento %d não está mais na página; leia a página de novo"};
  const a = el.closest && el.closest("a[target]"); if (a) a.target = "_self";
  el.scrollIntoView({block: "center", inline: "center", behavior: "instant"});
  const r = el.getBoundingClientRect();
  const kind = el.type === "password" ? "password" : el.isContentEditable ? "editable" : el.tagName.toLowerCase();
  return {x: r.left + Math.min(r.width / 2, 40 + r.width / 4), y: r.top + r.height / 2, kind};
})()`, ref, ref)
	if err := b.eval(ctx, js, &p); err != nil {
		return 0, 0, "", err
	}
	if p.Error != "" {
		return 0, 0, "", errors.New(p.Error)
	}
	return p.X, p.Y, p.Kind, nil
}

func (b *Browser) click(ctx context.Context, ref int) (Result, error) {
	x, y, _, err := b.point(ctx, ref)
	if err != nil {
		return Result{}, err
	}
	before := b.pageTargets(ctx)
	if err := b.run(ctx, 10*time.Second, chromedp.MouseClickXY(x, y)); err != nil {
		return Result{}, err
	}
	b.settle(ctx)
	b.followNewTab(ctx, before)
	return b.read(ctx, 1)
}

func (b *Browser) typeText(ctx context.Context, ref int, text string, enter bool) (Result, error) {
	x, y, kind, err := b.point(ctx, ref)
	if err != nil {
		return Result{}, err
	}
	if kind == "password" {
		return Result{}, errors.New("senha é o dono quem digita: peça para ele entrar no site pela janela do navegador")
	}
	if err := b.run(ctx, 10*time.Second, chromedp.MouseClickXY(x, y)); err != nil {
		return Result{}, err
	}
	// Replace what the field had.
	_ = b.eval(ctx, fmt.Sprintf(`(() => { const el = (window.__avoraRefs || [])[%d]; if (!el) return false;
  if (el.select && (el.tagName === "INPUT" || el.tagName === "TEXTAREA")) el.select(); else document.execCommand("selectAll"); return true; })()`, ref), nil)
	if err := b.run(ctx, 20*time.Second, chromedp.ActionFunc(func(c context.Context) error {
		return input.InsertText(text).Do(c)
	})); err != nil {
		return Result{}, err
	}
	if enter {
		time.Sleep(200 * time.Millisecond)
		if err := b.run(ctx, 10*time.Second, chromedp.KeyEvent(kb.Enter)); err != nil {
			return Result{}, err
		}
		b.settle(ctx)
	} else {
		time.Sleep(500 * time.Millisecond) // suggestions show up
	}
	return b.read(ctx, 1)
}

// pageTargets lists the open tabs.
func (b *Browser) pageTargets(ctx context.Context) []*target.Info {
	tctx, cancel := context.WithTimeout(b.browser, 5*time.Second)
	defer cancel()
	infos, err := chromedp.Targets(tctx)
	if err != nil {
		return nil
	}
	var pages []*target.Info
	for _, t := range infos {
		if t.Type == "page" && !strings.HasPrefix(t.URL, "devtools://") {
			pages = append(pages, t)
		}
	}
	return pages
}

// followNewTab switches to a tab the last click opened.
func (b *Browser) followNewTab(ctx context.Context, before []*target.Info) {
	seen := map[target.ID]bool{}
	for _, t := range before {
		seen[t.TargetID] = true
	}
	for _, t := range b.pageTargets(ctx) {
		if !seen[t.TargetID] {
			b.attach(t.TargetID)
			b.settle(ctx)
			return
		}
	}
}

func (b *Browser) attach(id target.ID) {
	if id == b.tabID {
		return
	}
	tctx, _ := chromedp.NewContext(b.browser, chromedp.WithTargetID(id), chromedp.WithErrorf(func(string, ...any) {}))
	b.tab, b.tabID = tctx, id
	_ = chromedp.Run(tctx, chromedp.ActionFunc(func(c context.Context) error {
		return target.ActivateTarget(id).Do(c)
	}))
}

// tabs lists the tabs, or switches to tab n (1-based).
func (b *Browser) tabs(ctx context.Context, n int) (Result, error) {
	pages := b.pageTargets(ctx)
	if n > 0 {
		if n > len(pages) {
			return Result{}, fmt.Errorf("só há %d abas", len(pages))
		}
		b.attach(pages[n-1].TargetID)
		b.settle(ctx)
		return b.read(ctx, 1)
	}
	var sb strings.Builder
	for i, p := range pages {
		mark := " "
		if p.TargetID == b.tabID {
			mark = "*"
		}
		fmt.Fprintf(&sb, "%s%d. %s — %s\n", mark, i+1, p.Title, p.URL)
	}
	return Result{Text: "Abas (* = a atual):\n" + sb.String()}, nil
}

// decorate adds the page's address, title and a screenshot.
func (b *Browser) decorate(ctx context.Context, res *Result) {
	if b.tab == nil || b.tab.Err() != nil {
		return
	}
	var info struct{ URL, Title string }
	if err := b.eval(ctx, `({url: location.href, title: document.title})`, &info); err == nil {
		res.URL, res.Title = info.URL, info.Title
	}
	var shot []byte
	if err := b.run(ctx, 15*time.Second, chromedp.ActionFunc(func(c context.Context) error {
		var err error
		shot, err = page.CaptureScreenshot().WithFormat(page.CaptureScreenshotFormatJpeg).WithQuality(55).Do(c)
		return err
	})); err == nil {
		res.Shot = shot
	}
}
