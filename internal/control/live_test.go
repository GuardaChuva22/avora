package control

import (
	"bufio"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strconv"
	"strings"
	"testing"
	"time"
)

// liveReader reads the "ev" events of a live stream.
type liveReader struct {
	t    *testing.T
	sc   *bufio.Scanner
	resp *http.Response
}

func openLive(t *testing.T, srv *httptest.Server, after int64) *liveReader {
	t.Helper()
	req, _ := http.NewRequestWithContext(t.Context(), "GET", srv.URL+"/v1/live?after="+strconv.FormatInt(after, 10), nil)
	req.Header.Set("Authorization", "Bearer "+adminTestToken)
	resp, err := http.DefaultClient.Do(req)
	if err != nil || resp.StatusCode != 200 {
		t.Fatalf("live: %v %v", err, resp)
	}
	t.Cleanup(func() { resp.Body.Close() })
	return &liveReader{t: t, sc: bufio.NewScanner(resp.Body), resp: resp}
}

// next returns the next event of the given kind ("hello" or "ev").
func (l *liveReader) next(kind string) map[string]any {
	l.t.Helper()
	done := make(chan map[string]any, 1)
	go func() {
		event := ""
		for l.sc.Scan() {
			line := l.sc.Text()
			switch {
			case strings.HasPrefix(line, "event: "):
				event = strings.TrimPrefix(line, "event: ")
			case strings.HasPrefix(line, "data: ") && event == kind:
				var m map[string]any
				_ = json.Unmarshal([]byte(strings.TrimPrefix(line, "data: ")), &m)
				done <- m
				return
			}
		}
		done <- nil
	}()
	select {
	case m := <-done:
		if m == nil {
			l.t.Fatal("stream ended")
		}
		return m
	case <-time.After(5 * time.Second):
		l.t.Fatalf("no %s event", kind)
	}
	return nil
}

func TestLive(t *testing.T) {
	s := chatServer(t, &fakeEngine{})
	srv := httptest.NewServer(s.Handler())
	t.Cleanup(srv.Close) // after the streams close (cleanups run last first)

	l := openLive(t, srv, 0)
	hello := l.next("hello")
	last := int64(hello["last"].(float64))

	// What happens after the app connects reaches it at once.
	c := newConversation(t, s)
	ev := l.next("ev")
	if ev["type"] != "conversation.created" || int64(ev["id"].(float64)) <= last {
		t.Fatalf("event: %v", ev)
	}
	if p, _ := ev["payload"].(map[string]any); p == nil {
		t.Fatalf("payload: %v", ev)
	}
	seen := int64(ev["id"].(float64))

	// Reconnecting with the last id seen replays what was missed.
	request(t, s, "PATCH", "/v1/conversations/"+c.ID, adminTestToken, map[string]any{"title": "outro"})
	l2 := openLive(t, srv, seen-1)
	if h := l2.next("hello"); h["reset"] != false {
		t.Fatalf("hello: %v", h)
	}
	if ev := l2.next("ev"); int64(ev["id"].(float64)) != seen {
		t.Fatalf("replay: %v", ev)
	}

	// No token, no stream.
	resp, err := http.Get(srv.URL + "/v1/live")
	if err != nil || resp.StatusCode != 401 {
		t.Fatalf("unauthenticated: %v %v", err, resp.StatusCode)
	}
}
