package control

import (
	"context"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"strconv"
	"strings"
	"time"
	"unicode/utf8"

	"github.com/GuardaChuva22/avora/internal/llm"
	"github.com/GuardaChuva22/avora/internal/store"
)

const (
	// ReplyTimeout bounds one model answer, streaming included.
	ReplyTimeout   = 5 * time.Minute
	maxMessageSize = 32 * 1024
)

// ChatEngine answers conversations. The native engine implements it.
type ChatEngine interface {
	Name() string
	Model() string
	Provider() string
	// Reply answers the last message of history as the agent described by
	// system (empty: the engine's own default prompt).
	Reply(ctx context.Context, system string, history []store.ChatMessage, onDelta func(llm.Delta)) (llm.Response, error)
}

type engineBox struct{ e ChatEngine }

// SetChatEngine enables the conversation endpoints (nil disables them).
func (s *Server) SetChatEngine(e ChatEngine) { s.engine.Store(&engineBox{e}) }

// chatEngine is the engine answering for agents on "Automático" and the
// server's own quick work, or nil when no AI is connected.
func (s *Server) chatEngine() ChatEngine {
	if e := s.currentEngine(); e != nil {
		return e
	}
	return s.autoEngine()
}

// currentEngine is the engine in place now, without looking for one.
func (s *Server) currentEngine() ChatEngine {
	if b := s.engine.Load(); b != nil {
		return b.e
	}
	return nil
}

type ConversationRequest struct {
	Title   string `json:"title"`
	AgentID string `json:"agent_id"`
}

type MessageRequest struct {
	Content string `json:"content"`
	// ReplyTo answers a specific earlier message of the same conversation.
	ReplyTo int64 `json:"reply_to,omitempty"`
}

type MessageResponse struct {
	User      store.ChatMessage `json:"user"`
	Assistant store.ChatMessage `json:"assistant"`
}

func (s *Server) registerChat(mux *http.ServeMux) {
	mux.HandleFunc("POST /v1/conversations", s.admin(s.createConversation))
	mux.HandleFunc("GET /v1/conversations", s.admin(s.listConversations))
	mux.HandleFunc("GET /v1/conversations/{id}", s.admin(s.getConversation))
	mux.HandleFunc("GET /v1/conversations/{id}/messages", s.admin(s.listMessages))
	mux.HandleFunc("POST /v1/conversations/{id}/messages", s.admin(s.sendMessage))
	mux.HandleFunc("POST /v1/conversations/{id}/stop", s.admin(s.stopConversation))
	mux.HandleFunc("PATCH /v1/conversations/{id}", s.admin(s.patchConversation))
	mux.HandleFunc("DELETE /v1/conversations/{id}", s.admin(s.deleteConversation))
	mux.HandleFunc("GET /v1/channel", s.admin(s.channelStatus))
	s.registerAgents(mux)
	s.registerAgentPage(mux)
	s.registerRoutines(mux)
	s.registerApprovals(mux)
}

// conversationAgent is the agent of a conversation; old conversations
// without one belong to the default agent.
func (s *Server) conversationAgent(ctx context.Context, c store.Conversation) (store.Agent, error) {
	id := c.AgentID
	if id == "" {
		id = DefaultAgentID
	}
	return s.store.GetAgent(ctx, id)
}

func (s *Server) engineActor() string {
	eng := s.chatEngine()
	if eng == nil {
		return store.ActorSystem
	}
	return "engine:" + eng.Name()
}

func (s *Server) createConversation(w http.ResponseWriter, r *http.Request) {
	wsID, ok := s.requireWorkspace(w, r)
	if !ok {
		return
	}
	ctx := store.WithWorkspace(r.Context(), wsID)
	var req ConversationRequest
	if !readJSON(w, r, &req) {
		return
	}
	req.Title = strings.TrimSpace(req.Title)
	if utf8.RuneCountInString(req.Title) > 200 {
		http.Error(w, "title too long", http.StatusBadRequest)
		return
	}
	if req.AgentID == "" {
		req.AgentID = DefaultAgentID
		if wsID != store.DefaultWorkspaceID {
			if list, err := s.store.ListAgents(ctx); err == nil && len(list) > 0 {
				req.AgentID = list[0].ID
			}
		}
	}
	agent, err := s.store.GetAgent(ctx, req.AgentID)
	if err != nil {
		storeError(w, err)
		return
	}
	if agent.WorkspaceID != wsID {
		storeError(w, store.ErrNotFound)
		return
	}
	eng := s.chatEngine()
	if eng == nil {
		http.Error(w, "no chat engine configured", http.StatusServiceUnavailable)
		return
	}
	model := eng.Model()
	id, err := randomBytes(12)
	if err != nil {
		http.Error(w, "failed to create conversation", http.StatusInternalServerError)
		return
	}
	c, err := s.store.CreateConversation(ctx, store.Conversation{
		ID: hex.EncodeToString(id), WorkspaceID: wsID, AgentID: agent.ID, Title: req.Title, Engine: "native", Model: model, CreatedAt: s.now(),
	}, actorFrom(r))
	if err != nil {
		storeError(w, err)
		return
	}
	writeJSON(w, http.StatusCreated, c)
}

func (s *Server) listConversations(w http.ResponseWriter, r *http.Request) {
	wsID, ok := s.requireWorkspace(w, r)
	if !ok {
		return
	}
	ctx := store.WithWorkspace(r.Context(), wsID)
	limit := 50
	if v := r.URL.Query().Get("limit"); v != "" {
		n, err := strconv.Atoi(v)
		if err != nil || n < 1 || n > 500 {
			http.Error(w, "invalid limit", http.StatusBadRequest)
			return
		}
		limit = n
	}
	list, err := s.store.ListConversations(ctx, r.URL.Query().Get("agent"), 1000)
	if err != nil {
		storeError(w, err)
		return
	}
	// Channels have their own endpoints (GET /v1/channels). Archived conversations
	// show only with ?archived=1.
	archived := r.URL.Query().Get("archived") == "1"
	out := make([]store.Conversation, 0, len(list))
	for _, c := range list {
		if c.Engine != "team" && (c.ArchivedAt != nil) == archived && len(out) < limit {
			out = append(out, c)
		}
	}
	list = out
	writeJSON(w, http.StatusOK, list)
}

func (s *Server) getConversation(w http.ResponseWriter, r *http.Request) {
	wsID, ok := s.requireWorkspace(w, r)
	if !ok {
		return
	}
	c, err := s.store.GetConversation(r.Context(), r.PathValue("id"))
	if err != nil {
		storeError(w, err)
		return
	}
	if c.WorkspaceID != wsID {
		storeError(w, store.ErrNotFound)
		return
	}
	writeJSON(w, http.StatusOK, c)
}

func (s *Server) listMessages(w http.ResponseWriter, r *http.Request) {
	wsID, ok := s.requireWorkspace(w, r)
	if !ok {
		return
	}
	conv, err := s.store.GetConversation(r.Context(), r.PathValue("id"))
	if err != nil {
		storeError(w, err)
		return
	}
	if conv.WorkspaceID != wsID {
		storeError(w, store.ErrNotFound)
		return
	}
	msgs, err := s.store.Messages(r.Context(), conv.ID)
	if err != nil {
		storeError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, msgs)
}

// sse writes server-sent events and flushes each one.
type sse struct {
	w  http.ResponseWriter
	rc *http.ResponseController
}

func (e sse) send(event string, data any) {
	b, err := json.Marshal(data)
	if err != nil {
		return
	}
	fmt.Fprintf(e.w, "event: %s\ndata: %s\n\n", event, b)
	_ = e.rc.Flush()
}

// comment keeps an idle stream open through proxies.
func (e sse) comment() {
	fmt.Fprint(e.w, ": keepalive\n\n")
	_ = e.rc.Flush()
}

func openStream(w http.ResponseWriter, d time.Duration) *sse {
	rc := http.NewResponseController(w)
	_ = rc.SetWriteDeadline(time.Now().Add(d))
	w.Header().Set("Content-Type", "text/event-stream")
	w.Header().Set("Cache-Control", "no-store")
	w.Header().Set("X-Accel-Buffering", "no")
	w.WriteHeader(http.StatusOK)
	return &sse{w: w, rc: rc}
}

func (s *Server) sendMessage(w http.ResponseWriter, r *http.Request) {
	wsID, ok := s.requireWorkspace(w, r)
	if !ok {
		return
	}
	r = r.WithContext(store.WithWorkspace(r.Context(), wsID))
	var req MessageRequest
	if !readJSONMax(w, r, &req, maxMessageSize+1024) {
		return
	}
	req.Content = strings.TrimSpace(req.Content)
	if req.Content == "" || len(req.Content) > maxMessageSize {
		http.Error(w, "content must have 1 to 32768 bytes", http.StatusBadRequest)
		return
	}
	convID := r.PathValue("id")
	conv, err := s.store.GetConversation(r.Context(), convID)
	if err != nil {
		storeError(w, err)
		return
	}
	if conv.WorkspaceID != wsID {
		storeError(w, store.ErrNotFound)
		return
	}
	if req.ReplyTo != 0 {
		if _, err := s.messageIn(r.Context(), conv.ID, req.ReplyTo); err != nil {
			http.Error(w, "reply_to is not a message of this conversation", http.StatusBadRequest)
			return
		}
	}
	if conv.Engine == "team" {
		s.sendChannel(w, r, conv.ID, req.Content, req.ReplyTo)
		return
	}
	if conv.JobID != "" {
		http.Error(w, "the agent is still answering the previous message", http.StatusConflict)
		return
	}
	agent, err := s.conversationAgent(r.Context(), conv)
	if err != nil {
		storeError(w, err)
		return
	}
	wantsStream := strings.Contains(r.Header.Get("Accept"), "text/event-stream")
	if s.chatEngine() == nil {
		http.Error(w, "no chat engine configured", http.StatusServiceUnavailable)
		return
	}
	id := newID()
	events, unsubscribe := s.hub.subscribe(id)
	defer unsubscribe()
	user, err := s.startTurn(r.Context(), id, conv, agent, req.Content, req.ReplyTo, "", store.ActorAdmin, nil)
	switch {
	case errors.Is(err, store.ErrConflict):
		http.Error(w, "the agent is still answering the previous message", http.StatusConflict)
		return
	case err != nil && user.ID == 0:
		storeError(w, err)
		return
	}
	if !wantsStream {
		// Waits for the answer; if the client leaves, the turn goes on.
		_ = http.NewResponseController(w).SetWriteDeadline(time.Now().Add((TurnMinutes + 1) * time.Minute))
		limit := time.NewTimer(TurnMinutes * time.Minute)
		defer limit.Stop()
		for {
			select {
			case <-r.Context().Done():
				return
			case <-limit.C:
				writeJSON(w, http.StatusAccepted, map[string]any{"user": user, "job_id": id})
				return
			case ev := <-events:
				if ev.kind != "done" {
					continue
				}
				answer := ev.data.(store.ChatMessage)
				if answer.Error != "" && answer.Content == "" {
					http.Error(w, answer.Error, http.StatusBadGateway)
					return
				}
				writeJSON(w, http.StatusOK, MessageResponse{User: user, Assistant: answer})
				return
			}
		}
	}
	stream := openStream(w, (TurnMinutes+1)*time.Minute)
	stream.send("user", user)
	if err != nil {
		// The turn failed before starting; its message is stored.
		msgs, _ := s.store.Messages(r.Context(), convID)
		if n := len(msgs); n > 0 && msgs[n-1].Role == "assistant" {
			stream.send("done", msgs[n-1])
		}
		return
	}
	s.streamAgentReply(r, stream, Job{ID: id}, events)
}

// stopConversation stops the answer in flight: every one in #time, or the
// agent job of a direct conversation.
func (s *Server) stopConversation(w http.ResponseWriter, r *http.Request) {
	wsID, ok := s.requireWorkspace(w, r)
	if !ok {
		return
	}
	conv, err := s.store.GetConversation(r.Context(), r.PathValue("id"))
	if err != nil {
		storeError(w, err)
		return
	}
	if conv.WorkspaceID != wsID {
		storeError(w, store.ErrNotFound)
		return
	}
	if conv.Engine == "team" {
		s.stopChannel(r.Context(), conv.ID)
	} else if conv.JobID != "" && !s.turns.stop(conv.JobID) {
		if job, err := s.store.CancelJob(r.Context(), conv.JobID, store.ActorAdmin, s.now()); err == nil {
			s.finishAgentReply(job, nil)
		}
	}
	w.WriteHeader(http.StatusNoContent)
}

// publicLLMError says what went wrong without echoing provider internals.
func publicLLMError(err error) string {
	var apiErr *llm.APIError
	var pub publicError
	switch {
	case errors.As(err, &pub):
		return pub.msg
	case errors.As(err, &apiErr) && apiErr.Status == http.StatusTooManyRequests:
		return "model provider rate limit reached; try again shortly"
	case errors.As(err, &apiErr) && (apiErr.Status == http.StatusUnauthorized || apiErr.Status == http.StatusForbidden):
		return "model provider rejected the API key"
	case errors.As(err, &apiErr):
		return fmt.Sprintf("model provider error (%d)", apiErr.Status)
	case errors.Is(err, context.DeadlineExceeded):
		return "model took too long to answer"
	default:
		return "model call failed"
	}
}

func (s *Server) recordLLMFailure(convID string, err error, took time.Duration) {
	detail := err.Error()
	if len(detail) > 300 {
		detail = detail[:300]
	}
	status := 0
	var apiErr *llm.APIError
	if errors.As(err, &apiErr) {
		status = apiErr.Status
	}
	provider, model := "", ""
	if eng := s.chatEngine(); eng != nil {
		provider, model = eng.Provider(), eng.Model()
	}
	payload, _ := json.Marshal(map[string]any{
		"conversation_id": convID, "provider": provider, "model": model,
		"status": status, "error": detail, "duration_ms": took.Milliseconds(),
	})
	_ = s.store.RecordEvent(context.Background(), store.Event{
		At: s.now(), Type: store.EventLLMFailed, Actor: s.engineActor(), Payload: payload,
	})
}
