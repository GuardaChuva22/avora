package store

import (
	"context"
	"encoding/json"
	"errors"
	"strings"
	"testing"
	"time"
)

func TestProvidersAndSettings(t *testing.T) {
	eachStore(t, func(t *testing.T, s Store) {
		ctx := context.Background()
		t0 := time.Date(2026, 11, 12, 10, 0, 0, 0, time.UTC)
		p, err := s.SaveProvider(ctx, Provider{ID: "nvidia", Kind: "nvidia", Name: "NVIDIA", BaseURL: "https://x", Status: "ok",
			Models: []string{"a", "b"}, Secret: []byte("sealed")}, ActorAdmin, t0)
		if err != nil || len(p.Models) != 2 || !p.CreatedAt.Equal(t0) {
			t.Fatalf("save: %+v %v", p, err)
		}
		if _, err := s.SaveProvider(ctx, Provider{ID: "claude.2", Kind: "claude", Name: "Claude 2", Status: "login", NodeID: "n1"}, "", t0.Add(time.Minute)); err != nil {
			t.Fatal(err)
		}
		p.Status, p.Account = "error", "x"
		if _, err := s.SaveProvider(ctx, p, "", t0.Add(time.Hour)); err != nil {
			t.Fatal(err)
		}
		got, err := s.GetProvider(ctx, "nvidia")
		if err != nil || got.Status != "error" || string(got.Secret) != "sealed" || !got.CreatedAt.Equal(t0) || got.Models[1] != "b" {
			t.Fatalf("get: %+v %v", got, err)
		}
		list, _ := s.ListProviders(ctx)
		if len(list) != 2 || list[0].ID != "nvidia" || list[1].NodeID != "n1" || list[1].Models == nil {
			t.Fatalf("list: %+v", list)
		}
		if err := s.DeleteProvider(ctx, "claude.2", ActorAdmin, t0); err != nil {
			t.Fatal(err)
		}
		if _, err := s.GetProvider(ctx, "claude.2"); !errors.Is(err, ErrNotFound) {
			t.Fatalf("after delete: %v", err)
		}

		if _, err := s.GetSetting(ctx, "main_model"); !errors.Is(err, ErrNotFound) {
			t.Fatalf("unset setting: %v", err)
		}
		if err := s.SetSetting(ctx, "main_model", json.RawMessage(`{"provider":"nvidia","model":"a"}`), ActorAdmin, t0); err != nil {
			t.Fatal(err)
		}
		s.SetSetting(ctx, "main_model", json.RawMessage(`{"provider":"nvidia","model":"b"}`), "", t0)
		v, err := s.GetSetting(ctx, "main_model")
		if err != nil || !strings.Contains(string(v), `"b"`) {
			t.Fatalf("setting: %s %v", v, err)
		}
		events, _ := s.Events(ctx, EventFilter{})
		for _, e := range events {
			if strings.Contains(string(e.Payload), "sealed") {
				t.Fatal("secret in the log")
			}
		}
	})
}
