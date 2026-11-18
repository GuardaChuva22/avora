package connect

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"
	"time"
)

func TestInstagram(t *testing.T) {
	var mu sync.Mutex
	var calls []string
	polls := 0
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		mu.Lock()
		defer mu.Unlock()
		body, _ := io.ReadAll(r.Body)
		calls = append(calls, r.Method+" "+r.URL.Path+" "+string(body))
		if r.URL.Path == "/refresh_access_token" {
			_ = json.NewEncoder(w).Encode(map[string]any{"access_token": "IGnovo", "expires_in": 5184000})
			return
		}
		if r.Header.Get("Authorization") == "Bearer IGvelho" {
			w.WriteHeader(400)
			_, _ = w.Write([]byte(`{"error":{"message":"Session has expired","type":"OAuthException","code":190}}`))
			return
		}
		switch {
		case r.URL.Path == "/v23.0/me":
			_, _ = w.Write([]byte(`{"id":"1789","username":"clinica.sorriso","name":"Clínica Sorriso","account_type":"BUSINESS","followers_count":1520,"follows_count":80,"media_count":42}`))
		case r.URL.Path == "/v23.0/me/media" && r.Method == "POST":
			_, _ = w.Write([]byte(`{"id":"17000001"}`))
		case r.URL.Path == "/v23.0/17000001":
			polls++
			if polls == 1 {
				_, _ = w.Write([]byte(`{"status_code":"IN_PROGRESS"}`))
			} else {
				_, _ = w.Write([]byte(`{"status_code":"FINISHED"}`))
			}
		case r.URL.Path == "/v23.0/me/media_publish":
			_, _ = w.Write([]byte(`{"id":"18000009"}`))
		case r.URL.Path == "/v23.0/18000009":
			_, _ = w.Write([]byte(`{"permalink":"https://www.instagram.com/p/abc/"}`))
		default:
			w.WriteHeader(404)
		}
	}))
	defer srv.Close()
	g := &Instagram{BaseURL: srv.URL, Sleep: func(time.Duration) {}}
	now := time.Date(2026, 11, 16, 12, 0, 0, 0, time.UTC)
	env := &Env{Cred: &Cred{Token: "IGtok"}, Now: func() time.Time { return now }}
	ctx := context.Background()

	acc, err := g.Check(ctx, env)
	if err != nil || acc != "@clinica.sorriso" {
		t.Fatalf("check: %q %v", acc, err)
	}
	if !env.Changed || env.Cred.Expiry.Sub(now) < 58*24*time.Hour {
		t.Fatalf("expiry not noted: %+v", env.Cred)
	}

	out, err := g.Call(ctx, env, "instagram_publicar", Args{"imagem_url": "https://exemplo.com/f.jpg", "legenda": "Oi #sorriso"})
	if err != nil || !strings.Contains(out, "post_id 18000009") || !strings.Contains(out, "instagram.com/p/abc") {
		t.Fatalf("publish: %q %v", out, err)
	}
	if _, err := g.Call(ctx, env, "instagram_publicar", Args{"imagem_url": "http://192.168.0.2/f.jpg"}); err == nil {
		t.Fatal("a non-https image was accepted")
	}

	// Close to expiry, the token is renewed.
	env.Cred.Expiry = now.Add(5 * 24 * time.Hour)
	env.Changed = false
	if _, err := g.Call(ctx, env, "instagram_perfil", Args{}); err != nil {
		t.Fatal(err)
	}
	if env.Cred.Token != "IGnovo" || !env.Changed {
		t.Fatalf("not renewed: %+v", env.Cred)
	}

	// An expired token asks the owner to reconnect.
	env.Cred = &Cred{Token: "IGvelho", Expiry: now.Add(40 * 24 * time.Hour)}
	if _, err := g.Call(ctx, env, "instagram_perfil", Args{}); !IsAuth(err) {
		t.Fatalf("expired token: %v", err)
	}
	for _, tl := range g.Tools() {
		if (tl.Name == "instagram_publicar" || tl.Name == "instagram_dm" || tl.Name == "instagram_responder_comentario") != tl.Risk.Locked() {
			t.Fatalf("%s risk %s", tl.Name, tl.Risk)
		}
	}
}

// A Meta token (Unified Login, EAA...) runs on graph.facebook.com against
// the Instagram account found through the token's Pages.
func TestInstagramUnified(t *testing.T) {
	var paths []string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		paths = append(paths, r.Method+" "+r.URL.Path)
		switch r.URL.Path {
		case "/v23.0/me/accounts":
			_, _ = w.Write([]byte(`{"data":[{"id":"555"},{"id":"777","instagram_business_account":{"id":"17841400000000001","username":"loja"}}]}`))
		case "/v23.0/17841400000000001":
			_, _ = w.Write([]byte(`{"id":"17841400000000001","username":"loja","name":"Loja","followers_count":10}`))
		case "/v23.0/777/messages":
			_, _ = w.Write([]byte(`{"message_id":"m1"}`))
		default:
			w.WriteHeader(404)
		}
	}))
	defer srv.Close()
	g := &Instagram{BaseURL: "http://127.0.0.1:1", FacebookURL: srv.URL}
	env := &Env{Cred: &Cred{Token: "EAAtok"}}
	acc, err := g.Check(context.Background(), env)
	if err != nil || acc != "@loja" || env.Cred.Extra["ig_id"] != "17841400000000001" || env.Cred.Extra["page_id"] != "777" || !env.Changed {
		t.Fatalf("check: %q %v %+v", acc, err, env.Cred)
	}
	if _, err := g.Call(context.Background(), env, "instagram_dm", Args{"para_id": "123456789", "texto": "oi"}); err != nil {
		t.Fatal(err)
	}
	// Given by hand after the token.
	env2 := &Env{Cred: &Cred{Token: "EAAtok#17841400000000001"}}
	if acc, err := g.Check(context.Background(), env2); err != nil || acc != "@loja" || env2.Cred.Token != "EAAtok" {
		t.Fatalf("manual id: %q %v %+v", acc, err, env2.Cred)
	}
}
