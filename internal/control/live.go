package control

import (
	"net/http"
	"strconv"
	"time"

	"github.com/GuardaChuva22/avora/internal/store"
)

// Live (D47): one stream per app with everything that happens, as it
// happens, in place of the app asking every few seconds. It is the event
// log (messages, agent steps, approvals, missions, jobs, connections...)
// read as it grows: the app reacts to each event by reading what changed.
// After a drop the app reconnects with the last id it saw and gets what it
// missed; past liveReplay events it gets "reset" and reads everything.

const (
	livePoll      = 200 * time.Millisecond
	liveKeepalive = 15 * time.Second
	liveReplay    = 1000
	// liveWindow: ids are taken in insert order but committed in any order,
	// so each read looks this far back for ids that committed late.
	liveWindow = 64
)

// LiveEvent is one event as the app gets it.
type LiveEvent struct {
	ID      int64  `json:"id"`
	Type    string `json:"type"`
	JobID   string `json:"job_id,omitempty"`
	Payload any    `json:"payload,omitempty"`
}

func (s *Server) registerLive(mux *http.ServeMux) {
	mux.HandleFunc("GET /v1/live", s.admin(s.live))
}

func (s *Server) live(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	after, _ := strconv.ParseInt(r.URL.Query().Get("after"), 10, 64)
	latest := int64(0)
	if evs, err := s.store.Events(ctx, store.EventFilter{Latest: true, Limit: 1}); err == nil && len(evs) == 1 {
		latest = evs[0].ID
	}
	reset := false
	switch {
	case after <= 0 || after > latest:
		after = latest
	case latest-after > liveReplay:
		after, reset = latest, true
	}
	stream := openStream(w, time.Hour)
	rc := http.NewResponseController(w)
	send := func(event string, data any) {
		_ = rc.SetWriteDeadline(time.Now().Add(time.Minute))
		stream.send(event, data)
	}
	send("hello", map[string]any{"last": after, "reset": reset})

	// sent holds the ids already sent within the window behind the newest.
	sent := map[int64]bool{}
	high := after
	poll := time.NewTicker(livePoll)
	defer poll.Stop()
	alive := time.NewTicker(liveKeepalive)
	defer alive.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-alive.C:
			_ = rc.SetWriteDeadline(time.Now().Add(time.Minute))
			stream.comment()
		case <-poll.C:
			evs, err := s.store.Events(ctx, store.EventFilter{AfterID: max(0, high-liveWindow), Limit: 500})
			if err != nil {
				continue
			}
			for _, e := range evs {
				if e.ID <= after || sent[e.ID] {
					continue
				}
				sent[e.ID] = true
				high = max(high, e.ID)
				send("ev", LiveEvent{ID: e.ID, Type: e.Type, JobID: e.JobID, Payload: e.Payload})
			}
			for id := range sent {
				if id <= high-liveWindow {
					delete(sent, id)
				}
			}
		}
	}
}
