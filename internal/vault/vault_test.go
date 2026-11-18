package vault

import (
	"bytes"
	"os"
	"path/filepath"
	"testing"
)

func TestSealOpen(t *testing.T) {
	v := Random()
	sealed, err := v.Seal([]byte("pk_123"), "clickup")
	if err != nil || bytes.Contains(sealed, []byte("pk_123")) {
		t.Fatalf("seal: %v", err)
	}
	if plain, err := v.Open(sealed, "clickup"); err != nil || string(plain) != "pk_123" {
		t.Fatalf("open: %q %v", plain, err)
	}
	if _, err := v.Open(sealed, "discord"); err != ErrSealed {
		t.Fatalf("other purpose: %v", err)
	}
	if _, err := Random().Open(sealed, "clickup"); err != ErrSealed {
		t.Fatalf("other key: %v", err)
	}
	path := filepath.Join(t.TempDir(), "vault.key")
	os.WriteFile(path, []byte("00112233445566778899aabbccddeeff00112233445566778899aabbccddeeff\n"), 0o600)
	if _, err := Open(path); err != nil {
		t.Fatal(err)
	}
}

func TestCreateWritesAKeyOnce(t *testing.T) {
	path := filepath.Join(t.TempDir(), "sub", "vault.key")
	if err := Create(path); err != nil {
		t.Fatal(err)
	}
	if _, err := Open(path); err != nil {
		t.Fatal(err)
	}
	if err := Create(path); err == nil {
		t.Fatal("Create overwrote an existing key")
	}
}
