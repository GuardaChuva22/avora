// Package llm talks to OpenAI-compatible chat completion APIs (NVIDIA build,
// OpenRouter, Ollama and others), always streaming, with a client-side rate
// limit and retries on 429.
package llm

import (
	"bufio"
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/url"
	"sort"
	"strconv"
	"strings"
	"sync"
	"time"
)

type Message struct {
	Role    string `json:"role"`
	Content string `json:"content"`
	// ToolCalls are the calls an assistant message made.
	ToolCalls []ToolCall `json:"tool_calls,omitempty"`
	// ToolCallID ties a "tool" message to the call it answers.
	ToolCallID string `json:"tool_call_id,omitempty"`
}

// Tool is a function the model may call (OpenAI "tools" format).
type Tool struct {
	Type     string       `json:"type"`
	Function ToolFunction `json:"function"`
}

type ToolFunction struct {
	Name        string          `json:"name"`
	Description string          `json:"description"`
	Parameters  json.RawMessage `json:"parameters"`
}

// ToolCall is one call the model asked for; Arguments is a JSON object.
type ToolCall struct {
	ID       string       `json:"id"`
	Type     string       `json:"type"`
	Function FunctionCall `json:"function"`
}

type FunctionCall struct {
	Name      string `json:"name"`
	Arguments string `json:"arguments"`
}

type Request struct {
	Model       string
	Messages    []Message
	Tools       []Tool
	MaxTokens   int
	Temperature *float64
	Speed       string
}

type Usage struct {
	PromptTokens     int `json:"prompt_tokens"`
	CompletionTokens int `json:"completion_tokens"`
}

type Response struct {
	Content      string
	Reasoning    string
	Model        string
	FinishReason string
	Usage        Usage
	ToolCalls    []ToolCall
}

// Delta is a streamed piece of the answer or of the model's reasoning.
type Delta struct {
	Content   string
	Reasoning string
}

// APIError is a non-2xx answer from the provider.
type APIError struct {
	Status int
	Body   string
}

func (e *APIError) Error() string {
	return fmt.Sprintf("provider returned %d: %s", e.Status, e.Body)
}

type Provider struct {
	Name    string
	BaseURL string
	// Format selects the wire API: "" / "openai" (default) or "anthropic"
	// (Messages API at /messages with x-api-key and anthropic-version).
	Format  string
	apiKey  string
	http    *http.Client
	limiter *limiter
	// MaxRetries is how many times a 429 or 5xx before the first byte is retried.
	MaxRetries int
	sleep      func(context.Context, time.Duration) error
}

func (p *Provider) isAnthropic() bool {
	return strings.EqualFold(strings.TrimSpace(p.Format), "anthropic")
}

// LocalURL reports whether baseURL is plain http to one of the owner's own
// machines: this one, the local network (private ranges, *.local, *.lan,
// *.home.arpa, *.internal) or the tailnet (100.64.0.0/10, *.ts.net). Those
// may skip https and the API key (Ollama, LM Studio, llama.cpp).
func LocalURL(baseURL string) bool {
	u, err := url.Parse(baseURL)
	if err != nil || u.Scheme != "http" || u.Hostname() == "" {
		return false
	}
	host := u.Hostname()
	if host == "localhost" {
		return true
	}
	for _, suffix := range []string{".ts.net", ".local", ".lan", ".home.arpa", ".internal"} {
		if strings.HasSuffix(host, suffix) {
			return true
		}
	}
	ip := net.ParseIP(host)
	return ip != nil && (ip.IsLoopback() || ip.IsPrivate() || tailnet.Contains(ip))
}

var tailnet = &net.IPNet{IP: net.IPv4(100, 64, 0, 0), Mask: net.CIDRMask(10, 32)}

// NewProvider builds a provider. rpm <= 0 disables the client-side limit.
// A local server (LocalURL) may speak plain http and need no key.
func NewProvider(name, baseURL, apiKey string, rpm int) (*Provider, error) {
	local := LocalURL(baseURL)
	if !strings.HasPrefix(baseURL, "https://") && !local {
		return nil, errors.New("provider base URL must be https (or http to this machine, the local network or the tailnet)")
	}
	if apiKey == "" && !local {
		return nil, errors.New("provider API key is empty")
	}
	return &Provider{
		Name: name, BaseURL: strings.TrimSuffix(baseURL, "/"), apiKey: apiKey,
		http:       &http.Client{Transport: http.DefaultTransport},
		limiter:    newLimiter(rpm),
		MaxRetries: 3,
		sleep:      sleepCtx,
	}, nil
}

// NewProviderWithTransport builds a provider backed by a custom RoundTripper
// (such as the node runner queue for local models).
func NewProviderWithTransport(name, baseURL, apiKey string, rpm int, rt http.RoundTripper) *Provider {
	if rt == nil {
		rt = http.DefaultTransport
	}
	if strings.TrimSpace(baseURL) == "" {
		baseURL = "http://127.0.0.1/v1"
	}
	return &Provider{
		Name:       name,
		BaseURL:    strings.TrimSuffix(strings.TrimSpace(baseURL), "/"),
		apiKey:     apiKey,
		http:       &http.Client{Transport: rt},
		limiter:    newLimiter(rpm),
		MaxRetries: 0,
		sleep:      sleepCtx,
	}
}

// authorize signs a request. Anthropic's OpenAI-compatible chat takes the
// Bearer key, while Anthropic's Messages API and model list want x-api-key
// and anthropic-version, so both go when applicable.
func (p *Provider) authorize(req *http.Request) {
	if p.apiKey == "" {
		return
	}
	req.Header.Set("Authorization", "Bearer "+p.apiKey)
	if req.URL.Hostname() == "api.anthropic.com" || p.isAnthropic() {
		req.Header.Set("x-api-key", p.apiKey)
		req.Header.Set("anthropic-version", "2023-06-01")
	}
}

func sleepCtx(ctx context.Context, d time.Duration) error {
	t := time.NewTimer(d)
	defer t.Stop()
	select {
	case <-ctx.Done():
		return ctx.Err()
	case <-t.C:
		return nil
	}
}

// Chat streams a completion. onDelta may be nil.
func (p *Provider) Chat(ctx context.Context, req Request, onDelta func(Delta)) (Response, error) {
	if p.isAnthropic() {
		return p.chatAnthropic(ctx, req, onDelta)
	}
	body := map[string]any{
		"model":          req.Model,
		"messages":       req.Messages,
		"stream":         true,
		"stream_options": map[string]bool{"include_usage": true},
	}
	if req.MaxTokens > 0 {
		body["max_tokens"] = req.MaxTokens
	}
	if len(req.Tools) > 0 {
		body["tools"] = req.Tools
		body["tool_choice"] = "auto"
	}
	if req.Temperature != nil {
		body["temperature"] = *req.Temperature
	}
	if req.Speed == "fast" && strings.Contains(p.BaseURL, "api.openai.com") {
		body["service_tier"] = "priority"
	}
	payload, err := json.Marshal(body)
	if err != nil {
		return Response{}, err
	}

	for attempt := 0; ; attempt++ {
		if err := p.limiter.wait(ctx); err != nil {
			return Response{}, err
		}
		resp, err := p.send(ctx, "/chat/completions", payload)
		if err != nil {
			return Response{}, err
		}
		if resp.StatusCode == http.StatusOK {
			defer resp.Body.Close()
			return readStream(resp.Body, onDelta)
		}
		apiErr := &APIError{Status: resp.StatusCode, Body: readSnippet(resp.Body)}
		retryAfter := parseRetryAfter(resp.Header.Get("Retry-After"))
		resp.Body.Close()
		retryable := resp.StatusCode == http.StatusTooManyRequests || resp.StatusCode >= 500
		if !retryable || attempt >= p.MaxRetries {
			return Response{}, apiErr
		}
		wait := time.Duration(1<<attempt) * 2 * time.Second
		if retryAfter > 0 {
			wait = retryAfter
		}
		if err := p.sleep(ctx, wait); err != nil {
			return Response{}, err
		}
	}
}

func (p *Provider) send(ctx context.Context, suffix string, payload []byte) (*http.Response, error) {
	httpReq, err := http.NewRequestWithContext(ctx, http.MethodPost, p.BaseURL+suffix, bytes.NewReader(payload))
	if err != nil {
		return nil, err
	}
	p.authorize(httpReq)
	httpReq.Header.Set("Content-Type", "application/json")
	httpReq.Header.Set("Accept", "text/event-stream")
	return p.http.Do(httpReq)
}

func readSnippet(r io.Reader) string {
	b, _ := io.ReadAll(io.LimitReader(r, 2048))
	return strings.TrimSpace(string(b))
}

func parseRetryAfter(v string) time.Duration {
	if secs, err := strconv.Atoi(strings.TrimSpace(v)); err == nil && secs > 0 && secs <= 300 {
		return time.Duration(secs) * time.Second
	}
	return 0
}

type chunk struct {
	Model   string `json:"model"`
	Choices []struct {
		Delta struct {
			Content          string `json:"content"`
			ReasoningContent string `json:"reasoning_content"`
			Reasoning        string `json:"reasoning"`
			ToolCalls        []struct {
				Index    int    `json:"index"`
				ID       string `json:"id"`
				Function struct {
					Name      string `json:"name"`
					Arguments string `json:"arguments"`
				} `json:"function"`
			} `json:"tool_calls"`
		} `json:"delta"`
		FinishReason *string `json:"finish_reason"`
	} `json:"choices"`
	Usage *Usage `json:"usage"`
	Error *struct {
		Message string `json:"message"`
	} `json:"error"`
}

func readStream(r io.Reader, onDelta func(Delta)) (Response, error) {
	var out Response
	var content, reasoning strings.Builder
	calls := map[int]*ToolCall{}
	order := []int{}
	scanner := bufio.NewScanner(r)
	scanner.Buffer(make([]byte, 64*1024), 1024*1024)
	done := false
	for scanner.Scan() {
		line := strings.TrimSpace(scanner.Text())
		if !strings.HasPrefix(line, "data:") {
			continue
		}
		data := strings.TrimSpace(strings.TrimPrefix(line, "data:"))
		if data == "[DONE]" {
			done = true
			break
		}
		var c chunk
		if err := json.Unmarshal([]byte(data), &c); err != nil {
			return Response{}, fmt.Errorf("malformed stream chunk: %w", err)
		}
		if c.Error != nil {
			return Response{}, fmt.Errorf("provider stream error: %s", c.Error.Message)
		}
		if c.Model != "" {
			out.Model = c.Model
		}
		if c.Usage != nil {
			out.Usage = *c.Usage
		}
		for _, choice := range c.Choices {
			d := Delta{Content: choice.Delta.Content, Reasoning: choice.Delta.ReasoningContent + choice.Delta.Reasoning}
			for _, tc := range choice.Delta.ToolCalls {
				c := calls[tc.Index]
				if c == nil {
					c = &ToolCall{Type: "function"}
					calls[tc.Index] = c
					order = append(order, tc.Index)
				}
				if tc.ID != "" {
					c.ID = tc.ID
				}
				if tc.Function.Name != "" && c.Function.Name == "" {
					c.Function.Name = tc.Function.Name
				}
				c.Function.Arguments += tc.Function.Arguments
			}
			content.WriteString(d.Content)
			reasoning.WriteString(d.Reasoning)
			if onDelta != nil && (d.Content != "" || d.Reasoning != "") {
				onDelta(d)
			}
			if choice.FinishReason != nil && *choice.FinishReason != "" {
				out.FinishReason = *choice.FinishReason
			}
		}
	}
	if err := scanner.Err(); err != nil {
		return Response{}, err
	}
	if !done && out.FinishReason == "" {
		return Response{}, errors.New("stream ended before the answer was complete")
	}
	out.Content, out.Reasoning = content.String(), reasoning.String()
	for _, i := range order {
		if c := calls[i]; c.Function.Name != "" {
			out.ToolCalls = append(out.ToolCalls, *c)
		}
	}
	return out, nil
}

// limiter spaces requests evenly to stay under a requests-per-minute cap.
type limiter struct {
	mu       sync.Mutex
	interval time.Duration
	next     time.Time
	now      func() time.Time
}

func newLimiter(rpm int) *limiter {
	l := &limiter{now: time.Now}
	if rpm > 0 {
		l.interval = time.Minute / time.Duration(rpm)
	}
	return l
}

func (l *limiter) wait(ctx context.Context) error {
	if l.interval == 0 {
		return nil
	}
	l.mu.Lock()
	now := l.now()
	start := l.next
	if start.Before(now) {
		start = now
	}
	l.next = start.Add(l.interval)
	l.mu.Unlock()
	if d := start.Sub(now); d > 0 {
		return sleepCtx(ctx, d)
	}
	return nil
}

// Models lists the model ids the provider serves (GET /models, the
// OpenAI format), sorted.
func (p *Provider) Models(ctx context.Context) ([]string, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, p.BaseURL+"/models", nil)
	if err != nil {
		return nil, err
	}
	p.authorize(req)
	resp, err := p.http.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	if resp.StatusCode == http.StatusNotFound && p.isAnthropic() {
		return []string{}, nil
	}
	if resp.StatusCode != http.StatusOK {
		return nil, &APIError{Status: resp.StatusCode, Body: readSnippet(resp.Body)}
	}
	var body struct {
		Data []struct {
			ID string `json:"id"`
		} `json:"data"`
	}
	if err := json.NewDecoder(io.LimitReader(resp.Body, 8<<20)).Decode(&body); err != nil {
		return nil, fmt.Errorf("reading the model list: %w", err)
	}
	out := make([]string, 0, len(body.Data))
	for _, m := range body.Data {
		if m.ID != "" {
			out = append(out, m.ID)
		}
	}
	sort.Strings(out)
	return out, nil
}

// ProbeTools makes a minimal request with a fake no-op tool to check whether
// model accepts tool/function calling on this provider.
func (p *Provider) ProbeTools(ctx context.Context, model string) (bool, error) {
	clone := *p
	clone.MaxRetries = 0
	dummy := Tool{
		Type: "function",
		Function: ToolFunction{
			Name:        "avora_ping",
			Description: "No-op tool capability check.",
			Parameters:  json.RawMessage(`{"type":"object","properties":{}}`),
		},
	}
	_, err := clone.Chat(ctx, Request{
		Model:     model,
		Messages:  []Message{{Role: "user", Content: "ping"}},
		Tools:     []Tool{dummy},
		MaxTokens: 16,
	}, nil)
	if err == nil {
		return true, nil
	}
	var apiErr *APIError
	if errors.As(err, &apiErr) {
		switch apiErr.Status {
		case http.StatusBadRequest, http.StatusNotFound, http.StatusMethodNotAllowed, http.StatusUnprocessableEntity, http.StatusNotImplemented:
			return false, nil
		}
	}
	low := strings.ToLower(err.Error())
	if strings.Contains(low, "tool") || strings.Contains(low, "function") || strings.Contains(low, "not support") || strings.Contains(low, "unsupported") {
		return false, nil
	}
	return false, err
}

func (p *Provider) chatAnthropic(ctx context.Context, req Request, onDelta func(Delta)) (Response, error) {
	payload, err := buildAnthropicPayload(req)
	if err != nil {
		return Response{}, err
	}
	for attempt := 0; ; attempt++ {
		if err := p.limiter.wait(ctx); err != nil {
			return Response{}, err
		}
		resp, err := p.send(ctx, "/messages", payload)
		if err != nil {
			return Response{}, err
		}
		if resp.StatusCode == http.StatusOK {
			defer resp.Body.Close()
			ct := strings.ToLower(resp.Header.Get("Content-Type"))
			if strings.Contains(ct, "application/json") {
				return readAnthropicJSON(resp.Body, onDelta)
			}
			return readAnthropicStream(resp.Body, onDelta)
		}
		apiErr := &APIError{Status: resp.StatusCode, Body: readSnippet(resp.Body)}
		retryAfter := parseRetryAfter(resp.Header.Get("Retry-After"))
		resp.Body.Close()
		retryable := resp.StatusCode == http.StatusTooManyRequests || resp.StatusCode >= 500
		if !retryable || attempt >= p.MaxRetries {
			return Response{}, apiErr
		}
		wait := time.Duration(1<<attempt) * 2 * time.Second
		if retryAfter > 0 {
			wait = retryAfter
		}
		if err := p.sleep(ctx, wait); err != nil {
			return Response{}, err
		}
	}
}

func buildAnthropicPayload(req Request) ([]byte, error) {
	maxTokens := req.MaxTokens
	if maxTokens <= 0 {
		maxTokens = 4096
	}
	var sysParts []string
	var msgs []map[string]any
	for i := 0; i < len(req.Messages); i++ {
		m := req.Messages[i]
		switch m.Role {
		case "system":
			if strings.TrimSpace(m.Content) != "" {
				sysParts = append(sysParts, m.Content)
			}
		case "tool":
			var blocks []map[string]any
			for ; i < len(req.Messages) && req.Messages[i].Role == "tool"; i++ {
				tm := req.Messages[i]
				blocks = append(blocks, map[string]any{
					"type":        "tool_result",
					"tool_use_id": tm.ToolCallID,
					"content":     tm.Content,
				})
			}
			i--
			msgs = append(msgs, map[string]any{"role": "user", "content": blocks})
		case "assistant":
			if len(m.ToolCalls) > 0 {
				var blocks []map[string]any
				if strings.TrimSpace(m.Content) != "" {
					blocks = append(blocks, map[string]any{"type": "text", "text": m.Content})
				}
				for _, tc := range m.ToolCalls {
					var input any = map[string]any{}
					if strings.TrimSpace(tc.Function.Arguments) != "" {
						_ = json.Unmarshal([]byte(tc.Function.Arguments), &input)
					}
					blocks = append(blocks, map[string]any{
						"type":  "tool_use",
						"id":    tc.ID,
						"name":  tc.Function.Name,
						"input": input,
					})
				}
				msgs = append(msgs, map[string]any{"role": "assistant", "content": blocks})
			} else {
				msgs = append(msgs, map[string]any{"role": "assistant", "content": m.Content})
			}
		default:
			msgs = append(msgs, map[string]any{"role": "user", "content": m.Content})
		}
	}
	if len(msgs) == 0 {
		msgs = []map[string]any{{"role": "user", "content": " "}}
	}
	body := map[string]any{
		"model":      req.Model,
		"max_tokens": maxTokens,
		"messages":   msgs,
		"stream":     true,
	}
	if len(sysParts) > 0 {
		body["system"] = strings.Join(sysParts, "\n\n")
	}
	if len(req.Tools) > 0 {
		tools := make([]map[string]any, 0, len(req.Tools))
		for _, t := range req.Tools {
			schema := t.Function.Parameters
			if len(bytes.TrimSpace(schema)) == 0 {
				schema = json.RawMessage(`{"type":"object","properties":{}}`)
			}
			tools = append(tools, map[string]any{
				"name":         t.Function.Name,
				"description":  t.Function.Description,
				"input_schema": schema,
			})
		}
		body["tools"] = tools
	}
	if req.Temperature != nil {
		body["temperature"] = *req.Temperature
	}
	return json.Marshal(body)
}

func readAnthropicJSON(r io.Reader, onDelta func(Delta)) (Response, error) {
	var msg struct {
		Model      string `json:"model"`
		StopReason string `json:"stop_reason"`
		Content    []struct {
			Type     string          `json:"type"`
			Text     string          `json:"text"`
			Thinking string          `json:"thinking"`
			ID       string          `json:"id"`
			Name     string          `json:"name"`
			Input    json.RawMessage `json:"input"`
		} `json:"content"`
		Usage struct {
			InputTokens  int `json:"input_tokens"`
			OutputTokens int `json:"output_tokens"`
		} `json:"usage"`
		Error *struct {
			Message string `json:"message"`
		} `json:"error"`
	}
	if err := json.NewDecoder(io.LimitReader(r, 8<<20)).Decode(&msg); err != nil {
		return Response{}, fmt.Errorf("malformed anthropic response: %w", err)
	}
	if msg.Error != nil {
		return Response{}, fmt.Errorf("provider error: %s", msg.Error.Message)
	}
	out := Response{
		Model:        msg.Model,
		FinishReason: msg.StopReason,
		Usage:        Usage{PromptTokens: msg.Usage.InputTokens, CompletionTokens: msg.Usage.OutputTokens},
	}
	if out.FinishReason == "" {
		out.FinishReason = "end_turn"
	}
	for _, b := range msg.Content {
		switch b.Type {
		case "text":
			out.Content += b.Text
			if onDelta != nil && b.Text != "" {
				onDelta(Delta{Content: b.Text})
			}
		case "thinking":
			out.Reasoning += b.Thinking
			if onDelta != nil && b.Thinking != "" {
				onDelta(Delta{Reasoning: b.Thinking})
			}
		case "tool_use":
			args := strings.TrimSpace(string(b.Input))
			if args == "" {
				args = "{}"
			}
			out.ToolCalls = append(out.ToolCalls, ToolCall{
				ID:   b.ID,
				Type: "function",
				Function: FunctionCall{
					Name:      b.Name,
					Arguments: args,
				},
			})
		}
	}
	return out, nil
}

func readAnthropicStream(r io.Reader, onDelta func(Delta)) (Response, error) {
	var out Response
	var content, reasoning strings.Builder
	calls := map[int]*ToolCall{}
	order := []int{}
	scanner := bufio.NewScanner(r)
	scanner.Buffer(make([]byte, 64*1024), 1024*1024)
	done := false
	for scanner.Scan() {
		line := strings.TrimSpace(scanner.Text())
		if !strings.HasPrefix(line, "data:") {
			continue
		}
		data := strings.TrimSpace(strings.TrimPrefix(line, "data:"))
		if data == "" || data == "[DONE]" {
			if data == "[DONE]" {
				done = true
				break
			}
			continue
		}
		var ev struct {
			Type    string `json:"type"`
			Index   int    `json:"index"`
			Message *struct {
				Model string `json:"model"`
				Usage *struct {
					InputTokens  int `json:"input_tokens"`
					OutputTokens int `json:"output_tokens"`
				} `json:"usage"`
			} `json:"message"`
			ContentBlock *struct {
				Type  string          `json:"type"`
				ID    string          `json:"id"`
				Name  string          `json:"name"`
				Text  string          `json:"text"`
				Input json.RawMessage `json:"input"`
			} `json:"content_block"`
			Delta *struct {
				Type        string `json:"type"`
				Text        string `json:"text"`
				Thinking    string `json:"thinking"`
				PartialJSON string `json:"partial_json"`
				StopReason  string `json:"stop_reason"`
			} `json:"delta"`
			Usage *struct {
				InputTokens  int `json:"input_tokens"`
				OutputTokens int `json:"output_tokens"`
			} `json:"usage"`
			Error *struct {
				Message string `json:"message"`
			} `json:"error"`
		}
		if err := json.Unmarshal([]byte(data), &ev); err != nil {
			return Response{}, fmt.Errorf("malformed anthropic stream event: %w", err)
		}
		if ev.Error != nil {
			return Response{}, fmt.Errorf("provider stream error: %s", ev.Error.Message)
		}
		switch ev.Type {
		case "message_start":
			if ev.Message != nil {
				if ev.Message.Model != "" {
					out.Model = ev.Message.Model
				}
				if ev.Message.Usage != nil {
					out.Usage.PromptTokens = ev.Message.Usage.InputTokens
					out.Usage.CompletionTokens = ev.Message.Usage.OutputTokens
				}
			}
		case "content_block_start":
			if cb := ev.ContentBlock; cb != nil {
				switch cb.Type {
				case "text":
					if cb.Text != "" {
						content.WriteString(cb.Text)
						if onDelta != nil {
							onDelta(Delta{Content: cb.Text})
						}
					}
				case "tool_use":
					tc := &ToolCall{ID: cb.ID, Type: "function", Function: FunctionCall{Name: cb.Name}}
					if s := strings.TrimSpace(string(cb.Input)); s != "" && s != "{}" {
						tc.Function.Arguments = s
					}
					calls[ev.Index] = tc
					order = append(order, ev.Index)
				}
			}
		case "content_block_delta":
			if d := ev.Delta; d != nil {
				switch d.Type {
				case "text_delta":
					content.WriteString(d.Text)
					if onDelta != nil && d.Text != "" {
						onDelta(Delta{Content: d.Text})
					}
				case "thinking_delta":
					reasoning.WriteString(d.Thinking)
					if onDelta != nil && d.Thinking != "" {
						onDelta(Delta{Reasoning: d.Thinking})
					}
				case "input_json_delta":
					if tc := calls[ev.Index]; tc != nil {
						tc.Function.Arguments += d.PartialJSON
					}
				}
			}
		case "message_delta":
			if ev.Delta != nil && ev.Delta.StopReason != "" {
				out.FinishReason = ev.Delta.StopReason
			}
			if ev.Usage != nil {
				if ev.Usage.InputTokens > 0 {
					out.Usage.PromptTokens = ev.Usage.InputTokens
				}
				if ev.Usage.OutputTokens > 0 {
					out.Usage.CompletionTokens = ev.Usage.OutputTokens
				}
			}
		case "message_stop":
			done = true
		}
	}
	if err := scanner.Err(); err != nil {
		return Response{}, err
	}
	if !done && out.FinishReason == "" {
		return Response{}, errors.New("stream ended before the answer was complete")
	}
	out.Content, out.Reasoning = content.String(), reasoning.String()
	for _, idx := range order {
		if tc := calls[idx]; tc != nil && tc.Function.Name != "" {
			if strings.TrimSpace(tc.Function.Arguments) == "" {
				tc.Function.Arguments = "{}"
			}
			out.ToolCalls = append(out.ToolCalls, *tc)
		}
	}
	return out, nil
}
