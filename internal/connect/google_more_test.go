package connect

import (
	"fmt"
	"net/http"
	"strings"
	"testing"
	"time"
)

func TestGoogleMore(t *testing.T) {
	f, url := newFake(t, func(w http.ResponseWriter, r *http.Request, body []byte) bool {
		p := r.URL.Path
		switch {
		case p == "/v4/spreadsheets/s1" && r.Method == "GET":
			fmt.Fprint(w, `{"properties":{"title":"Vendas 2026"},"sheets":[{"properties":{"title":"Nov","gridProperties":{"rowCount":40,"columnCount":5}}}]}`)
		case p == "/v4/spreadsheets/s1/values/'Nov'!A1:Z200":
			fmt.Fprint(w, `{"range":"Nov!A1:C3","values":[["Cliente","Valor"],["Ana",150],["Bia",90]]}`)
		case p == "/v4/spreadsheets/s1/values/Nov:append":
			if !strings.Contains(string(body), `["Caio",200]`) || r.URL.Query().Get("valueInputOption") != "USER_ENTERED" {
				t.Errorf("append body: %s", body)
			}
			fmt.Fprint(w, `{"updates":{"updatedRange":"Nov!A4:B4","updatedCells":2}}`)
		case p == "/v4/spreadsheets" && r.Method == "POST":
			fmt.Fprint(w, `{"spreadsheetId":"s2","spreadsheetUrl":"https://docs.google.com/spreadsheets/d/s2","sheets":[{"properties":{"title":"Página1"}}]}`)
		case p == "/v4/spreadsheets/s2/values/'Página1'!A1":
			fmt.Fprint(w, `{"updatedCells":2}`)
		case p == "/tasks/v1/users/@me/lists":
			fmt.Fprint(w, `{"items":[{"id":"L1","title":"Minhas tarefas"},{"id":"L2","title":"Casa"}]}`)
		case p == "/tasks/v1/lists/L2/tasks" && r.Method == "POST":
			if !strings.Contains(string(body), `"due":"2026-11-20T00:00:00Z"`) {
				t.Errorf("task body: %s", body)
			}
			fmt.Fprint(w, `{"id":"T9"}`)
		case p == "/tasks/v1/lists/L1/tasks":
			fmt.Fprint(w, `{"items":[{"id":"T1","title":"Pagar DAS","due":"2026-11-20T00:00:00.000Z","status":"needsAction"}]}`)
		case p == "/v1/people:searchContacts":
			if r.URL.Query().Get("query") == "" {
				fmt.Fprint(w, `{}`)
				return true
			}
			fmt.Fprint(w, `{"results":[{"person":{"names":[{"displayName":"Ana Lima"}],"phoneNumbers":[{"value":"(16) 99999-0000"}],"birthdays":[{"date":{"month":3,"day":9}}]}}]}`)
		case p == "/youtube/v3/channels":
			fmt.Fprint(w, `{"items":[{"snippet":{"title":"Gui"},"statistics":{"subscriberCount":"1200","viewCount":"50000","videoCount":"30"},"contentDetails":{"relatedPlaylists":{"uploads":"UU1"}}}]}`)
		case p == "/youtube/v3/playlistItems":
			fmt.Fprint(w, `{"items":[{"contentDetails":{"videoId":"v1"}}]}`)
		case p == "/youtube/v3/videos":
			fmt.Fprint(w, `{"items":[{"id":"v1","snippet":{"title":"Como emitir NFS-e","publishedAt":"2026-11-01T10:00:00Z"},"contentDetails":{"duration":"PT12M5S"},"statistics":{"viewCount":"800","likeCount":"40","commentCount":"6"}}]}`)
		case p == "/youtube/v3/search":
			w.WriteHeader(403)
			fmt.Fprint(w, `{"error":{"code":403,"message":"YouTube Data API v3 has not been used in project 123 before or it is disabled.","status":"PERMISSION_DENIED","details":[{"reason":"SERVICE_DISABLED"}]}}`)
		default:
			return false
		}
		return true
	})
	g := &Google{ClientID: "id", ClientSecret: "secret", API: url}
	e := env("tok")
	e.Cred.Expiry = e.now().Add(time.Hour)

	out := call(t, g, e, "planilhas_ler", `{"planilha":"https://docs.google.com/spreadsheets/d/s1/edit#gid=0"}`)
	if !strings.Contains(out, "Abas: Nov (40 linhas)") || !strings.Contains(out, "2: Ana | 150") {
		t.Fatalf("ler: %s", out)
	}
	if out := call(t, g, e, "planilhas_escrever", `{"planilha":"s1","intervalo":"Nov","linhas":[["Caio",200]],"adicionar":true}`); !strings.Contains(out, "2 células em Nov!A4:B4") {
		t.Fatalf("escrever: %s", out)
	}
	if out := call(t, g, e, "planilhas_criar", `{"titulo":"Clientes","linhas":[["Nome","Fone"]]}`); !strings.Contains(out, "id s2") || !strings.Contains(out, "Com 1 linhas") {
		t.Fatalf("criar: %s", out)
	}
	if out := call(t, g, e, "tarefas_criar", `{"titulo":"Comprar lâmpada","lista":"casa","prazo":"2026-11-20"}`); !strings.Contains(out, "«Casa»") {
		t.Fatalf("tarefa: %s", out)
	}
	if out := call(t, g, e, "tarefas_listar", `{"lista":"Minhas tarefas"}`); !strings.Contains(out, "Pagar DAS · prazo 20/11 [id T1]") {
		t.Fatalf("listar: %s", out)
	}
	if out := call(t, g, e, "contatos_buscar", `{"busca":"ana"}`); !strings.Contains(out, "Ana Lima · (16) 99999-0000 · aniversário 09/03") {
		t.Fatalf("contatos: %s", out)
	}
	if out := call(t, g, e, "youtube_meu_canal", `{}`); !strings.Contains(out, "1200 inscritos") || !strings.Contains(out, "Como emitir NFS-e · 01/11/2026 · 12:05 · 800 views, 40 curtidas") {
		t.Fatalf("canal: %s", out)
	}
	a, _ := ParseArgs(`{"busca":"nfse"}`)
	if _, err := g.Call(t.Context(), e, "youtube_buscar", a); err == nil || !strings.Contains(err.Error(), "YouTube Data API v3 está desligada") {
		t.Fatalf("disabled API: %v", err)
	}
	if !strings.Contains(g.AuthURL("http://127.0.0.1/cb", "st", "ver"), "spreadsheets") {
		t.Fatal("consent does not ask for Sheets")
	}
	_ = f
}
