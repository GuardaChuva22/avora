package control

import (
	"context"
	"log"
	"strings"
	"time"
)

// The model behind agents on "Automático" and the server's own quick work
// (routing, summaries, compaction). Nobody picks it in Settings: the server
// takes the best AI connected and moves on by itself when one is connected,
// signed in or removed. A choice pinned through PUT /v1/models/main (older
// versions saved one) holds until it is cleared or its provider goes.

// autoEvery is how often the automatic model is looked at again on its
// own, so a computer coming online (and its Codex or Antigravity) is seen.
const autoEvery = 45 * time.Second

// skipModel marks models that do not chat (embeddings, voice, images).
var skipModel = []string{"embed", "whisper", "tts", "dall-e", "image", "audio", "moderation", "rerank", "guard", "transcri", "realtime", "ocr"}

// modelFamilies rank an API's models for the automatic pick: the first
// family its list has wins; otherwise its first chat model.
var modelFamilies = []string{"claude", "gpt", "gemini", "grok", "deepseek", "mistral-large", "kimi", "qwen", "llama"}

// chatModel is the model an API answers with on "Automático".
func chatModel(models []string) string {
	chat := []string{}
	for _, m := range models {
		low, skip := strings.ToLower(m), false
		for _, w := range skipModel {
			skip = skip || strings.Contains(low, w)
		}
		if !skip {
			chat = append(chat, m)
		}
	}
	for _, f := range modelFamilies {
		for _, m := range chat {
			if strings.Contains(strings.ToLower(m), f) {
				return m
			}
		}
	}
	if len(chat) > 0 {
		return chat[0]
	}
	return ""
}

// autoMain is the best AI connected now: a ChatGPT account (Codex), the
// Antigravity accounts, an API that passed its last test, the server's own
// model, then free tiers that may train on what they get (NVIDIA).
func (s *Server) autoMain(ctx context.Context) (MainModel, bool) {
	list, err := s.store.ListProviders(ctx)
	if err != nil {
		return MainModel{}, false
	}
	views := make([]ProviderView, 0, len(list))
	for _, p := range list {
		views = append(views, ProviderView{Provider: p})
	}
	views = s.agyViews(ctx, s.codexViews(ctx, views, ""))
	var codex, agy, api, free []MainModel
	for _, v := range views {
		switch {
		case v.Kind == "codex" && v.CodexMain:
			codex = append(codex, MainModel{Provider: v.ID})
		case v.ID == agyBrain && v.CodexMain:
			agy = append(agy, MainModel{Provider: agyBrain})
		case v.Status == "ok":
			k, ok := providerKind(v.Kind)
			model := chatModel(v.Models)
			if !ok || !k.Main || k.Login || model == "" {
				continue
			}
			if v.Kind == "nvidia" {
				free = append(free, MainModel{Provider: v.ID, Model: model})
			} else {
				api = append(api, MainModel{Provider: v.ID, Model: model})
			}
		}
	}
	s.models.mu.Lock()
	if b := s.models.builtin; b != nil {
		m := MainModel{Provider: builtinProviderID, Model: s.models.builtinModel}
		if b.Name == "nvidia" {
			free = append(free, m)
		} else {
			api = append(api, m)
		}
	}
	s.models.mu.Unlock()
	for _, group := range [][]MainModel{codex, agy, api, free} {
		if len(group) > 0 {
			return group[0], true
		}
	}
	return MainModel{}, false
}

// repick puts the automatic model in place, unless one is pinned. force
// skips the wait between looks (something was just connected or removed).
func (s *Server) repick(ctx context.Context, force bool) {
	s.models.mu.Lock()
	if s.models.pinned || s.models.picking || !force && time.Since(s.models.looked) < autoEvery {
		s.models.mu.Unlock()
		return
	}
	s.models.picking, s.models.looked = true, time.Now()
	active := s.models.active
	s.models.mu.Unlock()
	defer func() {
		s.models.mu.Lock()
		s.models.picking = false
		s.models.mu.Unlock()
	}()
	m, ok := s.autoMain(ctx)
	if !ok {
		// Nothing ready right now (a computer offline): what answered stays,
		// with its own error, until something else is connected.
		return
	}
	if eng := s.currentEngine(); eng != nil && active == m.Provider && (m.Model == "" || eng.Model() == m.Model) {
		return
	}
	if err := s.useModel(ctx, m); err != nil {
		log.Printf("automatic model %s on %s: %v", m.Model, m.Provider, err)
		return
	}
	log.Printf("automatic model: %s", s.ChatEngineInfo())
}

// autoEngine is chatEngine's way out when nothing answers yet: it looks for
// a connected AI, at most every autoEvery.
func (s *Server) autoEngine() ChatEngine {
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	s.repick(ctx, false)
	return s.currentEngine()
}
