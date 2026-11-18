package connect

import (
	"io"
	"net/http"
	"net/url"
	"strings"
	"testing"
)

func TestMercadoLivre(t *testing.T) {
	tokens := 0
	f, base := newFake(t, func(w http.ResponseWriter, r *http.Request, body []byte) bool {
		if r.URL.Path == "/oauth/token" {
			form, _ := url.ParseQuery(string(body))
			if form.Get("grant_type") != "client_credentials" || form.Get("client_secret") != "S" {
				w.WriteHeader(400)
				io.WriteString(w, `{"error":"invalid_client"}`)
				return true
			}
			tokens++
			io.WriteString(w, `{"access_token":"APP-1","expires_in":21600}`)
			return true
		}
		if r.Header.Get("Authorization") != "Bearer APP-1" {
			w.WriteHeader(401)
			return true
		}
		switch r.URL.Path {
		case "/products/search":
			io.WriteString(w, `{"results":[{"id":"MLB1","name":"Air Fryer Mondial 4L"},{"id":"MLB2","name":"Air Fryer Philco 5L"}]}`)
		case "/products/MLB1/items":
			io.WriteString(w, `{"results":[{"item_id":"MLB10","price":399.9,"seller_id":7,"condition":"new","shipping":{"free_shipping":true,"logistic_type":"fulfillment"}},
				{"item_id":"MLB11","price":349,"original_price":420,"seller_id":8,"condition":"new","shipping":{"free_shipping":false}}]}`)
		case "/products/MLB2/items":
			io.WriteString(w, `{"results":[{"item_id":"MLB20","price":299.5,"seller_id":7,"shipping":{"free_shipping":true}}]}`)
		case "/products/MLB1":
			io.WriteString(w, `{"id":"MLB1","name":"Air Fryer Mondial 4L","attributes":[{"name":"Capacidade","value_name":"4 L"}]}`)
		case "/users/7":
			io.WriteString(w, `{"nickname":"LOJA_TOP","seller_reputation":{"level_id":"5_green","power_seller_status":"platinum"}}`)
		case "/users/8":
			io.WriteString(w, `{"nickname":"OUTRA"}`)
		case "/sites/MLB/domain_discovery/search":
			io.WriteString(w, `[{"category_id":"MLB123","category_name":"Fritadeiras"}]`)
		case "/highlights/MLB/category/MLB123":
			io.WriteString(w, `{"content":[{"id":"MLB2","type":"PRODUCT","position":1},{"id":"MLB99","type":"ITEM","position":2},{"id":"MLB1","type":"PRODUCT","position":3}]}`)
		case "/products/MLB2":
			io.WriteString(w, `{"id":"MLB2","name":"Air Fryer Philco 5L"}`)
		default:
			return false
		}
		return true
	})
	m := &MercadoLivre{BaseURL: base}
	e := env("")
	e.Cred.Extra = map[string]string{"app_id": "123456", "secret": "S"}
	if acc, err := m.Check(t.Context(), e); err != nil || acc != "app 3456" || !e.Changed {
		t.Fatalf("check: %q %v", acc, err)
	}
	out := call(t, m, e, "ml_buscar", `{"busca": "air fryer", "ordem": "menor_preco"}`)
	if !strings.Contains(out, "a partir de R$ 349,00 (de R$ 420,00) · 2 ofertas, 1 com frete grátis") || strings.Index(out, "Philco") > strings.Index(out, "Mondial") {
		t.Fatalf("buscar: %s", out)
	}
	out = call(t, m, e, "ml_produto", `{"id": "id:mlb1"}`)
	for _, want := range []string{"Capacidade: 4 L", "R$ 349,00 (de R$ 420,00) · novo · vendedor OUTRA", "R$ 399,90 · novo · frete grátis, Full · vendedor LOJA_TOP, reputação verde, MercadoLíder platinum"} {
		if !strings.Contains(out, want) {
			t.Fatalf("missing %q: %s", want, out)
		}
	}
	out = call(t, m, e, "ml_mais_vendidos", `{"tema": "air fryer"}`)
	if !strings.Contains(out, "Fritadeiras") || strings.Index(out, "Philco") > strings.Index(out, "Mondial") || strings.Contains(out, "MLB99") {
		t.Fatalf("mais vendidos: %s", out)
	}
	if tokens != 1 {
		t.Fatalf("the app token is kept: %d tokens", tokens)
	}
	_ = f
	bad := env("")
	bad.Cred.Extra = map[string]string{"app_id": "1", "secret": "x"}
	if _, err := m.Check(t.Context(), bad); !IsAuth(err) {
		t.Fatalf("bad secret: %v", err)
	}
}
