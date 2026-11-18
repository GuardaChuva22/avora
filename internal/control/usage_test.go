package control

import (
	"fmt"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/GuardaChuva22/avora/internal/connect"
	"github.com/GuardaChuva22/avora/internal/vault"
)

func TestTwoElevenLabsKeysOnTheSamePlanAreTwoAccounts(t *testing.T) {
	calls := 0
	api := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		users := map[string]string{"sk_one": "u_one1", "sk_two": "u_two2"}
		user, ok := users[r.Header.Get("xi-api-key")]
		if !ok {
			w.WriteHeader(401)
			return
		}
		switch r.URL.Path {
		case "/v1/user":
			fmt.Fprintf(w, `{"user_id":%q,"first_name":"Caio","subscription":{"tier":"free"}}`, user)
		case "/v1/user/subscription":
			calls++
			used := 100
			if user == "u_two2" {
				used = 9000
			}
			fmt.Fprintf(w, `{"tier":"free","character_count":%d,"character_limit":10000}`, used)
		default:
			w.WriteHeader(404)
		}
	}))
	defer api.Close()
	s := chatServer(t, nil)
	s.SetConnectors(connect.NewRegistry(&connect.ElevenLabs{BaseURL: api.URL}), vault.Random())
	for _, key := range []string{"sk_one", "sk_two"} {
		if w := request(t, s, "PUT", "/v1/connectors/elevenlabs", adminTestToken, ConnectRequest{Token: key}); w.Code != 200 {
			t.Fatalf("connect %s: %d %s", key, w.Code, w.Body)
		}
	}
	// The list answers at once; the readings arrive in the background.
	var v ConnectorView
	for i := 0; ; i++ {
		v = decode[[]ConnectorView](t, request(t, s, "GET", "/v1/connectors", adminTestToken, nil))[0]
		if len(v.Usage) == 2 && !v.Usage[v.Connections[0].ID].Pending && !v.Usage[v.Connections[1].ID].Pending {
			break
		}
		if i > 200 {
			t.Fatalf("usage never arrived: %+v", v.Usage)
		}
		time.Sleep(10 * time.Millisecond)
	}
	if len(v.Connections) != 2 || v.Connections[0].Account == v.Connections[1].Account {
		t.Fatalf("connections: %+v", v.Connections)
	}
	one, two := v.Usage[v.Connections[0].ID], v.Usage[v.Connections[1].ID]
	if one.Used+two.Used != 9100 || one.Limit != 10000 || one.Plan != "free" || one.Error != "" {
		t.Fatalf("usage: %+v", v.Usage)
	}
	before := calls
	request(t, s, "GET", "/v1/connectors", adminTestToken, nil)
	if calls != before {
		t.Fatal("usage read again inside the cache window")
	}
}
