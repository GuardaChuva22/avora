package connect

import (
	"context"
	"crypto/sha256"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"net/url"
	"strings"
	"time"
)

// Google: Gmail, Drive and Calendar with one OAuth consent. The OAuth client
// is a "Desktop app" client of the owner's own Google Cloud project; the
// desktop app catches the redirect on 127.0.0.1 and hands the code to the
// server, which keeps the refresh token in the vault.
type Google struct {
	ClientID     string
	ClientSecret string
	// API, when set, replaces every Google host (tests).
	API string
}

// GoogleScopes: mail (read, drafts, send), Drive (read everything, create
// files), Calendar events. Consent must grant all of these.
var GoogleScopes = []string{
	"https://www.googleapis.com/auth/gmail.modify",
	"https://www.googleapis.com/auth/drive.readonly",
	"https://www.googleapis.com/auth/drive.file",
	"https://www.googleapis.com/auth/calendar.events",
}

func (g *Google) url(host, path string) string {
	if g.API != "" {
		return strings.TrimRight(g.API, "/") + path
	}
	return host + path
}

const (
	gmailHost    = "https://gmail.googleapis.com"
	apisHost     = "https://www.googleapis.com"
	oauthHost    = "https://oauth2.googleapis.com"
	accountsHost = "https://accounts.google.com"
)

func (g *Google) Info() Info {
	info := Info{Kind: "google", Name: "Google", Auth: "oauth", Category: "Google", Apps: []string{"Gmail", "Drive", "Agenda", "Planilhas", "Tarefas", "Contatos", "YouTube"},
		Ready:       g.ClientID != "" && g.ClientSecret != "",
		Description: "Gmail, Drive, Agenda, Planilhas, Tarefas, Contatos e YouTube com um login só: e-mails, documentos, compromissos, planilhas, tarefas, contatos e o seu canal.",
		Help:        "Clique em Conectar: o navegador abre o login do Google. Como o app do Avora é seu e não é verificado pelo Google, clique em Avançado → Acessar e marque todas as permissões. No seu projeto do Google Cloud, ative também as APIs Google Sheets, Google Tasks, People e YouTube Data v3 (Biblioteca de APIs).",
		HelpURL:     "https://console.cloud.google.com/apis/credentials"}
	if !info.Ready {
		info.Help = "Falta o app OAuth do Google no servidor: crie um cliente OAuth do tipo App para computador no Google Cloud " +
			"(com as APIs Gmail, Drive, Calendar, Sheets, Tasks, People e YouTube Data v3 ativadas) e grave AVORA_GOOGLE_CLIENT_ID e AVORA_GOOGLE_CLIENT_SECRET com scripts/set-secret.sh."
	}
	return info
}

// PKCE returns a verifier and its S256 challenge.
func PKCE(verifier string) string {
	sum := sha256.Sum256([]byte(verifier))
	return base64.RawURLEncoding.EncodeToString(sum[:])
}

// BeginAuth and FinishAuth are the browser login the server drives.
func (g *Google) BeginAuth(_ context.Context, redirect, state, verifier string) (string, map[string]string, error) {
	return g.AuthURL(redirect, state, verifier), nil, nil
}

func (g *Google) FinishAuth(ctx context.Context, code, redirect, verifier string, _ map[string]string, now time.Time) (Cred, error) {
	return g.Exchange(ctx, code, redirect, verifier, now)
}

// AuthURL is where the owner consents.
func (g *Google) AuthURL(redirect, state, verifier string) string {
	q := url.Values{
		"client_id": {g.ClientID}, "redirect_uri": {redirect}, "response_type": {"code"},
		"scope": {strings.Join(append(append([]string{}, GoogleScopes...), googleExtraScopes...), " ")}, "access_type": {"offline"}, "prompt": {"select_account consent"},
		"include_granted_scopes": {"true"}, "state": {state},
		"code_challenge": {PKCE(verifier)}, "code_challenge_method": {"S256"},
	}
	return g.url(accountsHost, "/o/oauth2/v2/auth") + "?" + q.Encode()
}

type googleToken struct {
	AccessToken  string `json:"access_token"`
	RefreshToken string `json:"refresh_token"`
	ExpiresIn    int    `json:"expires_in"`
	Scope        string `json:"scope"`
}

func (g *Google) tokenCall(ctx context.Context, form url.Values) (googleToken, error) {
	form.Set("client_id", g.ClientID)
	form.Set("client_secret", g.ClientSecret)
	var t googleToken
	data, _, err := do(ctx, request{Method: "POST", URL: g.url(oauthHost, "/token"), Body: []byte(form.Encode()),
		ContentType: "application/x-www-form-urlencoded"})
	var api *APIError
	if errors.As(err, &api) && strings.Contains(api.Body, "invalid_grant") {
		return t, &AuthError{Msg: "o Google revogou o acesso do Avora; reconecte o Google em Plugins"}
	}
	if err != nil {
		return t, err
	}
	if err := json.Unmarshal(data, &t); err != nil || t.AccessToken == "" {
		return t, errors.New("resposta inesperada do Google ao renovar o acesso")
	}
	return t, nil
}

// Exchange turns the code from the redirect into a credential.
func (g *Google) Exchange(ctx context.Context, code, redirect, verifier string, now time.Time) (Cred, error) {
	t, err := g.tokenCall(ctx, url.Values{"grant_type": {"authorization_code"}, "code": {code},
		"redirect_uri": {redirect}, "code_verifier": {verifier}})
	if err != nil {
		return Cred{}, err
	}
	if t.RefreshToken == "" {
		return Cred{}, errors.New("o Google não devolveu acesso permanente; tente conectar de novo")
	}
	for _, s := range GoogleScopes {
		if !strings.Contains(t.Scope, s) {
			return Cred{}, fmt.Errorf("faltou permitir %s; conecte de novo e marque todas as caixas", strings.TrimPrefix(s, "https://www.googleapis.com/auth/"))
		}
	}
	return Cred{Token: t.AccessToken, Refresh: t.RefreshToken, Expiry: now.Add(time.Duration(t.ExpiresIn) * time.Second)}, nil
}

// access returns a valid access token, renewing it when it is about to end.
func (g *Google) access(ctx context.Context, env *Env) (string, error) {
	c := env.Cred
	if c.Token != "" && env.now().Add(2*time.Minute).Before(c.Expiry) {
		return c.Token, nil
	}
	if c.Refresh == "" {
		return "", errReconnect
	}
	t, err := g.tokenCall(ctx, url.Values{"grant_type": {"refresh_token"}, "refresh_token": {c.Refresh}})
	if err != nil {
		return "", err
	}
	c.Token, c.Expiry = t.AccessToken, env.now().Add(time.Duration(t.ExpiresIn)*time.Second)
	if t.RefreshToken != "" {
		c.Refresh = t.RefreshToken
	}
	env.Changed = true
	return c.Token, nil
}

// api calls a Google API as the owner.
func (g *Google) api(ctx context.Context, env *Env, req request, out any) error {
	tok, err := g.access(ctx, env)
	if err != nil {
		return err
	}
	if req.Header == nil {
		req.Header = map[string]string{}
	}
	req.Header["Authorization"] = "Bearer " + tok
	return callJSON(ctx, req, out)
}

func (g *Google) raw(ctx context.Context, env *Env, req request) ([]byte, error) {
	tok, err := g.access(ctx, env)
	if err != nil {
		return nil, err
	}
	if req.Header == nil {
		req.Header = map[string]string{}
	}
	req.Header["Authorization"] = "Bearer " + tok
	data, _, err := do(ctx, req)
	return data, err
}

func (g *Google) Check(ctx context.Context, env *Env) (string, error) {
	var p struct {
		Email string `json:"emailAddress"`
	}
	if err := g.api(ctx, env, request{URL: g.url(gmailHost, "/gmail/v1/users/me/profile")}, &p); err != nil {
		return "", err
	}
	return p.Email, nil
}

func (g *Google) Tools() []Tool {
	return append(append(append(g.gmailTools(), g.driveTools()...), g.calendarTools()...), g.moreTools()...)
}

func (g *Google) Call(ctx context.Context, env *Env, tool string, a Args) (string, error) {
	switch {
	case strings.HasPrefix(tool, "gmail_"):
		return g.gmail(ctx, env, tool, a)
	case strings.HasPrefix(tool, "drive_"):
		return g.drive(ctx, env, tool, a)
	case strings.HasPrefix(tool, "agenda_"):
		return g.calendar(ctx, env, tool, a)
	}
	return g.more(ctx, env, tool, a)
}
