package connect

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"math"
	"net/url"
	"strconv"
	"strings"
	"time"
)

// Apify runs the store's robots (Actors) with the owner's API tokens. The
// owner may connect several accounts: the server spreads the runs over
// them, the one with the most credit left first, and moves to the next when
// one runs out (Pooled). A run's id carries its account, so its results are
// read from the right one (Routed).
type Apify struct {
	BaseURL string
	// Wait bounds how long a call waits for a run (tests shorten it).
	Wait time.Duration
}

func (a *Apify) base() string {
	if a.BaseURL != "" {
		return strings.TrimRight(a.BaseURL, "/")
	}
	return "https://api.apify.com"
}

func (a *Apify) wait() time.Duration {
	if a.Wait > 0 {
		return a.Wait
	}
	return 100 * time.Second
}

func (a *Apify) Info() Info {
	return Info{Kind: "apify", Name: "Apify", Ready: true, Category: "Automação e dados", Auth: "token", Apps: []string{"Apify"},
		Description: "Pesquisa produtos direto no Mercado Livre, Shopee, AliExpress e Amazon (preços, promoções, vendas, notas), sem conta de vendedor, e roda robôs de coleta da loja do Apify: TikTok, Instagram, sites... Conecte várias contas: os agentes usam a que tem mais crédito e passam para a próxima quando uma acaba.",
		Help: "Em apify.com (o plano grátis dá crédito todo mês, sem cartão): Settings → API & Integrations → copie o Personal API token. Para somar o crédito de outra conta, conecte o token dela também. " +
			"Robôs de aluguel (mensalidade fixa) não rodam no plano grátis; os que cobram por resultado rodam.",
		HelpURL: "https://console.apify.com/settings/integrations"}
}

// Pooled: the server picks the account with the most credit.
func (a *Apify) Pooled() {}

func (a *Apify) Tools() []Tool {
	return append(storeTools(), []Tool{
		{Name: "apify_buscar_robos", Risk: Read,
			Description: "Busca robôs na loja do Apify (ex.: tiktok, instagram comments, google maps reviews, mercado livre). Traz o id (usuario/robo), o que faz, quanto custa e quantos usam. Prefira os de pagar por resultado e muito usados.",
			Params: `{"type": "object", "properties": {
	"busca": {"type": "string"}
}, "required": ["busca"]}`,
			Summary: func(x Args) string { return "buscou robôs no Apify" + quoted(x.Str("busca")) }},
		{Name: "apify_robo", Risk: Read,
			Description: "Como usar um robô do Apify: o que faz, o preço e os campos da entrada (com exemplo). Leia antes de rodar um robô novo.",
			Params: `{"type": "object", "properties": {
	"robo": {"type": "string", "description": "Id do robô, ex.: clockworks/tiktok-scraper."}
}, "required": ["robo"]}`,
			Summary: func(x Args) string { return "leu o robô " + x.Str("robo") + " do Apify" }},
		{Name: "apify_rodar", Risk: Write,
			Description: "Roda um robô do Apify com a entrada dada e traz os resultados (até max_itens). Gasta crédito do Apify: custo_max_usd limita o gasto da execução (padrão 0,50). Se demorar, devolve o id da execução para ler depois com apify_resultado.",
			Params: `{"type": "object", "properties": {
	"robo": {"type": "string", "description": "Id do robô, ex.: clockworks/tiktok-scraper."},
	"entrada": {"type": "object", "description": "A entrada do robô (veja os campos com apify_robo)."},
	"max_itens": {"type": "integer", "description": "Máximo de resultados cobrados e trazidos (padrão 30, até 500)."},
	"custo_max_usd": {"type": "number", "description": "Gasto máximo da execução em dólares (padrão 0.5, até 5)."}
}, "required": ["robo"]}`,
			Summary: func(x Args) string { return "rodou o robô " + x.Str("robo") + " no Apify" }},
		{Name: "apify_resultado", Risk: Read,
			Description: "Situação e resultados de uma execução do Apify pelo id que apify_rodar ou apify_execucoes deu (o id já diz de qual conta é).",
			Params: `{"type": "object", "properties": {
	"execucao": {"type": "string"},
	"max_itens": {"type": "integer", "description": "Padrão 30, até 500."},
	"parte": {"type": "integer", "description": "Resultados longos vêm em partes."}
}, "required": ["execucao"]}`,
			Summary: func(x Args) string { return "leu uma execução do Apify" }},
		{Name: "apify_execucoes", Risk: Read,
			Description: "As últimas execuções de robôs no Apify (da conta usada), com situação, custo e id.",
			Params:      `{"type": "object", "properties": {}}`,
			Summary:     func(Args) string { return "execuções do Apify" }},
	}...)
}

// RouteOf names the account that ran an execution: its id reads
// "<run>@<connection>".
func (a *Apify) RouteOf(tool string, x Args) string {
	if tool != "apify_resultado" {
		return ""
	}
	if _, conn, ok := strings.Cut(x.Str("execucao"), "@"); ok {
		return strings.TrimSpace(conn)
	}
	return ""
}

func runRef(run, conn string) string {
	if conn == "" {
		return run
	}
	return run + "@" + conn
}

func (a *Apify) do(ctx context.Context, env *Env, method, path string, body any, out any) error {
	tok := strings.TrimSpace(env.Cred.Token)
	if tok == "" {
		return &AuthError{Msg: "falta o token do Apify"}
	}
	err := callJSON(ctx, request{Method: method, URL: a.base() + path, Body: body, Header: map[string]string{"Authorization": "Bearer " + tok}}, out)
	var ae *APIError
	if errors.As(err, &ae) {
		b := strings.ToLower(ae.Body)
		switch {
		case ae.Status == 402 || strings.Contains(b, "not-enough-usage") || strings.Contains(b, "usage limit") || strings.Contains(b, "hard limit") ||
			strings.Contains(b, "insufficient") || strings.Contains(b, "exceeded your") || strings.Contains(b, "credit"):
			return &CreditError{Msg: "a conta do Apify ficou sem crédito este mês"}
		case strings.Contains(b, "actor-is-not-rented") || strings.Contains(b, "rent"):
			return errors.New("esse robô é de aluguel (mensalidade) e não roda nesta conta; escolha um de pagar por resultado com apify_buscar_robos")
		case ae.Status == 404:
			return errors.New("o Apify não achou isso (confira o id do robô ou da execução)")
		case ae.Status == 403 && (strings.Contains(b, "token") || strings.Contains(b, "auth")):
			return &AuthError{Msg: "o Apify recusou o token; confira em Plugins"}
		}
	}
	return err
}

func (a *Apify) Check(ctx context.Context, env *Env) (string, error) {
	var me struct {
		Data struct {
			Username string `json:"username"`
			Plan     struct {
				ID string `json:"id"`
			} `json:"plan"`
		} `json:"data"`
	}
	if err := a.do(ctx, env, "GET", "/v2/users/me", nil, &me); err != nil {
		var ae *APIError
		if errors.As(err, &ae) && ae.Status < 500 {
			return "", &AuthError{Msg: "o Apify recusou o token; copie o Personal API token em Settings → API & Integrations"}
		}
		return "", err
	}
	if me.Data.Username == "" {
		return "conta …" + tail(env.Cred.Token), nil
	}
	return me.Data.Username, nil
}

// Usage is the month's spend against the plan's limit, in US cents.
func (a *Apify) Usage(ctx context.Context, env *Env) (Usage, error) {
	var l struct {
		Data struct {
			Cycle struct {
				End string `json:"endAt"`
			} `json:"monthlyUsageCycle"`
			Limits struct {
				Max float64 `json:"maxMonthlyUsageUsd"`
			} `json:"limits"`
			Current struct {
				Used float64 `json:"monthlyUsageUsd"`
			} `json:"current"`
		} `json:"data"`
	}
	if err := a.do(ctx, env, "GET", "/v2/users/me/limits", nil, &l); err != nil {
		return Usage{}, err
	}
	u := Usage{Used: int64(math.Round(l.Data.Current.Used * 100)), Limit: int64(math.Round(l.Data.Limits.Max * 100)), Unit: "usd_cents"}
	if t, err := time.Parse(time.RFC3339, l.Data.Cycle.End); err == nil {
		u.ResetsAt = t.Unix()
	}
	return u, nil
}

func actorPath(id string) string {
	id = strings.TrimSpace(strings.TrimPrefix(id, "https://apify.com/"))
	return url.PathEscape(strings.Replace(id, "/", "~", 1))
}

type apifyPricing struct {
	Model    string  `json:"pricingModel"`
	PerUnit  float64 `json:"pricePerUnitUsd"`
	Unit     string  `json:"unitName"`
	PerEvent *struct {
		Events map[string]struct {
			Title string  `json:"eventTitle"`
			Price float64 `json:"eventPriceUsd"`
		} `json:"actorChargeEvents"`
	} `json:"pricingPerEvent"`
}

func (p *apifyPricing) text() string {
	if p == nil {
		return "grátis (paga só o uso da plataforma)"
	}
	switch p.Model {
	case "FREE", "":
		return "grátis (paga só o uso da plataforma)"
	case "FLAT_PRICE_PER_MONTH":
		return fmt.Sprintf("aluguel de US$ %.2f/mês (não roda no plano grátis)", p.PerUnit)
	case "PRICE_PER_DATASET_ITEM":
		unit := firstNonEmpty(p.Unit, "resultado")
		return fmt.Sprintf("US$ %.2f por mil %ss", p.PerUnit*1000, unit)
	case "PAY_PER_EVENT":
		if p.PerEvent != nil {
			parts := []string{}
			for _, e := range p.PerEvent.Events {
				if len(parts) == 3 {
					break
				}
				parts = append(parts, fmt.Sprintf("%s US$ %.4f", firstNonEmpty(e.Title, "evento"), e.Price))
			}
			return "por evento: " + strings.Join(parts, "; ")
		}
		return "por evento"
	}
	return strings.ToLower(p.Model)
}

func (a *Apify) Call(ctx context.Context, env *Env, tool string, x Args) (string, error) {
	switch tool {
	case "apify_buscar_robos":
		return a.store(ctx, env, x)
	case "apify_robo":
		return a.actor(ctx, env, x)
	case "apify_rodar":
		return a.run(ctx, env, x)
	case "apify_resultado":
		return a.result(ctx, env, x)
	case "apify_execucoes":
		return a.runs(ctx, env)
	case "lojas_buscar":
		return a.storeSearch(ctx, env, x)
	case "lojas_comparar":
		return a.storeCompare(ctx, env, x)
	}
	return "", fmt.Errorf("ferramenta desconhecida: %s", tool)
}

func (a *Apify) store(ctx context.Context, env *Env, x Args) (string, error) {
	q, err := x.Need("busca")
	if err != nil {
		return "", err
	}
	var out struct {
		Data struct {
			Items []struct {
				Name    string                     `json:"name"`
				User    string                     `json:"username"`
				Title   string                     `json:"title"`
				Desc    string                     `json:"description"`
				Pricing *apifyPricing              `json:"currentPricingInfo"`
				Stats   struct{ TotalUsers int64 } `json:"stats"`
			} `json:"items"`
		} `json:"data"`
	}
	if err := a.do(ctx, env, "GET", "/v2/store?"+url.Values{"search": {q}, "limit": {"10"}}.Encode(), nil, &out); err != nil {
		return "", err
	}
	if len(out.Data.Items) == 0 {
		return fmt.Sprintf("Nenhum robô no Apify para %q.", q), nil
	}
	var b strings.Builder
	fmt.Fprintf(&b, "Robôs do Apify para %q:\n", q)
	for i, it := range out.Data.Items {
		fmt.Fprintf(&b, "%d. %s — %s/%s\n   %s\n   %s · %d usuários\n", i+1, it.Title, it.User, it.Name, cut(it.Desc, 200), it.Pricing.text(), it.Stats.TotalUsers)
	}
	return b.String(), nil
}

func (a *Apify) actor(ctx context.Context, env *Env, x Args) (string, error) {
	id, err := x.Need("robo")
	if err != nil {
		return "", err
	}
	var act struct {
		Data struct {
			Title   string          `json:"title"`
			Desc    string          `json:"description"`
			Pricing []*apifyPricing `json:"pricingInfos"`
			Example struct {
				Body string `json:"body"`
			} `json:"exampleRunInput"`
		} `json:"data"`
	}
	if err := a.do(ctx, env, "GET", "/v2/acts/"+actorPath(id), nil, &act); err != nil {
		return "", err
	}
	var build struct {
		Data struct {
			InputSchema string `json:"inputSchema"`
			Definition  struct {
				Input json.RawMessage `json:"input"`
			} `json:"actorDefinition"`
		} `json:"data"`
	}
	_ = a.do(ctx, env, "GET", "/v2/acts/"+actorPath(id)+"/builds/default", nil, &build)
	var b strings.Builder
	fmt.Fprintf(&b, "%s (%s)\n%s\n", act.Data.Title, id, cut(act.Data.Desc, 500))
	var price *apifyPricing
	if n := len(act.Data.Pricing); n > 0 {
		price = act.Data.Pricing[n-1]
	}
	b.WriteString("Preço: " + price.text() + "\n")
	schema := []byte(build.Data.Definition.Input)
	if len(schema) == 0 || string(schema) == "null" {
		schema = []byte(build.Data.InputSchema)
	}
	var s struct {
		Props    map[string]map[string]any `json:"properties"`
		Required []string                  `json:"required"`
	}
	if json.Unmarshal(schema, &s) == nil && len(s.Props) > 0 {
		b.WriteString("Campos da entrada:\n")
		n := 0
		for name, p := range s.Props {
			if n == 30 {
				b.WriteString("  …\n")
				break
			}
			n++
			req := ""
			for _, r := range s.Required {
				if r == name {
					req = " (obrigatório)"
				}
			}
			extra := ""
			for _, k := range []string{"prefill", "default", "enum"} {
				if v, ok := p[k]; ok {
					raw, _ := json.Marshal(v)
					extra += fmt.Sprintf(" %s=%s", k, cut(string(raw), 120))
				}
			}
			fmt.Fprintf(&b, "  %s: %v%s — %s%s\n", name, p["type"], req, cut(fmt.Sprint(firstNonEmpty(fmt.Sprint(p["title"]), "")), 80)+" "+cut(strings.ReplaceAll(fmt.Sprint(p["description"]), "\n", " "), 160), extra)
		}
	}
	if ex := strings.TrimSpace(act.Data.Example.Body); ex != "" && ex != "{}" {
		b.WriteString("Exemplo de entrada: " + cut(ex, 800) + "\n")
	}
	return b.String(), nil
}

type apifyRun struct {
	ID      string  `json:"id"`
	ActID   string  `json:"actId"`
	Status  string  `json:"status"`
	Dataset string  `json:"defaultDatasetId"`
	Cost    float64 `json:"usageTotalUsd"`
	Started string  `json:"startedAt"`
	Message string  `json:"statusMessage"`
}

var runStatus = map[string]string{"READY": "na fila", "RUNNING": "rodando", "SUCCEEDED": "terminou", "FAILED": "falhou",
	"TIMING-OUT": "estourando o tempo", "TIMED-OUT": "estourou o tempo", "ABORTING": "parando", "ABORTED": "parada"}

func (r apifyRun) done() bool {
	return r.Status != "READY" && r.Status != "RUNNING" && r.Status != "TIMING-OUT" && r.Status != "ABORTING"
}

func (a *Apify) run(ctx context.Context, env *Env, x Args) (string, error) {
	id, err := x.Need("robo")
	if err != nil {
		return "", err
	}
	input := map[string]any{}
	if v, ok := x["entrada"].(map[string]any); ok {
		input = v
	} else if s := x.Str("entrada"); s != "" {
		if err := json.Unmarshal([]byte(s), &input); err != nil {
			return "", errors.New("entrada precisa ser um objeto JSON")
		}
	}
	maxItems := x.Int("max_itens", 30, 1, 500)
	maxCost := 0.5
	if v, ok := x["custo_max_usd"].(float64); ok && v > 0 {
		maxCost = math.Min(v, 5)
	}
	v := url.Values{"waitForFinish": {"60"}, "maxItems": {strconv.Itoa(maxItems)}, "maxTotalChargeUsd": {strconv.FormatFloat(maxCost, 'f', 2, 64)}}
	var out struct {
		Data apifyRun `json:"data"`
	}
	if err := a.do(ctx, env, "POST", "/v2/acts/"+actorPath(id)+"/runs?"+v.Encode(), input, &out); err != nil {
		return "", err
	}
	return a.finish(ctx, env, out.Data, maxItems, 1)
}

// finish waits for a run up to the call's limit and reads its results.
func (a *Apify) finish(ctx context.Context, env *Env, r apifyRun, maxItems, part int) (string, error) {
	deadline := time.Now().Add(a.wait())
	for !r.done() && time.Now().Before(deadline) {
		secs := int(math.Min(60, time.Until(deadline).Seconds()))
		if secs < 1 {
			break
		}
		var out struct {
			Data apifyRun `json:"data"`
		}
		if err := a.do(ctx, env, "GET", fmt.Sprintf("/v2/actor-runs/%s?waitForFinish=%d", url.PathEscape(r.ID), secs), nil, &out); err != nil {
			return "", err
		}
		r = out.Data
	}
	ref := runRef(r.ID, env.Conn)
	head := fmt.Sprintf("Execução %s: %s · custo US$ %.3f", ref, firstNonEmpty(runStatus[r.Status], r.Status), r.Cost)
	if !r.done() {
		return head + "\nAinda rodando: leia depois com apify_resultado (execucao=" + ref + ").", nil
	}
	if r.Status != "SUCCEEDED" && r.Message != "" {
		head += " · " + cut(r.Message, 200)
	}
	if r.Dataset == "" {
		return head, nil
	}
	data, _, err := do(ctx, request{URL: a.base() + "/v2/datasets/" + url.PathEscape(r.Dataset) + "/items?" + url.Values{"clean": {"true"}, "limit": {strconv.Itoa(maxItems)}, "format": {"json"}}.Encode(),
		Header: map[string]string{"Authorization": "Bearer " + strings.TrimSpace(env.Cred.Token)}})
	if err != nil {
		return head + "\nNão consegui ler os resultados: " + err.Error(), nil
	}
	var items []json.RawMessage
	if json.Unmarshal(data, &items) != nil {
		return head + "\nResultados em formato inesperado.", nil
	}
	if len(items) == 0 {
		return head + "\nNenhum resultado.", nil
	}
	var b strings.Builder
	for i, it := range items {
		var c bytes.Buffer
		if json.Compact(&c, it) != nil {
			continue
		}
		fmt.Fprintf(&b, "%d. %s\n", i+1, cut(c.String(), 1500))
	}
	return withPart(fmt.Sprintf("%s · %d resultados", head, len(items)), b.String(), part), nil
}

func (a *Apify) result(ctx context.Context, env *Env, x Args) (string, error) {
	ref, err := x.Need("execucao")
	if err != nil {
		return "", err
	}
	id, _, _ := strings.Cut(ref, "@")
	var out struct {
		Data apifyRun `json:"data"`
	}
	if err := a.do(ctx, env, "GET", "/v2/actor-runs/"+url.PathEscape(strings.TrimSpace(id)), nil, &out); err != nil {
		return "", err
	}
	return a.finish(ctx, env, out.Data, x.Int("max_itens", 30, 1, 500), x.Int("parte", 1, 1, 100))
}

func (a *Apify) runs(ctx context.Context, env *Env) (string, error) {
	var out struct {
		Data struct {
			Items []apifyRun `json:"items"`
		} `json:"data"`
	}
	if err := a.do(ctx, env, "GET", "/v2/actor-runs?desc=true&limit=10", nil, &out); err != nil {
		return "", err
	}
	if len(out.Data.Items) == 0 {
		return "Nenhuma execução nesta conta do Apify.", nil
	}
	var b strings.Builder
	b.WriteString("Últimas execuções do Apify:\n")
	for _, r := range out.Data.Items {
		when := r.Started
		if t, err := time.Parse(time.RFC3339, r.Started); err == nil {
			when = t.In(env.zone()).Format("02/01 15:04")
		}
		fmt.Fprintf(&b, "- %s · %s · US$ %.3f · execucao=%s\n", when, firstNonEmpty(runStatus[r.Status], r.Status), r.Cost, runRef(r.ID, env.Conn))
	}
	return b.String(), nil
}
