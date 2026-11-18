package connect

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/url"
	"regexp"
	"strings"
	"time"
	"unicode"
)

// LinkedIn: the owner's profile and posts in their name. LinkedIn's open
// API stops there (no feed, no search, no messages), so that is all this
// does. The app is the owner's own LinkedIn app with the products "Sign In
// with LinkedIn using OpenID Connect" and "Share on LinkedIn"; it only
// accepts the exact redirect URI it registered, so the desktop listens on
// a fixed port. Tokens last 60 days and have no refresh for self-serve
// apps: after that the owner reconnects.
type LinkedIn struct {
	ClientID, ClientSecret string
	// API and Auth are for tests.
	API, Auth string
}

const LinkedInPort = 47821

var LinkedInRedirect = fmt.Sprintf("http://127.0.0.1:%d/callback", LinkedInPort)

var linkedinPost = regexp.MustCompile(`^urn:li:(share|ugcPost|activity):[0-9]{5,25}$`)

func (l *LinkedIn) api() string {
	if l.API != "" {
		return strings.TrimRight(l.API, "/")
	}
	return "https://api.linkedin.com"
}

func (l *LinkedIn) auth() string {
	if l.Auth != "" {
		return strings.TrimRight(l.Auth, "/")
	}
	return "https://www.linkedin.com"
}

func (l *LinkedIn) Info() Info {
	info := Info{Kind: "linkedin", Name: "LinkedIn", Auth: "oauth", Category: "Redes sociais", Apps: []string{"LinkedIn"},
		Ready:       l.ClientID != "" && l.ClientSecret != "",
		Description: "Seu perfil e publicar posts no seu nome (o LinkedIn não abre o feed nem as mensagens para apps).",
		Help:        "Clique em Conectar e entre no LinkedIn. A conexão vale 60 dias; depois é só reconectar.",
		HelpURL:     "https://www.linkedin.com/developers/apps",
		OAuthPort:   LinkedInPort}
	if !info.Ready {
		info.Help = "Falta o app do LinkedIn no servidor: em linkedin.com/developers/apps crie um app, adicione os produtos " +
			"\"Sign In with LinkedIn using OpenID Connect\" e \"Share on LinkedIn\", em Auth cadastre o redirect " + LinkedInRedirect +
			" e grave AVORA_LINKEDIN_CLIENT_ID e AVORA_LINKEDIN_CLIENT_SECRET com scripts/set-secret.sh."
	}
	return info
}

func (l *LinkedIn) BeginAuth(_ context.Context, redirect, state, _ string) (string, map[string]string, error) {
	if redirect != LinkedInRedirect {
		return "", nil, fmt.Errorf("o LinkedIn só aceita o redirect %s; atualize o app do Avora", LinkedInRedirect)
	}
	q := url.Values{
		"response_type": {"code"}, "client_id": {l.ClientID}, "redirect_uri": {redirect},
		"state": {state}, "scope": {"openid profile email w_member_social"},
	}
	return l.auth() + "/oauth/v2/authorization?" + q.Encode(), nil, nil
}

func (l *LinkedIn) FinishAuth(ctx context.Context, code, redirect, _ string, _ map[string]string, now time.Time) (Cred, error) {
	form := url.Values{"grant_type": {"authorization_code"}, "code": {code}, "redirect_uri": {redirect},
		"client_id": {l.ClientID}, "client_secret": {l.ClientSecret}}
	data, _, err := do(ctx, request{Method: "POST", URL: l.auth() + "/oauth/v2/accessToken", Body: []byte(form.Encode()),
		ContentType: "application/x-www-form-urlencoded"})
	if err != nil {
		return Cred{}, err
	}
	var t struct {
		AccessToken string `json:"access_token"`
		ExpiresIn   int    `json:"expires_in"`
	}
	if err := json.Unmarshal(data, &t); err != nil || t.AccessToken == "" {
		return Cred{}, errors.New("resposta inesperada do LinkedIn")
	}
	return Cred{Token: t.AccessToken, Expiry: now.Add(time.Duration(t.ExpiresIn) * time.Second)}, nil
}

// version is the LinkedIn-Version header: a month LinkedIn still serves
// (each one lives about a year), two months back to be safe.
func linkedinVersion(now time.Time) string {
	return now.AddDate(0, -2, 0).Format("200601")
}

func (l *LinkedIn) call(ctx context.Context, env *Env, method, path string, body any) ([]byte, map[string][]string, error) {
	c := env.Cred
	if c.Token == "" || (!c.Expiry.IsZero() && env.now().After(c.Expiry)) {
		return nil, nil, &AuthError{Msg: "a conexão do LinkedIn venceu (dura 60 dias); reconecte em Plugins"}
	}
	data, header, err := do(ctx, request{Method: method, URL: l.api() + path, Body: body, Header: map[string]string{
		"Authorization": "Bearer " + c.Token, "LinkedIn-Version": linkedinVersion(env.now()), "X-Restli-Protocol-Version": "2.0.0",
	}})
	return data, header, err
}

type linkedinMe struct {
	Sub   string `json:"sub"`
	Name  string `json:"name"`
	Email string `json:"email"`
}

func (l *LinkedIn) me(ctx context.Context, env *Env) (linkedinMe, error) {
	var me linkedinMe
	data, _, err := l.call(ctx, env, "GET", "/v2/userinfo", nil)
	if err != nil {
		return me, err
	}
	if err := json.Unmarshal(data, &me); err != nil || me.Sub == "" {
		return me, errors.New("resposta inesperada do LinkedIn")
	}
	return me, nil
}

func (l *LinkedIn) Check(ctx context.Context, env *Env) (string, error) {
	me, err := l.me(ctx, env)
	if err != nil {
		return "", err
	}
	if me.Email != "" {
		return me.Email, nil
	}
	return me.Name, nil
}

func (l *LinkedIn) Tools() []Tool {
	return []Tool{
		{Name: "linkedin_perfil", Risk: Read,
			Description: "O perfil do dono no LinkedIn (nome, e-mail) e até quando a conexão vale.",
			Params:      `{"type": "object", "properties": {}}`,
			Summary:     func(Args) string { return "perfil do LinkedIn" }},
		{Name: "linkedin_publicar", Risk: Send,
			Description: "Publica um post no LinkedIn em nome do dono. Mostre o texto final ao dono antes. Opcional: um link (vira o cartão do post).",
			Params: `{"type": "object", "properties": {
	"texto": {"type": "string", "description": "O post, até 3000 caracteres."},
	"link": {"type": "string", "description": "URL https para anexar."},
	"visibilidade": {"type": "string", "enum": ["publico", "conexoes"], "description": "Padrão: publico."}
}, "required": ["texto"]}`,
			Summary: func(a Args) string { return "post no LinkedIn" + quoted(a.Str("texto")) }},
		{Name: "linkedin_apagar_post", Risk: Delete,
			Description: "Apaga um post do dono pelo id (urn:li:share:... ou urn:li:ugcPost:...), o que linkedin_publicar devolveu.",
			Params:      `{"type": "object", "properties": {"id": {"type": "string"}}, "required": ["id"]}`,
			Summary:     func(a Args) string { return "apagar post do LinkedIn " + cut(a.Str("id"), 60) }},
	}
}

func (l *LinkedIn) Call(ctx context.Context, env *Env, tool string, a Args) (string, error) {
	switch tool {
	case "linkedin_perfil":
		me, err := l.me(ctx, env)
		if err != nil {
			return "", err
		}
		out := me.Name
		if me.Email != "" {
			out += " <" + me.Email + ">"
		}
		if !env.Cred.Expiry.IsZero() {
			out += "\nConexão vale até " + env.Cred.Expiry.In(env.zone()).Format("2006-01-02")
		}
		return out, nil

	case "linkedin_publicar":
		text, err := a.Need("texto")
		if err != nil {
			return "", err
		}
		if len([]rune(text)) > 3000 {
			return "", errors.New("o LinkedIn aceita até 3000 caracteres")
		}
		vis := "PUBLIC"
		if a.Str("visibilidade") == "conexoes" {
			vis = "CONNECTIONS"
		}
		me, err := l.me(ctx, env)
		if err != nil {
			return "", err
		}
		post := map[string]any{
			"author": "urn:li:person:" + me.Sub, "commentary": linkedinEscape(text), "visibility": vis,
			"distribution":   map[string]any{"feedDistribution": "MAIN_FEED", "targetEntities": []any{}, "thirdPartyDistributionChannels": []any{}},
			"lifecycleState": "PUBLISHED", "isReshareDisabledByAuthor": false,
		}
		if link := a.Str("link"); link != "" {
			if u, err := url.Parse(link); err != nil || u.Scheme != "https" || u.Host == "" {
				return "", errors.New("o link precisa ser https://...")
			}
			post["content"] = map[string]any{"article": map[string]any{"source": link, "title": cut(link, 200)}}
		}
		_, header, err := l.call(ctx, env, "POST", "/rest/posts", post)
		if err != nil {
			return "", err
		}
		id := ""
		for k, v := range header {
			if strings.EqualFold(k, "x-restli-id") && len(v) > 0 {
				id = v[0]
			}
		}
		if id == "" {
			return "Publicado no LinkedIn.", nil
		}
		return "Publicado no LinkedIn: https://www.linkedin.com/feed/update/" + id + "/ (id " + id + ")", nil

	case "linkedin_apagar_post":
		id := a.Str("id")
		if !linkedinPost.MatchString(id) {
			return "", errors.New("id inválido: use urn:li:share:... ou urn:li:ugcPost:...")
		}
		if _, _, err := l.call(ctx, env, "DELETE", "/rest/posts/"+url.PathEscape(id), nil); err != nil {
			return "", err
		}
		return "Post apagado.", nil
	}
	return "", fmt.Errorf("ferramenta desconhecida %s", tool)
}

// linkedinEscape protects the characters LinkedIn's "little text" format
// reads as markup, so the post reads exactly as written; #words stay
// hashtags.
func linkedinEscape(s string) string {
	var b strings.Builder
	rs := []rune(s)
	for i := 0; i < len(rs); i++ {
		r := rs[i]
		if r == '#' {
			j := i + 1
			for j < len(rs) && (unicode.IsLetter(rs[j]) || unicode.IsDigit(rs[j]) || rs[j] == '_') {
				j++
			}
			if j > i+1 && (i == 0 || unicode.IsSpace(rs[i-1])) {
				b.WriteString(`{hashtag|\#|` + strings.ReplaceAll(string(rs[i+1:j]), "_", `\_`) + `}`)
				i = j - 1
				continue
			}
		}
		if strings.ContainsRune(`\|{}@[]()<>#*_~`, r) {
			b.WriteRune('\\')
		}
		b.WriteRune(r)
	}
	return b.String()
}
