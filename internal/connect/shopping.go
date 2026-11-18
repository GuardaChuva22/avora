package connect

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"math"
	"net/url"
	"sort"
	"strconv"
	"strings"
	"sync"
)

// Shopping: Mercado Livre through its official API with the owner's app
// keys. Shopee and AliExpress ask for affiliate or seller accounts, so they
// run on the Apify robots instead (stores.go). Nothing is bought.

func brl(v float64) string { return pixReais(int64(math.Round(v * 100))) }

func num(v any) float64 {
	switch x := v.(type) {
	case float64:
		return x
	case string:
		f, _ := strconv.ParseFloat(strings.TrimSpace(x), 64)
		return f
	case json.Number:
		f, _ := x.Float64()
		return f
	}
	return 0
}

// --- Mercado Livre -------------------------------------------------------

// MercadoLivre searches the catalog with an app token (client
// credentials): Mercado Livre closed its public listing search to most
// apps, but the catalog products and their offers still answer.
type MercadoLivre struct {
	BaseURL string
}

func (m *MercadoLivre) base() string {
	if m.BaseURL != "" {
		return strings.TrimRight(m.BaseURL, "/")
	}
	return "https://api.mercadolibre.com"
}

func (m *MercadoLivre) Info() Info {
	return Info{Kind: "mercadolivre", Name: "Mercado Livre", Ready: true, Category: "Compras", Auth: "token", Apps: []string{"Mercado Livre"},
		Description: "Pesquisa de produtos no Mercado Livre: os agentes buscam no catálogo, comparam as ofertas de cada produto (preço, frete grátis, Full, vendedor e reputação), veem a ficha técnica e os mais vendidos de uma categoria.",
		Help: "Em developers.mercadolivre.com.br → Minhas aplicações → Criar aplicação (é grátis, com a sua conta do Mercado Livre): dê um nome, use https://localhost como URI de redirect, marque leitura e salve. " +
			"Cole o App ID (ID do aplicativo) e a Secret Key (chave secreta). Os agentes só pesquisam: nada é comprado nem anunciado.",
		HelpURL: "https://developers.mercadolivre.com.br/devcenter",
		Fields: []Field{
			{Key: "app_id", Label: "App ID (ID do aplicativo)"},
			{Key: "secret", Label: "Secret Key (chave secreta)", Secret: true},
		}}
}

func (m *MercadoLivre) Tools() []Tool {
	return []Tool{
		{Name: "ml_buscar", Risk: Read,
			Description: "Busca produtos no catálogo do Mercado Livre e traz, de cada um, o menor preço entre as ofertas, quantas ofertas há, se tem frete grátis ou Full, o link e o id (use em ml_produto). Não traz nota nem número de avaliações: para isso, ou para comparar com outras lojas, use lojas_comparar.",
			Params: `{"type": "object", "properties": {
	"busca": {"type": "string", "description": "O que procurar, ex.: iphone 15 128gb, air fryer 5 litros."},
	"max": {"type": "integer", "description": "Quantos produtos (padrão 8, até 15)."},
	"ordem": {"type": "string", "enum": ["relevancia", "menor_preco"]}
}, "required": ["busca"]}`,
			Summary: func(a Args) string { return "buscou no Mercado Livre" + quoted(a.Str("busca")) }},
		{Name: "ml_produto", Risk: Read,
			Description: "Ficha de um produto do catálogo do Mercado Livre pelo id (MLB...): nome, ficha técnica e as ofertas da mais barata para a mais cara, com condição, frete, Full, vendedor e reputação.",
			Params: `{"type": "object", "properties": {
	"id": {"type": "string"}
}, "required": ["id"]}`,
			Summary: func(a Args) string { return "produto do Mercado Livre " + a.Str("id") }},
		{Name: "ml_mais_vendidos", Risk: Read,
			Description: "Os mais vendidos do Mercado Livre na categoria de um produto ou tema (ex.: fone bluetooth, panela elétrica).",
			Params: `{"type": "object", "properties": {
	"tema": {"type": "string"},
	"max": {"type": "integer", "description": "Padrão 10, até 20."}
}, "required": ["tema"]}`,
			Summary: func(a Args) string { return "mais vendidos do Mercado Livre" + quoted(a.Str("tema")) }},
	}
}

// token is an app token (client credentials), kept with the credential
// until it expires.
func (m *MercadoLivre) token(ctx context.Context, env *Env) (string, error) {
	x := env.Cred.Extra
	if exp, err := strconv.ParseInt(x["token_exp"], 10, 64); err == nil && x["token"] != "" && env.now().Unix() < exp-120 {
		return x["token"], nil
	}
	id, secret := strings.TrimSpace(x["app_id"]), strings.TrimSpace(x["secret"])
	if id == "" || secret == "" {
		return "", &AuthError{Msg: "faltam o App ID e a Secret Key do Mercado Livre"}
	}
	form := url.Values{"grant_type": {"client_credentials"}, "client_id": {id}, "client_secret": {secret}}
	var t struct {
		Token   string `json:"access_token"`
		Expires int64  `json:"expires_in"`
	}
	err := callJSON(ctx, request{Method: "POST", URL: m.base() + "/oauth/token", Body: []byte(form.Encode()),
		ContentType: "application/x-www-form-urlencoded", Header: map[string]string{"Accept": "application/json"}}, &t)
	var ae *APIError
	if errors.As(err, &ae) && ae.Status < 500 {
		return "", &AuthError{Msg: "o Mercado Livre recusou o App ID ou a Secret Key; confira em Plugins"}
	}
	if err != nil {
		return "", err
	}
	if t.Expires <= 0 {
		t.Expires = 6 * 3600
	}
	x["token"], x["token_exp"] = t.Token, strconv.FormatInt(env.now().Unix()+t.Expires, 10)
	env.Changed = true
	return t.Token, nil
}

func (m *MercadoLivre) get(ctx context.Context, env *Env, path string, out any) error {
	tok, err := m.token(ctx, env)
	if err != nil {
		return err
	}
	err = callJSON(ctx, request{URL: m.base() + path, Header: map[string]string{"Authorization": "Bearer " + tok}}, out)
	if IsAuth(err) {
		// The app token expired early: take a new one once.
		env.Cred.Extra["token_exp"] = "0"
		if tok, err = m.token(ctx, env); err != nil {
			return err
		}
		err = callJSON(ctx, request{URL: m.base() + path, Header: map[string]string{"Authorization": "Bearer " + tok}}, out)
	}
	return err
}

func (m *MercadoLivre) Check(ctx context.Context, env *Env) (string, error) {
	if env.Cred.Extra == nil {
		env.Cred.Extra = map[string]string{}
	}
	var out struct {
		Results []json.RawMessage `json:"results"`
	}
	if err := m.get(ctx, env, "/products/search?site_id=MLB&status=active&limit=1&q=celular", &out); err != nil {
		return "", err
	}
	return "app " + tail(env.Cred.Extra["app_id"]), nil
}

type mlProduct struct {
	ID         string `json:"id"`
	Name       string `json:"name"`
	Domain     string `json:"domain_id"`
	Attributes []struct {
		Name  string `json:"name"`
		Value string `json:"value_name"`
	} `json:"attributes"`
	Short struct {
		Content string `json:"content"`
	} `json:"short_description"`
}

type mlOffer struct {
	ItemID    string  `json:"item_id"`
	Price     float64 `json:"price"`
	Original  float64 `json:"original_price"`
	Condition string  `json:"condition"`
	SellerID  int64   `json:"seller_id"`
	Official  int64   `json:"official_store_id"`
	Shipping  struct {
		Free     bool   `json:"free_shipping"`
		Logistic string `json:"logistic_type"`
	} `json:"shipping"`
}

func (o mlOffer) shipping() string {
	parts := []string{}
	if o.Shipping.Free {
		parts = append(parts, "frete grátis")
	}
	if o.Shipping.Logistic == "fulfillment" {
		parts = append(parts, "Full")
	}
	return strings.Join(parts, ", ")
}

func (m *MercadoLivre) offers(ctx context.Context, env *Env, id string, limit int) ([]mlOffer, error) {
	var out struct {
		Results []mlOffer `json:"results"`
	}
	err := m.get(ctx, env, "/products/"+url.PathEscape(id)+"/items?limit="+strconv.Itoa(limit), &out)
	sort.SliceStable(out.Results, func(i, j int) bool { return out.Results[i].Price < out.Results[j].Price })
	return out.Results, err
}

func mlLink(id string) string { return "https://www.mercadolivre.com.br/p/" + id }

func (m *MercadoLivre) Call(ctx context.Context, env *Env, tool string, a Args) (string, error) {
	if env.Cred.Extra == nil {
		env.Cred.Extra = map[string]string{}
	}
	switch tool {
	case "ml_buscar":
		return m.search(ctx, env, a)
	case "ml_produto":
		return m.product(ctx, env, a)
	case "ml_mais_vendidos":
		return m.best(ctx, env, a)
	}
	return "", fmt.Errorf("ferramenta desconhecida: %s", tool)
}

type mlRow struct {
	p      mlProduct
	offers []mlOffer
	err    error
}

// withOffers reads the offers of each product, a few at a time.
func (m *MercadoLivre) withOffers(ctx context.Context, env *Env, products []mlProduct) []*mlRow {
	rows := make([]*mlRow, len(products))
	var wg sync.WaitGroup
	sem := make(chan struct{}, 5)
	// The app token is taken once before the parallel reads.
	if _, err := m.token(ctx, env); err != nil {
		for i, p := range products {
			rows[i] = &mlRow{p: p, err: err}
		}
		return rows
	}
	for i, p := range products {
		rows[i] = &mlRow{p: p}
		wg.Add(1)
		go func(r *mlRow) {
			defer wg.Done()
			sem <- struct{}{}
			defer func() { <-sem }()
			r.offers, r.err = m.offers(ctx, env, r.p.ID, 50)
		}(rows[i])
	}
	wg.Wait()
	return rows
}

func (r *mlRow) line(n int) string {
	var b strings.Builder
	fmt.Fprintf(&b, "%d. %s\n", n, r.p.Name)
	switch {
	case r.err != nil || len(r.offers) == 0:
		b.WriteString("   sem ofertas ativas no momento\n")
	default:
		o := r.offers[0]
		price := "a partir de " + brl(o.Price)
		if o.Original > o.Price {
			price += fmt.Sprintf(" (de %s)", brl(o.Original))
		}
		free := 0
		for _, x := range r.offers {
			if x.Shipping.Free {
				free++
			}
		}
		extra := fmt.Sprintf("%d ofertas", len(r.offers))
		if len(r.offers) == 1 {
			extra = "1 oferta"
		}
		if free > 0 {
			extra += fmt.Sprintf(", %d com frete grátis", free)
		}
		fmt.Fprintf(&b, "   %s · %s\n", price, extra)
	}
	fmt.Fprintf(&b, "   id:%s  %s\n", r.p.ID, mlLink(r.p.ID))
	return b.String()
}

func (m *MercadoLivre) search(ctx context.Context, env *Env, a Args) (string, error) {
	q, err := a.Need("busca")
	if err != nil {
		return "", err
	}
	n := a.Int("max", 8, 1, 15)
	var out struct {
		Results []mlProduct `json:"results"`
	}
	v := url.Values{"site_id": {"MLB"}, "status": {"active"}, "q": {q}, "limit": {strconv.Itoa(n)}}
	if err := m.get(ctx, env, "/products/search?"+v.Encode(), &out); err != nil {
		return "", err
	}
	if len(out.Results) == 0 {
		return fmt.Sprintf("Nada no catálogo do Mercado Livre para %q.", q), nil
	}
	rows := m.withOffers(ctx, env, out.Results)
	if a.Str("ordem") == "menor_preco" {
		sort.SliceStable(rows, func(i, j int) bool {
			if len(rows[i].offers) == 0 || len(rows[j].offers) == 0 {
				return len(rows[i].offers) > 0
			}
			return rows[i].offers[0].Price < rows[j].offers[0].Price
		})
	}
	var b strings.Builder
	fmt.Fprintf(&b, "%d produtos do Mercado Livre para %q:\n", len(rows), q)
	for i, r := range rows {
		b.WriteString(r.line(i + 1))
	}
	return b.String(), nil
}

func (m *MercadoLivre) product(ctx context.Context, env *Env, a Args) (string, error) {
	id, err := a.Need("id")
	if err != nil {
		return "", err
	}
	id = strings.ToUpper(strings.TrimPrefix(strings.TrimSpace(id), "id:"))
	var p mlProduct
	if err := m.get(ctx, env, "/products/"+url.PathEscape(id), &p); err != nil {
		var ae *APIError
		if errors.As(err, &ae) && ae.Status == 404 {
			return "", fmt.Errorf("o Mercado Livre não tem o produto %s no catálogo (use o id de ml_buscar)", id)
		}
		return "", err
	}
	offers, _ := m.offers(ctx, env, id, 50)
	sellers := m.sellers(ctx, env, offers, 8)
	var b strings.Builder
	fmt.Fprintf(&b, "%s\n%s\n", p.Name, mlLink(p.ID))
	if p.Short.Content != "" {
		b.WriteString(cut(p.Short.Content, 600) + "\n")
	}
	if len(p.Attributes) > 0 {
		b.WriteString("Ficha técnica:\n")
		for i, at := range p.Attributes {
			if i == 20 {
				break
			}
			if at.Value != "" {
				fmt.Fprintf(&b, "  %s: %s\n", at.Name, at.Value)
			}
		}
	}
	if len(offers) == 0 {
		b.WriteString("Sem ofertas ativas no momento.")
		return b.String(), nil
	}
	fmt.Fprintf(&b, "Ofertas (%d), da mais barata:\n", len(offers))
	for i, o := range offers {
		if i == 8 {
			break
		}
		cond := map[string]string{"new": "novo", "used": "usado", "refurbished": "recondicionado"}[o.Condition]
		price := brl(o.Price)
		if o.Original > o.Price {
			price += fmt.Sprintf(" (de %s)", brl(o.Original))
		}
		seller := sellers[o.SellerID]
		if o.Official != 0 {
			seller = joinNonEmpty(" ", seller, "(loja oficial)")
		}
		fmt.Fprintf(&b, "  %s · %s\n", price, joinNonEmpty(" · ", cond, o.shipping(), seller))
	}
	return b.String(), nil
}

var mlLevels = map[string]string{"5_green": "reputação verde", "4_light_green": "reputação verde-clara", "3_yellow": "reputação amarela", "2_orange": "reputação laranja", "1_red": "reputação vermelha"}

// sellers names the first sellers with their reputation.
func (m *MercadoLivre) sellers(ctx context.Context, env *Env, offers []mlOffer, n int) map[int64]string {
	out := map[int64]string{}
	var mu sync.Mutex
	var wg sync.WaitGroup
	seen := map[int64]bool{}
	for i, o := range offers {
		if i == n || seen[o.SellerID] || o.SellerID == 0 {
			continue
		}
		seen[o.SellerID] = true
		wg.Add(1)
		go func(id int64) {
			defer wg.Done()
			var u struct {
				Nick string `json:"nickname"`
				Rep  struct {
					Level string `json:"level_id"`
					Power string `json:"power_seller_status"`
				} `json:"seller_reputation"`
			}
			if m.get(ctx, env, "/users/"+strconv.FormatInt(id, 10), &u) != nil || u.Nick == "" {
				return
			}
			s := "vendedor " + u.Nick
			if l := mlLevels[u.Rep.Level]; l != "" {
				s += ", " + l
			}
			if u.Rep.Power != "" {
				s += ", MercadoLíder " + u.Rep.Power
			}
			mu.Lock()
			out[id] = s
			mu.Unlock()
		}(o.SellerID)
	}
	wg.Wait()
	return out
}

func (m *MercadoLivre) best(ctx context.Context, env *Env, a Args) (string, error) {
	q, err := a.Need("tema")
	if err != nil {
		return "", err
	}
	n := a.Int("max", 10, 1, 20)
	var cats []struct {
		ID   string `json:"category_id"`
		Name string `json:"category_name"`
	}
	if err := m.get(ctx, env, "/sites/MLB/domain_discovery/search?"+url.Values{"q": {q}, "limit": {"1"}}.Encode(), &cats); err != nil {
		return "", err
	}
	if len(cats) == 0 {
		return fmt.Sprintf("Não achei a categoria de %q no Mercado Livre.", q), nil
	}
	var hl struct {
		Content []struct {
			ID   string `json:"id"`
			Type string `json:"type"`
		} `json:"content"`
	}
	if err := m.get(ctx, env, "/highlights/MLB/category/"+url.PathEscape(cats[0].ID), &hl); err != nil {
		var ae *APIError
		if errors.As(err, &ae) && (ae.Status == 403 || ae.Status == 404) {
			return "", fmt.Errorf("o Mercado Livre não liberou os mais vendidos de %s para este app; use ml_buscar", cats[0].Name)
		}
		return "", err
	}
	products := []mlProduct{}
	var mu sync.Mutex
	var wg sync.WaitGroup
	order := map[string]int{}
	for i, c := range hl.Content {
		if len(order) == n {
			break
		}
		if c.Type != "PRODUCT" {
			continue
		}
		order[c.ID] = i
		wg.Add(1)
		go func(id string) {
			defer wg.Done()
			var p mlProduct
			if m.get(ctx, env, "/products/"+url.PathEscape(id), &p) == nil && p.Name != "" {
				mu.Lock()
				products = append(products, p)
				mu.Unlock()
			}
		}(c.ID)
	}
	wg.Wait()
	if len(products) == 0 {
		return fmt.Sprintf("O Mercado Livre não trouxe mais vendidos de catálogo em %s.", cats[0].Name), nil
	}
	sort.Slice(products, func(i, j int) bool { return order[products[i].ID] < order[products[j].ID] })
	rows := m.withOffers(ctx, env, products)
	var b strings.Builder
	fmt.Fprintf(&b, "Mais vendidos do Mercado Livre em %s:\n", cats[0].Name)
	for i, r := range rows {
		b.WriteString(r.line(i + 1))
	}
	return b.String(), nil
}

func firstNonEmpty(s ...string) string {
	for _, x := range s {
		if strings.TrimSpace(x) != "" {
			return x
		}
	}
	return ""
}

