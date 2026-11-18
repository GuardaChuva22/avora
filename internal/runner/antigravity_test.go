package runner

import (
	"encoding/json"
	"slices"
	"strings"
	"testing"
	"time"

	"github.com/GuardaChuva22/avora/internal/control"
)

func TestAgyArgs(t *testing.T) {
	args, err := agyArgs("edit", "gemini-3-pro", "high")
	if err != nil {
		t.Fatal(err)
	}
	got := strings.Join(args, " ")
	for _, want := range []string{"--input-format stream-json", "--output-format stream-json", "--mode accept-edits", "--model gemini-3-pro", "--effort high"} {
		if !strings.Contains(got, want) {
			t.Fatalf("missing %q in %s", want, got)
		}
	}
	if slices.Contains(args, "--dangerously-skip-permissions") {
		t.Fatal("never skips permissions")
	}
	if read, _ := agyArgs("read", "", ""); slices.Contains(read, "--mode") {
		t.Fatalf("read: %v", read)
	}
	if _, err := agyArgs("shell", "", ""); err == nil {
		t.Fatal("unknown mode")
	}
	var in struct {
		Event   string `json:"event"`
		Message struct {
			Content string `json:"content"`
		} `json:"message"`
	}
	if json.Unmarshal([]byte(agyInput("oi \"você\"")), &in) != nil || in.Event != "user" || in.Message.Content != `oi "você"` {
		t.Fatalf("input: %+v", in)
	}
}

func TestParseAgyStream(t *testing.T) {
	stream := strings.Join([]string{
		`{"event":"init","init":{"cwd":"/w","tools":["read_file"],"permission_mode":"request-review","model":"gemini-3-pro"}}`,
		`{"event":"step_update","step_update":{"conversation_id":"c1","step_index":0,"state":"DONE","step_type":"user_input"}}`,
		`{"event":"step_update","step_update":{"conversation_id":"c1","step_index":1,"state":"ACTIVE","step_type":"tool","tool_name":"read_file"}}`,
		`{"event":"step_update","step_update":{"conversation_id":"c1","step_index":1,"state":"DONE","step_type":"tool","tool_name":"read_file","tool_info":{"name":"read_file","parameters":{"path":"notas.md"},"output":"..."}}}`,
		`{"event":"step_update","step_update":{"conversation_id":"c1","step_index":2,"state":"ACTIVE","step_type":"agent_response","text_delta":"O arquivo "}}`,
		`{"event":"step_update","step_update":{"conversation_id":"c1","step_index":2,"state":"DONE","step_type":"agent_response","text_delta":"tem 3 notas."}}`,
		`{"event":"result","result":{"conversation_id":"c1","status":"SUCCESS","response":"O arquivo tem 3 notas.","num_turns":1,"duration_seconds":4.5,"usage":{"input_tokens":120,"output_tokens":30,"thinking_tokens":10,"cache_read_tokens":5}}}`,
	}, "\n")
	var steps []control.AgentStep
	answer, isErr, stats, err := parseAgyStream(strings.NewReader(stream), func(s control.AgentStep) { steps = append(steps, s) })
	if err != nil || isErr || answer != "O arquivo tem 3 notas." {
		t.Fatalf("answer %q err %v isErr %v", answer, err, isErr)
	}
	if stats.SessionID != "c1" || stats.Model != "gemini-3-pro" || stats.OutputTokens != 40 || stats.DurationMS != 4500 || stats.Steps != 1 {
		t.Fatalf("stats: %+v", stats)
	}
	if len(steps) != 2 || steps[0].Summary != "read_file: notas.md" || steps[1].Tool != control.StepText {
		t.Fatalf("steps: %+v", steps)
	}

	failed := `{"event":"result","result":{"status":"ERROR","error":{"message":"quota exceeded"}}}`
	if answer, isErr, _, _ := parseAgyStream(strings.NewReader(failed), nil); !isErr || answer != "quota exceeded" {
		t.Fatalf("error: %q %v", answer, isErr)
	}
	if _, _, _, err := parseAgyStream(strings.NewReader(""), nil); err == nil {
		t.Fatal("no result is an error")
	}
}

func TestAgyQuota(t *testing.T) {
	raw := `{"status":"ok","command":{"data":{"groups":[
	{"name":"Claude and GPT models","buckets":[{"id":"3p-weekly","window":"weekly","remaining_fraction":0,"reset_time":"2026-11-20T10:00:00Z"}]},
	{"name":"Gemini Models","buckets":[
	 {"id":"gemini-weekly","name":"Semanal","window":"weekly","remaining_fraction":0.75,"reset_time":"2026-11-20T10:00:00Z"},
	 {"id":"gemini-5h","name":"5 horas","window":"5h","remaining_fraction":0.4,"reset_time":"2026-11-16T20:00:00Z"}]}]}}}`
	q := agyQuota([]byte(raw), time.Now())
	if q.Engine != "antigravity" || q.Error != "" || q.Limited {
		t.Fatalf("quota: %+v", q)
	}
	if q.Weekly == nil || q.Weekly.UsedPercent != 25 || q.FiveHour == nil || q.FiveHour.UsedPercent != 60 || q.FiveHour.ResetsAt == 0 {
		t.Fatalf("windows: %+v %+v", q.Weekly, q.FiveHour)
	}
	if q := agyQuota([]byte(`{"status":"error","error":"not logged in"}`), time.Now()); !strings.Contains(q.Error, "not logged in") {
		t.Fatalf("error: %+v", q)
	}
}

func TestParseAgyModelsOutput(t *testing.T) {
	jsonEnv := `{"status":"ok","command":{"data":{"models":[{"id":"gemini-4-carbon-high","display_name":"Gemini 4 Carbon (High)"},{"model":"gemini-3.8-flash-low","label":"Gemini 3.8 Flash (Low)"}]}}}`
	list := parseAgyModelsOutput([]byte(jsonEnv))
	if len(list) != 2 || list[0].ID != "gemini-4-carbon-high" || list[0].Name != "Gemini 4 Carbon (High)" || list[1].ID != "gemini-3.8-flash-low" {
		t.Fatalf("json env: %+v", list)
	}

	plain := "MODEL                     NAME\n------------------------  ---------------------\ngemini-4-carbon-medium    Gemini 4 Carbon\nclaude-sonnet-4-6         Claude Sonnet 4.6\n"
	list2 := parseAgyModelsOutput([]byte(plain))
	if len(list2) != 2 || list2[0].ID != "gemini-4-carbon-medium" || list2[0].Name != "Gemini 4 Carbon" || list2[1].ID != "claude-sonnet-4-6" {
		t.Fatalf("plain text: %+v", list2)
	}
}
