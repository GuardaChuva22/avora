package connect

import (
	"context"
	"encoding/json"
	"fmt"
	"net/url"
	"strconv"
	"strings"
	"time"
)

// ClickUp, through its REST API v2 with the owner's personal token
// (Settings → Apps → API Token). The token acts as the owner, so tasks and
// comments show up under their name.
type ClickUp struct {
	BaseURL string // tests point it at a fake
}

func (c *ClickUp) base() string {
	if c.BaseURL != "" {
		return strings.TrimRight(c.BaseURL, "/")
	}
	return "https://api.clickup.com/api/v2"
}

func (c *ClickUp) Info() Info {
	return Info{Kind: "clickup-token", Name: "ClickUp (token)", Ready: true, Category: "Com chave ou token", Auth: "token", Apps: []string{"ClickUp"},
		Description: "Plano B, se o login de um clique do ClickUp não funcionar: tarefas, listas e comentários pelo token pessoal.",
		Help:        "No ClickUp: avatar → Settings → Apps → API Token → Generate. Cole o token que começa com pk_.",
		HelpURL:     "https://app.clickup.com/settings/apps"}
}

func (c *ClickUp) Tools() []Tool {
	return []Tool{
		{Name: "clickup_espacos", Risk: Read,
			Description: "Mostra a estrutura do ClickUp do dono: workspaces, espaços, pastas e listas, com os ids. Use para achar a lista certa antes de criar tarefas.",
			Params:      `{"type": "object", "properties": {}}`,
			Summary:     func(Args) string { return "olhou os espaços do ClickUp" }},
		{Name: "clickup_tarefas", Risk: Read,
			Description: "Lista tarefas do ClickUp: de uma lista (lista_id) ou do workspace todo, filtrando por texto no nome. Mostra id, status, prazo, responsáveis e lista.",
			Params: `{"type": "object", "properties": {
	"lista_id": {"type": "string", "description": "Id de uma lista; vazio = o workspace todo."},
	"busca": {"type": "string", "description": "Texto que o nome da tarefa deve conter."},
	"etiqueta": {"type": "string", "description": "Só tarefas com esta etiqueta (tag), ex.: avora."},
	"incluir_fechadas": {"type": "boolean"},
	"quantidade": {"type": "integer", "description": "Máximo de tarefas (padrão 30, até 100)."}}}`,
			Summary: func(a Args) string { return "tarefas do ClickUp" + quoted(a.Str("busca")) }},
		{Name: "clickup_tarefa", Risk: Read,
			Description: "Lê uma tarefa do ClickUp por id: descrição, status, prazo, responsáveis e os últimos comentários.",
			Params:      `{"type": "object", "properties": {"id": {"type": "string"}}, "required": ["id"]}`,
			Summary:     func(a Args) string { return "leu a tarefa " + a.Str("id") }},
		{Name: "clickup_criar_tarefa", Risk: Write,
			Description: "Cria uma tarefa numa lista do ClickUp. Prazo em AAAA-MM-DD ou AAAA-MM-DD HH:MM (Brasília). Prioridade: urgente, alta, normal ou baixa.",
			Params: `{"type": "object", "properties": {
	"lista_id": {"type": "string"},
	"nome": {"type": "string"},
	"descricao": {"type": "string"},
	"prazo": {"type": "string"},
	"prioridade": {"type": "string", "enum": ["urgente", "alta", "normal", "baixa"]}
}, "required": ["lista_id", "nome"]}`,
			Summary: func(a Args) string { return "criou a tarefa «" + cut(a.Str("nome"), 120) + "»" }},
		{Name: "clickup_atualizar_tarefa", Risk: Write,
			Description: "Muda uma tarefa do ClickUp: só os campos enviados (nome, descricao, status, prazo, prioridade). Status com o nome exato da lista, ex.: 'complete'.",
			Params: `{"type": "object", "properties": {
	"id": {"type": "string"},
	"nome": {"type": "string"},
	"descricao": {"type": "string"},
	"status": {"type": "string"},
	"prazo": {"type": "string"},
	"prioridade": {"type": "string", "enum": ["urgente", "alta", "normal", "baixa"]}
}, "required": ["id"]}`,
			Summary: func(a Args) string { return "atualizou a tarefa " + a.Str("id") + changes(a, "status", "prazo") }},
		{Name: "clickup_comentar", Risk: Send,
			Description: "Comenta numa tarefa do ClickUp, em nome do dono (quem segue a tarefa vê). Pede aprovação.",
			Params: `{"type": "object", "properties": {
	"id": {"type": "string"},
	"comentario": {"type": "string"}
}, "required": ["id", "comentario"]}`,
			Summary: func(a Args) string {
				return "Comentar na tarefa " + a.Str("id") + " do ClickUp: «" + cut(a.Str("comentario"), 300) + "»"
			}},
	}
}

func quoted(s string) string {
	if s == "" {
		return ""
	}
	return " «" + cut(s, 60) + "»"
}

func changes(a Args, keys ...string) string {
	out := []string{}
	for _, k := range keys {
		if v := a.Str(k); v != "" {
			out = append(out, k+" "+cut(v, 40))
		}
	}
	if len(out) == 0 {
		return ""
	}
	return " (" + strings.Join(out, ", ") + ")"
}

func (c *ClickUp) req(ctx context.Context, env *Env, method, path string, body, out any) error {
	return callJSON(ctx, request{Method: method, URL: c.base() + path, Body: body,
		Header: map[string]string{"Authorization": env.Cred.Token}}, out)
}

func (c *ClickUp) Check(ctx context.Context, env *Env) (string, error) {
	var r struct {
		User struct {
			Username string `json:"username"`
			Email    string `json:"email"`
		} `json:"user"`
	}
	if err := c.req(ctx, env, "GET", "/user", nil, &r); err != nil {
		return "", err
	}
	return strings.TrimSpace(r.User.Username + " · " + r.User.Email), nil
}

type cuTask struct {
	ID          string `json:"id"`
	Name        string `json:"name"`
	TextContent string `json:"text_content"`
	Description string `json:"description"`
	Status      struct {
		Status string `json:"status"`
	} `json:"status"`
	DueDate  *string `json:"due_date"`
	Priority *struct {
		Priority string `json:"priority"`
	} `json:"priority"`
	Assignees []struct {
		Username string `json:"username"`
	} `json:"assignees"`
	List struct {
		ID   string `json:"id"`
		Name string `json:"name"`
	} `json:"list"`
	URL string `json:"url"`
}

var cuPriority = map[string]int{"urgente": 1, "urgent": 1, "alta": 2, "high": 2, "normal": 3, "baixa": 4, "low": 4}
var cuPriorityName = map[string]string{"urgent": "urgente", "high": "alta", "normal": "normal", "low": "baixa"}

func (t cuTask) line(loc *time.Location) string {
	parts := []string{"[" + t.ID + "] " + t.Name, t.Status.Status}
	if t.DueDate != nil {
		if ms, err := strconv.ParseInt(*t.DueDate, 10, 64); err == nil {
			parts = append(parts, "prazo "+time.UnixMilli(ms).In(loc).Format("02/01 15:04"))
		}
	}
	if t.Priority != nil {
		parts = append(parts, "prioridade "+cuPriorityName[t.Priority.Priority])
	}
	if len(t.Assignees) > 0 {
		names := []string{}
		for _, a := range t.Assignees {
			names = append(names, a.Username)
		}
		parts = append(parts, "com "+strings.Join(names, ", "))
	}
	if t.List.Name != "" {
		parts = append(parts, "lista "+t.List.Name+" ("+t.List.ID+")")
	}
	return strings.Join(parts, " · ")
}

func (c *ClickUp) teams(ctx context.Context, env *Env) ([]struct{ ID, Name string }, error) {
	var r struct {
		Teams []struct {
			ID   string `json:"id"`
			Name string `json:"name"`
		} `json:"teams"`
	}
	if err := c.req(ctx, env, "GET", "/team", nil, &r); err != nil {
		return nil, err
	}
	out := []struct{ ID, Name string }{}
	for _, t := range r.Teams {
		out = append(out, struct{ ID, Name string }{t.ID, t.Name})
	}
	return out, nil
}

func (c *ClickUp) Call(ctx context.Context, env *Env, tool string, a Args) (string, error) {
	loc := env.zone()
	switch tool {
	case "clickup_espacos":
		teams, err := c.teams(ctx, env)
		if err != nil {
			return "", err
		}
		var b strings.Builder
		type list struct {
			ID   string `json:"id"`
			Name string `json:"name"`
		}
		for _, t := range teams {
			fmt.Fprintf(&b, "Workspace %s (%s)\n", t.Name, t.ID)
			var spaces struct {
				Spaces []list `json:"spaces"`
			}
			if err := c.req(ctx, env, "GET", "/team/"+t.ID+"/space?archived=false", nil, &spaces); err != nil {
				return "", err
			}
			for _, sp := range spaces.Spaces {
				fmt.Fprintf(&b, "  Espaço %s (%s)\n", sp.Name, sp.ID)
				var folders struct {
					Folders []struct {
						list
						Lists []list `json:"lists"`
					} `json:"folders"`
				}
				if err := c.req(ctx, env, "GET", "/space/"+sp.ID+"/folder?archived=false", nil, &folders); err != nil {
					return "", err
				}
				for _, f := range folders.Folders {
					fmt.Fprintf(&b, "    Pasta %s (%s)\n", f.Name, f.ID)
					for _, l := range f.Lists {
						fmt.Fprintf(&b, "      Lista %s (lista_id %s)\n", l.Name, l.ID)
					}
				}
				var loose struct {
					Lists []list `json:"lists"`
				}
				if err := c.req(ctx, env, "GET", "/space/"+sp.ID+"/list?archived=false", nil, &loose); err != nil {
					return "", err
				}
				for _, l := range loose.Lists {
					fmt.Fprintf(&b, "    Lista %s (lista_id %s)\n", l.Name, l.ID)
				}
			}
		}
		if b.Len() == 0 {
			return "Nenhum workspace no ClickUp.", nil
		}
		return b.String(), nil

	case "clickup_tarefas":
		n := a.Int("quantidade", 30, 1, 100)
		q := url.Values{"subtasks": {"true"}, "include_closed": {strconv.FormatBool(a.Bool("incluir_fechadas"))}, "order_by": {"updated"}}
		if tag := strings.ToLower(strings.TrimSpace(a.Str("etiqueta"))); tag != "" {
			q.Add("tags[]", tag)
		}
		paths := []string{}
		if id := a.Str("lista_id"); id != "" {
			paths = append(paths, "/list/"+url.PathEscape(id)+"/task?")
		} else {
			teams, err := c.teams(ctx, env)
			if err != nil {
				return "", err
			}
			for _, t := range teams {
				paths = append(paths, "/team/"+t.ID+"/task?")
			}
		}
		needle := strings.ToLower(a.Str("busca"))
		lines := []string{}
		for _, p := range paths {
			for page := 0; page < 5 && len(lines) < n; page++ {
				q.Set("page", strconv.Itoa(page))
				var r struct {
					Tasks    []cuTask `json:"tasks"`
					LastPage bool     `json:"last_page"`
				}
				if err := c.req(ctx, env, "GET", p+q.Encode(), nil, &r); err != nil {
					return "", err
				}
				for _, t := range r.Tasks {
					if needle == "" || strings.Contains(strings.ToLower(t.Name), needle) {
						lines = append(lines, t.line(loc))
						if len(lines) >= n {
							break
						}
					}
				}
				if r.LastPage || len(r.Tasks) == 0 {
					break
				}
			}
		}
		if len(lines) == 0 {
			return "Nenhuma tarefa encontrada.", nil
		}
		return strings.Join(lines, "\n"), nil

	case "clickup_tarefa":
		id, err := a.Need("id")
		if err != nil {
			return "", err
		}
		var t cuTask
		if err := c.req(ctx, env, "GET", "/task/"+url.PathEscape(id), nil, &t); err != nil {
			return "", err
		}
		out := t.line(loc) + "\n" + t.URL
		if d := strings.TrimSpace(orStr(t.TextContent, t.Description)); d != "" {
			out += "\n\nDescrição:\n" + cut(d, 4000)
		}
		var cm struct {
			Comments []struct {
				Text string `json:"comment_text"`
				User struct {
					Username string `json:"username"`
				} `json:"user"`
				Date string `json:"date"`
			} `json:"comments"`
		}
		if err := c.req(ctx, env, "GET", "/task/"+url.PathEscape(id)+"/comment", nil, &cm); err == nil && len(cm.Comments) > 0 {
			out += "\n\nComentários (mais recentes primeiro):"
			for i, x := range cm.Comments {
				if i == 10 {
					break
				}
				when := ""
				if ms, err := strconv.ParseInt(x.Date, 10, 64); err == nil {
					when = time.UnixMilli(ms).In(loc).Format("02/01 15:04") + " "
				}
				out += fmt.Sprintf("\n- %s%s: %s", when, x.User.Username, cut(x.Text, 600))
			}
		}
		return out, nil

	case "clickup_criar_tarefa", "clickup_atualizar_tarefa":
		body := map[string]any{}
		if v := a.Str("nome"); v != "" {
			body["name"] = v
		}
		if v := a.Str("descricao"); v != "" {
			body["description"] = v
		}
		if v := a.Str("status"); v != "" {
			body["status"] = v
		}
		if v := a.Str("prioridade"); v != "" {
			p, ok := cuPriority[strings.ToLower(v)]
			if !ok {
				return "", fmt.Errorf("prioridade inválida %q", v)
			}
			body["priority"] = p
		}
		if v := a.Str("prazo"); v != "" {
			t, allDay, err := parseWhen(v, loc)
			if err != nil {
				return "", err
			}
			body["due_date"], body["due_date_time"] = t.UnixMilli(), !allDay
		}
		var t cuTask
		if tool == "clickup_criar_tarefa" {
			list, err := a.Need("lista_id")
			if err != nil {
				return "", err
			}
			if body["name"] == nil {
				return "", fmt.Errorf("falta o argumento nome")
			}
			if err := c.req(ctx, env, "POST", "/list/"+url.PathEscape(list)+"/task", body, &t); err != nil {
				return "", err
			}
			return "Tarefa criada: " + t.line(loc) + "\n" + t.URL, nil
		}
		id, err := a.Need("id")
		if err != nil {
			return "", err
		}
		if len(body) == 0 {
			return "", fmt.Errorf("nada para mudar")
		}
		if err := c.req(ctx, env, "PUT", "/task/"+url.PathEscape(id), body, &t); err != nil {
			return "", err
		}
		return "Tarefa atualizada: " + t.line(loc), nil

	case "clickup_comentar":
		id, err := a.Need("id")
		if err != nil {
			return "", err
		}
		text, err := a.Need("comentario")
		if err != nil {
			return "", err
		}
		var r json.RawMessage
		if err := c.req(ctx, env, "POST", "/task/"+url.PathEscape(id)+"/comment", map[string]any{"comment_text": text, "notify_all": false}, &r); err != nil {
			return "", err
		}
		return "Comentário publicado na tarefa " + id + ".", nil
	}
	return "", fmt.Errorf("ferramenta desconhecida %s", tool)
}

func orStr(a, b string) string {
	if strings.TrimSpace(a) != "" {
		return a
	}
	return b
}
