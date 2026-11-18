package control

import (
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"

	"github.com/GuardaChuva22/avora/internal/connect"
	"github.com/GuardaChuva22/avora/internal/store"
	"github.com/GuardaChuva22/avora/internal/vault"
)

// Two Apify accounts: runs go to the one with more credit, move on when it
// runs out, and a run's results are read from the account that ran it.
func TestApifyAccountsArePooled(t *testing.T) {
	var mu sync.Mutex
	ran := []string{}
	used := map[string]string{"apify_api_A": "4.9", "apify_api_B": "1.0"}
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		tok := strings.TrimPrefix(r.Header.Get("Authorization"), "Bearer ")
		user := map[string]string{"apify_api_A": "conta-a", "apify_api_B": "conta-b"}[tok]
		if user == "" {
			w.WriteHeader(401)
			return
		}
		switch {
		case r.URL.Path == "/v2/users/me":
			io.WriteString(w, `{"data":{"username":"`+user+`"}}`)
		case r.URL.Path == "/v2/users/me/limits":
			mu.Lock()
			u := used[tok]
			mu.Unlock()
			io.WriteString(w, `{"data":{"limits":{"maxMonthlyUsageUsd":5},"current":{"monthlyUsageUsd":`+u+`}}}`)
		case strings.HasSuffix(r.URL.Path, "/runs"):
			mu.Lock()
			ran = append(ran, user)
			spent := used[tok] == "5"
			mu.Unlock()
			if spent {
				w.WriteHeader(402)
				io.WriteString(w, `{"error":{"type":"not-enough-usage-to-run-paid-actor"}}`)
				return
			}
			io.WriteString(w, `{"data":{"id":"RUN-`+user+`","status":"SUCCEEDED"}}`)
		case strings.HasPrefix(r.URL.Path, "/v2/actor-runs/RUN-"):
			if !strings.HasSuffix(r.URL.Path, user) {
				w.WriteHeader(404)
				return
			}
			io.WriteString(w, `{"data":{"id":"RUN-`+user+`","status":"SUCCEEDED"}}`)
		default:
			w.WriteHeader(404)
		}
	}))
	defer srv.Close()

	s := chatServer(t, nil)
	apify := &connect.Apify{BaseURL: srv.URL}
	s.SetConnectors(connect.NewRegistry(apify), vault.Random())
	for _, tok := range []string{"apify_api_A", "apify_api_B"} {
		if _, err := s.saveCredential(t.Context(), apify, connect.Cred{Token: tok}, ""); err != nil {
			t.Fatal(err)
		}
	}
	tr := &turn{agent: store.Agent{ID: "cora"}}
	run := func(name, args string) string {
		out, err, ok := s.runConnector(t.Context(), tr, name, args)
		if !ok || err != nil {
			t.Fatalf("%s: %v", name, err)
		}
		return out
	}

	// B has more credit left: it runs first, though A is the older account.
	out := run("apify_rodar", `{"robo": "x/y"}`)
	if ran[len(ran)-1] != "conta-b" {
		t.Fatalf("ran on %v", ran)
	}
	ref := out[strings.Index(out, "RUN-conta-b@"):]
	ref = ref[:strings.IndexAny(ref, ": ")]

	// B runs out: the call moves on to A.
	mu.Lock()
	used["apify_api_B"] = "5"
	mu.Unlock()
	for _, c := range s.accounts(t.Context(), "apify") {
		s.usage.drop(c.ID)
	}
	s.connectionsUsage(t.Context(), apify, s.accounts(t.Context(), "apify"), true)
	mu.Lock()
	used["apify_api_A"] = "1.0"
	mu.Unlock()
	ran = nil
	if out := run("apify_rodar", `{"robo": "x/y"}`); !strings.Contains(out, "RUN-conta-a@") || len(ran) != 1 || ran[0] != "conta-a" {
		t.Fatalf("after B ran out: %v %s", ran, out)
	}

	// B's run is read from B, whatever account has more credit now.
	if out := run("apify_resultado", `{"execucao": "`+ref+`"}`); !strings.Contains(out, "RUN-conta-b") {
		t.Fatalf("routed read: %s", out)
	}

	// Both spent: the agent hears it.
	mu.Lock()
	used["apify_api_A"], used["apify_api_B"] = "5", "5"
	mu.Unlock()
	for _, c := range s.accounts(t.Context(), "apify") {
		s.usage.drop(c.ID)
	}
	if _, err, _ := s.runConnector(t.Context(), tr, "apify_rodar", `{"robo": "x/y"}`); err == nil || !strings.Contains(err.Error(), "todas as 2 contas") {
		t.Fatalf("all spent: %v", err)
	}
}

// Shopee runs on the Apify accounts: it shows as connected when Apify is,
// its tool goes to the Apify account, and it cannot be connected apart.
func TestStoreAppRunsOnApify(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Header.Get("Authorization") != "Bearer apify_api_A" {
			w.WriteHeader(401)
			return
		}
		switch {
		case r.URL.Path == "/v2/users/me":
			io.WriteString(w, `{"data":{"username":"conta-a"}}`)
		case r.URL.Path == "/v2/users/me/limits":
			io.WriteString(w, `{"data":{"limits":{"maxMonthlyUsageUsd":5},"current":{"monthlyUsageUsd":1}}}`)
		case r.URL.Path == "/v2/acts/gio21~shopee-scraper/runs":
			io.WriteString(w, `{"data":{"id":"R1","status":"SUCCEEDED","defaultDatasetId":"D1"}}`)
		case r.URL.Path == "/v2/datasets/D1/items":
			io.WriteString(w, `[{"name":"Fone X","price":49.9,"url":"https://shopee/1"}]`)
		default:
			w.WriteHeader(404)
		}
	}))
	defer srv.Close()

	s := chatServer(t, nil)
	apify := &connect.Apify{BaseURL: srv.URL}
	shopee := &connect.StoreApp{Apify: apify, Store: "shopee"}
	s.SetConnectors(connect.NewRegistry(apify, shopee), vault.Random())
	tr := &turn{agent: store.Agent{ID: "cora"}}
	if _, err, _ := s.runConnector(t.Context(), tr, "shopee_buscar", `{"busca": "fone"}`); err == nil {
		t.Fatal("ran without Apify")
	}
	if _, err := s.saveCredential(t.Context(), apify, connect.Cred{Token: "apify_api_A"}, ""); err != nil {
		t.Fatal(err)
	}
	names := []string{}
	for _, tl := range s.connectorTools(t.Context(), "cora", nil) {
		names = append(names, tl.Function.Name)
	}
	if !strings.Contains(strings.Join(names, ","), "shopee_buscar") {
		t.Fatalf("tools: %v", names)
	}
	out, err, _ := s.runConnector(t.Context(), tr, "shopee_buscar", `{"busca": "fone"}`)
	if err != nil || !strings.Contains(out, "Fone X") || !strings.Contains(out, "R$ 49,90") {
		t.Fatalf("run: %s %v", out, err)
	}
	if v := s.connectorView(t.Context(), shopee); v.Connection == nil || v.Connection.Account != "conta-a" {
		t.Fatalf("view: %+v", v.Connection)
	}
	if !strings.Contains(s.connectionsBlock(t.Context(), "cora"), "Shopee (conta-a)") {
		t.Fatal("block")
	}
}
