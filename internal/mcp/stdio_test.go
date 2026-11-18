package mcp

import (
	"bytes"
	"encoding/json"
	"errors"
	"strings"
	"testing"
)

func TestStdioClient(t *testing.T) {
	var stdin bytes.Buffer
	stdout := strings.NewReader(`{"jsonrpc":"2.0","id":1,"result":{"protocolVersion":"2024-11-05","serverInfo":{"name":"test"}}}
{"jsonrpc":"2.0","method":"notifications/message"}
{"jsonrpc":"2.0","method":"ping","id":"server-ping"}
{"jsonrpc":"2.0","id":2,"result":{"tools":[{"name":"first"}],"nextCursor":"page-2"}}
{"jsonrpc":"2.0","id":3,"result":{"tools":[{"name":"second"}]}}
{"jsonrpc":"2.0","id":4,"result":{"content":[{"type":"text","text":"tool result"}]}}
{"jsonrpc":"2.0","id":5,"error":{"code":-32602,"message":"invalid arguments"}}
`)
	c := NewStdioClient(&stdin, stdout)
	if info, err := c.Initialize(); err != nil || info.Name != "test" {
		t.Fatalf("initialize: %+v %v", info, err)
	}
	if tools, err := c.ListTools(); err != nil || len(tools) != 2 || tools[1].Name != "second" {
		t.Fatalf("list tools: %+v %v", tools, err)
	}
	if res, err := c.CallTool("second", json.RawMessage(`{"a":1}`)); err != nil || len(res.Content) != 1 || res.Content[0].Text != "tool result" {
		t.Fatalf("call tool: %+v %v", res, err)
	}
	var rpcErr *RPCError
	if _, err := c.CallTool("second", nil); !errors.As(err, &rpcErr) || rpcErr.Code != -32602 {
		t.Fatalf("expected RPC error, got %v", err)
	}
	for _, want := range []string{
		`"method":"notifications/initialized"`,
		`"id":"server-ping","result":{}`,
		`"method":"tools/list","params":{"cursor":"page-2"}`,
		`"method":"tools/call","params":{"arguments":{"a":1},"name":"second"}`,
	} {
		if !strings.Contains(stdin.String(), want) {
			t.Fatalf("missing %s in %s", want, stdin.String())
		}
	}
}
