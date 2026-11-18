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

const testItem = "0a1b2c3d-0000-4000-8000-000000000001"

func fakePluggy(t *testing.T) (*httptest.Server, *int) {
	auths := 0
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/auth" {
			var body map[string]string
			_ = json.NewDecoder(r.Body).Decode(&body)
			if body["clientSecret"] != "s3cret" {
				http.Error(w, `{"message":"bad"}`, http.StatusForbidden)
				return
			}
			auths++
			fmt.Fprint(w, `{"apiKey":"key-1"}`)
			return
		}
		if r.Header.Get("X-API-KEY") != "key-1" {
			http.Error(w, "no", http.StatusUnauthorized)
			return
		}
		switch r.URL.Path {
		case "/accounts":
			if r.URL.Query().Get("itemId") != testItem {
				http.Error(w, "item", http.StatusNotFound)
				return
			}
			fmt.Fprint(w, `{"results":[
				{"id":"acc-1","itemId":"`+testItem+`","type":"BANK","subtype":"CHECKING_ACCOUNT","name":"Conta Corrente","marketingName":"Nubank","number":"12345-6","balance":1520.5,"currencyCode":"BRL"},
				{"id":"card-1","itemId":"`+testItem+`","type":"CREDIT","subtype":"CREDIT_CARD","name":"Ultravioleta","number":"9876","balance":830,"currencyCode":"BRL","creditData":{"creditLimit":5000,"availableCreditLimit":4170}}]}`)
		case "/transactions":
			q := r.URL.Query()
			if q.Get("from") == "" || q.Get("pageSize") != "500" {
				http.Error(w, "params", http.StatusBadRequest)
				return
			}
			if q.Get("accountId") == "acc-1" {
				fmt.Fprint(w, `{"totalPages":1,"results":[
					{"date":"2026-11-10T00:00:00.000Z","description":"PIX recebido Maria","amount":300,"type":"CREDIT","category":"Transfer"},
					{"date":"2026-11-11T00:00:00.000Z","description":"Uber *Trip","amount":-23.9,"type":"DEBIT","category":"Taxi"}]}`)
			} else {
				fmt.Fprint(w, `{"totalPages":1,"results":[{"date":"2026-11-09T00:00:00.000Z","description":"Mercado Livre","amount":120,"type":"DEBIT"}]}`)
			}
		case "/bills":
			fmt.Fprint(w, `{"results":[{"dueDate":"2026-11-20T00:00:00.000Z","totalAmount":830,"minimumPaymentAmount":124.5,"totalAmountCurrencyCode":"BRL"}]}`)
		case "/investments":
			fmt.Fprint(w, `{"results":[{"name":"CDB Banco X","type":"FIXED_INCOME","subtype":"CDB","balance":10000,"amountProfit":850,"dueDate":"2028-01-01T00:00:00.000Z"}]}`)
		default:
			http.NotFound(w, r)
		}
	}))
	t.Cleanup(srv.Close)
	return srv, &auths
}

func TestPluggyReadsTheOwnersBanks(t *testing.T) {
	srv, auths := fakePluggy(t)
	p := &Pluggy{BaseURL: srv.URL}
	now := time.Date(2026, 11, 12, 10, 0, 0, 0, time.UTC)
	env := &Env{Cred: &Cred{Extra: map[string]string{"client_id": "cid", "client_secret": "s3cret", "items": testItem}}, Now: func() time.Time { return now }}
	ctx := context.Background()

	account, err := p.Check(ctx, env)
	if err != nil || account != "1 bancos, 2 contas" {
		t.Fatalf("check: %q %v", account, err)
	}
	out, _ := p.Call(ctx, env, "pluggy_contas", Args{})
	if !strings.Contains(out, "Nubank final 3456 (checking_account) id=acc-1: saldo R$ 1520.50") || !strings.Contains(out, "limite R$ 5000.00") {
		t.Fatalf("contas: %s", out)
	}
	out, _ = p.Call(ctx, env, "pluggy_transacoes", Args{})
	if !strings.Contains(out, "3 lançamentos") || !strings.Contains(out, "R$ -120.00  Mercado Livre") || !strings.Contains(out, "saídas R$ -143.90") {
		t.Fatalf("extrato: %s", out)
	}
	out, _ = p.Call(ctx, env, "pluggy_transacoes", Args{"conta": "nubank", "busca": "uber"})
	if !strings.Contains(out, "1 lançamentos") || !strings.Contains(out, "2026-11-11  R$ -23.90  Uber") || strings.Contains(out, "·") {
		t.Fatalf("extrato filtrado: %s", out)
	}
	out, _ = p.Call(ctx, env, "pluggy_faturas", Args{})
	if !strings.Contains(out, "vence 2026-11-20: R$ 830.00 (mínimo R$ 124.50)") {
		t.Fatalf("faturas: %s", out)
	}
	out, _ = p.Call(ctx, env, "pluggy_investimentos", Args{})
	if !strings.Contains(out, "Total: R$ 10000.00") || !strings.Contains(out, "rendeu R$ 850.00") {
		t.Fatalf("investimentos: %s", out)
	}
	if *auths != 1 {
		t.Fatalf("the API key is cached: %d logins", *auths)
	}
	if _, err := p.Call(ctx, env, "pluggy_transacoes", Args{"de": "ontem"}); err == nil {
		t.Fatal("bad date accepted")
	}
	for _, tool := range p.Tools() {
		if tool.Risk != Read {
			t.Fatalf("%s is not read-only", tool.Name)
		}
	}

	bad := &Env{Cred: &Cred{Extra: map[string]string{"client_id": "cid2", "client_secret": "wrong", "items": testItem}}, Now: env.Now}
	if _, err := p.Check(ctx, bad); !IsAuth(err) {
		t.Fatalf("wrong secret: %v", err)
	}
	if _, err := p.Check(ctx, &Env{Cred: &Cred{Extra: map[string]string{"client_id": "c", "client_secret": "s3cret", "items": "../x"}}}); err == nil {
		t.Fatal("bad item accepted")
	}
}
