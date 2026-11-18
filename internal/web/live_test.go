package web

import (
	"context"
	"os"
	"testing"
)

// TestLive hits the real internet; run with AVORA_LIVE=1.
func TestLive(t *testing.T) {
	if os.Getenv("AVORA_LIVE") == "" {
		t.Skip("set AVORA_LIVE=1")
	}
	c := New(Options{BraveKey: os.Getenv("AVORA_BRAVE_API_KEY")})
	res, err := c.Search(context.Background(), "preço notebook lenovo ideapad", 5)
	if err != nil {
		t.Fatal(err)
	}
	for _, r := range res {
		t.Logf("%s | %s | %.80s", r.Title, r.URL, r.Snippet)
	}
	if len(res) == 0 {
		t.Fatal("no results")
	}
	p, err := c.Read(context.Background(), "https://pt.wikipedia.org/wiki/Go_(linguagem_de_programa%C3%A7%C3%A3o)")
	if err != nil {
		t.Fatal(err)
	}
	t.Logf("title=%q len=%d links=%d\n%.600s", p.Title, len(p.Text), len(p.Links), p.Text)
}
