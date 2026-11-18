package connect

import (
	"context"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"
)

func TestTinder(t *testing.T) {
	me := "5a0000000000000000000001"
	her := "5b0000000000000000000002"
	match := me + her
	var sent string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Header.Get("X-Auth-Token") != "tok" {
			w.WriteHeader(403)
			return
		}
		switch {
		case r.URL.Path == "/v2/profile":
			_, _ = w.Write([]byte(`{"data":{"user":{"_id":"` + me + `","name":"Caio","birth_date":"1998-03-01T00:00:00.000Z"}}}`))
		case r.URL.Path == "/v2/recs/core":
			_, _ = w.Write([]byte(`{"data":{"results":[{"user":{"_id":"` + her + `","name":"Ana","bio":"café e trilha","birth_date":"2000-05-10T00:00:00.000Z","photos":[{"url":"https://images/1.jpg"}]},"distance_mi":3,"s_number":987}]}}`))
		case r.URL.Path == "/like/"+her:
			if r.URL.Query().Get("s_number") != "987" {
				w.WriteHeader(400)
				return
			}
			_, _ = w.Write([]byte(`{"match":{"_id":"` + match + `"},"likes_remaining":99}`))
		case r.URL.Path == "/v2/matches":
			_, _ = w.Write([]byte(`{"data":{"matches":[{"_id":"` + match + `","person":{"_id":"` + her + `","name":"Ana"},"messages":[{"from":"` + her + `","message":"oi! tudo bem?"}]}]}}`))
		case r.URL.Path == "/user/matches/"+match && r.Method == "POST":
			sent = "ok"
			_, _ = w.Write([]byte(`{"_id":"m1"}`))
		default:
			w.WriteHeader(404)
		}
	}))
	defer srv.Close()
	d := &Tinder{BaseURL: srv.URL}
	now := time.Date(2026, 11, 16, 12, 0, 0, 0, time.UTC)
	env := &Env{Cred: &Cred{Token: "tok"}, Now: func() time.Time { return now }}
	ctx := context.Background()
	if acc, err := d.Check(ctx, env); err != nil || acc != "Caio" {
		t.Fatalf("check: %q %v", acc, err)
	}
	out, err := d.Call(ctx, env, "tinder_sugestoes", Args{})
	if err != nil || !strings.Contains(out, "Ana, 26") || !strings.Contains(out, "s_number 987") || !strings.Contains(out, "a 5 km") {
		t.Fatalf("recs: %q %v", out, err)
	}
	if out, err := d.Call(ctx, env, "tinder_curtir", Args{"id": her, "s_number": "987"}); err != nil || !strings.Contains(out, "match") {
		t.Fatalf("like: %q %v", out, err)
	}
	if out, err := d.Call(ctx, env, "tinder_matches", Args{"conversas": true}); err != nil || !strings.Contains(out, "⏳ Ana") {
		t.Fatalf("matches: %q %v", out, err)
	}
	if _, err := d.Call(ctx, env, "tinder_enviar", Args{"match_id": match, "texto": "tudo e você?"}); err != nil || sent != "ok" {
		t.Fatalf("send: %v", err)
	}
	if _, err := d.Check(ctx, &Env{Cred: &Cred{Token: "velho"}}); !IsAuth(err) {
		t.Fatalf("bad token: %v", err)
	}
	for _, tl := range d.Tools() {
		if (tl.Name == "tinder_curtir" || tl.Name == "tinder_enviar") != tl.Risk.Locked() {
			t.Fatalf("%s risk %s", tl.Name, tl.Risk)
		}
	}
}
