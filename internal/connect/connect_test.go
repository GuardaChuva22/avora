package connect

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"
	"time"
)

var loc, _ = time.LoadLocation("America/Sao_Paulo")

type fakeAPI struct {
	mu    sync.Mutex
	calls []string // "METHOD path?query"
	auth  []string
	body  map[string]string // last body per path
	h     func(w http.ResponseWriter, r *http.Request, body []byte) bool
}

func newFake(t *testing.T, h func(w http.ResponseWriter, r *http.Request, body []byte) bool) (*fakeAPI, string) {
	f := &fakeAPI{h: h, body: map[string]string{}}
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		b, _ := io.ReadAll(r.Body)
		f.mu.Lock()
		f.calls = append(f.calls, r.Method+" "+r.URL.RequestURI())
		f.auth = append(f.auth, r.Header.Get("Authorization")+r.Header.Get("xi-api-key"))
		f.body[r.URL.Path] = string(b)
		f.mu.Unlock()
		if !f.h(w, r, b) {
			http.Error(w, `{"err":"not found"}`, 404)
		}
	}))
	t.Cleanup(srv.Close)
	return f, srv.URL
}

type memFiles map[string][]byte

func (m memFiles) Save(name string, data []byte) (string, error) {
	m[name] = data
	return name, nil
}

func env(token string) *Env {
	return &Env{Cred: &Cred{Token: token}, Loc: loc, Files: memFiles{},
		Now: func() time.Time { return time.Date(2026, 11, 12, 13, 0, 0, 0, time.UTC) }}
}

func call(t *testing.T, c Connector, e *Env, tool, args string) string {
	t.Helper()
	a, err := ParseArgs(args)
	if err != nil {
		t.Fatal(err)
	}
	out, err := c.Call(context.Background(), e, tool, a)
	if err != nil {
		t.Fatalf("%s: %v", tool, err)
	}
	return out
}

func TestClickUp(t *testing.T) {
	f, url := newFake(t, func(w http.ResponseWriter, r *http.Request, body []byte) bool {
		switch r.URL.Path {
		case "/user":
			fmt.Fprint(w, `{"user":{"username":"Gui","email":"gui@example.com"}}`)
		case "/team":
			fmt.Fprint(w, `{"teams":[{"id":"9","name":"Casa"}]}`)
		case "/team/9/space":
			fmt.Fprint(w, `{"spaces":[{"id":"s1","name":"Trabalho"}]}`)
		case "/space/s1/folder":
			fmt.Fprint(w, `{"folders":[{"id":"f1","name":"Clientes","lists":[{"id":"l1","name":"Orçamentos"}]}]}`)
		case "/space/s1/list":
			fmt.Fprint(w, `{"lists":[{"id":"l2","name":"Geral"}]}`)
		case "/team/9/task":
			fmt.Fprint(w, `{"tasks":[{"id":"t1","name":"Orçamento Ana","status":{"status":"to do"},"due_date":"1762966800000","list":{"id":"l1","name":"Orçamentos"}},{"id":"t2","name":"Outra","status":{"status":"done"}}],"last_page":true}`)
		case "/list/l1/task":
			fmt.Fprint(w, `{"id":"t3","name":"Responder Ana","status":{"status":"to do"},"url":"https://app.clickup.com/t/t3"}`)
		case "/task/t1/comment":
			fmt.Fprint(w, `{"id":"c1"}`)
		default:
			return false
		}
		return true
	})
	c := &ClickUp{BaseURL: url}
	e := env("pk_secret")
	if acc, err := c.Check(context.Background(), e); err != nil || acc != "Gui · gui@example.com" {
		t.Fatalf("check: %q %v", acc, err)
	}
	if out := call(t, c, e, "clickup_espacos", `{}`); !strings.Contains(out, "Lista Orçamentos (lista_id l1)") || !strings.Contains(out, "Lista Geral (lista_id l2)") {
		t.Fatalf("espacos: %s", out)
	}
	if out := call(t, c, e, "clickup_tarefas", `{"busca":"orçamento"}`); !strings.Contains(out, "[t1] Orçamento Ana · to do · prazo") || strings.Contains(out, "Outra") {
		t.Fatalf("tarefas: %s", out)
	}
	out := call(t, c, e, "clickup_criar_tarefa", `{"lista_id":"l1","nome":"Responder Ana","prazo":"2026-11-13 10:00","prioridade":"alta"}`)
	var body map[string]any
	json.Unmarshal([]byte(f.body["/list/l1/task"]), &body)
	want := float64(time.Date(2026, 11, 13, 10, 0, 0, 0, loc).UnixMilli())
	if !strings.Contains(out, "Tarefa criada") || body["due_date"] != want || body["priority"] != float64(2) || body["due_date_time"] != true {
		t.Fatalf("criar: %s %v", out, body)
	}
	call(t, c, e, "clickup_comentar", `{"id":"t1","comentario":"Enviado."}`)
	for _, a := range f.auth {
		if a != "pk_secret" {
			t.Fatalf("auth header: %q", a)
		}
	}
	tools := map[string]Risk{}
	for _, tl := range c.Tools() {
		tools[tl.Name] = tl.Risk
	}
	if !tools["clickup_comentar"].Locked() || tools["clickup_criar_tarefa"].Locked() {
		t.Fatal("risks")
	}
}

func TestRetriesAndAuth(t *testing.T) {
	n := 0
	_, url := newFake(t, func(w http.ResponseWriter, r *http.Request, _ []byte) bool {
		if r.URL.Path == "/users/@me" {
			n++
			if n == 1 {
				w.Header().Set("Retry-After", "0")
				w.WriteHeader(429)
				return true
			}
			fmt.Fprint(w, `{"username":"avora"}`)
			return true
		}
		if r.URL.Path == "/users/@me/guilds" {
			w.WriteHeader(401)
			return true
		}
		return false
	})
	d := &Discord{BaseURL: url}
	if acc, err := d.Check(context.Background(), env("x")); err != nil || acc != "bot avora" || n != 2 {
		t.Fatalf("retry: %q %v %d", acc, err, n)
	}
	if _, err := d.Call(context.Background(), env("x"), "discord_canais", Args{}); !IsAuth(err) {
		t.Fatalf("401: %v", err)
	}
}

func TestDiscordSend(t *testing.T) {
	f, url := newFake(t, func(w http.ResponseWriter, r *http.Request, _ []byte) bool {
		switch r.URL.Path {
		case "/channels/c1/messages":
			if r.Method == "POST" {
				fmt.Fprint(w, `{"id":"m9"}`)
			} else {
				fmt.Fprint(w, `[{"content":"oi","timestamp":"2026-11-12T12:00:00Z","author":{"username":"ana"}},{"content":"","timestamp":"2026-11-12T11:00:00Z","author":{"username":"bia"}}]`)
			}
			return true
		}
		return false
	})
	d := &Discord{BaseURL: url}
	e := env("bot-token")
	if out := call(t, d, e, "discord_ler", `{"canal_id":"c1"}`); !strings.HasPrefix(out, "12/11 08:00 bia: (sem texto") || !strings.Contains(out, "09:00 ana: oi") {
		t.Fatalf("ler: %s", out)
	}
	call(t, d, e, "discord_enviar", `{"canal_id":"c1","mensagem":"@everyone oi"}`)
	if !strings.Contains(f.body["/channels/c1/messages"], `"allowed_mentions":{"parse":["users"]}`) || f.auth[0] != "Bot bot-token" {
		t.Fatalf("send: %s %v", f.body["/channels/c1/messages"], f.auth)
	}
}

func TestElevenLabsSpeaks(t *testing.T) {
	f, url := newFake(t, func(w http.ResponseWriter, r *http.Request, _ []byte) bool {
		switch r.URL.Path {
		case "/v2/voices":
			fmt.Fprint(w, `{"voices":[{"voice_id":"abcdefghij0123456789","name":"Roberta","category":"premade","labels":{"accent":"brazilian"}}]}`)
		case "/v1/text-to-speech/abcdefghij0123456789":
			w.Write([]byte("ID3fakeaudio"))
		case "/v1/user/subscription":
			fmt.Fprint(w, `{"tier":"starter","character_count":1200,"character_limit":30000}`)
		default:
			return false
		}
		return true
	})
	e := env("sk_x")
	el := &ElevenLabs{BaseURL: url}
	out := call(t, el, e, "elevenlabs_falar", `{"texto":"Bom dia, Gui!","voz":"Roberta","arquivo":"bom-dia"}`)
	if string(e.Files.(memFiles)["audio/bom-dia.mp3"]) != "ID3fakeaudio" || !strings.Contains(out, "voz Roberta") {
		t.Fatalf("falar: %s", out)
	}
	if !strings.Contains(f.body["/v1/text-to-speech/abcdefghij0123456789"], elevenModel) {
		t.Fatal("model")
	}
	if out := call(t, el, e, "elevenlabs_uso", `{}`); !strings.Contains(out, "1200 de 30000") {
		t.Fatalf("uso: %s", out)
	}
}

func TestGoogle(t *testing.T) {
	enc := func(s string) string { return base64.URLEncoding.EncodeToString([]byte(s)) }
	f, url := newFake(t, func(w http.ResponseWriter, r *http.Request, body []byte) bool {
		switch r.URL.Path {
		case "/token":
			if strings.Contains(string(body), "refresh_token=bad") {
				w.WriteHeader(400)
				fmt.Fprint(w, `{"error":"invalid_grant"}`)
				return true
			}
			fmt.Fprint(w, `{"access_token":"fresh","expires_in":3600}`)
		case "/gmail/v1/users/me/profile":
			fmt.Fprint(w, `{"emailAddress":"gui@example.com"}`)
		case "/gmail/v1/users/me/messages":
			fmt.Fprint(w, `{"messages":[{"id":"m1"}]}`)
		case "/gmail/v1/users/me/messages/m1":
			fmt.Fprintf(w, `{"id":"m1","threadId":"th1","labelIds":["UNREAD"],"snippet":"Quanto custa?","internalDate":"1762948800000",
				"payload":{"headers":[{"name":"From","value":"Ana <ana@example.com>"},{"name":"Subject","value":"Orçamento"},{"name":"Message-ID","value":"<x@mail>"}],
				"parts":[{"mimeType":"text/html","body":{"data":"%s"}},{"mimeType":"application/pdf","filename":"planta.pdf","body":{"size":20480}}]}}`,
				enc("<p>Olá, <b>quanto custa</b> a reforma?</p>"))
		case "/gmail/v1/users/me/threads":
			fmt.Fprint(w, `{"threads":[{"id":"t1"},{"id":"t2"},{"id":"t3"},{"id":"t4"}]}`)
		case "/gmail/v1/users/me/threads/t1": // waiting: the last message is Ana's
			fmt.Fprint(w, `{"messages":[{"id":"a1","labelIds":["INBOX"],"internalDate":"1762948800000","snippet":"Quanto custa?",
				"payload":{"headers":[{"name":"From","value":"Ana <ana@example.com>"},{"name":"Subject","value":"Orçamento"}]}}]}`)
		case "/gmail/v1/users/me/threads/t2": // answered: the last one is the owner's
			fmt.Fprint(w, `{"messages":[{"id":"b1","labelIds":["INBOX"],"internalDate":"1762948800000","payload":{"headers":[{"name":"From","value":"Bia <b@x>"}]}},
				{"id":"b2","labelIds":["SENT"],"internalDate":"1762948900000","payload":{"headers":[{"name":"From","value":"gui@example.com"}]}}]}`)
		case "/gmail/v1/users/me/threads/t4": // a store's mailing list
			fmt.Fprint(w, `{"messages":[{"id":"d1","labelIds":["INBOX"],"internalDate":"1762948800000","payload":{"headers":[{"name":"From","value":"AliExpress <ofertas@aliexpress.com>"},{"name":"List-Unsubscribe","value":"<mailto:u@x>"}]}}]}`)
		case "/gmail/v1/users/me/threads/t3": // automated
			fmt.Fprint(w, `{"messages":[{"id":"c1","labelIds":["INBOX"],"internalDate":"1762948800000","payload":{"headers":[{"name":"From","value":"Loja <no-reply@loja.com>"}]}}]}`)
		case "/gmail/v1/users/me/drafts":
			fmt.Fprint(w, `{"id":"d1"}`)
		case "/drive/v3/files/doc1":
			fmt.Fprint(w, `{"id":"doc1","name":"Proposta","mimeType":"application/vnd.google-apps.document","modifiedTime":"2026-11-10T10:00:00Z"}`)
		case "/drive/v3/files/doc1/export":
			fmt.Fprint(w, "Proposta de reforma: R$ 12.000")
		case "/calendar/v3/calendars/primary/events":
			fmt.Fprint(w, `{"id":"e1","htmlLink":"https://calendar/e1"}`)
		default:
			return false
		}
		return true
	})
	g := &Google{ClientID: "id", ClientSecret: "secret", API: url}
	e := env("")
	e.Cred.Refresh = "good"
	if acc, err := g.Check(context.Background(), e); err != nil || acc != "gui@example.com" || !e.Changed || e.Cred.Token != "fresh" {
		t.Fatalf("check/refresh: %q %v %+v", acc, err, e.Cred)
	}
	if out := call(t, g, e, "gmail_buscar", `{"busca":"is:unread"}`); !strings.Contains(out, "[m1]") || !strings.Contains(out, "não lido") || !strings.Contains(out, "Orçamento") {
		t.Fatalf("buscar: %s", out)
	}
	if out := call(t, g, e, "gmail_sem_resposta", `{}`); !strings.Contains(out, "[a1]") || !strings.Contains(out, "dias sem resposta") ||
		strings.Contains(out, "[b2]") || strings.Contains(out, "[b1]") || strings.Contains(out, "[c1]") || strings.Contains(out, "[d1]") {
		t.Fatalf("sem resposta: %s", out)
	}
	if out := call(t, g, e, "gmail_ler", `{"id":"m1"}`); !strings.Contains(out, "quanto custa a reforma") || !strings.Contains(out, "planta.pdf") {
		t.Fatalf("ler: %s", out)
	}
	call(t, g, e, "gmail_rascunho", `{"responder_a":"m1","corpo":"Olá, Ana! Fica R$ 12.000."}`)
	var d struct {
		Message struct {
			Raw      string `json:"raw"`
			ThreadID string `json:"threadId"`
		} `json:"message"`
	}
	json.Unmarshal([]byte(f.body["/gmail/v1/users/me/drafts"]), &d)
	raw, _ := base64.URLEncoding.DecodeString(d.Message.Raw)
	if d.Message.ThreadID != "th1" || !strings.Contains(string(raw), "To: Ana <ana@example.com>") || !strings.Contains(string(raw), "In-Reply-To: <x@mail>") ||
		!strings.Contains(string(raw), "Subject: =?utf-8?q?Re:_Or=C3=A7amento?=") {
		t.Fatalf("draft: %s", raw)
	}
	if out := call(t, g, e, "drive_ler", `{"id":"doc1"}`); !strings.Contains(out, "R$ 12.000") {
		t.Fatalf("drive: %s", out)
	}
	_, tl, _ := NewRegistry(g).Tool("agenda_criar")
	if tl.RiskFor(Args{"convidados": []any{"ana@example.com"}}) != Send || tl.RiskFor(Args{}) != Write {
		t.Fatal("agenda risk")
	}
	call(t, g, e, "agenda_criar", `{"titulo":"Visita Ana","inicio":"2026-11-14 09:00","convidados":["ana@example.com"]}`)
	if !strings.Contains(strings.Join(f.calls, "\n"), "events?sendUpdates=all") || !strings.Contains(f.body["/calendar/v3/calendars/primary/events"], `"dateTime":"2026-11-14T10:00:00-03:00"`) {
		t.Fatalf("event: %v %s", f.calls, f.body["/calendar/v3/calendars/primary/events"])
	}
	bad := env("")
	bad.Cred.Refresh = "bad"
	if _, err := g.Check(context.Background(), bad); !IsAuth(err) {
		t.Fatalf("invalid_grant: %v", err)
	}
	if u := g.AuthURL("http://127.0.0.1:5000/", "st", "verifier"); !strings.Contains(u, "code_challenge="+PKCE("verifier")) || !strings.Contains(u, "access_type=offline") {
		t.Fatalf("auth url: %s", u)
	}
}

func TestElevenLabsNamesAccountsByUser(t *testing.T) {
	_, url := newFake(t, func(w http.ResponseWriter, r *http.Request, _ []byte) bool {
		key := r.Header.Get("xi-api-key")
		switch {
		case r.URL.Path == "/v1/user" && key == "sk_a":
			fmt.Fprint(w, `{"user_id":"u_aaaa1111","first_name":"Caio","subscription":{"tier":"free"}}`)
		case r.URL.Path == "/v1/user" && key == "sk_b":
			fmt.Fprint(w, `{"user_id":"u_bbbb2222","first_name":"Caio","subscription":{"tier":"free"}}`)
		case r.URL.Path == "/v1/user":
			w.WriteHeader(401)
			fmt.Fprint(w, `{"detail":{"status":"missing_permissions"}}`)
		case r.URL.Path == "/v2/voices":
			fmt.Fprint(w, `{"voices":[]}`)
		case r.URL.Path == "/v1/user/subscription":
			fmt.Fprint(w, `{"tier":"creator","character_count":1200,"character_limit":100000,"next_character_count_reset_unix":1795000000}`)
		default:
			return false
		}
		return true
	})
	el := &ElevenLabs{BaseURL: url}
	a, _ := el.Check(t.Context(), env("sk_a"))
	b, _ := el.Check(t.Context(), env("sk_b"))
	c, err := el.Check(t.Context(), env("sk_onlyspeech9z"))
	if a != "Caio · …1111" || b != "Caio · …2222" || c != "chave …ch9z" || err != nil {
		t.Fatalf("accounts: %q %q %q %v", a, b, c, err)
	}
	u, err := el.Usage(t.Context(), env("sk_a"))
	if err != nil || u.Plan != "creator" || u.Used != 1200 || u.Limit != 100000 || u.ResetsAt != 1795000000 {
		t.Fatalf("usage: %+v %v", u, err)
	}
}
