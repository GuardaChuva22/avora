package connect

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"
	"time"

	"github.com/GuardaChuva22/avora/internal/mcp"
)

// The ElevenLabs login: its authorization server takes Avora's client
// metadata document as the client id (no sign-up), and the token it gives
// is a bearer token for the same API the keys use, renewed when it ends.
func TestElevenLabsLogin(t *testing.T) {
	var auth *httptest.Server
	grants := []url.Values{}
	auth = httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/mcp":
			w.Header().Set("WWW-Authenticate", `Bearer resource_metadata="`+auth.URL+`/.well-known/oauth-protected-resource"`)
			http.Error(w, `{"detail":"OAuth bearer token required"}`, http.StatusUnauthorized)
		case "/.well-known/oauth-protected-resource":
			json.NewEncoder(w).Encode(map[string]any{"resource": auth.URL + "/mcp", "authorization_servers": []string{auth.URL},
				"scopes_supported": []string{"text_to_speech", "ads_write"}})
		case "/.well-known/oauth-authorization-server":
			json.NewEncoder(w).Encode(map[string]any{"issuer": auth.URL, "authorization_endpoint": "https://elevenlabs.example/app/oauth/authorize",
				"token_endpoint": auth.URL + "/token", "client_id_metadata_document_supported": true})
		case "/token":
			r.ParseForm()
			grants = append(grants, r.PostForm)
			tok := "tok-1"
			if r.PostForm.Get("grant_type") == "refresh_token" {
				tok = "tok-2"
			}
			json.NewEncoder(w).Encode(map[string]any{"access_token": tok, "refresh_token": "ref", "expires_in": 3600})
		default:
			http.NotFound(w, r)
		}
	}))
	defer auth.Close()
	api := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Header.Get("xi-api-key") != "" {
			t.Errorf("login call sent a key header")
		}
		if r.Header.Get("Authorization") != "Bearer tok-2" {
			http.Error(w, "unauthorized", http.StatusUnauthorized)
			return
		}
		json.NewEncoder(w).Encode(map[string]any{"user_id": "abcdefgh1234", "first_name": "Caio"})
	}))
	defer api.Close()

	e := &ElevenLabs{BaseURL: api.URL, MCPURL: auth.URL + "/mcp", HTTP: auth.Client()}
	if !e.Info().Login || e.Info().Auth != "token" || e.Info().OAuthPort != mcp.ClientPort {
		t.Fatalf("info = %+v", e.Info())
	}
	ctx := context.Background()
	redirect := "http://127.0.0.1:47613/callback"
	u, extra, err := e.BeginAuth(ctx, redirect, "st", "verifier")
	if err != nil {
		t.Fatal(err)
	}
	q, _ := url.Parse(u)
	if q.Query().Get("client_id") != mcp.ClientMetadataURL || q.Query().Get("scope") != elevenScope {
		t.Fatalf("auth url = %s", u)
	}
	now := time.Now()
	cred, err := e.FinishAuth(ctx, "code", redirect, "verifier", extra, now)
	if err != nil {
		t.Fatal(err)
	}
	if !ByLogin(&cred) || cred.Token != "tok-1" || grants[0].Get("client_id") != mcp.ClientMetadataURL {
		t.Fatalf("cred = %+v, grant = %v", cred, grants[0])
	}
	// The API refuses tok-1: the token is renewed and the call tried again.
	env := &Env{Cred: &cred, Now: func() time.Time { return now }}
	account, err := e.Check(ctx, env)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.HasSuffix(account, "· login") || cred.Token != "tok-2" || !env.Changed {
		t.Fatalf("account %q, cred %+v, changed %v", account, cred, env.Changed)
	}
	if _, err := e.Transcribe(ctx, env, "a.ogg", "audio/ogg", []byte("x")); err != ErrNoTranscribe {
		t.Fatalf("transcribe by login: %v", err)
	}
}
