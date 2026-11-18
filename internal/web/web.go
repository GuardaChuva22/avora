// Package web gives the agents their own eyes on the internet: a web search
// and a page reader, without delegating to Claude Code.
//
// Both only reach the public internet: every connection is checked against
// loopback, private, link-local and the tailnet's CGNAT range, so a page
// (or a redirect) can never point an agent at a service on the VPS or the
// tailnet. Bodies are capped, results are cached for a few minutes.
package web

import (
	"context"
	"errors"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/url"
	"strings"
	"sync"
	"syscall"
	"time"
)

const (
	// MaxBody is the most a page download reads.
	MaxBody = 3 << 20
	// cacheTTL keeps a page or a search for a short while: agents often read
	// the same thing twice in one turn.
	cacheTTL     = 10 * time.Minute
	cacheEntries = 256
	userAgent    = "Mozilla/5.0 (Windows NT 10.0; Win64; x64) AppleWebKit/537.36 (KHTML, like Gecko) Chrome/131.0 Safari/537.36 Avora/1.0"
)

// ErrBlocked is returned for addresses outside the public internet.
var ErrBlocked = errors.New("endereço fora da internet pública (rede local, VPS ou tailnet)")

var blockedNets = func() []*net.IPNet {
	out := []*net.IPNet{}
	for _, c := range []string{
		"0.0.0.0/8", "10.0.0.0/8", "100.64.0.0/10", "127.0.0.0/8", "169.254.0.0/16", "172.16.0.0/12",
		"192.0.0.0/24", "192.168.0.0/16", "198.18.0.0/15", "224.0.0.0/4", "240.0.0.0/4",
		"::/128", "::1/128", "fc00::/7", "fe80::/10", "ff00::/8", "64:ff9b::/96",
	} {
		_, n, _ := net.ParseCIDR(c)
		out = append(out, n)
	}
	return out
}()

// Public reports whether ip is on the public internet.
func Public(ip net.IP) bool {
	if ip4 := ip.To4(); ip4 != nil {
		ip = ip4
	}
	for _, n := range blockedNets {
		if n.Contains(ip) {
			return false
		}
	}
	return true
}

// Client fetches pages and searches. The zero value is not usable; use New.
type Client struct {
	http      *http.Client
	searchURL string // DuckDuckGo's HTML endpoint, replaceable in tests
	// Fallbacks when DuckDuckGo blocks the server (202): Yahoo, then Bing.
	// Empty in tests that replace searchURL.
	yahooURL, bingURL string
	braveKey          string
	allowAll          bool // tests only: reach local servers

	mu    sync.Mutex
	cache map[string]cached
}

type cached struct {
	at  time.Time
	val any
}

// Options configure a Client.
type Options struct {
	// BraveKey switches search to the Brave Search API (more stable than
	// the keyless default).
	BraveKey string
	// SearchURL overrides the keyless search endpoint (tests).
	SearchURL string
	// AllowPrivate lets the client reach private addresses (tests only).
	AllowPrivate bool
}

// New builds a Client.
func New(o Options) *Client {
	c := &Client{searchURL: o.SearchURL, braveKey: o.BraveKey, allowAll: o.AllowPrivate, cache: map[string]cached{}}
	if c.searchURL == "" {
		c.searchURL = "https://html.duckduckgo.com/html/"
		c.yahooURL = "https://search.yahoo.com/search"
		c.bingURL = "https://www.bing.com/search"
	}
	dialer := &net.Dialer{Timeout: 10 * time.Second, Control: func(_, address string, _ syscall.RawConn) error {
		if c.allowAll {
			return nil
		}
		host, _, err := net.SplitHostPort(address)
		if err != nil {
			return err
		}
		ip := net.ParseIP(host)
		if ip == nil || !Public(ip) {
			return ErrBlocked
		}
		return nil
	}}
	transport := &http.Transport{
		Proxy: nil, DialContext: dialer.DialContext, ForceAttemptHTTP2: true,
		TLSHandshakeTimeout: 10 * time.Second, ResponseHeaderTimeout: 20 * time.Second,
		MaxIdleConns: 20, IdleConnTimeout: 60 * time.Second,
	}
	c.http = &http.Client{Transport: transport, Timeout: 30 * time.Second, CheckRedirect: func(req *http.Request, via []*http.Request) error {
		if len(via) >= 5 {
			return errors.New("redirecionamentos demais")
		}
		if req.URL.Scheme != "http" && req.URL.Scheme != "https" {
			return errors.New("redirecionou para um esquema não suportado")
		}
		return nil
	}}
	return c
}

func (c *Client) cached(key string) (any, bool) {
	c.mu.Lock()
	defer c.mu.Unlock()
	e, ok := c.cache[key]
	if !ok || time.Since(e.at) > cacheTTL {
		return nil, false
	}
	return e.val, true
}

func (c *Client) remember(key string, val any) {
	c.mu.Lock()
	defer c.mu.Unlock()
	if len(c.cache) >= cacheEntries {
		for k, e := range c.cache {
			if time.Since(e.at) > cacheTTL || len(c.cache) >= cacheEntries {
				delete(c.cache, k)
			}
		}
	}
	c.cache[key] = cached{at: time.Now(), val: val}
}

// CheckURL validates a URL an agent asked for.
func CheckURL(raw string) (*url.URL, error) {
	raw = strings.TrimSpace(raw)
	if raw == "" {
		return nil, errors.New("diga a URL")
	}
	if !strings.Contains(raw, "://") {
		raw = "https://" + raw
	}
	u, err := url.Parse(raw)
	if err != nil || u.Host == "" {
		return nil, errors.New("URL inválida")
	}
	if u.Scheme != "http" && u.Scheme != "https" {
		return nil, errors.New("só http e https")
	}
	if u.User != nil {
		return nil, errors.New("URL com usuário e senha não é aceita")
	}
	host := strings.ToLower(u.Hostname())
	if host == "localhost" || strings.HasSuffix(host, ".localhost") || strings.HasSuffix(host, ".ts.net") ||
		strings.HasSuffix(host, ".internal") || strings.HasSuffix(host, ".local") {
		return nil, ErrBlocked
	}
	u.Fragment = ""
	return u, nil
}

// get downloads a URL, up to MaxBody.
func (c *Client) get(ctx context.Context, u string, accept string) (body []byte, contentType, final string, err error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, u, nil)
	if err != nil {
		return nil, "", "", err
	}
	req.Header.Set("User-Agent", userAgent)
	req.Header.Set("Accept", accept)
	req.Header.Set("Accept-Language", "pt-BR,pt;q=0.9,en;q=0.8")
	resp, err := c.http.Do(req)
	if err != nil {
		return nil, "", "", friendly(err)
	}
	defer resp.Body.Close()
	if resp.StatusCode >= 400 {
		return nil, "", "", fmt.Errorf("o site respondeu %d %s", resp.StatusCode, http.StatusText(resp.StatusCode))
	}
	body, err = io.ReadAll(io.LimitReader(resp.Body, MaxBody+1))
	if err != nil {
		return nil, "", "", friendly(err)
	}
	if len(body) > MaxBody {
		body = body[:MaxBody]
	}
	return body, resp.Header.Get("Content-Type"), resp.Request.URL.String(), nil
}

func friendly(err error) error {
	switch {
	case errors.Is(err, ErrBlocked):
		return ErrBlocked
	case errors.Is(err, context.DeadlineExceeded):
		return errors.New("o site demorou demais para responder")
	}
	var dns *net.DNSError
	if errors.As(err, &dns) {
		return errors.New("o domínio não existe ou não resolveu")
	}
	msg := err.Error()
	if strings.Contains(msg, ErrBlocked.Error()) {
		return ErrBlocked
	}
	return fmt.Errorf("não consegui acessar: %s", msg)
}
