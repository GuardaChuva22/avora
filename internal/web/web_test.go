package web

import (
	"context"
	"errors"
	"fmt"
	"net"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"golang.org/x/net/html"
)

func TestPublicBlocksInternalRanges(t *testing.T) {
	for _, ip := range []string{"127.0.0.1", "10.1.2.3", "192.168.0.10", "172.20.1.1", "100.124.207.108", "169.254.169.254", "::1", "fd00::1", "0.0.0.0"} {
		if Public(net.ParseIP(ip)) {
			t.Errorf("%s should be blocked", ip)
		}
	}
	for _, ip := range []string{"8.8.8.8", "142.250.79.46", "2606:4700::1111"} {
		if !Public(net.ParseIP(ip)) {
			t.Errorf("%s should be public", ip)
		}
	}
}

func TestCheckURL(t *testing.T) {
	if u, err := CheckURL("exemplo.com.br/a#x"); err != nil || u.String() != "https://exemplo.com.br/a" {
		t.Fatalf("got %v %v", u, err)
	}
	for _, bad := range []string{"", "ftp://x.com", "file:///etc/passwd", "http://localhost:3190", "https://pc.tail0000.ts.net", "http://u:p@x.com"} {
		if _, err := CheckURL(bad); err == nil {
			t.Errorf("%q should be refused", bad)
		}
	}
}

func TestReadRefusesPrivateAddresses(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { fmt.Fprint(w, "segredo") }))
	defer srv.Close()
	c := New(Options{})
	_, err := c.Read(context.Background(), srv.URL)
	if !errors.Is(err, ErrBlocked) {
		t.Fatalf("want ErrBlocked, got %v", err)
	}
}

const page = `<!doctype html><html><head><title>Notebook X — Loja</title><script>var a=1</script></head>
<body><nav>Menu Início Ofertas</nav><div class="cookie-banner">Aceite os cookies</div>
<main><h1>Notebook X</h1><p>Preço: <b>R$ 3.999</b> à vista.</p>
<ul><li>16 GB de RAM</li><li>SSD 512 GB</li></ul>
<table><tr><th>Loja</th><th>Preço</th></tr><tr><td>A</td><td>3.999</td></tr></table>
<a href="/specs">Ficha técnica</a> <a href="javascript:void(0)">x</a></main>
<footer>© Loja</footer></body></html>`

func TestReadExtractsTheContent(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/html; charset=utf-8")
		fmt.Fprint(w, page)
	}))
	defer srv.Close()
	c := New(Options{AllowPrivate: true})
	p, err := c.Read(context.Background(), srv.URL)
	if err != nil {
		t.Fatal(err)
	}
	if p.Title != "Notebook X — Loja" {
		t.Errorf("title %q", p.Title)
	}
	for _, want := range []string{"# Notebook X", "Preço: R$ 3.999 à vista.", "- 16 GB de RAM", "| Loja", "Ficha técnica"} {
		if !strings.Contains(p.Text, want) {
			t.Errorf("text misses %q:\n%s", want, p.Text)
		}
	}
	for _, bad := range []string{"var a=1", "Menu Início", "© Loja", "cookies"} {
		if strings.Contains(p.Text, bad) {
			t.Errorf("text keeps %q:\n%s", bad, p.Text)
		}
	}
	if len(p.Links) != 1 || p.Links[0].URL != srv.URL+"/specs" {
		t.Errorf("links %+v", p.Links)
	}
}

const duckPage = `<html><body>
<div class="result results_links result--ad"><a class="result__a" href="https://ads.example/x">Anúncio</a></div>
<div class="result results_links"><h2><a class="result__a" href="//duckduckgo.com/l/?uddg=https%3A%2F%2Fwww.kabum.com.br%2Fnotebook&rut=1">Notebook na <b>KaBuM</b></a></h2>
<a class="result__snippet">Os melhores   notebooks com desconto.</a></div>
<div class="result results_links"><a class="result__a" href="https://www.amazon.com.br/n">Amazon</a><div class="result__snippet">Notebooks</div></div>
</body></html>`

func TestSearchParsesDuckDuckGo(t *testing.T) {
	var got string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_ = r.ParseForm()
		got = r.Form.Get("q")
		fmt.Fprint(w, duckPage)
	}))
	defer srv.Close()
	c := New(Options{AllowPrivate: true, SearchURL: srv.URL})
	res, err := c.Search(context.Background(), "notebook barato", 5)
	if err != nil {
		t.Fatal(err)
	}
	if got != "notebook barato" {
		t.Errorf("query %q", got)
	}
	if len(res) != 2 || res[0].URL != "https://www.kabum.com.br/notebook" || res[0].Title != "Notebook na KaBuM" ||
		res[0].Snippet != "Os melhores notebooks com desconto." || res[1].URL != "https://www.amazon.com.br/n" {
		t.Fatalf("results %+v", res)
	}
}

func TestSearchFallsBackWhenDuckDuckGoBlocks(t *testing.T) {
	duck := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { w.WriteHeader(http.StatusAccepted) }))
	defer duck.Close()
	yahoo := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Query().Get("p") != "casa 4 quartos" {
			t.Errorf("yahoo query %q", r.URL.RawQuery)
		}
		fmt.Fprint(w, `<ol><li><div class="dd algo algo-sr"><div class="compTitle"><a href="https://r.search.yahoo.com/_ylt=x/RV=2/RE=1/RO=10/RU=https%3a%2f%2fimoveis.mitula.com.br%2fcasas-4-quartos/RK=2/RS=y-"><span>Mitula</span><h3 class="title"><span>Casas 4 quartos em S&atilde;o Sebasti&atilde;o</span></h3></a></div><div class="compText"><p> Ofertas de <b>v&aacute;rios</b> sites. </p></div></div></li>
<li><div class="dd algo"><div class="compTitle"><a href="https://search.yahoo.com/images/search?p=x"><h3>Images</h3></a></div></div></li></ol>`)
	}))
	defer yahoo.Close()
	c := New(Options{AllowPrivate: true, SearchURL: duck.URL})
	c.yahooURL = yahoo.URL
	res, err := c.Search(context.Background(), "casa 4 quartos", 5)
	if err != nil {
		t.Fatal(err)
	}
	if len(res) != 1 || res[0].URL != "https://imoveis.mitula.com.br/casas-4-quartos" || res[0].Title != "Casas 4 quartos em São Sebastião" || res[0].Snippet != "Ofertas de vários sites." {
		t.Fatalf("results %+v", res)
	}
}

func TestParseBing(t *testing.T) {
	doc, _ := html.Parse(strings.NewReader(`<ol><li class="b_algo"><div class="b_tpcn"><a class="tilk" href="x"><cite>imovelweb</cite></a></div><h2><a href="https://www.bing.com/ck/a?!&amp;p=1&amp;u=a1aHR0cHM6Ly93d3cuaW1vdmVsd2ViLmNvbS5ici9jYXNhcy5odG1s&amp;ntb=1">Casas <strong>para alugar</strong></a></h2><div class="b_caption"><p>Sobrado com 4 quartos.</p></div></li></ol>`))
	res := parseBing(doc, 5)
	if len(res) != 1 || res[0].URL != "https://www.imovelweb.com.br/casas.html" || res[0].Title != "Casas para alugar" || res[0].Snippet != "Sobrado com 4 quartos." {
		t.Fatalf("results %+v", res)
	}
}
