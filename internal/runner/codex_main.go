package runner

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/GuardaChuva22/avora/internal/control"
	"github.com/GuardaChuva22/avora/internal/store"
)

// Codex as the main model: when Settings → Modelos picks a Codex account,
// every chat turn runs here as a codex.main job. Codex thinks with the
// owner's ChatGPT subscription and acts only through Avora's own tools,
// which reach it as the "avora" MCP server (the bridge); its sandbox is
// read-only. The same job kind lists the models the account can use.

// A node answers up to codexMainSlots turns at once, plus nested work those
// turns wait for (a consulted colleague, a model list).
const (
	codexMainSlots   = 2
	codexNestedSlots = 3
)

// CodexModel is one model a Codex account offers.
type CodexModel struct {
	ID   string `json:"id"`
	Name string `json:"name"`
}

// RunCodexMain runs one codex.main job: "models" lists the account's
// models; "chat" and "consult" answer one turn.
func RunCodexMain(ctx context.Context, cfg AgentConfig, spec store.AgentSpec, onStep func(control.AgentStep), upload func(name string, data []byte) (string, error)) control.JobResult {
	cfg, err := cfg.withAccount(store.AgentSpec{Engine: "codex", Account: spec.Account})
	if err != nil {
		return control.JobResult{Error: err.Error()}
	}
	if spec.Mode == store.CodexModelsMode {
		models, err := listCodexModels(ctx, cfg)
		if err != nil {
			return control.JobResult{Error: clip(err.Error(), 2000)}
		}
		b, _ := json.Marshal(models)
		return control.JobResult{Success: true, Output: string(b)}
	}
	run := spec
	run.Engine, run.Mode, run.ConversationID, run.Resume = "codex", "read", "", ""
	result := RunCodex(ctx, cfg, run, func(st control.AgentStep) {
		// Calls to Avora's tools show up as the turn's own steps already.
		if st.Tool != "MCP" && onStep != nil {
			onStep(st)
		}
	})
	if result.Success && upload != nil {
		result.Output += generatedImages(cfg.CodexHome, result.Stats, upload)
	}
	return result
}

// generatedImages sends the images Codex drew in this turn to the server
// and returns them as Markdown for the answer. Codex keeps them in
// $CODEX_HOME/generated_images/<thread id>/ and only says so in its own
// history, so without this the owner never saw them.
func generatedImages(home string, statsJSON []byte, upload func(string, []byte) (string, error)) string {
	var stats struct {
		SessionID string `json:"session_id"`
	}
	if home == "" || json.Unmarshal(statsJSON, &stats) != nil || stats.SessionID == "" || filepath.Base(stats.SessionID) != stats.SessionID || strings.HasPrefix(stats.SessionID, ".") {
		return ""
	}
	dir := filepath.Join(home, "generated_images", stats.SessionID)
	entries, err := os.ReadDir(dir)
	if err != nil {
		return ""
	}
	var out strings.Builder
	for i, e := range entries {
		if i >= 8 || e.IsDir() {
			continue
		}
		data, err := os.ReadFile(filepath.Join(dir, e.Name()))
		if err != nil || len(data) == 0 || len(data) > store.MaxFileBytes {
			continue
		}
		id, err := upload(e.Name(), data)
		if err != nil {
			continue
		}
		fmt.Fprintf(&out, "\n\n![imagem gerada](avora:file/%s)", id)
	}
	return out.String()
}

// listCodexModels asks Codex's app-server which models the account can
// pick, in Codex's own order.
func listCodexModels(ctx context.Context, cfg AgentConfig) ([]CodexModel, error) {
	if !installed(cfg.CodexBin) {
		return nil, errors.New("Codex not installed on this node (" + cfg.CodexBin + ")")
	}
	out := []CodexModel{}
	cursor := ""
	for page := 0; page < 5; page++ {
		params := map[string]any{}
		if cursor != "" {
			params["cursor"] = cursor
		}
		raw, err := codexAppServer(ctx, cfg, "model/list", params)
		if err != nil {
			return nil, err
		}
		var list struct {
			Data []struct {
				ID          string `json:"id"`
				Model       string `json:"model"`
				DisplayName string `json:"displayName"`
				Hidden      bool   `json:"hidden"`
			} `json:"data"`
			NextCursor string `json:"nextCursor"`
		}
		if err := json.Unmarshal(raw, &list); err != nil {
			return nil, errors.New("unexpected Codex answer to model/list")
		}
		for _, m := range list.Data {
			id := m.Model
			if id == "" {
				id = m.ID
			}
			if m.Hidden || id == "" {
				continue
			}
			name := m.DisplayName
			if name == "" {
				name = id
			}
			out = append(out, CodexModel{ID: clip(id, 100), Name: clip(name, 100)})
		}
		if list.NextCursor == "" || len(out) >= 60 {
			break
		}
		cursor = list.NextCursor
	}
	if len(out) == 0 {
		return nil, errors.New("this Codex account has no models: is it logged in?")
	}
	return out, nil
}
