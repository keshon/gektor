package credentials

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"

	shared "github.com/keshon/gektor/internal/credentials"
)

func TestStoredPayloadPreservesOriginAndHeaderBinding(t *testing.T) {
	dir := t.TempDir()
	storage := shared.Store{Directory: dir}
	raw, err := json.Marshal(entry{Origin: "https://example.com", Header: "Authorization", Value: "stored-private-value"})
	if err != nil {
		t.Fatal(err)
	}
	generated, err := storage.Put(raw)
	if err != nil {
		t.Fatal(err)
	}
	storedID := strings.Repeat("a", 32)
	err = os.Rename(filepath.Join(dir, generated), filepath.Join(dir, storedID))
	if err != nil {
		t.Fatal(err)
	}
	store := Store{Directory: dir}
	value, err := store.Resolve(storedID, "https://EXAMPLE.com:443/path", "authorization")
	if err != nil || value != "stored-private-value" {
		t.Fatal("stored binding lost", err)
	}
	for _, pair := range [][2]string{{"https://other.example", "Authorization"}, {"https://example.com", "X-API-Key"}, {"http://example.com", "Authorization"}} {
		_, err = store.Resolve(storedID, pair[0], pair[1])
		if err == nil {
			t.Fatal("MCP credential binding bypassed")
		}
	}
}

func TestRevokedMCPSecretCannotResolve(t *testing.T) {
	store := Store{Directory: t.TempDir()}
	id, err := store.Put("https://api.example.com", "Authorization", "private-token")
	if err != nil {
		t.Fatal(err)
	}
	err = store.Revoke(id)
	if err != nil {
		t.Fatal(err)
	}
	_, err = store.Resolve(id, "https://api.example.com", "Authorization")
	if err == nil || strings.Contains(err.Error(), "private-token") {
		t.Fatal("revoked MCP credential resolved or leaked", err)
	}
}
