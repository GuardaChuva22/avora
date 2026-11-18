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
)

// State is what a node keeps between runs: its identity and token.
type State struct {
	NodeID string `json:"node_id"`
	Name   string `json:"name"`
	Kind   string `json:"kind"`
	Server string `json:"server"`
	Token  string `json:"token"`
}

// LoadOrPair returns the saved node state, pairing with code first when no
// state exists yet. The state file holds a secret, so it is written 0600.
func LoadOrPair(ctx context.Context, path, serverURL, code string) (State, error) {
	if state, err := loadState(path); err == nil {
		return state, nil
	} else if !errors.Is(err, os.ErrNotExist) {
		return State{}, err
	}
	if strings.TrimSpace(code) == "" {
		return State{}, fmt.Errorf("node not paired yet: set AVORA_PAIRING_CODE (state file %s)", path)
	}
	paired, err := Pair(ctx, serverURL, code)
	if err != nil {
		return State{}, err
	}
	state := State{NodeID: paired.NodeID, Name: paired.Name, Kind: string(paired.Kind), Server: serverURL, Token: paired.Token}
	if err := saveState(path, state); err != nil {
		return State{}, fmt.Errorf("paired as %s but could not save state: %w", paired.NodeID, err)
	}
	return state, nil
}

func loadState(path string) (State, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return State{}, err
	}
	var state State
	if err := json.Unmarshal(data, &state); err != nil {
		return State{}, fmt.Errorf("corrupt state file %s: %w", path, err)
	}
	if state.NodeID == "" || !strings.HasPrefix(state.Token, control.NodeTokenPrefix) {
		return State{}, fmt.Errorf("incomplete state file %s", path)
	}
	return state, nil
}

func saveState(path string, state State) error {
	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		return err
	}
	data, err := json.MarshalIndent(state, "", "  ")
	if err != nil {
		return err
	}
	tmp := path + ".tmp"
	if err := os.WriteFile(tmp, data, 0o600); err != nil {
		return err
	}
	return os.Rename(tmp, path)
}

// DefaultStatePath is %APPDATA%\Avora\node.json on Windows and
// ~/.config/avora/node.json elsewhere.
func DefaultStatePath() (string, error) {
	dir, err := os.UserConfigDir()
	if err != nil {
		return "", err
	}
	return filepath.Join(dir, "Avora", "node.json"), nil
}
