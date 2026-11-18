package control

import (
	"encoding/json"
	"errors"
	"strings"
)

// StepAsk is a question the agent puts to the owner with options to pick:
// Summary holds the question, Detail the options as JSON (askDetail). The
// app draws it as a card under the answer; the owner's pick comes back as
// the next message.
const StepAsk = "perguntar"

const (
	maxAskOptions = 8
	maxAskOption  = 140
)

type askDetail struct {
	Options  []string `json:"opcoes"`
	Multiple bool     `json:"multipla,omitempty"`
	Other    bool     `json:"outra,omitempty"`
}

var askParams = `{"type": "object", "properties": {
	"pergunta": {"type": "string", "description": "A pergunta, curta e direta."},
	"opcoes": {"type": "array", "items": {"type": "string"}, "description": "De 2 a 8 respostas prontas, curtas (até umas 8 palavras cada), na ordem em que você recomenda."},
	"multipla": {"type": "boolean", "description": "true se o dono pode marcar mais de uma."},
	"outra": {"type": "boolean", "description": "true para deixar o dono escrever outra resposta."}
}, "required": ["pergunta", "opcoes"]}`

const askToolDesc = "Faz uma pergunta ao dono com respostas prontas para ele clicar (múltipla escolha), em vez de escrever as opções no texto. Use quando você precisa de uma decisão dele para seguir e as respostas possíveis são poucas e claras (qual opção, sim/não, que formato). Depois, termine a resposta em uma frase curta sem repetir as opções: a escolha dele chega como a próxima mensagem."

// ask records the question as a step for the app to draw.
func (s *Server) ask(t *turn, a toolArgs) (string, error) {
	q := strings.TrimSpace(a.Pergunta)
	if q == "" {
		return "", errors.New("falta a pergunta")
	}
	opts := []string{}
	for _, o := range a.Opcoes {
		o = strings.TrimSpace(strings.ReplaceAll(o, "\n", " "))
		if o == "" {
			continue
		}
		if r := []rune(o); len(r) > maxAskOption {
			o = string(r[:maxAskOption-1]) + "…"
		}
		opts = append(opts, o)
		if len(opts) == maxAskOptions {
			break
		}
	}
	if len(opts) < 2 && !a.Outra {
		return "", errors.New("dê pelo menos duas opções")
	}
	detail, _ := json.Marshal(askDetail{Options: opts, Multiple: a.Multipla, Other: a.Outra})
	s.step(t, AgentStep{Tool: StepAsk, Summary: truncate(q, 300), Detail: string(detail)})
	return "Pergunta mostrada ao dono, com as opções para clicar. Termine a resposta agora em no máximo uma frase curta, sem repetir as opções; a escolha dele chega como a próxima mensagem.", nil
}
