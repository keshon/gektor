package config

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestDecodeRejectsExtraDocumentsAndOversizedInput(t *testing.T) {
	profile := validProfile()
	data, err := json.Marshal(profile)
	if err != nil {
		t.Fatal(err)
	}

	for name, body := range map[string]string{
		"second document":        string(data) + `{}`,
		"trailing garbage":       string(data) + `invalid`,
		"oversized valid prefix": string(data) + strings.Repeat(" ", 1<<20),
	} {
		t.Run(name, func(t *testing.T) {
			reader := strings.NewReader(body)
			_, decodeErr := Decode(reader)
			if decodeErr == nil {
				t.Fatal("accepted invalid profile input")
			}
		})
	}
}

func TestBundledProfilesLoad(t *testing.T) {
	dir := filepath.Join("..", "..", "profiles")
	entries, err := os.ReadDir(dir)
	if err != nil {
		t.Fatalf("bundled profiles unreadable: %v", err)
	}

	loaded := 0
	for _, entry := range entries {
		if entry.IsDir() || !strings.HasSuffix(entry.Name(), ".json") {
			continue
		}
		path := filepath.Join(dir, entry.Name())
		_, err = Load(path)
		if err != nil {
			t.Errorf("%s does not load: %v", entry.Name(), err)
		}
		loaded++
	}

	if loaded == 0 {
		t.Fatal("no bundled profiles found; the check passed vacuously")
	}
}

func TestMisspelledFieldIsRejected(t *testing.T) {
	body := `{
      "version": 1,
      "provider": {"kind":"llama","url":"http://127.0.0.1:8080","model":"local"},
      "generation": {"max_tokens":256,"context_limit":4096},
      "identty": {"user_name":"Alex"}
    }`
	path := filepath.Join(t.TempDir(), "typo.json")
	err := os.WriteFile(path, []byte(body), 0o600)
	if err != nil {
		t.Fatal(err)
	}

	_, err = Load(path)
	if err == nil {
		t.Fatal("a misspelled field was accepted")
	}

	if !strings.Contains(err.Error(), "identty") {
		t.Fatalf("error does not name the offending key: %v", err)
	}
}

func TestRemovedKeysAreRejectedByName(t *testing.T) {
	for key, body := range map[string]string{
		"conversation": `{"version":1,"provider":{"kind":"llama","url":"http://127.0.0.1:8080","model":"local"},"generation":{"max_tokens":256,"context_limit":4096},"conversation":{"user_name":"Alex"}}`,
		"speech":       `{"version":1,"provider":{"kind":"llama","url":"http://127.0.0.1:8080","model":"local"},"generation":{"max_tokens":256,"context_limit":4096},"speech":{"max_seconds":120}}`,
	} {
		t.Run(key, func(t *testing.T) {
			path := filepath.Join(t.TempDir(), "old.json")
			err := os.WriteFile(path, []byte(body), 0o600)
			if err != nil {
				t.Fatal(err)
			}
			_, err = Load(path)
			if err == nil {
				t.Fatalf("a removed %q block was accepted", key)
			}
			if !strings.Contains(err.Error(), key) {
				t.Fatalf("error does not name %q: %v", key, err)
			}
		})
	}
}

func TestIdentityRoundTrips(t *testing.T) {
	p := validProfile()
	p.Identity = &Identity{UserName: "Alex", UserPersona: "a colleague", SystemPrompt: "be terse", PostHistory: "cite sources", Name: "Ada", PromptPreset: "coding"}
	path := filepath.Join(t.TempDir(), "profile.json")
	err := SaveCopy(p, path)
	if err != nil {
		t.Fatal(err)
	}

	loaded, err := Load(path)
	if err != nil {
		t.Fatal(err)
	}

	if loaded.Identity == nil || *loaded.Identity != *p.Identity {
		t.Fatalf("identity did not round-trip: %+v", loaded.Identity)
	}
}
