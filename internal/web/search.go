package web

import (
	"bytes"
	"context"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strings"

	"golang.org/x/net/html"
	"golang.org/x/net/html/atom"
)

// Result is one search hit.
type Result struct {
	Title   string
	URL     string
	Snippet string
}

// MaxResults bounds one search.
const MaxResults = 10

// Search runs a web search: the Brave Search API when a key is set, then
// the keyless HTML results of DuckDuckGo, Yahoo and Bing.
func (c *Client) Search(ctx context.Context, query string, n int) ([]Result, error) {
	query = strings.TrimSpace(query)
	if query == "" {
		return nil, errors.New("diga o que buscar")
	}
	if len(query) > 400 {
		return nil, errors.New("busca longa demais (máx. 400 caracteres)")
	}
	if n <= 0 || n > MaxResults {
		n = 8
	}
	key := fmt.Sprintf("search:%d:%s", n, strings.ToLower(query))
	if v, ok := c.cached(key); ok {
		return v.([]Result), nil
	}
	// Each engine in turn until one answers with results: the keyless ones
	// block server IPs now and then (DuckDuckGo answers 202).
	engines := []func(context.Context, string, int) ([]Result, error){c.duck}
	if c.braveKey != "" {
		engines = append([]func(context.Context, string, int) ([]Result, error){c.brave}, engines...)
	}
	if c.yahooURL != "" {
		engines = append(engines, c.yahoo)
	}
	if c.bingURL != "" {
		engines = append(engines, c.bing)
	}
	var out []Result
	var err error
	for _, engine := range engines {
		out, err = engine(ctx, query, n)
		if err == nil && len(out) > 0 {
			break
		}
		if ctx.Err() != nil {
			break
		}
	}
	if err != nil {
		return nil, err
	}
	c.remember(key, out)
	return out, nil
}

// getPage fetches a search results page as a browser would.
func (c *Client) getPage(ctx context.Context, u string) (*html.Node, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, u, nil)
	if err != nil {
		return nil, err
	}
	req.Header.Set("User-Agent", userAgent)
	req.Header.Set("Accept-Language", "pt-BR,pt;q=0.9")
	resp, err := c.http.Do(req)
	if err != nil {
		return nil, friendly(err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("a busca respondeu %d; tente de novo em instantes", resp.StatusCode)
	}
	doc, err := html.Parse(io.LimitReader(resp.Body, MaxBody))
	if err != nil {
		return nil, errors.New("não consegui ler os resultados da busca")
	}
	return doc, nil
}

func (c *Client) yahoo(ctx context.Context, query string, n int) ([]Result, error) {
	doc, err := c.getPage(ctx, c.yahooURL+"?"+url.Values{"p": {query}}.Encode())
	if err != nil {
		return nil, err
	}
	return parseYahoo(doc, n), nil
}

func (c *Client) bing(ctx context.Context, query string, n int) ([]Result, error) {
	doc, err := c.getPage(ctx, c.bingURL+"?"+url.Values{"q": {query}, "setlang": {"pt-br"}, "cc": {"BR"}}.Encode())
	if err != nil {
		return nil, err
	}
	return parseBing(doc, n), nil
}

// collect walks doc for result blocks and reads each with read.
func collect(doc *html.Node, n int, isBlock func(*html.Node) bool, read func(*html.Node) Result) []Result {
	out := []Result{}
	var walk func(*html.Node)
	walk = func(node *html.Node) {
		if len(out) >= n {
			return
		}
		if node.Type == html.ElementNode && isBlock(node) {
			if r := read(node); r.Title != "" && r.URL != "" {
				out = append(out, r)
			}
			return
		}
		for ch := node.FirstChild; ch != nil; ch = ch.NextSibling {
			walk(ch)
		}
	}
	walk(doc)
	return out
}

// find returns the first element under n that matches.
func find(n *html.Node, match func(*html.Node) bool) *html.Node {
	for ch := n.FirstChild; ch != nil; ch = ch.NextSibling {
		if ch.Type == html.ElementNode && match(ch) {
			return ch
		}
		if f := find(ch, match); f != nil {
			return f
		}
	}
	return nil
}

func clean(n *html.Node) string {
	if n == nil {
		return ""
	}
	return strings.TrimSpace(spaces.ReplaceAllString(textOf(n), " "))
}

// parseYahoo reads Yahoo's results page (organic results are div.algo).
func parseYahoo(doc *html.Node, n int) []Result {
	return collect(doc, n, func(x *html.Node) bool { return x.DataAtom == atom.Div && hasClass(x, "algo") }, func(x *html.Node) Result {
		var r Result
		title := find(x, func(y *html.Node) bool { return y.DataAtom == atom.H3 })
		if a := find(x, func(y *html.Node) bool { return y.DataAtom == atom.A }); a != nil {
			r.URL = yahooTarget(attr(a, "href"))
		}
		r.Title = clean(title)
		r.Snippet = clean(find(x, func(y *html.Node) bool { return hasClass(y, "compText") }))
		return r
	})
}

// yahooTarget unwraps Yahoo's redirect (".../RU=<url>/RK=...").
func yahooTarget(href string) string {
	if i := strings.Index(href, "/RU="); i >= 0 {
		rest := href[i+4:]
		if j := strings.Index(rest, "/R"); j >= 0 {
			rest = rest[:j]
		}
		if t, err := url.QueryUnescape(rest); err == nil {
			href = t
		}
	}
	if u, err := url.Parse(href); err == nil && (u.Scheme == "http" || u.Scheme == "https") && !strings.HasSuffix(u.Host, "yahoo.com") {
		return u.String()
	}
	return ""
}

// parseBing reads Bing's results page (organic results are li.b_algo).
func parseBing(doc *html.Node, n int) []Result {
	return collect(doc, n, func(x *html.Node) bool { return x.DataAtom == atom.Li && hasClass(x, "b_algo") }, func(x *html.Node) Result {
		var r Result
		if h2 := find(x, func(y *html.Node) bool { return y.DataAtom == atom.H2 }); h2 != nil {
			r.Title = clean(h2)
			if a := find(h2, func(y *html.Node) bool { return y.DataAtom == atom.A }); a != nil {
				r.URL = bingTarget(attr(a, "href"))
			}
		}
		r.Snippet = clean(find(x, func(y *html.Node) bool { return hasClass(y, "b_caption") }))
		return r
	})
}

// bingTarget unwraps Bing's redirect (u=a1<base64url of the target>).
func bingTarget(href string) string {
	u, err := url.Parse(href)
	if err != nil {
		return ""
	}
	if t := u.Query().Get("u"); strings.HasPrefix(u.Host, "www.bing.com") && strings.HasPrefix(t, "a1") {
		b, err := base64.RawURLEncoding.DecodeString(strings.TrimRight(t[2:], "="))
		if err != nil {
			return ""
		}
		u, err = url.Parse(string(b))
		if err != nil {
			return ""
		}
	}
	if u.Scheme == "http" || u.Scheme == "https" {
		return u.String()
	}
	return ""
}

func (c *Client) duck(ctx context.Context, query string, n int) ([]Result, error) {
	form := url.Values{"q": {query}, "kl": {"br-pt"}}
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, c.searchURL, strings.NewReader(form.Encode()))
	if err != nil {
		return nil, err
	}
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	req.Header.Set("User-Agent", userAgent)
	req.Header.Set("Accept-Language", "pt-BR,pt;q=0.9")
	resp, err := c.http.Do(req)
	if err != nil {
		return nil, friendly(err)
	}
	defer resp.Body.Close()
	body, err := io.ReadAll(io.LimitReader(resp.Body, MaxBody))
	if err != nil {
		return nil, friendly(err)
	}
	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("a busca respondeu %d; tente de novo em instantes", resp.StatusCode)
	}
	doc, err := html.Parse(bytes.NewReader(body))
	if err != nil {
		return nil, errors.New("não consegui ler os resultados da busca")
	}
	out := parseDuck(doc, n)
	if len(out) == 0 && bytes.Contains(body, []byte("anomaly")) {
		return nil, errors.New("o buscador pediu verificação anti-robô; tente de novo em alguns minutos")
	}
	return out, nil
}

func hasClass(n *html.Node, class string) bool {
	for _, f := range strings.Fields(attr(n, "class")) {
		if f == class {
			return true
		}
	}
	return false
}

// parseDuck reads DuckDuckGo's HTML results page.
func parseDuck(doc *html.Node, n int) []Result {
	out := []Result{}
	var walk func(*html.Node)
	walk = func(node *html.Node) {
		if len(out) >= n {
			return
		}
		if node.Type == html.ElementNode && node.DataAtom == atom.Div && hasClass(node, "result") && !hasClass(node, "result--ad") {
			var r Result
			var inner func(*html.Node)
			inner = func(x *html.Node) {
				if x.Type == html.ElementNode {
					if x.DataAtom == atom.A && hasClass(x, "result__a") {
						r.Title = strings.TrimSpace(textOf(x))
						r.URL = duckTarget(attr(x, "href"))
					}
					if hasClass(x, "result__snippet") {
						r.Snippet = strings.TrimSpace(spaces.ReplaceAllString(textOf(x), " "))
					}
				}
				for ch := x.FirstChild; ch != nil; ch = ch.NextSibling {
					inner(ch)
				}
			}
			inner(node)
			if r.Title != "" && r.URL != "" {
				out = append(out, r)
			}
			return
		}
		for ch := node.FirstChild; ch != nil; ch = ch.NextSibling {
			walk(ch)
		}
	}
	walk(doc)
	return out
}

// duckTarget unwraps DuckDuckGo's redirect link to the real URL.
func duckTarget(href string) string {
	if strings.HasPrefix(href, "//") {
		href = "https:" + href
	}
	u, err := url.Parse(href)
	if err != nil {
		return ""
	}
	if t := u.Query().Get("uddg"); t != "" {
		return t
	}
	if u.Scheme == "http" || u.Scheme == "https" {
		return u.String()
	}
	return ""
}

func (c *Client) brave(ctx context.Context, query string, n int) ([]Result, error) {
	q := url.Values{"q": {query}, "count": {fmt.Sprint(n)}, "country": {"BR"}, "search_lang": {"pt-br"}}
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, "https://api.search.brave.com/res/v1/web/search?"+q.Encode(), nil)
	if err != nil {
		return nil, err
	}
	req.Header.Set("Accept", "application/json")
	req.Header.Set("X-Subscription-Token", c.braveKey)
	resp, err := c.http.Do(req)
	if err != nil {
		return nil, friendly(err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("a busca respondeu %d", resp.StatusCode)
	}
	var data struct {
		Web struct {
			Results []struct {
				Title       string `json:"title"`
				URL         string `json:"url"`
				Description string `json:"description"`
			} `json:"results"`
		} `json:"web"`
	}
	if err := json.NewDecoder(io.LimitReader(resp.Body, MaxBody)).Decode(&data); err != nil {
		return nil, errors.New("resposta da busca ilegível")
	}
	out := []Result{}
	for _, r := range data.Web.Results {
		if len(out) >= n {
			break
		}
		out = append(out, Result{Title: r.Title, URL: r.URL, Snippet: stripTags(r.Description)})
	}
	return out, nil
}

func stripTags(s string) string {
	doc, err := html.Parse(strings.NewReader(s))
	if err != nil {
		return s
	}
	return strings.TrimSpace(textOf(doc))
}
