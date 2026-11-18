package connect

import (
	"context"
	"fmt"
	"math"
	"net/url"
	"regexp"
	"sort"
	"strings"
	"sync"
	"time"
)

// Pluggy: the owner's bank data through Meu Pluggy (my.pluggy.ai), read
// only. The owner connects the banks in Meu Pluggy, adds the MeuPluggy
// connector to an application in the Pluggy Dashboard, and gives Avora the
// application's client id and secret plus the item id of each bank (Meu
// Pluggy items cannot be listed). Nothing here moves money.
type Pluggy struct {
	BaseURL string

	mu   sync.Mutex
	keys map[string]pluggyKey // API keys by client id; they last 2 hours
}

type pluggyKey struct {
	key     string
	expires time.Time
}

var pluggyItem = regexp.MustCompile(`^[0-9a-fA-F-]{36}$`)

func (p *Pluggy) base() string {
	if p.BaseURL != "" {
		return strings.TrimRight(p.BaseURL, "/")
	}
	return "https://api.pluggy.ai"
}

func (p *Pluggy) Info() Info {
	return Info{Kind: "pluggy", Name: "Pluggy", Ready: true, Category: "Com chave ou token", Auth: "token", Apps: []string{"Bancos (Meu Pluggy)"},
		Description: "Seus bancos pelo Meu Pluggy: saldos, extratos, cartões, faturas e investimentos. Só leitura.",
		Help: "1. Conecte os bancos em meu.pluggy.ai. 2. No dashboard.pluggy.ai, abra Applications → sua aplicação → Demo → Connect Account e escolha MeuPluggy. " +
			"3. Copie de Connected Items o itemId de cada banco, e de Applications o Client ID e o Client Secret.",
		HelpURL: "https://dashboard.pluggy.ai",
		Fields: []Field{
			{Key: "client_id", Label: "Client ID"},
			{Key: "client_secret", Label: "Client Secret", Secret: true},
			{Key: "items", Label: "Item IDs, um por banco, separados por vírgula"},
		}}
}

func (p *Pluggy) Tools() []Tool {
	return []Tool{
		{Name: "pluggy_contas", Risk: Read,
			Description: "Lista as contas bancárias e cartões do dono em todos os bancos: banco, tipo, número final, saldo e limite do cartão.",
			Params:      `{"type": "object", "properties": {}}`,
			Summary:     func(Args) string { return "contas e saldos" }},
		{Name: "pluggy_transacoes", Risk: Read,
			Description: "Extrato de uma conta ou cartão: data, descrição, valor (negativo = saída), categoria. Por padrão os últimos 30 dias, de todas as contas.",
			Params: `{"type": "object", "properties": {
	"conta": {"type": "string", "description": "Id da conta (de pluggy_contas) ou parte do nome do banco/conta; vazio = todas."},
	"de": {"type": "string", "description": "Data inicial AAAA-MM-DD; padrão: 30 dias atrás."},
	"ate": {"type": "string", "description": "Data final AAAA-MM-DD; padrão: hoje."},
	"busca": {"type": "string", "description": "Filtra pela descrição (ex.: uber, mercado)."},
	"limite": {"type": "integer", "description": "Máximo de lançamentos (padrão 100, até 500)."}
}}`,
			Summary: func(a Args) string { return "extrato" + quoted(a.Str("conta")) + quoted(a.Str("busca")) }},
		{Name: "pluggy_faturas", Risk: Read,
			Description: "Faturas dos cartões de crédito: vencimento, valor total e pagamento mínimo.",
			Params:      `{"type": "object", "properties": {"conta": {"type": "string", "description": "Id ou nome do cartão; vazio = todos."}}}`,
			Summary:     func(a Args) string { return "faturas" + quoted(a.Str("conta")) }},
		{Name: "pluggy_investimentos", Risk: Read,
			Description: "Investimentos do dono: nome, tipo, saldo, rendimento e vencimento.",
			Params:      `{"type": "object", "properties": {}}`,
			Summary:     func(Args) string { return "investimentos" }},
	}
}

func (p *Pluggy) items(env *Env) ([]string, error) {
	var out []string
	for _, it := range strings.FieldsFunc(env.Cred.Extra["items"], func(r rune) bool { return r == ',' || r == ';' || r == ' ' || r == '\n' }) {
		if !pluggyItem.MatchString(it) {
			return nil, fmt.Errorf("item id inválido: %q", cut(it, 40))
		}
		out = append(out, it)
	}
	if len(out) == 0 {
		return nil, fmt.Errorf("informe ao menos um item id")
	}
	return out, nil
}

// apiKey trades the client id and secret for an API key, cached.
func (p *Pluggy) apiKey(ctx context.Context, env *Env, fresh bool) (string, error) {
	id, secret := env.Cred.Extra["client_id"], env.Cred.Extra["client_secret"]
	if id == "" || secret == "" {
		return "", errReconnect
	}
	now := env.now()
	p.mu.Lock()
	k, ok := p.keys[id]
	p.mu.Unlock()
	if ok && !fresh && now.Before(k.expires) {
		return k.key, nil
	}
	var r struct {
		APIKey string `json:"apiKey"`
	}
	err := callJSON(ctx, request{Method: "POST", URL: p.base() + "/auth", Body: map[string]string{"clientId": id, "clientSecret": secret}}, &r)
	if err != nil {
		if api, ok := err.(*APIError); ok && (api.Status == 400 || api.Status == 403) {
			return "", &AuthError{Msg: "a Pluggy recusou o Client ID ou o Client Secret; reconecte em Plugins"}
		}
		return "", err
	}
	if r.APIKey == "" {
		return "", fmt.Errorf("a Pluggy não devolveu a chave")
	}
	p.mu.Lock()
	if p.keys == nil {
		p.keys = map[string]pluggyKey{}
	}
	p.keys[id] = pluggyKey{key: r.APIKey, expires: now.Add(110 * time.Minute)}
	p.mu.Unlock()
	return r.APIKey, nil
}

func (p *Pluggy) get(ctx context.Context, env *Env, path string, q url.Values, out any) error {
	for attempt := 0; ; attempt++ {
		key, err := p.apiKey(ctx, env, attempt > 0)
		if err != nil {
			return err
		}
		u := p.base() + path
		if len(q) > 0 {
			u += "?" + q.Encode()
		}
		err = callJSON(ctx, request{URL: u, Header: map[string]string{"X-API-KEY": key}}, out)
		if IsAuth(err) && attempt == 0 {
			continue // the cached key expired early
		}
		return err
	}
}

type pluggyAccount struct {
	ID         string  `json:"id"`
	ItemID     string  `json:"itemId"`
	Type       string  `json:"type"`    // BANK, CREDIT
	Subtype    string  `json:"subtype"` // CHECKING_ACCOUNT, SAVINGS_ACCOUNT, CREDIT_CARD
	Name       string  `json:"name"`
	Marketing  string  `json:"marketingName"`
	Number     string  `json:"number"`
	Balance    float64 `json:"balance"`
	Currency   string  `json:"currencyCode"`
	CreditData *struct {
		Limit     float64 `json:"creditLimit"`
		Available float64 `json:"availableCreditLimit"`
		Level     string  `json:"level"`
		Brand     string  `json:"brand"`
	} `json:"creditData"`
}

func (a pluggyAccount) label() string {
	name := a.Marketing
	if name == "" {
		name = a.Name
	}
	n := strings.Map(func(r rune) rune {
		if r >= '0' && r <= '9' {
			return r
		}
		return -1
	}, a.Number)
	if len(n) >= 4 {
		name += " final " + n[len(n)-4:]
	}
	return name
}

func (p *Pluggy) accounts(ctx context.Context, env *Env) ([]pluggyAccount, error) {
	items, err := p.items(env)
	if err != nil {
		return nil, err
	}
	var all []pluggyAccount
	for _, it := range items {
		var r struct {
			Results []pluggyAccount `json:"results"`
		}
		if err := p.get(ctx, env, "/accounts", url.Values{"itemId": {it}}, &r); err != nil {
			return nil, err
		}
		all = append(all, r.Results...)
	}
	return all, nil
}

// pick filters accounts by id or a piece of their name ("" = all).
func pick(list []pluggyAccount, want string, kind string) []pluggyAccount {
	want = strings.ToLower(strings.TrimSpace(want))
	var out []pluggyAccount
	for _, a := range list {
		if kind != "" && a.Type != kind {
			continue
		}
		if want == "" || a.ID == want || strings.Contains(strings.ToLower(a.label()+" "+a.Name), want) {
			out = append(out, a)
		}
	}
	return out
}

func money(v float64, cur string) string {
	if cur == "" || cur == "BRL" {
		return fmt.Sprintf("R$ %.2f", v)
	}
	return fmt.Sprintf("%s %.2f", cur, v)
}

func (p *Pluggy) Check(ctx context.Context, env *Env) (string, error) {
	list, err := p.accounts(ctx, env)
	if err != nil {
		return "", err
	}
	banks := map[string]bool{}
	for _, a := range list {
		banks[a.ItemID] = true
	}
	return fmt.Sprintf("%d bancos, %d contas", len(banks), len(list)), nil
}

func (p *Pluggy) Call(ctx context.Context, env *Env, tool string, a Args) (string, error) {
	switch tool {
	case "pluggy_contas":
		list, err := p.accounts(ctx, env)
		if err != nil {
			return "", err
		}
		var b strings.Builder
		for _, ac := range list {
			fmt.Fprintf(&b, "- %s (%s) id=%s: saldo %s", ac.label(), strings.ToLower(ac.Subtype), ac.ID, money(ac.Balance, ac.Currency))
			if c := ac.CreditData; c != nil {
				fmt.Fprintf(&b, ", limite %s, disponível %s", money(c.Limit, ac.Currency), money(c.Available, ac.Currency))
			}
			b.WriteString("\n")
		}
		if b.Len() == 0 {
			return "Nenhuma conta nos bancos conectados.", nil
		}
		return "No cartão, o saldo é o valor da fatura em aberto.\n" + b.String(), nil

	case "pluggy_transacoes":
		return p.transactions(ctx, env, a)

	case "pluggy_faturas":
		list, err := p.accounts(ctx, env)
		if err != nil {
			return "", err
		}
		var b strings.Builder
		for _, ac := range pick(list, a.Str("conta"), "CREDIT") {
			var r struct {
				Results []struct {
					DueDate string  `json:"dueDate"`
					Total   float64 `json:"totalAmount"`
					Minimum float64 `json:"minimumPaymentAmount"`
					Cur     string  `json:"totalAmountCurrencyCode"`
				} `json:"results"`
			}
			if err := p.get(ctx, env, "/bills", url.Values{"accountId": {ac.ID}}, &r); err != nil {
				return "", err
			}
			fmt.Fprintf(&b, "%s:\n", ac.label())
			if len(r.Results) == 0 {
				b.WriteString("  sem faturas\n")
			}
			sort.Slice(r.Results, func(i, j int) bool { return r.Results[i].DueDate > r.Results[j].DueDate })
			for i, f := range r.Results {
				if i == 6 {
					break
				}
				fmt.Fprintf(&b, "  vence %s: %s (mínimo %s)\n", day(f.DueDate), money(f.Total, f.Cur), money(f.Minimum, f.Cur))
			}
		}
		if b.Len() == 0 {
			return "Nenhum cartão de crédito nos bancos conectados.", nil
		}
		return b.String(), nil

	case "pluggy_investimentos":
		items, err := p.items(env)
		if err != nil {
			return "", err
		}
		var b strings.Builder
		total := 0.0
		for _, it := range items {
			var r struct {
				Results []struct {
					Name     string  `json:"name"`
					Type     string  `json:"type"`
					Subtype  string  `json:"subtype"`
					Balance  float64 `json:"balance"`
					Amount   float64 `json:"amountProfit"`
					Rate     float64 `json:"lastTwelveMonthsRate"`
					Due      string  `json:"dueDate"`
					Currency string  `json:"currencyCode"`
				} `json:"results"`
			}
			if err := p.get(ctx, env, "/investments", url.Values{"itemId": {it}}, &r); err != nil {
				return "", err
			}
			for _, iv := range r.Results {
				total += iv.Balance
				fmt.Fprintf(&b, "- %s (%s): %s", iv.Name, strings.ToLower(orStr(iv.Subtype, iv.Type)), money(iv.Balance, iv.Currency))
				if iv.Amount != 0 {
					fmt.Fprintf(&b, ", rendeu %s", money(iv.Amount, iv.Currency))
				}
				if iv.Rate != 0 {
					fmt.Fprintf(&b, ", %.2f%% em 12 meses", iv.Rate)
				}
				if iv.Due != "" {
					fmt.Fprintf(&b, ", vence %s", day(iv.Due))
				}
				b.WriteString("\n")
			}
		}
		if b.Len() == 0 {
			return "Nenhum investimento nos bancos conectados.", nil
		}
		return fmt.Sprintf("Total: %s\n%s", money(total, ""), b.String()), nil
	}
	return "", fmt.Errorf("ferramenta desconhecida %s", tool)
}

func (p *Pluggy) transactions(ctx context.Context, env *Env, a Args) (string, error) {
	now := env.now().In(env.zone())
	from, to := now.AddDate(0, 0, -30).Format("2006-01-02"), now.Format("2006-01-02")
	for _, d := range []struct {
		arg string
		dst *string
	}{{"de", &from}, {"ate", &to}} {
		if v := strings.TrimSpace(a.Str(d.arg)); v != "" {
			if _, err := time.Parse("2006-01-02", v); err != nil {
				return "", fmt.Errorf("data inválida %q: use AAAA-MM-DD", v)
			}
			*d.dst = v
		}
	}
	limit := a.Int("limite", 100, 1, 500)
	search := strings.ToLower(strings.TrimSpace(a.Str("busca")))
	list, err := p.accounts(ctx, env)
	if err != nil {
		return "", err
	}
	chosen := pick(list, a.Str("conta"), "")
	if len(chosen) == 0 {
		return "", fmt.Errorf("nenhuma conta com %q; veja os nomes em pluggy_contas", a.Str("conta"))
	}
	type line struct {
		date, text string
	}
	var lines []line
	in, out := 0.0, 0.0
	for _, ac := range chosen {
		for page := 1; page <= 5; page++ {
			var r struct {
				Results []struct {
					Date        string  `json:"date"`
					Description string  `json:"description"`
					Amount      float64 `json:"amount"`
					Currency    string  `json:"currencyCode"`
					Category    string  `json:"category"`
					Type        string  `json:"type"` // DEBIT, CREDIT
				} `json:"results"`
				TotalPages int `json:"totalPages"`
			}
			q := url.Values{"accountId": {ac.ID}, "from": {from}, "to": {to}, "pageSize": {"500"}, "page": {fmt.Sprint(page)}}
			if err := p.get(ctx, env, "/transactions", q, &r); err != nil {
				return "", err
			}
			for _, t := range r.Results {
				if search != "" && !strings.Contains(strings.ToLower(t.Description), search) {
					continue
				}
				amount := t.Amount
				if ac.Type == "CREDIT" && amount > 0 && t.Type != "CREDIT" {
					amount = -amount // card purchases come positive
				}
				if amount < 0 {
					out += amount
				} else {
					in += amount
				}
				text := fmt.Sprintf("%s  %s  %s", day(t.Date), money(amount, t.Currency), cut(t.Description, 80))
				if t.Category != "" {
					text += "  [" + t.Category + "]"
				}
				if len(chosen) > 1 {
					text += "  · " + ac.label()
				}
				lines = append(lines, line{t.Date, text})
			}
			if page >= r.TotalPages {
				break
			}
		}
	}
	if len(lines) == 0 {
		return fmt.Sprintf("Nenhum lançamento de %s a %s.", from, to), nil
	}
	sort.Slice(lines, func(i, j int) bool { return lines[i].date > lines[j].date })
	var b strings.Builder
	fmt.Fprintf(&b, "%d lançamentos de %s a %s: entradas %s, saídas %s.\n", len(lines), from, to, money(in, ""), money(out, ""))
	for i, l := range lines {
		if i == limit {
			fmt.Fprintf(&b, "… e mais %d (use de/ate ou busca para ver).\n", len(lines)-limit)
			break
		}
		b.WriteString(l.text + "\n")
	}
	return b.String(), nil
}

// day turns an ISO timestamp into its date.
func day(ts string) string {
	if len(ts) >= 10 {
		return ts[:10]
	}
	return ts
}

// Incoming is every credit into the owner's bank accounts since a day, for
// confirming Pix charges.
func (p *Pluggy) Incoming(ctx context.Context, env *Env, since time.Time) ([]Incoming, error) {
	list, err := p.accounts(ctx, env)
	if err != nil {
		return nil, err
	}
	from := since.In(env.zone()).Format("2006-01-02")
	to := env.now().In(env.zone()).AddDate(0, 0, 1).Format("2006-01-02")
	out := []Incoming{}
	for _, ac := range list {
		if ac.Type != "BANK" {
			continue
		}
		for page := 1; page <= 5; page++ {
			var r struct {
				Results []struct {
					ID          string  `json:"id"`
					Date        string  `json:"date"`
					Description string  `json:"description"`
					Amount      float64 `json:"amount"`
					Type        string  `json:"type"`
					PaymentData *struct {
						Payer *struct {
							Name string `json:"name"`
						} `json:"payer"`
					} `json:"paymentData"`
				} `json:"results"`
				TotalPages int `json:"totalPages"`
			}
			q := url.Values{"accountId": {ac.ID}, "from": {from}, "to": {to}, "pageSize": {"500"}, "page": {fmt.Sprint(page)}}
			if err := p.get(ctx, env, "/transactions", q, &r); err != nil {
				return nil, err
			}
			for _, t := range r.Results {
				if t.Amount <= 0 || (t.Type != "" && t.Type != "CREDIT") {
					continue
				}
				at, err := time.Parse(time.RFC3339, t.Date)
				if err != nil {
					continue
				}
				in := Incoming{ID: t.ID, At: at, Cents: int64(math.Round(t.Amount * 100)), Text: t.Description, Account: ac.label()}
				if t.PaymentData != nil && t.PaymentData.Payer != nil {
					in.Payer = t.PaymentData.Payer.Name
				}
				out = append(out, in)
			}
			if page >= r.TotalPages {
				break
			}
		}
	}
	return out, nil
}
