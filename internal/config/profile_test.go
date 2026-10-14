package config

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestLoadProfile(t *testing.T) {
	valid := `{"version":1,"provider":{"kind":"llama","url":"http://127.0.0.1:8080","model":"local","ownership":"external"},"generation":{"max_tokens":1024,"context_limit":4096}}`
	for _, tc := range []struct {
		name, text string
		fail       bool
	}{
		{"valid", valid, false},
		{"unknown", strings.Replace(valid, `"version":1`, `"version":1,"speech":{}`, 1), true},
		{"managed", strings.Replace(valid, "external", "managed", 1), true},
		{"budget", strings.Replace(valid, "4096", "1024", 1), true},
		{"credentials", strings.Replace(valid, "127.0.0.1", "user:secret@127.0.0.1", 1), true},
		{"extra", valid + ` {}`, true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			path := filepath.Join(t.TempDir(), "profile.json")
			if err := os.WriteFile(path, []byte(tc.text), 0600); err != nil {
				t.Fatal(err)
			}
			_, err := Load(path)
			if (err != nil) != tc.fail {
				t.Fatalf("error=%v", err)
			}
		})
	}
}

func TestCharacterPathIsProfileRelative(t *testing.T) {
	path := filepath.Join(t.TempDir(), "persona.json")
	text := `{"version":1,"provider":{"kind":"llama","url":"http://127.0.0.1:8080","model":"local","ownership":"external"},"generation":{"max_tokens":100,"context_limit":1000,"temperature":0.7},"conversation":{"character":"cards/ada.png"}}`
	if err := os.WriteFile(path, []byte(text), 0600); err != nil {
		t.Fatal(err)
	}
	p, err := Load(path)
	if err != nil {
		t.Fatal(err)
	}
	if p.Conversation.Character != filepath.Join(filepath.Dir(path), "cards", "ada.png") || p.Generation.Temperature != .7 {
		t.Fatalf("wrong resolved profile: %+v", p)
	}
}

func TestManagedThinkingPathsAndOwnership(t *testing.T) {
	root := t.TempDir()
	path := filepath.Join(root, "local.json")
	text := `{"version":1,"provider":{"kind":"llama","url":"http://127.0.0.1:19080","model":"local","ownership":"managed","runtime":"../runtime/llama-server.exe","model_file":"models/vendor/name/model.gguf","model_root":"models","gpu_layers":99},"generation":{"max_tokens":100,"context_limit":4096}}`
	os.WriteFile(path, []byte(text), 0600)
	p, err := Load(path)
	if err != nil {
		t.Fatal(err)
	}
	if p.Provider.ModelFile != filepath.Join(root, "models", "vendor", "name", "model.gguf") || !filepath.IsAbs(p.Provider.Runtime) {
		t.Fatal("paths did not resolve")
	}
	p.Provider.URL = "http://example.com:8080"
	if p.Validate() == nil {
		t.Fatal("remote managed binding accepted")
	}
	p.Provider.URL = "http://127.0.0.1:19080"
	p.Provider.Kind = "kobold"
	if p.Validate() == nil {
		t.Fatal("unsupported managed runtime accepted")
	}
}
