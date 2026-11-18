package connect

import (
	"encoding/json"
	"io"
	"net/http"
	"strings"
	"sync"
	"testing"
	"time"
)

func TestStores(t *testing.T) {
	var mu sync.Mutex
	inputs := map[string]map[string]any{}
	items := map[string]string{
		"riseandcode~apify-mercadolivre-search": `[{"title":"Air Fryer Mondial 4L","price":349,"originalPrice":420,"url":"https://ml/1","seller":{"name":"LOJA X"},"freeShipping":true,"rating":4.7,"reviewsCount":1200,"soldQuantity":"+5mil"}]`,
		"gio21~shopee-scraper": `[{"name":"Air Fryer Philco","price":299.90,"currency":"BRL","originalPrice":399.90,"discountPercent":25,"historicalSoldEstimated":"1k+","reviewCount":830,"rating":4.8,"shopName":"Philco Oficial","url":"https://shopee/2"},
			{"name":"Air Fryer Grande","price":899,"rating":4.9,"reviewCount":10,"url":"https://shopee/9"}]`,
		"piotrv1001~aliexpress-listings-scraper": `[{"title":"Mini air fryer","price":{"value":159.5},"originalPrice":{"value":300},"orders":"2.000+","rating":4.6,"url":"https://ali/3"}]`,
		"junglee~Amazon-crawler":                `[{"title":"Air Fryer Electrolux","price":{"value":379.9,"currency":"R$"},"getPriceBeforeDiscount":{"value":449},"stars":4.5,"reviewsCount":310,"asin":"B0X"}]`,
	}
	_, base := newFake(t, func(w http.ResponseWriter, r *http.Request, body []byte) bool {
		p := r.URL.Path
		switch {
		case strings.HasPrefix(p, "/v2/acts/") && strings.HasSuffix(p, "/runs"):
			act := strings.TrimSuffix(strings.TrimPrefix(p, "/v2/acts/"), "/runs")
			var in map[string]any
			json.Unmarshal(body, &in)
			mu.Lock()
			inputs[act] = in
			mu.Unlock()
			io.WriteString(w, `{"data":{"id":"R-`+act+`","status":"RUNNING","defaultDatasetId":"D-`+act+`"}}`)
		case strings.HasPrefix(p, "/v2/actor-runs/R-"):
			act := strings.TrimPrefix(p, "/v2/actor-runs/R-")
			io.WriteString(w, `{"data":{"id":"R-`+act+`","status":"SUCCEEDED","defaultDatasetId":"D-`+act+`","usageTotalUsd":0.01}}`)
		case strings.HasPrefix(p, "/v2/datasets/D-"):
			io.WriteString(w, items[strings.TrimSuffix(strings.TrimPrefix(p, "/v2/datasets/D-"), "/items")])
		default:
			return false
		}
		return true
	})
	a := &Apify{BaseURL: base, Wait: 5 * time.Second}
	e := env("apify_api_A")

	out := call(t, a, e, "lojas_buscar", `{"loja": "shopee", "busca": "air fryer", "ordem": "menor_preco", "preco_max": 500, "max": 5}`)
	for _, want := range []string{"1 produtos de Shopee", "Air Fryer Philco", "R$ 299,90 (de R$ 399,90) -25%", "nota 4,8 (830)", "1000 vendidos", "Philco Oficial", "https://shopee/2"} {
		if !strings.Contains(out, want) {
			t.Fatalf("missing %q: %s", want, out)
		}
	}
	if strings.Contains(out, "Air Fryer Grande") {
		t.Fatalf("over preco_max: %s", out)
	}
	if in := inputs["gio21~shopee-scraper"]; in["country"] != "BR" || in["maxItems"] != float64(10) {
		t.Fatalf("shopee input: %v", in)
	}

	out = call(t, a, e, "lojas_comparar", `{"busca": "air fryer", "preco_max": 500, "lojas": ["mercadolivre", "shopee", "aliexpress", "amazon"]}`)
	order := []string{"[AliExpress] Mini air fryer", "[Shopee]", "[Mercado Livre]", "[Amazon]"}
	last := -1
	for _, want := range order {
		i := strings.Index(out, want)
		if i < 0 || i < last {
			t.Fatalf("order %q: %s", want, out)
		}
		last = i
	}
	for _, want := range []string{"4 ofertas", "R$ 159,50 (de R$ 300,00)", "2000 vendidos", "frete grátis", "LOJA X", "R$ 379,90 (de R$ 449,00)", "amazon.com.br/dp/B0X", "5000 vendidos"} {
		if !strings.Contains(out, want) {
			t.Fatalf("missing %q: %s", want, out)
		}
	}
	if in := inputs["piotrv1001~aliexpress-listings-scraper"]; in["shipTo"] != "BR" || in["currency"] != "BRL" {
		t.Fatalf("ali input: %v", in)
	}
	if in := inputs["riseandcode~apify-mercadolivre-search"]; in["maxItems"] != float64(48) {
		t.Fatalf("ml input: %v", in)
	}
	// Best rated first: Shopee (4,8) before Amazon (4,5).
	out = call(t, a, e, "lojas_comparar", `{"busca": "air fryer", "ordem": "melhor_avaliados", "preco_max": 500, "lojas": ["shopee", "amazon"]}`)
	if i, j := strings.Index(out, "[Shopee]"), strings.Index(out, "[Amazon]"); i < 0 || j < 0 || i > j {
		t.Fatalf("melhor_avaliados: %s", out)
	}
	if in := inputs["junglee~Amazon-crawler"]; !strings.Contains(toJSON(in), "amazon.com.br/s?k=air+fryer") {
		t.Fatalf("amazon input: %v", in)
	}
	if _, err := a.Call(t.Context(), e, "lojas_buscar", Args{"loja": "magalu", "busca": "x"}); err == nil {
		t.Fatal("unknown store")
	}
}

func toJSON(v any) string {
	b, _ := json.Marshal(v)
	return string(b)
}

func TestAmountOf(t *testing.T) {
	for in, want := range map[string]float64{"R$ 1.234,56": 1234.56, "1,234.56": 1234.56, "299.9": 299.9, "R$ 1.299": 1299, "12,5": 12.5} {
		if got := amountOf(in); got != want {
			t.Errorf("%q: %v", in, got)
		}
	}
}
