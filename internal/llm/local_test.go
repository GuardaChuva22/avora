package llm

import "testing"

func TestLocalURL(t *testing.T) {
	for u, want := range map[string]bool{
		"http://127.0.0.1:8080/v1":       true,
		"http://localhost:8080/v1":       true,
		"http://100.101.102.103:8080/v1": true,
		"http://pc.tail1234.ts.net/v1":   true,
		"http://100.128.0.1/v1":          false, // outside 100.64.0.0/10
		"http://192.168.0.10/v1":         true,  // local network
		"http://10.0.0.5:11434/v1":       true,
		"http://pc.local:1234/v1":        true,
		"http://8.8.8.8/v1":              false,
		"http://example.com/v1":          false,
		"https://100.101.102.103/v1":     false, // https needs no exception
		"http://evil.ts.net.example/v1":  false,
	} {
		if got := LocalURL(u); got != want {
			t.Errorf("%s: %v", u, got)
		}
	}
	if _, err := NewProvider("local", "http://100.101.102.103:8080/v1", "", 0); err != nil {
		t.Fatalf("local without key: %v", err)
	}
	if _, err := NewProvider("x", "https://api.example.com/v1", "", 0); err == nil {
		t.Fatal("cloud without key accepted")
	}
	if _, err := NewProvider("x", "http://example.com/v1", "k", 0); err == nil {
		t.Fatal("plain http to the internet accepted")
	}
}
