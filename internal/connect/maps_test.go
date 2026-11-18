package connect

import (
	"context"
	"io"
	"net/http"
	"strings"
	"testing"
)

func mapsFakes(t *testing.T) (*fakeAPI, string, *fakeAPI, string) {
	tt, ttURL := newFake(t, func(w http.ResponseWriter, r *http.Request, body []byte) bool {
		if r.URL.Query().Get("key") != "TT" {
			w.WriteHeader(403)
			io.WriteString(w, `{"detailedError":{"code":"Forbidden"}}`)
			return true
		}
		p := r.URL.Path
		switch {
		case strings.HasPrefix(p, "/search/2/search/"):
			if strings.Contains(p, "Major Claudiano") {
				io.WriteString(w, `{"results":[{"type":"Point Address","address":{"freeformAddress":"Rua Major Claudiano 1000, Franca"},"position":{"lat":-20.53,"lon":-47.4}}]}`)
				return true
			}
			io.WriteString(w, `{"results":[{"type":"POI","poi":{"name":"Shopping Franca","phone":"+55 16 3333-0000"},"address":{"freeformAddress":"Av. Brasil 10, Franca"},"position":{"lat":-20.5,"lon":-47.39}}]}`)
			return true
		case strings.HasPrefix(p, "/routing/1/calculateRoute/"):
			if strings.Contains(p, "-20.100000") {
				io.WriteString(w, `{"routes":[{"summary":{"lengthInMeters":3000,"travelTimeInSeconds":600}}]}`)
				return true
			}
			io.WriteString(w, `{"routes":[{"summary":{"lengthInMeters":98400,"travelTimeInSeconds":5100,"trafficDelayInSeconds":600},
				"sections":[{"sectionType":"TOLL"}],"guidance":{"instructions":[{"message":"Siga para o norte","routeOffsetInMeters":0}]}}]}`)
			return true
		case strings.HasPrefix(p, "/search/2/reverseGeocode/"):
			io.WriteString(w, `{"addresses":[{"address":{"freeformAddress":"Rua X 1, Franca"}}]}`)
			return true
		}
		return false
	})
	sp, spURL := newFake(t, func(w http.ResponseWriter, r *http.Request, body []byte) bool {
		q := r.URL.Query()
		if q.Get("api_key") != "SP" {
			w.WriteHeader(401)
			io.WriteString(w, `{"error":"Invalid API key."}`)
			return true
		}
		switch {
		case r.URL.Path == "/account.json":
			io.WriteString(w, `{"plan_name":"Free Plan","searches_per_month":250,"this_month_usage":10,"total_searches_left":240}`)
		case q.Get("engine") == "google_maps" && q.Get("place_id") != "":
			io.WriteString(w, `{"place_results":{"title":"Pizzaria Bella","place_id":"ChIJpizza","rating":4.6,"reviews":812,"address":"Av. Brasil, 10, Franca",
				"hours":[{"segunda-feira":"18:00–23:00"}],"gps_coordinates":{"latitude":-20.1,"longitude":-47.1},
				"user_reviews":{"most_relevant":[{"username":"Ana","rating":5,"description":"Ótima massa","date":"há 2 semanas"}]}}}`)
		case q.Get("engine") == "google_maps":
			io.WriteString(w, `{"local_results":[
				{"title":"Pizzaria Bella","place_id":"ChIJpizza","rating":4.6,"reviews":812,"price":"R$ 50–100","type":"Pizzaria","address":"Av. Brasil, 10","open_state":"Aberto ⋅ Fecha às 23:00","phone":"(16) 3333-0000"},
				{"title":"Pizza Ruim","place_id":"ChIJruim","rating":3.1,"reviews":20,"open_state":"Aberto ⋅ Fecha às 22:00"},
				{"title":"Pizza Fechada","place_id":"ChIJfech","rating":4.9,"reviews":50,"open_state":"Fechado ⋅ Abre às 18:00"}]}`)
		case q.Get("engine") == "google_maps_directions":
			io.WriteString(w, `{"directions":[{"via":"Linha 3","formatted_duration":"42 min","formatted_distance":"12 km",
				"trips":[{"title":"Ônibus 301","formatted_duration":"30 min"}]}]}`)
		default:
			return false
		}
		return true
	})
	return tt, ttURL, sp, spURL
}

func TestMaps(t *testing.T) {
	tt, ttURL, sp, spURL := mapsFakes(t)
	m := &Maps{TomTomURL: ttURL, SerpURL: spURL}
	e := env("")
	e.Cred.Extra = map[string]string{"serpapi": "SP", "tomtom": "TT", "base": "Rua Major Claudiano, Franca"}
	acc, err := m.Check(t.Context(), e)
	if err != nil || !strings.Contains(acc, "240 buscas") || !strings.Contains(acc, "TomTom") || !strings.Contains(acc, "Major Claudiano") || e.Cred.Extra["base_pos"] != "-20.530000,-47.400000" {
		t.Fatalf("check: %q %v %+v", acc, err, e.Cred.Extra)
	}
	if u, err := m.Usage(t.Context(), e); err != nil || u.Used != 10 || u.Limit != 250 {
		t.Fatalf("usage: %+v %v", u, err)
	}

	out := call(t, m, e, "maps_buscar_lugares", `{"busca": "pizzaria", "aberto_agora": true, "nota_minima": 4}`)
	for _, want := range []string{"Pizzaria Bella", "nota 4,6 (812 avaliações)", "Aberto", "R$ 50–100", "id:ChIJpizza", "perto de Rua Major"} {
		if !strings.Contains(out, want) {
			t.Fatalf("missing %q: %s", want, out)
		}
	}
	if strings.Contains(out, "Pizza Ruim") || strings.Contains(out, "Pizza Fechada") {
		t.Fatalf("filters: %s", out)
	}
	if !strings.Contains(sp.calls[len(sp.calls)-1], "ll=%40-20.530000%2C-47.400000%2C14z") {
		t.Fatalf("near base: %v", sp.calls)
	}

	out = call(t, m, e, "maps_lugar", `{"id": "id:ChIJpizza"}`)
	if !strings.Contains(out, "segunda-feira: 18:00–23:00") || !strings.Contains(out, "5★ Ana") {
		t.Fatalf("lugar: %s", out)
	}

	out = call(t, m, e, "maps_rota", `{"destino": "Shopping Franca", "evitar": ["pedagios"], "passos": true}`)
	for _, want := range []string{"1 h 25 min (10 min de atraso pelo trânsito)", "98,4 km", "tem pedágio", "Siga para o norte", "Abrir no Google Maps"} {
		if !strings.Contains(out, want) {
			t.Fatalf("missing %q: %s", want, out)
		}
	}
	last := tt.calls[len(tt.calls)-1]
	if !strings.Contains(last, "avoid=tollRoads") || !strings.Contains(last, "-20.530000,-47.400000:-20.500000,-47.390000") || !strings.Contains(last, "instructionsType=text") {
		t.Fatalf("route call: %s", last)
	}

	out = call(t, m, e, "maps_rota", `{"destino": "Centro", "modo": "transporte"}`)
	if !strings.Contains(out, "Ônibus 301") || !strings.Contains(out, "42 min") || !strings.Contains(sp.calls[len(sp.calls)-1], "travel_mode=3") {
		t.Fatalf("transit: %s", out)
	}

	out = call(t, m, e, "maps_distancias", `{"destinos": ["Shopping Franca", "id:ChIJpizza"]}`)
	if strings.Index(out, "id:ChIJpizza: 10 min") < 0 || strings.Index(out, "id:ChIJpizza") > strings.Index(out, "Shopping Franca: 1 h 25 min") {
		t.Fatalf("matrix: %s", out)
	}
	if out := call(t, m, e, "maps_endereco", `{"endereco": "-20.5,-47.4"}`); !strings.Contains(out, "Rua X 1") {
		t.Fatalf("reverse: %s", out)
	}
}

func TestMapsOneKey(t *testing.T) {
	_, ttURL, _, spURL := mapsFakes(t)
	m := &Maps{TomTomURL: ttURL, SerpURL: spURL}

	// TomTom only: places without ratings, transit says what to add.
	e := env("")
	e.Cred.Extra = map[string]string{"tomtom": "TT"}
	if out := call(t, m, e, "maps_buscar_lugares", `{"busca": "shopping"}`); !strings.Contains(out, "Shopping Franca") || !strings.Contains(out, "sem notas") {
		t.Fatalf("tomtom places: %s", out)
	}
	if _, err := m.Call(context.Background(), e, "maps_rota", Args{"origem": "A", "destino": "B", "modo": "transporte"}); err == nil || !strings.Contains(err.Error(), "SerpApi") {
		t.Fatalf("transit: %v", err)
	}
	if _, err := m.Call(context.Background(), e, "maps_rota", Args{"destino": "B"}); err == nil || !strings.Contains(err.Error(), "endereço base") {
		t.Fatalf("no base: %v", err)
	}

	// SerpApi only: routes go through Google Maps, distances need TomTom.
	e = env("")
	e.Cred.Extra = map[string]string{"serpapi": "SP"}
	if out := call(t, m, e, "maps_rota", `{"origem": "A", "destino": "B"}`); !strings.Contains(out, "42 min") {
		t.Fatalf("serp route: %s", out)
	}
	if _, err := m.Call(context.Background(), e, "maps_distancias", Args{"destinos": []any{"B"}}); err == nil || !strings.Contains(err.Error(), "TomTom") {
		t.Fatalf("matrix: %v", err)
	}

	// Wrong keys are reconnect errors; none at all is refused.
	e = env("")
	e.Cred.Extra = map[string]string{"serpapi": "x"}
	if _, err := m.Check(t.Context(), e); !IsAuth(err) {
		t.Fatalf("bad serp: %v", err)
	}
	e.Cred.Extra = map[string]string{"tomtom": "x"}
	if _, err := m.Check(t.Context(), e); !IsAuth(err) {
		t.Fatalf("bad tomtom: %v", err)
	}
	e.Cred.Extra = map[string]string{}
	if _, err := m.Check(t.Context(), e); !IsAuth(err) {
		t.Fatalf("no key: %v", err)
	}
}
