package control

import (
	"context"
	"encoding/json"
	"errors"
	"log"
	"net/http"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/GuardaChuva22/avora/internal/engine"
	"github.com/GuardaChuva22/avora/internal/llm"
	"github.com/GuardaChuva22/avora/internal/store"
)

// Settings → Modelos: the providers the owner configures and the main model
// every agent thinks with. A provider is any API in the OpenAI format: the
// presets below (OpenRouter, Anthropic, Gemini...) only fill the address, and
// "openai" takes any other. Local servers (Ollama, LM Studio, llama.cpp) run
// on the owner's own machines.
// Claude Code and Codex subscriptions are listed here too: the agents
// delegate to them, and a Codex account can also be the main model
// (codex_main.go).

const (
	mainModelKey = "main_model"
	// builtinProviderID is the provider the server's environment configures
	// (AVORA_NATIVE_*). Settings shows it but cannot change or remove it.
	builtinProviderID   = "servidor"
	builtinProviderName = "Modelo do servidor"
	probeTimeout        = 20 * time.Second
	maxProviders        = 20
)

// ProviderKind is one kind of provider Settings can add.
type ProviderKind struct {
	Kind        string `json:"kind"`
	Name        string `json:"name"`
	Description string `json:"description"`
	BaseURL     string `json:"base_url,omitempty"`
	// Placeholder hints the address when the owner must type one.
	Placeholder string `json:"placeholder,omitempty"`
	NeedsKey    bool   `json:"needs_key"`
	NeedsURL    bool   `json:"needs_url"`
	// Main says the kind can be the main model.
	Main bool `json:"main"`
	// Login says the kind is a subscription CLI logged in on a node.
	Login bool   `json:"login,omitempty"`
	Soon  bool   `json:"soon,omitempty"`
	Help  string `json:"help,omitempty"`
}

var ProviderKinds = []ProviderKind{
	// APIs with a fixed address: only the key is asked.
	{Kind: "openrouter", Name: "OpenRouter", Main: true, NeedsKey: true,
		Description: "Centenas de modelos (Claude, GPT, Gemini, Llama, DeepSeek...) com uma chave só.",
		BaseURL:     "https://openrouter.ai/api/v1",
		Help:        "Gere a chave em openrouter.ai/keys."},
	{Kind: "anthropic", Name: "Anthropic", Main: true, NeedsKey: true,
		Description: "Claude pela API da Anthropic, pago por uso.",
		BaseURL:     "https://api.anthropic.com/v1",
		Help:        "Gere a chave em console.anthropic.com, em API Keys."},
	{Kind: "openai_api", Name: "OpenAI", Main: true, NeedsKey: true,
		Description: "GPT pela API da OpenAI, pago por uso.",
		BaseURL:     "https://api.openai.com/v1",
		Help:        "Gere a chave em platform.openai.com/api-keys."},
	{Kind: "gemini", Name: "Google Gemini", Main: true, NeedsKey: true,
		Description: "Gemini pela API do Google AI Studio.",
		BaseURL:     "https://generativelanguage.googleapis.com/v1beta/openai",
		Help:        "Gere a chave em aistudio.google.com/apikey. O plano grátis pode usar as conversas para treino."},
	{Kind: "xai", Name: "xAI", Main: true, NeedsKey: true,
		Description: "Grok pela API da xAI.",
		BaseURL:     "https://api.x.ai/v1",
		Help:        "Gere a chave em console.x.ai."},
	{Kind: "deepseek", Name: "DeepSeek", Main: true, NeedsKey: true,
		Description: "DeepSeek V e R pela API oficial, barato.",
		BaseURL:     "https://api.deepseek.com/v1",
		Help:        "Gere a chave em platform.deepseek.com/api_keys."},
	{Kind: "mistral", Name: "Mistral", Main: true, NeedsKey: true,
		Description: "Mistral Large, Medium e Codestral.",
		BaseURL:     "https://api.mistral.ai/v1",
		Help:        "Gere a chave em console.mistral.ai/api-keys."},
	{Kind: "groq", Name: "Groq", Main: true, NeedsKey: true,
		Description: "Modelos abertos com resposta muito rápida.",
		BaseURL:     "https://api.groq.com/openai/v1",
		Help:        "Gere a chave em console.groq.com/keys."},
	{Kind: "cerebras", Name: "Cerebras", Main: true, NeedsKey: true,
		Description: "Modelos abertos com resposta muito rápida.",
		BaseURL:     "https://api.cerebras.ai/v1",
		Help:        "Gere a chave em cloud.cerebras.ai, em API Keys."},
	{Kind: "together", Name: "Together AI", Main: true, NeedsKey: true,
		Description: "Llama, Qwen, DeepSeek e outros modelos abertos.",
		BaseURL:     "https://api.together.xyz/v1",
		Help:        "Gere a chave em api.together.ai/settings/api-keys."},
	{Kind: "fireworks", Name: "Fireworks", Main: true, NeedsKey: true,
		Description: "Modelos abertos rápidos, pago por uso.",
		BaseURL:     "https://api.fireworks.ai/inference/v1",
		Help:        "Gere a chave em fireworks.ai/account/api-keys."},
	{Kind: "nvidia", Name: "NVIDIA", Main: true, NeedsKey: true,
		Description: "Nemotron, DeepSeek, Qwen, Llama e outros modelos abertos na NVIDIA build.",
		BaseURL:     "https://integrate.api.nvidia.com/v1",
		Help:        "Gere a chave em build.nvidia.com, em API Keys. O plano grátis pode usar as conversas para treino: não use com dados privados."},
	// Servers on the owner's own machines: the address is asked, no key.
	{Kind: "ollama", Name: "Ollama", Main: true, NeedsURL: true,
		Description: "Modelos rodando no seu computador pelo Ollama. Nada sai das suas máquinas.",
		BaseURL:     "http://localhost:11434/v1",
		Placeholder: "http://192.168.0.10:11434/v1",
		Help:        "Se o Ollama roda em outro computador, abra-o com OLLAMA_HOST=0.0.0.0 e use o endereço dele na rede local ou na Tailscale."},
	{Kind: "lmstudio", Name: "LM Studio", Main: true, NeedsURL: true,
		Description: "Modelos rodando no seu computador pelo LM Studio.",
		BaseURL:     "http://localhost:1234/v1",
		Placeholder: "http://192.168.0.10:1234/v1",
		Help:        "No LM Studio, ligue o servidor em Developer → Start Server (com Serve on Local Network se ele roda em outro computador)."},
	{Kind: "local", Name: "llama.cpp", Main: true, NeedsURL: true,
		Description: "Um llama-server num dos seus computadores. Nada sai das suas máquinas.",
		Placeholder: "http://192.168.0.10:8080/v1",
		Help:        "No computador: llama-server -m modelo.gguf --host 0.0.0.0 --port 8080 --jinja. O --jinja liga as ferramentas."},
	// Anything else that speaks the OpenAI or Anthropic format (vLLM, LiteLLM, Azure...).
	{Kind: "openai", Name: "Outro provedor", Main: true, NeedsURL: true,
		Description: "Qualquer API no formato OpenAI ou Anthropic: vLLM, LiteLLM, Azure, um proxy seu...",
		Placeholder: "https://seu-servidor.com/v1",
		Help:        "Escolha o formato OpenAI ou Anthropic. A chave é opcional para endereços da sua rede."},
	{Kind: "claude", Name: "Claude", Login: true,
		Description: "Claude Code com a sua assinatura Pro ou Max. Os agentes delegam tarefas a ele."},
	{Kind: "codex", Name: "Codex", Login: true,
		Description: "Codex com a sua conta ChatGPT. Responde pelos agentes e recebe as tarefas que eles delegam."},
	{Kind: "antigravity", Name: "Antigravity", Login: true,
		Description: "O agente do Google (Gemini) no seu PC. Junte quantas contas Google AI quiser: o Avora divide o trabalho entre elas.",
		Help:        "Instale o Antigravity CLI (agy) pelo antigravity.google, na aba CLI. Cada conta entra por um link do Google: abra no navegador do PC e escolha a conta (de preferência, deixe as contas logadas nesse navegador)."},
}

func providerKind(kind string) (ProviderKind, bool) {
	for _, k := range ProviderKinds {
		if k.Kind == kind {
			return k, true
		}
	}
	return ProviderKind{}, false
}

type agyCachedModelSet struct {
	models []codexModel
	at     time.Time
}

type modelState struct {
	mu            sync.Mutex
	builtin       *llm.Provider
	builtinModel  string
	builtinModels []string
	// active is the provider id the main model runs on ("" = none).
	active string
	// codex caches the models each Codex provider offers.
	codex map[string][]codexModel
	// agy caches the models "agy models" returned per account/pool for 1 hour.
	agy map[string]agyCachedModelSet
	// pinned says PUT /v1/models/main chose the model; otherwise the
	// server picks it (auto_main.go).
	pinned  bool
	picking bool
	looked  time.Time
}

type MainModel struct {
	Provider string `json:"provider"`
	Model    string `json:"model"`
}

type ProviderView struct {
	store.Provider
	Builtin bool `json:"builtin,omitempty"`
	HasKey  bool `json:"has_key"`
	// CodexMain says a Codex account can answer as the main model now.
	CodexMain bool `json:"codex_main,omitempty"`
	// ModelNames are the models' display names, when they differ from ids.
	ModelNames map[string]string `json:"model_names,omitempty"`
}

type ProviderRequest struct {
	Kind      string `json:"kind"`
	Name      string `json:"name"`
	BaseURL   string `json:"base_url"`
	APIFormat string `json:"api_format"`
	// APIKey is sealed at once and never returned; empty on an edit keeps
	// the saved one.
	APIKey string `json:"api_key"`
	// NodeID is where a new Claude or Codex account logs in.
	NodeID string `json:"node_id"`
	// ModelMeta sets optional per-model prices (USD per 1M tokens) or limits.
	ModelMeta map[string]store.ModelMeta `json:"model_meta"`
}

// ValidateAPIKeyPrefix checks known provider key prefixes and returns a plain
// Portuguese sentence when a pasted key does not match.
func ValidateAPIKeyPrefix(kind, key string) string {
	key = strings.TrimSpace(key)
	if key == "" {
		return ""
	}
	switch kind {
	case "openrouter":
		if !strings.HasPrefix(key, "sk-or-v1-") {
			return "As chaves da OpenRouter começam com sk-or-v1-"
		}
	case "anthropic":
		if !strings.HasPrefix(key, "sk-ant-") {
			return "As chaves da Anthropic começam com sk-ant-"
		}
	case "openai_api":
		if !strings.HasPrefix(key, "sk-") || strings.HasPrefix(key, "sk-or-v1-") || strings.HasPrefix(key, "sk-ant-") {
			return "As chaves da OpenAI começam com sk-"
		}
	case "groq":
		if !strings.HasPrefix(key, "gsk_") {
			return "As chaves da Groq começam com gsk_"
		}
	case "xai":
		if !strings.HasPrefix(key, "xai-") {
			return "As chaves da xAI começam com xai-"
		}
	case "gemini":
		if !strings.HasPrefix(key, "AIza") {
			return "As chaves do Gemini começam com AIza"
		}
	}
	return ""
}

func providerPurpose(id string) string { return "provider:" + id }

// SetBuiltinModel registers the provider the server's environment
// configures and makes it the main model until Settings picks another.
func (s *Server) SetBuiltinModel(p *llm.Provider, model string) {
	s.models.mu.Lock()
	s.models.builtin, s.models.builtinModel, s.models.builtinModels = p, model, []string{model}
	s.models.active = builtinProviderID
	s.models.mu.Unlock()
	s.SetChatEngine(engine.NewNative(p, model))
}

// RestoreModels applies a pinned model, or picks the best AI connected.
func (s *Server) RestoreModels(ctx context.Context) error {
	raw, err := s.store.GetSetting(ctx, mainModelKey)
	if err != nil && !errors.Is(err, store.ErrNotFound) {
		return err
	}
	var m MainModel
	if err == nil && json.Unmarshal(raw, &m) == nil && m.Provider != "" && m.Model != "" {
		if err := s.useModel(ctx, m); err != nil {
			log.Printf("pinned model %s on %s: %v; picking one", m.Model, m.Provider, err)
		} else {
			s.models.mu.Lock()
			s.models.pinned = true
			s.models.mu.Unlock()
			return nil
		}
	}
	s.repick(ctx, true)
	return nil
}

// providerClient builds the client of a provider; the key is opened only
// here, never returned.
func (s *Server) providerClient(ctx context.Context, id string) (*llm.Provider, error) {
	if id == builtinProviderID {
		s.models.mu.Lock()
		defer s.models.mu.Unlock()
		if s.models.builtin == nil {
			return nil, store.ErrNotFound
		}
		return s.models.builtin, nil
	}
	p, err := s.store.GetProvider(ctx, id)
	if err != nil {
		return nil, err
	}
	return s.clientFor(p)
}

func (s *Server) clientFor(p store.Provider) (*llm.Provider, error) {
	if k, ok := providerKind(p.Kind); !ok || !k.Main {
		return nil, errors.New("este provedor não responde pelo chat")
	}
	key := ""
	if len(p.Secret) > 0 {
		if s.vault == nil {
			return nil, errors.New("o cofre de credenciais não está configurado")
		}
		b, err := s.vault.Open(p.Secret, providerPurpose(p.ID))
		if err != nil {
			return nil, errors.New("não consegui abrir a chave salva")
		}
		key = string(b)
	}
	rpm := 0
	if p.Kind == "nvidia" {
		rpm = 30 // the free tier's limit
	}
	c, err := llm.NewProvider(p.Kind, p.BaseURL, key, rpm)
	if err != nil {
		return nil, err
	}
	if strings.EqualFold(p.APIFormat, "anthropic") {
		c.Format = "anthropic"
	}
	return c, nil
}

func (s *Server) useModel(ctx context.Context, m MainModel) error {
	if isAgyProvider(m.Provider) {
		if m.Provider != agyBrain {
			if _, err := s.store.GetProvider(ctx, m.Provider); err != nil {
				return err
			}
		}
		s.SetChatEngine(&agyEngine{provider: m.Provider, model: m.Model, rotate: true})
		s.models.mu.Lock()
		s.models.active = m.Provider
		s.models.mu.Unlock()
		return nil
	}
	if isCodexProvider(m.Provider) {
		if _, _, err := s.codexTarget(ctx, m.Provider, false); err != nil {
			return err
		}
		s.SetChatEngine(&codexEngine{provider: m.Provider, model: m.Model, rotate: true})
		s.models.mu.Lock()
		s.models.active = m.Provider
		s.models.mu.Unlock()
		return nil
	}
	c, err := s.providerClient(ctx, m.Provider)
	if err != nil {
		return err
	}
	s.SetChatEngine(engine.NewNative(c, m.Model))
	s.models.mu.Lock()
	s.models.active = m.Provider
	s.models.mu.Unlock()
	return nil
}

// providerError is what Settings shows when a provider does not answer.
func providerError(err error) string {
	var apiErr *llm.APIError
	switch {
	case errors.As(err, &apiErr) && (apiErr.Status == http.StatusUnauthorized || apiErr.Status == http.StatusForbidden):
		return "a chave foi recusada"
	case errors.As(err, &apiErr) && apiErr.Status == http.StatusNotFound:
		return "o endereço não tem a lista de modelos (/models): confira se termina em /v1"
	case errors.As(err, &apiErr):
		return "o provedor respondeu com erro " + strconv.Itoa(apiErr.Status)
	case errors.Is(err, context.DeadlineExceeded):
		return "o provedor não respondeu a tempo"
	case strings.Contains(err.Error(), "connection refused"):
		return "nada escutando nesse endereço: o servidor está ligado?"
	case strings.Contains(err.Error(), "no such host"):
		return "endereço não encontrado"
	default:
		return "não consegui falar com o provedor"
	}
}

func isNonChatModel(m string) bool {
	low := strings.ToLower(m)
	for _, w := range skipModel {
		if strings.Contains(low, w) {
			return true
		}
	}
	return false
}

// probe lists a provider's models, tests tool support on chat models, and records how it went.
func (s *Server) probe(ctx context.Context, p store.Provider) store.Provider {
	c, err := s.clientFor(p)
	if err == nil {
		pctx, cancel := context.WithTimeout(ctx, probeTimeout)
		var models []string
		models, err = c.Models(pctx)
		cancel()
		if err == nil {
			if len(models) == 0 && len(p.Models) > 0 && strings.EqualFold(p.APIFormat, "anthropic") {
				models = p.Models
			}
			meta := map[string]store.ModelMeta{}
			for k, v := range p.ModelMeta {
				meta[k] = v
			}
			var toProbe []string
			for _, m := range models {
				if !isNonChatModel(m) && len(toProbe) < 12 {
					toProbe = append(toProbe, m)
				}
			}
			if len(toProbe) > 0 {
				var wg sync.WaitGroup
				var mu sync.Mutex
				sem := make(chan struct{}, 4)
				for _, m := range toProbe {
					wg.Add(1)
					go func(model string) {
						defer wg.Done()
						sem <- struct{}{}
						defer func() { <-sem }()
						tctx, tcancel := context.WithTimeout(ctx, 6*time.Second)
						ok, perr := c.ProbeTools(tctx, model)
						tcancel()
						if perr != nil {
							return
						}
						mu.Lock()
						mm := meta[model]
						val := ok
						mm.Tools = &val
						meta[model] = mm
						mu.Unlock()
					}(m)
				}
				wg.Wait()
			}
			if len(meta) > 0 {
				p.ModelMeta = meta
			}
			p.Models, p.Status, p.LastError = models, "ok", ""
			return p
		}
		p.LastError = providerError(err)
	} else {
		p.LastError = err.Error()
	}
	p.Status = "error"
	return p
}

func (s *Server) registerModels(mux *http.ServeMux) {
	mux.HandleFunc("GET /v1/models", s.admin(s.listModels))
	mux.HandleFunc("POST /v1/models/providers", s.admin(s.saveProvider))
	mux.HandleFunc("PUT /v1/models/providers/{id}", s.admin(s.saveProvider))
	mux.HandleFunc("POST /v1/providers", s.admin(s.saveProvider))
	mux.HandleFunc("PUT /v1/providers/{id}", s.admin(s.saveProvider))
	mux.HandleFunc("POST /v1/models/providers/{id}/refresh", s.admin(s.refreshProvider))
	mux.HandleFunc("DELETE /v1/models/providers/{id}", s.admin(s.deleteProvider))
	mux.HandleFunc("PUT /v1/models/main", s.admin(s.setMainModel))
	mux.HandleFunc("DELETE /v1/models/main", s.admin(s.unpinMainModel))
}

func (s *Server) listModels(w http.ResponseWriter, r *http.Request) {
	wsID, ok := s.requireWorkspace(w, r)
	if !ok {
		return
	}
	ctx := store.WithWorkspace(r.Context(), wsID)
	list, err := s.store.ListProviders(ctx)
	if err != nil {
		storeError(w, err)
		return
	}
	out := []ProviderView{}
	s.models.mu.Lock()
	if s.models.builtin != nil {
		out = append(out, ProviderView{Builtin: true, HasKey: true, Provider: store.Provider{
			ID: builtinProviderID, Kind: s.models.builtin.Name, Name: builtinProviderName, BaseURL: s.models.builtin.BaseURL,
			Status: "ok", Models: append([]string{}, s.models.builtinModels...)}})
	}
	active, pinned := s.models.active, s.models.pinned
	s.models.mu.Unlock()
	for _, p := range list {
		out = append(out, ProviderView{Provider: p, HasKey: len(p.Secret) > 0})
	}
	out = s.codexViews(ctx, out, active)
	out = s.agyViews(ctx, out)
	main := MainModel{}
	if eng := s.chatEngine(); eng != nil && active != "" {
		main = MainModel{Provider: active, Model: eng.Model()}
	}
	writeJSON(w, http.StatusOK, map[string]any{"main": main, "pinned": pinned, "providers": out, "kinds": ProviderKinds})
}

func (s *Server) saveProvider(w http.ResponseWriter, r *http.Request) {
	wsID, ok := s.requireWorkspace(w, r)
	if !ok {
		return
	}
	ctx := store.WithWorkspace(r.Context(), wsID)
	r = r.WithContext(ctx)
	var req ProviderRequest
	if !readJSON(w, r, &req) {
		return
	}
	id := r.PathValue("id")
	req.Name, req.BaseURL, req.APIKey = strings.TrimSpace(req.Name), strings.TrimRight(strings.TrimSpace(req.BaseURL), "/"), strings.TrimSpace(req.APIKey)
	if k, ok := providerKind(req.Kind); ok && k.Login && id == "" {
		s.addAccount(w, r, k, req)
		return
	}
	var p store.Provider
	if id != "" {
		if id == builtinProviderID {
			http.Error(w, "o provedor do servidor é configurado no próprio servidor", http.StatusBadRequest)
			return
		}
		old, err := s.store.GetProvider(ctx, id)
		if err != nil {
			storeError(w, err)
			return
		}
		if old.WorkspaceID != wsID {
			storeError(w, store.ErrNotFound)
			return
		}
		p = old
	} else {
		list, err := s.store.ListProviders(ctx)
		if err != nil {
			storeError(w, err)
			return
		}
		if len(list) >= maxProviders {
			http.Error(w, "limite de provedores atingido", http.StatusBadRequest)
			return
		}
		p = store.Provider{ID: req.Kind, WorkspaceID: wsID, Kind: req.Kind}
		if _, err := s.store.GetProvider(ctx, p.ID); err == nil {
			p.ID = req.Kind + "." + strings.ToLower(randomString(6))
		}
	}
	kind, ok := providerKind(p.Kind)
	if ok && kind.Login && req.Name != "" {
		// A subscription account only gets renamed here.
		if len([]rune(req.Name)) > 60 {
			http.Error(w, "nome longo demais", http.StatusBadRequest)
			return
		}
		p.Name = req.Name
		saved, err := s.store.SaveProvider(ctx, p, store.ActorAdmin, s.now())
		if err != nil {
			storeError(w, err)
			return
		}
		writeJSON(w, http.StatusOK, ProviderView{Provider: saved})
		return
	}
	if !ok || !kind.Main {
		http.Error(w, "tipo de provedor desconhecido", http.StatusBadRequest)
		return
	}
	if req.Name != "" {
		p.Name = req.Name
	} else if p.Name == "" {
		p.Name = kind.Name
	}
	if len([]rune(p.Name)) > 60 {
		http.Error(w, "nome longo demais", http.StatusBadRequest)
		return
	}
	formatChanged := false
	if req.APIFormat != "" {
		fmtVal := strings.ToLower(strings.TrimSpace(req.APIFormat))
		switch fmtVal {
		case "openai":
			fmtVal = ""
		case "anthropic":
		default:
			http.Error(w, "formato de API inválido (use openai ou anthropic)", http.StatusBadRequest)
			return
		}
		if p.APIFormat != fmtVal {
			p.APIFormat = fmtVal
			formatChanged = true
		}
	}
	urlChanged := false
	if req.BaseURL != "" {
		if p.BaseURL != req.BaseURL {
			urlChanged = true
		}
		p.BaseURL = req.BaseURL
	} else if p.BaseURL == "" {
		p.BaseURL = kind.BaseURL
	}
	switch {
	case p.BaseURL == "":
		http.Error(w, "informe o endereço do provedor", http.StatusBadRequest)
		return
	case !strings.HasPrefix(p.BaseURL, "https://") && !llm.LocalURL(p.BaseURL):
		http.Error(w, "o endereço precisa ser https, ou http para este servidor, a sua rede local ou a sua Tailscale", http.StatusBadRequest)
		return
	case len(p.BaseURL) > 300:
		http.Error(w, "endereço longo demais", http.StatusBadRequest)
		return
	}
	if msg := ValidateAPIKeyPrefix(p.Kind, req.APIKey); msg != "" {
		http.Error(w, msg, http.StatusBadRequest)
		return
	}
	if req.APIKey != "" {
		if s.vault == nil {
			http.Error(w, "o cofre de credenciais não está configurado", http.StatusServiceUnavailable)
			return
		}
		sealed, err := s.vault.Seal([]byte(req.APIKey), providerPurpose(p.ID))
		if err != nil {
			storeError(w, err)
			return
		}
		p.Secret = sealed
	}
	if kind.NeedsKey && len(p.Secret) == 0 {
		http.Error(w, "informe a chave da API", http.StatusBadRequest)
		return
	}
	if req.ModelMeta != nil {
		if p.ModelMeta == nil {
			p.ModelMeta = map[string]store.ModelMeta{}
		}
		for m, incoming := range req.ModelMeta {
			m = strings.TrimSpace(m)
			if m == "" || len(m) > 200 {
				continue
			}
			cur := p.ModelMeta[m]
			if incoming.InputPrice >= 0 {
				cur.InputPrice = incoming.InputPrice
			}
			if incoming.OutputPrice >= 0 {
				cur.OutputPrice = incoming.OutputPrice
			}
			if incoming.Context > 0 {
				cur.Context = incoming.Context
			}
			if incoming.Tools != nil {
				val := *incoming.Tools
				cur.Tools = &val
			}
			p.ModelMeta[m] = cur
		}
	}
	needProbe := id == "" || req.APIKey != "" || urlChanged || formatChanged || p.Status != "ok"
	if needProbe {
		p = s.probe(ctx, p)
	}
	saved, err := s.store.SaveProvider(ctx, p, store.ActorAdmin, s.now())
	if err != nil {
		storeError(w, err)
		return
	}
	s.reloadIfActive(ctx, saved.ID)
	s.repick(ctx, true)
	writeJSON(w, http.StatusOK, ProviderView{Provider: saved, HasKey: len(saved.Secret) > 0})
}

// reloadIfActive rebuilds the engine when the main model's provider changed.
func (s *Server) reloadIfActive(ctx context.Context, id string) {
	s.models.mu.Lock()
	active := s.models.active
	s.models.mu.Unlock()
	if eng := s.chatEngine(); active == id && eng != nil {
		if err := s.useModel(ctx, MainModel{Provider: id, Model: eng.Model()}); err != nil {
			log.Printf("reloading the main model: %v", err)
		}
	}
}

func (s *Server) refreshProvider(w http.ResponseWriter, r *http.Request) {
	ctx, id := r.Context(), r.PathValue("id")
	if isAgyProvider(id) {
		view, err := s.refreshAgy(ctx, id)
		var pub publicError
		switch {
		case errors.As(err, &pub):
			view, verr := s.agyProviderView(ctx, id)
			if verr != nil {
				http.Error(w, pub.msg, http.StatusConflict)
				return
			}
			view.LastError = pub.msg
			writeJSON(w, http.StatusOK, view)
		case err != nil:
			storeError(w, err)
		default:
			writeJSON(w, http.StatusOK, view)
		}
		return
	}
	if isCodexProvider(id) {
		view, err := s.refreshCodex(ctx, id)
		var pub publicError
		switch {
		case errors.As(err, &pub):
			view, verr := s.codexProvider(ctx, id)
			if verr != nil {
				http.Error(w, pub.msg, http.StatusConflict)
				return
			}
			view.LastError = pub.msg
			writeJSON(w, http.StatusOK, view)
		case err != nil:
			storeError(w, err)
		default:
			writeJSON(w, http.StatusOK, view)
		}
		return
	}
	if id == builtinProviderID {
		s.models.mu.Lock()
		c, model := s.models.builtin, s.models.builtinModel
		s.models.mu.Unlock()
		if c == nil {
			http.Error(w, "not found", http.StatusNotFound)
			return
		}
		pctx, cancel := context.WithTimeout(ctx, probeTimeout)
		models, err := c.Models(pctx)
		cancel()
		view := ProviderView{Builtin: true, HasKey: true, Provider: store.Provider{ID: id, Kind: c.Name, Name: builtinProviderName, BaseURL: c.BaseURL, Status: "ok"}}
		if err != nil {
			view.Status, view.LastError, view.Models = "error", providerError(err), []string{model}
		} else {
			s.models.mu.Lock()
			s.models.builtinModels = models
			s.models.mu.Unlock()
			view.Models = models
		}
		writeJSON(w, http.StatusOK, view)
		return
	}
	p, err := s.store.GetProvider(ctx, id)
	if err != nil {
		storeError(w, err)
		return
	}
	if k, _ := providerKind(p.Kind); k.Login {
		writeJSON(w, http.StatusOK, ProviderView{Provider: p})
		return
	}
	p = s.probe(ctx, p)
	saved, err := s.store.SaveProvider(ctx, p, "", s.now())
	if err != nil {
		storeError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, ProviderView{Provider: saved, HasKey: len(saved.Secret) > 0})
}

func (s *Server) deleteProvider(w http.ResponseWriter, r *http.Request) {
	wsID, ok := s.requireWorkspace(w, r)
	if !ok {
		return
	}
	ctx, id := store.WithWorkspace(r.Context(), wsID), r.PathValue("id")
	if id == builtinProviderID {
		http.Error(w, "o provedor do servidor é configurado no próprio servidor", http.StatusBadRequest)
		return
	}
	if cur, err := s.store.GetProvider(ctx, id); err != nil {
		storeError(w, err)
		return
	} else if cur.WorkspaceID != wsID {
		storeError(w, store.ErrNotFound)
		return
	}
	if err := s.store.DeleteProvider(ctx, id, actorFrom(r), s.now()); err != nil {
		storeError(w, err)
		return
	}
	s.models.mu.Lock()
	wasMain, builtin, model := s.models.active == id, s.models.builtin, s.models.builtinModel
	s.models.mu.Unlock()
	if wasMain {
		// The next best AI connected, the server's own model, or none.
		_ = s.store.SetSetting(ctx, mainModelKey, json.RawMessage(`{}`), store.ActorAdmin, s.now())
		if builtin != nil {
			s.SetBuiltinModel(builtin, model)
		} else {
			s.SetChatEngine(nil)
			s.models.mu.Lock()
			s.models.active = ""
			s.models.mu.Unlock()
		}
		s.models.mu.Lock()
		s.models.pinned = false
		s.models.mu.Unlock()
	}
	s.repick(ctx, true)
	w.WriteHeader(http.StatusNoContent)
}

func (s *Server) setMainModel(w http.ResponseWriter, r *http.Request) {
	var m MainModel
	if !readJSON(w, r, &m) {
		return
	}
	ctx := r.Context()
	m.Model = strings.TrimSpace(m.Model)
	if m.Provider == "" || m.Model == "" || len(m.Model) > 200 {
		http.Error(w, "escolha um provedor e um modelo", http.StatusBadRequest)
		return
	}
	var models []string
	if isAgyProvider(m.Provider) {
		models = s.agyKnownModelIDs(ctx, m.Provider)
	} else if isCodexProvider(m.Provider) {
		for _, cm := range s.cachedCodexModels(m.Provider) {
			models = append(models, cm.ID)
		}
		if _, _, err := s.codexTarget(ctx, m.Provider, false); err != nil {
			storeError(w, err)
			return
		}
	} else if m.Provider == builtinProviderID {
		s.models.mu.Lock()
		models = append([]string{}, s.models.builtinModels...)
		s.models.mu.Unlock()
	} else {
		p, err := s.store.GetProvider(ctx, m.Provider)
		if err != nil {
			storeError(w, err)
			return
		}
		models = p.Models
	}
	known := len(models) == 0
	for _, id := range models {
		known = known || id == m.Model
	}
	if !known {
		http.Error(w, "esse provedor não tem o modelo "+m.Model, http.StatusBadRequest)
		return
	}
	if err := s.useModel(ctx, m); err != nil {
		if errors.Is(err, store.ErrNotFound) {
			http.Error(w, "provedor não encontrado", http.StatusNotFound)
			return
		}
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}
	raw, _ := json.Marshal(m)
	if err := s.store.SetSetting(ctx, mainModelKey, raw, store.ActorAdmin, s.now()); err != nil {
		storeError(w, err)
		return
	}
	s.models.mu.Lock()
	s.models.pinned = true
	s.models.mu.Unlock()
	writeJSON(w, http.StatusOK, m)
}

// unpinMainModel: DELETE /v1/models/main, back to the automatic pick.
func (s *Server) unpinMainModel(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	if err := s.store.SetSetting(ctx, mainModelKey, json.RawMessage(`{}`), store.ActorAdmin, s.now()); err != nil {
		storeError(w, err)
		return
	}
	s.models.mu.Lock()
	s.models.pinned = false
	s.models.mu.Unlock()
	s.repick(ctx, true)
	w.WriteHeader(http.StatusNoContent)
}

// ChatEngineInfo describes the engine answering now ("" = none), for logs.
func (s *Server) ChatEngineInfo() string {
	eng := s.chatEngine()
	if eng == nil {
		return ""
	}
	return eng.Name() + ", " + eng.Model() + " via " + eng.Provider()
}
