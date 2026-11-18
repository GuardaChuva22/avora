package mcp

import (
	"bufio"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"strconv"
)

// StdioClient speaks MCP to one subprocess. Use it for one operation at a
// time and close the subprocess when the operation is done.
type StdioClient struct {
	out *json.Encoder
	in  *bufio.Scanner
	id  int64
}

func NewStdioClient(stdin io.Writer, stdout io.Reader) *StdioClient {
	scanner := bufio.NewScanner(stdout)
	scanner.Buffer(make([]byte, 4096), maxBody)
	return &StdioClient{out: json.NewEncoder(stdin), in: scanner}
}

func (c *StdioClient) Initialize() (ServerInfo, error) {
	raw, err := c.request("initialize", initializeParams())
	if err != nil {
		return ServerInfo{}, err
	}
	var r initializeResult
	if err := json.Unmarshal(raw, &r); err != nil || r.ProtocolVersion == "" || r.ServerInfo.Name == "" {
		return ServerInfo{}, errors.New("resposta inválida à inicialização do MCP")
	}
	err = c.out.Encode(rpcMessage{JSONRPC: "2.0", Method: "notifications/initialized"})
	return r.ServerInfo, err
}

func (c *StdioClient) ListTools() ([]Tool, error) { return listTools(c.request) }

func (c *StdioClient) CallTool(name string, args json.RawMessage) (CallResult, error) {
	return callTool(c.request, name, args)
}

func (c *StdioClient) request(method string, params any) (json.RawMessage, error) {
	c.id++
	msg := map[string]any{"jsonrpc": "2.0", "id": c.id, "method": method}
	if params != nil {
		msg["params"] = params
	}
	if err := c.out.Encode(msg); err != nil {
		return nil, err
	}
	want := strconv.FormatInt(c.id, 10)
	for c.in.Scan() {
		if len(c.in.Bytes()) == 0 {
			continue
		}
		var response rpcMessage
		if err := json.Unmarshal(c.in.Bytes(), &response); err != nil || response.JSONRPC != "2.0" {
			return nil, errors.New("resposta inválida em stdout do MCP")
		}
		switch {
		case response.Method == "" && string(response.ID) == want:
			return response.answer(want)
		case response.Method != "" && len(response.ID) > 0:
			reply := rpcMessage{JSONRPC: "2.0", ID: response.ID, Result: json.RawMessage(`{}`)}
			if response.Method != "ping" {
				reply.Result = nil
				reply.Error = &RPCError{Code: -32601, Message: "Method not found"}
			}
			if err := c.out.Encode(reply); err != nil {
				return nil, err
			}
		}
	}
	if err := c.in.Err(); err != nil {
		return nil, err
	}
	return nil, fmt.Errorf("o processo MCP fechou stdout sem responder a %s", method)
}
