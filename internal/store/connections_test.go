package store

import (
	"context"
	"errors"
	"strings"
	"testing"
	"time"
)

func TestConnections(t *testing.T) {
	eachStore(t, func(t *testing.T, s Store) {
		ctx := context.Background()
		t0 := time.Date(2026, 11, 12, 10, 0, 0, 0, time.UTC)
		c, err := s.SaveConnection(ctx, Connection{Kind: "clickup", Account: "Gui", Status: "ok", Secret: []byte("sealed")}, ActorAdmin, t0)
		if err != nil || c.Agents == nil || !c.CreatedAt.Equal(t0) {
			t.Fatalf("save: %+v %v", c, err)
		}
		// A refresh (no actor) keeps created_at and records nothing.
		ok := t0.Add(time.Hour)
		c.Secret, c.LastOK, c.Agents = []byte("sealed2"), &ok, []string{"atlas"}
		if _, err := s.SaveConnection(ctx, c, "", t0.Add(time.Hour)); err != nil {
			t.Fatal(err)
		}
		got, err := s.GetConnection(ctx, "clickup")
		if err != nil || string(got.Secret) != "sealed2" || !got.CreatedAt.Equal(t0) || got.LastOK == nil || len(got.Agents) != 1 {
			t.Fatalf("get: %+v %v", got, err)
		}
		// A second account of the same app lives beside the first, after it.
		if _, err := s.SaveConnection(ctx, Connection{ID: "clickup.b2", Kind: "clickup", Account: "Ana", Status: "ok", Secret: []byte("s")}, "", t0.Add(time.Minute)); err != nil {
			t.Fatal(err)
		}
		if _, err := s.SaveConnection(ctx, Connection{Kind: "asana", Account: "Gui", Status: "ok", Secret: []byte("s")}, "", t0.Add(2*time.Minute)); err != nil {
			t.Fatal(err)
		}
		list, _ := s.ListConnections(ctx)
		if len(list) != 3 || list[0].ID != "asana" || list[1].ID != "clickup" || list[2].ID != "clickup.b2" || list[2].Kind != "clickup" {
			t.Fatalf("list: %+v", list)
		}
		if err := s.DeleteConnection(ctx, "clickup.b2", "", t0); err != nil {
			t.Fatal(err)
		}
		if got, err := s.GetConnection(ctx, "clickup"); err != nil || got.Account != "Gui" {
			t.Fatalf("first account after deleting the second: %+v %v", got, err)
		}
		if err := s.DeleteConnection(ctx, "clickup", ActorAdmin, t0); err != nil {
			t.Fatal(err)
		}
		if _, err := s.GetConnection(ctx, "clickup"); !errors.Is(err, ErrNotFound) {
			t.Fatalf("after delete: %v", err)
		}
		events, _ := s.Events(ctx, EventFilter{})
		saved := 0
		for _, e := range events {
			if e.Type == EventConnectionSaved {
				saved++
				if strings.Contains(string(e.Payload), "sealed") {
					t.Fatal("the secret reached the event log")
				}
			}
		}
		if saved != 1 {
			t.Fatalf("connection.saved events: %d", saved)
		}
	})
}
