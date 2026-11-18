package connect

import (
	"io"
	"net/http"
	"strings"
	"testing"
	"time"
)

func TestApify(t *testing.T) {
	polls := 0
	f, base := newFake(t, func(w http.ResponseWriter, r *http.Request, body []byte) bool {
		if r.Header.Get("Authorization") != "Bearer apify_api_A" {
			w.WriteHeader(401)
			io.WriteString(w, `{"error":{"type":"token-not-valid"}}`)
			return true
		}
		switch {
		case r.URL.Path == "/v2/users/me":
			io.WriteString(w, `{"data":{"username":"gui","plan":{"id":"FREE"}}}`)
		case r.URL.Path == "/v2/users/me/limits":
			io.WriteString(w, `{"data":{"monthlyUsageCycle":{"endAt":"2026-12-01T00:00:00Z"},"limits":{"maxMonthlyUsageUsd":5},"current":{"monthlyUsageUsd":1.25}}}`)
		case r.URL.Path == "/v2/store":
			io.WriteString(w, `{"data":{"items":[{"name":"tiktok-scraper","username":"clockworks","title":"TikTok Scraper","description":"Videos","stats":{"totalUsers":90000},
				"currentPricingInfo":{"pricingModel":"PRICE_PER_DATASET_ITEM","pricePerUnitUsd":0.004,"unitName":"result"}},
				{"name":"rent","username":"x","title":"Rental","currentPricingInfo":{"pricingModel":"FLAT_PRICE_PER_MONTH","pricePerUnitUsd":30}}]}}`)
		case r.URL.Path == "/v2/acts/clockworks~tiktok-scraper":
			io.WriteString(w, `{"data":{"title":"TikTok Scraper","description":"Videos","exampleRunInput":{"body":"{\"profiles\":[\"x\"]}"}}}`)
		case r.URL.Path == "/v2/acts/clockworks~tiktok-scraper/builds/default":
			io.WriteString(w, `{"data":{"actorDefinition":{"input":{"properties":{"profiles":{"type":"array","title":"Perfis","prefill":["khaby.lame"]}},"required":["profiles"]}}}}`)
		case r.URL.Path == "/v2/acts/clockworks~tiktok-scraper/runs":
			if r.URL.Query().Get("maxItems") != "5" || r.URL.Query().Get("maxTotalChargeUsd") != "0.50" || !strings.Contains(string(body), `"profiles":["gui"]`) {
				t.Errorf("run: %s %s", r.URL.RawQuery, body)
			}
			io.WriteString(w, `{"data":{"id":"RUN1","status":"RUNNING","defaultDatasetId":"DS1"}}`)
		case r.URL.Path == "/v2/actor-runs/RUN1":
			polls++
			io.WriteString(w, `{"data":{"id":"RUN1","status":"SUCCEEDED","defaultDatasetId":"DS1","usageTotalUsd":0.02}}`)
		case r.URL.Path == "/v2/datasets/DS1/items":
			io.WriteString(w, `[{"text":"vídeo 1","playCount":100},{"text":"vídeo 2","playCount":50}]`)
		case r.URL.Path == "/v2/acts/paid~one/runs":
			w.WriteHeader(402)
			io.WriteString(w, `{"error":{"type":"not-enough-usage-to-run-paid-actor","message":"You have exceeded your monthly usage limit"}}`)
		default:
			return false
		}
		return true
	})
	a := &Apify{BaseURL: base, Wait: 5 * time.Second}
	e := env("apify_api_A")
	e.Conn = "conn9"
	if acc, err := a.Check(t.Context(), e); err != nil || acc != "gui" {
		t.Fatalf("check: %q %v", acc, err)
	}
	if u, err := a.Usage(t.Context(), e); err != nil || u.Used != 125 || u.Limit != 500 || u.ResetsAt == 0 {
		t.Fatalf("usage: %+v %v", u, err)
	}
	out := call(t, a, e, "apify_buscar_robos", `{"busca": "tiktok"}`)
	if !strings.Contains(out, "clockworks/tiktok-scraper") || !strings.Contains(out, "US$ 4.00 por mil results") || !strings.Contains(out, "aluguel") {
		t.Fatalf("store: %s", out)
	}
	out = call(t, a, e, "apify_robo", `{"robo": "clockworks/tiktok-scraper"}`)
	if !strings.Contains(out, "profiles: array (obrigatório)") || !strings.Contains(out, "khaby.lame") || !strings.Contains(out, "Exemplo de entrada") {
		t.Fatalf("actor: %s", out)
	}
	out = call(t, a, e, "apify_rodar", `{"robo": "clockworks/tiktok-scraper", "entrada": {"profiles": ["gui"]}, "max_itens": 5}`)
	if !strings.Contains(out, "RUN1@conn9") || !strings.Contains(out, "terminou") || !strings.Contains(out, `"playCount":100`) || polls != 1 {
		t.Fatalf("rodar: %s (%d polls)", out, polls)
	}
	if got := a.RouteOf("apify_resultado", Args{"execucao": "RUN1@conn9"}); got != "conn9" {
		t.Fatalf("route: %q", got)
	}
	if out := call(t, a, e, "apify_resultado", `{"execucao": "RUN1@conn9"}`); !strings.Contains(out, "2 resultados") {
		t.Fatalf("resultado: %s", out)
	}
	if _, err := a.Call(t.Context(), e, "apify_rodar", Args{"robo": "paid/one"}); !IsCredit(err) {
		t.Fatalf("credit: %v", err)
	}
	_ = f
	if _, err := a.Check(t.Context(), env("errado")); !IsAuth(err) {
		t.Fatalf("bad token: %v", err)
	}
}
