package connect

import (
	"context"
	"errors"
	"fmt"
	"net/url"
	"regexp"
	"strings"
	"time"
)

// More of Google on the same login: Sheets (read any tab, write, append,
// create), Tasks, Contacts and YouTube. Docs are read and created through
// Drive already. Their scopes are optional at consent: a connection made
// before them keeps working, and these tools ask for a reconnect.

const (
	sheetsHost = "https://sheets.googleapis.com"
	peopleHost = "https://people.googleapis.com"
)

// googleExtraScopes are asked for but not required.
var googleExtraScopes = []string{
	"https://www.googleapis.com/auth/spreadsheets",
	"https://www.googleapis.com/auth/tasks",
	"https://www.googleapis.com/auth/contacts.readonly",
	"https://www.googleapis.com/auth/youtube.readonly",
}

// googleAPIName is what the owner enables in Google Cloud for each tool group.
var googleAPIName = map[string]string{
	"planilhas_": "Google Sheets API",
	"tarefas_":   "Google Tasks API",
	"contatos_":  "People API",
	"youtube_":   "YouTube Data API v3",
	"gmail_":     "Gmail API",
	"drive_":     "Google Drive API",
	"agenda_":    "Google Calendar API",
}

// explainGoogle turns the two refusals the owner can fix into what to do.
func explainGoogle(tool string, err error) error {
	var api *APIError
	if !errors.As(err, &api) || api.Status != 403 {
		return err
	}
	name := ""
	for prefix, n := range googleAPIName {
		if strings.HasPrefix(tool, prefix) {
			name = n
		}
	}
	switch {
	case strings.Contains(api.Body, "SERVICE_DISABLED") || strings.Contains(api.Body, "has not been used") || strings.Contains(api.Body, "is disabled"):
		return fmt.Errorf("a %s está desligada no projeto do Google Cloud do dono: ele ativa em console.cloud.google.com → APIs e serviços → Biblioteca → %s → Ativar, e tenta de novo em 1 minuto", name, name)
	case strings.Contains(api.Body, "insufficient") || strings.Contains(api.Body, "INSUFFICIENT") || strings.Contains(api.Body, "PERMISSION_DENIED") && strings.Contains(api.Body, "scope"):
		return errors.New("o Google ainda não deu ao Avora a permissão para isso (a conexão é anterior a ela, ou a caixa ficou desmarcada); o dono reconecta o Google em Plugins e marca todas as caixas")
	}
	return err
}

func (g *Google) moreTools() []Tool {
	return []Tool{
		{Name: "planilhas_ler", Risk: Read,
			Description: "Lê uma Planilha Google do dono: sem intervalo, lista as abas e mostra a primeira; com intervalo (ex.: Vendas!A1:F50 ou só Vendas), lê esse trecho. Linhas numeradas, colunas separadas por |.",
			Params: `{"type": "object", "properties": {
	"planilha": {"type": "string", "description": "O id ou o link da planilha (ache com drive_buscar)."},
	"intervalo": {"type": "string", "description": "Notação A1: Aba!A1:D20, ou o nome da aba."}
}, "required": ["planilha"]}`,
			Summary: func(a Args) string { return "leu a planilha" + quoted(a.Str("intervalo")) }},
		{Name: "planilhas_escrever", Risk: Write,
			Description: "Escreve numa Planilha Google do dono: substitui as células a partir do intervalo (ex.: Vendas!A2) ou, com adicionar=true, acrescenta as linhas no fim da tabela da aba. Fórmulas (=SOMA(...)) funcionam.",
			Params: `{"type": "object", "properties": {
	"planilha": {"type": "string", "description": "O id ou o link."},
	"intervalo": {"type": "string", "description": "Onde começar (Aba!A1) ou, para adicionar, só a aba."},
	"linhas": {"type": "array", "items": {"type": "array", "items": {"type": ["string", "number", "boolean"]}}, "description": "As linhas, cada uma uma lista de células."},
	"adicionar": {"type": "boolean", "description": "true acrescenta no fim em vez de substituir."}
}, "required": ["planilha", "intervalo", "linhas"]}`,
			Summary: func(a Args) string {
				verb := "escreveu em"
				if a.Bool("adicionar") {
					verb = "acrescentou linhas em"
				}
				return verb + " " + a.Str("intervalo")
			}},
		{Name: "planilhas_criar", Risk: Write,
			Description: "Cria uma Planilha Google no Drive do dono, opcionalmente já com linhas (a primeira costuma ser o cabeçalho). Devolve o id e o link.",
			Params: `{"type": "object", "properties": {
	"titulo": {"type": "string"},
	"linhas": {"type": "array", "items": {"type": "array", "items": {"type": ["string", "number", "boolean"]}}}
}, "required": ["titulo"]}`,
			Summary: func(a Args) string { return "criou a planilha «" + cut(a.Str("titulo"), 80) + "»" }},
		{Name: "tarefas_listar", Risk: Read,
			Description: "Lista as tarefas pendentes do Google Tasks do dono (as listas e, em cada uma, título, prazo, notas e id). concluidas=true inclui as feitas.",
			Params: `{"type": "object", "properties": {
	"lista": {"type": "string", "description": "Nome ou id de uma lista; vazio = todas."},
	"concluidas": {"type": "boolean"}}}`,
			Summary: func(a Args) string { return "tarefas do Google" + quoted(a.Str("lista")) }},
		{Name: "tarefas_criar", Risk: Write,
			Description: "Cria uma tarefa no Google Tasks do dono (aparece no Gmail, na Agenda e no celular).",
			Params: `{"type": "object", "properties": {
	"titulo": {"type": "string"},
	"notas": {"type": "string"},
	"prazo": {"type": "string", "description": "AAAA-MM-DD."},
	"lista": {"type": "string", "description": "Nome ou id da lista; vazio = a principal."}
}, "required": ["titulo"]}`,
			Summary: func(a Args) string { return "criou a tarefa «" + cut(a.Str("titulo"), 80) + "»" }},
		{Name: "tarefas_concluir", Risk: Write,
			Description: "Marca uma tarefa do Google Tasks como feita, pelo id de tarefas_listar.",
			Params: `{"type": "object", "properties": {
	"id": {"type": "string"},
	"lista": {"type": "string", "description": "A lista da tarefa (nome ou id); vazio = a principal."}
}, "required": ["id"]}`,
			Summary: func(a Args) string { return "concluiu a tarefa " + a.Str("id") }},
		{Name: "contatos_buscar", Risk: Read,
			Description: "Busca nos Contatos Google do dono por nome, e-mail ou telefone: devolve nome, e-mails, telefones, empresa e aniversário.",
			Params: `{"type": "object", "properties": {
	"busca": {"type": "string"}}, "required": ["busca"]}`,
			Summary: func(a Args) string { return "contatos" + quoted(a.Str("busca")) }},
		{Name: "youtube_buscar", Risk: Read,
			Description: "Busca vídeos no YouTube: título, canal, data, duração, visualizações e link.",
			Params: `{"type": "object", "properties": {
	"busca": {"type": "string"},
	"quantidade": {"type": "integer", "description": "Padrão 8, até 25."}}, "required": ["busca"]}`,
			Summary: func(a Args) string { return "YouTube" + quoted(a.Str("busca")) }},
		{Name: "youtube_meu_canal", Risk: Read,
			Description: "O canal do dono no YouTube: inscritos, visualizações, número de vídeos e os últimos vídeos com visualizações, curtidas e comentários.",
			Params:      `{"type": "object", "properties": {"quantidade": {"type": "integer", "description": "Quantos vídeos recentes (padrão 10, até 50)."}}}`,
			Summary:     func(Args) string { return "canal do YouTube" }},
	}
}

func (g *Google) more(ctx context.Context, env *Env, tool string, a Args) (string, error) {
	var out string
	var err error
	switch {
	case strings.HasPrefix(tool, "planilhas_"):
		out, err = g.sheets(ctx, env, tool, a)
	case strings.HasPrefix(tool, "tarefas_"):
		out, err = g.tasks(ctx, env, tool, a)
	case tool == "contatos_buscar":
		out, err = g.contacts(ctx, env, a)
	case strings.HasPrefix(tool, "youtube_"):
		out, err = g.youtube(ctx, env, tool, a)
	default:
		return "", fmt.Errorf("ferramenta desconhecida %s", tool)
	}
	return out, explainGoogle(tool, err)
}

// ── Sheets ──────────────────────────────────────────────────────

var sheetLink = regexp.MustCompile(`/spreadsheets/d/([A-Za-z0-9_-]+)`)

func sheetID(s string) string {
	if m := sheetLink.FindStringSubmatch(s); m != nil {
		return m[1]
	}
	return strings.TrimSpace(s)
}

// cells turns the "linhas" argument into rows for the API.
func cells(v any) ([][]any, error) {
	rows, ok := v.([]any)
	if !ok || len(rows) == 0 {
		return nil, errors.New("linhas precisa ser uma lista de linhas, cada uma uma lista de células")
	}
	out := make([][]any, 0, len(rows))
	for _, r := range rows {
		switch row := r.(type) {
		case []any:
			out = append(out, row)
		default:
			out = append(out, []any{row})
		}
	}
	return out, nil
}

func rowsText(values [][]any, first int) string {
	if len(values) == 0 {
		return "(vazio)"
	}
	lines := make([]string, 0, len(values))
	for i, r := range values {
		cs := make([]string, len(r))
		for j, c := range r {
			cs[j] = fmt.Sprint(c)
		}
		lines = append(lines, fmt.Sprintf("%d: %s", first+i, strings.Join(cs, " | ")))
	}
	return strings.Join(lines, "\n")
}

// firstRow is the row number an A1 range starts at (1 when unknown).
func firstRow(rng string) int {
	if i := strings.LastIndex(rng, "!"); i >= 0 {
		rng = rng[i+1:]
	}
	n := 0
	for _, r := range rng {
		if r >= '0' && r <= '9' {
			n = n*10 + int(r-'0')
		} else if n > 0 {
			break
		}
	}
	return max(1, n)
}

func (g *Google) sheets(ctx context.Context, env *Env, tool string, a Args) (string, error) {
	base := g.url(sheetsHost, "/v4/spreadsheets")
	switch tool {
	case "planilhas_ler":
		id := sheetID(a.Str("planilha"))
		if id == "" {
			return "", errors.New("falta o argumento planilha")
		}
		rng := a.Str("intervalo")
		head := ""
		if rng == "" {
			var meta struct {
				Properties struct {
					Title string `json:"title"`
				} `json:"properties"`
				Sheets []struct {
					Properties struct {
						Title string `json:"title"`
						Grid  struct {
							Rows int `json:"rowCount"`
							Cols int `json:"columnCount"`
						} `json:"gridProperties"`
					} `json:"properties"`
				} `json:"sheets"`
			}
			if err := g.api(ctx, env, request{URL: base + "/" + url.PathEscape(id) + "?fields=properties.title,sheets.properties(title,gridProperties)"}, &meta); err != nil {
				return "", err
			}
			if len(meta.Sheets) == 0 {
				return "A planilha não tem abas.", nil
			}
			tabs := []string{}
			for _, s := range meta.Sheets {
				tabs = append(tabs, fmt.Sprintf("%s (%d linhas)", s.Properties.Title, s.Properties.Grid.Rows))
			}
			head = fmt.Sprintf("Planilha «%s». Abas: %s.\n\n", meta.Properties.Title, strings.Join(tabs, "; "))
			rng = "'" + strings.ReplaceAll(meta.Sheets[0].Properties.Title, "'", "''") + "'!A1:Z200"
		}
		var r struct {
			Range  string  `json:"range"`
			Values [][]any `json:"values"`
		}
		if err := g.api(ctx, env, request{URL: base + "/" + url.PathEscape(id) + "/values/" + url.PathEscape(rng) + "?valueRenderOption=FORMATTED_VALUE"}, &r); err != nil {
			return "", err
		}
		return head + r.Range + "\n" + rowsText(r.Values, firstRow(r.Range)), nil

	case "planilhas_escrever":
		id := sheetID(a.Str("planilha"))
		rng, err := a.Need("intervalo")
		if err != nil {
			return "", err
		}
		if id == "" {
			return "", errors.New("falta o argumento planilha")
		}
		rows, err := cells(a["linhas"])
		if err != nil {
			return "", err
		}
		var r struct {
			Updates struct {
				Range string `json:"updatedRange"`
				Cells int    `json:"updatedCells"`
			} `json:"updates"`
			Range string `json:"updatedRange"`
			Cells int    `json:"updatedCells"`
		}
		body := map[string]any{"values": rows, "majorDimension": "ROWS"}
		path := base + "/" + url.PathEscape(id) + "/values/" + url.PathEscape(rng)
		if a.Bool("adicionar") {
			err = g.api(ctx, env, request{Method: "POST", URL: path + ":append?valueInputOption=USER_ENTERED&insertDataOption=INSERT_ROWS", Body: body}, &r)
			r.Range, r.Cells = r.Updates.Range, r.Updates.Cells
		} else {
			err = g.api(ctx, env, request{Method: "PUT", URL: path + "?valueInputOption=USER_ENTERED", Body: body}, &r)
		}
		if err != nil {
			return "", err
		}
		return fmt.Sprintf("Pronto: %d células em %s.", r.Cells, r.Range), nil

	case "planilhas_criar":
		title, err := a.Need("titulo")
		if err != nil {
			return "", err
		}
		var r struct {
			ID     string `json:"spreadsheetId"`
			URL    string `json:"spreadsheetUrl"`
			Sheets []struct {
				Properties struct {
					Title string `json:"title"`
				} `json:"properties"`
			} `json:"sheets"`
		}
		if err := g.api(ctx, env, request{Method: "POST", URL: base, Body: map[string]any{"properties": map[string]string{"title": title}}}, &r); err != nil {
			return "", err
		}
		msg := fmt.Sprintf("Criada: %s (id %s)\n%s", title, r.ID, r.URL)
		if a["linhas"] != nil {
			rows, err := cells(a["linhas"])
			if err != nil {
				return msg + "\nAs linhas não foram escritas: " + err.Error(), nil
			}
			tab := "Sheet1"
			if len(r.Sheets) > 0 {
				tab = r.Sheets[0].Properties.Title
			}
			rng := "'" + strings.ReplaceAll(tab, "'", "''") + "'!A1"
			if err := g.api(ctx, env, request{Method: "PUT", URL: base + "/" + url.PathEscape(r.ID) + "/values/" + url.PathEscape(rng) + "?valueInputOption=USER_ENTERED",
				Body: map[string]any{"values": rows}}, nil); err != nil {
				return msg + "\nAs linhas não foram escritas: " + explainGoogle("planilhas_criar", err).Error(), nil
			}
			msg += fmt.Sprintf("\nCom %d linhas.", len(rows))
		}
		return msg, nil
	}
	return "", fmt.Errorf("ferramenta desconhecida %s", tool)
}

// ── Tasks ───────────────────────────────────────────────────────

type taskList struct {
	ID    string `json:"id"`
	Title string `json:"title"`
}

func (g *Google) taskLists(ctx context.Context, env *Env) ([]taskList, error) {
	var r struct {
		Items []taskList `json:"items"`
	}
	err := g.api(ctx, env, request{URL: g.url(apisHost, "/tasks/v1/users/@me/lists?maxResults=100")}, &r)
	return r.Items, err
}

// pickList finds a list by id or name; "" is the first (the default one).
func pickList(lists []taskList, want string) (taskList, error) {
	if len(lists) == 0 {
		return taskList{}, errors.New("o dono não tem listas no Google Tasks")
	}
	if want == "" {
		return lists[0], nil
	}
	for _, l := range lists {
		if l.ID == want || strings.EqualFold(l.Title, want) {
			return l, nil
		}
	}
	names := []string{}
	for _, l := range lists {
		names = append(names, l.Title)
	}
	return taskList{}, fmt.Errorf("não achei a lista %q; as listas são: %s", want, strings.Join(names, ", "))
}

func (g *Google) tasks(ctx context.Context, env *Env, tool string, a Args) (string, error) {
	lists, err := g.taskLists(ctx, env)
	if err != nil {
		return "", err
	}
	switch tool {
	case "tarefas_listar":
		chosen := lists
		if want := a.Str("lista"); want != "" {
			l, err := pickList(lists, want)
			if err != nil {
				return "", err
			}
			chosen = []taskList{l}
		}
		var b strings.Builder
		for _, l := range chosen {
			q := url.Values{"maxResults": {"100"}, "showCompleted": {fmt.Sprint(a.Bool("concluidas"))}, "showHidden": {fmt.Sprint(a.Bool("concluidas"))}}
			var r struct {
				Items []struct {
					ID     string `json:"id"`
					Title  string `json:"title"`
					Notes  string `json:"notes"`
					Due    string `json:"due"`
					Status string `json:"status"`
				} `json:"items"`
			}
			if err := g.api(ctx, env, request{URL: g.url(apisHost, "/tasks/v1/lists/"+url.PathEscape(l.ID)+"/tasks?"+q.Encode())}, &r); err != nil {
				return "", err
			}
			fmt.Fprintf(&b, "Lista «%s» (%d):\n", l.Title, len(r.Items))
			for _, t := range r.Items {
				line := "- " + orStr(t.Title, "(sem título)")
				if t.Status == "completed" {
					line = "- ✓ " + orStr(t.Title, "(sem título)")
				}
				if d, err := time.Parse(time.RFC3339, t.Due); err == nil {
					line += " · prazo " + d.Format("02/01")
				}
				if t.Notes != "" {
					line += " · " + cut(t.Notes, 120)
				}
				b.WriteString(line + " [id " + t.ID + "]\n")
			}
		}
		return strings.TrimSpace(b.String()), nil

	case "tarefas_criar":
		title, err := a.Need("titulo")
		if err != nil {
			return "", err
		}
		l, err := pickList(lists, a.Str("lista"))
		if err != nil {
			return "", err
		}
		body := map[string]string{"title": title}
		if s := a.Str("notas"); s != "" {
			body["notes"] = s
		}
		if s := a.Str("prazo"); s != "" {
			d, err := time.Parse("2006-01-02", s)
			if err != nil {
				return "", fmt.Errorf("prazo em AAAA-MM-DD: %q", s)
			}
			body["due"] = d.Format(time.RFC3339)
		}
		var r struct {
			ID string `json:"id"`
		}
		if err := g.api(ctx, env, request{Method: "POST", URL: g.url(apisHost, "/tasks/v1/lists/"+url.PathEscape(l.ID)+"/tasks"), Body: body}, &r); err != nil {
			return "", err
		}
		return fmt.Sprintf("Tarefa criada em «%s»: %s (id %s)", l.Title, title, r.ID), nil

	case "tarefas_concluir":
		id, err := a.Need("id")
		if err != nil {
			return "", err
		}
		l, err := pickList(lists, a.Str("lista"))
		if err != nil {
			return "", err
		}
		var r struct {
			Title string `json:"title"`
		}
		if err := g.api(ctx, env, request{Method: "PATCH", URL: g.url(apisHost, "/tasks/v1/lists/"+url.PathEscape(l.ID)+"/tasks/"+url.PathEscape(id)),
			Body: map[string]string{"status": "completed"}}, &r); err != nil {
			return "", err
		}
		return "Feita: " + orStr(r.Title, id), nil
	}
	return "", fmt.Errorf("ferramenta desconhecida %s", tool)
}

// ── Contacts ────────────────────────────────────────────────────

func (g *Google) contacts(ctx context.Context, env *Env, a Args) (string, error) {
	q, err := a.Need("busca")
	if err != nil {
		return "", err
	}
	type person struct {
		Names []struct {
			Display string `json:"displayName"`
		} `json:"names"`
		Emails []struct {
			Value string `json:"value"`
		} `json:"emailAddresses"`
		Phones []struct {
			Value string `json:"value"`
		} `json:"phoneNumbers"`
		Orgs []struct {
			Name  string `json:"name"`
			Title string `json:"title"`
		} `json:"organizations"`
		Birthdays []struct {
			Date struct {
				Year, Month, Day int
			} `json:"date"`
		} `json:"birthdays"`
	}
	var r struct {
		Results []struct {
			Person person `json:"person"`
		} `json:"results"`
	}
	search := func(query string) error {
		v := url.Values{"query": {query}, "pageSize": {"20"}, "readMask": {"names,emailAddresses,phoneNumbers,organizations,birthdays"}}
		return g.api(ctx, env, request{URL: g.url(peopleHost, "/v1/people:searchContacts?"+v.Encode())}, &r)
	}
	if err := search(q); err != nil {
		return "", err
	}
	if len(r.Results) == 0 {
		// The search index warms up on the first, empty query (Google's docs).
		_ = search("")
		r.Results = nil
		if err := search(q); err != nil {
			return "", err
		}
	}
	if len(r.Results) == 0 {
		return "Nenhum contato com " + q + ".", nil
	}
	lines := []string{}
	for _, res := range r.Results {
		p := res.Person
		parts := []string{}
		if len(p.Names) > 0 {
			parts = append(parts, p.Names[0].Display)
		}
		for _, e := range p.Emails {
			parts = append(parts, e.Value)
		}
		for _, ph := range p.Phones {
			parts = append(parts, ph.Value)
		}
		for _, o := range p.Orgs {
			parts = append(parts, strings.TrimSpace(o.Title+" "+o.Name))
		}
		for _, b := range p.Birthdays {
			if b.Date.Day > 0 {
				parts = append(parts, fmt.Sprintf("aniversário %02d/%02d", b.Date.Day, b.Date.Month))
			}
		}
		lines = append(lines, "- "+strings.Join(parts, " · "))
	}
	return strings.Join(lines, "\n"), nil
}

// ── YouTube ─────────────────────────────────────────────────────

type ytVideo struct {
	ID      string `json:"id"`
	Snippet struct {
		Title        string `json:"title"`
		ChannelTitle string `json:"channelTitle"`
		PublishedAt  string `json:"publishedAt"`
	} `json:"snippet"`
	Details struct {
		Duration string `json:"duration"`
	} `json:"contentDetails"`
	Stats struct {
		Views    string `json:"viewCount"`
		Likes    string `json:"likeCount"`
		Comments string `json:"commentCount"`
	} `json:"statistics"`
}

var isoDuration = regexp.MustCompile(`^PT(?:(\d+)H)?(?:(\d+)M)?(?:(\d+)S)?$`)

func ytDuration(s string) string {
	m := isoDuration.FindStringSubmatch(s)
	if m == nil {
		return s
	}
	n := func(x string) int {
		v := 0
		fmt.Sscan(x, &v)
		return v
	}
	if m[1] != "" {
		return fmt.Sprintf("%d:%02d:%02d", n(m[1]), n(m[2]), n(m[3]))
	}
	return fmt.Sprintf("%d:%02d", n(m[2]), n(m[3]))
}

func (g *Google) videos(ctx context.Context, env *Env, ids []string) ([]ytVideo, error) {
	if len(ids) == 0 {
		return nil, nil
	}
	var r struct {
		Items []ytVideo `json:"items"`
	}
	v := url.Values{"part": {"snippet,contentDetails,statistics"}, "id": {strings.Join(ids, ",")}}
	err := g.api(ctx, env, request{URL: g.url(apisHost, "/youtube/v3/videos?"+v.Encode())}, &r)
	return r.Items, err
}

func videoLine(v ytVideo, channel bool) string {
	line := "- " + v.Snippet.Title
	if channel {
		line += " · " + v.Snippet.ChannelTitle
	}
	if t, err := time.Parse(time.RFC3339, v.Snippet.PublishedAt); err == nil {
		line += " · " + t.Format("02/01/2006")
	}
	if v.Details.Duration != "" {
		line += " · " + ytDuration(v.Details.Duration)
	}
	line += " · " + orStr(v.Stats.Views, "?") + " views"
	if !channel {
		line += fmt.Sprintf(", %s curtidas, %s comentários", orStr(v.Stats.Likes, "0"), orStr(v.Stats.Comments, "0"))
	}
	return line + " · https://youtu.be/" + v.ID
}

func (g *Google) youtube(ctx context.Context, env *Env, tool string, a Args) (string, error) {
	switch tool {
	case "youtube_buscar":
		q, err := a.Need("busca")
		if err != nil {
			return "", err
		}
		var r struct {
			Items []struct {
				ID struct {
					VideoID string `json:"videoId"`
				} `json:"id"`
			} `json:"items"`
		}
		v := url.Values{"part": {"id"}, "type": {"video"}, "q": {q}, "maxResults": {fmt.Sprint(a.Int("quantidade", 8, 1, 25))},
			"regionCode": {"BR"}, "relevanceLanguage": {"pt"}}
		if err := g.api(ctx, env, request{URL: g.url(apisHost, "/youtube/v3/search?"+v.Encode())}, &r); err != nil {
			return "", err
		}
		ids := []string{}
		for _, it := range r.Items {
			if it.ID.VideoID != "" {
				ids = append(ids, it.ID.VideoID)
			}
		}
		vids, err := g.videos(ctx, env, ids)
		if err != nil {
			return "", err
		}
		if len(vids) == 0 {
			return "Nenhum vídeo para " + q + ".", nil
		}
		lines := []string{}
		for _, vd := range vids {
			lines = append(lines, videoLine(vd, true))
		}
		return strings.Join(lines, "\n"), nil

	case "youtube_meu_canal":
		var ch struct {
			Items []struct {
				Snippet struct {
					Title string `json:"title"`
				} `json:"snippet"`
				Stats struct {
					Subs   string `json:"subscriberCount"`
					Views  string `json:"viewCount"`
					Videos string `json:"videoCount"`
				} `json:"statistics"`
				Details struct {
					Related struct {
						Uploads string `json:"uploads"`
					} `json:"relatedPlaylists"`
				} `json:"contentDetails"`
			} `json:"items"`
		}
		if err := g.api(ctx, env, request{URL: g.url(apisHost, "/youtube/v3/channels?part=snippet,statistics,contentDetails&mine=true")}, &ch); err != nil {
			return "", err
		}
		if len(ch.Items) == 0 {
			return "Essa conta Google não tem canal no YouTube.", nil
		}
		c := ch.Items[0]
		head := fmt.Sprintf("Canal «%s»: %s inscritos, %s visualizações, %s vídeos.", c.Snippet.Title, orStr(c.Stats.Subs, "?"), c.Stats.Views, c.Stats.Videos)
		if c.Details.Related.Uploads == "" {
			return head, nil
		}
		var pl struct {
			Items []struct {
				Details struct {
					VideoID string `json:"videoId"`
				} `json:"contentDetails"`
			} `json:"items"`
		}
		v := url.Values{"part": {"contentDetails"}, "playlistId": {c.Details.Related.Uploads}, "maxResults": {fmt.Sprint(a.Int("quantidade", 10, 1, 50))}}
		if err := g.api(ctx, env, request{URL: g.url(apisHost, "/youtube/v3/playlistItems?"+v.Encode())}, &pl); err != nil {
			return head, nil
		}
		ids := []string{}
		for _, it := range pl.Items {
			ids = append(ids, it.Details.VideoID)
		}
		vids, err := g.videos(ctx, env, ids)
		if err != nil || len(vids) == 0 {
			return head, nil
		}
		lines := []string{head, "", "Últimos vídeos:"}
		for _, vd := range vids {
			lines = append(lines, videoLine(vd, false))
		}
		return strings.Join(lines, "\n"), nil
	}
	return "", fmt.Errorf("ferramenta desconhecida %s", tool)
}
