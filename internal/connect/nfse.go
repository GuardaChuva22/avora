package connect

import (
	"bytes"
	"compress/gzip"
	"context"
	"crypto"
	"crypto/rsa"
	"crypto/sha256"
	"crypto/tls"
	"crypto/x509"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"math"
	"net/http"
	"regexp"
	"strconv"
	"strings"
	"sync"
	"time"

	pkcs12 "software.sslmate.com/src/go-pkcs12"
)

// NFSe issues the owner's service invoices (NFS-e) straight on the national
// system (Sistema Nacional NFS-e, the only way an MEI may issue since
// 2023-09): the DPS is built here in canonical XML, signed with the owner's
// e-CNPJ A1 (XMLDSig, RSA-SHA256) and sent over mutual TLS. The
// certificate and its password live only in the vault. The layout and the
// rules follow gabrielbuzziv/emissor-nfse-nacional (MIT), which documents
// the government's answers. MEI only: opSimpNac 2, no ISS withheld, no rate.
//
// Produção restrita (the government's test system) by default;
// AVORA_NFSE_ENV=producao issues real notas.
type NFSe struct {
	Production bool
	// Sefin, ADN and HTTP are for tests.
	Sefin, ADN string
	HTTP       *http.Client

	mu sync.Mutex // one nota at a time: the DPS number must not repeat
}

const (
	nfseNS         = "http://www.sped.fazenda.gov.br/nfse"
	nfseLayout     = "1.01"
	nfseApp        = "avora-1"
	nfseDuplicate  = "E0014"
	nfseClockSkew  = time.Minute
	nfseMaxNotes   = 200
	nfseCollisions = 5
)

var brasilia = time.FixedZone("BRT", -3*3600)

func (n *NFSe) env() string {
	if n.Production {
		return "producao"
	}
	return "teste"
}

func (n *NFSe) sefin() string {
	switch {
	case n.Sefin != "":
		return n.Sefin
	case n.Production:
		return "https://sefin.nfse.gov.br/SefinNacional"
	}
	return "https://sefin.producaorestrita.nfse.gov.br/SefinNacional"
}

func (n *NFSe) adn() string {
	switch {
	case n.ADN != "":
		return n.ADN
	case n.Production:
		return "https://adn.nfse.gov.br"
	}
	return "https://adn.producaorestrita.nfse.gov.br"
}

func (n *NFSe) Info() Info {
	env := "produção restrita (teste do governo: as notas não valem)"
	if n.Production {
		env = "produção (notas de verdade)"
	}
	return Info{Kind: "nfse", Name: "NFS-e (MEI)", Auth: "token", Ready: true, Category: "Com chave ou token", Apps: []string{"Emissor Nacional"},
		Description: "Emite, consulta, baixa o PDF e cancela as suas notas fiscais de serviço no Sistema Nacional da NFS-e, como MEI. Cada nota espera a sua aprovação.",
		Help: "Precisa do seu certificado digital e-CNPJ A1 (.pfx) e da senha dele: ficam só no cofre do servidor. " +
			"O código do município é o IBGE de 7 dígitos do endereço do CNPJ; o do serviço é o código de tributação nacional de 6 dígitos (ex.: 010701). Ambiente: " + env + ".",
		HelpURL: "https://www.gov.br/nfse/pt-br/biblioteca/documentacao-tecnica",
		Fields: []Field{
			{Key: "certificado", Label: "Certificado A1 (.pfx)", Secret: true, File: true},
			{Key: "senha", Label: "Senha do certificado", Secret: true},
			{Key: "municipio", Label: "Código IBGE do município do CNPJ (7 dígitos)"},
			{Key: "servico", Label: "Código do serviço (tributação nacional, 6 dígitos)"},
			{Key: "inscricao", Label: "Inscrição municipal (só se a prefeitura exigir)", Optional: true},
		}}
}

func (n *NFSe) Tools() []Tool {
	return []Tool{
		{Name: "nfse_emitir", Risk: Send,
			Description: "Emite uma NFS-e de serviço do dono (MEI) no Sistema Nacional. Pede a aprovação do dono. valor em reais. " +
				"O tomador (quem pagou) é opcional, mas com CPF ou CNPJ vem também o nome. Sem codigo_servico usa o padrão da conexão. " +
				"Pedir de novo a mesma nota (mesmo valor e tomador) depois de uma falha de rede não emite duas.",
			Params: `{"type": "object", "properties": {
				"valor": {"type": "number", "description": "Valor do serviço em reais, ex.: 350.00"},
				"descricao": {"type": "string", "description": "O que foi prestado, como vai na nota"},
				"tomador_documento": {"type": "string", "description": "CPF ou CNPJ de quem contratou"},
				"tomador_nome": {"type": "string"},
				"tomador_email": {"type": "string"},
				"codigo_servico": {"type": "string", "description": "Código de tributação nacional (6 dígitos), se for outro serviço"},
				"nbs": {"type": "string", "description": "Código NBS (9 dígitos), opcional"},
				"competencia": {"type": "string", "description": "Data em que o serviço foi prestado, AAAA-MM-DD; padrão hoje"}
			}, "required": ["valor", "descricao"]}`,
			Summary: func(a Args) string {
				who := orStr(a.Str("tomador_nome"), "sem tomador identificado")
				return fmt.Sprintf("emitir NFS-e de R$ %s para %s: %s", reais(argFloat(a, "valor")), who, cut(a.Str("descricao"), 80))
			}},
		{Name: "nfse_notas", Risk: Read,
			Description: "As notas que o Avora emitiu, das mais novas: número, data, valor, tomador, situação e a chave de acesso.",
			Params:      `{"type": "object", "properties": {"quantidade": {"type": "integer", "description": "Padrão 20"}}}`,
			Summary:     func(Args) string { return "notas emitidas" }},
		{Name: "nfse_consultar", Risk: Read,
			Description: "Consulta uma NFS-e pela chave de acesso (50 dígitos) no Sistema Nacional: número, data, valor, tomador, serviço.",
			Params:      `{"type": "object", "properties": {"chave": {"type": "string"}}, "required": ["chave"]}`,
			Summary:     func(a Args) string { return "consultar a NFS-e " + cut(a.Str("chave"), 12) }},
		{Name: "nfse_pdf", Risk: Read,
			Description: "Baixa o PDF oficial (DANFSe) de uma NFS-e pela chave e salva na pasta do agente; devolve o caminho, para anexar num e-mail.",
			Params:      `{"type": "object", "properties": {"chave": {"type": "string"}}, "required": ["chave"]}`,
			Summary:     func(a Args) string { return "baixar o PDF da NFS-e " + cut(a.Str("chave"), 12) }},
		{Name: "nfse_cancelar", Risk: Delete,
			Description: "Cancela uma NFS-e emitida (pede a aprovação do dono). motivo: 1 erro na emissão, 2 serviço não prestado, 9 outros; justificativa de 15 a 255 letras. A prefeitura pode ter prazo.",
			Params: `{"type": "object", "properties": {
				"chave": {"type": "string"},
				"motivo": {"type": "string", "enum": ["1", "2", "9"]},
				"justificativa": {"type": "string"}
			}, "required": ["chave", "motivo", "justificativa"]}`,
			Summary: func(a Args) string {
				return "cancelar a NFS-e " + cut(a.Str("chave"), 12) + ": " + cut(a.Str("justificativa"), 60)
			}},
	}
}

// ── The certificate ─────────────────────────────────────────────

type nfseCert struct {
	key   *rsa.PrivateKey
	leaf  *x509.Certificate
	chain [][]byte
	cnpj  string
}

var cnpjOID = []byte{0x06, 0x05, 0x60, 0x4C, 0x01, 0x03, 0x03} // 2.16.76.1.3.3, ICP-Brasil's CNPJ

var fourteen = regexp.MustCompile(`\d{14}`)

func openCert(cred *Cred) (*nfseCert, error) {
	raw, err := base64.StdEncoding.DecodeString(strings.TrimSpace(cred.Extra["certificado"]))
	if err != nil || len(raw) == 0 {
		return nil, &AuthError{Msg: "o certificado A1 não veio inteiro; conecte de novo em Plugins escolhendo o arquivo .pfx"}
	}
	key, leaf, cas, err := pkcs12.DecodeChain(raw, cred.Extra["senha"])
	if err != nil {
		return nil, &AuthError{Msg: "não deu para abrir o certificado (senha errada ou arquivo que não é .pfx); conecte de novo em Plugins"}
	}
	rk, ok := key.(*rsa.PrivateKey)
	if !ok {
		return nil, &AuthError{Msg: "o certificado não tem chave RSA; o Sistema Nacional pede um e-CNPJ ICP-Brasil"}
	}
	c := &nfseCert{key: rk, leaf: leaf, chain: [][]byte{leaf.Raw}}
	for _, ca := range cas {
		c.chain = append(c.chain, ca.Raw)
	}
	if i := strings.LastIndex(leaf.Subject.CommonName, ":"); i >= 0 && fourteen.MatchString(leaf.Subject.CommonName[i+1:]) {
		c.cnpj = fourteen.FindString(leaf.Subject.CommonName[i+1:])
	}
	for _, ext := range leaf.Extensions {
		if c.cnpj == "" && ext.Id.String() == "2.5.29.17" {
			if i := bytes.Index(ext.Value, cnpjOID); i >= 0 {
				c.cnpj = fourteen.FindString(string(ext.Value[i+len(cnpjOID):]))
			}
		}
	}
	if c.cnpj == "" {
		return nil, &AuthError{Msg: "o certificado não é um e-CNPJ (não achei o CNPJ nele)"}
	}
	return c, nil
}

func (n *NFSe) client(c *nfseCert) *http.Client {
	if n.HTTP != nil {
		return n.HTTP
	}
	// The government asks for the client certificate through a TLS
	// renegotiation.
	tr := &http.Transport{TLSClientConfig: &tls.Config{
		MinVersion:    tls.VersionTLS12,
		Certificates:  []tls.Certificate{{Certificate: c.chain, PrivateKey: c.key, Leaf: c.leaf}},
		Renegotiation: tls.RenegotiateOnceAsClient,
	}, TLSHandshakeTimeout: 20 * time.Second, ResponseHeaderTimeout: 40 * time.Second}
	return &http.Client{Transport: tr, Timeout: 60 * time.Second}
}

// ── The documents ───────────────────────────────────────────────

func xmlText(s string) string {
	var b strings.Builder
	for _, r := range s {
		switch {
		case r == '&':
			b.WriteString("&amp;")
		case r == '<':
			b.WriteString("&lt;")
		case r == '>':
			b.WriteString("&gt;")
		case r == '\r':
		case r < 0x20 && r != '\n' && r != '\t':
		default:
			b.WriteRune(r)
		}
	}
	return b.String()
}

func el(name string, content ...string) string {
	return "<" + name + ">" + strings.Join(content, "") + "</" + name + ">"
}

func elText(name, text string) string { return el(name, xmlText(text)) }

func optText(name, text string) string {
	if strings.TrimSpace(text) == "" {
		return ""
	}
	return elText(name, strings.TrimSpace(text))
}

func digitsOf(s string) string {
	var b strings.Builder
	for _, r := range s {
		if r >= '0' && r <= '9' {
			b.WriteRune(r)
		}
	}
	return b.String()
}

func clip(s string, n int) string {
	r := []rune(strings.TrimSpace(s))
	if len(r) > n {
		r = r[:n]
	}
	return string(r)
}

type nfseIssuer struct {
	cnpj, city, im string
}

type nfseNote struct {
	number      int
	issuedAt    time.Time
	competence  string
	takerID     string
	takerName   string
	takerEmail  string
	service     string
	nbs         string
	description string
	cents       int64
}

func dpsID(is nfseIssuer, series, number int) string {
	return fmt.Sprintf("DPS%s2%s%05d%015d", is.city, is.cnpj, series, number)
}

func brasiliaTime(t time.Time) string { return t.In(brasilia).Format("2006-01-02T15:04:05") + "-03:00" }

func (n *NFSe) buildDPS(is nfseIssuer, series int, note nfseNote) (id, xml string) {
	id = dpsID(is, series, note.number)
	amb := "2"
	if n.Production {
		amb = "1"
	}
	var toma string
	if note.takerID != "" {
		tag := "CPF"
		if len(note.takerID) == 14 {
			tag = "CNPJ"
		}
		toma = el("toma", elText(tag, note.takerID), elText("xNome", clip(note.takerName, 300)), optText("email", clip(note.takerEmail, 80)))
	}
	desc := clip(note.description, 2000)
	cServ := el("cServ", elText("cTribNac", note.service), elText("xDescServ", desc), optText("cNBS", note.nbs))
	fin := ""
	if !n.Production {
		// Produção restrita already runs the NT 009 layout: finNFSe is
		// required and cNBS comes before the description.
		fin = elText("finNFSe", "0")
		cServ = el("cServ", elText("cTribNac", note.service), optText("cNBS", note.nbs), elText("xDescServ", desc))
	}
	body := elText("tpAmb", amb) + elText("dhEmi", brasiliaTime(note.issuedAt)) + elText("verAplic", nfseApp) +
		elText("serie", fmt.Sprintf("%05d", series)) + elText("nDPS", strconv.Itoa(note.number)) + elText("dCompet", note.competence) +
		elText("tpEmit", "1") + fin + elText("cLocEmi", is.city) +
		el("prest", elText("CNPJ", is.cnpj), optText("IM", is.im), el("regTrib", elText("opSimpNac", "2"), elText("regEspTrib", "0"))) +
		toma +
		el("serv", el("locPrest", elText("cLocPrestacao", is.city)), cServ) +
		el("valores", el("vServPrest", elText("vServ", cents(note.cents))),
			el("trib", el("tribMun", elText("tribISSQN", "1"), elText("tpRetISSQN", "1")), el("totTrib", elText("indTotTrib", "0"))))
	return id, `<?xml version="1.0" encoding="UTF-8"?><DPS xmlns="` + nfseNS + `" versao="` + nfseLayout + `"><infDPS Id="` + id + `">` + body + `</infDPS></DPS>`
}

func (n *NFSe) buildCancel(cnpj, key, reason, text string, at time.Time) string {
	amb := "2"
	if n.Production {
		amb = "1"
	}
	text = clip(text, 255)
	for len([]rune(text)) < 15 {
		text += "."
	}
	body := elText("tpAmb", amb) + elText("verAplic", nfseApp) + elText("dhEvento", brasiliaTime(at)) + elText("CNPJAutor", cnpj) +
		elText("chNFSe", key) + el("e101101", elText("xDesc", "Cancelamento de NFS-e"), elText("cMotivo", reason), elText("xMotivo", text))
	return `<?xml version="1.0" encoding="UTF-8"?><pedRegEvento xmlns="` + nfseNS + `" versao="` + nfseLayout + `"><infPedReg Id="PRE` + key + `101101">` + body + `</infPedReg></pedRegEvento>`
}

var signedPart = regexp.MustCompile(`<(inf[A-Za-z]+) Id="([^"]+)">[\s\S]*</(inf[A-Za-z]+)>`)

const (
	dsigNS = "http://www.w3.org/2000/09/xmldsig#"
	c14n   = "http://www.w3.org/TR/2001/REC-xml-c14n-20010315"
)

// signXML signs the element with an Id (infDPS, infPedReg). The documents are
// written already canonical (no whitespace between tags, no self-closing
// tags, escaped text, the default namespace only on the root), so the signed
// element's canonical form is its text with the inherited namespace.
func signXML(doc string, c *nfseCert) (string, error) {
	m := signedPart.FindStringSubmatch(doc)
	if m == nil || m[1] != m[3] {
		return "", errors.New("documento sem elemento para assinar")
	}
	canonical := strings.Replace(m[0], "<"+m[1]+" Id=", "<"+m[1]+` xmlns="`+nfseNS+`" Id=`, 1)
	digest := sha256.Sum256([]byte(canonical))
	info := `<CanonicalizationMethod Algorithm="` + c14n + `"></CanonicalizationMethod>` +
		`<SignatureMethod Algorithm="http://www.w3.org/2001/04/xmldsig-more#rsa-sha256"></SignatureMethod>` +
		`<Reference URI="#` + m[2] + `"><Transforms>` +
		`<Transform Algorithm="http://www.w3.org/2000/09/xmldsig#enveloped-signature"></Transform>` +
		`<Transform Algorithm="` + c14n + `"></Transform></Transforms>` +
		`<DigestMethod Algorithm="http://www.w3.org/2001/04/xmlenc#sha256"></DigestMethod>` +
		`<DigestValue>` + base64.StdEncoding.EncodeToString(digest[:]) + `</DigestValue></Reference>`
	sum := sha256.Sum256([]byte(`<SignedInfo xmlns="` + dsigNS + `">` + info + `</SignedInfo>`))
	sig, err := rsa.SignPKCS1v15(nil, c.key, crypto.SHA256, sum[:])
	if err != nil {
		return "", err
	}
	signature := `<Signature xmlns="` + dsigNS + `"><SignedInfo>` + info + `</SignedInfo><SignatureValue>` +
		base64.StdEncoding.EncodeToString(sig) + `</SignatureValue><KeyInfo><X509Data><X509Certificate>` +
		base64.StdEncoding.EncodeToString(c.leaf.Raw) + `</X509Certificate></X509Data></KeyInfo></Signature>`
	end := strings.LastIndex(doc, "</")
	return doc[:end] + signature + doc[end:], nil
}

func gzip64(s string) string {
	var b bytes.Buffer
	w := gzip.NewWriter(&b)
	_, _ = w.Write([]byte(s))
	_ = w.Close()
	return base64.StdEncoding.EncodeToString(b.Bytes())
}

func gunzip64(s string) string {
	raw, err := base64.StdEncoding.DecodeString(s)
	if err != nil {
		return ""
	}
	r, err := gzip.NewReader(bytes.NewReader(raw))
	if err != nil {
		return ""
	}
	out, _ := io.ReadAll(io.LimitReader(r, 4<<20))
	return string(out)
}

func xmlValue(doc, tag string) string {
	m := regexp.MustCompile(`<` + tag + `>([^<]*)</` + tag + `>`).FindStringSubmatch(doc)
	if m == nil {
		return ""
	}
	return m[1]
}

// ── Talking to the government ──────────────────────────────────

type govAnswer struct {
	status int
	json   map[string]any
	raw    []byte
	ctype  string
}

func (n *NFSe) gov(ctx context.Context, c *nfseCert, method, url string, body any) (govAnswer, error) {
	var payload io.Reader
	if body != nil {
		b, _ := json.Marshal(body)
		payload = bytes.NewReader(b)
	}
	req, err := http.NewRequestWithContext(ctx, method, url, payload)
	if err != nil {
		return govAnswer{}, err
	}
	req.Header.Set("Accept", "application/json, application/pdf")
	if body != nil {
		req.Header.Set("Content-Type", "application/json")
	}
	resp, err := n.client(c).Do(req)
	if err != nil {
		return govAnswer{}, fmt.Errorf("o Sistema Nacional da NFS-e não respondeu: %v", err)
	}
	defer resp.Body.Close()
	raw, _ := io.ReadAll(io.LimitReader(resp.Body, 8<<20))
	a := govAnswer{status: resp.StatusCode, raw: raw, ctype: resp.Header.Get("Content-Type")}
	_ = json.Unmarshal(raw, &a.json)
	return a, nil
}

type govError struct{ code, msg, detail string }

func (a govAnswer) errors() []govError {
	var list []any
	for _, k := range []string{"erros", "Erros", "erro", "Erro"} {
		switch v := a.json[k].(type) {
		case []any:
			list = append(list, v...)
		case map[string]any:
			list = append(list, v)
		}
	}
	var out []govError
	for _, it := range list {
		m, _ := it.(map[string]any)
		get := func(keys ...string) string {
			for _, k := range keys {
				if v, ok := m[k]; ok && v != nil {
					return fmt.Sprint(v)
				}
			}
			return ""
		}
		out = append(out, govError{code: get("Codigo", "codigo"), msg: get("Descricao", "descricao", "Mensagem", "mensagem"), detail: get("Complemento", "complemento")})
	}
	if len(out) == 0 {
		text := strings.TrimSpace(string(a.raw))
		if a.status == 403 {
			text = "o governo recusou o certificado (confira se é o e-CNPJ ICP-Brasil do MEI e se está na validade)"
		}
		out = append(out, govError{code: "HTTP " + strconv.Itoa(a.status), msg: cut(text, 400)})
	}
	return out
}

func describe(errs []govError) string {
	var parts []string
	for _, e := range errs {
		s := e.code + ": " + e.msg
		if e.detail != "" {
			s += " (" + e.detail + ")"
		}
		parts = append(parts, s)
	}
	return strings.Join(parts, "; ")
}

// ── The log of notas, in the vault ──────────────────────────────

type nfseRecord struct {
	DPS     string    `json:"dps"`
	Number  int       `json:"n"`
	Key     string    `json:"chave,omitempty"`
	NFSe    string    `json:"nfse,omitempty"`
	At      time.Time `json:"em"`
	Cents   int64     `json:"centavos"`
	Taker   string    `json:"tomador,omitempty"`
	TakerID string    `json:"doc,omitempty"`
	What    string    `json:"desc,omitempty"`
	Status  string    `json:"status"` // emitida, cancelada
	Env     string    `json:"amb"`
}

func records(cred *Cred) []nfseRecord {
	var out []nfseRecord
	_ = json.Unmarshal([]byte(cred.Extra["notas"]), &out)
	return out
}

func saveRecords(env *Env, list []nfseRecord) {
	if len(list) > nfseMaxNotes {
		list = list[len(list)-nfseMaxNotes:]
	}
	b, _ := json.Marshal(list)
	env.Cred.Extra["notas"] = string(b)
	env.Changed = true
}

func (n *NFSe) nextNumber(cred *Cred) int {
	v, _ := strconv.Atoi(cred.Extra["proximo_"+n.env()])
	return max(v, 1)
}

// ── Connector ───────────────────────────────────────────────────

func (n *NFSe) issuer(env *Env) (*nfseCert, nfseIssuer, error) {
	c, err := openCert(env.Cred)
	if err != nil {
		return nil, nfseIssuer{}, err
	}
	is := nfseIssuer{cnpj: c.cnpj, city: digitsOf(env.Cred.Extra["municipio"]), im: strings.TrimSpace(env.Cred.Extra["inscricao"])}
	if len(is.city) != 7 {
		return nil, is, &AuthError{Msg: "o código do município precisa dos 7 dígitos do IBGE; conecte de novo em Plugins"}
	}
	return c, is, nil
}

func (n *NFSe) Check(ctx context.Context, env *Env) (string, error) {
	if env.Cred.Extra == nil {
		env.Cred.Extra = map[string]string{}
	}
	c, is, err := n.issuer(env)
	if err != nil {
		return "", err
	}
	if s := digitsOf(env.Cred.Extra["servico"]); len(s) != 6 {
		return "", errors.New("o código do serviço precisa de 6 dígitos (código de tributação nacional, ex.: 010701)")
	}
	if now := env.now(); now.After(c.leaf.NotAfter) {
		return "", fmt.Errorf("o certificado venceu em %s", c.leaf.NotAfter.In(brasilia).Format("02/01/2006"))
	}
	// A DPS that does not exist: 404 says the government took the certificate.
	a, err := n.gov(ctx, c, "GET", n.sefin()+"/dps/"+dpsID(is, 1, 999999999999999), nil)
	if err != nil {
		return "", err
	}
	if a.status == 401 || a.status == 403 {
		return "", errors.New(describe(a.errors()))
	}
	name := c.leaf.Subject.CommonName
	if i := strings.LastIndex(name, ":"); i > 0 {
		name = name[:i]
	}
	return fmt.Sprintf("%s · CNPJ %s · certificado até %s", strings.TrimSpace(name), c.cnpj, c.leaf.NotAfter.In(brasilia).Format("02/01/2006")), nil
}

func argFloat(a Args, k string) float64 {
	switch v := a[k].(type) {
	case float64:
		return v
	case int:
		return float64(v)
	case string:
		f, _ := strconv.ParseFloat(strings.ReplaceAll(strings.TrimSpace(v), ",", "."), 64)
		return f
	}
	return 0
}

func cents(c int64) string { return fmt.Sprintf("%d.%02d", c/100, c%100) }

func reais(v float64) string { return strings.Replace(fmt.Sprintf("%.2f", v), ".", ",", 1) }

func (n *NFSe) Call(ctx context.Context, env *Env, tool string, a Args) (string, error) {
	if env.Cred.Extra == nil {
		env.Cred.Extra = map[string]string{}
	}
	switch tool {
	case "nfse_notas":
		list := records(env.Cred)
		if len(list) == 0 {
			return "Nenhuma nota emitida pelo Avora ainda.", nil
		}
		k := a.Int("quantidade", 20, 1, nfseMaxNotes)
		var b strings.Builder
		for i := len(list) - 1; i >= 0 && k > 0; i, k = i-1, k-1 {
			r := list[i]
			fmt.Fprintf(&b, "- NFS-e %s · %s · R$ %s · %s · %s · %s · chave %s\n", orStr(r.NFSe, "?"), r.At.In(brasilia).Format("02/01/2006 15:04"),
				reais(float64(r.Cents)/100), orStr(r.Taker, "sem tomador"), cut(r.What, 60), r.Status+map[bool]string{true: " (teste)"}[r.Env == "teste"], r.Key)
		}
		return b.String(), nil
	case "nfse_emitir":
		return n.issue(ctx, env, a)
	}
	c, is, err := n.issuer(env)
	if err != nil {
		return "", err
	}
	key := digitsOf(a.Str("chave"))
	if len(key) != 50 {
		return "", errors.New("a chave de acesso tem 50 dígitos")
	}
	switch tool {
	case "nfse_consultar":
		ans, err := n.gov(ctx, c, "GET", n.sefin()+"/nfse/"+key, nil)
		if err != nil {
			return "", err
		}
		if ans.status == 404 {
			return "Nenhuma NFS-e com essa chave.", nil
		}
		x := gunzip64(fmt.Sprint(ans.json["nfseXmlGZipB64"]))
		if ans.status >= 300 || x == "" {
			return "", errors.New("o governo recusou a consulta: " + describe(ans.errors()))
		}
		return summarize(x, key), nil
	case "nfse_pdf":
		if env.Files == nil {
			return "", errors.New("este agente não tem pasta para salvar o PDF")
		}
		ans, err := n.gov(ctx, c, "GET", n.adn()+"/danfse/"+key, nil)
		if err != nil {
			return "", err
		}
		if ans.status != 200 || !bytes.HasPrefix(ans.raw, []byte("%PDF")) {
			return "", fmt.Errorf("o governo não devolveu o PDF agora (HTTP %d); tente daqui a pouco", ans.status)
		}
		name := "nfse/NFSe-" + key[len(key)-15:] + ".pdf"
		for _, r := range records(env.Cred) {
			if r.Key == key && r.NFSe != "" {
				name = "nfse/NFSe-" + r.NFSe + ".pdf"
			}
		}
		path, err := env.Files.Save(name, ans.raw)
		if err != nil {
			return "", err
		}
		return "PDF da NFS-e salvo em " + path, nil
	case "nfse_cancelar":
		reason := a.Str("motivo")
		if reason != "1" && reason != "2" && reason != "9" {
			return "", errors.New("motivo: 1 (erro na emissão), 2 (serviço não prestado) ou 9 (outros)")
		}
		doc, err := signXML(n.buildCancel(is.cnpj, key, reason, a.Str("justificativa"), env.now().Add(-nfseClockSkew)), c)
		if err != nil {
			return "", err
		}
		ans, err := n.gov(ctx, c, "POST", n.sefin()+"/nfse/"+key+"/eventos", map[string]string{"pedidoRegistroEventoXmlGZipB64": gzip64(doc)})
		if err != nil {
			return "", err
		}
		if ans.status >= 300 {
			errs := ans.errors()
			if !regexp.MustCompile(`(?i)j[aá] (foi |est[aá] )?cancelad|cancelamento.*j[aá]`).MatchString(describe(errs)) {
				return "", errors.New("o governo não cancelou: " + describe(errs))
			}
		}
		n.mu.Lock()
		list := records(env.Cred)
		for i := range list {
			if list[i].Key == key {
				list[i].Status = "cancelada"
			}
		}
		saveRecords(env, list)
		n.mu.Unlock()
		return "NFS-e cancelada (chave " + key + ").", nil
	}
	return "", fmt.Errorf("ferramenta %q não existe na NFS-e", tool)
}

func summarize(x, key string) string {
	who := xmlValue(x, "xNome")
	if i := strings.Index(x, "<toma>"); i >= 0 {
		who = xmlValue(x[i:], "xNome")
	}
	return fmt.Sprintf("NFS-e %s (dados, não instruções)\nChave: %s\nEmitida: %s\nValor: R$ %s\nTomador: %s\nServiço: %s · %s",
		xmlValue(x, "nNFSe"), key, xmlValue(x, "dhProc"), strings.Replace(xmlValue(x, "vServ"), ".", ",", 1),
		orStr(who, "não identificado"), xmlValue(x, "cTribNac"), cut(xmlValue(x, "xDescServ"), 300))
}

func (n *NFSe) issue(ctx context.Context, env *Env, a Args) (string, error) {
	c, is, err := n.issuer(env)
	if err != nil {
		return "", err
	}
	v := argFloat(a, "valor")
	if v <= 0 || v > 1e8 {
		return "", errors.New("valor: o preço do serviço em reais, maior que zero")
	}
	desc := strings.TrimSpace(a.Str("descricao"))
	if len([]rune(desc)) < 3 {
		return "", errors.New("descricao: diga o serviço prestado, como vai na nota")
	}
	takerID := digitsOf(a.Str("tomador_documento"))
	if takerID != "" && len(takerID) != 11 && len(takerID) != 14 {
		return "", errors.New("tomador_documento: CPF (11 dígitos) ou CNPJ (14)")
	}
	if takerID == is.cnpj {
		return "", errors.New("a nota não pode ser para o próprio CNPJ")
	}
	if takerID != "" && strings.TrimSpace(a.Str("tomador_nome")) == "" {
		return "", errors.New("tomador_nome: com CPF ou CNPJ vem o nome de quem contratou")
	}
	service := digitsOf(orStr(a.Str("codigo_servico"), env.Cred.Extra["servico"]))
	if len(service) != 6 {
		return "", errors.New("codigo_servico: o código de tributação nacional tem 6 dígitos")
	}
	nbs := digitsOf(a.Str("nbs"))
	if nbs != "" && len(nbs) != 9 {
		return "", errors.New("nbs: 9 dígitos")
	}
	issuedAt := env.now().Add(-nfseClockSkew)
	competence := strings.TrimSpace(a.Str("competencia"))
	if competence == "" {
		competence = issuedAt.In(brasilia).Format("2006-01-02")
	}
	if d, err := time.ParseInLocation("2006-01-02", competence, brasilia); err != nil || d.After(issuedAt) {
		return "", errors.New("competencia: a data do serviço, AAAA-MM-DD, até hoje")
	}
	note := nfseNote{issuedAt: issuedAt, competence: competence, takerID: takerID, takerName: a.Str("tomador_nome"),
		takerEmail: a.Str("tomador_email"), service: service, nbs: nbs, description: desc, cents: int64(math.Round(v * 100))}

	n.mu.Lock()
	defer n.mu.Unlock()
	const series = 1
	number := n.nextNumber(env.Cred)
	for try := 0; ; try++ {
		note.number = number
		id, doc := n.buildDPS(is, series, note)
		signed, err := signXML(doc, c)
		if err != nil {
			return "", err
		}
		ans, err := n.gov(ctx, c, "POST", n.sefin()+"/nfse", map[string]string{"dpsXmlGZipB64": gzip64(signed)})
		if err != nil {
			// The answer was lost: the number stays, and asking again finds the nota if it was issued.
			return "", fmt.Errorf("%v. A nota pode ter saído: peça de novo a mesma nota que eu confiro antes de emitir outra", err)
		}
		if ans.status >= 500 {
			return "", fmt.Errorf("o Sistema Nacional está fora agora (HTTP %d); tente daqui a pouco", ans.status)
		}
		key, _ := ans.json["chaveAcesso"].(string)
		if ans.status < 300 && key != "" {
			return n.issued(env, note, id, number, key, gunzip64(fmt.Sprint(ans.json["nfseXmlGZipB64"]))), nil
		}
		errs := ans.errors()
		dup := false
		for _, e := range errs {
			dup = dup || e.code == nfseDuplicate
		}
		if !dup || try >= nfseCollisions {
			return "", errors.New("o governo recusou a nota: " + describe(errs))
		}
		// This number already has a nota: ours from a lost answer, or one
		// issued elsewhere.
		key, x, ours := n.find(ctx, c, id, note)
		if ours {
			return n.issued(env, note, id, number, key, x), nil
		}
		number++
		env.Cred.Extra["proximo_"+n.env()] = strconv.Itoa(number)
		env.Changed = true
	}
}

// find looks up the nota a DPS number produced and says whether it is this
// one (same value and taker).
func (n *NFSe) find(ctx context.Context, c *nfseCert, id string, note nfseNote) (key, x string, ours bool) {
	ans, err := n.gov(ctx, c, "GET", n.sefin()+"/dps/"+id, nil)
	if err != nil || ans.status != 200 {
		return "", "", false
	}
	key, _ = ans.json["chaveAcesso"].(string)
	if key == "" {
		return "", "", false
	}
	nf, err := n.gov(ctx, c, "GET", n.sefin()+"/nfse/"+key, nil)
	if err != nil {
		return "", "", false
	}
	x = gunzip64(fmt.Sprint(nf.json["nfseXmlGZipB64"]))
	same := x != "" && xmlValue(x, "vServ") == cents(note.cents) && (note.takerID == "" || strings.Contains(x, ">"+note.takerID+"<"))
	return key, x, same
}

func (n *NFSe) issued(env *Env, note nfseNote, id string, number int, key, x string) string {
	env.Cred.Extra["proximo_"+n.env()] = strconv.Itoa(number + 1)
	num := xmlValue(x, "nNFSe")
	list := records(env.Cred)
	list = append(list, nfseRecord{DPS: id, Number: number, Key: key, NFSe: num, At: env.now(), Cents: note.cents,
		Taker: strings.TrimSpace(note.takerName), TakerID: note.takerID, What: clip(note.description, 200), Status: "emitida", Env: n.env()})
	saveRecords(env, list)
	out := fmt.Sprintf("NFS-e %s emitida: R$ %s para %s.\nChave de acesso: %s\nPDF: nfse_pdf com essa chave.", orStr(num, "(número a confirmar)"),
		reais(float64(note.cents)/100), orStr(strings.TrimSpace(note.takerName), "tomador não identificado"), key)
	if !n.Production {
		out += "\nAmbiente de teste do governo: esta nota não vale."
	}
	return out
}
