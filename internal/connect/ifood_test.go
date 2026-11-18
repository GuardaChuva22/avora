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

func TestIFood(t *testing.T) {
	var called []string
	var lastArgs map[string]any
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/token" {
			_ = r.ParseForm()
			if r.Form.Get("client_id") != "cid" || r.Form.Get("client_secret") != "sec" {
				http.Error(w, `{"error":"invalid_client"}`, 401)
				return
			}
			switch r.Form.Get("grant_type") {
			case "authorization_code":
				if r.Form.Get("code_verifier") != "verif" {
					http.Error(w, `{"error":"invalid_grant"}`, 400)
					return
				}
				fmt.Fprint(w, `{"access_token":"at-1","refresh_token":"rt-1","expires_in":3600}`)
			case "refresh_token":
				fmt.Fprint(w, `{"access_token":"at-2","refresh_token":"rt-2","expires_in":3600}`)
			}
			return
		}
		if !strings.HasPrefix(r.URL.Path, "/v3/") {
			http.NotFound(w, r)
			return
		}
		// at-1 "expired" on the server: only at-2 is accepted after the first call.
		tok := r.Header.Get("Authorization")
		if tok != "Bearer at-1" && tok != "Bearer at-2" {
			http.Error(w, "unauthorized", 401)
			return
		}
		var m struct {
			ID     json.RawMessage `json:"id"`
			Method string          `json:"method"`
			Params struct {
				Name      string         `json:"name"`
				Arguments map[string]any `json:"arguments"`
			} `json:"params"`
		}
		_ = json.NewDecoder(r.Body).Decode(&m)
		answer := func(result string) {
			fmt.Fprintf(w, `{"jsonrpc":"2.0","id":%s,"result":%s}`, m.ID, result)
		}
		server := strings.TrimPrefix(r.URL.Path, "/v3/")
		switch m.Method {
		case "initialize":
			w.Header().Set("Mcp-Session-Id", "s-"+server)
			answer(`{"protocolVersion":"2025-06-18","capabilities":{},"serverInfo":{"name":"ifood-` + server + `"}}`)
		case "notifications/initialized":
			w.WriteHeader(202)
		case "tools/list":
			if server == "discovery" {
				answer(`{"tools":[{"name":"searchOffers","inputSchema":{"type":"object","properties":{"query":{"type":"string"}},"required":["query"]}}]}`)
				return
			}
			answer(`{"tools":[]}`)
		case "tools/call":
			called = append(called, server+"/"+m.Params.Name)
			lastArgs = m.Params.Arguments
			if m.Params.Name == "getOrder" && tok == "Bearer at-1" {
				http.Error(w, "expired", 401)
				return
			}
			switch m.Params.Name {
			case "getUser":
				answer(`{"content":[{"type":"text","text":"{\"name\":\"Dono\",\"email\":\"dono@example.com\"}"}]}`)
			case "searchOffers":
				answer(`{"content":[{"type":"text","text":"Pizzaria X (merchantId m1)"}]}`)
			case "submitOrder":
				answer(`{"content":[{"type":"text","text":"cart has no payment"}],"isError":true}`)
			default:
				answer(`{"content":[{"type":"text","text":"ok"}]}`)
			}
		}
	}))
	defer srv.Close()

	f := &IFood{ClientID: "cid", ClientSecret: "sec", Auth: srv.URL, API: srv.URL, HTTP: srv.Client()}
	if !f.Info().Ready || (&IFood{}).Info().Ready || f.Info().OAuthPort != IFoodPort {
		t.Fatal("ready only with a client id, on the fixed port")
	}
	ctx := context.Background()
	link, _, err := f.BeginAuth(ctx, IFoodRedirect, "st", "verif")
	if err != nil {
		t.Fatal(err)
	}
	u, _ := url.Parse(link)
	if u.Path != "/authorize" || u.Query().Get("code_challenge") != PKCE("verif") || !strings.Contains(u.Query().Get("scope"), "cart:write") {
		t.Fatalf("authorize: %s", link)
	}
	now := time.Now()
	cred, err := f.FinishAuth(ctx, "c0de", IFoodRedirect, "verif", nil, now)
	if err != nil || cred.Token != "at-1" || cred.Refresh != "rt-1" {
		t.Fatalf("finish: %+v %v", cred, err)
	}
	env := &Env{Cred: &cred}
	if who, err := f.Check(ctx, env); err != nil || who != "dono@example.com" {
		t.Fatalf("check: %q %v", who, err)
	}
	// The server's own schema replaces the loose one.
	var search Tool
	tools := f.Tools()
	for _, tl := range tools {
		if tl.Name == "ifood_buscar" {
			search = tl
		}
		if tl.Name == "ifood_fazer_pedido" && !tl.Risk.Locked() {
			t.Fatal("placing an order needs approval")
		}
	}
	if len(tools) != len(ifoodTools) || !strings.Contains(search.Params, `"query"`) {
		t.Fatalf("schema not learned: %q", search.Params)
	}
	out, err := f.Call(ctx, env, "ifood_buscar", Args{"query": "pizza"})
	if err != nil || !strings.Contains(out, "Pizzaria X") || lastArgs["query"] != "pizza" {
		t.Fatalf("search: %q %v %v", out, err, lastArgs)
	}
	// A 401 refreshes once and retries.
	if out, err := f.Call(ctx, env, "ifood_pedido", Args{"orderId": "o1"}); err != nil || !strings.Contains(out, "ok") {
		t.Fatalf("order: %q %v", out, err)
	}
	if !env.Changed || env.Cred.Token != "at-2" || env.Cred.Refresh != "rt-2" {
		t.Fatalf("refresh: %+v", env.Cred)
	}
	if _, err := f.Call(ctx, env, "ifood_fazer_pedido", Args{"cartId": "c1"}); err == nil || !strings.Contains(err.Error(), "no payment") {
		t.Fatalf("isError: %v", err)
	}
	want := []string{"user/getUser", "discovery/searchOffers", "order/getOrder", "order/getOrder", "cart/submitOrder"}
	if strings.Join(called, ",") != strings.Join(want, ",") {
		t.Fatalf("calls: %v", called)
	}
}
