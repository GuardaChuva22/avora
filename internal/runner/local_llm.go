package runner

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
	"path/filepath"
	"sort"
	"strings"
	"sync"
	"time"

	"github.com/GuardaChuva22/avora/internal/control"
)

const (
	localModelScanTTL = 15 * time.Second
	localProbeTimeout = 800 * time.Millisecond
)

// LocalProbeTarget names one local OpenAI-compatible server to probe on the
// runner's own machine.
type LocalProbeTarget struct {
	Kind    string
	BaseURL string
}

var defaultLocalProbes = []LocalProbeTarget{
	{Kind: "ollama", BaseURL: "http://127.0.0.1:11434/v1"},
	{Kind: "lmstudio", BaseURL: "http://127.0.0.1:1234/v1"},
}

type localModelCache struct {
	mu    sync.Mutex
	at    time.Time
	found []control.LocalModelReport
}

func (c *Client) detectedLocalModels(ctx context.Context) []control.LocalModelReport {
	c.localModels.mu.Lock()
	defer c.localModels.mu.Unlock()
	if !c.localModels.at.IsZero() && time.Since(c.localModels.at) < localModelScanTTL {
		return cloneLocalReports(c.localModels.found)
	}
	probes := c.LocalProbes
	if len(probes) == 0 {
		probes = defaultLocalProbes
	}
	found := DetectLocalModelsFrom(ctx, probes)
	c.localModels.at = time.Now()
	c.localModels.found = found
	return cloneLocalReports(found)
}

func cloneLocalReports(in []control.LocalModelReport) []control.LocalModelReport {
	if len(in) == 0 {
		return nil
	}
	out := make([]control.LocalModelReport, len(in))
	for i, m := range in {
		out[i] = control.LocalModelReport{
			Kind:   m.Kind,
			URL:    m.URL,
			Models: append([]string{}, m.Models...),
		}
	}
	return out
}

func (c *Client) sharedDirPath() string {
	if strings.TrimSpace(c.Agents.Workspace) == "" {
		return ""
	}
	return filepath.Join(filepath.Clean(c.Agents.Workspace), "shared")
}

// DetectLocalModels checks whether Ollama (11434) or LM Studio (1234) is
// listening on loopback and returns the models each one serves.
func DetectLocalModels(ctx context.Context) []control.LocalModelReport {
	return DetectLocalModelsFrom(ctx, defaultLocalProbes)
}

// DetectLocalModelsFrom probes the given loopback endpoints concurrently.
func DetectLocalModelsFrom(ctx context.Context, targets []LocalProbeTarget) []control.LocalModelReport {
	if len(targets) == 0 {
		return nil
	}
	results := make([]*control.LocalModelReport, len(targets))
	var wg sync.WaitGroup
	for i, target := range targets {
		wg.Add(1)
		go func(idx int, t LocalProbeTarget) {
			defer wg.Done()
			if rep, ok := probeOneLocalServer(ctx, t); ok {
				results[idx] = &rep
			}
		}(i, target)
	}
	wg.Wait()
	out := make([]control.LocalModelReport, 0, len(targets))
	for _, r := range results {
		if r != nil {
			out = append(out, *r)
		}
	}
	return out
}

func probeOneLocalServer(ctx context.Context, t LocalProbeTarget) (control.LocalModelReport, bool) {
	base, err := resolveLocalLLMURL(t.Kind, t.BaseURL)
	if err != nil {
		return control.LocalModelReport{}, false
	}
	pctx, cancel := context.WithTimeout(ctx, localProbeTimeout)
	defer cancel()
	client := &http.Client{Timeout: localProbeTimeout}

	if models, ok := fetchOpenAIModels(pctx, client, base); ok {
		return control.LocalModelReport{Kind: t.Kind, URL: base, Models: models}, true
	}
	if t.Kind == "ollama" {
		if models, ok := fetchOllamaTags(pctx, client, base); ok {
			return control.LocalModelReport{Kind: t.Kind, URL: base, Models: models}, true
		}
	}
	return control.LocalModelReport{}, false
}

func fetchOpenAIModels(ctx context.Context, client *http.Client, base string) ([]string, bool) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, base+"/models", nil)
	if err != nil {
		return nil, false
	}
	resp, err := client.Do(req)
	if err != nil {
		return nil, false
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return nil, false
	}
	var body struct {
		Data []struct {
			ID string `json:"id"`
		} `json:"data"`
	}
	if err := json.NewDecoder(io.LimitReader(resp.Body, 2<<20)).Decode(&body); err != nil {
		return nil, false
	}
	models := make([]string, 0, len(body.Data))
	for _, m := range body.Data {
		if id := strings.TrimSpace(m.ID); id != "" {
			models = append(models, id)
		}
	}
	sort.Strings(models)
	return models, true
}

func fetchOllamaTags(ctx context.Context, client *http.Client, base string) ([]string, bool) {
	u, err := url.Parse(base)
	if err != nil {
		return nil, false
	}
	tagsURL := u.Scheme + "://" + u.Host + "/api/tags"
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, tagsURL, nil)
	if err != nil {
		return nil, false
	}
	resp, err := client.Do(req)
	if err != nil {
		return nil, false
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return nil, false
	}
	var body struct {
		Models []struct {
			Name string `json:"name"`
		} `json:"models"`
	}
	if err := json.NewDecoder(io.LimitReader(resp.Body, 2<<20)).Decode(&body); err != nil {
		return nil, false
	}
	models := make([]string, 0, len(body.Models))
	for _, m := range body.Models {
		if id := strings.TrimSpace(m.Name); id != "" {
			models = append(models, id)
		}
	}
	sort.Strings(models)
	return models, true
}

// resolveLocalLLMURL normalizes a local model base URL to 127.0.0.1 and
// rejects any non-loopback destination so the runner cannot be used to reach
// arbitrary hosts on the LAN or internet.
func resolveLocalLLMURL(service, raw string) (string, error) {
	base := strings.TrimSpace(raw)
	if base == "" {
		switch service {
		case "lmstudio":
			base = "http://127.0.0.1:1234/v1"
		default:
			base = "http://127.0.0.1:11434/v1"
		}
	}
	u, err := url.Parse(base)
	if err != nil || u == nil || u.Host == "" {
		return "", errors.New("invalid local model URL")
	}
	if u.Scheme != "http" && u.Scheme != "https" {
		return "", errors.New("local model URL must use http or https")
	}
	if u.User != nil || u.RawQuery != "" || u.Fragment != "" {
		return "", errors.New("local model URL must not contain credentials or query")
	}
	host := u.Hostname()
	ip := net.ParseIP(host)
	if !strings.EqualFold(host, "localhost") && (ip == nil || !ip.IsLoopback()) {
		return "", errors.New("local model URL must point to loopback (127.0.0.1)")
	}
	port := u.Port()
	if strings.EqualFold(host, "localhost") {
		if port != "" {
			u.Host = net.JoinHostPort("127.0.0.1", port)
		} else {
			u.Host = "127.0.0.1"
		}
	}
	if u.Path == "" || u.Path == "/" {
		u.Path = "/v1"
	}
	return strings.TrimRight(u.String(), "/"), nil
}

func (c *Client) llmLoop(ctx context.Context) {
	long := &http.Client{Timeout: 45 * time.Second}
	backoff := time.Second
	for ctx.Err() == nil {
		call, err := c.nextLLMCall(ctx, long)
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
		c.wg.Add(1)
		go func(call control.LocalLLMCall) {
			defer c.wg.Done()
			c.handleLLMCall(ctx, long, call)
		}(*call)
	}
}

// PollLLMOnce executes a single poll+dispatch cycle against the server's local
// LLM queue.
func (c *Client) PollLLMOnce(ctx context.Context) (bool, error) {
	long := &http.Client{Timeout: 45 * time.Second}
	call, err := c.nextLLMCall(ctx, long)
	if err != nil || call == nil {
		return false, err
	}
	c.handleLLMCall(ctx, long, *call)
	return true, nil
}

func (c *Client) nextLLMCall(ctx context.Context, long *http.Client) (*control.LocalLLMCall, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, c.base+"/v1/runtime/llm/next", nil)
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
		var call control.LocalLLMCall
		if err := json.NewDecoder(io.LimitReader(resp.Body, 4<<20)).Decode(&call); err != nil {
			return nil, err
		}
		return &call, nil
	}
	return nil, fmt.Errorf("llm poll: %s", resp.Status)
}

func (c *Client) sendLLMResult(ctx context.Context, client *http.Client, res control.LocalLLMResult) error {
	body, err := json.Marshal(res)
	if err != nil {
		return err
	}
	for attempt := 0; attempt < 3; attempt++ {
		sctx, cancel := context.WithTimeout(context.WithoutCancel(ctx), 15*time.Second)
		req, rerr := http.NewRequestWithContext(sctx, http.MethodPost, c.base+"/v1/runtime/llm/result", bytes.NewReader(body))
		if rerr != nil {
			cancel()
			return rerr
		}
		req.Header.Set("Authorization", "Bearer "+c.token)
		req.Header.Set("Content-Type", "application/json")
		resp, derr := client.Do(req)
		cancel()
		if derr == nil {
			resp.Body.Close()
			if resp.StatusCode < 500 {
				return nil
			}
		}
		if ctx.Err() != nil {
			return ctx.Err()
		}
		time.Sleep(300 * time.Millisecond)
	}
	return errors.New("failed to report local llm result")
}

func (c *Client) handleLLMCall(ctx context.Context, reportClient *http.Client, call control.LocalLLMCall) {
	base, err := resolveLocalLLMURL(call.Service, call.BaseURL)
	if err != nil {
		_ = c.sendLLMResult(ctx, reportClient, control.LocalLLMResult{
			ID: call.ID, Seq: 1, Error: err.Error(), Done: true,
		})
		return
	}
	if call.Kind == "models" {
		c.handleLocalModelsCall(ctx, reportClient, call, base)
		return
	}
	c.handleLocalChatCall(ctx, reportClient, call, base)
}

func (c *Client) handleLocalModelsCall(ctx context.Context, reportClient *http.Client, call control.LocalLLMCall, base string) {
	mctx, cancel := context.WithTimeout(ctx, 15*time.Second)
	defer cancel()
	localHTTP := &http.Client{Timeout: 15 * time.Second}
	req, err := http.NewRequestWithContext(mctx, http.MethodGet, base+"/models", nil)
	if err != nil {
		_ = c.sendLLMResult(ctx, reportClient, control.LocalLLMResult{ID: call.ID, Seq: 1, Error: err.Error(), Done: true})
		return
	}
	resp, err := localHTTP.Do(req)
	if err != nil {
		_ = c.sendLLMResult(ctx, reportClient, control.LocalLLMResult{ID: call.ID, Seq: 1, Error: err.Error(), Done: true})
		return
	}
	defer resp.Body.Close()
	data, _ := io.ReadAll(io.LimitReader(resp.Body, 2<<20))

	if resp.StatusCode == http.StatusNotFound && call.Service == "ollama" {
		if models, ok := fetchOllamaTags(mctx, localHTTP, base); ok {
			type item struct {
				ID string `json:"id"`
			}
			items := make([]item, len(models))
			for i, m := range models {
				items[i] = item{ID: m}
			}
			synthesized, _ := json.Marshal(map[string]any{"data": items})
			_ = c.sendLLMResult(ctx, reportClient, control.LocalLLMResult{
				ID: call.ID, Seq: 1, Status: http.StatusOK, Chunk: string(synthesized), Done: true,
			})
			return
		}
	}

	_ = c.sendLLMResult(ctx, reportClient, control.LocalLLMResult{
		ID: call.ID, Seq: 1, Status: resp.StatusCode, Chunk: string(data), Done: true,
	})
}

func (c *Client) handleLocalChatCall(ctx context.Context, reportClient *http.Client, call control.LocalLLMCall, base string) {
	cctx, cancel := context.WithTimeout(ctx, 10*time.Minute)
	defer cancel()
	req, err := http.NewRequestWithContext(cctx, http.MethodPost, base+"/chat/completions", bytes.NewReader(call.Body))
	if err != nil {
		_ = c.sendLLMResult(ctx, reportClient, control.LocalLLMResult{ID: call.ID, Seq: 1, Error: err.Error(), Done: true})
		return
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Accept", "text/event-stream")

	resp, err := (&http.Client{}).Do(req)
	if err != nil {
		_ = c.sendLLMResult(ctx, reportClient, control.LocalLLMResult{ID: call.ID, Seq: 1, Error: err.Error(), Done: true})
		return
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		errBody, _ := io.ReadAll(io.LimitReader(resp.Body, 4096))
		_ = c.sendLLMResult(ctx, reportClient, control.LocalLLMResult{
			ID: call.ID, Seq: 1, Status: resp.StatusCode, Chunk: string(errBody), Done: true,
		})
		return
	}

	scanner := bufio.NewScanner(resp.Body)
	scanner.Buffer(make([]byte, 64<<10), 2<<20)
	seq := 0
	for scanner.Scan() {
		line := strings.TrimSpace(scanner.Text())
		if line == "" || strings.HasPrefix(line, ":") {
			continue
		}
		if strings.HasPrefix(line, "data:") {
			seq++
			if err := c.sendLLMResult(ctx, reportClient, control.LocalLLMResult{
				ID: call.ID, Seq: seq, Status: http.StatusOK, Chunk: line + "\n\n", Done: false,
			}); err != nil {
				return
			}
		}
	}
	seq++
	if err := scanner.Err(); err != nil {
		_ = c.sendLLMResult(ctx, reportClient, control.LocalLLMResult{
			ID: call.ID, Seq: seq, Status: http.StatusOK, Error: err.Error(), Done: true,
		})
		return
	}
	_ = c.sendLLMResult(ctx, reportClient, control.LocalLLMResult{
		ID: call.ID, Seq: seq, Status: http.StatusOK, Done: true,
	})
}
