package app

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"testing"
)

func inspectionBackend(t *testing.T, kind, base, model, key string) *Backend {
	t.Helper()
	b, err := NewBackend(t.Context(), BackendOptions{Kind: kind, URL: base, Model: model, APIKey: key})
	if err != nil {
		t.Fatal(err)
	}
	return b
}

// llama-server reports both facts from one endpoint; koboldcpp from two.
// The client asks for them separately, so neither backend may require the
// other's endpoint to exist.
func TestLlamaDialect_ReadsModelAndContextFromProps(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/props" {
			t.Errorf("unexpected path: %s", r.URL.Path)
		}
		_ = json.NewEncoder(w).Encode(map[string]any{
			"model_path":                  "/models/qwen3.gguf",
			"default_generation_settings": map[string]any{"n_ctx": 32768},
		})
	}))
	defer srv.Close()

	c := inspectionBackend(t, "llama", srv.URL, "local", "")
	if c.Backend() != "llama-server" {
		t.Errorf("Backend() = %q", c.Backend())
	}
	name, err := c.ModelName(context.Background())
	if err != nil || name != "/models/qwen3.gguf" {
		t.Fatalf("ModelName = %q, %v", name, err)
	}
	n, err := c.MaxContextLength(context.Background())
	if err != nil || n != 32768 {
		t.Fatalf("MaxContextLength = %d, %v", n, err)
	}
}

func TestSupportsVision_OpenAIReadsModelCapabilities(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/v1/models" {
			http.NotFound(w, r)
			return
		}
		if r.Header.Get("Authorization") != "Bearer secret" {
			t.Errorf("probe did not send the API key")
		}
		fmt.Fprint(w, `{"data":[
			{"id":"text-only","architecture":{"input_modalities":["text"]}},
			{"id":"sees","architecture":{"modality":"text+image->text","input_modalities":["text","image"]}}
		]}`)
	}))
	defer srv.Close()

	c := inspectionBackend(t, "openai", srv.URL+"/v1", "sees", "secret")
	if got, err := c.SupportsVision(context.Background()); err != nil || !got {
		t.Fatalf("vision model probe = %t, %v", got, err)
	}

	c = inspectionBackend(t, "openai", srv.URL+"/v1", "text-only", "secret")
	if got, err := c.SupportsVision(context.Background()); err != nil || got {
		t.Fatalf("text model probe = %t, %v", got, err)
	}
}

func TestModelDiscoveryUsesProviderInspector(t *testing.T) {
	for _, tc := range []struct{ kind, root, path, payload string }{
		{"llama", "/", "/v1/models", `{"data":[{"id":"loaded"}]}`},
		{"kobold", "/", "/api/v1/model", `{"result":"loaded"}`},
		{"openai", "/v1/", "/v1/models", `{"data":[{"id":"loaded"}]}`},
	} {
		t.Run(tc.kind, func(t *testing.T) {
			srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				if r.URL.Path != tc.path {
					t.Errorf("path=%q want %q", r.URL.Path, tc.path)
					http.NotFound(w, r)
					return
				}
				if r.Header.Get("Authorization") != "Bearer secret" {
					t.Error("discovery lost connection credentials")
				}
				fmt.Fprint(w, tc.payload)
			}))
			defer srv.Close()
			models, err := DiscoverModels(t.Context(), BackendOptions{Kind: tc.kind, URL: srv.URL + tc.root, APIKey: "secret"})
			if err != nil || len(models) != 1 || models[0] != "loaded" {
				t.Fatalf("models=%v error=%v", models, err)
			}
		})
	}
}
