package connect

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"
)

func TestLinkedInPostsInTheOwnersName(t *testing.T) {
	var post map[string]any
	var version, deleted string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/oauth/v2/accessToken" {
			_ = r.ParseForm()
			if r.Form.Get("client_secret") != "sec" || r.Form.Get("redirect_uri") != LinkedInRedirect {
				http.Error(w, `{"error":"invalid_client"}`, 401)
				return
			}
			fmt.Fprint(w, `{"access_token":"li-1","expires_in":5184000}`)
			return
		}
		if r.Header.Get("Authorization") != "Bearer li-1" {
			http.Error(w, "no", 401)
			return
		}
		switch r.Method + " " + r.URL.Path {
		case "GET /v2/userinfo":
			fmt.Fprint(w, `{"sub":"AbC123","name":"Dono","email":"dono@example.com"}`)
		case "POST /rest/posts":
			version = r.Header.Get("LinkedIn-Version")
			_ = json.NewDecoder(r.Body).Decode(&post)
			w.Header().Set("x-restli-id", "urn:li:share:7000000000001")
			w.WriteHeader(201)
		case "DELETE /rest/posts/urn:li:share:7000000000001":
			deleted = "yes"
			w.WriteHeader(204)
		default:
			http.NotFound(w, r)
		}
	}))
	defer srv.Close()

	l := &LinkedIn{ClientID: "cid", ClientSecret: "sec", API: srv.URL, Auth: srv.URL}
	ctx := context.Background()
	if info := l.Info(); !info.Ready || info.OAuthPort != LinkedInPort {
		t.Fatalf("info: %+v", info)
	}
	if _, _, err := l.BeginAuth(ctx, "http://127.0.0.1:5000/callback", "st", "v"); err == nil {
		t.Fatal("another port accepted")
	}
	if link, _, _ := l.BeginAuth(ctx, LinkedInRedirect, "st", "v"); !strings.Contains(link, "w_member_social") {
		t.Fatalf("auth url: %s", link)
	}
	now := time.Date(2026, 11, 12, 10, 0, 0, 0, time.UTC)
	cred, err := l.FinishAuth(ctx, "code", LinkedInRedirect, "v", nil, now)
	if err != nil || !cred.Expiry.Equal(now.Add(60*24*time.Hour)) {
		t.Fatalf("finish: %+v %v", cred, err)
	}
	env := &Env{Cred: &cred, Now: func() time.Time { return now }}
	if acc, err := l.Check(ctx, env); err != nil || acc != "dono@example.com" {
		t.Fatalf("check: %q %v", acc, err)
	}
	out, err := l.Call(ctx, env, "linkedin_publicar", Args{"texto": "Novo projeto (beta) #IA #open_source no ar", "link": "https://example.com/x"})
	if err != nil || !strings.Contains(out, "feed/update/urn:li:share:7000000000001") {
		t.Fatalf("publicar: %s %v", out, err)
	}
	if post["author"] != "urn:li:person:AbC123" || post["visibility"] != "PUBLIC" || version != "202609" {
		t.Fatalf("post: %v %s", post, version)
	}
	if c := post["commentary"]; c != `Novo projeto \(beta\) {hashtag|\#|IA} {hashtag|\#|open\_source} no ar` {
		t.Fatalf("commentary: %v", c)
	}
	if _, err := l.Call(ctx, env, "linkedin_apagar_post", Args{"id": "urn:li:share:7000000000001"}); err != nil || deleted != "yes" {
		t.Fatalf("apagar: %v", err)
	}
	if _, err := l.Call(ctx, env, "linkedin_apagar_post", Args{"id": "../x"}); err == nil {
		t.Fatal("bad id accepted")
	}
	for _, tool := range l.Tools() {
		if tool.Name != "linkedin_perfil" && !tool.Risk.Locked() {
			t.Fatalf("%s should need approval", tool.Name)
		}
	}

	later := &Env{Cred: &cred, Now: func() time.Time { return now.Add(61 * 24 * time.Hour) }}
	if _, err := l.Check(ctx, later); !IsAuth(err) {
		t.Fatalf("expired: %v", err)
	}
}
