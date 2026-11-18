package connect

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"
	"time"
)

func TestSpotifyAuthAndTools(t *testing.T) {
	var played map[string]any
	var queued, added string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/api/token" {
			_ = r.ParseForm()
			if r.Form.Get("client_id") != "cid" {
				http.Error(w, `{"error":"invalid_client"}`, 400)
				return
			}
			switch r.Form.Get("grant_type") {
			case "authorization_code":
				if r.Form.Get("code_verifier") != "verif" || r.Form.Get("code") != "c0de" {
					http.Error(w, `{"error":"invalid_grant"}`, 400)
					return
				}
				fmt.Fprint(w, `{"access_token":"at-1","refresh_token":"rt-1","expires_in":3600}`)
			case "refresh_token":
				if r.Form.Get("refresh_token") != "rt-1" {
					http.Error(w, `{"error":"invalid_grant"}`, 400)
					return
				}
				fmt.Fprint(w, `{"access_token":"at-2","expires_in":3600}`)
			}
			return
		}
		if tok := r.Header.Get("Authorization"); tok != "Bearer at-1" && tok != "Bearer at-2" {
			http.Error(w, "no", 401)
			return
		}
		switch r.Method + " " + r.URL.Path {
		case "GET /v1/me":
			fmt.Fprint(w, `{"id":"u1","display_name":"Dono","email":"dono@example.com"}`)
		case "GET /v1/me/player":
			fmt.Fprint(w, `{"is_playing":true,"device":{"name":"PC","volume_percent":40},"item":{"name":"Song","uri":"spotify:track:abcdefghij0123456789ab","artists":[{"name":"Band"}],"album":{"name":"LP"}}}`)
		case "GET /v1/search":
			if r.URL.Query().Get("type") != "track" {
				http.Error(w, "type", 400)
				return
			}
			fmt.Fprint(w, `{"tracks":{"items":[{"name":"Hit","uri":"spotify:track:zzzzzzzzzz0123456789ab","artists":[{"name":"A"},{"name":"B"}],"album":{"name":"X"}}]}}`)
		case "PUT /v1/me/player/play":
			_ = json.NewDecoder(r.Body).Decode(&played)
			w.WriteHeader(204)
		case "POST /v1/me/player/next":
			http.Error(w, `{"error":{"status":403,"message":"Player command failed: Premium required","reason":"PREMIUM_REQUIRED"}}`, 403)
		case "POST /v1/me/player/queue":
			queued = r.URL.Query().Get("uri")
			w.WriteHeader(204)
		case "POST /v1/users/u1/playlists":
			fmt.Fprint(w, `{"id":"pl9"}`)
		case "POST /v1/playlists/pl9/tracks":
			var b struct{ URIs []string }
			_ = json.NewDecoder(r.Body).Decode(&b)
			added = strings.Join(b.URIs, ",")
			fmt.Fprint(w, `{"snapshot_id":"s"}`)
		default:
			http.NotFound(w, r)
		}
	}))
	defer srv.Close()

	s := &Spotify{ClientID: "cid", API: srv.URL, Accounts: srv.URL}
	ctx := context.Background()
	if !s.Info().Ready || (&Spotify{}).Info().Ready {
		t.Fatal("ready only with a client id")
	}
	link, _, _ := s.BeginAuth(ctx, "http://127.0.0.1:5555/callback", "st", "verif")
	u, _ := url.Parse(link)
	if q := u.Query(); q.Get("code_challenge") != PKCE("verif") || q.Get("redirect_uri") != "http://127.0.0.1:5555/callback" || !strings.Contains(q.Get("scope"), "user-modify-playback-state") {
		t.Fatalf("auth url: %s", link)
	}
	now := time.Date(2026, 11, 12, 10, 0, 0, 0, time.UTC)
	cred, err := s.FinishAuth(ctx, "c0de", "http://127.0.0.1:5555/callback", "verif", nil, now)
	if err != nil || cred.Refresh != "rt-1" {
		t.Fatalf("finish: %+v %v", cred, err)
	}

	// An hour later the token is refreshed and the refresh token kept.
	env := &Env{Cred: &cred, Now: func() time.Time { return now.Add(time.Hour) }}
	if acc, err := s.Check(ctx, env); err != nil || acc != "dono@example.com" || !env.Changed || cred.Token != "at-2" || cred.Refresh != "rt-1" {
		t.Fatalf("check: %q %v %+v", acc, err, cred)
	}
	if out, _ := s.Call(ctx, env, "spotify_tocando", Args{}); !strings.Contains(out, "tocando: Song — Band (LP)") || !strings.Contains(out, "PC, volume 40%") {
		t.Fatalf("tocando: %s", out)
	}
	if out, _ := s.Call(ctx, env, "spotify_buscar", Args{"busca": "hit"}); !strings.Contains(out, "Hit — A, B (X)  spotify:track:zzzzzzzzzz0123456789ab") {
		t.Fatalf("buscar: %s", out)
	}
	if _, err := s.Call(ctx, env, "spotify_controlar", Args{"acao": "tocar", "uri": "spotify:album:abcdefghij0123456789ab"}); err != nil || played["context_uri"] != "spotify:album:abcdefghij0123456789ab" {
		t.Fatalf("tocar: %v %v", played, err)
	}
	if _, err := s.Call(ctx, env, "spotify_controlar", Args{"acao": "proxima"}); err == nil || !strings.Contains(err.Error(), "Premium") {
		t.Fatalf("premium: %v", err)
	}
	if _, err := s.Call(ctx, env, "spotify_fila", Args{"uri": "https://evil/x"}); err == nil {
		t.Fatal("bad uri accepted")
	}
	if _, err := s.Call(ctx, env, "spotify_fila", Args{"uri": "spotify:track:zzzzzzzzzz0123456789ab"}); err != nil || queued != "spotify:track:zzzzzzzzzz0123456789ab" {
		t.Fatalf("fila: %q %v", queued, err)
	}
	if out, err := s.Call(ctx, env, "spotify_playlist_adicionar", Args{"nova": "Foco", "uris": []any{"spotify:track:zzzzzzzzzz0123456789ab"}}); err != nil || added != "spotify:track:zzzzzzzzzz0123456789ab" || !strings.Contains(out, "pl9") {
		t.Fatalf("playlist: %s %v", out, err)
	}

	gone := Cred{Refresh: "revoked"}
	if _, err := s.Check(ctx, &Env{Cred: &gone}); !IsAuth(err) {
		t.Fatalf("revoked: %v", err)
	}
}
