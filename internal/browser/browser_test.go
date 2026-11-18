package browser

import (
	"context"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"strings"
	"testing"
)

const testPage = `<!doctype html><html><head><title>Casas</title></head><body>
<h1>Casas para alugar</h1>
<p>Casa com <b>4 quartos</b> no Centro, R$ 2.000</p>
<a href="/casa/1">Ver casa 1</a>
<a href="/novo" target="_blank">Abrir em nova aba</a>
<input id="q" placeholder="Buscar cidade">
<input type="password" placeholder="Senha">
<button onclick="document.getElementById('out').textContent='busca: '+document.getElementById('q').value">Buscar</button>
<div id="out"></div>
<div style="display:none">escondido</div>
</body></html>`

// Needs a Chrome or Edge: AVORA_BROWSER_LIVE=1 go test ./internal/browser
func TestBrowserReadsClicksAndTypes(t *testing.T) {
	if os.Getenv("AVORA_BROWSER_LIVE") == "" || FindChrome() == "" {
		t.Skip("set AVORA_BROWSER_LIVE=1 with Chrome or Edge installed")
	}
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/casa/1":
			fmt.Fprint(w, `<title>Casa 1</title><p>Casa 1: 4 quartos, 2 vagas. Contato (16) 99999-0000</p>`)
		case "/novo":
			fmt.Fprint(w, `<title>Nova</title><p>Página nova</p>`)
		default:
			fmt.Fprint(w, testPage)
		}
	}))
	defer srv.Close()
	b := &Browser{Profile: t.TempDir(), Headless: true}
	defer b.Close()
	ctx := context.Background()
	r := b.Do(ctx, Command{Action: "abrir", URL: srv.URL})
	if !r.OK || !strings.Contains(r.Text, "4 quartos") || strings.Contains(r.Text, "escondido") || len(r.Shot) == 0 || r.Title != "Casas" {
		t.Fatalf("open: %+v\n%s", r.Error, r.Text)
	}
	ref := func(res Result, kind, label string) int {
		for i, e := range res.Elements {
			if e.Kind == kind && strings.Contains(e.Label, label) {
				return i + 1
			}
		}
		t.Fatalf("no %s %q in %s", kind, label, res.Text)
		return 0
	}
	t.Log(r.Text)
	if r2 := b.Do(ctx, Command{Action: "digitar", Ref: ref(r, "campo de senha", "Senha"), Text: "x"}); r2.OK || !strings.Contains(r2.Error, "senha") {
		t.Fatalf("password: %+v", r2.Error)
	}
	r = b.Do(ctx, Command{Action: "digitar", Ref: ref(r, "campo text", "Buscar cidade"), Text: "São Sebastião"})
	r = b.Do(ctx, Command{Action: "clicar", Ref: ref(r, "botão", "Buscar")})
	if !strings.Contains(r.Text, "busca: São Sebastião") {
		t.Fatalf("type+click: %s %s", r.Error, r.Text)
	}
	r = b.Do(ctx, Command{Action: "clicar", Ref: ref(r, "link", "Ver casa 1")})
	if !strings.Contains(r.Text, "(16) 99999-0000") || !strings.HasSuffix(r.URL, "/casa/1") {
		t.Fatalf("link: %s %s", r.Error, r.Text)
	}
	r = b.Do(ctx, Command{Action: "voltar"})
	r = b.Do(ctx, Command{Action: "clicar", Ref: ref(r, "link", "nova aba")})
	if !strings.Contains(r.Text, "Página nova") {
		t.Fatalf("new tab: %s %s", r.Error, r.Text)
	}
	if r = b.Do(ctx, Command{Action: "abas"}); !strings.Contains(r.Text, "Nova") {
		t.Fatalf("tabs: %s", r.Text)
	}
}
