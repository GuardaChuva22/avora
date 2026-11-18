package web

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"net/url"
	"regexp"
	"strings"
	"unicode/utf8"

	"golang.org/x/net/html"
	"golang.org/x/net/html/atom"
	"golang.org/x/net/html/charset"
)

// Page is a page reduced to what a model reads: title, text, links.
type Page struct {
	URL   string
	Title string
	Text  string
	Links []Link
}

// Link is one link found on a page.
type Link struct {
	Text string
	URL  string
}

const maxLinks = 40

// Read downloads a page and extracts its readable text.
func (c *Client) Read(ctx context.Context, raw string) (Page, error) {
	u, err := CheckURL(raw)
	if err != nil {
		return Page{}, err
	}
	key := "page:" + u.String()
	if v, ok := c.cached(key); ok {
		return v.(Page), nil
	}
	body, ctype, final, err := c.get(ctx, u.String(), "text/html,application/xhtml+xml,text/plain,application/json;q=0.9,*/*;q=0.5")
	if err != nil {
		return Page{}, err
	}
	ctype = strings.ToLower(ctype)
	var p Page
	switch {
	case strings.Contains(ctype, "html") || ctype == "" && looksHTML(body):
		r, err := charset.NewReader(bytes.NewReader(body), ctype)
		if err != nil {
			r = bytes.NewReader(body)
		}
		doc, err := html.Parse(r)
		if err != nil {
			return Page{}, errors.New("não consegui ler o HTML da página")
		}
		p = extract(doc, final)
	case strings.Contains(ctype, "json"):
		var v any
		if json.Unmarshal(body, &v) == nil {
			pretty, _ := json.MarshalIndent(v, "", "  ")
			body = pretty
		}
		p = Page{Text: string(body)}
	case strings.HasPrefix(ctype, "text/") || strings.Contains(ctype, "xml"):
		p = Page{Text: string(body)}
	case strings.Contains(ctype, "pdf"):
		return Page{}, errors.New("é um PDF; ainda não leio PDF direto (delegue ao claude_code para ler)")
	default:
		return Page{}, errors.New("não é uma página de texto (" + strings.Split(ctype, ";")[0] + ")")
	}
	if !utf8.ValidString(p.Text) {
		p.Text = strings.ToValidUTF8(p.Text, "")
	}
	p.URL = final
	p.Text = strings.TrimSpace(p.Text)
	if p.Text == "" {
		return Page{}, errors.New("a página não tem texto legível (pode depender de JavaScript)")
	}
	c.remember(key, p)
	return p, nil
}

func looksHTML(b []byte) bool {
	head := strings.ToLower(string(b[:min(len(b), 512)]))
	return strings.Contains(head, "<html") || strings.Contains(head, "<!doctype html")
}

// Skipped subtrees: code, chrome and widgets, not content.
var skip = map[atom.Atom]bool{
	atom.Script: true, atom.Style: true, atom.Noscript: true, atom.Svg: true, atom.Template: true,
	atom.Iframe: true, atom.Nav: true, atom.Footer: true, atom.Form: true, atom.Button: true,
	atom.Select: true, atom.Head: true, atom.Canvas: true, atom.Dialog: true,
}

var block = map[atom.Atom]bool{
	atom.P: true, atom.Div: true, atom.Section: true, atom.Article: true, atom.Main: true, atom.Header: true,
	atom.Br: true, atom.Li: true, atom.Ul: true, atom.Ol: true, atom.Tr: true, atom.Table: true,
	atom.Blockquote: true, atom.Pre: true, atom.Dd: true, atom.Dt: true, atom.Figcaption: true, atom.Hr: true,
	atom.H1: true, atom.H2: true, atom.H3: true, atom.H4: true, atom.H5: true, atom.H6: true, atom.Aside: true,
}

var spaces = regexp.MustCompile(`[ \t\x{00a0}]+`)
var blankLines = regexp.MustCompile(`\n{3,}`)

// noise marks page chrome by class or id (menus, cookie banners, ads).
var noise = regexp.MustCompile(`(?i)(^|[\s_-])(cookie|consent|banner|advert|ads?|sidebar|menu|newsletter|popup|modal|share|social|breadcrumb|dropdown|portlet|toc|navbox|languages?|mw-editsection)([\s_-]|$)`)

func attr(n *html.Node, key string) string {
	for _, a := range n.Attr {
		if a.Key == key {
			return a.Val
		}
	}
	return ""
}

func extract(doc *html.Node, base string) Page {
	baseURL, _ := url.Parse(base)
	var p Page
	var b strings.Builder
	seen := map[string]bool{}
	// Prefer <main> or <article> when the page has one: less chrome.
	root := doc
	var find func(*html.Node)
	find = func(n *html.Node) {
		if root != doc {
			return
		}
		if n.Type == html.ElementNode && (n.DataAtom == atom.Main || n.DataAtom == atom.Article) {
			root = n
			return
		}
		for ch := n.FirstChild; ch != nil; ch = ch.NextSibling {
			find(ch)
		}
	}
	find(doc)
	var title func(*html.Node)
	title = func(n *html.Node) {
		if n.Type == html.ElementNode && n.DataAtom == atom.Title && n.FirstChild != nil && p.Title == "" {
			p.Title = strings.TrimSpace(n.FirstChild.Data)
		}
		for ch := n.FirstChild; ch != nil; ch = ch.NextSibling {
			title(ch)
		}
	}
	title(doc)

	var walk func(*html.Node, bool)
	walk = func(n *html.Node, pre bool) {
		switch n.Type {
		case html.TextNode:
			t := n.Data
			if !pre {
				t = spaces.ReplaceAllString(strings.ReplaceAll(t, "\n", " "), " ")
			}
			b.WriteString(t)
			return
		case html.ElementNode:
			if skip[n.DataAtom] || attr(n, "aria-hidden") == "true" || attr(n, "hidden") != "" {
				return
			}
			if noise.MatchString(attr(n, "class")+" "+attr(n, "id")) && n.DataAtom != atom.Body {
				return
			}
			if block[n.DataAtom] {
				b.WriteString("\n")
			}
			switch n.DataAtom {
			case atom.H1, atom.H2, atom.H3:
				b.WriteString("\n" + strings.Repeat("#", int(n.DataAtom.String()[1]-'0')) + " ")
			case atom.Li:
				b.WriteString("- ")
			case atom.Td, atom.Th:
				b.WriteString(" | ")
			case atom.Img:
				if alt := strings.TrimSpace(attr(n, "alt")); alt != "" && len(alt) < 120 {
					b.WriteString("[imagem: " + alt + "]")
				}
			case atom.A:
				href := strings.TrimSpace(attr(n, "href"))
				text := strings.TrimSpace(spaces.ReplaceAllString(textOf(n), " "))
				if href != "" && text != "" && len(p.Links) < maxLinks && !strings.HasPrefix(href, "#") &&
					!strings.HasPrefix(strings.ToLower(href), "javascript:") && baseURL != nil {
					if ref, err := baseURL.Parse(href); err == nil && (ref.Scheme == "http" || ref.Scheme == "https") {
						abs := ref.String()
						if !seen[abs] && len(text) <= 120 {
							seen[abs] = true
							p.Links = append(p.Links, Link{Text: text, URL: abs})
						}
					}
				}
			}
		}
		for ch := n.FirstChild; ch != nil; ch = ch.NextSibling {
			walk(ch, pre || n.DataAtom == atom.Pre)
		}
		if n.Type == html.ElementNode && block[n.DataAtom] {
			b.WriteString("\n")
		}
	}
	walk(root, false)
	lines := strings.Split(b.String(), "\n")
	for i, l := range lines {
		lines[i] = strings.TrimRight(strings.TrimLeft(l, " "), " ")
	}
	text := blankLines.ReplaceAllString(strings.Join(lines, "\n"), "\n\n")
	for strings.Contains(text, "\n\n- ") { // list items stay together
		text = strings.ReplaceAll(text, "\n\n- ", "\n- ")
	}
	p.Text = text
	return p
}

func textOf(n *html.Node) string {
	if n.Type == html.TextNode {
		return n.Data
	}
	var b strings.Builder
	for ch := n.FirstChild; ch != nil; ch = ch.NextSibling {
		b.WriteString(textOf(ch))
	}
	return b.String()
}

// HTMLText turns an HTML document (an e-mail body, say) into readable text.
func HTMLText(doc string) string {
	n, err := html.Parse(strings.NewReader(doc))
	if err != nil {
		return doc
	}
	return extract(n, "").Text
}
