// Package engine holds Avora's own (native) agent engine. It runs inside the
// server and talks to an OpenAI-compatible model provider.
package engine

import (
	"context"
	"fmt"
	"strings"
	"time"

	"github.com/GuardaChuva22/avora/internal/llm"
	"github.com/GuardaChuva22/avora/internal/store"
)

// MaxHistory is how many past messages are sent with each request.
const MaxHistory = 40

type Native struct {
	provider   *llm.Provider
	model      string
	speed      string
	maxHistory int
	location   *time.Location
	now        func() time.Time
}

func NewNative(provider *llm.Provider, model string) *Native {
	loc, err := time.LoadLocation("America/Sao_Paulo")
	if err != nil {
		loc = time.FixedZone("BRT", -3*60*60)
	}
	return &Native{provider: provider, model: model, maxHistory: MaxHistory, location: loc, now: time.Now}
}

func (n *Native) WithSpeed(speed string) *Native {
	n.speed = speed
	return n
}

func (n *Native) WithContextTokens(tokens int) *Native {
	if tokens > 0 {
		if h := tokens / 2000; h > n.maxHistory {
			n.maxHistory = h
		}
	}
	return n
}

func (n *Native) Name() string     { return "native" }
func (n *Native) Model() string    { return n.model }
func (n *Native) Speed() string    { return n.speed }
func (n *Native) Provider() string { return n.provider.Name }

func (n *Native) systemPrompt(system string) string {
	now := n.now().In(n.location).Format("02/01/2006 15:04, Monday")
	if system != "" {
		return system + "\n\nAgora são " + now + " (horário de Brasília)."
	}
	return fmt.Sprintf(`Você é o Avora, o assistente pessoal e funcionário digital de uma única pessoa, o seu dono.
Responda em português do Brasil, de forma direta e útil, sem enrolação.
Agora são %s (horário de Brasília).
Você ainda não tem ferramentas: não consegue acessar arquivos, internet, agenda nem executar ações.
Nunca diga que fez algo que não fez. Quando o pedido exigir agir, explique o que faria e que essa capacidade ainda não está ligada.`,
		now)
}

// MaxRounds bounds the model calls of one turn; the last one gets no tools,
// so the turn always ends in an answer.
const MaxRounds = 10

// ToolRunner runs one tool call and returns what the model reads back.
type ToolRunner func(ctx context.Context, call llm.ToolCall) string

func (n *Native) messages(system string, history []store.ChatMessage) []llm.Message {
	byID := make(map[int64]store.ChatMessage, len(history))
	for _, m := range history {
		byID[m.ID] = m
	}
	limit := n.maxHistory
	if limit <= 0 {
		limit = MaxHistory
	}
	if len(history) > limit {
		history = history[len(history)-limit:]
	}
	msgs := make([]llm.Message, 0, len(history)+1)
	msgs = append(msgs, llm.Message{Role: "system", Content: n.systemPrompt(system)})
	for _, m := range history {
		if (m.Role == "user" || m.Role == "assistant") && m.Content != "" {
			content := m.Content
			if q, ok := byID[m.ReplyTo]; ok && m.ReplyTo != 0 {
				content = Quote(q) + "\n\n" + content
			}
			msgs = append(msgs, llm.Message{Role: m.Role, Content: content})
		}
	}
	return msgs
}

// Quote introduces a reply to a specific earlier message.
func Quote(q store.ChatMessage) string {
	who := "à sua mensagem"
	if q.Role == "user" {
		who = "à mensagem do dono"
	}
	text := strings.TrimSpace(q.Content)
	if r := []rune(text); len(r) > 600 {
		text = string(r[:600]) + "…"
	}
	return "[Respondendo " + who + ": «" + text + "»]"
}

// Reply answers the last message of history, streaming through onDelta.
func (n *Native) Reply(ctx context.Context, system string, history []store.ChatMessage, onDelta func(llm.Delta)) (llm.Response, error) {
	return n.provider.Chat(ctx, llm.Request{Model: n.model, Messages: n.messages(system, history), MaxTokens: 4096, Speed: n.speed}, onDelta)
}

// Run answers like Reply, letting the model call tools until it answers.
// The text of every round is kept, so "vou criar a rotina" before a call
// stays in the answer. Usage adds up all rounds.
func (n *Native) Run(ctx context.Context, system string, history []store.ChatMessage, tools []llm.Tool, run ToolRunner, onDelta func(llm.Delta)) (llm.Response, error) {
	return n.RunDynamic(ctx, system, history, func() []llm.Tool { return tools }, run, onDelta)
}

// RunDynamic resolves tools each round so plugins installed during a turn
// become available to the model as soon as they are connected.
func (n *Native) RunDynamic(ctx context.Context, system string, history []store.ChatMessage, tools func() []llm.Tool, run ToolRunner, onDelta func(llm.Delta)) (llm.Response, error) {
	msgs := n.messages(system, history)
	var out llm.Response
	parts := []string{}
	for round := 0; round < MaxRounds; round++ {
		req := llm.Request{Model: n.model, Messages: msgs, MaxTokens: 4096, Speed: n.speed}
		if round < MaxRounds-1 {
			req.Tools = tools()
		}
		wrote := false
		resp, err := n.provider.Chat(ctx, req, func(d llm.Delta) {
			if onDelta == nil {
				return
			}
			if d.Content != "" && !wrote && len(parts) > 0 {
				onDelta(llm.Delta{Content: "\n\n"})
			}
			wrote = wrote || d.Content != ""
			onDelta(d)
		})
		if err != nil {
			return out, err
		}
		out.Model, out.FinishReason = resp.Model, resp.FinishReason
		out.Usage.PromptTokens += resp.Usage.PromptTokens
		out.Usage.CompletionTokens += resp.Usage.CompletionTokens
		out.Reasoning += resp.Reasoning
		if text := strings.TrimSpace(resp.Content); text != "" {
			parts = append(parts, text)
		}
		if len(resp.ToolCalls) == 0 {
			break
		}
		calls := resp.ToolCalls
		for i := range calls {
			if calls[i].ID == "" {
				calls[i].ID = fmt.Sprintf("call_%d_%d", round, i)
			}
			calls[i].Type = "function"
		}
		msgs = append(msgs, llm.Message{Role: "assistant", Content: resp.Content, ToolCalls: calls})
		for _, c := range calls {
			result := run(ctx, c)
			if ctx.Err() != nil {
				return out, ctx.Err()
			}
			msgs = append(msgs, llm.Message{Role: "tool", ToolCallID: c.ID, Content: result})
		}
	}
	out.Content = strings.Join(parts, "\n\n")
	return out, nil
}
