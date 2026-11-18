package control

import (
	"bytes"
	"context"
	"errors"
	"io"
	"net/http"

	"github.com/GuardaChuva22/avora/internal/store"
)

// Replying to a message, archiving and deleting conversations, and the
// agents' photos.

// MaxAvatarBytes bounds an agent photo. The app sends a 512 px WebP, a few
// tens of KB.
const MaxAvatarBytes = 512 * 1024

// messageIn finds message id in a conversation.
func (s *Server) messageIn(ctx context.Context, convID string, id int64) (store.ChatMessage, error) {
	msgs, err := s.store.Messages(ctx, convID)
	if err != nil {
		return store.ChatMessage{}, err
	}
	for _, m := range msgs {
		if m.ID == id {
			return m, nil
		}
	}
	return store.ChatMessage{}, store.ErrNotFound
}

// ConversationPatch changes a conversation: today only whether it is archived.
type ConversationPatch struct {
	Archived *bool `json:"archived"`
}

func (s *Server) patchConversation(w http.ResponseWriter, r *http.Request) {
	var req ConversationPatch
	if !readJSON(w, r, &req) {
		return
	}
	id := r.PathValue("id")
	if req.Archived == nil {
		http.Error(w, "nothing to change", http.StatusBadRequest)
		return
	}
	if s.isChannel(r.Context(), id) {
		http.Error(w, "channels are archived in PATCH /v1/channels/{id}", http.StatusBadRequest)
		return
	}
	c, err := s.store.ArchiveConversation(r.Context(), id, *req.Archived, store.ActorAdmin, s.now())
	if err != nil {
		storeError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, c)
}

func (s *Server) deleteConversation(w http.ResponseWriter, r *http.Request) {
	id := r.PathValue("id")
	if s.isChannel(r.Context(), id) {
		http.Error(w, "channels are archived, not deleted", http.StatusBadRequest)
		return
	}
	err := s.store.DeleteConversation(r.Context(), id, store.ActorAdmin, s.now())
	if errors.Is(err, store.ErrConflict) {
		http.Error(w, "the agent is still answering in this conversation", http.StatusConflict)
		return
	}
	if err != nil {
		storeError(w, err)
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

// avatarType names the image format from its first bytes; only the formats
// the app shows are accepted.
func avatarType(b []byte) string {
	switch {
	case len(b) >= 12 && bytes.Equal(b[:4], []byte("RIFF")) && bytes.Equal(b[8:12], []byte("WEBP")):
		return "image/webp"
	case bytes.HasPrefix(b, []byte("\x89PNG\r\n\x1a\n")):
		return "image/png"
	case bytes.HasPrefix(b, []byte{0xFF, 0xD8, 0xFF}):
		return "image/jpeg"
	}
	return ""
}

func (s *Server) putAvatar(w http.ResponseWriter, r *http.Request) {
	data, err := io.ReadAll(http.MaxBytesReader(w, r.Body, MaxAvatarBytes+1))
	if err != nil || len(data) > MaxAvatarBytes {
		http.Error(w, "the photo must have at most 512 KB", http.StatusRequestEntityTooLarge)
		return
	}
	kind := avatarType(data)
	if kind == "" {
		http.Error(w, "the photo must be WebP, PNG or JPEG", http.StatusBadRequest)
		return
	}
	if err := s.store.SetAvatar(r.Context(), r.PathValue("id"), data, kind, store.ActorAdmin, s.now()); err != nil {
		storeError(w, err)
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

func (s *Server) getAvatar(w http.ResponseWriter, r *http.Request) {
	a, err := s.store.GetAvatar(r.Context(), r.PathValue("id"))
	if err != nil {
		storeError(w, err)
		return
	}
	w.Header().Set("Content-Type", a.ContentType)
	w.Header().Set("Cache-Control", "private, max-age=86400")
	w.Header().Set("X-Content-Type-Options", "nosniff")
	_, _ = w.Write(a.Data)
}

func (s *Server) deleteAvatar(w http.ResponseWriter, r *http.Request) {
	if err := s.store.DeleteAvatar(r.Context(), r.PathValue("id"), store.ActorAdmin, s.now()); err != nil {
		storeError(w, err)
		return
	}
	w.WriteHeader(http.StatusNoContent)
}
