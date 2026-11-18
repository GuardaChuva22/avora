package connect

import (
	"context"
	"crypto/rand"
	"encoding/json"
	"errors"
	"fmt"
	"math"
	"regexp"
	"sort"
	"strconv"
	"strings"
	"time"

	"github.com/skip2/go-qrcode"
)

// Pix: charges paid straight into the owner's bank. The server writes the
// Pix "copia e cola" (the BR Code of the Banco Central's EMV-QR manual,
// static, with the amount and an id) for the owner's own Pix key, so the
// money lands in the key's bank with no intermediary and no fee. Payment is
// confirmed by reading the bank statement through Pluggy (Env.Incoming).
// Nothing here moves money: a charge is a text and a QR the agent hands on.
type Pix struct{}

// Incoming is money that came into one of the owner's accounts.
type Incoming struct {
	ID      string // the transaction, so one payment pays one charge
	At      time.Time
	Cents   int64
	Payer   string
	Text    string // the bank's description
	Account string
}

const pixMaxCharges = 300

type pixCharge struct {
	ID       string     `json:"id"` // also the BR Code txid
	Cents    int64      `json:"centavos"`
	What     string     `json:"desc,omitempty"`
	Client   string     `json:"cliente,omitempty"`
	Created  time.Time  `json:"em"`
	Status   string     `json:"status"` // pendente, paga, cancelada
	PaidAt   *time.Time `json:"paga_em,omitempty"`
	PaidBy   string     `json:"pagador,omitempty"`
	PaidTx   string     `json:"tx,omitempty"`
	PaidHow  string     `json:"como,omitempty"` // extrato, manual
	Code     string     `json:"code"`
}

func (p *Pix) Info() Info {
	return Info{Kind: "pix", Name: "Pix (cobranças)", Ready: true, Category: "Com chave ou token", Auth: "token", Apps: []string{"Pix"},
		Description: "Cobranças por Pix copia-e-cola e QR com a sua chave: o dinheiro cai direto no seu banco, sem taxa. Com o Pluggy conectado, o Avora confere no extrato quem pagou.",
		Help: "Informe a chave Pix onde quer receber (CPF, CNPJ, e-mail, celular com +55 ou chave aleatória), o nome do recebedor como está no banco e a cidade. " +
			"Para o Avora saber quando pagaram, conecte também o Pluggy com o banco dessa chave.",
		Fields: []Field{
			{Key: "chave", Label: "Chave Pix"},
			{Key: "nome", Label: "Nome do recebedor (como no banco)"},
			{Key: "cidade", Label: "Cidade"},
		}}
}

func (p *Pix) Tools() []Tool {
	return []Tool{
		{Name: "pix_cobrar", Risk: Write,
			Description: "Cria uma cobrança Pix com a chave do dono: devolve o Pix copia-e-cola e o QR (imagem na sua pasta) para mandar ao cliente. O dinheiro cai direto no banco do dono. Gerar não envia nada; mandar ao cliente é com whatsapp_enviar ou gmail (pede aprovação).",
			Params: `{"type": "object", "properties": {
	"valor": {"type": "number", "description": "Em reais, ex.: 150.00."},
	"descricao": {"type": "string", "description": "O que está sendo cobrado (aparece no app do banco de quem paga, se o banco mostrar)."},
	"cliente": {"type": "string", "description": "Nome de quem vai pagar: ajuda a reconhecer o pagamento no extrato."}
}, "required": ["valor"]}`,
			Summary: func(a Args) string {
				return "cobrança Pix de R$ " + a.Str("valor") + quoted(a.Str("cliente"))
			}},
		{Name: "pix_cobrancas", Risk: Read,
			Description: "Lista as cobranças Pix com o status (pendente, paga, cancelada), quem pagou e quando, mais o total em aberto.",
			Params: `{"type": "object", "properties": {
	"status": {"type": "string", "enum": ["pendente", "paga", "cancelada", "todas"], "description": "Padrão: todas as dos últimos 60 dias."}}}`,
			Summary: func(a Args) string { return "cobranças Pix" + quoted(a.Str("status")) }},
		{Name: "pix_conferir", Risk: Write,
			Description: "Confere no extrato dos bancos (Pluggy) se as cobranças pendentes foram pagas e marca as pagas. O extrato do Pluggy pode levar algumas horas para mostrar um Pix.",
			Params:      `{"type": "object", "properties": {}}`,
			Summary:     func(Args) string { return "conferiu os pagamentos Pix" }},
		{Name: "pix_marcar", Risk: Write,
			Description: "Marca uma cobrança à mão: paga (o dono disse que recebeu) ou cancelada.",
			Params: `{"type": "object", "properties": {
	"id": {"type": "string"},
	"status": {"type": "string", "enum": ["paga", "cancelada", "pendente"]}
}, "required": ["id", "status"]}`,
			Summary: func(a Args) string { return "marcou a cobrança " + a.Str("id") + " como " + a.Str("status") }},
	}
}

func (p *Pix) Check(_ context.Context, env *Env) (string, error) {
	key, err := pixKey(env.Cred.Extra["chave"])
	if err != nil {
		return "", &AuthError{Msg: err.Error()}
	}
	name, city := pixText(env.Cred.Extra["nome"], 25), pixText(env.Cred.Extra["cidade"], 15)
	if name == "" || city == "" {
		return "", &AuthError{Msg: "faltou o nome do recebedor ou a cidade"}
	}
	if _, err := BRCode(key, name, city, 100, "", "CHECK"); err != nil {
		return "", &AuthError{Msg: err.Error()}
	}
	return "chave " + maskKey(key) + " · " + name, nil
}

// ── The BR Code ─────────────────────────────────────────────────

var (
	evpKey   = regexp.MustCompile(`^[0-9a-f]{8}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{12}$`)
	emailKey = regexp.MustCompile(`^[^@\s]+@[^@\s]+\.[^@\s]+$`)
	txidOK   = regexp.MustCompile(`^[A-Za-z0-9]{1,25}$`)
)

func onlyDigits(s string) string {
	return strings.Map(func(r rune) rune {
		if r >= '0' && r <= '9' {
			return r
		}
		return -1
	}, s)
}

func cpfValid(d string) bool {
	if len(d) != 11 || strings.Count(d, d[:1]) == 11 {
		return false
	}
	for n := 9; n <= 10; n++ {
		sum := 0
		for i := 0; i < n; i++ {
			sum += int(d[i]-'0') * (n + 1 - i)
		}
		if v := sum * 10 % 11 % 10; v != int(d[n]-'0') {
			return false
		}
	}
	return true
}

// pixKey normalises a Pix key the way the DICT stores it.
func pixKey(raw string) (string, error) {
	k := strings.TrimSpace(raw)
	low := strings.ToLower(k)
	switch {
	case k == "":
		return "", errors.New("faltou a chave Pix")
	case evpKey.MatchString(low):
		return low, nil
	case emailKey.MatchString(k):
		return low, nil
	case strings.HasPrefix(k, "+"):
		d := onlyDigits(k)
		if len(d) < 12 || len(d) > 13 {
			return "", fmt.Errorf("celular %q: use +55, DDD e número", k)
		}
		return "+" + d, nil
	}
	d := onlyDigits(k)
	switch {
	case len(d) == 14:
		return d, nil // CNPJ
	case len(d) == 11 && cpfValid(d):
		return d, nil
	case len(d) == 11 || len(d) == 10:
		return "+55" + d, nil // a cell phone without +55
	case len(d) == 13 && strings.HasPrefix(d, "55"):
		return "+" + d, nil
	}
	return "", fmt.Errorf("não reconheci a chave Pix %q: use CPF, CNPJ, e-mail, celular com +55 ou a chave aleatória", k)
}

func maskKey(k string) string {
	r := []rune(k)
	if len(r) <= 6 {
		return k
	}
	return string(r[:3]) + strings.Repeat("•", len(r)-6) + string(r[len(r)-3:])
}

var pixFold = strings.NewReplacer("á", "a", "à", "a", "â", "a", "ã", "a", "ä", "a", "é", "e", "ê", "e", "è", "e", "í", "i", "ì", "i", "ó", "o", "ô", "o", "õ", "o", "ö", "o", "ú", "u", "ü", "u", "ç", "c", "ñ", "n",
	"Á", "A", "À", "A", "Â", "A", "Ã", "A", "É", "E", "Ê", "E", "Í", "I", "Ó", "O", "Ô", "O", "Õ", "O", "Ú", "U", "Ü", "U", "Ç", "C", "Ñ", "N")

// pixText keeps what every bank reads: ASCII, no control characters, cut.
func pixText(s string, n int) string {
	s = pixFold.Replace(strings.TrimSpace(s))
	var b strings.Builder
	for _, r := range s {
		if r >= 32 && r < 127 {
			b.WriteRune(r)
		}
	}
	out := strings.Join(strings.Fields(b.String()), " ")
	if len(out) > n {
		out = strings.TrimSpace(out[:n])
	}
	return out
}

func emv(id, value string) string { return fmt.Sprintf("%s%02d%s", id, len(value), value) }

// crc16 is CRC-16/CCITT-FALSE, the BR Code checksum.
func crc16(s string) string {
	crc := uint16(0xFFFF)
	for i := 0; i < len(s); i++ {
		crc ^= uint16(s[i]) << 8
		for range 8 {
			if crc&0x8000 != 0 {
				crc = crc<<1 ^ 0x1021
			} else {
				crc <<= 1
			}
		}
	}
	return fmt.Sprintf("%04X", crc)
}

// BRCode is a static Pix copia-e-cola. cents 0 lets the payer type the
// amount; txid "" is "***".
func BRCode(key, name, city string, cents int64, what, txid string) (string, error) {
	if txid == "" {
		txid = "***"
	} else if !txidOK.MatchString(txid) {
		return "", fmt.Errorf("txid inválido %q", txid)
	}
	gui := emv("00", "br.gov.bcb.pix") + emv("01", key)
	if what = pixText(what, 72); what != "" {
		room := 99 - len(gui) - 4
		if room > 0 {
			if len(what) > room {
				what = strings.TrimSpace(what[:room])
			}
			gui += emv("02", what)
		}
	}
	if len(gui) > 99 {
		return "", errors.New("a chave Pix é longa demais")
	}
	name, city = pixText(name, 25), pixText(city, 15)
	if name == "" || city == "" {
		return "", errors.New("faltou o nome do recebedor ou a cidade")
	}
	s := emv("00", "01") + emv("26", gui) + emv("52", "0000") + emv("53", "986")
	if cents > 0 {
		s += emv("54", fmt.Sprintf("%d.%02d", cents/100, cents%100))
	}
	s += emv("58", "BR") + emv("59", name) + emv("60", city) + emv("62", emv("05", txid)) + "6304"
	return s + crc16(s), nil
}

// ── Charges ─────────────────────────────────────────────────────

func charges(cred *Cred) []pixCharge {
	var out []pixCharge
	_ = json.Unmarshal([]byte(cred.Extra["cobrancas"]), &out)
	return out
}

func saveCharges(env *Env, list []pixCharge) {
	if len(list) > pixMaxCharges {
		// Drop the oldest that are settled; pending ones stay.
		keep := []pixCharge{}
		extra := len(list) - pixMaxCharges
		for _, c := range list {
			if extra > 0 && c.Status != "pendente" {
				extra--
				continue
			}
			keep = append(keep, c)
		}
		list = keep
	}
	b, _ := json.Marshal(list)
	env.Cred.Extra["cobrancas"] = string(b)
	env.Changed = true
}

func newTxid() string {
	const abc = "ABCDEFGHJKLMNPQRSTUVWXYZ23456789"
	b := make([]byte, 10)
	_, _ = rand.Read(b)
	for i := range b {
		b[i] = abc[int(b[i])%len(abc)]
	}
	return "AV" + string(b)
}

func pixReais(c int64) string {
	s := strconv.FormatInt(c/100, 10)
	for i := len(s) - 3; i > 0; i -= 3 {
		s = s[:i] + "." + s[i:]
	}
	return fmt.Sprintf("R$ %s,%02d", s, c%100)
}

func parseReais(a Args, k string) (int64, error) {
	var v float64
	switch x := a[k].(type) {
	case float64:
		v = x
	case string:
		s := strings.TrimSpace(strings.TrimPrefix(strings.TrimSpace(x), "R$"))
		if strings.Contains(s, ",") {
			s = strings.ReplaceAll(strings.ReplaceAll(s, ".", ""), ",", ".")
		}
		f, err := strconv.ParseFloat(strings.TrimSpace(s), 64)
		if err != nil {
			return 0, fmt.Errorf("valor inválido %q", x)
		}
		v = f
	default:
		return 0, errors.New("falta o argumento valor")
	}
	c := int64(math.Round(v * 100))
	if c <= 0 || c > 100_000_000 {
		return 0, fmt.Errorf("valor fora do limite: %v", v)
	}
	return c, nil
}

func chargeLine(c pixCharge, loc *time.Location) string {
	line := fmt.Sprintf("- %s · %s · %s", c.ID, pixReais(c.Cents), c.Status)
	if c.Client != "" {
		line += " · " + c.Client
	}
	if c.What != "" {
		line += " · " + cut(c.What, 60)
	}
	line += " · criada " + c.Created.In(loc).Format("02/01 15:04")
	if c.PaidAt != nil {
		line += " · paga " + c.PaidAt.In(loc).Format("02/01")
		if c.PaidBy != "" {
			line += " por " + c.PaidBy
		}
	}
	return line
}

func (p *Pix) Call(ctx context.Context, env *Env, tool string, a Args) (string, error) {
	if env.Cred.Extra == nil {
		env.Cred.Extra = map[string]string{}
	}
	loc := env.zone()
	list := charges(env.Cred)
	switch tool {
	case "pix_cobrar":
		cents, err := parseReais(a, "valor")
		if err != nil {
			return "", err
		}
		key, err := pixKey(env.Cred.Extra["chave"])
		if err != nil {
			return "", &AuthError{Msg: err.Error()}
		}
		c := pixCharge{ID: newTxid(), Cents: cents, What: cut(a.Str("descricao"), 200), Client: cut(a.Str("cliente"), 80),
			Created: env.now(), Status: "pendente"}
		c.Code, err = BRCode(key, env.Cred.Extra["nome"], env.Cred.Extra["cidade"], cents, c.What, c.ID)
		if err != nil {
			return "", err
		}
		saveCharges(env, append(list, c))
		msg := fmt.Sprintf("Cobrança %s de %s criada. Pix copia e cola (mande assim, numa linha só):\n%s", c.ID, pixReais(cents), c.Code)
		if png, err := qrcode.Encode(c.Code, qrcode.Medium, 512); err == nil && env.Files != nil {
			if path, err := env.Files.Save("pix-"+c.ID+".png", png); err == nil {
				msg += "\nQR code: " + path
			}
		}
		return msg + "\nQuando o cliente pagar, confira com pix_conferir.", nil

	case "pix_cobrancas":
		want := orStr(a.Str("status"), "todas")
		since := env.now().AddDate(0, 0, -60)
		open, n := int64(0), 0
		lines := []string{}
		for i := len(list) - 1; i >= 0; i-- {
			c := list[i]
			if c.Status == "pendente" {
				open += c.Cents
			}
			if (want != "todas" && c.Status != want) || (want == "todas" && c.Created.Before(since) && c.Status != "pendente") {
				continue
			}
			if n++; n > 60 {
				break
			}
			lines = append(lines, chargeLine(c, loc))
		}
		if len(lines) == 0 {
			return "Nenhuma cobrança Pix " + want + ".", nil
		}
		return fmt.Sprintf("Em aberto: %s.\n%s", pixReais(open), strings.Join(lines, "\n")), nil

	case "pix_marcar":
		id, err := a.Need("id")
		if err != nil {
			return "", err
		}
		st := a.Str("status")
		if st != "paga" && st != "cancelada" && st != "pendente" {
			return "", fmt.Errorf("status %q: use paga, cancelada ou pendente", st)
		}
		for i := range list {
			if strings.EqualFold(list[i].ID, id) {
				list[i].Status = st
				if st == "paga" {
					now := env.now()
					list[i].PaidAt, list[i].PaidHow = &now, "manual"
				} else {
					list[i].PaidAt, list[i].PaidBy, list[i].PaidTx, list[i].PaidHow = nil, "", "", ""
				}
				saveCharges(env, list)
				return chargeLine(list[i], loc), nil
			}
		}
		return "", fmt.Errorf("não achei a cobrança %s", id)

	case "pix_conferir":
		if env.Incoming == nil {
			return "", errors.New("para conferir os pagamentos, o dono precisa conectar o Pluggy (com o banco da chave Pix) em Plugins; até lá, use pix_marcar quando ele disser que recebeu")
		}
		pending := []int{}
		since := env.now()
		for i, c := range list {
			if c.Status == "pendente" {
				pending = append(pending, i)
				if c.Created.Before(since) {
					since = c.Created
				}
			}
		}
		if len(pending) == 0 {
			return "Nenhuma cobrança pendente.", nil
		}
		in, err := env.Incoming(ctx, since.Add(-24*time.Hour))
		if err != nil {
			return "", fmt.Errorf("não consegui ler o extrato: %w", err)
		}
		paid, unsure := matchPayments(list, pending, in)
		if len(paid) > 0 {
			saveCharges(env, list)
		}
		var b strings.Builder
		for _, i := range paid {
			b.WriteString("Paga: " + chargeLine(list[i], loc) + "\n")
		}
		for _, s := range unsure {
			b.WriteString(s + "\n")
		}
		still := 0
		for _, i := range pending {
			if list[i].Status == "pendente" {
				still++
			}
		}
		fmt.Fprintf(&b, "%d pendente(s). O extrato do Pluggy pode levar algumas horas para mostrar um Pix recebido.", still)
		return b.String(), nil
	}
	return "", fmt.Errorf("ferramenta desconhecida %s", tool)
}

// nameWords are the parts of a name worth comparing (no "da", "de"...).
func nameWords(s string) []string {
	out := []string{}
	for _, w := range strings.Fields(strings.ToLower(pixText(s, 200))) {
		if len(w) > 2 {
			out = append(out, w)
		}
	}
	return out
}

func sameName(client, payer string) bool {
	cw, pw := nameWords(client), nameWords(payer)
	for _, a := range cw {
		for _, b := range pw {
			if a == b {
				return true
			}
		}
	}
	return false
}

// matchPayments marks the pending charges an incoming credit pays: same
// amount, after the charge was made, each credit used once. When several
// pending charges have the amount, the payer's name decides; a tie is
// reported, not guessed.
func matchPayments(list []pixCharge, pending []int, in []Incoming) (paid []int, unsure []string) {
	used := map[string]bool{}
	for _, c := range list {
		if c.PaidTx != "" {
			used[c.PaidTx] = true
		}
	}
	sort.Slice(in, func(i, j int) bool { return in[i].At.Before(in[j].At) })
	for _, tx := range in {
		if used[tx.ID] || tx.Cents <= 0 {
			continue
		}
		cands := []int{}
		for _, i := range pending {
			c := list[i]
			// The statement may carry only the day: compare by day.
			if c.Status == "pendente" && c.Cents == tx.Cents && !tx.At.Before(c.Created.Truncate(24*time.Hour).Add(-24*time.Hour)) {
				cands = append(cands, i)
			}
		}
		if len(cands) == 0 {
			continue
		}
		pickIdx := -1
		if len(cands) == 1 {
			pickIdx = cands[0]
			if c := list[pickIdx]; c.Client != "" && tx.Payer != "" && !sameName(c.Client, tx.Payer) && !sameName(c.Client, tx.Text) {
				unsure = append(unsure, fmt.Sprintf("Talvez paga: %s de %s (%s), mas quem pagou foi %s em %s; confirme com o dono e use pix_marcar.",
					c.ID, pixReais(c.Cents), c.Client, tx.Payer, tx.At.Format("02/01")))
				used[tx.ID] = true
				continue
			}
		} else {
			named := []int{}
			for _, i := range cands {
				if list[i].Client != "" && (sameName(list[i].Client, tx.Payer) || sameName(list[i].Client, tx.Text)) {
					named = append(named, i)
				}
			}
			if len(named) == 1 {
				pickIdx = named[0]
			} else {
				ids := []string{}
				for _, i := range cands {
					ids = append(ids, list[i].ID)
				}
				unsure = append(unsure, fmt.Sprintf("Entrou %s de %s em %s, que pode ser de %s; confirme com o dono e use pix_marcar.",
					pixReais(tx.Cents), orStr(tx.Payer, cut(tx.Text, 60)), tx.At.Format("02/01"), strings.Join(ids, " ou ")))
				used[tx.ID] = true
				continue
			}
		}
		at := tx.At
		list[pickIdx].Status, list[pickIdx].PaidAt, list[pickIdx].PaidBy, list[pickIdx].PaidTx, list[pickIdx].PaidHow =
			"paga", &at, orStr(tx.Payer, cut(tx.Text, 60)), tx.ID, "extrato"
		used[tx.ID] = true
		paid = append(paid, pickIdx)
	}
	return paid, unsure
}
