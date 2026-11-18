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

// ElevenLabs: voices and text to speech with the owner's API key. Audio is
// saved as an MP3 in the agent's folder.
type ElevenLabs struct {
	BaseURL string
}

const elevenModel = "eleven_multilingual_v2"

// MaxSpeech bounds one text-to-speech call (credits are per character).
const MaxSpeech = 5000

var voiceID = regexp.MustCompile(`^[A-Za-z0-9]{20}$`)

func (e *ElevenLabs) base() string {
	if e.BaseURL != "" {
		return strings.TrimRight(e.BaseURL, "/")
	}
	return "https://api.elevenlabs.io"
}

func (e *ElevenLabs) Info() Info {
	return Info{Kind: "elevenlabs", Name: "ElevenLabs", Ready: true, Category: "Com chave ou token", Auth: "token", Apps: []string{"ElevenLabs"},
		Description: "Vozes e texto para fala: os agentes geram áudios em MP3 na própria pasta.",
		Help:        "Em elevenlabs.io: Developers → API Keys → Create API Key, com acesso a Text to Speech, Speech to Text, Voices e User (Speech to Text transcreve os áudios do WhatsApp). Cole a chave (sk_...).",
		HelpURL:     "https://elevenlabs.io/app/developers/api-keys"}
}

func (e *ElevenLabs) Tools() []Tool {
	return []Tool{
		{Name: "elevenlabs_vozes", Risk: Read,
			Description: "Lista as vozes do ElevenLabs disponíveis ao dono (id, nome, sotaque, estilo). Busca opcional por nome ou estilo.",
			Params:      `{"type": "object", "properties": {"busca": {"type": "string"}}}`,
			Summary:     func(a Args) string { return "vozes do ElevenLabs" + quoted(a.Str("busca")) }},
		{Name: "elevenlabs_falar", Risk: Write,
			Description: "Transforma um texto em fala (MP3) com uma voz do ElevenLabs e salva na sua pasta, em audio/. Gasta créditos do dono: 1 por caractere, até 5000 caracteres por vez.",
			Params: `{"type": "object", "properties": {
	"texto": {"type": "string"},
	"voz": {"type": "string", "description": "Id ou nome da voz; vazio = a primeira da conta."},
	"arquivo": {"type": "string", "description": "Nome do arquivo, ex.: boas-vindas.mp3."}
}, "required": ["texto"]}`,
			Summary: func(a Args) string {
				return fmt.Sprintf("gerou áudio (%d caracteres)%s", len([]rune(a.Str("texto"))), quoted(a.Str("voz")))
			}},
		{Name: "elevenlabs_uso", Risk: Read,
			Description: "Mostra o plano do ElevenLabs e quantos caracteres (créditos) já foram usados no mês.",
			Params:      `{"type": "object", "properties": {}}`,
			Summary:     func(Args) string { return "uso do ElevenLabs" }},
	}
}

func (e *ElevenLabs) header(env *Env) map[string]string {
	return map[string]string{"xi-api-key": env.Cred.Token}
}

type elevenSub struct {
	Tier       string `json:"tier"`
	Used       int    `json:"character_count"`
	Limit      int    `json:"character_limit"`
	ResetsUnix int64  `json:"next_character_count_reset_unix"`
}

type elevenUser struct {
	UserID       string    `json:"user_id"`
	FirstName    string    `json:"first_name"`
	Subscription elevenSub `json:"subscription"`
}

func (e *ElevenLabs) subscription(ctx context.Context, env *Env) (elevenSub, error) {
	var s elevenSub
	err := callJSON(ctx, request{URL: e.base() + "/v1/user/subscription", Header: e.header(env)}, &s)
	return s, err
}

// tail is the last four characters of an id, to tell accounts apart.
func tail(id string) string {
	r := []rune(strings.TrimSpace(id))
	if len(r) > 4 {
		r = r[len(r)-4:]
	}
	return string(r)
}

// Check names the account by its user, never by its plan: two accounts on
// the same plan are still two accounts.
func (e *ElevenLabs) Check(ctx context.Context, env *Env) (string, error) {
	var u elevenUser
	err := callJSON(ctx, request{URL: e.base() + "/v1/user", Header: e.header(env)}, &u)
	if err == nil && u.UserID != "" {
		name := strings.TrimSpace(u.FirstName)
		if name == "" {
			name = "conta"
		}
		return name + " · …" + tail(u.UserID), nil
	}
	// A key without the User permission can still speak; it is named by
	// the end of the key itself.
	if _, verr := e.voices(ctx, env, ""); verr == nil {
		return "chave …" + tail(env.Cred.Token), nil
	}
	if err == nil {
		err = errors.New("a chave não respondeu")
	}
	return "", err
}

// Usage reads how many credits (characters) the account has left.
func (e *ElevenLabs) Usage(ctx context.Context, env *Env) (Usage, error) {
	s, err := e.subscription(ctx, env)
	if err != nil {
		return Usage{}, err
	}
	return Usage{Plan: s.Tier, Used: int64(s.Used), Limit: int64(s.Limit), Unit: "créditos", ResetsAt: s.ResetsUnix}, nil
}

type elevenVoice struct {
	ID          string            `json:"voice_id"`
	Name        string            `json:"name"`
	Category    string            `json:"category"`
	Description string            `json:"description"`
	Labels      map[string]string `json:"labels"`
}

func (e *ElevenLabs) voices(ctx context.Context, env *Env, search string) ([]elevenVoice, error) {
	q := url.Values{"page_size": {"50"}}
	if search != "" {
		q.Set("search", search)
	}
	var r struct {
		Voices []elevenVoice `json:"voices"`
	}
	if err := callJSON(ctx, request{URL: e.base() + "/v2/voices?" + q.Encode(), Header: e.header(env)}, &r); err != nil {
		return nil, err
	}
	return r.Voices, nil
}

func (e *ElevenLabs) Call(ctx context.Context, env *Env, tool string, a Args) (string, error) {
	switch tool {
	case "elevenlabs_vozes":
		vs, err := e.voices(ctx, env, a.Str("busca"))
		if err != nil {
			return "", err
		}
		if len(vs) == 0 {
			return "Nenhuma voz encontrada.", nil
		}
		lines := []string{}
		for _, v := range vs {
			tags := []string{}
			for _, k := range []string{"gender", "age", "accent", "description", "use_case"} {
				if v.Labels[k] != "" {
					tags = append(tags, v.Labels[k])
				}
			}
			lines = append(lines, fmt.Sprintf("%s (%s) · %s · %s", v.Name, v.ID, v.Category, strings.Join(tags, ", ")))
		}
		return strings.Join(lines, "\n"), nil

	case "elevenlabs_falar":
		text, err := a.Need("texto")
		if err != nil {
			return "", err
		}
		if n := len([]rune(text)); n > MaxSpeech {
			return "", fmt.Errorf("texto grande demais (%d caracteres, máx. %d por vez)", n, MaxSpeech)
		}
		if env.Files == nil {
			return "", errors.New("este servidor não tem as pastas dos agentes para salvar o áudio")
		}
		voice, name := a.Str("voz"), ""
		if !voiceID.MatchString(voice) {
			vs, err := e.voices(ctx, env, voice)
			if err != nil {
				return "", err
			}
			if len(vs) == 0 {
				return "", fmt.Errorf("não achei a voz %q; veja elevenlabs_vozes", voice)
			}
			voice, name = vs[0].ID, vs[0].Name
		}
		audio, _, err := do(ctx, request{Method: "POST",
			URL:    e.base() + "/v1/text-to-speech/" + url.PathEscape(voice) + "?output_format=mp3_44100_128",
			Header: map[string]string{"xi-api-key": env.Cred.Token, "Accept": "audio/mpeg"},
			Body:   map[string]any{"text": text, "model_id": elevenModel}})
		if err != nil {
			return "", err
		}
		file := strings.TrimSpace(a.Str("arquivo"))
		if file == "" {
			file = "fala-" + env.now().In(env.zone()).Format("2006-01-02-150405") + ".mp3"
		}
		if !strings.HasSuffix(strings.ToLower(file), ".mp3") {
			file += ".mp3"
		}
		path, err := env.Files.Save("audio/"+strings.TrimPrefix(file, "audio/"), audio)
		if err != nil {
			return "", err
		}
		who := voice
		if name != "" {
			who = name
		}
		return fmt.Sprintf("Áudio salvo em %s (%d KB, voz %s, %d caracteres).", path, len(audio)>>10, who, len([]rune(text))), nil

	case "elevenlabs_uso":
		s, err := e.subscription(ctx, env)
		if err != nil {
			return "", err
		}
		out := fmt.Sprintf("Plano %s: %d de %d caracteres usados (%d restantes).", s.Tier, s.Used, s.Limit, max(0, s.Limit-s.Used))
		if s.ResetsUnix > 0 {
			out += " Renova em " + time.Unix(s.ResetsUnix, 0).In(env.zone()).Format("02/01/2006") + "."
		}
		return out, nil
	}
	return "", fmt.Errorf("ferramenta desconhecida %s", tool)
}
