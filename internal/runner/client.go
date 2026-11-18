// Package runner implements the Avora runtime's task protocol. It starts
// agent CLIs under bounded modes and runs Linux jobs in a sandbox.
package runner

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"github.com/GuardaChuva22/avora/internal/browser"
	"io"
	"net"
	"net/http"
	"net/url"
	"os"
	"runtime"
	"strings"
	"sync"
	"time"

	"github.com/GuardaChuva22/avora/internal/control"
	"github.com/GuardaChuva22/avora/internal/store"
)

// ErrUnauthorized means the server rejected this node's token: it was
// revoked or never existed. The node must be paired again.
var ErrUnauthorized = errors.New("node token rejected by the server (revoked?); pair this node again")

type Client struct {
	base  string
	token string
	http  *http.Client
	// Agents configures agent.run jobs; MaxAgents 0 disables them.
	Agents AgentConfig
	// Browser is the agents' browser on this node (nil = none).
	Browser *browser.Browser
	// LocalProbes overrides the loopback addresses scanned for local models (tests).
	LocalProbes []LocalProbeTarget

	quota       *quotaTracker
	localModels localModelCache

	mu      sync.Mutex
	running map[string]context.CancelFunc
	mcpJob  string
	// codex holds the codex.main jobs running here: true = nested lane.
	codex map[string]bool
	wg    sync.WaitGroup
}

// ValidateServerURL accepts https URLs, or plain http on loopback only.
func ValidateServerURL(rawURL string) (string, error) {
	u, err := url.Parse(rawURL)
	if err != nil || u == nil || u.Host == "" {
		return "", errors.New("invalid AVORA_SERVER_URL")
	}
	if u.User != nil || u.RawQuery != "" || u.Fragment != "" || u.Path != "" && u.Path != "/" {
		return "", errors.New("server URL must not contain credentials, path or query")
	}
	if u.Scheme != "https" {
		host := u.Hostname()
		ip := net.ParseIP(host)
		if u.Scheme != "http" || (host != "localhost" && (ip == nil || !ip.IsLoopback())) {
			return "", errors.New("remote server requires HTTPS")
		}
	}
	return strings.TrimSuffix(rawURL, "/"), nil
}

func NewClient(rawURL, token string) (*Client, error) {
	base, err := ValidateServerURL(rawURL)
	if err != nil {
		return nil, err
	}
	if !strings.HasPrefix(token, control.NodeTokenPrefix) {
		return nil, errors.New("invalid node token")
	}
	return &Client{base: base, token: token, http: &http.Client{Timeout: 10 * time.Second},
		running: map[string]context.CancelFunc{}, quota: newQuotaTracker()}, nil
}

// Pair redeems a one-time pairing code and returns this node's identity and token.
func Pair(ctx context.Context, rawURL, code string) (control.PairResponse, error) {
	base, err := ValidateServerURL(rawURL)
	if err != nil {
		return control.PairResponse{}, err
	}
	payload, err := json.Marshal(control.PairRequest{Code: code})
	if err != nil {
		return control.PairResponse{}, err
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, base+"/v1/runtime/pair", bytes.NewReader(payload))
	if err != nil {
		return control.PairResponse{}, err
	}
	req.Header.Set("Content-Type", "application/json")
	resp, err := (&http.Client{Timeout: 15 * time.Second}).Do(req)
	if err != nil {
		return control.PairResponse{}, err
	}
	defer resp.Body.Close()
	if resp.StatusCode == http.StatusUnauthorized {
		return control.PairResponse{}, errors.New("pairing code invalid, already used or expired")
	}
	if resp.StatusCode != http.StatusCreated {
		return control.PairResponse{}, fmt.Errorf("pairing failed with status %d", resp.StatusCode)
	}
	var paired control.PairResponse
	if err := json.NewDecoder(io.LimitReader(resp.Body, 8192)).Decode(&paired); err != nil {
		return control.PairResponse{}, err
	}
	if paired.NodeID == "" || !strings.HasPrefix(paired.Token, control.NodeTokenPrefix) {
		return control.PairResponse{}, errors.New("server returned an invalid pairing response")
	}
	return paired, nil
}

func (c *Client) post(ctx context.Context, path string, body any) (*http.Response, error) {
	var reader io.Reader
	if body != nil {
		payload, err := json.Marshal(body)
		if err != nil {
			return nil, err
		}
		reader = bytes.NewReader(payload)
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, c.base+path, reader)
	if err != nil {
		return nil, err
	}
	req.Header.Set("Authorization", "Bearer "+c.token)
	if body != nil {
		req.Header.Set("Content-Type", "application/json")
	}
	resp, err := c.http.Do(req)
	if err == nil && resp.StatusCode == http.StatusUnauthorized {
		resp.Body.Close()
		return nil, ErrUnauthorized
	}
	return resp, err
}

// Heartbeat reports presence and the jobs still running here, and returns the
// ones the server wants stopped.
func (c *Client) Heartbeat(ctx context.Context, running []string) ([]string, error) {
	resp, err := c.post(ctx, "/v1/runtime/heartbeat", control.HeartbeatRequest{
		Running:     running,
		Quotas:      c.quota.snapshot(),
		Tools:       InstalledTools(ctx),
		LocalModels: c.detectedLocalModels(ctx),
		SharedDir:   c.sharedDirPath(),
	})
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("heartbeat status %d", resp.StatusCode)
	}
	var hb control.HeartbeatResponse
	if err := json.NewDecoder(io.LimitReader(resp.Body, 8192)).Decode(&hb); err != nil {
		return nil, err
	}
	return hb.Stop, nil
}

// Claim asks for the next job the node has a slot for.
func (c *Client) Claim(ctx context.Context, slots control.ClaimRequest) (*control.Job, error) {
	resp, err := c.post(ctx, "/v1/runtime/claim", slots)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	if resp.StatusCode == http.StatusNoContent {
		return nil, nil
	}
	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("claim status %d", resp.StatusCode)
	}
	// A chat turn's prompt can reach 120 KB (codex.main and Claude brains).
	var job control.Job
	if err := json.NewDecoder(io.LimitReader(resp.Body, 1<<20)).Decode(&job); err != nil {
		return nil, err
	}
	return &job, nil
}

func (c *Client) Complete(ctx context.Context, jobID string, result control.JobResult) error {
	if jobID == "" || strings.ContainsAny(jobID, "/\\?#") {
		return errors.New("invalid job ID")
	}
	resp, err := c.post(ctx, "/v1/runtime/jobs/"+jobID+"/complete", result)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return fmt.Errorf("completion status %d", resp.StatusCode)
	}
	return nil
}

// Step reports one step of a running agent job to the audit log.
func (c *Client) Step(ctx context.Context, jobID string, step control.AgentStep) error {
	if jobID == "" || strings.ContainsAny(jobID, "/\\?#") {
		return errors.New("invalid job ID")
	}
	resp, err := c.post(ctx, "/v1/runtime/jobs/"+jobID+"/steps", step)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusNoContent {
		return fmt.Errorf("step status %d", resp.StatusCode)
	}
	return nil
}

// uploadJobFile sends a file a run made (an image Codex drew) to the server
// and returns its id, for avora:file/<id> in the answer.
func (c *Client) uploadJobFile(ctx context.Context, jobID, name string, data []byte) (string, error) {
	if jobID == "" || strings.ContainsAny(jobID, "/\\?#") {
		return "", errors.New("invalid job ID")
	}
	upCtx, cancel := context.WithTimeout(ctx, 2*time.Minute)
	defer cancel()
	req, err := http.NewRequestWithContext(upCtx, http.MethodPost, c.base+"/v1/runtime/jobs/"+jobID+"/files?name="+url.QueryEscape(name), bytes.NewReader(data))
	if err != nil {
		return "", err
	}
	req.Header.Set("Authorization", "Bearer "+c.token)
	req.Header.Set("Content-Type", "application/octet-stream")
	resp, err := (&http.Client{Timeout: 2 * time.Minute}).Do(req)
	if err != nil {
		return "", err
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusCreated {
		return "", fmt.Errorf("upload status %d", resp.StatusCode)
	}
	var f struct {
		ID string `json:"id"`
	}
	if err := json.NewDecoder(io.LimitReader(resp.Body, 4096)).Decode(&f); err != nil || f.ID == "" {
		return "", errors.New("upload: no id")
	}
	return f.ID, nil
}

func Execute(job control.Job) control.JobResult {
	switch job.Action {
	case "ping":
		return control.JobResult{Success: true, Output: "pong from " + job.DeviceID}
	case "diagnostics":
		hostname, err := os.Hostname()
		if err != nil {
			return control.JobResult{Success: false, Error: "unable to read hostname"}
		}
		return control.JobResult{Success: true, Output: fmt.Sprintf("host=%s os=%s arch=%s", hostname, runtime.GOOS, runtime.GOARCH)}
	default:
		return control.JobResult{Success: false, Error: "unsupported action"}
	}
}

func (c *Client) runningIDs() []string {
	c.mu.Lock()
	defer c.mu.Unlock()
	ids := make([]string, 0, len(c.running))
	for id := range c.running {
		ids = append(ids, id)
	}
	return ids
}

func (c *Client) freeSlots() control.ClaimRequest {
	c.mu.Lock()
	defer c.mu.Unlock()
	agents := len(c.running) - len(c.codex)
	if c.mcpJob != "" {
		agents--
	}
	main, nested := 0, 0
	for _, n := range c.codex {
		if n {
			nested++
		} else {
			main++
		}
	}
	codex := c.Agents.MaxAgents > 0 && installed(c.Agents.CodexBin)
	// The main model's lane also runs the Antigravity pool (D61), whose
	// accounts answer side by side: more turns at once on a PC with agy.
	mainSlots, nestedSlots := codexMainSlots, codexNestedSlots
	if c.Agents.MaxAgents > 0 && runtime.GOOS == "windows" && installed(c.Agents.AgyBin) {
		codex = true
		mainSlots, nestedSlots = agyMainSlots, agyNestedSlots
	}
	return control.ClaimRequest{Agents: agents < c.Agents.MaxAgents, MCP: runtime.GOOS == "linux" && c.mcpJob == "",
		CodexMain: codex && main < mainSlots, CodexNested: codex && nested < nestedSlots}
}

func (c *Client) stop(ids []string) {
	c.mu.Lock()
	defer c.mu.Unlock()
	for _, id := range ids {
		if cancel, ok := c.running[id]; ok {
			cancel()
		}
	}
}

// startAgent runs an agent job in the background; heartbeats keep its lease
// alive meanwhile, and a stop from the server cancels it.
func (c *Client) startAgent(ctx context.Context, job control.Job) {
	jobCtx, cancel := context.WithCancel(ctx)
	c.mu.Lock()
	c.running[job.ID] = cancel
	if job.Action == store.ActionMCPRun {
		c.mcpJob = job.ID
	}
	if job.Action == store.ActionCodexMain {
		if c.codex == nil {
			c.codex = map[string]bool{}
		}
		c.codex[job.ID] = job.Agent != nil && (job.Agent.Mode == store.CodexConsultMode || job.Agent.Mode == store.CodexModelsMode)
	}
	c.mu.Unlock()
	c.wg.Add(1)
	go func() {
		defer c.wg.Done()
		defer func() {
			c.mu.Lock()
			delete(c.running, job.ID)
			if c.mcpJob == job.ID {
				c.mcpJob = ""
			}
			delete(c.codex, job.ID)
			c.mu.Unlock()
			cancel()
		}()
		var result control.JobResult
		if job.Agent == nil {
			result = control.JobResult{Error: "agent job without a spec"}
		} else if job.Action == store.ActionAgentLogin {
			result = RunLogin(jobCtx, c.Agents, *job.Agent, func(step control.AgentStep) {
				stepCtx, done := context.WithTimeout(jobCtx, 5*time.Second)
				_ = c.Step(stepCtx, job.ID, step)
				done()
			}, func(ctx context.Context) string {
				inCtx, done := context.WithTimeout(ctx, 5*time.Second)
				defer done()
				return c.Input(inCtx, job.ID)
			})
		} else if job.Action == store.ActionMCPRun {
			result = RunMCP(jobCtx, c.Agents, *job.Agent)
		} else if job.Action == store.ActionCodexMain && job.Agent.Engine == "antigravity" {
			cfg := c.Agents
			cfg.OnQuota = c.quota.observe
			result = RunAgyMain(jobCtx, cfg, *job.Agent, func(step control.AgentStep) {
				stepCtx, done := context.WithTimeout(jobCtx, 5*time.Second)
				_ = c.Step(stepCtx, job.ID, step)
				done()
			})
			if job.Agent.Mode != store.CodexModelsMode {
				c.quota.markStale(quotaKey("antigravity", job.Agent.Account))
			}
		} else if job.Action == store.ActionCodexMain {
			cfg := c.Agents
			cfg.OnQuota = c.quota.observe
			result = RunCodexMain(jobCtx, cfg, *job.Agent, func(step control.AgentStep) {
				stepCtx, done := context.WithTimeout(jobCtx, 5*time.Second)
				_ = c.Step(stepCtx, job.ID, step)
				done()
			}, func(name string, data []byte) (string, error) {
				return c.uploadJobFile(jobCtx, job.ID, name, data)
			})
			if job.Agent.Mode != store.CodexModelsMode {
				c.quota.markStale(quotaKey("codex", job.Agent.Account))
			}
		} else {
			cfg := c.Agents
			cfg.OnQuota = c.quota.observe
			runCtx := WithRunMeta(jobCtx, RunMeta{
				JobID:      job.ID,
				AgentID:    job.Agent.Owner,
				AgentName:  job.Agent.AgentName,
				AgentEmail: job.Agent.AgentEmail,
			})
			result = RunAgent(runCtx, cfg, *job.Agent, func(step control.AgentStep) {
				stepCtx, done := context.WithTimeout(jobCtx, 5*time.Second)
				_ = c.Step(stepCtx, job.ID, step)
				done()
			})
			if job.Agent.Engine == "codex" || job.Agent.Engine == "antigravity" {
				c.quota.markStale(quotaKey(job.Agent.Engine, job.Agent.Account)) // their output carries no limits
			}
		}
		// Report even when the runtime is shutting down.
		for attempt := 0; attempt < 3; attempt++ {
			reportCtx, done := context.WithTimeout(context.WithoutCancel(ctx), 15*time.Second)
			err := c.Complete(reportCtx, job.ID, result)
			done()
			if err == nil || errors.Is(err, ErrUnauthorized) || strings.Contains(err.Error(), "status 409") {
				return
			}
			time.Sleep(2 * time.Second)
		}
	}()
}

func (c *Client) Tick(ctx context.Context) error {
	stop, err := c.Heartbeat(ctx, c.runningIDs())
	if err != nil {
		return err
	}
	c.stop(stop)
	job, err := c.Claim(ctx, c.freeSlots())
	if err != nil {
		return err
	}
	if job == nil {
		return nil
	}
	if job.Action == store.ActionAgentRun || job.Action == store.ActionAgentLogin || job.Action == store.ActionMCPRun ||
		job.Action == store.ActionCodexMain {
		c.startAgent(ctx, *job)
		return nil
	}
	return c.Complete(ctx, job.ID, Execute(*job))
}

// Run ticks every 2 seconds until ctx ends. Transient errors are reported and
// retried; a rejected token stops the loop and is returned.
func (c *Client) Run(ctx context.Context, report func(error)) error {
	ticker := time.NewTicker(2 * time.Second)
	defer ticker.Stop()
	defer c.wg.Wait()
	c.wg.Add(1)
	go func() {
		defer c.wg.Done()
		c.llmLoop(ctx)
	}()
	if c.Browser != nil {
		c.wg.Add(1)
		go func() {
			defer c.wg.Done()
			c.browserLoop(ctx, c.Browser)
		}()
	}
	if c.Agents.MaxAgents > 0 {
		c.wg.Add(1)
		go func() {
			defer c.wg.Done()
			c.quota.loop(ctx, c.Agents)
		}()
	}
	for {
		err := c.Tick(ctx)
		if errors.Is(err, ErrUnauthorized) {
			return err
		}
		if err != nil && ctx.Err() == nil && report != nil {
			report(err)
		}
		select {
		case <-ctx.Done():
			return nil
		case <-ticker.C:
		}
	}
}
