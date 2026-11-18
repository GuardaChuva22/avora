package main

import (
	"context"
	"errors"
	"log"
	"net"
	"net/http"
	"os"
	"os/signal"
	"path/filepath"
	"runtime"
	"strconv"
	"strings"
	"syscall"
	"time"

	"github.com/GuardaChuva22/avora/internal/connect"
	"github.com/GuardaChuva22/avora/internal/control"
	"github.com/GuardaChuva22/avora/internal/llm"
	"github.com/GuardaChuva22/avora/internal/store"
	"github.com/GuardaChuva22/avora/internal/vault"
	"github.com/GuardaChuva22/avora/internal/web"
	"github.com/GuardaChuva22/avora/internal/webui"
	"github.com/GuardaChuva22/avora/internal/whatsapp"
)

const sweepInterval = 15 * time.Second

func main() {
	address := os.Getenv("AVORA_LISTEN_ADDR")
	if address == "" {
		address = "127.0.0.1:8080"
	}
	host, _, err := net.SplitHostPort(address)
	if err != nil {
		log.Fatal("invalid AVORA_LISTEN_ADDR: ", err)
	}
	ip := net.ParseIP(host)
	if host != "localhost" && (ip == nil || !ip.IsLoopback()) {
		// Loopback behind a TLS proxy (tailscale serve, Caddy, nginx) is the
		// safe default. Binding elsewhere is an explicit choice.
		if os.Getenv("AVORA_ALLOW_REMOTE") != "true" {
			log.Fatal("AVORA_LISTEN_ADDR is not loopback: put Avora behind a TLS proxy (tailscale serve, Caddy, nginx) or set AVORA_ALLOW_REMOTE=true")
		}
		log.Printf("listening on %s, outside loopback: use TLS in front of it, the admin token travels in every request", address)
	}
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()

	st := openStore(ctx)
	defer st.Close()

	service, err := control.NewServer(os.Getenv("AVORA_ADMIN_TOKEN"), st)
	if err != nil {
		log.Fatal(err)
	}
	zone := envOr("AVORA_TIMEZONE", systemTimeZone())
	if err := service.SetTimeZone(zone); err != nil {
		log.Fatal("invalid AVORA_TIMEZONE: ", err)
	}
	log.Print("time zone: ", zone)
	if err := service.EnsureTeam(ctx); err != nil {
		log.Fatal("creating the starting team: ", err)
	}
	if err := service.RecoverTurns(ctx); err != nil {
		log.Fatal("ending interrupted answers: ", err)
	}
	// WhatsApp needs Postgres (the device keys and the messages live there).
	if pg, ok := st.(*store.Postgres); ok {
		if wa, err := whatsapp.New(ctx, pg.SQLDB()); err != nil {
			log.Printf("whatsapp unavailable: %v", err)
		} else {
			service.SetWhatsApp(wa)
		}
	}
	// How long a locked action waits for the owner's answer.
	if v := os.Getenv("AVORA_APPROVAL_MINUTES"); v != "" {
		n, err := strconv.Atoi(v)
		if err == nil {
			err = service.SetApprovalMinutes(n)
		}
		if err != nil {
			log.Fatal("invalid AVORA_APPROVAL_MINUTES: ", err)
		}
	}
	// The model Claude Code and Codex get when an agent delegates ("" = the
	// CLI's own default).
	if err := service.SetDelegateModels(os.Getenv("AVORA_CLAUDE_MODEL"), os.Getenv("AVORA_CODEX_MODEL")); err != nil {
		log.Fatal(err)
	}
	// The Codex model of the pesquisar tool ("" = Codex's own default).
	if m := os.Getenv("AVORA_RESEARCH_MODEL"); m != "" {
		if err := service.SetResearchModel(m); err != nil {
			log.Fatal(err)
		}
	}
	// The agents' folders: the same tree the VPS runtime works in.
	if err := service.SetWorkspace(os.Getenv("AVORA_WORKSPACE")); err != nil {
		log.Fatal("invalid AVORA_WORKSPACE: ", err)
	}
	if key := os.Getenv("AVORA_BRAVE_API_KEY"); key != "" {
		service.SetWeb(web.New(web.Options{BraveKey: key}))
		log.Print("web search: Brave Search API")
	}
	service.SetConnectors(connectors(), openVault())
	if err := service.RestoreConnectors(ctx); err != nil {
		log.Fatal("loading the Plugins: ", err)
	}
	if provider, model := nativeProvider(); provider != nil {
		service.SetBuiltinModel(provider, model)
		log.Printf("server model: %s via %s", model, provider.Name)
	}
	// The main model picked in Settings wins over the server's.
	if err := service.RestoreModels(ctx); err != nil {
		log.Fatal("loading the main model: ", err)
	}
	if eng := service.ChatEngineInfo(); eng != "" {
		log.Print("chat engine: ", eng)
	} else {
		log.Print("chat engine disabled: connect an AI in Settings → Contas de IA, or set AVORA_NATIVE_BASE_URL, AVORA_NATIVE_MODEL and AVORA_NATIVE_API_KEY")
	}
	service.SetUI(webui.Handler())
	if !webui.Built() {
		log.Print("web app not built into this binary; / answers 503")
	}
	go sweep(ctx, service)
	go service.RunRoutines(ctx)
	go service.RunGoals(ctx)
	go service.RunHeartbeat(ctx)
	go service.RunNight(ctx)
	go service.RunTriggers(ctx)

	server := &http.Server{
		Addr: address, Handler: service.Handler(),
		ReadHeaderTimeout: 5 * time.Second, ReadTimeout: 10 * time.Second,
		WriteTimeout: 10 * time.Second, IdleTimeout: 60 * time.Second,
	}
	go func() {
		<-ctx.Done()
		shutdown, cancel := context.WithTimeout(context.Background(), 10*time.Second)
		defer cancel()
		_ = server.Shutdown(shutdown)
	}()
	log.Printf("Avora control plane on %s (durable=%v): allowlisted ping/diagnostics only", address, st.Durable())
	if err := server.ListenAndServe(); err != nil && !errors.Is(err, http.ErrServerClosed) {
		log.Fatal(err)
	}
}

// openStore uses Postgres when AVORA_DATABASE_URL is set; otherwise state
// lives in memory and is lost on restart.
func openStore(ctx context.Context) store.Store {
	url := os.Getenv("AVORA_DATABASE_URL")
	if url == "" {
		log.Print("AVORA_DATABASE_URL not set: using in-memory state, lost on restart")
		return store.NewMemory()
	}
	connect, cancel := context.WithTimeout(ctx, 15*time.Second)
	defer cancel()
	st, err := store.OpenPostgres(connect, url)
	if err != nil {
		log.Fatal(err)
	}
	return st
}

// nativeProvider is the server's own model provider from the environment:
// any OpenAI-compatible API (AVORA_NATIVE_BASE_URL, AVORA_NATIVE_MODEL,
// AVORA_NATIVE_API_KEY). It is optional: providers added in Settings →
// Modelos do the same and win over it. A key with no address is an install
// from before these variables (NVIDIA build) and keeps working.
func nativeProvider() (*llm.Provider, string) {
	key := os.Getenv("AVORA_NATIVE_API_KEY")
	if key == "" {
		key = os.Getenv("NVIDIA_API_KEY")
	}
	baseURL := os.Getenv("AVORA_NATIVE_BASE_URL")
	model := os.Getenv("AVORA_NATIVE_MODEL")
	providerName := envOr("AVORA_NATIVE_PROVIDER", "servidor")
	rpmDefault := "0"
	if baseURL == "" && key != "" {
		log.Print("AVORA_NATIVE_BASE_URL not set: the server's model is NVIDIA build (older setup); set AVORA_NATIVE_BASE_URL and AVORA_NATIVE_MODEL to pick another")
		baseURL, providerName, rpmDefault = "https://integrate.api.nvidia.com/v1", envOr("AVORA_NATIVE_PROVIDER", "nvidia"), "30"
		if model == "" {
			model = "nvidia/nemotron-3-ultra-550b-a55b"
		}
	}
	if baseURL == "" && model == "" {
		return nil, ""
	}
	if baseURL == "" || model == "" {
		log.Fatal("the server's model needs both AVORA_NATIVE_BASE_URL and AVORA_NATIVE_MODEL")
	}
	rpm, err := strconv.Atoi(envOr("AVORA_NATIVE_RPM", rpmDefault))
	if err != nil || rpm < 0 {
		log.Fatal("invalid AVORA_NATIVE_RPM")
	}
	provider, err := llm.NewProvider(providerName, baseURL, key, rpm)
	if err != nil {
		log.Fatal(err)
	}
	return provider, model
}

// systemTimeZone is the machine's IANA zone (TZ, /etc/timezone or the
// /etc/localtime link), or UTC when it cannot tell.
func systemTimeZone() string {
	if tz := strings.TrimPrefix(os.Getenv("TZ"), ":"); tz != "" {
		if _, err := time.LoadLocation(tz); err == nil {
			return tz
		}
	}
	if b, err := os.ReadFile("/etc/timezone"); err == nil {
		if tz := strings.TrimSpace(string(b)); tz != "" {
			if _, err := time.LoadLocation(tz); err == nil {
				return tz
			}
		}
	}
	if target, err := os.Readlink("/etc/localtime"); err == nil {
		if i := strings.Index(target, "zoneinfo/"); i >= 0 {
			tz := target[i+len("zoneinfo/"):]
			if _, err := time.LoadLocation(tz); err == nil {
				return tz
			}
		}
	}
	return "UTC"
}

func envOr(name, fallback string) string {
	if v := os.Getenv(name); v != "" {
		return v
	}
	return fallback
}

func sweep(ctx context.Context, service *control.Server) {
	ticker := time.NewTicker(sweepInterval)
	defer ticker.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			if err := service.Sweep(ctx); err != nil && ctx.Err() == nil {
				log.Printf("sweep: %v", err)
			}
		}
	}
}

// connectors are the apps the owner can connect on the Plugins page.
func connectors() *connect.Registry {
	google := &connect.Google{ClientID: os.Getenv("AVORA_GOOGLE_CLIENT_ID"), ClientSecret: os.Getenv("AVORA_GOOGLE_CLIENT_SECRET")}
	if !google.Info().Ready {
		log.Print("Google (Gmail, Drive, Agenda) off: set AVORA_GOOGLE_CLIENT_ID and AVORA_GOOGLE_CLIENT_SECRET")
	}
	all := []connect.Connector{google}
	for _, app := range connect.MCPCatalog() {
		all = append(all, app)
	}
	apify := &connect.Apify{}
	all = append(all, &connect.ClickUp{}, &connect.ElevenLabs{}, &connect.Discord{}, &connect.Instagram{}, &connect.Tinder{}, &connect.Pluggy{}, &connect.Pix{}, &connect.Maps{}, apify, &connect.Products{}, &connect.MercadoLivre{}, &connect.StoreApp{Apify: apify, Store: "shopee"}, &connect.StoreApp{Apify: apify, Store: "aliexpress"},
		&connect.Spotify{ClientID: os.Getenv("AVORA_SPOTIFY_CLIENT_ID")},
		&connect.LinkedIn{ClientID: os.Getenv("AVORA_LINKEDIN_CLIENT_ID"), ClientSecret: os.Getenv("AVORA_LINKEDIN_CLIENT_SECRET")},
		&connect.IFood{ClientID: os.Getenv("AVORA_IFOOD_CLIENT_ID"), ClientSecret: os.Getenv("AVORA_IFOOD_CLIENT_SECRET"),
			Production: os.Getenv("AVORA_IFOOD_ENV") == "producao"},
		&connect.NFSe{Production: os.Getenv("AVORA_NFSE_ENV") == "producao"})
	return connect.NewRegistry(all...)
}

// openVault loads the key that seals the Plugins' credentials and the
// providers' API keys (AVORA_VAULT_KEY_FILE, default /etc/avora/vault.key on
// Linux and avora/vault.key in the user's config folder elsewhere). A
// missing key is created, so connections survive restarts on any machine.
func openVault() *vault.Vault {
	path := os.Getenv("AVORA_VAULT_KEY_FILE")
	if path == "" {
		path = defaultVaultPath()
	}
	v, err := vault.Open(path)
	if err == nil {
		return v
	}
	if !errors.Is(err, os.ErrNotExist) {
		log.Fatal("vault key: ", err)
	}
	if err := vault.Create(path); err == nil {
		if v, err := vault.Open(path); err == nil {
			log.Printf("vault key created at %s", path)
			return v
		}
	}
	log.Printf("vault key %s missing and could not be created: using a temporary key; connected apps are lost on restart", path)
	return vault.Random()
}

func defaultVaultPath() string {
	if runtime.GOOS == "linux" {
		return "/etc/avora/vault.key"
	}
	dir, err := os.UserConfigDir()
	if err != nil {
		return "vault.key"
	}
	return filepath.Join(dir, "avora", "vault.key")
}
