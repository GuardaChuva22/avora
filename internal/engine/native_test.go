package engine

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/GuardaChuva22/avora/internal/llm"
	"github.com/GuardaChuva22/avora/internal/store"
)

func TestRunCallsToolsUntilAnswer(t *testing.T) {
	var bodies []map[string]any
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var body map[string]any
		_ = json.NewDecoder(r.Body).Decode(&body)
		bodies = append(bodies, body)
		w.Header().Set("Content-Type", "text/event-stream")
		if len(bodies) == 1 {
			fmt.Fprint(w, `data: {"choices":[{"delta":{"content":"Vou ver."}}]}`+"\n\n")
			fmt.Fprint(w, `data: {"choices":[{"delta":{"tool_calls":[{"index":0,"id":"c1","function":{"name":"estado","arguments":"{}"}}]},"finish_reason":"tool_calls"}],"usage":{"prompt_tokens":10,"completion_tokens":2}}`+"\n\n")
		} else {
			fmt.Fprint(w, `data: {"choices":[{"delta":{"content":"Tudo online."},"finish_reason":"stop"}],"usage":{"prompt_tokens":20,"completion_tokens":3}}`+"\n\n")
		}
		fmt.Fprint(w, "data: [DONE]\n\n")
	}))
	defer srv.Close()
	p, err := llm.NewProvider("test", srv.URL+"/v1", "key", 0)
	if err != nil {
		t.Fatal(err)
	}
	n := NewNative(p, "m")
	tools := []llm.Tool{{Type: "function", Function: llm.ToolFunction{Name: "estado", Parameters: json.RawMessage(`{"type":"object"}`)}}}
	var streamed strings.Builder
	called := 0
	resp, err := n.Run(context.Background(), "Você é Vega.", []store.ChatMessage{{Role: "user", Content: "como estamos?"}}, tools,
		func(_ context.Context, c llm.ToolCall) string {
			called++
			return "2 nós online"
		}, func(d llm.Delta) { streamed.WriteString(d.Content) })
	if err != nil {
		t.Fatal(err)
	}
	if called != 1 || resp.Content != "Vou ver.\n\nTudo online." || streamed.String() != "Vou ver.\n\nTudo online." ||
		resp.Usage.PromptTokens != 30 || resp.Usage.CompletionTokens != 5 {
		t.Fatalf("resp %+v streamed %q called %d", resp, streamed.String(), called)
	}
	// The second request carries the call and its result.
	msgs := bodies[1]["messages"].([]any)
	call := msgs[len(msgs)-2].(map[string]any)
	result := msgs[len(msgs)-1].(map[string]any)
	if call["role"] != "assistant" || call["tool_calls"] == nil || result["role"] != "tool" || result["tool_call_id"] != "c1" ||
		result["content"] != "2 nós online" || bodies[0]["tools"] == nil {
		t.Fatalf("second request: %+v", bodies[1])
	}
}

func TestRepliesQuoteTheMessage(t *testing.T) {
	n := NewNative(nil, "m")
	msgs := n.messages("", []store.ChatMessage{
		{ID: 1, Role: "assistant", Content: "Sobram R$ 1.400."},
		{ID: 2, Role: "user", Content: "e se cortar?", ReplyTo: 1},
	})
	if got := msgs[len(msgs)-1].Content; got != "[Respondendo à sua mensagem: «Sobram R$ 1.400.»]\n\ne se cortar?" {
		t.Fatalf("content: %q", got)
	}
}
