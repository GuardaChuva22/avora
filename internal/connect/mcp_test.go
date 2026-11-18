package connect

import (
	"encoding/json"
	"fmt"
	"strings"
	"testing"

	"github.com/GuardaChuva22/avora/internal/mcp"
)

func TestMCPRisk(t *testing.T) {
	yes := true
	cases := map[string]Risk{
		"clickup_get_task": Read, "clickup_filter_tasks": Read, "clickup_remove_tag_from_task": Delete, "execute_operator": Write, "search_files": Read, "notion-fetch": Read,
		"clickup_create_task": Write, "notion-update-page": Write, "clickup_add_tag_to_task": Write,
		"clickup_create_task_comment": Send, "clickup_send_chat_message": Send, "share_file": Send, "publishPost": Send,
		"clickup_delete_task": Delete, "trash_file": Delete, "archive_project": Delete,
		"create_refund": Spend, "create_payment_link": Spend, "list_payments": Read, "get_invoice": Read,
		"clickup_get_chat_channel_messages": Read,
	}
	for name, want := range cases {
		if got := MCPRisk(mcp.Tool{Name: name}); got != want {
			t.Errorf("%s: %s, want %s", name, got, want)
		}
	}
	if MCPRisk(mcp.Tool{Name: "do_thing", Annotations: &mcp.Annotations{ReadOnlyHint: &yes}}) != Read {
		t.Error("read-only hint")
	}
	if MCPRisk(mcp.Tool{Name: "update_row", Annotations: &mcp.Annotations{DestructiveHint: &yes}}) != Delete {
		t.Error("destructive hint")
	}
}

func TestMCPBigAppUsesMetaTools(t *testing.T) {
	app := &MCPApp{Kind: "clickup", Name: "ClickUp"}
	tools := []mcp.Tool{}
	for i := 0; i < 30; i++ {
		tools = append(tools, mcp.Tool{Name: fmt.Sprintf("clickup_get_thing_%02d", i), Description: "Reads a thing."})
	}
	tools = append(tools, mcp.Tool{Name: "clickup_send_chat_message", Description: "Sends.",
		InputSchema: []byte(`{"type":"object","properties":{"text":{"type":"string"}}}`)})
	tools = append(tools, mcp.Tool{Name: strings.Repeat("very_long_name_", 6), Description: "Long."})
	app.Restore([]byte(fmt.Sprintf(`{"tools":%s}`, mustJSON(tools))))

	got := app.Tools()
	if len(got) != 2 || got[0].Name != "clickup_ferramentas" || got[1].Name != "clickup_usar" || len(app.AllTools()) != 32 {
		t.Fatalf("tools: %d %v", len(got), got[0].Name)
	}
	if !strings.Contains(got[0].Description, "clickup_send_chat_message 🔒") {
		t.Fatalf("list: %s", got[0].Description)
	}
	use := got[1]
	send := Args{"ferramenta": "clickup_send_chat_message", "argumentos": map[string]any{"text": "oi"}}
	if !use.RiskFor(send).Locked() || use.RiskFor(Args{"ferramenta": "clickup_get_thing_01"}).Locked() {
		t.Fatal("inner risk")
	}
	if s := use.SummaryFor(send); !strings.Contains(s, `"text":"oi"`) {
		t.Fatalf("summary: %s", s)
	}
	for _, tl := range app.AllTools() {
		if len(tl.Name) > 64 || !strings.HasPrefix(tl.Name, "clickup_") {
			t.Fatalf("name: %s", tl.Name)
		}
	}
	out, err := app.Call(nil, &Env{}, "clickup_ferramentas", Args{"nomes": []any{"clickup_send_chat_message"}})
	if err != nil || !strings.Contains(out, `"text":{"type":"string"}`) || !strings.Contains(out, "pede aprovação") {
		t.Fatalf("describe: %s %v", out, err)
	}
}

func mustJSON(v any) string {
	b, err := json.Marshal(v)
	if err != nil {
		panic(err)
	}
	return string(b)
}
