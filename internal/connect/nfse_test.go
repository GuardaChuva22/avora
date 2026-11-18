package connect

import (
	"context"
	"crypto"
	"crypto/rand"
	"crypto/rsa"
	"crypto/sha256"
	"crypto/x509"
	"crypto/x509/pkix"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"math/big"
	"net/http"
	"net/http/httptest"
	"regexp"
	"strings"
	"testing"
	"time"

	pkcs12 "software.sslmate.com/src/go-pkcs12"
)

func testPFX(t *testing.T) (string, *rsa.PrivateKey) {
	key, _ := rsa.GenerateKey(rand.Reader, 2048)
	tpl := &x509.Certificate{SerialNumber: big.NewInt(7), Subject: pkix.Name{CommonName: "MINHA EMPRESA MEI:12345678000199"},
		NotBefore: time.Now().Add(-time.Hour), NotAfter: time.Now().Add(365 * 24 * time.Hour)}
	der, err := x509.CreateCertificate(rand.Reader, tpl, tpl, &key.PublicKey, key)
	if err != nil {
		t.Fatal(err)
	}
	cert, _ := x509.ParseCertificate(der)
	pfx, err := pkcs12.Modern.Encode(key, cert, nil, "s3nha")
	if err != nil {
		t.Fatal(err)
	}
	return base64.StdEncoding.EncodeToString(pfx), key
}

// verifySigned checks the XMLDSig the way the government does.
func verifySigned(t *testing.T, doc string, pub *rsa.PublicKey) {
	t.Helper()
	i := strings.Index(doc, `<Signature xmlns=`)
	if i < 0 {
		t.Fatalf("no signature: %s", doc)
	}
	sig := doc[i : strings.Index(doc, "</Signature>")+len("</Signature>")]
	unsigned := doc[:i] + doc[i+len(sig):]
	m := signedPart.FindStringSubmatch(unsigned)
	canonical := strings.Replace(m[0], "<"+m[1]+" Id=", "<"+m[1]+` xmlns="`+nfseNS+`" Id=`, 1)
	digest := sha256.Sum256([]byte(canonical))
	if xmlValue(sig, "DigestValue") != base64.StdEncoding.EncodeToString(digest[:]) {
		t.Fatal("digest does not match")
	}
	info := regexp.MustCompile(`<SignedInfo>([\s\S]*)</SignedInfo>`).FindStringSubmatch(sig)[1]
	sum := sha256.Sum256([]byte(`<SignedInfo xmlns="` + dsigNS + `">` + info + `</SignedInfo>`))
	value, _ := base64.StdEncoding.DecodeString(xmlValue(sig, "SignatureValue"))
	if err := rsa.VerifyPKCS1v15(pub, crypto.SHA256, sum[:], value); err != nil {
		t.Fatalf("signature: %v", err)
	}
	if !strings.Contains(sig, `URI="#`+m[2]+`"`) {
		t.Fatal("reference")
	}
}

func TestNFSe(t *testing.T) {
	pfx, key := testPFX(t)
	const takenKey = "35503082123456780001990000000000000011000000001234"
	const newKey = "35503082123456780001990000000000000021000000005678"
	var dps []string
	var cancelled string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		answer := func(status int, v any) {
			w.Header().Set("Content-Type", "application/json")
			w.WriteHeader(status)
			_ = json.NewEncoder(w).Encode(v)
		}
		nfse := func(n, value string) string {
			return gzip64(`<NFSe><infNFSe><nNFSe>` + n + `</nNFSe><dhProc>2026-11-16T10:00:00-03:00</dhProc><DPS><infDPS><toma><CPF>52998224725</CPF><xNome>Cliente</xNome></toma>` +
				`<cServ><cTribNac>010701</cTribNac><xDescServ>Site</xDescServ></cServ><vServ>` + value + `</vServ></infDPS></DPS></infNFSe></NFSe>`)
		}
		switch {
		case r.Method == "POST" && r.URL.Path == "/sefin/nfse":
			var body map[string]string
			_ = json.NewDecoder(r.Body).Decode(&body)
			doc := gunzip64(body["dpsXmlGZipB64"])
			verifySigned(t, doc, &key.PublicKey)
			dps = append(dps, doc)
			if xmlValue(doc, "nDPS") == "1" { // number 1 was used elsewhere
				answer(400, map[string]any{"erros": []map[string]string{{"Codigo": "E0014", "Descricao": "DPS já existe"}}})
				return
			}
			answer(201, map[string]any{"chaveAcesso": newKey, "nfseXmlGZipB64": nfse("2", xmlValue(doc, "vServ"))})
		case r.URL.Path == "/sefin/dps/"+dpsID(nfseIssuer{cnpj: "12345678000199", city: "3550308"}, 1, 1):
			answer(200, map[string]any{"chaveAcesso": takenKey})
		case strings.HasPrefix(r.URL.Path, "/sefin/dps/"):
			answer(404, map[string]any{"erros": []map[string]string{{"Codigo": "E404", "Descricao": "não encontrada"}}})
		case r.URL.Path == "/sefin/nfse/"+takenKey:
			answer(200, map[string]any{"nfseXmlGZipB64": nfse("1", "99.00")})
		case r.URL.Path == "/sefin/nfse/"+newKey:
			answer(200, map[string]any{"nfseXmlGZipB64": nfse("2", "350.50")})
		case r.URL.Path == "/sefin/nfse/"+newKey+"/eventos":
			var body map[string]string
			_ = json.NewDecoder(r.Body).Decode(&body)
			cancelled = gunzip64(body["pedidoRegistroEventoXmlGZipB64"])
			verifySigned(t, cancelled, &key.PublicKey)
			answer(201, map[string]any{"eventoXmlGZipB64": ""})
		case r.URL.Path == "/adn/danfse/"+newKey:
			w.Header().Set("Content-Type", "application/pdf")
			fmt.Fprint(w, "%PDF-1.4 fake")
		default:
			http.NotFound(w, r)
		}
	}))
	defer srv.Close()

	n := &NFSe{Sefin: srv.URL + "/sefin", ADN: srv.URL + "/adn", HTTP: srv.Client()}
	ctx := context.Background()
	files := memFiles{}
	at := time.Date(2026, 11, 16, 13, 0, 0, 0, time.UTC)
	e := &Env{Cred: &Cred{Extra: map[string]string{"certificado": pfx, "senha": "s3nha", "municipio": "3550308", "servico": "01.07.01"}},
		Files: files, Now: func() time.Time { return at }}
	who, err := n.Check(ctx, e)
	if err != nil || !strings.Contains(who, "MINHA EMPRESA MEI") || !strings.Contains(who, "12345678000199") {
		t.Fatalf("check: %q %v", who, err)
	}
	bad := &Env{Cred: &Cred{Extra: map[string]string{"certificado": pfx, "senha": "errada", "municipio": "3550308", "servico": "010701"}}}
	if _, err := n.Check(ctx, bad); !IsAuth(err) {
		t.Fatalf("wrong password: %v", err)
	}

	// Number 1 is someone else's nota: it moves on to 2.
	out, err := n.Call(ctx, e, "nfse_emitir", Args{"valor": 350.5, "descricao": "Criação de site <institucional> & SEO",
		"tomador_documento": "529.982.247-25", "tomador_nome": "Cliente", "tomador_email": "c@example.com"})
	if err != nil || !strings.Contains(out, "NFS-e 2 emitida") || !strings.Contains(out, newKey) || !strings.Contains(out, "não vale") {
		t.Fatalf("issue: %q %v", out, err)
	}
	if len(dps) != 2 || e.Cred.Extra["proximo_teste"] != "3" || !e.Changed {
		t.Fatalf("numbers: %d %q", len(dps), e.Cred.Extra["proximo_teste"])
	}
	d := dps[1]
	for _, want := range []string{`<infDPS Id="DPS355030821234567800019900001000000000000002">`, "<tpAmb>2</tpAmb>", "<dhEmi>2026-11-16T09:59:00-03:00</dhEmi>",
		"<serie>00001</serie>", "<nDPS>2</nDPS>", "<dCompet>2026-11-16</dCompet>", "<finNFSe>0</finNFSe>", "<cLocEmi>3550308</cLocEmi>",
		"<prest><CNPJ>12345678000199</CNPJ><regTrib><opSimpNac>2</opSimpNac><regEspTrib>0</regEspTrib></regTrib></prest>",
		"<toma><CPF>52998224725</CPF><xNome>Cliente</xNome><email>c@example.com</email></toma>",
		"<cTribNac>010701</cTribNac><xDescServ>Criação de site &lt;institucional&gt; &amp; SEO</xDescServ>",
		"<vServ>350.50</vServ>", "<tribMun><tribISSQN>1</tribISSQN><tpRetISSQN>1</tpRetISSQN></tribMun><totTrib><indTotTrib>0</indTotTrib></totTrib>"} {
		if !strings.Contains(d, want) {
			t.Fatalf("DPS without %s:\n%s", want, d)
		}
	}
	if len(dpsID(nfseIssuer{cnpj: "12345678000199", city: "3550308"}, 1, 2)) != 45 {
		t.Fatal("the DPS id has 45 characters")
	}

	if out, _ := n.Call(ctx, e, "nfse_notas", Args{}); !strings.Contains(out, "NFS-e 2") || !strings.Contains(out, "350,50") {
		t.Fatalf("list: %q", out)
	}
	if out, err := n.Call(ctx, e, "nfse_consultar", Args{"chave": newKey}); err != nil || !strings.Contains(out, "Tomador: Cliente") {
		t.Fatalf("query: %q %v", out, err)
	}
	if out, err := n.Call(ctx, e, "nfse_pdf", Args{"chave": newKey}); err != nil || files["nfse/NFSe-2.pdf"] == nil {
		t.Fatalf("pdf: %q %v", out, err)
	}
	if _, err := n.Call(ctx, e, "nfse_cancelar", Args{"chave": newKey, "motivo": "2", "justificativa": "não prestado"}); err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(cancelled, `<infPedReg Id="PRE`+newKey+`101101">`) || !strings.Contains(cancelled, "<xMotivo>não prestado...</xMotivo>") {
		t.Fatalf("cancel: %s", cancelled)
	}
	if !strings.Contains(e.Cred.Extra["notas"], `"status":"cancelada"`) {
		t.Fatal("the log marks it cancelled")
	}
	if !n.Tools()[0].Risk.Locked() || !n.Tools()[4].Risk.Locked() {
		t.Fatal("issuing and cancelling need approval")
	}
}
