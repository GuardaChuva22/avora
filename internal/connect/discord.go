package connect

import (
	"context"
	"fmt"
	"net/url"
	"sort"
	"strings"
	"time"
)

// Discord, through the owner's own bot (a Discord application with a bot,
// invited to their servers). It reads and posts where the bot was invited.
type Discord struct {
	BaseURL string
}

// MaxDiscordMessage is Discord's limit for one message.
const MaxDiscordMessage = 2000

func (d *Discord) base() string {
	if d.BaseURL != "" {
		return strings.TrimRight(d.BaseURL, "/")
	}
	return "https://discord.com/api/v10"
}

func (d *Discord) Info() Info {
	return Info{Kind: "discord", Name: "Discord", Ready: true, Category: "Com chave ou token", Auth: "token", Apps: []string{"Discord"},
		Description: "Um bot seu que lê e posta nos seus servidores.",
		Help: "Em discord.com/developers/applications: New Application → Bot → Reset Token (copie o token) e ligue Message Content Intent. " +
			"Depois em OAuth2 → URL Generator marque bot com View Channels, Read Message History e Send Messages, abra a URL e convide o bot pro seu servidor.",
		HelpURL: "https://discord.com/developers/applications"}
}

func (d *Discord) Tools() []Tool {
	return []Tool{
		{Name: "discord_canais", Risk: Read,
			Description: "Lista os servidores do Discord em que o bot do dono está e os canais de texto de cada um, com os ids.",
			Params:      `{"type": "object", "properties": {}}`,
			Summary:     func(Args) string { return "canais do Discord" }},
		{Name: "discord_ler", Risk: Read,
			Description: "Lê as últimas mensagens de um canal do Discord (da mais antiga para a mais nova).",
			Params: `{"type": "object", "properties": {
	"canal_id": {"type": "string"},
	"quantidade": {"type": "integer", "description": "Padrão 30, até 100."}
}, "required": ["canal_id"]}`,
			Summary: func(a Args) string { return "leu o canal " + a.Str("canal_id") + " do Discord" }},
		{Name: "discord_enviar", Risk: Send,
			Description: "Posta uma mensagem num canal do Discord pelo bot do dono (até 2000 caracteres; menções a @everyone e cargos ficam desligadas). Pede aprovação.",
			Params: `{"type": "object", "properties": {
	"canal_id": {"type": "string"},
	"mensagem": {"type": "string"}
}, "required": ["canal_id", "mensagem"]}`,
			Summary: func(a Args) string {
				return "Postar no canal " + a.Str("canal_id") + " do Discord: «" + cut(a.Str("mensagem"), 400) + "»"
			}},
	}
}

func (d *Discord) req(ctx context.Context, env *Env, method, path string, body, out any) error {
	return callJSON(ctx, request{Method: method, URL: d.base() + path, Body: body,
		Header: map[string]string{"Authorization": "Bot " + env.Cred.Token}}, out)
}

func (d *Discord) Check(ctx context.Context, env *Env) (string, error) {
	var me struct {
		Username string `json:"username"`
	}
	if err := d.req(ctx, env, "GET", "/users/@me", nil, &me); err != nil {
		return "", err
	}
	return "bot " + me.Username, nil
}

type discordChannel struct {
	ID       string `json:"id"`
	Name     string `json:"name"`
	Type     int    `json:"type"`
	Position int    `json:"position"`
}

func (d *Discord) Call(ctx context.Context, env *Env, tool string, a Args) (string, error) {
	switch tool {
	case "discord_canais":
		var guilds []struct {
			ID   string `json:"id"`
			Name string `json:"name"`
		}
		if err := d.req(ctx, env, "GET", "/users/@me/guilds", nil, &guilds); err != nil {
			return "", err
		}
		if len(guilds) == 0 {
			return "O bot não está em nenhum servidor; convide-o pela URL do OAuth2 do app no Discord.", nil
		}
		var b strings.Builder
		for _, g := range guilds {
			fmt.Fprintf(&b, "Servidor %s (%s)\n", g.Name, g.ID)
			var chans []discordChannel
			if err := d.req(ctx, env, "GET", "/guilds/"+g.ID+"/channels", nil, &chans); err != nil {
				fmt.Fprintf(&b, "  (sem acesso aos canais: %v)\n", err)
				continue
			}
			sort.Slice(chans, func(i, j int) bool { return chans[i].Position < chans[j].Position })
			for _, c := range chans {
				if c.Type == 0 || c.Type == 5 { // text, announcement
					fmt.Fprintf(&b, "  #%s (canal_id %s)\n", c.Name, c.ID)
				}
			}
		}
		return b.String(), nil

	case "discord_ler":
		ch, err := a.Need("canal_id")
		if err != nil {
			return "", err
		}
		n := a.Int("quantidade", 30, 1, 100)
		var msgs []struct {
			Content   string    `json:"content"`
			Timestamp time.Time `json:"timestamp"`
			Author    struct {
				Username   string `json:"username"`
				GlobalName string `json:"global_name"`
				Bot        bool   `json:"bot"`
			} `json:"author"`
			Attachments []struct {
				Filename string `json:"filename"`
			} `json:"attachments"`
		}
		if err := d.req(ctx, env, "GET", fmt.Sprintf("/channels/%s/messages?limit=%d", url.PathEscape(ch), n), nil, &msgs); err != nil {
			return "", err
		}
		if len(msgs) == 0 {
			return "O canal não tem mensagens.", nil
		}
		lines := []string{}
		for i := len(msgs) - 1; i >= 0; i-- {
			m := msgs[i]
			who := orStr(m.Author.GlobalName, m.Author.Username)
			if m.Author.Bot {
				who += " (bot)"
			}
			text := m.Content
			for _, at := range m.Attachments {
				text += " [anexo " + at.Filename + "]"
			}
			if strings.TrimSpace(text) == "" {
				text = "(sem texto; ligue Message Content Intent no bot para ler o conteúdo)"
			}
			lines = append(lines, fmt.Sprintf("%s %s: %s", m.Timestamp.In(env.zone()).Format("02/01 15:04"), who, cut(text, 800)))
		}
		return strings.Join(lines, "\n"), nil

	case "discord_enviar":
		ch, err := a.Need("canal_id")
		if err != nil {
			return "", err
		}
		text, err := a.Need("mensagem")
		if err != nil {
			return "", err
		}
		if n := len([]rune(text)); n > MaxDiscordMessage {
			return "", fmt.Errorf("mensagem grande demais (%d caracteres, máx. %d)", n, MaxDiscordMessage)
		}
		var m struct {
			ID string `json:"id"`
		}
		body := map[string]any{"content": text, "allowed_mentions": map[string]any{"parse": []string{"users"}}}
		if err := d.req(ctx, env, "POST", "/channels/"+url.PathEscape(ch)+"/messages", body, &m); err != nil {
			return "", err
		}
		return "Mensagem postada (id " + m.ID + ").", nil
	}
	return "", fmt.Errorf("ferramenta desconhecida %s", tool)
}
