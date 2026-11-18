package connect

import (
	"context"
	"strings"
	"testing"
	"time"
)

func TestBRCode(t *testing.T) {
	// The example of the Banco Central's BR Code manual.
	got, err := BRCode("123e4567-e12b-12d1-a456-426655440000", "Fulano de Tal", "BRASILIA", 0, "", "")
	want := "00020126580014br.gov.bcb.pix0136123e4567-e12b-12d1-a456-426655440000" +
		"5204000053039865802BR5913Fulano de Tal6008BRASILIA62070503***63041D3D"
	if err != nil || got != want {
		t.Fatalf("BRCode:\n got %s\nwant %s (%v)", got, want, err)
	}
	code, err := BRCode("+5516999990000", "João Ção da Silva Sauro Junior", "São Sebastião do Paraíso", 15050, "Reforma do banheiro", "AV123")
	if err != nil {
		t.Fatal(err)
	}
	for _, part := range []string{"0114+5516999990000", "0219Reforma do banheiro", "540615050"[:4] + "150.50", "5925Joao Cao da Silva Sauro J", "6015Sao Sebastiao d", "62090505AV123"} {
		if !strings.Contains(code, part) {
			t.Fatalf("missing %q in %s", part, code)
		}
	}
	if crc16(code[:len(code)-4]) != code[len(code)-4:] {
		t.Fatal("bad checksum")
	}
}

func TestPixKey(t *testing.T) {
	for in, want := range map[string]string{
		"529.982.247-25":     "52998224725",
		"12.345.678/0001-90": "12345678000190",
		"(16) 99999-0000":    "+5516999990000",
		"+55 16 99999-0000":  "+5516999990000",
		"Gui@Exemplo.com":    "gui@exemplo.com",
		"123E4567-E12B-12D1-A456-426655440000": "123e4567-e12b-12d1-a456-426655440000",
	} {
		if got, err := pixKey(in); err != nil || got != want {
			t.Fatalf("%s: %s %v", in, got, err)
		}
	}
	if _, err := pixKey("abc"); err == nil {
		t.Fatal("abc is no key")
	}
}

func TestPixCharges(t *testing.T) {
	p := &Pix{}
	e := env("")
	e.Cred.Extra = map[string]string{"chave": "52998224725", "nome": "Gui Dono", "cidade": "Franca"}
	if acc, err := p.Check(t.Context(), e); err != nil || !strings.Contains(acc, "Gui Dono") {
		t.Fatalf("check: %s %v", acc, err)
	}
	out := call(t, p, e, "pix_cobrar", `{"valor": "1.250,00", "descricao": "Site", "cliente": "Ana Lima"}`)
	if !strings.Contains(out, "R$ 1.250,00") || !strings.Contains(out, "000201") || !strings.Contains(out, "QR code: pix-AV") {
		t.Fatalf("cobrar: %s", out)
	}
	call(t, p, e, "pix_cobrar", `{"valor": 90, "cliente": "Bia Souza"}`)
	call(t, p, e, "pix_cobrar", `{"valor": 90, "cliente": "Caio Reis"}`)
	list := charges(e.Cred)
	if len(list) != 3 || list[0].Cents != 125000 {
		t.Fatalf("charges: %+v", list)
	}
	if _, err := p.Call(t.Context(), e, "pix_conferir", Args{}); err == nil || !strings.Contains(err.Error(), "Pluggy") {
		t.Fatalf("no pluggy: %v", err)
	}
	now := e.now()
	e.Incoming = func(_ context.Context, since time.Time) ([]Incoming, error) {
		return []Incoming{
			{ID: "t1", At: now.Add(time.Hour), Cents: 125000, Payer: "ANA M LIMA"},
			{ID: "t2", At: now.Add(time.Hour), Cents: 9000, Payer: "CAIO REIS"},
			{ID: "t3", At: now.Add(time.Hour), Cents: 4200, Payer: "Fulano"},
		}, nil
	}
	out = call(t, p, e, "pix_conferir", `{}`)
	if strings.Count(out, "Paga:") != 2 || !strings.Contains(out, "Caio Reis") || !strings.Contains(out, "1 pendente") {
		t.Fatalf("conferir: %s", out)
	}
	// The same credit never pays twice.
	if out := call(t, p, e, "pix_conferir", `{}`); strings.Contains(out, "Paga:") {
		t.Fatalf("again: %s", out)
	}
	bia := charges(e.Cred)[1]
	if bia.Status != "pendente" {
		t.Fatalf("bia: %+v", bia)
	}
	if out := call(t, p, e, "pix_marcar", `{"id": "`+bia.ID+`", "status": "paga"}`); !strings.Contains(out, "paga") {
		t.Fatalf("marcar: %s", out)
	}
	if out := call(t, p, e, "pix_cobrancas", `{"status": "pendente"}`); !strings.Contains(out, "Nenhuma") {
		t.Fatalf("pendentes: %s", out)
	}

	// Two pending charges of one amount and a payer who names neither: ask.
	e2 := env("")
	e2.Cred.Extra = map[string]string{"chave": "52998224725", "nome": "Gui", "cidade": "Franca"}
	call(t, p, e2, "pix_cobrar", `{"valor": 50, "cliente": "Ana"}`)
	call(t, p, e2, "pix_cobrar", `{"valor": 50, "cliente": "Bia"}`)
	e2.Incoming = func(context.Context, time.Time) ([]Incoming, error) {
		return []Incoming{{ID: "x", At: now.Add(time.Hour), Cents: 5000, Payer: "Zeca"}}, nil
	}
	if out := call(t, p, e2, "pix_conferir", `{}`); strings.Contains(out, "Paga:") || !strings.Contains(out, "pode ser de") {
		t.Fatalf("tie: %s", out)
	}
}
