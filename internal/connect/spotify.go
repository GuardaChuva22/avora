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
)

// Spotify: what is playing, search, playback, the queue and playlists, with
// the owner's login. The app is the owner's own Spotify app (a public
// client: PKCE, no secret), its id in AVORA_SPOTIFY_CLIENT_ID, with the
// redirect URI http://127.0.0.1/callback (Spotify takes any port on
// loopback). Controlling playback needs Spotify Premium.
type Spotify struct {
	ClientID string
	// API and Accounts are for tests.
	API      string
	Accounts string
}

var SpotifyScopes = []string{
	"user-read-playback-state", "user-modify-playback-state", "user-read-currently-playing",
	"user-read-recently-played", "user-top-read", "user-read-private", "user-read-email",
	"playlist-read-private", "playlist-modify-private", "playlist-modify-public",
	"user-library-read", "user-library-modify",
}

var spotifyURI = regexp.MustCompile(`^spotify:(track|album|playlist|artist|episode|show):[A-Za-z0-9]{10,40}$`)

func (s *Spotify) api() string {
	if s.API != "" {
		return strings.TrimRight(s.API, "/")
	}
	return "https://api.spotify.com"
}

func (s *Spotify) accounts() string {
	if s.Accounts != "" {
		return strings.TrimRight(s.Accounts, "/")
	}
	return "https://accounts.spotify.com"
}

func (s *Spotify) Info() Info {
	info := Info{Kind: "spotify", Name: "Spotify", Auth: "oauth", Category: "Música e mídia", Apps: []string{"Spotify"},
		Ready:       s.ClientID != "",
		Description: "O que está tocando, buscar músicas, tocar, pausar, pular, fila e playlists.",
		Help:        "Clique em Conectar: o navegador abre o login do Spotify. Tocar, pausar e pular pedem o Premium.",
		HelpURL:     "https://developer.spotify.com/dashboard"}
	if !info.Ready {
		info.Help = "Falta o app do Spotify no servidor: em developer.spotify.com/dashboard crie um app com a Web API, redirect URI " +
			"http://127.0.0.1/callback, e grave o Client ID em AVORA_SPOTIFY_CLIENT_ID com scripts/set-secret.sh."
	}
	return info
}

func (s *Spotify) BeginAuth(_ context.Context, redirect, state, verifier string) (string, map[string]string, error) {
	q := url.Values{
		"client_id": {s.ClientID}, "response_type": {"code"}, "redirect_uri": {redirect},
		"scope": {strings.Join(SpotifyScopes, " ")}, "state": {state},
		"code_challenge_method": {"S256"}, "code_challenge": {PKCE(verifier)},
	}
	return s.accounts() + "/authorize?" + q.Encode(), nil, nil
}

type spotifyToken struct {
	AccessToken  string `json:"access_token"`
	RefreshToken string `json:"refresh_token"`
	ExpiresIn    int    `json:"expires_in"`
}

func (s *Spotify) token(ctx context.Context, form url.Values) (spotifyToken, error) {
	form.Set("client_id", s.ClientID)
	var t spotifyToken
	data, _, err := do(ctx, request{Method: "POST", URL: s.accounts() + "/api/token", Body: []byte(form.Encode()),
		ContentType: "application/x-www-form-urlencoded"})
	var api *APIError
	if errors.As(err, &api) && strings.Contains(api.Body, "invalid_grant") {
		return t, &AuthError{Msg: "o Spotify revogou o acesso do Avora; reconecte em Plugins"}
	}
	if err != nil {
		return t, err
	}
	if err := json.Unmarshal(data, &t); err != nil || t.AccessToken == "" {
		return t, errors.New("resposta inesperada do Spotify")
	}
	return t, nil
}

func (s *Spotify) FinishAuth(ctx context.Context, code, redirect, verifier string, _ map[string]string, now time.Time) (Cred, error) {
	t, err := s.token(ctx, url.Values{"grant_type": {"authorization_code"}, "code": {code}, "redirect_uri": {redirect}, "code_verifier": {verifier}})
	if err != nil {
		return Cred{}, err
	}
	if t.RefreshToken == "" {
		return Cred{}, errors.New("o Spotify não devolveu acesso permanente; tente de novo")
	}
	return Cred{Token: t.AccessToken, Refresh: t.RefreshToken, Expiry: now.Add(time.Duration(t.ExpiresIn) * time.Second)}, nil
}

func (s *Spotify) access(ctx context.Context, env *Env) (string, error) {
	c := env.Cred
	if c.Token != "" && env.now().Add(2*time.Minute).Before(c.Expiry) {
		return c.Token, nil
	}
	if c.Refresh == "" {
		return "", errReconnect
	}
	t, err := s.token(ctx, url.Values{"grant_type": {"refresh_token"}, "refresh_token": {c.Refresh}})
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

// call talks to the Web API; out may be nil. A 204 (nothing playing) is
// an empty body.
func (s *Spotify) call(ctx context.Context, env *Env, method, path string, body any, out any) ([]byte, error) {
	tok, err := s.access(ctx, env)
	if err != nil {
		return nil, err
	}
	data, _, err := do(ctx, request{Method: method, URL: s.api() + path, Body: body, Header: map[string]string{"Authorization": "Bearer " + tok}})
	var api *APIError
	if errors.As(err, &api) && api.Status == 403 && strings.Contains(api.Body, "PREMIUM") {
		return nil, errors.New("controlar o player pede o Spotify Premium")
	}
	if errors.As(err, &api) && api.Status == 404 && strings.Contains(api.Body, "NO_ACTIVE_DEVICE") {
		return nil, errors.New("nenhum aparelho com o Spotify aberto; abra o Spotify no PC ou no celular e tente de novo")
	}
	if err != nil {
		return nil, err
	}
	if out != nil && len(data) > 0 {
		if err := json.Unmarshal(data, out); err != nil {
			return nil, fmt.Errorf("resposta inesperada do Spotify: %v", err)
		}
	}
	return data, nil
}

func (s *Spotify) Check(ctx context.Context, env *Env) (string, error) {
	var me struct {
		Name    string `json:"display_name"`
		Email   string `json:"email"`
		Product string `json:"product"`
	}
	if _, err := s.call(ctx, env, "GET", "/v1/me", nil, &me); err != nil {
		return "", err
	}
	if me.Email != "" {
		return me.Email, nil
	}
	return me.Name, nil
}

func (s *Spotify) Tools() []Tool {
	return []Tool{
		{Name: "spotify_tocando", Risk: Read,
			Description: "O que está tocando agora no Spotify do dono: música, artista, álbum, aparelho e se está pausado.",
			Params:      `{"type": "object", "properties": {}}`,
			Summary:     func(Args) string { return "o que está tocando" }},
		{Name: "spotify_buscar", Risk: Read,
			Description: "Busca no Spotify. Devolve nomes e URIs (spotify:track:...) para tocar, pôr na fila ou numa playlist.",
			Params: `{"type": "object", "properties": {
	"busca": {"type": "string"},
	"tipo": {"type": "string", "enum": ["track", "artist", "album", "playlist"], "description": "Padrão: track."},
	"limite": {"type": "integer", "description": "Até 20 (padrão 8)."}
}, "required": ["busca"]}`,
			Summary: func(a Args) string { return "buscou no Spotify" + quoted(a.Str("busca")) }},
		{Name: "spotify_controlar", Risk: Write,
			Description: "Controla o player do dono (precisa de um aparelho com o Spotify aberto e do Premium): tocar (opcionalmente uma URI), pausar, próxima, anterior ou volume.",
			Params: `{"type": "object", "properties": {
	"acao": {"type": "string", "enum": ["tocar", "pausar", "proxima", "anterior", "volume"]},
	"uri": {"type": "string", "description": "Para tocar: spotify:track:..., spotify:album:..., spotify:playlist:... ou spotify:artist:...; vazio retoma."},
	"volume": {"type": "integer", "description": "0 a 100, para acao=volume."}
}, "required": ["acao"]}`,
			Summary: func(a Args) string { return "Spotify: " + a.Str("acao") + quoted(a.Str("uri")) }},
		{Name: "spotify_fila", Risk: Write,
			Description: "Põe uma música (spotify:track:...) ou episódio na fila do player.",
			Params:      `{"type": "object", "properties": {"uri": {"type": "string"}}, "required": ["uri"]}`,
			Summary:     func(a Args) string { return "pôs na fila" + quoted(a.Str("uri")) }},
		{Name: "spotify_playlists", Risk: Read,
			Description: "Lista as playlists do dono, ou as músicas de uma (com id).",
			Params:      `{"type": "object", "properties": {"id": {"type": "string", "description": "Id da playlist para ver as músicas; vazio lista as playlists."}}}`,
			Summary:     func(a Args) string { return "playlists" + quoted(a.Str("id")) }},
		{Name: "spotify_playlist_adicionar", Risk: Write,
			Description: "Adiciona músicas (URIs spotify:track:...) a uma playlist do dono, ou cria uma playlist nova com elas quando 'nova' tem um nome.",
			Params: `{"type": "object", "properties": {
	"id": {"type": "string", "description": "Id da playlist; vazio quando for criar uma."},
	"nova": {"type": "string", "description": "Nome de uma playlist nova (privada)."},
	"uris": {"type": "array", "items": {"type": "string"}}
}, "required": ["uris"]}`,
			Summary: func(a Args) string {
				return fmt.Sprintf("%d músicas na playlist%s", len(a.List("uris")), quoted(a.Str("nova")))
			}},
		{Name: "spotify_historico", Risk: Read,
			Description: "Músicas tocadas recentemente, ou as mais ouvidas do dono (tipo=top) nas últimas semanas.",
			Params:      `{"type": "object", "properties": {"tipo": {"type": "string", "enum": ["recentes", "top"]}}}`,
			Summary:     func(a Args) string { return "histórico do Spotify" }},
	}
}

type spotifyTrack struct {
	Name    string `json:"name"`
	URI     string `json:"uri"`
	ID      string `json:"id"`
	Artists []struct {
		Name string `json:"name"`
	} `json:"artists"`
	Album struct {
		Name string `json:"name"`
	} `json:"album"`
	Owner *struct {
		Name string `json:"display_name"`
	} `json:"owner"`
	Tracks *struct {
		Total int `json:"total"`
	} `json:"tracks"`
}

func (t spotifyTrack) line() string {
	var names []string
	for _, a := range t.Artists {
		names = append(names, a.Name)
	}
	out := t.Name
	if len(names) > 0 {
		out += " — " + strings.Join(names, ", ")
	}
	if t.Album.Name != "" {
		out += " (" + t.Album.Name + ")"
	}
	if t.Owner != nil {
		out += " · de " + t.Owner.Name
	}
	if t.Tracks != nil {
		out += fmt.Sprintf(" · %d músicas", t.Tracks.Total)
	}
	return out + "  " + t.URI
}

func needURI(uri string) error {
	if !spotifyURI.MatchString(uri) {
		return fmt.Errorf("URI inválida %q: use spotify:track:... (pegue com spotify_buscar)", cut(uri, 60))
	}
	return nil
}

func (s *Spotify) Call(ctx context.Context, env *Env, tool string, a Args) (string, error) {
	switch tool {
	case "spotify_tocando":
		var st struct {
			Playing bool         `json:"is_playing"`
			Item    spotifyTrack `json:"item"`
			Device  struct {
				Name   string `json:"name"`
				Volume int    `json:"volume_percent"`
			} `json:"device"`
		}
		data, err := s.call(ctx, env, "GET", "/v1/me/player", nil, &st)
		if err != nil {
			return "", err
		}
		if len(data) == 0 || st.Item.Name == "" {
			return "Nada tocando agora.", nil
		}
		state := "tocando"
		if !st.Playing {
			state = "pausado"
		}
		return fmt.Sprintf("%s: %s\nAparelho: %s, volume %d%%", state, st.Item.line(), st.Device.Name, st.Device.Volume), nil

	case "spotify_buscar":
		q, err := a.Need("busca")
		if err != nil {
			return "", err
		}
		kind := a.Str("tipo")
		if kind == "" {
			kind = "track"
		}
		var r map[string]struct {
			Items []spotifyTrack `json:"items"`
		}
		params := url.Values{"q": {q}, "type": {kind}, "limit": {fmt.Sprint(a.Int("limite", 8, 1, 20))}}
		if _, err := s.call(ctx, env, "GET", "/v1/search?"+params.Encode(), nil, &r); err != nil {
			return "", err
		}
		var b strings.Builder
		for _, it := range r[kind+"s"].Items {
			if it.URI != "" {
				b.WriteString("- " + it.line() + "\n")
			}
		}
		if b.Len() == 0 {
			return "Nada encontrado.", nil
		}
		return b.String(), nil

	case "spotify_controlar":
		switch a.Str("acao") {
		case "tocar":
			var body any
			if uri := a.Str("uri"); uri != "" {
				if err := needURI(uri); err != nil {
					return "", err
				}
				if strings.HasPrefix(uri, "spotify:track:") || strings.HasPrefix(uri, "spotify:episode:") {
					body = map[string]any{"uris": []string{uri}}
				} else {
					body = map[string]any{"context_uri": uri}
				}
			}
			_, err := s.call(ctx, env, "PUT", "/v1/me/player/play", body, nil)
			return "Tocando.", err
		case "pausar":
			_, err := s.call(ctx, env, "PUT", "/v1/me/player/pause", nil, nil)
			return "Pausado.", err
		case "proxima":
			_, err := s.call(ctx, env, "POST", "/v1/me/player/next", nil, nil)
			return "Pulou para a próxima.", err
		case "anterior":
			_, err := s.call(ctx, env, "POST", "/v1/me/player/previous", nil, nil)
			return "Voltou para a anterior.", err
		case "volume":
			v := a.Int("volume", 50, 0, 100)
			_, err := s.call(ctx, env, "PUT", fmt.Sprintf("/v1/me/player/volume?volume_percent=%d", v), nil, nil)
			return fmt.Sprintf("Volume em %d%%.", v), err
		}
		return "", errors.New("acao: tocar, pausar, proxima, anterior ou volume")

	case "spotify_fila":
		uri := a.Str("uri")
		if err := needURI(uri); err != nil {
			return "", err
		}
		_, err := s.call(ctx, env, "POST", "/v1/me/player/queue?uri="+url.QueryEscape(uri), nil, nil)
		return "Na fila.", err

	case "spotify_playlists":
		var b strings.Builder
		if id := a.Str("id"); id != "" {
			var r struct {
				Items []struct {
					Track spotifyTrack `json:"track"`
				} `json:"items"`
				Total int `json:"total"`
			}
			if _, err := s.call(ctx, env, "GET", "/v1/playlists/"+url.PathEscape(id)+"/tracks?limit=50", nil, &r); err != nil {
				return "", err
			}
			fmt.Fprintf(&b, "%d músicas (mostrando até 50):\n", r.Total)
			for _, it := range r.Items {
				b.WriteString("- " + it.Track.line() + "\n")
			}
			return b.String(), nil
		}
		var r struct {
			Items []spotifyTrack `json:"items"`
		}
		if _, err := s.call(ctx, env, "GET", "/v1/me/playlists?limit=50", nil, &r); err != nil {
			return "", err
		}
		for _, p := range r.Items {
			b.WriteString("- " + p.line() + " id=" + p.ID + "\n")
		}
		if b.Len() == 0 {
			return "Nenhuma playlist.", nil
		}
		return b.String(), nil

	case "spotify_playlist_adicionar":
		uris := a.List("uris")
		if len(uris) == 0 || len(uris) > 100 {
			return "", errors.New("mande de 1 a 100 URIs")
		}
		for _, u := range uris {
			if err := needURI(u); err != nil {
				return "", err
			}
		}
		id := a.Str("id")
		if name := strings.TrimSpace(a.Str("nova")); id == "" && name != "" {
			var me struct {
				ID string `json:"id"`
			}
			if _, err := s.call(ctx, env, "GET", "/v1/me", nil, &me); err != nil {
				return "", err
			}
			var p struct {
				ID string `json:"id"`
			}
			if _, err := s.call(ctx, env, "POST", "/v1/users/"+url.PathEscape(me.ID)+"/playlists", map[string]any{"name": cut(name, 100), "public": false}, &p); err != nil {
				return "", err
			}
			id = p.ID
		}
		if id == "" {
			return "", errors.New("diga o id da playlist ou o nome de uma nova")
		}
		if _, err := s.call(ctx, env, "POST", "/v1/playlists/"+url.PathEscape(id)+"/tracks", map[string]any{"uris": uris}, nil); err != nil {
			return "", err
		}
		return fmt.Sprintf("%d músicas adicionadas à playlist %s.", len(uris), id), nil

	case "spotify_historico":
		var b strings.Builder
		if a.Str("tipo") == "top" {
			var r struct {
				Items []spotifyTrack `json:"items"`
			}
			if _, err := s.call(ctx, env, "GET", "/v1/me/top/tracks?limit=20&time_range=short_term", nil, &r); err != nil {
				return "", err
			}
			for i, t := range r.Items {
				fmt.Fprintf(&b, "%d. %s\n", i+1, t.line())
			}
			return b.String(), nil
		}
		var r struct {
			Items []struct {
				Track    spotifyTrack `json:"track"`
				PlayedAt string       `json:"played_at"`
			} `json:"items"`
		}
		if _, err := s.call(ctx, env, "GET", "/v1/me/player/recently-played?limit=20", nil, &r); err != nil {
			return "", err
		}
		for _, it := range r.Items {
			fmt.Fprintf(&b, "%s  %s\n", cut(it.PlayedAt, 16), it.Track.line())
		}
		return b.String(), nil
	}
	return "", fmt.Errorf("ferramenta desconhecida %s", tool)
}
