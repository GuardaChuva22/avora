package connect

import (
	"io"
	"net/http"
	"strings"
	"testing"
)

func TestProducts(t *testing.T) {
	f, base := newFake(t, func(w http.ResponseWriter, r *http.Request, body []byte) bool {
		q := r.URL.Query()
		if q.Get("api_key") != "SP" {
			w.WriteHeader(401)
			io.WriteString(w, `{"error":"Invalid API key."}`)
			return true
		}
		switch {
		case r.URL.Path == "/account.json":
			io.WriteString(w, `{"plan_name":"Free Plan","searches_per_month":250,"this_month_usage":3,"total_searches_left":247}`)
		case q.Get("engine") == "google_shopping":
			io.WriteString(w, `{"shopping_results":[
				{"title":"Air Fryer Mondial 4L","source":"Mercado Livre","price":"R$ 349,00","extracted_price":349,"old_price":"R$ 420,00","rating":4.7,"reviews":1200,"delivery":"Frete grátis","multiple_sources":true,"immersive_product_page_token":"TOKEN-A"},
				{"title":"Air Fryer Philco","source":"Shopee","price":"R$ 299,90","extracted_price":299.9,"immersive_product_page_token":"TOKEN-B"}]}`)
		case q.Get("engine") == "google_immersive_product" && q.Get("page_token") == "TOKEN-B":
			io.WriteString(w, `{"product_results":{"title":"Air Fryer Philco 5L","brand":"Philco","rating":4.5,"reviews":800,
				"stores":[{"name":"Shopee","price":"R$ 299,90","shipping":"Frete grátis","tag":"Melhor preço","link":"https://shopee.com.br/x"},{"name":"Amazon","price":"R$ 319,00","original_price":"R$ 399,00","discount":"20% off","link":"https://amazon.com.br/x"}],
				"about_the_product":{"features":[{"title":"Capacidade","value":"5 L"}]},"ratings":[{"stars":5,"amount":600},{"stars":1,"amount":40}],
				"user_reviews":[{"user_name":"Ana","rating":5,"text":"Esquenta rápido","date":"há 1 mês","source":"Amazon"}]}}`)
		default:
			return false
		}
		return true
	})
	p := &Products{BaseURL: base}
	e := env("SP")
	if acc, err := p.Check(t.Context(), e); err != nil || !strings.Contains(acc, "247 buscas") {
		t.Fatalf("check: %q %v", acc, err)
	}
	out := call(t, p, e, "produtos_buscar", `{"busca": "air fryer", "ordem": "menor_preco", "preco_max": 400, "frete_gratis": true}`)
	for _, want := range []string{"R$ 349,00 (de R$ 420,00) · Mercado Livre e outras lojas · nota 4,7 (1200 avaliações) · Frete grátis", "Shopee", "id:p1-2"} {
		if !strings.Contains(out, want) {
			t.Fatalf("missing %q: %s", want, out)
		}
	}
	if last := f.calls[len(f.calls)-1]; !strings.Contains(last, "sort_by=1") || !strings.Contains(last, "max_price=400") || !strings.Contains(last, "free_shipping=true") || !strings.Contains(last, "gl=br") {
		t.Fatalf("query: %s", last)
	}
	out = call(t, p, e, "produtos_comparar", `{"id": "id:p1-2"}`)
	for _, want := range []string{"Lojas (2)", "Shopee: R$ 299,90 · Frete grátis · Melhor preço", "Amazon: R$ 319,00 (de R$ 399,00) · 20% off", "Capacidade: 5 L", "5★ 600", "Esquenta rápido"} {
		if !strings.Contains(out, want) {
			t.Fatalf("missing %q: %s", want, out)
		}
	}
	if _, err := p.Call(t.Context(), e, "produtos_comparar", Args{"id": "p9-9"}); err == nil {
		t.Fatal("unknown id")
	}
	if _, err := p.Check(t.Context(), env("x")); !IsAuth(err) {
		t.Fatalf("bad key: %v", err)
	}
}
