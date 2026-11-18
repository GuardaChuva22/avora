package connect

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"math"
	"net/url"
	"regexp"
	"sort"
	"strconv"
	"strings"
	"sync"
	"time"
)

// Marketplace search through chosen Apify robots: the stores' own
// listings, prices and promotions (Mercado Livre, Shopee, AliExpress,
// Amazon), with no seller or affiliate account. Each store has one robot,
// picked on 2026-11-16 for its users, rating and price per result and
// checked against real runs (xtracto/shopee-search gave R$ 1 for a HyperX
// QuadCast in Brazil, so Shopee moved to gio21/shopee-scraper); its input is
// built here and its results read into one shape. The robots' own sorting
// is loose, so the results are sorted and filtered here too.

type storeRobot struct {
	name  string
	actor string
	input func(q, order string, lo, hi float64, n int) map[string]any
	// cost is roughly what one result costs on the free plan, in US$, and
	// fee what a run costs besides (a search fee, the start).
	cost, fee float64
	// minCharge is the least cost cap the robot accepts.
	minCharge float64
	// fetch is how many results the robot must be asked for (the Mercado
	// Livre one takes at least 48); max is the most worth asking (the
	// Shopee one gives the free plan 20 per run).
	fetch func(n int) int
	max   int
}

func (r storeRobot) ask(n int) int {
	if r.max > 0 && n > r.max {
		n = r.max
	}
	if r.fetch != nil {
		return r.fetch(n)
	}
	return n
}

var storeRobots = map[string]storeRobot{
	"mercadolivre": {name: "Mercado Livre", actor: "riseandcode/apify-mercadolivre-search", cost: 0.001, fee: 0.04, minCharge: 0.1,
		fetch: func(n int) int { return max(n, 48) },
		input: func(q, order string, lo, hi float64, n int) map[string]any {
			sortBy := map[string]string{"menor_preco": "price_asc", "maior_preco": "price_desc"}[order]
			return map[string]any{"searchTerms": []string{q}, "country": "BR", "maxItems": n, "sortBy": firstNonEmpty(sortBy, "relevance"), "excludeSponsored": true}
		}},
	"shopee": {name: "Shopee", actor: "gio21/shopee-scraper", cost: 0.0085, fee: 0.01, minCharge: 0.25, max: 20,
		fetch: func(n int) int { return max(n, 10) },
		input: func(q, order string, lo, hi float64, n int) map[string]any {
			return map[string]any{"keywords": []string{q}, "country": "BR", "maxItems": n}
		}},
	"aliexpress": {name: "AliExpress", actor: "piotrv1001/aliexpress-listings-scraper", cost: 0.0025,
		input: func(q, order string, lo, hi float64, n int) map[string]any {
			sortBy := map[string]string{"menor_preco": "priceAsc", "maior_preco": "priceDesc", "mais_vendidos": "orders"}[order]
			in := map[string]any{"searchQueries": []string{q}, "maxResults": n, "shipTo": "BR", "currency": "BRL", "sort": firstNonEmpty(sortBy, "bestMatch")}
			if lo > 0 {
				in["minPrice"] = lo
			}
			if hi > 0 {
				in["maxPrice"] = hi
			}
			return in
		}},
	"amazon": {name: "Amazon", actor: "junglee/Amazon-crawler", cost: 0.005, fee: 0.01,
		input: func(q, order string, lo, hi float64, n int) map[string]any {
			v := url.Values{"k": {q}}
			if s := map[string]string{"menor_preco": "price-asc-rank", "maior_preco": "price-desc-rank", "mais_vendidos": "exact-aware-popularity-rank"}[order]; s != "" {
				v.Set("s", s)
			}
			if lo > 0 || hi > 0 {
				r := "p_36:"
				if lo > 0 {
					r += strconv.Itoa(int(lo * 100))
				}
				r += "-"
				if hi > 0 {
					r += strconv.Itoa(int(math.Ceil(hi) * 100))
				}
				v.Set("rh", r)
			}
			return map[string]any{"categoryOrProductUrls": []map[string]string{{"url": "https://www.amazon.com.br/s?" + v.Encode()}},
				"maxItemsPerStartUrl": n, "maxSearchPagesPerStartUrl": 1, "language": "pt", "countryCode": "BR", "scrapeProductDetails": false,
				// Setting the delivery place costs US$ 0,06 a page.
				"locationDeliverableRoutes": []string{}}
		}},
}

// storeOrder are the stores compared by default. Amazon is left out: its
// robot brought one product per search in the 2026-11-16 tests.
var storeOrder = []string{"mercadolivre", "shopee", "aliexpress"}

func storeKind(s string) string {
	s = strings.ToLower(strings.TrimSpace(s))
	s = strings.NewReplacer(" ", "", "-", "", "_", "", ".com.br", "", ".com", "").Replace(s)
	switch s {
	case "mercadolivre", "ml", "meli", "mercadolibre":
		return "mercadolivre"
	case "shopee":
		return "shopee"
	case "aliexpress", "ali":
		return "aliexpress"
	case "amazon", "amazonbr":
		return "amazon"
	}
	return ""
}

func storeTools() []Tool {
	stores := `"enum": ["mercadolivre", "shopee", "aliexpress", "amazon"]`
	return []Tool{
		{Name: "lojas_buscar", Risk: Write,
			Description: "Pesquisa produtos direto numa loja (Mercado Livre, Shopee, AliExpress ou Amazon Brasil), com os preços, promoções, notas, vendas e vendedores da própria loja. Traz nota, avaliações e vendas também do Mercado Livre (a API oficial do ml_buscar não traz). Roda um robô no Apify (centavos de dólar por busca) e leva de 30 s a 2 min; se passar disso, devolve o id para ler com apify_resultado.",
			Params: `{"type": "object", "properties": {
	"loja": {"type": "string", ` + stores + `},
	"busca": {"type": "string"},
	"ordem": {"type": "string", "enum": ["relevancia", "menor_preco", "maior_preco", "mais_vendidos"]},
	"preco_min": {"type": "number", "description": "Em reais."},
	"preco_max": {"type": "number", "description": "Em reais."},
	"max": {"type": "integer", "description": "Quantos produtos (padrão 15, até 40; Shopee até 20)."}
}, "required": ["loja", "busca"]}`,
			Summary: func(a Args) string {
				return "pesquisou " + storeRobots[storeKind(a.Str("loja"))].name + quoted(a.Str("busca"))
			}},
		{Name: "lojas_comparar", Risk: Write,
			Description: "O jeito certo de pesquisar em várias lojas: uma chamada só roda Mercado Livre, Shopee e AliExpress ao mesmo tempo (Amazon só se pedir em lojas, e ela traz poucos produtos) e junta as ofertas com preço, preço antigo, nota, avaliações, vendas e loja. ordem=custo_beneficio põe primeiro os bem avaliados (nota 4,5+ com 50+ avaliações ou vendas) do mais barato ao mais caro; melhor_avaliados ordena pela nota e pelo número de avaliações. Use buscas curtas e específicas (ex.: \"microfone condensador usb\"). Custa cerca de US$ 0,30 por chamada.",
			Params: `{"type": "object", "properties": {
	"busca": {"type": "string"},
	"lojas": {"type": "array", "items": {"type": "string", ` + stores + `}},
	"preco_min": {"type": "number"},
	"preco_max": {"type": "number"},
	"ordem": {"type": "string", "enum": ["menor_preco", "custo_beneficio", "melhor_avaliados"], "description": "Padrão: menor_preco."},
	"por_loja": {"type": "integer", "description": "Produtos por loja (padrão 12, até 20)."}
}, "required": ["busca"]}`,
			Summary: func(a Args) string { return "comparou lojas" + quoted(a.Str("busca")) }},
	}
}

// offer is one product read from a store robot's result.
type offer struct {
	store, title, url, seller, shipping string
	price, original, rating            float64
	reviews, sold                      int64
	discount                           string
	raw                                string
}

// lookup reads the first present key, dots going into nested objects.
func lookup(m map[string]any, keys ...string) any {
	for _, k := range keys {
		var cur any = m
		for _, part := range strings.Split(k, ".") {
			mm, ok := cur.(map[string]any)
			if !ok {
				cur = nil
				break
			}
			cur = mm[part]
		}
		if cur == nil {
			continue
		}
		if s, ok := cur.(string); ok && strings.TrimSpace(s) == "" {
			continue
		}
		return cur
	}
	return nil
}

func lookupStr(m map[string]any, keys ...string) string {
	switch v := lookup(m, keys...).(type) {
	case string:
		return strings.TrimSpace(v)
	case float64:
		return strconv.FormatFloat(v, 'f', -1, 64)
	case bool:
		if v {
			return "sim"
		}
	case map[string]any:
		return lookupStr(v, "name", "title", "text", "value")
	}
	return ""
}

var moneyRe = regexp.MustCompile(`\d[\d.,]*`)

// money reads 1234.5, "1234.5", "R$ 1.234,56" or {"value": ...}.
func amountOf(v any) float64 {
	switch x := v.(type) {
	case float64:
		return x
	case map[string]any:
		return amountOf(lookup(x, "value", "amount", "current", "price", "min"))
	case string:
		s := moneyRe.FindString(x)
		if s == "" {
			return 0
		}
		lastComma, lastDot := strings.LastIndex(s, ","), strings.LastIndex(s, ".")
		switch {
		case lastComma > lastDot: // 1.234,56
			s = strings.ReplaceAll(s, ".", "")
			s = strings.Replace(s, ",", ".", 1)
		case lastDot > lastComma && strings.Count(s, ".") > 1: // 1.234.567
			s = strings.ReplaceAll(s, ".", "")
		case lastDot > lastComma && lastComma >= 0: // 1,234.56
			s = strings.ReplaceAll(s, ",", "")
		case lastDot > 0 && len(s)-lastDot == 4 && lastComma < 0: // 1.234 (milhar)
			s = strings.ReplaceAll(s, ".", "")
		}
		f, _ := strconv.ParseFloat(strings.TrimRight(s, ".,"), 64)
		return f
	}
	return 0
}

func count(v any) int64 {
	switch x := v.(type) {
	case float64:
		return int64(x)
	case string:
		s := strings.ToLower(x)
		mult := 1.0
		if strings.Contains(s, "mil") || strings.Contains(s, "k") {
			mult = 1000
		}
		f := amountOf(s)
		if mult > 1 && f < 1000 {
			return int64(f * mult)
		}
		return int64(f)
	case map[string]any:
		return count(lookup(x, "count", "total", "value"))
	}
	return 0
}

func readOffer(store string, raw json.RawMessage) offer {
	var m map[string]any
	o := offer{store: store}
	var c bytes.Buffer
	if json.Compact(&c, raw) == nil {
		o.raw = cut(c.String(), 600)
	}
	if json.Unmarshal(raw, &m) != nil {
		return o
	}
	o.title = lookupStr(m, "title", "name", "productName", "product_title", "itemName", "productTitle")
	o.url = lookupStr(m, "url", "link", "productUrl", "product_url", "permalink", "itemUrl", "productLink", "detailUrl")
	o.price = amountOf(lookup(m, "price", "currentPrice", "salePrice", "sale_price", "finalPrice", "priceMin", "price_min", "prices.current", "price.value", "discountPrice", "minPrice"))
	o.original = amountOf(lookup(m, "originalPrice", "original_price", "oldPrice", "old_price", "listPrice", "list_price", "priceBeforeDiscount", "price_before_discount", "regularPrice", "prices.original", "retailPrice", "getPriceBeforeDiscount", "priceMaxBeforeDiscount"))
	o.rating = amountOf(lookup(m, "rating", "stars", "ratingStar", "averageRating", "rating.average", "rating.value", "productRating", "item_rating.rating_star", "evaluateRate"))
	if o.rating > 5 && o.rating <= 100 { // AliExpress: % positive
		o.rating = o.rating / 20
	}
	o.reviews = count(lookup(m, "reviewsCount", "reviewCount", "reviews", "ratingCount", "rating_count", "total_ratings", "ratingsCount", "numberOfReviews", "review_count", "totalReviews", "rating.count", "item_rating.rating_count"))
	o.sold = count(lookup(m, "totalSoldCount", "historicalSoldEstimated", "sold_count", "sold", "soldCount", "sales", "historicalSold", "historical_sold", "orders", "tradeCount", "salesCount", "soldQuantity", "sold_quantity", "totalSold"))
	o.seller = lookupStr(m, "seller.name", "sellerName", "seller", "shopName", "shop_name", "storeName", "store.name", "store", "shop.name", "brand")
	if v := lookup(m, "freeShipping", "isFreeShipping", "free_shipping", "shipping.free_shipping", "shipping.freeShipping"); v == true {
		o.shipping = "frete grátis"
	} else if s := lookupStr(m, "delivery", "shippingText", "shipping"); s != "" && len(s) < 60 {
		o.shipping = s
	}
	if asin := lookupStr(m, "asin"); o.url == "" && asin != "" {
		o.url = "https://www.amazon.com.br/dp/" + asin
	}
	o.discount = lookupStr(m, "discount", "discountPercent", "discount_pct", "discountPercentage", "discount_percentage", "raw_discount")
	if o.original > 0 && (o.original <= o.price || o.original > o.price*6) {
		o.original = 0
	}
	return o
}

func (o offer) line() string {
	if o.title == "" && o.price == 0 {
		return o.raw
	}
	price := ""
	if o.price > 0 {
		price = brl(o.price)
		if o.original > 0 {
			price += fmt.Sprintf(" (de %s)", brl(o.original))
		}
	}
	if o.discount != "" && !strings.HasPrefix(o.discount, "0") {
		d := o.discount
		if !strings.Contains(d, "%") {
			d += "%"
		}
		price = joinNonEmpty(" ", price, "-"+strings.TrimPrefix(d, "-"))
	}
	rating := ""
	if o.rating > 0 {
		rating = strings.Replace(fmt.Sprintf("nota %.1f", o.rating), ".", ",", 1)
		if o.reviews > 0 {
			rating += fmt.Sprintf(" (%d)", o.reviews)
		}
	}
	sold := ""
	if o.sold > 0 {
		sold = fmt.Sprintf("%d vendidos", o.sold)
	}
	return fmt.Sprintf("%s\n   %s\n   %s", cut(o.title, 160), joinNonEmpty(" · ", price, rating, sold, o.shipping, cut(o.seller, 40)), o.url)
}

type storeRun struct {
	kind   string
	run    apifyRun
	offers []offer
	err    error
}

// startStore starts a store robot capped by items and cost.
func (a *Apify) startStore(ctx context.Context, env *Env, kind, q, order string, lo, hi float64, n int) (apifyRun, error) {
	r := storeRobots[kind]
	ask := r.ask(n)
	maxCost := math.Max(math.Max(0.1, r.minCharge), math.Min(2, float64(ask)*r.cost*1.5+r.fee+0.05))
	v := url.Values{"maxItems": {strconv.Itoa(ask)}, "maxTotalChargeUsd": {strconv.FormatFloat(maxCost, 'f', 2, 64)}}
	var out struct {
		Data apifyRun `json:"data"`
	}
	err := a.do(ctx, env, "POST", "/v2/acts/"+actorPath(r.actor)+"/runs?"+v.Encode(), r.input(q, order, lo, hi, ask), &out)
	return out.Data, err
}

// waitStores waits for the runs until the deadline and reads the finished.
func (a *Apify) waitStores(ctx context.Context, env *Env, runs []*storeRun, n int) {
	deadline := time.Now().Add(a.wait())
	var wg sync.WaitGroup
	for _, sr := range runs {
		if sr.err != nil {
			continue
		}
		wg.Add(1)
		go func(sr *storeRun) {
			defer wg.Done()
			for !sr.run.done() && time.Now().Before(deadline) {
				secs := int(math.Min(60, time.Until(deadline).Seconds()))
				if secs < 1 {
					break
				}
				var out struct {
					Data apifyRun `json:"data"`
				}
				if sr.err = a.do(ctx, env, "GET", fmt.Sprintf("/v2/actor-runs/%s?waitForFinish=%d", url.PathEscape(sr.run.ID), secs), nil, &out); sr.err != nil {
					return
				}
				sr.run = out.Data
			}
			if !sr.run.done() || sr.run.Dataset == "" {
				return
			}
			data, _, err := do(ctx, request{URL: a.base() + "/v2/datasets/" + url.PathEscape(sr.run.Dataset) + "/items?" + url.Values{"clean": {"true"}, "limit": {strconv.Itoa(n)}, "format": {"json"}}.Encode(),
				Header: map[string]string{"Authorization": "Bearer " + strings.TrimSpace(env.Cred.Token)}})
			if err != nil {
				sr.err = err
				return
			}
			var items []json.RawMessage
			if json.Unmarshal(data, &items) != nil {
				return
			}
			for _, it := range items {
				sr.offers = append(sr.offers, readOffer(sr.kind, it))
			}
		}(sr)
	}
	wg.Wait()
}

func (sr *storeRun) status(conn string) string {
	name := storeRobots[sr.kind].name
	switch {
	case sr.err != nil:
		return fmt.Sprintf("%s: %v", name, sr.err)
	case !sr.run.done():
		return fmt.Sprintf("%s: ainda rodando (leia depois com apify_resultado, execucao=%s)", name, runRef(sr.run.ID, conn))
	case sr.run.Status != "SUCCEEDED" && len(sr.offers) == 0:
		return fmt.Sprintf("%s: o robô %s (%s)", name, firstNonEmpty(runStatus[sr.run.Status], sr.run.Status), cut(sr.run.Message, 160))
	case len(sr.offers) == 0:
		return name + ": nada encontrado"
	}
	return ""
}

func storeArgs(a Args) (q, order string, lo, hi float64, err error) {
	if q, err = a.Need("busca"); err != nil {
		return
	}
	order = a.Str("ordem")
	lo, _ = a["preco_min"].(float64)
	hi, _ = a["preco_max"].(float64)
	return
}

func (a *Apify) storeSearch(ctx context.Context, env *Env, x Args) (string, error) {
	kind := storeKind(x.Str("loja"))
	if kind == "" {
		return "", fmt.Errorf("loja desconhecida %q: use mercadolivre, shopee, aliexpress ou amazon", x.Str("loja"))
	}
	q, order, lo, hi, err := storeArgs(x)
	if err != nil {
		return "", err
	}
	n := x.Int("max", 15, 1, 40)
	run, err := a.startStore(ctx, env, kind, q, order, lo, hi, n)
	if err != nil {
		return "", err
	}
	sr := &storeRun{kind: kind, run: run}
	a.waitStores(ctx, env, []*storeRun{sr}, n)
	if s := sr.status(env.Conn); s != "" {
		if sr.err != nil {
			return "", sr.err
		}
		return s, nil
	}
	sr.offers = inRange(sr.offers, lo, hi)
	sortOffers(sr.offers, order)
	var b strings.Builder
	for i, o := range sr.offers {
		fmt.Fprintf(&b, "%d. %s\n", i+1, o.line())
	}
	head := fmt.Sprintf("%d produtos de %s para %q (custo US$ %.3f)", len(sr.offers), storeRobots[kind].name, q, sr.run.Cost)
	return withPart(head, b.String(), x.Int("parte", 1, 1, 50)), nil
}

func inRange(list []offer, lo, hi float64) []offer {
	out := []offer{}
	for _, o := range list {
		if o.price == 0 || ((lo == 0 || o.price >= lo) && (hi == 0 || o.price <= hi)) {
			out = append(out, o)
		}
	}
	return out
}

func (a *Apify) storeCompare(ctx context.Context, env *Env, x Args) (string, error) {
	q, order, lo, hi, err := storeArgs(x)
	if err != nil {
		return "", err
	}
	kinds := []string{}
	for _, s := range x.List("lojas") {
		if k := storeKind(s); k != "" && !slicesContains(kinds, k) {
			kinds = append(kinds, k)
		}
	}
	if len(kinds) == 0 {
		kinds = storeOrder
	}
	n := x.Int("por_loja", 12, 1, 20)
	runs := make([]*storeRun, len(kinds))
	var wg sync.WaitGroup
	var creditErr error
	var mu sync.Mutex
	for i, k := range kinds {
		runs[i] = &storeRun{kind: k}
		wg.Add(1)
		go func(sr *storeRun) {
			defer wg.Done()
			sr.run, sr.err = a.startStore(ctx, env, sr.kind, q, "relevancia", lo, hi, n)
			if IsCredit(sr.err) {
				mu.Lock()
				creditErr = sr.err
				mu.Unlock()
			}
		}(runs[i])
	}
	wg.Wait()
	if creditErr != nil && !slicesContainsFunc(runs, func(sr *storeRun) bool { return sr.err == nil }) {
		return "", creditErr
	}
	a.waitStores(ctx, env, runs, n)
	all := []offer{}
	notes := []string{}
	cost := 0.0
	for _, sr := range runs {
		cost += sr.run.Cost
		if s := sr.status(env.Conn); s != "" {
			notes = append(notes, s)
			continue
		}
		all = append(all, inRange(sr.offers, lo, hi)...)
	}
	if order == "" || order == "relevancia" {
		order = "menor_preco"
	}
	sortOffers(all, order)
	var b strings.Builder
	for i, o := range all {
		fmt.Fprintf(&b, "%d. [%s] %s\n", i+1, storeRobots[o.store].name, o.line())
	}
	for _, s := range notes {
		b.WriteString("Obs.: " + s + "\n")
	}
	how := map[string]string{"menor_preco": "do mais barato ao mais caro", "custo_beneficio": "os bem avaliados primeiro, do mais barato ao mais caro",
		"melhor_avaliados": "dos melhor avaliados aos piores", "maior_preco": "do mais caro ao mais barato", "mais_vendidos": "dos mais vendidos aos menos"}[order]
	head := fmt.Sprintf("%d ofertas para %q em %d lojas, %s (custo US$ %.3f)", len(all), q, len(kinds), how, cost)
	return withPart(head, b.String(), x.Int("parte", 1, 1, 50)), nil
}

func slicesContains(list []string, s string) bool {
	for _, x := range list {
		if x == s {
			return true
		}
	}
	return false
}

func slicesContainsFunc(list []*storeRun, f func(*storeRun) bool) bool {
	for _, x := range list {
		if f(x) {
			return true
		}
	}
	return false
}

// StoreApp is a store shown as its own app (its name, logo and @) whose
// searches run on the Apify robots and accounts: Shopee and AliExpress only
// open their APIs to approved affiliates and sellers.
type StoreApp struct {
	Apify *Apify
	Store string
}

func (s *StoreApp) Info() Info {
	name := storeRobots[s.Store].name
	return Info{Kind: s.Store, Name: name, Ready: true, Category: "Compras", Auth: "token", Apps: []string{name}, Via: "apify",
		Description: "Produtos da " + name + " com os preços, promoções, notas e vendas da própria loja, sem conta de vendedor nem de afiliado. Usa o crédito do Apify.",
		Help:        "Funciona com a conta do Apify: conecte o Apify em Plugins e a " + name + " já fica pronta."}
}

func (s *StoreApp) Tools() []Tool {
	name := storeRobots[s.Store].name
	return []Tool{{Name: s.Store + "_buscar", Risk: Write,
		Description: "Pesquisa produtos na " + name + " (Brasil) com os preços, preços antigos, descontos, notas, vendas e lojas da própria " + name + ". Roda um robô no Apify e gasta um pouco de crédito por produto; para comparar com outras lojas use lojas_comparar.",
		Params: `{"type": "object", "properties": {
	"busca": {"type": "string"},
	"ordem": {"type": "string", "enum": ["relevancia", "menor_preco", "maior_preco", "mais_vendidos"]},
	"preco_min": {"type": "number", "description": "Em reais."},
	"preco_max": {"type": "number", "description": "Em reais."},
	"max": {"type": "integer", "description": "Quantos produtos (padrão 15, até 40; Shopee até 20)."}
}, "required": ["busca"]}`,
		Summary: func(a Args) string { return "pesquisou na " + name + quoted(a.Str("busca")) }}}
}

// Pooled: the searches go to the Apify account with the most credit.
func (s *StoreApp) Pooled() {}

func (s *StoreApp) Check(ctx context.Context, env *Env) (string, error) {
	return s.Apify.Check(ctx, env)
}

func (s *StoreApp) Usage(ctx context.Context, env *Env) (Usage, error) {
	return s.Apify.Usage(ctx, env)
}

func (s *StoreApp) Call(ctx context.Context, env *Env, tool string, x Args) (string, error) {
	if tool != s.Store+"_buscar" {
		return "", fmt.Errorf("ferramenta desconhecida: %s", tool)
	}
	y := Args{}
	for k, v := range x {
		y[k] = v
	}
	y["loja"] = s.Store
	return s.Apify.storeSearch(ctx, env, y)
}

// trusted: rated 4.5 or more by at least 50 buyers (reviews or sales).
func (o offer) trusted() bool {
	return o.rating >= 4.5 && max(o.reviews, o.sold) >= 50
}

// sortOffers orders the offers the way asked; offers without a price go
// last. The robots' own orders are loose, so this is the order shown.
func sortOffers(list []offer, order string) {
	priced := func(i, j int) (bool, bool) {
		if (list[i].price > 0) != (list[j].price > 0) {
			return list[i].price > 0, true
		}
		return false, false
	}
	sort.SliceStable(list, func(i, j int) bool {
		if r, ok := priced(i, j); ok {
			return r
		}
		a, b := list[i], list[j]
		switch order {
		case "menor_preco":
			return a.price < b.price
		case "maior_preco":
			return a.price > b.price
		case "mais_vendidos":
			return a.sold > b.sold
		case "melhor_avaliados":
			if a.rating != b.rating {
				return a.rating > b.rating
			}
			return max(a.reviews, a.sold) > max(b.reviews, b.sold)
		case "custo_beneficio":
			if a.trusted() != b.trusted() {
				return a.trusted()
			}
			return a.price < b.price
		}
		return false
	})
}
