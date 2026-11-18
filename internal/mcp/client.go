// Package mcp is a client for remote MCP servers over Streamable HTTP, with
// the OAuth the MCP authorization spec describes (discovery, dynamic client
// registration, PKCE, refresh). It is what lets the owner connect an app
// with one click, the way Claude and Codex do.
package mcp

import (
	"bufio"
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"strings"
	"sync"
	"sync/atomic"
)

// ProtocolVersion is the version this client asks for; servers may answer
// with an older one, which it then sends on every request.
const ProtocolVersion = "2025-06-18"

const maxBody = 8 << 20

// Tool is a tool a server offers.
type Tool struct {
	Name        string          `json:"name"`
	Title       string          `json:"title,omitempty"`
	Description string          `json:"description,omitempty"`
	InputSchema json.RawMessage `json:"inputSchema,omitempty"`
	Annotations *Annotations    `json:"annotations,omitempty"`
}

// Annotations are the server's hints about what a tool does.
type Annotations struct {
	Title           string `json:"title,omitempty"`
	ReadOnlyHint    *bool  `json:"readOnlyHint,omitempty"`
	DestructiveHint *bool  `json:"destructiveHint,omitempty"`
	IdempotentHint  *bool  `json:"idempotentHint,omitempty"`
	OpenWorldHint   *bool  `json:"openWorldHint,omitempty"`
}

type ServerInfo struct {
	Name    string `json:"name"`
	Title   string `json:"title,omitempty"`
	Version string `json:"version,omitempty"`
}

// Content is one piece of a tool result.
type Content struct {
	Type     string `json:"type"`
	Text     string `json:"text,omitempty"`
	Data     string `json:"data,omitempty"`
	MimeType string `json:"mimeType,omitempty"`
	URI      string `json:"uri,omitempty"`
	Name     string `json:"name,omitempty"`
	Resource *struct {
		URI      string `json:"uri"`
		MimeType string `json:"mimeType,omitempty"`
		Text     string `json:"text,omitempty"`
		Blob     string `json:"blob,omitempty"`
	} `json:"resource,omitempty"`
}

type CallResult struct {
	Content           []Content       `json:"content"`
	StructuredContent json.RawMessage `json:"structuredContent,omitempty"`
	IsError           bool            `json:"isError,omitempty"`
}

// HTTPError is a non-2xx answer. A 401 means the token is missing or no
// longer valid; WWWAuth carries the server's hints.
type HTTPError struct {
	Status     int
	Body       string
	WWWAuth    string
	Challenged bool
}

func (e *HTTPError) Error() string {
	if e.Body != "" {
		return fmt.Sprintf("HTTP %d: %s", e.Status, e.Body)
	}
	return fmt.Sprintf("HTTP %d", e.Status)
}

func IsUnauthorized(err error) bool {
	var h *HTTPError
	return errors.As(err, &h) && (h.Status == 401 || h.Status == 403 && strings.Contains(h.WWWAuth, "invalid_token"))
}

// RPCError is an error the server answered in JSON-RPC.
type RPCError struct {
	Code    int    `json:"code"`
	Message string `json:"message"`
}

func (e *RPCError) Error() string { return fmt.Sprintf("%s (código %d)", e.Message, e.Code) }

// Client talks to one server. It keeps the session the server hands out
// and opens a new one when the server forgets it.
type Client struct {
	URL  string
	HTTP *http.Client

	mu      sync.Mutex
	session string
	version string
	info    ServerInfo
	ready   bool
	ids     atomic.Int64
}

func (c *Client) httpClient() *http.Client {
	if c.HTTP != nil {
		return c.HTTP
	}
	return SafeClient()
}

type rpcMessage struct {
	JSONRPC string          `json:"jsonrpc"`
	ID      json.RawMessage `json:"id,omitempty"`
	Method  string          `json:"method,omitempty"`
	Result  json.RawMessage `json:"result,omitempty"`
	Error   *RPCError       `json:"error,omitempty"`
}

// post sends one JSON-RPC message; for a request it returns the result.
func (c *Client) post(ctx context.Context, token, method string, params any, notify bool) (json.RawMessage, error) {
	msg := map[string]any{"jsonrpc": "2.0", "method": method}
	if params != nil {
		msg["params"] = params
	}
	var id int64
	if !notify {
		id = c.ids.Add(1)
		msg["id"] = id
	}
	body, err := json.Marshal(msg)
	if err != nil {
		return nil, err
	}
	req, err := http.NewRequestWithContext(ctx, "POST", c.URL, bytes.NewReader(body))
	if err != nil {
		return nil, err
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Accept", "application/json, text/event-stream")
	req.Header.Set("User-Agent", "avora/1")
	if token != "" {
		req.Header.Set("Authorization", "Bearer "+token)
	}
	c.mu.Lock()
	session, version := c.session, c.version
	c.mu.Unlock()
	if session != "" {
		req.Header.Set("Mcp-Session-Id", session)
	}
	if version != "" && method != "initialize" {
		req.Header.Set("MCP-Protocol-Version", version)
	}
	resp, err := c.httpClient().Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	if resp.StatusCode < 200 || resp.StatusCode > 299 {
		b, _ := io.ReadAll(io.LimitReader(resp.Body, 2000))
		return nil, &HTTPError{Status: resp.StatusCode, Body: strings.TrimSpace(string(b)), WWWAuth: resp.Header.Get("WWW-Authenticate"),
			Challenged: strings.EqualFold(resp.Header.Get("CF-Mitigated"), "challenge")}
	}
	if s := resp.Header.Get("Mcp-Session-Id"); s != "" && method == "initialize" {
		c.mu.Lock()
		c.session = s
		c.mu.Unlock()
	}
	if notify {
		return nil, nil
	}
	want := fmt.Sprint(id)
	if strings.HasPrefix(resp.Header.Get("Content-Type"), "text/event-stream") {
		return readStream(io.LimitReader(resp.Body, maxBody), want)
	}
	var m rpcMessage
	if err := json.NewDecoder(io.LimitReader(resp.Body, maxBody)).Decode(&m); err != nil {
		return nil, fmt.Errorf("resposta inválida do servidor MCP: %v", err)
	}
	return m.answer(want)
}

func (m rpcMessage) answer(want string) (json.RawMessage, error) {
	if string(m.ID) != want {
		return nil, fmt.Errorf("resposta do servidor MCP para outro pedido")
	}
	if m.Error != nil {
		return nil, m.Error
	}
	return m.Result, nil
}

// readStream reads server-sent events until the answer to id arrives,
// skipping the server's notifications and requests.
func readStream(r io.Reader, want string) (json.RawMessage, error) {
	sc := bufio.NewScanner(r)
	sc.Buffer(make([]byte, 64<<10), maxBody)
	var data strings.Builder
	flush := func() (json.RawMessage, bool, error) {
		defer data.Reset()
		if data.Len() == 0 {
			return nil, false, nil
		}
		var m rpcMessage
		if json.Unmarshal([]byte(data.String()), &m) != nil || string(m.ID) != want || m.Method != "" {
			return nil, false, nil
		}
		res, err := m.answer(want)
		return res, true, err
	}
	for sc.Scan() {
		line := sc.Text()
		switch {
		case line == "":
			if res, ok, err := flush(); ok {
				return res, err
			}
		case strings.HasPrefix(line, "data:"):
			if data.Len() > 0 {
				data.WriteByte('\n')
			}
			data.WriteString(strings.TrimPrefix(strings.TrimPrefix(line, "data:"), " "))
		}
	}
	if res, ok, err := flush(); ok {
		return res, err
	}
	if err := sc.Err(); err != nil {
		return nil, err
	}
	return nil, errors.New("o servidor MCP fechou a resposta sem responder")
}

// Initialize opens a session.
func (c *Client) Initialize(ctx context.Context, token string) (ServerInfo, error) {
	c.mu.Lock()
	c.session, c.version, c.ready = "", "", false
	c.mu.Unlock()
	raw, err := c.post(ctx, token, "initialize", initializeParams(), false)
	if err != nil {
		return ServerInfo{}, err
	}
	var r initializeResult
	if err := json.Unmarshal(raw, &r); err != nil {
		return ServerInfo{}, fmt.Errorf("initialize inválido: %v", err)
	}
	c.mu.Lock()
	c.version, c.info = r.ProtocolVersion, r.ServerInfo
	c.mu.Unlock()
	if _, err := c.post(ctx, token, "notifications/initialized", nil, true); err != nil {
		return ServerInfo{}, err
	}
	c.mu.Lock()
	c.ready = true
	c.mu.Unlock()
	return r.ServerInfo, nil
}

type initializeResult struct {
	ProtocolVersion string     `json:"protocolVersion"`
	ServerInfo      ServerInfo `json:"serverInfo"`
}

func initializeParams() map[string]any {
	return map[string]any{
		"protocolVersion": ProtocolVersion,
		"capabilities":    map[string]any{},
		"clientInfo":      map[string]any{"name": "avora", "title": "Avora", "version": "1"},
	}
}

// Reset forgets the session (the token changed).
func (c *Client) Reset() {
	c.mu.Lock()
	c.session, c.version, c.ready = "", "", false
	c.mu.Unlock()
}

// request runs a method in a session, opening one first or again when the
// server lost it.
func (c *Client) request(ctx context.Context, token, method string, params any) (json.RawMessage, error) {
	c.mu.Lock()
	ready := c.ready
	c.mu.Unlock()
	if !ready {
		if _, err := c.Initialize(ctx, token); err != nil {
			return nil, err
		}
	}
	raw, err := c.post(ctx, token, method, params, false)
	var h *HTTPError
	if errors.As(err, &h) && (h.Status == 404 || h.Status == 400 && strings.Contains(strings.ToLower(h.Body), "session")) {
		if _, err := c.Initialize(ctx, token); err != nil {
			return nil, err
		}
		return c.post(ctx, token, method, params, false)
	}
	return raw, err
}

// ListTools reads every page of the server's tools.
func (c *Client) ListTools(ctx context.Context, token string) ([]Tool, error) {
	return listTools(func(method string, params any) (json.RawMessage, error) {
		return c.request(ctx, token, method, params)
	})
}

func listTools(request func(string, any) (json.RawMessage, error)) ([]Tool, error) {
	out := []Tool{}
	cursor := ""
	for page := 0; page < 20; page++ {
		var params any
		if cursor != "" {
			params = map[string]any{"cursor": cursor}
		}
		raw, err := request("tools/list", params)
		if err != nil {
			return nil, err
		}
		var r struct {
			Tools      []Tool `json:"tools"`
			NextCursor string `json:"nextCursor"`
		}
		if err := json.Unmarshal(raw, &r); err != nil {
			return nil, fmt.Errorf("tools/list inválido: %v", err)
		}
		out = append(out, r.Tools...)
		if r.NextCursor == "" {
			break
		}
		cursor = r.NextCursor
	}
	return out, nil
}

// CallTool runs a tool. A failed call comes back with IsError set.
func (c *Client) CallTool(ctx context.Context, token, name string, args json.RawMessage) (CallResult, error) {
	return callTool(func(method string, params any) (json.RawMessage, error) {
		return c.request(ctx, token, method, params)
	}, name, args)
}

func callTool(request func(string, any) (json.RawMessage, error), name string, args json.RawMessage) (CallResult, error) {
	if len(args) == 0 {
		args = json.RawMessage("{}")
	}
	raw, err := request("tools/call", map[string]any{"name": name, "arguments": args})
	if err != nil {
		return CallResult{}, err
	}
	var r CallResult
	if err := json.Unmarshal(raw, &r); err != nil {
		return CallResult{}, fmt.Errorf("resultado inválido: %v", err)
	}
	return r, nil
}
