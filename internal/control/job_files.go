package control

import (
	"io"
	"net/http"
	"path"
	"strings"

	"github.com/GuardaChuva22/avora/internal/store"
)

// Files a run makes on a node: today the images Codex draws as the main
// model. The node sends each one while the job runs and gets an id back;
// the answer then shows it as ![…](avora:file/<id>).
func (s *Server) jobFile(w http.ResponseWriter, r *http.Request, node store.Node) {
	job, err := s.store.GetJob(r.Context(), r.PathValue("id"))
	if err != nil {
		storeError(w, err)
		return
	}
	if job.DeviceID != node.ID {
		storeError(w, store.ErrForbidden)
		return
	}
	if job.Status != store.StatusRunning || job.Action != store.ActionCodexMain || job.Agent == nil {
		storeError(w, store.ErrConflict)
		return
	}
	data, err := io.ReadAll(http.MaxBytesReader(w, r.Body, store.MaxFileBytes))
	if err != nil || len(data) == 0 {
		http.Error(w, "file empty or too large", http.StatusBadRequest)
		return
	}
	mime := http.DetectContentType(data)
	if !strings.HasPrefix(mime, "image/") {
		http.Error(w, "only images", http.StatusBadRequest)
		return
	}
	name := path.Base(strings.ReplaceAll(r.URL.Query().Get("name"), `\`, "/"))
	if name == "" || name == "." || name == "/" {
		name = "imagem"
	}
	f, err := s.store.SaveFile(r.Context(), store.File{ID: newID(), AgentID: strings.TrimPrefix(job.Agent.Workdir, "agents/"),
		Name: truncate(name, 120), Mime: mime, Data: data, CreatedAt: s.now()})
	if err != nil {
		storeError(w, err)
		return
	}
	writeJSON(w, http.StatusCreated, map[string]string{"id": f.ID})
}
