package connect

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/url"
	"strconv"
	"strings"
)

// Products is product research across every Brazilian store at once
// through Google Shopping (SerpApi): one key, no seller or affiliate
// account. A search lists offers from Mercado Livre, Shopee, Amazon,
// Magalu, AliExpress and the rest; a product's page compares its stores,
// specs and reviews.
type Products struct {
	BaseURL string
}

func (p *Products) base() string {
	if p.BaseURL != "" {
		return strings.TrimRight(p.BaseURL, "/")
	}
	return "https://serpapi.com"
}

func (p *Products) Info() Info {
	return Info{Kind: "produtos", Name: "Google Shopping", Ready: true, Category: "Compras", Auth: "token", Apps: []string{"Google Shopping", "SerpApi"},
		Description: "Pesquisa de produtos em todas as lojas de uma vez (Mercado Livre, Shopee, Amazon, Magalu, AliExpress, Casas Bahia...): preço, loja, nota, frete e promoções; e de um produto, a comparação de preço entre as lojas, a ficha técnica e as avaliações.",
		Help: "Só uma chave, sem cadastro de vendedor nem de afiliado: crie uma conta grátis em serpapi.com (só com e-mail) e copie a chave em Your Account → API Key. É a mesma chave do conector Mapas. " +
			"O plano grátis tem cerca de 250 buscas por mês; cada pesquisa ou comparação gasta uma.",
		HelpURL: "https://serpapi.com/manage-api-key"}
}

func (p *Products) Tools() []Tool {
	return []Tool{
		{Name: "produtos_buscar", Risk: Read,
			Description: "Pesquisa um produto no Google Shopping, em todas as lojas do Brasil de uma vez: preço, loja, nota e avaliações, frete, se está em promoção e o id de cada resultado (use em produtos_comparar). Gasta 1 busca da SerpApi.",
			Params: `{"type": "object", "properties": {
	"busca": {"type": "string", "description": "O produto, ex.: air fryer mondial 4l, iphone 15 128gb, tênis nike pegasus 41."},
	"ordem": {"type": "string", "enum": ["relevancia", "menor_preco", "maior_preco"]},
	"preco_min": {"type": "number", "description": "Em reais."},
	"preco_max": {"type": "number", "description": "Em reais."},
	"frete_gratis": {"type": "boolean"},
	"em_promocao": {"type": "boolean"},
	"max": {"type": "integer", "description": "Quantos resultados mostrar (padrão 15, até 40)."}
}, "required": ["busca"]}`,
			Summary: func(a Args) string { return "pesquisou no Google Shopping" + quoted(a.Str("busca")) }},
		{Name: "produtos_comparar", Risk: Read,
			Description: "Abre um produto da pesquisa (pelo id) e compara o preço em todas as lojas que o vendem (preço, frete, total, desconto, nota da loja), com a ficha técnica, a distribuição das notas e as avaliações de quem comprou. Gasta 1 busca da SerpApi.",
			Params: `{"type": "object", "properties": {
	"id": {"type": "string", "description": "O id que produtos_buscar deu, ex.: p3k-2."}
}, "required": ["id"]}`,
			Summary: func(a Args) string { return "comparou lojas no Google Shopping" }},
	}
}

func (p *Products) serp(ctx context.Context, env *Env, v url.Values, out any) error {
	key := strings.TrimSpace(env.Cred.Token)
	if key == "" {
		return &AuthError{Msg: "falta a chave da SerpApi"}
	}
	v.Set("api_key", key)
	v.Set("hl", "pt-br")
	v.Set("gl", "br")
	err := callJSON(ctx, request{URL: p.base() + "/search.json?" + v.Encode()}, out)
	var ae *APIError
	if IsAuth(err) || (errors.As(err, &ae) && strings.Contains(strings.ToLower(ae.Body), "invalid api key")) {
		return &AuthError{Msg: "a chave da SerpApi foi recusada; confira em Plugins → Google Shopping"}
	}
	if errors.As(err, &ae) && strings.Contains(strings.ToLower(ae.Body), "run out of searches") {
		return errors.New("acabaram as buscas do mês da SerpApi (plano grátis); elas voltam no começo do próximo ciclo")
	}
	return err
}

func (p *Products) account(ctx context.Context, env *Env) (serpAccountInfo, error) {
	var u serpAccountInfo
	key := strings.TrimSpace(env.Cred.Token)
	if key == "" {
		return u, &AuthError{Msg: "falta a chave da SerpApi"}
	}
	err := callJSON(ctx, request{URL: p.base() + "/account.json?" + url.Values{"api_key": {key}}.Encode()}, &u)
	var ae *APIError
	if IsAuth(err) || errors.As(err, &ae) || u.Error != "" {
		return u, &AuthError{Msg: "a chave da SerpApi foi recusada; copie a chave em serpapi.com → Your Account → API Key"}
	}
	return u, err
}

func (p *Products) Check(ctx context.Context, env *Env) (string, error) {
	u, err := p.account(ctx, env)
	if err != nil {
		return "", err
	}
	return fmt.Sprintf("SerpApi …%s (%d buscas restantes)", tail(env.Cred.Token), u.Left), nil
}

func (p *Products) Usage(ctx context.Context, env *Env) (Usage, error) {
	u, err := p.account(ctx, env)
	if err != nil {
		return Usage{}, err
	}
	return Usage{Plan: u.Plan, Used: u.Used, Limit: u.Month, Unit: "buscas"}, nil
}

// productTokens keeps the page tokens of the last searches, so the agent
// passes a short id instead of Google's long token.
const productTokensKept = 120

func (p *Products) remember(env *Env, tokens []string) string {
	if env.Cred.Extra == nil {
		env.Cred.Extra = map[string]string{}
	}
	var kept [][2]string
	_ = json.Unmarshal([]byte(env.Cred.Extra["produtos"]), &kept)
	n, _ := strconv.Atoi(env.Cred.Extra["produtos_n"])
	n++
	prefix := "p" + strconv.FormatInt(int64(n%46656), 36)
	for i, t := range tokens {
		if t != "" {
			kept = append(kept, [2]string{fmt.Sprintf("%s-%d", prefix, i+1), t})
		}
	}
	if len(kept) > productTokensKept {
		kept = kept[len(kept)-productTokensKept:]
	}
	raw, _ := json.Marshal(kept)
	env.Cred.Extra["produtos"], env.Cred.Extra["produtos_n"] = string(raw), strconv.Itoa(n)
	env.Changed = true
	return prefix
}

func (p *Products) token(env *Env, id string) string {
	id = strings.TrimSpace(strings.TrimPrefix(strings.TrimSpace(id), "id:"))
	var kept [][2]string
	_ = json.Unmarshal([]byte(env.Cred.Extra["produtos"]), &kept)
	for i := len(kept) - 1; i >= 0; i-- {
		if strings.EqualFold(kept[i][0], id) {
			return kept[i][1]
		}
	}
	return ""
}

func (p *Products) Call(ctx context.Context, env *Env, tool string, a Args) (string, error) {
	switch tool {
	case "produtos_buscar":
		return p.search(ctx, env, a)
	case "produtos_comparar":
		return p.compare(ctx, env, a)
	}
	return "", fmt.Errorf("ferramenta desconhecida: %s", tool)
}

type shoppingResult struct {
	Title    string   `json:"title"`
	Source   string   `json:"source"`
	Price    string   `json:"price"`
	Value    float64  `json:"extracted_price"`
	Old      string   `json:"old_price"`
	Rating   float64  `json:"rating"`
	Reviews  int      `json:"reviews"`
	Delivery string   `json:"delivery"`
	Snippet  string   `json:"snippet"`
	Multi    bool     `json:"multiple_sources"`
	Tag      string   `json:"tag"`
	Ext      []string `json:"extensions"`
	Token    string   `json:"immersive_product_page_token"`
	Link     string   `json:"product_link"`
}

func stars(r float64, n int) string {
	if r == 0 {
		return ""
	}
	s := strings.Replace(fmt.Sprintf("nota %.1f", r), ".", ",", 1)
	if n > 0 {
		s += fmt.Sprintf(" (%d avaliações)", n)
	}
	return s
}

func (p *Products) search(ctx context.Context, env *Env, a Args) (string, error) {
	q, err := a.Need("busca")
	if err != nil {
		return "", err
	}
	v := url.Values{"engine": {"google_shopping"}, "q": {q}}
	switch a.Str("ordem") {
	case "menor_preco":
		v.Set("sort_by", "1")
	case "maior_preco":
		v.Set("sort_by", "2")
	}
	if x, ok := a["preco_min"].(float64); ok && x > 0 {
		v.Set("min_price", strconv.Itoa(int(x)))
	}
	if x, ok := a["preco_max"].(float64); ok && x > 0 {
		v.Set("max_price", strconv.Itoa(int(x+0.999)))
	}
	if a.Bool("frete_gratis") {
		v.Set("free_shipping", "true")
	}
	if a.Bool("em_promocao") {
		v.Set("on_sale", "true")
	}
	var out struct {
		Error   string           `json:"error"`
		Results []shoppingResult `json:"shopping_results"`
		Inline  []shoppingResult `json:"inline_shopping_results"`
	}
	if err := p.serp(ctx, env, v, &out); err != nil {
		return "", err
	}
	results := out.Results
	if len(results) == 0 {
		results = out.Inline
	}
	if len(results) == 0 {
		if out.Error != "" && !noResults(out.Error) {
			return "", errors.New("a SerpApi respondeu: " + out.Error)
		}
		return fmt.Sprintf("Nada no Google Shopping para %q.", q), nil
	}
	n := a.Int("max", 15, 1, 40)
	if len(results) > n {
		results = results[:n]
	}
	tokens := make([]string, len(results))
	for i, r := range results {
		tokens[i] = r.Token
	}
	prefix := p.remember(env, tokens)
	var b strings.Builder
	fmt.Fprintf(&b, "%d ofertas no Google Shopping para %q:\n", len(results), q)
	for i, r := range results {
		price := firstNonEmpty(r.Price, brl(r.Value))
		if r.Old != "" {
			price += " (de " + r.Old + ")"
		}
		store := r.Source
		if r.Multi {
			store += " e outras lojas"
		}
		fmt.Fprintf(&b, "%d. %s\n   %s\n", i+1, r.Title, joinNonEmpty(" · ", price, store, stars(r.Rating, r.Reviews), r.Delivery, r.Tag, strings.Join(r.Ext, ", ")))
		if r.Snippet != "" {
			fmt.Fprintf(&b, "   %s\n", cut(r.Snippet, 160))
		}
		if r.Token != "" {
			fmt.Fprintf(&b, "   id:%s-%d\n", prefix, i+1)
		}
	}
	return b.String(), nil
}

func (p *Products) compare(ctx context.Context, env *Env, a Args) (string, error) {
	id, err := a.Need("id")
	if err != nil {
		return "", err
	}
	tok := p.token(env, id)
	if tok == "" {
		return "", fmt.Errorf("não conheço o id %s: use um id da última pesquisa de produtos_buscar", id)
	}
	var out struct {
		Error   string `json:"error"`
		Product struct {
			Title   string  `json:"title"`
			Brand   string  `json:"brand"`
			Rating  float64 `json:"rating"`
			Reviews int     `json:"reviews"`
			Range   string  `json:"price_range"`
			Stores  []struct {
				Name     string   `json:"name"`
				Link     string   `json:"link"`
				Price    string   `json:"price"`
				Original string   `json:"original_price"`
				Discount string   `json:"discount"`
				Shipping string   `json:"shipping"`
				Total    string   `json:"total"`
				Tag      string   `json:"tag"`
				Rating   float64  `json:"rating"`
				Reviews  int      `json:"reviews"`
				Details  []string `json:"details_and_offers"`
			} `json:"stores"`
			About struct {
				Description string `json:"description"`
				Features    []struct {
					Title string `json:"title"`
					Value string `json:"value"`
				} `json:"features"`
			} `json:"about_the_product"`
			Ratings []struct {
				Stars  int `json:"stars"`
				Amount int `json:"amount"`
			} `json:"ratings"`
			UserReviews []struct {
				User   string  `json:"user_name"`
				Rating float64 `json:"rating"`
				Title  string  `json:"title"`
				Text   string  `json:"text"`
				Date   string  `json:"date"`
				Source string  `json:"source"`
			} `json:"user_reviews"`
			Insights []struct {
				Title   string `json:"title"`
				Snippet string `json:"snippet"`
			} `json:"top_insights"`
		} `json:"product_results"`
	}
	if err := p.serp(ctx, env, url.Values{"engine": {"google_immersive_product"}, "page_token": {tok}, "more_stores": {"true"}}, &out); err != nil {
		return "", err
	}
	pr := out.Product
	if pr.Title == "" {
		if out.Error != "" {
			return "", errors.New("a SerpApi respondeu: " + out.Error)
		}
		return "O Google não trouxe a página desse produto; pesquise de novo.", nil
	}
	var b strings.Builder
	fmt.Fprintf(&b, "%s\n", joinNonEmpty(" · ", pr.Title, pr.Brand, stars(pr.Rating, pr.Reviews), pr.Range))
	if len(pr.Stores) > 0 {
		fmt.Fprintf(&b, "Lojas (%d):\n", len(pr.Stores))
		for _, s := range pr.Stores {
			price := s.Price
			if s.Original != "" {
				price += " (de " + s.Original + ")"
			}
			ship := s.Shipping
			if s.Total != "" && s.Total != s.Price {
				ship = joinNonEmpty(", ", ship, "total "+s.Total)
			}
			fmt.Fprintf(&b, "  %s: %s\n    %s\n", s.Name, joinNonEmpty(" · ", price, s.Discount, ship, s.Tag, stars(s.Rating, s.Reviews), strings.Join(s.Details, ", ")), s.Link)
		}
	}
	if pr.About.Description != "" {
		b.WriteString("Sobre: " + cut(pr.About.Description, 500) + "\n")
	}
	if len(pr.About.Features) > 0 {
		b.WriteString("Ficha técnica:\n")
		for i, f := range pr.About.Features {
			if i == 20 {
				break
			}
			fmt.Fprintf(&b, "  %s: %s\n", f.Title, f.Value)
		}
	}
	if len(pr.Ratings) > 0 {
		parts := []string{}
		for _, r := range pr.Ratings {
			parts = append(parts, fmt.Sprintf("%d★ %d", r.Stars, r.Amount))
		}
		b.WriteString("Notas: " + strings.Join(parts, " · ") + "\n")
	}
	for i, in := range pr.Insights {
		if i == 0 {
			b.WriteString("Destaques das avaliações:\n")
		}
		if i == 5 {
			break
		}
		fmt.Fprintf(&b, "  %s\n", joinNonEmpty(": ", in.Title, cut(in.Snippet, 200)))
	}
	for i, r := range pr.UserReviews {
		if i == 0 {
			b.WriteString("Avaliações:\n")
		}
		if i == 6 {
			break
		}
		fmt.Fprintf(&b, "  %.0f★ %s\n", r.Rating, joinNonEmpty(" · ", r.User, r.Date, r.Source)+": "+cut(joinNonEmpty(" — ", r.Title, r.Text), 400))
	}
	return b.String(), nil
}
