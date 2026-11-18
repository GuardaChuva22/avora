package connect

import (
	"context"
	"errors"
	"fmt"
	"net/url"
	"regexp"
	"slices"
	"sort"
	"strconv"
	"strings"
	"sync"
	"time"
)

// Maps is places, routes and distances with no card: places, ratings,
// reviews and public transport come from Google Maps through SerpApi;
// routes with live traffic, distances and addresses from TomTom. Each part
// works with its own key, and one key alone still gives what it can.
type Maps struct {
	// TomTomURL and SerpURL replace the hosts in tests.
	TomTomURL, SerpURL string
}

func (m *Maps) tomtom() string {
	if m.TomTomURL != "" {
		return strings.TrimRight(m.TomTomURL, "/")
	}
	return "https://api.tomtom.com"
}

func (m *Maps) serp() string {
	if m.SerpURL != "" {
		return strings.TrimRight(m.SerpURL, "/")
	}
	return "https://serpapi.com"
}

func (m *Maps) Info() Info {
	return Info{Kind: "maps", Name: "Mapas", Ready: true, Category: "Com chave ou token", Auth: "token", Apps: []string{"Google Maps (SerpApi)", "TomTom"},
		Description: "Google Maps sem cartão: lugares, rotas e distâncias. Os agentes acham restaurantes, lojas e serviços com a nota e as avaliações do Google, traçam rotas com o trânsito de agora (carro, a pé, bicicleta, moto ou transporte público) e comparam quanto tempo leva até cada lugar.",
		Help: "Duas contas grátis, só com e-mail. SerpApi (serpapi.com → Sign up → Your Account → API Key): lugares com nota, avaliações e transporte público do Google Maps; o plano grátis tem cerca de 250 buscas por mês. " +
			"TomTom (developer.tomtom.com → Register → Dashboard → Keys): rotas com trânsito, distâncias e endereços; o plano grátis tem cerca de 2.500 chamadas por dia. Pode colar só uma das chaves. " +
			"O endereço base é opcional: é de onde os agentes partem quando você diz \"perto de mim\" ou não diz a origem.",
		HelpURL: "https://serpapi.com/manage-api-key",
		Fields: []Field{
			{Key: "serpapi", Label: "Chave da SerpApi (lugares e notas)", Secret: true, Optional: true},
			{Key: "tomtom", Label: "Chave do TomTom (rotas e trânsito)", Secret: true, Optional: true},
			{Key: "base", Label: "Seu endereço ou bairro e cidade (para \"perto de mim\")", Optional: true},
		}}
}

func (m *Maps) Tools() []Tool {
	return []Tool{
		{Name: "maps_buscar_lugares", Risk: Read,
			Description: "Busca lugares no Google Maps: restaurantes, lojas, serviços, pontos turísticos... Devolve nome, endereço, nota e número de avaliações, se está aberto, faixa de preço, telefone e o id de cada lugar (use id:... em maps_lugar ou como origem/destino de rotas). Sem perto_de, busca perto do endereço base do dono. Cada busca gasta 1 das buscas do mês da SerpApi: peça o que precisa de uma vez.",
			Params: `{"type": "object", "properties": {
	"busca": {"type": "string", "description": "O que procurar, ex.: pizzaria, farmácia 24h, Parque Ibirapuera, padaria na Vila Madalena."},
	"perto_de": {"type": "string", "description": "Endereço, bairro, cidade ou lat,lng para centrar a busca; vazio = endereço base do dono."},
	"aberto_agora": {"type": "boolean"},
	"nota_minima": {"type": "number", "description": "De 1 a 5."},
	"max": {"type": "integer", "description": "Quantos lugares (padrão 10, até 20)."}
}, "required": ["busca"]}`,
			Summary: func(a Args) string { return "buscou no Maps" + quoted(a.Str("busca")) }},
		{Name: "maps_lugar", Risk: Read,
			Description: "Detalhes de um lugar do Google Maps pelo id (de maps_buscar_lugares): endereço, telefone, site, horários da semana, se está aberto, nota, descrição e as avaliações mais relevantes. Gasta 1 busca da SerpApi.",
			Params: `{"type": "object", "properties": {
	"id": {"type": "string"}
}, "required": ["id"]}`,
			Summary: func(a Args) string { return "detalhes de um lugar no Maps" }},
		{Name: "maps_rota", Risk: Read,
			Description: "Traça uma rota: tempo com o trânsito de agora (ou do horário de saída), atraso pelo trânsito, distância, se tem pedágio e, se pedido, o passo a passo; de transporte público mostra as opções com linhas e baldeações. Origem e destino: endereço, nome de lugar, lat,lng ou id:... de um lugar; origem vazia = endereço base do dono. Traz o link para abrir no Google Maps.",
			Params: `{"type": "object", "properties": {
	"origem": {"type": "string"},
	"destino": {"type": "string"},
	"modo": {"type": "string", "enum": ["carro", "a_pe", "bicicleta", "transporte", "moto"], "description": "Padrão carro."},
	"paradas": {"type": "array", "items": {"type": "string"}, "description": "Paradas no caminho, em ordem (não vale para transporte)."},
	"saida": {"type": "string", "description": "Quando sair: AAAA-MM-DD HH:MM no fuso do dono; vazio = agora."},
	"evitar": {"type": "array", "items": {"type": "string", "enum": ["pedagios", "rodovias", "balsas"]}},
	"alternativas": {"type": "boolean", "description": "Traz até 3 rotas."},
	"passos": {"type": "boolean", "description": "Traz o passo a passo."}
}, "required": ["destino"]}`,
			Summary: func(a Args) string {
				return "rota até " + strings.Trim(quoted(a.Str("destino")), " ")
			}},
		{Name: "maps_distancias", Risk: Read,
			Description: "Compara tempo (com o trânsito de agora) e distância de uma ou mais origens até um ou mais destinos (ex.: qual de 4 lojas fica mais perto, ou quanto cada pessoa leva até o encontro). Até 25 combinações; origens vazias = endereço base do dono.",
			Params: `{"type": "object", "properties": {
	"origens": {"type": "array", "items": {"type": "string"}},
	"destinos": {"type": "array", "items": {"type": "string"}},
	"modo": {"type": "string", "enum": ["carro", "a_pe", "bicicleta", "moto"]}
}, "required": ["destinos"]}`,
			Summary: func(a Args) string { return fmt.Sprintf("distâncias (%d destinos)", len(a.List("destinos"))) }},
		{Name: "maps_endereco", Risk: Read,
			Description: "Converte um endereço em coordenadas (e o endereço completo, com CEP) ou coordenadas lat,lng no endereço.",
			Params: `{"type": "object", "properties": {
	"endereco": {"type": "string", "description": "Endereço ou lat,lng."}
}, "required": ["endereco"]}`,
			Summary: func(a Args) string { return "endereço" + quoted(a.Str("endereco")) }},
	}
}

func (m *Maps) keys(env *Env) (serp, tomtom string) {
	x := env.Cred.Extra
	return strings.TrimSpace(x["serpapi"]), strings.TrimSpace(x["tomtom"])
}

var errNeedSerp = errors.New("isso usa a SerpApi, e o conector Mapas está sem a chave dela: cole a chave da SerpApi (grátis, só com e-mail) em Plugins → Mapas")
var errNeedTomTom = errors.New("isso usa o TomTom, e o conector Mapas está sem a chave dele: cole a chave do TomTom (grátis, só com e-mail) em Plugins → Mapas")

// --- SerpApi -------------------------------------------------------------

// serpCall runs one SerpApi search; out gets the JSON.
func (m *Maps) serpCall(ctx context.Context, env *Env, v url.Values, out any) error {
	key, _ := m.keys(env)
	if key == "" {
		return errNeedSerp
	}
	v.Set("api_key", key)
	v.Set("hl", "pt-br")
	v.Set("gl", "br")
	err := callJSON(ctx, request{URL: m.serp() + "/search.json?" + v.Encode()}, out)
	var ae *APIError
	if errors.As(err, &ae) || IsAuth(err) {
		b := ""
		if ae != nil {
			b = strings.ToLower(ae.Body)
		}
		switch {
		case IsAuth(err) || strings.Contains(b, "invalid api key"):
			return &AuthError{Msg: "a chave da SerpApi foi recusada; confira em Plugins → Mapas"}
		case strings.Contains(b, "run out of searches"):
			return errors.New("acabaram as buscas do mês da SerpApi (plano grátis); elas voltam no começo do próximo ciclo. Rotas e distâncias continuam pelo TomTom")
		}
	}
	return err
}

type serpPlace struct {
	Title     string  `json:"title"`
	PlaceID   string  `json:"place_id"`
	DataID    string  `json:"data_id"`
	Rating    float64 `json:"rating"`
	Reviews   int     `json:"reviews"`
	Price     string  `json:"price"`
	Type      any     `json:"type"`
	Address   string  `json:"address"`
	Phone     string  `json:"phone"`
	Website   string  `json:"website"`
	OpenState string  `json:"open_state"`
	Hours     any     `json:"hours"`
	Desc      string  `json:"description"`
	GPS       *struct {
		Lat float64 `json:"latitude"`
		Lng float64 `json:"longitude"`
	} `json:"gps_coordinates"`
	UserReviews struct {
		MostRelevant []struct {
			User   string  `json:"username"`
			Rating float64 `json:"rating"`
			Text   string  `json:"description"`
			Date   string  `json:"date"`
		} `json:"most_relevant"`
	} `json:"user_reviews"`
}

func (p serpPlace) kind() string {
	switch t := p.Type.(type) {
	case string:
		return t
	case []any:
		if len(t) > 0 {
			return fmt.Sprint(t[0])
		}
	}
	return ""
}

func (p serpPlace) rating() string {
	if p.Rating == 0 {
		return ""
	}
	return strings.Replace(fmt.Sprintf("nota %.1f (%d avaliações)", p.Rating, p.Reviews), ".", ",", 1)
}

// closed says whether Google shows the place closed now ("Fechado ⋅ Abre
// às 18:00", "Fechado temporariamente"); "Fecha em breve" is still open.
func (p serpPlace) closed() bool {
	s := strings.ToLower(strings.TrimSpace(p.OpenState))
	return strings.HasPrefix(s, "fechado") || strings.HasPrefix(s, "closed") || strings.Contains(s, "permanentemente")
}

func (p serpPlace) mapsLink() string {
	if p.PlaceID == "" {
		return ""
	}
	return "https://www.google.com/maps/place/?q=place_id:" + p.PlaceID
}

type serpSearch struct {
	Error  string      `json:"error"`
	Local  []serpPlace `json:"local_results"`
	Place  *serpPlace  `json:"place_results"`
	Search struct {
		Status string `json:"status"`
	} `json:"search_metadata"`
}

func noResults(msg string) bool {
	m := strings.ToLower(msg)
	return strings.Contains(m, "hasn't returned any results") || strings.Contains(m, "no results")
}

// --- TomTom --------------------------------------------------------------

type ttPos struct {
	Lat float64 `json:"lat"`
	Lon float64 `json:"lon"`
}

type ttResult struct {
	Type    string `json:"type"`
	Dist    float64
	Address struct {
		Free string `json:"freeformAddress"`
	} `json:"address"`
	Position ttPos `json:"position"`
	POI      *struct {
		Name    string `json:"name"`
		Phone   string `json:"phone"`
		URL     string `json:"url"`
		Classes []struct {
			Names []struct {
				Name string `json:"name"`
			} `json:"names"`
		} `json:"classifications"`
		Hours *struct {
			Ranges []struct {
				Start ttTime `json:"startTime"`
				End   ttTime `json:"endTime"`
			} `json:"timeRanges"`
		} `json:"openingHours"`
	} `json:"poi"`
}

type ttTime struct {
	Date   string `json:"date"`
	Hour   int    `json:"hour"`
	Minute int    `json:"minute"`
}

func (t ttTime) at(loc *time.Location) (time.Time, bool) {
	d, err := time.ParseInLocation("2006-01-02", t.Date, loc)
	if err != nil {
		return time.Time{}, false
	}
	return d.Add(time.Duration(t.Hour)*time.Hour + time.Duration(t.Minute)*time.Minute), true
}

func (r ttResult) name() string {
	if r.POI != nil && r.POI.Name != "" {
		return r.POI.Name
	}
	return r.Address.Free
}

// ttCall runs one TomTom request.
func (m *Maps) ttCall(ctx context.Context, env *Env, path string, v url.Values, out any) error {
	_, key := m.keys(env)
	if key == "" {
		return errNeedTomTom
	}
	v.Set("key", key)
	err := callJSON(ctx, request{URL: m.tomtom() + path + "?" + v.Encode()}, out)
	var ae *APIError
	if IsAuth(err) || (errors.As(err, &ae) && ae.Status == 403) {
		b := ""
		if ae != nil {
			b = strings.ToLower(ae.Body)
		}
		if strings.Contains(b, "over the limit") || strings.Contains(b, "quota") {
			return errors.New("acabaram as chamadas de hoje do TomTom (plano grátis); voltam amanhã")
		}
		return &AuthError{Msg: "a chave do TomTom foi recusada; confira em Plugins → Mapas (no painel do TomTom a chave precisa ter Search e Routing)"}
	}
	return err
}

// ttSearch is TomTom's fuzzy search: places and addresses.
func (m *Maps) ttSearch(ctx context.Context, env *Env, q string, limit int, near *ttPos, radius int) ([]ttResult, error) {
	v := url.Values{"language": {"pt-BR"}, "countrySet": {"BR"}, "limit": {strconv.Itoa(limit)}, "openingHours": {"nextSevenDays"}}
	if near != nil {
		v.Set("lat", fmt.Sprintf("%f", near.Lat))
		v.Set("lon", fmt.Sprintf("%f", near.Lon))
		if radius > 0 {
			v.Set("radius", strconv.Itoa(radius))
		}
	}
	var out struct {
		Results []ttResult `json:"results"`
	}
	err := m.ttCall(ctx, env, "/search/2/search/"+url.PathEscape(q)+".json", v, &out)
	return out.Results, err
}

// --- places --------------------------------------------------------------

var latLngRe = regexp.MustCompile(`^\s*(-?\d{1,2}(?:\.\d+)?)\s*,\s*(-?\d{1,3}(?:\.\d+)?)\s*$`)

func parseLatLng(s string) (ttPos, bool) {
	mm := latLngRe.FindStringSubmatch(s)
	if mm == nil {
		return ttPos{}, false
	}
	lat, _ := strconv.ParseFloat(mm[1], 64)
	lng, _ := strconv.ParseFloat(mm[2], 64)
	if lat < -90 || lat > 90 || lng < -180 || lng > 180 {
		return ttPos{}, false
	}
	return ttPos{lat, lng}, true
}

func (p ttPos) String() string { return fmt.Sprintf("%.6f,%.6f", p.Lat, p.Lon) }

// locate finds where a text is: TomTom when there is its key, else the
// first Google Maps result through SerpApi.
func (m *Maps) locate(ctx context.Context, env *Env, where string, near *ttPos) (ttPos, string, error) {
	if ll, ok := parseLatLng(where); ok {
		return ll, ll.String(), nil
	}
	if id := placeID(where); id != where || strings.HasPrefix(where, "ChIJ") {
		p, err := m.serpPlace(ctx, env, id)
		if err != nil {
			return ttPos{}, "", err
		}
		if p.GPS == nil {
			return ttPos{}, "", fmt.Errorf("o Google não deu a posição de %s", p.Title)
		}
		return ttPos{p.GPS.Lat, p.GPS.Lng}, joinNonEmpty(", ", p.Title, p.Address), nil
	}
	if _, tt := m.keys(env); tt != "" {
		res, err := m.ttSearch(ctx, env, where, 1, near, 0)
		if err != nil {
			return ttPos{}, "", err
		}
		if len(res) == 0 {
			return ttPos{}, "", fmt.Errorf("não achei %q no mapa; escreva com bairro e cidade", where)
		}
		return res[0].Position, joinNonEmpty(", ", poiName(res[0]), res[0].Address.Free), nil
	}
	var out serpSearch
	v := url.Values{"engine": {"google_maps"}, "type": {"search"}, "q": {where}}
	if near != nil {
		v.Set("ll", fmt.Sprintf("@%f,%f,14z", near.Lat, near.Lon))
	}
	if err := m.serpCall(ctx, env, v, &out); err != nil {
		return ttPos{}, "", err
	}
	p := out.Place
	if p == nil && len(out.Local) > 0 {
		p = &out.Local[0]
	}
	if p == nil || p.GPS == nil {
		return ttPos{}, "", fmt.Errorf("não achei %q no mapa; escreva com bairro e cidade", where)
	}
	return ttPos{p.GPS.Lat, p.GPS.Lng}, joinNonEmpty(", ", p.Title, p.Address), nil
}

func poiName(r ttResult) string {
	if r.POI != nil {
		return r.POI.Name
	}
	return ""
}

// base is the owner's base address, found once and kept with the
// credential.
func (m *Maps) base(ctx context.Context, env *Env) (ttPos, string, bool) {
	addr := strings.TrimSpace(env.Cred.Extra["base"])
	if addr == "" {
		return ttPos{}, "", false
	}
	x := env.Cred.Extra
	if x["base_de"] == addr {
		if ll, ok := parseLatLng(x["base_pos"]); ok {
			return ll, x["base_endereco"], true
		}
	}
	ll, full, err := m.locate(ctx, env, addr, nil)
	if err != nil {
		return ttPos{}, "", false
	}
	x["base_de"], x["base_endereco"], x["base_pos"] = addr, full, ll.String()
	env.Changed = true
	return ll, full, true
}

func (m *Maps) Check(ctx context.Context, env *Env) (string, error) {
	if env.Cred.Extra == nil {
		env.Cred.Extra = map[string]string{}
	}
	serp, tt := m.keys(env)
	if serp == "" && tt == "" {
		return "", &AuthError{Msg: "cole pelo menos uma chave: a da SerpApi (lugares e notas) ou a do TomTom (rotas e trânsito)"}
	}
	parts := []string{}
	if serp != "" {
		u, err := m.serpAccount(ctx, env)
		if err != nil {
			return "", err
		}
		parts = append(parts, fmt.Sprintf("SerpApi (%d buscas restantes)", u.Left))
	}
	if tt != "" {
		if _, err := m.ttSearch(ctx, env, "Praça da Sé, São Paulo", 1, nil, 0); err != nil {
			return "", err
		}
		parts = append(parts, "TomTom")
	}
	if strings.TrimSpace(env.Cred.Extra["base"]) != "" {
		_, full, ok := m.base(ctx, env)
		if !ok {
			return "", fmt.Errorf("não achei o endereço base %q no mapa; escreva com bairro e cidade", env.Cred.Extra["base"])
		}
		parts = append(parts, cut(full, 50))
	}
	return strings.Join(parts, " · "), nil
}

type serpAccountInfo struct {
	Plan  string `json:"plan_name"`
	Month int64  `json:"searches_per_month"`
	Used  int64  `json:"this_month_usage"`
	Left  int64  `json:"total_searches_left"`
	Error string `json:"error"`
}

// serpAccount reads the SerpApi plan; it costs no search.
func (m *Maps) serpAccount(ctx context.Context, env *Env) (serpAccountInfo, error) {
	key, _ := m.keys(env)
	var u serpAccountInfo
	if key == "" {
		return u, errNeedSerp
	}
	err := callJSON(ctx, request{URL: m.serp() + "/account.json?" + url.Values{"api_key": {key}}.Encode()}, &u)
	var ae *APIError
	if IsAuth(err) || errors.As(err, &ae) || u.Error != "" {
		return u, &AuthError{Msg: "a chave da SerpApi foi recusada; confira em Plugins → Mapas"}
	}
	return u, err
}

// Usage is the SerpApi searches of the month, the scarce part.
func (m *Maps) Usage(ctx context.Context, env *Env) (Usage, error) {
	u, err := m.serpAccount(ctx, env)
	if err != nil {
		return Usage{}, err
	}
	return Usage{Plan: u.Plan, Used: u.Used, Limit: u.Month, Unit: "buscas"}, nil
}

func (m *Maps) Call(ctx context.Context, env *Env, tool string, a Args) (string, error) {
	if env.Cred.Extra == nil {
		env.Cred.Extra = map[string]string{}
	}
	switch tool {
	case "maps_buscar_lugares":
		return m.findPlaces(ctx, env, a)
	case "maps_lugar":
		return m.place(ctx, env, a)
	case "maps_rota":
		return m.route(ctx, env, a)
	case "maps_distancias":
		return m.matrix(ctx, env, a)
	case "maps_endereco":
		return m.geocode(ctx, env, a)
	}
	return "", fmt.Errorf("ferramenta desconhecida: %s", tool)
}

func joinNonEmpty(sep string, parts ...string) string {
	out := []string{}
	for _, p := range parts {
		if p = strings.TrimSpace(p); p != "" {
			out = append(out, p)
		}
	}
	return strings.Join(out, sep)
}

// center is where a search looks around: the place asked, else the base.
func (m *Maps) center(ctx context.Context, env *Env, where string) (*ttPos, string, error) {
	if where != "" {
		ll, full, err := m.locate(ctx, env, where, nil)
		if err != nil {
			return nil, "", err
		}
		return &ll, full, nil
	}
	if ll, full, ok := m.base(ctx, env); ok {
		return &ll, full, nil
	}
	return nil, "", nil
}

func (m *Maps) findPlaces(ctx context.Context, env *Env, a Args) (string, error) {
	q, err := a.Need("busca")
	if err != nil {
		return "", err
	}
	n := a.Int("max", 10, 1, 20)
	minRating, _ := a["nota_minima"].(float64)
	near, nearName, err := m.center(ctx, env, a.Str("perto_de"))
	if err != nil {
		return "", err
	}
	if serp, _ := m.keys(env); serp == "" {
		return m.findPlacesTomTom(ctx, env, q, n, near, nearName, a.Bool("aberto_agora"))
	}
	v := url.Values{"engine": {"google_maps"}, "type": {"search"}, "q": {q}}
	if near != nil {
		v.Set("ll", fmt.Sprintf("@%f,%f,14z", near.Lat, near.Lon))
	}
	var out serpSearch
	if err := m.serpCall(ctx, env, v, &out); err != nil {
		return "", err
	}
	if out.Error != "" && !noResults(out.Error) {
		return "", errors.New("a SerpApi respondeu: " + out.Error)
	}
	places := out.Local
	if len(places) == 0 && out.Place != nil {
		places = []serpPlace{*out.Place}
	}
	kept := []serpPlace{}
	for _, p := range places {
		if a.Bool("aberto_agora") && (p.OpenState == "" || p.closed()) {
			continue
		}
		if minRating > 0 && p.Rating < minRating {
			continue
		}
		kept = append(kept, p)
	}
	if len(kept) > n {
		kept = kept[:n]
	}
	if len(kept) == 0 {
		return fmt.Sprintf("Nada encontrado no Google Maps para %q com esses filtros.", q), nil
	}
	var b strings.Builder
	fmt.Fprintf(&b, "%d lugares no Google Maps para %q", len(kept), q)
	if nearName != "" {
		fmt.Fprintf(&b, " perto de %s", nearName)
	}
	b.WriteString(":\n")
	for i, p := range kept {
		fmt.Fprintf(&b, "%d. %s", i+1, p.Title)
		if k := p.kind(); k != "" {
			fmt.Fprintf(&b, " (%s)", k)
		}
		fmt.Fprintf(&b, "\n   %s\n", joinNonEmpty(" · ", p.Address, p.rating(), p.OpenState, p.Price, p.Phone))
		if p.PlaceID != "" {
			fmt.Fprintf(&b, "   id:%s  %s\n", p.PlaceID, p.mapsLink())
		}
	}
	return b.String(), nil
}

// findPlacesTomTom is the search without SerpApi: no ratings.
func (m *Maps) findPlacesTomTom(ctx context.Context, env *Env, q string, n int, near *ttPos, nearName string, openNow bool) (string, error) {
	res, err := m.ttSearch(ctx, env, q, min(n*2, 40), near, 15000)
	if err != nil {
		return "", err
	}
	now := env.now()
	var b strings.Builder
	count := 0
	for _, r := range res {
		if count == n {
			break
		}
		open := ttOpen(r, now, env.zone())
		if openNow && open != "aberto agora" {
			continue
		}
		count++
		fmt.Fprintf(&b, "%d. %s", count, r.name())
		if r.POI != nil && len(r.POI.Classes) > 0 && len(r.POI.Classes[0].Names) > 0 {
			fmt.Fprintf(&b, " (%s)", r.POI.Classes[0].Names[0].Name)
		}
		phone, site := "", ""
		if r.POI != nil {
			phone, site = r.POI.Phone, r.POI.URL
		}
		fmt.Fprintf(&b, "\n   %s\n   %s\n", joinNonEmpty(" · ", r.Address.Free, open, phone, site), r.Position)
	}
	if count == 0 {
		return fmt.Sprintf("Nada encontrado para %q.", q), nil
	}
	head := fmt.Sprintf("%d lugares (TomTom, sem notas: cole a chave da SerpApi para ter as notas do Google) para %q", count, q)
	if nearName != "" {
		head += " perto de " + nearName
	}
	return head + ":\n" + b.String(), nil
}

// ttOpen says whether a TomTom place is open at now.
func ttOpen(r ttResult, now time.Time, loc *time.Location) string {
	if r.POI == nil || r.POI.Hours == nil || len(r.POI.Hours.Ranges) == 0 {
		return ""
	}
	for _, t := range r.POI.Hours.Ranges {
		s, ok1 := t.Start.at(loc)
		e, ok2 := t.End.at(loc)
		if ok1 && ok2 && !now.Before(s) && now.Before(e) {
			return "aberto agora"
		}
	}
	return "fechado agora"
}

func placeID(s string) string {
	s = strings.TrimSpace(s)
	s = strings.TrimPrefix(s, "id:")
	return strings.TrimSpace(s)
}

func (m *Maps) serpPlace(ctx context.Context, env *Env, id string) (serpPlace, error) {
	var out serpSearch
	if err := m.serpCall(ctx, env, url.Values{"engine": {"google_maps"}, "place_id": {id}}, &out); err != nil {
		return serpPlace{}, err
	}
	if out.Place == nil {
		msg := out.Error
		if msg == "" {
			msg = "o Google não achou esse lugar"
		}
		return serpPlace{}, errors.New(msg)
	}
	if out.Place.PlaceID == "" {
		out.Place.PlaceID = id
	}
	return *out.Place, nil
}

// hoursLines reads place_results.hours: [{"segunda-feira": "11:00–23:00"}].
func hoursLines(h any) []string {
	out := []string{}
	switch v := h.(type) {
	case []any:
		for _, d := range v {
			if mm, ok := d.(map[string]any); ok {
				for day, t := range mm {
					out = append(out, fmt.Sprintf("%s: %v", day, t))
				}
			}
		}
	case map[string]any:
		days := make([]string, 0, len(v))
		for d := range v {
			days = append(days, d)
		}
		sort.Strings(days)
		for _, d := range days {
			out = append(out, fmt.Sprintf("%s: %v", d, v[d]))
		}
	}
	return out
}

func (m *Maps) place(ctx context.Context, env *Env, a Args) (string, error) {
	raw, err := a.Need("id")
	if err != nil {
		return "", err
	}
	p, err := m.serpPlace(ctx, env, placeID(raw))
	if err != nil {
		return "", err
	}
	var b strings.Builder
	b.WriteString(p.Title)
	if k := p.kind(); k != "" {
		fmt.Fprintf(&b, " (%s)", k)
	}
	b.WriteString("\n")
	for _, line := range []string{p.Address, joinNonEmpty(" · ", p.rating(), p.OpenState, p.Price), p.Phone, p.Website, p.Desc} {
		if line != "" {
			b.WriteString(line + "\n")
		}
	}
	if hs := hoursLines(p.Hours); len(hs) > 0 {
		b.WriteString("Horários:\n")
		for _, h := range hs {
			b.WriteString("  " + h + "\n")
		}
	}
	for i, r := range p.UserReviews.MostRelevant {
		if i == 0 {
			b.WriteString("Avaliações mais relevantes:\n")
		}
		if i == 5 {
			break
		}
		fmt.Fprintf(&b, "  %.0f★ %s, %s: %s\n", r.Rating, r.User, r.Date, cut(r.Text, 400))
	}
	b.WriteString(p.mapsLink())
	return b.String(), nil
}

// --- routes --------------------------------------------------------------

var travelModes = map[string]struct{ tomtom, serp, link, word string }{
	"carro":      {"car", "0", "driving", "de carro"},
	"a_pe":       {"pedestrian", "2", "walking", "a pé"},
	"bicicleta":  {"bicycle", "1", "bicycling", "de bicicleta"},
	"transporte": {"", "3", "transit", "de transporte público"},
	"moto":       {"motorcycle", "9", "driving", "de moto"},
}

func travelMode(s string) (string, struct{ tomtom, serp, link, word string }) {
	s = strings.ToLower(strings.TrimSpace(s))
	switch s {
	case "a pé", "a pe", "ape", "andando", "caminhando":
		s = "a_pe"
	case "onibus", "ônibus", "metro", "metrô", "transporte publico", "transporte público":
		s = "transporte"
	}
	t, ok := travelModes[s]
	if !ok {
		s, t = "carro", travelModes["carro"]
	}
	return s, t
}

func isHome(s string) bool {
	switch strings.ToLower(strings.TrimSpace(s)) {
	case "", "casa", "minha casa", "aqui", "base", "endereço base", "endereco base", "daqui", "eu", "mim":
		return true
	}
	return false
}

// point resolves one end of a route to a position and a name for links.
func (m *Maps) point(ctx context.Context, env *Env, s string) (ttPos, string, error) {
	if isHome(s) {
		ll, full, ok := m.base(ctx, env)
		if !ok {
			return ttPos{}, "", errors.New("não sei de onde partir: diga a origem ou cadastre o endereço base em Plugins → Mapas")
		}
		return ll, full, nil
	}
	near, _, _ := m.base(ctx, env)
	var bias *ttPos
	if near != (ttPos{}) {
		bias = &near
	}
	return m.locate(ctx, env, s, bias)
}

func fmtDuration(sec int) string {
	mins := (sec + 30) / 60
	if mins < 1 {
		mins = 1
	}
	if mins < 60 {
		return fmt.Sprintf("%d min", mins)
	}
	if mins%60 == 0 {
		return fmt.Sprintf("%d h", mins/60)
	}
	return fmt.Sprintf("%d h %d min", mins/60, mins%60)
}

func fmtDistance(m int) string {
	if m < 1000 {
		return fmt.Sprintf("%d m", m)
	}
	return strings.Replace(fmt.Sprintf("%.1f km", float64(m)/1000), ".", ",", 1)
}

type ttRoute struct {
	Summary struct {
		Length    int `json:"lengthInMeters"`
		Time      int `json:"travelTimeInSeconds"`
		Delay     int `json:"trafficDelayInSeconds"`
		NoTraffic int `json:"noTrafficTravelTimeInSeconds"`
	} `json:"summary"`
	Sections []struct {
		Type string `json:"sectionType"`
	} `json:"sections"`
	Guidance struct {
		Instructions []struct {
			Message string `json:"message"`
			Offset  int    `json:"routeOffsetInMeters"`
		} `json:"instructions"`
	} `json:"guidance"`
}

func (r ttRoute) toll() bool {
	return slices.ContainsFunc(r.Sections, func(s struct {
		Type string `json:"sectionType"`
	}) bool {
		return s.Type == "TOLL" || s.Type == "TOLL_ROAD" || s.Type == "TOLL_VIGNETTE"
	})
}

func mapsDirLink(link string, names ...string) string {
	if slices.Contains(names, "") || len(names) < 2 {
		return ""
	}
	v := url.Values{"api": {"1"}, "origin": {names[0]}, "destination": {names[len(names)-1]}, "travelmode": {link}}
	if len(names) > 2 {
		v.Set("waypoints", strings.Join(names[1:len(names)-1], "|"))
	}
	return "Abrir no Google Maps: https://www.google.com/maps/dir/?" + v.Encode()
}

func (m *Maps) route(ctx context.Context, env *Env, a Args) (string, error) {
	dest, err := a.Need("destino")
	if err != nil {
		return "", err
	}
	modeName, mode := travelMode(a.Str("modo"))
	var depart time.Time
	if s := a.Str("saida"); s != "" && !strings.EqualFold(s, "agora") {
		t, _, err := parseWhen(s, env.zone())
		if err != nil {
			return "", err
		}
		if t.After(env.now()) {
			depart = t
		}
	}
	serp, tt := m.keys(env)
	if modeName == "transporte" || tt == "" {
		if serp == "" {
			if modeName == "transporte" {
				return "", errors.New("transporte público usa a SerpApi: cole a chave dela em Plugins → Mapas")
			}
			return "", errNeedTomTom
		}
		return m.routeSerp(ctx, env, a, dest, mode, depart)
	}
	names := []string{}
	points := []ttPos{}
	for _, s := range append(append([]string{a.Str("origem")}, a.List("paradas")...), dest) {
		p, name, err := m.point(ctx, env, s)
		if err != nil {
			return "", err
		}
		points, names = append(points, p), append(names, name)
	}
	locs := make([]string, len(points))
	for i, p := range points {
		locs[i] = p.String()
	}
	v := url.Values{"travelMode": {mode.tomtom}, "traffic": {"true"}, "computeTravelTimeFor": {"all"},
		"routeRepresentation": {"summaryOnly"}, "sectionType": {"toll"}, "language": {"pt-BR"}}
	if a.Bool("alternativas") {
		v.Set("maxAlternatives", "2")
	}
	if a.Bool("passos") {
		v.Set("instructionsType", "text")
	}
	if !depart.IsZero() {
		v.Set("departAt", depart.Format(time.RFC3339))
	}
	for _, e := range a.List("evitar") {
		switch strings.ToLower(e) {
		case "pedagios", "pedágios", "pedagio", "pedágio":
			v.Add("avoid", "tollRoads")
		case "rodovias", "rodovia", "estradas":
			v.Add("avoid", "motorways")
		case "balsas", "balsa":
			v.Add("avoid", "ferries")
		}
	}
	var out struct {
		Routes []ttRoute `json:"routes"`
	}
	if err := m.ttCall(ctx, env, "/routing/1/calculateRoute/"+strings.Join(locs, ":")+"/json", v, &out); err != nil {
		var ae *APIError
		if errors.As(err, &ae) && ae.Status == 400 {
			return "", fmt.Errorf("o TomTom não achou rota %s entre esses pontos", mode.word)
		}
		return "", err
	}
	if len(out.Routes) == 0 {
		return fmt.Sprintf("Não achei rota %s até %s.", mode.word, dest), nil
	}
	var b strings.Builder
	fmt.Fprintf(&b, "Rota %s", mode.word)
	if !depart.IsZero() {
		fmt.Fprintf(&b, " saindo %s", depart.In(env.zone()).Format("02/01 15:04"))
	}
	fmt.Fprintf(&b, " de %s até %s:\n", names[0], names[len(names)-1])
	for i, r := range out.Routes {
		if len(out.Routes) > 1 {
			fmt.Fprintf(&b, "%d. ", i+1)
		}
		line := fmtDuration(r.Summary.Time)
		if r.Summary.Delay >= 120 {
			line += fmt.Sprintf(" (%s de atraso pelo trânsito)", fmtDuration(r.Summary.Delay))
		}
		toll := ""
		if r.toll() {
			toll = "tem pedágio"
		}
		b.WriteString(joinNonEmpty(" · ", line, fmtDistance(r.Summary.Length), toll) + "\n")
		for j, in := range r.Guidance.Instructions {
			if j == 40 {
				b.WriteString("   …\n")
				break
			}
			if in.Message != "" {
				fmt.Fprintf(&b, "   %d. %s (km %s)\n", j+1, in.Message, strings.Replace(fmt.Sprintf("%.1f", float64(in.Offset)/1000), ".", ",", 1))
			}
		}
	}
	b.WriteString(mapsDirLink(mode.link, names...))
	return b.String(), nil
}

type serpDirections struct {
	Error      string `json:"error"`
	Directions []struct {
		Via      string `json:"via"`
		Duration string `json:"formatted_duration"`
		Distance string `json:"formatted_distance"`
		Ext      []any  `json:"extensions"`
		Trips    []struct {
			Title    string `json:"title"`
			Mode     string `json:"travel_mode"`
			Duration string `json:"formatted_duration"`
			Distance string `json:"formatted_distance"`
			Details  []struct {
				Title string `json:"title"`
			} `json:"details"`
		} `json:"trips"`
	} `json:"directions"`
}

// routeSerp asks Google Maps for the route through SerpApi: public
// transport, or any mode when there is no TomTom key.
func (m *Maps) routeSerp(ctx context.Context, env *Env, a Args, dest string, mode struct{ tomtom, serp, link, word string }, depart time.Time) (string, error) {
	v := url.Values{"engine": {"google_maps_directions"}, "travel_mode": {mode.serp}}
	ends := map[string]string{"start": a.Str("origem"), "end": dest}
	names := []string{}
	for _, side := range []string{"start", "end"} {
		s := ends[side]
		switch {
		case isHome(s):
			ll, full, ok := m.base(ctx, env)
			if !ok {
				return "", errors.New("não sei de onde partir: diga a origem ou cadastre o endereço base em Plugins → Mapas")
			}
			v.Set(side+"_coords", ll.String())
			names = append(names, full)
		default:
			if ll, ok := parseLatLng(s); ok {
				v.Set(side+"_coords", ll.String())
			} else if id := placeID(s); id != s {
				p, err := m.serpPlace(ctx, env, id)
				if err != nil {
					return "", err
				}
				s = joinNonEmpty(", ", p.Title, p.Address)
				v.Set(side+"_addr", s)
			} else {
				v.Set(side+"_addr", s)
			}
			names = append(names, s)
		}
	}
	if !depart.IsZero() {
		v.Set("time", fmt.Sprintf("depart_at:%d", depart.Unix()))
	}
	var out serpDirections
	if err := m.serpCall(ctx, env, v, &out); err != nil {
		return "", err
	}
	if len(out.Directions) == 0 {
		if out.Error != "" && !noResults(out.Error) {
			return "", errors.New("a SerpApi respondeu: " + out.Error)
		}
		return fmt.Sprintf("O Google Maps não achou rota %s até %s.", mode.word, dest), nil
	}
	var b strings.Builder
	fmt.Fprintf(&b, "Rota %s (Google Maps) de %s até %s:\n", mode.word, names[0], names[1])
	for i, d := range out.Directions {
		if i == 3 {
			break
		}
		fmt.Fprintf(&b, "%d. %s", i+1, joinNonEmpty(" · ", d.Duration, d.Distance))
		if d.Via != "" {
			b.WriteString(" · via " + d.Via)
		}
		b.WriteString("\n")
		if mode.serp == "3" || a.Bool("passos") {
			for _, t := range d.Trips {
				fmt.Fprintf(&b, "   - %s\n", joinNonEmpty(" · ", t.Title, t.Duration, t.Distance))
				if a.Bool("passos") {
					for j, s := range t.Details {
						if j == 15 {
							break
						}
						fmt.Fprintf(&b, "       %s\n", s.Title)
					}
				}
			}
		}
	}
	b.WriteString(mapsDirLink(mode.link, names...))
	return b.String(), nil
}

// matrixCells caps how many routes one comparison asks for.
const matrixCells = 25

func (m *Maps) matrix(ctx context.Context, env *Env, a Args) (string, error) {
	dests := a.List("destinos")
	origins := a.List("origens")
	if len(origins) == 0 {
		origins = []string{""}
	}
	if len(dests) == 0 {
		return "", errors.New("falta o argumento destinos")
	}
	if len(origins)*len(dests) > matrixCells {
		return "", fmt.Errorf("até %d combinações de origem e destino por vez", matrixCells)
	}
	if _, tt := m.keys(env); tt == "" {
		return "", errNeedTomTom
	}
	_, mode := travelMode(a.Str("modo"))
	if mode.tomtom == "" {
		_, mode = travelMode("carro")
	}
	resolve := func(list []string) ([]ttPos, []string, error) {
		ps, ns := []ttPos{}, []string{}
		for _, s := range list {
			p, name, err := m.point(ctx, env, s)
			if err != nil {
				return nil, nil, err
			}
			if !isHome(s) {
				name = s
			}
			ps, ns = append(ps, p), append(ns, name)
		}
		return ps, ns, nil
	}
	op, on, err := resolve(origins)
	if err != nil {
		return "", err
	}
	dp, dn, err := resolve(dests)
	if err != nil {
		return "", err
	}
	type cell struct {
		o, d, sec, meters int
		err               error
	}
	cells := []*cell{}
	for i := range op {
		for j := range dp {
			cells = append(cells, &cell{o: i, d: j})
		}
	}
	var wg sync.WaitGroup
	sem := make(chan struct{}, 4)
	for _, c := range cells {
		wg.Add(1)
		go func(c *cell) {
			defer wg.Done()
			sem <- struct{}{}
			defer func() { <-sem }()
			var out struct {
				Routes []ttRoute `json:"routes"`
			}
			v := url.Values{"travelMode": {mode.tomtom}, "traffic": {"true"}, "routeRepresentation": {"summaryOnly"}}
			c.err = m.ttCall(ctx, env, "/routing/1/calculateRoute/"+op[c.o].String()+":"+dp[c.d].String()+"/json", v, &out)
			if c.err == nil && len(out.Routes) > 0 {
				c.sec, c.meters = out.Routes[0].Summary.Time, out.Routes[0].Summary.Length
			} else if c.err == nil {
				c.err = errors.New("sem rota")
			}
		}(c)
	}
	wg.Wait()
	for _, c := range cells {
		if IsAuth(c.err) {
			return "", c.err
		}
	}
	sort.SliceStable(cells, func(i, j int) bool {
		if cells[i].o != cells[j].o {
			return cells[i].o < cells[j].o
		}
		if (cells[i].err == nil) != (cells[j].err == nil) {
			return cells[i].err == nil
		}
		return cells[i].sec < cells[j].sec
	})
	var b strings.Builder
	fmt.Fprintf(&b, "Tempo %s com o trânsito de agora, do mais perto ao mais longe:\n", mode.word)
	last := -1
	for _, c := range cells {
		if c.o != last {
			fmt.Fprintf(&b, "De %s:\n", on[c.o])
			last = c.o
		}
		if c.err != nil {
			fmt.Fprintf(&b, "  %s: sem rota\n", dn[c.d])
			continue
		}
		fmt.Fprintf(&b, "  %s: %s · %s\n", dn[c.d], fmtDuration(c.sec), fmtDistance(c.meters))
	}
	return b.String(), nil
}

func (m *Maps) geocode(ctx context.Context, env *Env, a Args) (string, error) {
	q, err := a.Need("endereco")
	if err != nil {
		return "", err
	}
	if _, tt := m.keys(env); tt == "" {
		ll, full, err := m.locate(ctx, env, q, nil)
		if err != nil {
			return "", err
		}
		return fmt.Sprintf("%s\n   %s", full, ll), nil
	}
	if ll, ok := parseLatLng(q); ok {
		var out struct {
			Addresses []struct {
				Address struct {
					Free string `json:"freeformAddress"`
				} `json:"address"`
			} `json:"addresses"`
		}
		if err := m.ttCall(ctx, env, "/search/2/reverseGeocode/"+ll.String()+".json", url.Values{"language": {"pt-BR"}}, &out); err != nil {
			return "", err
		}
		if len(out.Addresses) == 0 {
			return "Não achei endereço nesse ponto.", nil
		}
		return out.Addresses[0].Address.Free + "\n   " + ll.String(), nil
	}
	var out struct {
		Results []ttResult `json:"results"`
	}
	if err := m.ttCall(ctx, env, "/search/2/geocode/"+url.PathEscape(q)+".json", url.Values{"language": {"pt-BR"}, "countrySet": {"BR"}, "limit": {"3"}}, &out); err != nil {
		return "", err
	}
	if len(out.Results) == 0 {
		return fmt.Sprintf("Não achei %q.", q), nil
	}
	var b strings.Builder
	for _, r := range out.Results {
		fmt.Fprintf(&b, "%s\n   %s\n", r.Address.Free, r.Position)
	}
	return b.String(), nil
}
