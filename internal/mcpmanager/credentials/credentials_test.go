package credentials

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"

	shared "github.com/keshon/gektor/internal/credentials"
)

func TestLegacyPayloadAndOriginHeaderBindingSurviveStorageExtraction(t *testing.T) {
	dir := t.TempDir()
	storage := shared.Store{Directory: dir}
	raw, err := json.Marshal(entry{Origin: "https://example.com", Header: "Authorization", Value: "legacy-private-value"})
	if err != nil {
		t.Fatal(err)
	}
	generated, err := storage.Put(raw)
	if err != nil {
		t.Fatal(err)
	}
	legacyID := strings.Repeat("a", 32)
	err = os.Rename(filepath.Join(dir, generated), filepath.Join(dir, legacyID))
	if err != nil {
		t.Fatal(err)
	}
	store := Store{Directory: dir}
	value, err := store.Resolve(legacyID, "https://EXAMPLE.com:443/path", "authorization")
	if err != nil || value != "legacy-private-value" {
		t.Fatal("legacy binding lost", err)
	}
	for _, pair := range [][2]string{{"https://other.example", "Authorization"}, {"https://example.com", "X-API-Key"}, {"http://example.com", "Authorization"}} {
		_, err = store.Resolve(legacyID, pair[0], pair[1])
		if err == nil {
			t.Fatal("MCP credential binding bypassed")
		}
	}
}
