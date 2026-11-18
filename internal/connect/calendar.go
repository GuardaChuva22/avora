package connect

import (
	"context"
	"fmt"
	"net/url"
	"strings"
	"time"
)

func (g *Google) calendarTools() []Tool {
	return []Tool{
		{Name: "agenda_eventos", Risk: Read,
			Description: "Lista os compromissos da Agenda Google do dono num período (padrão: hoje e os próximos 7 dias), com busca opcional.",
			Params: `{"type": "object", "properties": {
	"de": {"type": "string", "description": "AAAA-MM-DD; padrão hoje."},
	"ate": {"type": "string", "description": "AAAA-MM-DD, inclusive; padrão de + 7 dias."},
	"busca": {"type": "string"}}}`,
			Summary: func(a Args) string { return "agenda" + changes(a, "de", "ate") }},
		{Name: "agenda_criar", Risk: Write,
			RiskOf: func(a Args) Risk {
				if len(a.List("convidados")) > 0 {
					return Send // invitations go out
				}
				return Write
			},
			Description: "Marca um compromisso na Agenda Google do dono. inicio/fim em AAAA-MM-DD HH:MM (Brasília) ou só AAAA-MM-DD para o dia todo; fim padrão = 1 hora depois. Com convidados, eles recebem convite e o dono aprova antes.",
			Params: `{"type": "object", "properties": {
	"titulo": {"type": "string"},
	"inicio": {"type": "string"},
	"fim": {"type": "string"},
	"descricao": {"type": "string"},
	"local": {"type": "string"},
	"convidados": {"type": "array", "items": {"type": "string"}, "description": "E-mails."}
}, "required": ["titulo", "inicio"]}`,
			Summary: func(a Args) string {
				s := "Marcar «" + cut(a.Str("titulo"), 100) + "» em " + a.Str("inicio")
				if g := a.List("convidados"); len(g) > 0 {
					s += " e convidar " + strings.Join(g, ", ")
				}
				return s
			}},
	}
}

type calTime struct {
	DateTime string `json:"dateTime,omitempty"`
	Date     string `json:"date,omitempty"`
	TimeZone string `json:"timeZone,omitempty"`
}

func (c calTime) text(loc *time.Location) string {
	if c.Date != "" {
		if t, err := time.Parse("2006-01-02", c.Date); err == nil {
			return t.Format("02/01") + " (dia todo)"
		}
		return c.Date
	}
	if t, err := time.Parse(time.RFC3339, c.DateTime); err == nil {
		return t.In(loc).Format("02/01 15:04")
	}
	return c.DateTime
}

func (g *Google) calendar(ctx context.Context, env *Env, tool string, a Args) (string, error) {
	loc := env.zone()
	events := g.url(apisHost, "/calendar/v3/calendars/primary/events")
	switch tool {
	case "agenda_eventos":
		now := env.now().In(loc)
		from := time.Date(now.Year(), now.Month(), now.Day(), 0, 0, 0, 0, loc)
		if s := a.Str("de"); s != "" {
			t, _, err := parseWhen(s, loc)
			if err != nil {
				return "", err
			}
			from = t
		}
		to := from.AddDate(0, 0, 8)
		if s := a.Str("ate"); s != "" {
			t, allDay, err := parseWhen(s, loc)
			if err != nil {
				return "", err
			}
			to = t
			if allDay {
				to = t.AddDate(0, 0, 1)
			}
		}
		q := url.Values{"timeMin": {from.Format(time.RFC3339)}, "timeMax": {to.Format(time.RFC3339)},
			"singleEvents": {"true"}, "orderBy": {"startTime"}, "maxResults": {"100"}}
		if s := a.Str("busca"); s != "" {
			q.Set("q", s)
		}
		var r struct {
			Items []struct {
				ID        string  `json:"id"`
				Summary   string  `json:"summary"`
				Location  string  `json:"location"`
				Start     calTime `json:"start"`
				End       calTime `json:"end"`
				Attendees []struct {
					Email  string `json:"email"`
					Status string `json:"responseStatus"`
				} `json:"attendees"`
			} `json:"items"`
		}
		if err := g.api(ctx, env, request{URL: events + "?" + q.Encode()}, &r); err != nil {
			return "", err
		}
		if len(r.Items) == 0 {
			return fmt.Sprintf("Nada na agenda de %s a %s.", from.Format("02/01"), to.Add(-time.Second).Format("02/01")), nil
		}
		lines := []string{}
		for _, e := range r.Items {
			line := fmt.Sprintf("%s → %s · %s", e.Start.text(loc), e.End.text(loc), orStr(e.Summary, "(sem título)"))
			if e.Location != "" {
				line += " · " + e.Location
			}
			if len(e.Attendees) > 0 {
				line += fmt.Sprintf(" · %d convidados", len(e.Attendees))
			}
			lines = append(lines, line+" [id "+e.ID+"]")
		}
		return strings.Join(lines, "\n"), nil

	case "agenda_criar":
		title, err := a.Need("titulo")
		if err != nil {
			return "", err
		}
		startS, err := a.Need("inicio")
		if err != nil {
			return "", err
		}
		start, allDay, err := parseWhen(startS, loc)
		if err != nil {
			return "", err
		}
		end := start.Add(time.Hour)
		if allDay {
			end = start.AddDate(0, 0, 1)
		}
		if s := a.Str("fim"); s != "" {
			e, eAll, err := parseWhen(s, loc)
			if err != nil {
				return "", err
			}
			end = e
			if eAll {
				end = e.AddDate(0, 0, 1)
			}
		}
		if !end.After(start) {
			return "", fmt.Errorf("o fim precisa ser depois do início")
		}
		tz := loc.String()
		ev := map[string]any{"summary": title}
		if allDay {
			ev["start"], ev["end"] = calTime{Date: start.Format("2006-01-02")}, calTime{Date: end.Format("2006-01-02")}
		} else {
			ev["start"], ev["end"] = calTime{DateTime: start.Format(time.RFC3339), TimeZone: tz}, calTime{DateTime: end.Format(time.RFC3339), TimeZone: tz}
		}
		if s := a.Str("descricao"); s != "" {
			ev["description"] = s
		}
		if s := a.Str("local"); s != "" {
			ev["location"] = s
		}
		guests := a.List("convidados")
		send := "none"
		if len(guests) > 0 {
			list := []map[string]string{}
			for _, e := range guests {
				list = append(list, map[string]string{"email": e})
			}
			ev["attendees"], send = list, "all"
		}
		var out struct {
			ID       string `json:"id"`
			HTMLLink string `json:"htmlLink"`
		}
		if err := g.api(ctx, env, request{Method: "POST", URL: events + "?sendUpdates=" + send, Body: ev}, &out); err != nil {
			return "", err
		}
		msg := fmt.Sprintf("Marcado: %s em %s (id %s)\n%s", title, calTime{DateTime: start.Format(time.RFC3339)}.text(loc), out.ID, out.HTMLLink)
		if allDay {
			msg = fmt.Sprintf("Marcado: %s, dia todo em %s (id %s)\n%s", title, start.Format("02/01"), out.ID, out.HTMLLink)
		}
		if len(guests) > 0 {
			msg += "\nConvites enviados para " + strings.Join(guests, ", ") + "."
		}
		return msg, nil
	}
	return "", fmt.Errorf("ferramenta desconhecida %s", tool)
}
