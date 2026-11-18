package mcp

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/url"
	"regexp"
	"strings"
	"syscall"
	"time"
)

// SafeClient refuses to reach loopback, private and link-local addresses,
// so a server's metadata cannot point Avora at the VPS's own services.
func SafeClient() *http.Client {
	dialer := &net.Dialer{Timeout: 15 * time.Second, Control: func(_, address string, _ syscall.RawConn) error {
		host, _, err := net.SplitHostPort(address)
		if err != nil {
			return err
		}
		ip := net.ParseIP(host)
		if ip == nil || ip.IsLoopback() || ip.IsPrivate() || ip.IsLinkLocalUnicast() || ip.IsLinkLocalMulticast() ||
			ip.IsUnspecified() || ip.IsMulticast() || (ip.To4() != nil && ip.To4()[0] == 100 && ip.To4()[1]&0xc0 == 64) {
			return fmt.Errorf("endereço %s não permitido", host)
		}
		return nil
	}}
	transport := &http.Transport{DialContext: dialer.DialContext, ForceAttemptHTTP2: true,
		TLSHandshakeTimeout: 15 * time.Second, ResponseHeaderTimeout: 60 * time.Second, IdleConnTimeout: 90 * time.Second}
	return &http.Client{Transport: transport, CheckRedirect: func(req *http.Request, via []*http.Request) error {
		if len(via) >= 5 || req.URL.Scheme != "https" {
			return errors.New("redirecionamento não permitido")
		}
		return nil
	}}
}

// Auth is how a server wants to be reached.
type Auth string

const (
	AuthNone  Auth = "none"  // open server
	AuthOAuth Auth = "oauth" // one-click login (dynamic client registration)
	AuthToken Auth = "token" // a key the owner pastes
)

// Metadata is what discovery found about a server's login.
type Metadata struct {
	Resource              string `json:"resource"`
	Scope                 string `json:"scope,omitempty"`
	Issuer                string `json:"issuer"`
	AuthorizationEndpoint string `json:"authorization_endpoint"`
	TokenEndpoint         string `json:"token_endpoint"`
	RegistrationEndpoint  string `json:"registration_endpoint,omitempty"`
}

var (
	resourceMetaRe = regexp.MustCompile(`resource_metadata="([^"]+)"`)
	scopeRe        = regexp.MustCompile(`scope="([^"]+)"`)
)

func getJSON(ctx context.Context, hc *http.Client, u string, out any) bool {
	req, err := http.NewRequestWithContext(ctx, "GET", u, nil)
	if err != nil {
		return false
	}
	req.Header.Set("Accept", "application/json")
	req.Header.Set("MCP-Protocol-Version", ProtocolVersion)
	resp, err := hc.Do(req)
	if err != nil {
		return false
	}
	defer resp.Body.Close()
	if resp.StatusCode != 200 {
		return false
	}
	return json.NewDecoder(io.LimitReader(resp.Body, 1<<20)).Decode(out) == nil
}

func origin(u *url.URL) string { return u.Scheme + "://" + u.Host }

// Discover asks the server how to log in: AuthNone when it answers without
// a token, AuthOAuth with the endpoints when it supports one-click login,
// AuthToken when it wants a token but offers no registration.
func Discover(ctx context.Context, hc *http.Client, serverURL string) (Auth, Metadata, error) {
	if hc == nil {
		hc = SafeClient()
	}
	su, err := url.Parse(serverURL)
	if err != nil || su.Host == "" {
		return "", Metadata{}, errors.New("endereço inválido")
	}
	c := &Client{URL: serverURL, HTTP: hc}
	_, err = c.Initialize(ctx, "")
	if err == nil {
		return AuthNone, Metadata{}, nil
	}
	var h *HTTPError
	if !errors.As(err, &h) || (h.Status != 401 && h.Status != 403) {
		return "", Metadata{}, fmt.Errorf("não é um servidor MCP que eu consiga usar (%v)", err)
	}
	if h.Challenged {
		return "", Metadata{}, fmt.Errorf("o endereço devolveu uma proteção de navegador (HTTP %d); use o endpoint do servidor MCP", h.Status)
	}
	md := Metadata{Resource: serverURL}
	if m := scopeRe.FindStringSubmatch(h.WWWAuth); m != nil {
		md.Scope = m[1]
	}
	var prm struct {
		Resource             string   `json:"resource"`
		AuthorizationServers []string `json:"authorization_servers"`
		ScopesSupported      []string `json:"scopes_supported"`
	}
	found := false
	if m := resourceMetaRe.FindStringSubmatch(h.WWWAuth); m != nil {
		found = getJSON(ctx, hc, m[1], &prm)
	}
	path := strings.TrimRight(su.Path, "/")
	for _, u := range []string{origin(su) + "/.well-known/oauth-protected-resource" + path, origin(su) + "/.well-known/oauth-protected-resource"} {
		if !found {
			found = getJSON(ctx, hc, u, &prm)
		}
	}
	if prm.Resource != "" {
		md.Resource = prm.Resource
	}
	if md.Scope == "" && len(prm.ScopesSupported) > 0 {
		md.Scope = strings.Join(prm.ScopesSupported, " ")
	}
	issuer := origin(su)
	if len(prm.AuthorizationServers) > 0 {
		issuer = prm.AuthorizationServers[0]
	}
	iu, err := url.Parse(issuer)
	if err != nil || iu.Host == "" {
		return "", Metadata{}, fmt.Errorf("servidor de login inválido %q", issuer)
	}
	ipath := strings.TrimRight(iu.Path, "/")
	var as struct {
		Issuer                string `json:"issuer"`
		AuthorizationEndpoint string `json:"authorization_endpoint"`
		TokenEndpoint         string `json:"token_endpoint"`
		RegistrationEndpoint  string `json:"registration_endpoint"`
	}
	ok := false
	for _, u := range []string{
		origin(iu) + "/.well-known/oauth-authorization-server" + ipath,
		origin(iu) + "/.well-known/openid-configuration" + ipath,
		strings.TrimRight(issuer, "/") + "/.well-known/openid-configuration",
		strings.TrimRight(issuer, "/") + "/.well-known/oauth-authorization-server",
	} {
		if ok = getJSON(ctx, hc, u, &as) && as.AuthorizationEndpoint != "" && as.TokenEndpoint != ""; ok {
			break
		}
	}
	if !ok {
		// The 2025-03 spec's defaults, for servers without metadata.
		base := origin(iu)
		as.AuthorizationEndpoint, as.TokenEndpoint, as.RegistrationEndpoint = base+"/authorize", base+"/token", base+"/register"
	}
	md.Issuer = orStr(as.Issuer, issuer)
	md.AuthorizationEndpoint, md.TokenEndpoint, md.RegistrationEndpoint = as.AuthorizationEndpoint, as.TokenEndpoint, as.RegistrationEndpoint
	for _, e := range []string{md.AuthorizationEndpoint, md.TokenEndpoint} {
		if !strings.HasPrefix(e, "https://") {
			return "", Metadata{}, fmt.Errorf("o login do servidor não usa https (%s)", e)
		}
	}
	if md.RegistrationEndpoint == "" || !strings.HasPrefix(md.RegistrationEndpoint, "https://") {
		return AuthToken, md, nil
	}
	return AuthOAuth, md, nil
}

func orStr(s, def string) string {
	if s != "" {
		return s
	}
	return def
}

// Client credentials from dynamic registration.
type Registration struct {
	ClientID     string `json:"client_id"`
	ClientSecret string `json:"client_secret,omitempty"`
}

// Register signs Avora up as a client of the server's login, for this
// redirect address.
func Register(ctx context.Context, hc *http.Client, md Metadata, redirect string) (Registration, error) {
	if hc == nil {
		hc = SafeClient()
	}
	body, _ := json.Marshal(map[string]any{
		"client_name": "Avora", "redirect_uris": []string{redirect},
		"grant_types": []string{"authorization_code", "refresh_token"}, "response_types": []string{"code"},
		"token_endpoint_auth_method": "none", "application_type": "native",
	})
	req, err := http.NewRequestWithContext(ctx, "POST", md.RegistrationEndpoint, bytes.NewReader(body))
	if err != nil {
		return Registration{}, err
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Accept", "application/json")
	resp, err := hc.Do(req)
	if err != nil {
		return Registration{}, err
	}
	defer resp.Body.Close()
	b, _ := io.ReadAll(io.LimitReader(resp.Body, 1<<20))
	if resp.StatusCode < 200 || resp.StatusCode > 299 {
		return Registration{}, fmt.Errorf("o app recusou o cadastro do Avora (HTTP %d): %s", resp.StatusCode, cut(string(b), 300))
	}
	var r Registration
	if err := json.Unmarshal(b, &r); err != nil || r.ClientID == "" {
		return Registration{}, errors.New("cadastro sem client_id")
	}
	return r, nil
}

// AuthURL is where the owner consents. challenge is the PKCE S256 of the
// verifier.
func AuthURL(md Metadata, clientID, redirect, state, challenge string) string {
	q := url.Values{
		"response_type": {"code"}, "client_id": {clientID}, "redirect_uri": {redirect}, "state": {state},
		"code_challenge": {challenge}, "code_challenge_method": {"S256"}, "resource": {md.Resource},
	}
	if md.Scope != "" {
		q.Set("scope", md.Scope)
	}
	sep := "?"
	if strings.Contains(md.AuthorizationEndpoint, "?") {
		sep = "&"
	}
	return md.AuthorizationEndpoint + sep + q.Encode()
}

// Token is what the login hands back.
type Token struct {
	Access  string
	Refresh string
	Expiry  time.Time // zero = no expiry given
}

// Grant asks the token endpoint for a token: an authorization code or a
// refresh.
func Grant(ctx context.Context, hc *http.Client, tokenEndpoint string, form url.Values, reg Registration, now time.Time) (Token, error) {
	if hc == nil {
		hc = SafeClient()
	}
	form.Set("client_id", reg.ClientID)
	if reg.ClientSecret != "" {
		form.Set("client_secret", reg.ClientSecret)
	}
	req, err := http.NewRequestWithContext(ctx, "POST", tokenEndpoint, strings.NewReader(form.Encode()))
	if err != nil {
		return Token{}, err
	}
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	req.Header.Set("Accept", "application/json")
	resp, err := hc.Do(req)
	if err != nil {
		return Token{}, err
	}
	defer resp.Body.Close()
	b, _ := io.ReadAll(io.LimitReader(resp.Body, 1<<20))
	var r struct {
		AccessToken  string      `json:"access_token"`
		RefreshToken string      `json:"refresh_token"`
		ExpiresIn    json.Number `json:"expires_in"`
		Error        string      `json:"error"`
		Description  string      `json:"error_description"`
	}
	_ = json.Unmarshal(b, &r)
	if resp.StatusCode < 200 || resp.StatusCode > 299 || r.AccessToken == "" {
		msg := orStr(r.Description, r.Error)
		if msg == "" {
			msg = fmt.Sprintf("HTTP %d", resp.StatusCode)
		}
		return Token{}, &GrantError{Code: r.Error, Msg: msg}
	}
	t := Token{Access: r.AccessToken, Refresh: r.RefreshToken}
	if n, err := r.ExpiresIn.Int64(); err == nil && n > 0 {
		t.Expiry = now.Add(time.Duration(n) * time.Second)
	}
	return t, nil
}

// GrantError is a token endpoint refusal; invalid_grant means the login is
// gone and the owner has to connect again.
type GrantError struct{ Code, Msg string }

func (e *GrantError) Error() string { return "o login recusou: " + e.Msg }

func cut(s string, n int) string {
	r := []rune(strings.TrimSpace(s))
	if len(r) <= n {
		return string(r)
	}
	return string(r[:n]) + "…"
}
