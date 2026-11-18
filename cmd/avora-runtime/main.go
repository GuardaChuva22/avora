package main

import (
	"context"
	"flag"
	"log"
	"os"
	"os/signal"
	"path/filepath"
	"syscall"

	"github.com/GuardaChuva22/avora/internal/runner"
)

func main() {
	logPath := flag.String("log", "", "append logs to this file instead of stderr (for runs without a console)")
	flag.Parse()
	if *logPath != "" {
		if err := os.MkdirAll(filepath.Dir(*logPath), 0o700); err != nil {
			log.Fatal(err)
		}
		f, err := os.OpenFile(*logPath, os.O_CREATE|os.O_APPEND|os.O_WRONLY, 0o600)
		if err != nil {
			log.Fatal(err)
		}
		defer f.Close()
		log.SetOutput(f)
	}

	serverURL := os.Getenv("AVORA_SERVER_URL")
	statePath := os.Getenv("AVORA_STATE_FILE")
	if statePath == "" {
		var err error
		if statePath, err = runner.DefaultStatePath(); err != nil {
			log.Fatal(err)
		}
	}

	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()

	state, err := runner.LoadOrPair(ctx, statePath, serverURL, os.Getenv("AVORA_PAIRING_CODE"))
	if err != nil {
		log.Fatal(err)
	}
	if serverURL == "" {
		serverURL = state.Server
	}
	client, err := runner.NewClient(serverURL, state.Token)
	if err != nil {
		log.Fatal(err)
	}
	client.Agents = runner.DefaultAgentConfig()
	runner.RestoreAgyPrincipal(client.Agents)
	client.Agents.ServerURL = serverURL
	client.Browser = runner.NewBrowser()
	if client.Browser != nil {
		log.Printf("agents' browser: %s (profile %s)", client.Browser.Exe, client.Browser.Profile)
	}
	log.Printf("Avora runtime %s (%s, %s) connected to %s; up to %d agent runs (claude %s, codex %s, workspace %s)",
		state.NodeID, state.Name, state.Kind, serverURL, client.Agents.MaxAgents,
		client.Agents.ClaudeBin, client.Agents.CodexBin, client.Agents.Workspace)

	// Log an error once, not every 5 seconds while it lasts (e.g. Tailscale down).
	last := ""
	report := func(err error) {
		if msg := err.Error(); msg != last {
			log.Printf("runtime error: %s", msg)
			last = msg
		}
	}
	if err := client.Run(ctx, report); err != nil {
		log.Fatal(err)
	}
}
