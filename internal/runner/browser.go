package runner

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/GuardaChuva22/avora/internal/browser"
	"github.com/GuardaChuva22/avora/internal/control"
)

// NewBrowser is the agents' browser of this node, or nil when it has none:
// off by default (the agents' browser is the panel inside the app);
// AVORA_BROWSER=1 turns it on, for when the app is closed. Profiles live under
// %LOCALAPPDATA%\Avora\navegador (one folder per agent when call.Agent is set).
func NewBrowser() *browser.Browser {
	if os.Getenv("AVORA_BROWSER") != "1" {
		return nil // the agents' browser is the panel inside the app
	}
	exe := os.Getenv("AVORA_BROWSER_EXE")
	if exe == "" {
		exe = browser.FindChrome()
	}
	if exe == "" {
		return nil
	}
	base := os.Getenv("LOCALAPPDATA")
	if base == "" {
		home, _ := os.UserHomeDir()
		base = filepath.Join(home, ".local", "share")
	}
	return &browser.Browser{Exe: exe, Profile: filepath.Join(base, "Avora", "navegador"), Headless: os.Getenv("AVORA_BROWSER_HEADLESS") == "1"}
}

func cleanBrowserAgent(id string) string {
	var out strings.Builder
	for _, r := range strings.ToLower(strings.TrimSpace(id)) {
		if (r >= 'a' && r <= 'z') || (r >= '0' && r <= '9') || r == '-' || r == '_' {
			out.WriteRune(r)
		}
	}
	return out.String()
}

// browserLoop takes browser commands from the server (a long poll) and runs
// them in the calling agent's own Chrome profile.
func (c *Client) browserLoop(ctx context.Context, b *browser.Browser) {
	byAgent := map[string]*browser.Browser{}
	defer func() {
		b.Close()
		for _, ab := range byAgent {
			ab.Close()
		}
	}()
	forAgent := func(agent string) *browser.Browser {
		id := cleanBrowserAgent(agent)
		if id == "" {
			return b
		}
		if ab := byAgent[id]; ab != nil {
			return ab
		}
		ab := &browser.Browser{
			Exe:      b.Exe,
			Profile:  filepath.Join(b.Profile, "agent-"+id),
			Headless: b.Headless,
		}
		byAgent[id] = ab
		return ab
	}
	long := &http.Client{Timeout: 45 * time.Second}
	backoff := time.Second
	for ctx.Err() == nil {
		call, err := c.nextBrowserCall(ctx, long)
		if err != nil {
			if errors.Is(err, ErrUnauthorized) {
				return
			}
			select {
			case <-ctx.Done():
				return
			case <-time.After(backoff):
			}
			backoff = min(backoff*2, 30*time.Second)
			continue
		}
		backoff = time.Second
		if call == nil {
			continue
		}
		cctx, cancel := context.WithTimeout(ctx, 2*time.Minute)
		res := forAgent(call.Agent).Do(cctx, call.Command)
		cancel()
		c.answerBrowser(ctx, long, control.BrowserAnswer{ID: call.ID, Agent: call.Agent, Result: res})
	}
}

func (c *Client) nextBrowserCall(ctx context.Context, long *http.Client) (*control.BrowserCall, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, c.base+"/v1/runtime/browser/next", nil)
	if err != nil {
		return nil, err
	}
	req.Header.Set("Authorization", "Bearer "+c.token)
	resp, err := long.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	switch resp.StatusCode {
	case http.StatusNoContent:
		return nil, nil
	case http.StatusUnauthorized:
		return nil, ErrUnauthorized
	case http.StatusOK:
		var call control.BrowserCall
		if err := json.NewDecoder(resp.Body).Decode(&call); err != nil {
			return nil, err
		}
		return &call, nil
	}
	return nil, errors.New("browser poll: " + resp.Status)
}

func (c *Client) answerBrowser(ctx context.Context, long *http.Client, a control.BrowserAnswer) {
	body, _ := json.Marshal(a)
	for attempt := 0; attempt < 3; attempt++ {
		req, err := http.NewRequestWithContext(ctx, http.MethodPost, c.base+"/v1/runtime/browser/result", bytes.NewReader(body))
		if err != nil {
			return
		}
		req.Header.Set("Authorization", "Bearer "+c.token)
		req.Header.Set("Content-Type", "application/json")
		resp, err := long.Do(req)
		if err == nil {
			resp.Body.Close()
			if resp.StatusCode < 500 {
				return
			}
		}
		time.Sleep(time.Second)
	}
}
