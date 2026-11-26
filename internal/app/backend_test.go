package app_test

import (
	"fmt"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/keshon/gektor/internal/app"
	"github.com/keshon/gektor/internal/llm"
	"github.com/keshon/gektor/internal/providers"
	"github.com/keshon/gektor/internal/roles"
)

func TestCompositionAndProbeAreReplacedTogether(t *testing.T) {
	makeBackend := func(model string, window int) *app.Backend {
		srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			if r.URL.Path == "/props" {
				fmt.Fprintf(w, `{"model_path":%q,"default_generation_settings":{"n_ctx":%d}}`, model, window)
				return
			}
			http.NotFound(w, r)
		}))
		t.Cleanup(srv.Close)
		b, err := app.NewBackend(t.Context(), app.BackendOptions{Kind: "llama", URL: srv.URL, Model: "local"})
		if err != nil {
			t.Fatal(err)
		}
		return b
	}
	first := makeBackend("first", 4096)
	second := makeBackend("second", 8192)
	old := roles.Env{Client: first, RefreshModelFacts: first.RefreshModelFacts, Model: "stale", ContextLimit: 1024, Vision: true}
	a := old.Refreshed(t.Context())
	if a.Model != "first" || a.ContextLimit != 4096 || old.Model != "stale" {
		t.Fatal("snapshot mutated or missing", a.Model, a.ContextLimit)
	}
	next := old
	next.Client = second
	next.RefreshModelFacts = second.RefreshModelFacts
	b := next.Refreshed(t.Context())
	if b.Model != "second" || b.ContextLimit != 8192 || a.Model != "first" {
		t.Fatal("old probe survived replacement", b.Model, b.ContextLimit)
	}
}

func TestCatalogEndpointsAndConstructionAgree(t *testing.T) {
	for _, kind := range providers.Kinds() {
		d, err := providers.Lookup(kind)
		if err != nil {
			t.Fatal(err)
		}
		if err = d.ValidateEndpoint(d.DefaultURL); err != nil {
			t.Fatal(err)
		}
		if d.FixedURL != "" {
			if _, err = app.NewBackend(t.Context(), app.BackendOptions{Kind: kind, URL: "https://other.example/v1", Model: "test"}); err == nil {
				t.Fatal("OAuth endpoint override accepted")
			}
		} else {
			if _, err = app.NewBackend(t.Context(), app.BackendOptions{Kind: kind, URL: d.DefaultURL, Model: "test"}); err != nil {
				t.Fatal(err)
			}
		}
	}
}

func TestDiscoveryUsesInferenceConnectionHeaders(t *testing.T) {
	t.Setenv("GEKTOR_SITE_TITLE", "test-app")
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Header.Get("Authorization") != "Bearer test-key" || r.Header.Get("X-Title") != "test-app" {
			t.Error("missing connection headers", r.URL.Path)
			http.Error(w, "missing", http.StatusUnauthorized)
			return
		}
		switch r.URL.Path {
		case "/props":
			fmt.Fprint(w, `{"model_path":"model","default_generation_settings":{"n_ctx":4096}}`)
		case "/v1/models":
			fmt.Fprint(w, `{"data":[{"id":"model"}]}`)
		default:
			http.NotFound(w, r)
		}
	}))
	defer srv.Close()
	options := app.BackendOptions{Kind: "llama", URL: srv.URL, Model: "model", APIKey: "test-key"}
	b, err := app.NewBackend(t.Context(), options)
	if err != nil {
		t.Fatal(err)
	}
	facts := b.RefreshModelFacts(t.Context())
	if !facts.ContextKnown || !facts.ModelKnown {
		t.Fatal(facts)
	}
	models, err := app.DiscoverModels(t.Context(), options)
	if err != nil || len(models) != 1 {
		t.Fatal(models, err)
	}
}

func TestStrictInspectionRejectsFallbackWithoutBackendEvidence(t *testing.T) {
	srv := httptest.NewServer(http.NotFoundHandler())
	defer srv.Close()
	b, err := app.NewBackend(t.Context(), app.BackendOptions{Kind: "llama", URL: srv.URL, Model: "local"})
	if err != nil {
		t.Fatal(err)
	}
	if _, err = b.Inspect(t.Context(), srv.URL, 8192, true); err == nil {
		t.Fatal("configured budget mistaken for backend evidence")
	}
	f, err := b.Inspect(t.Context(), srv.URL, 8192, false)
	if err != nil || f.ContextLimit != 8192 {
		t.Fatal("interactive fallback lost", f, err)
	}
}

func TestProviderPresetsDiscoverAndStreamWithRouteCredentials(t *testing.T) {
	t.Setenv("OPENROUTER_API_KEY", "default-hosted-key")
	t.Setenv("GEKTOR_PRESET_TEST_KEY", "explicit-gateway-key")

	for _, route := range []struct {
		kind, keyEnv, authorization string
	}{
		{kind: "openrouter", keyEnv: "GEKTOR_PRESET_TEST_KEY", authorization: "Bearer explicit-gateway-key"},
		{kind: "openrouter"},
		{kind: "lmstudio"},
	} {
		t.Run(route.kind+"/"+route.keyEnv, func(t *testing.T) {
			srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				if got := r.Header.Get("Authorization"); got != route.authorization {
					t.Error("unexpected authorization for", r.URL.Path)
				}
				switch r.URL.Path {
				case "/v1/models":
					fmt.Fprint(w, `{"data":[{"id":"fixture-model"}]}`)
				case "/v1/chat/completions":
					w.Header().Set("Content-Type", "text/event-stream")
					fmt.Fprintln(w, `data: {"choices":[{"delta":{"tool_calls":[{"index":0,"id":"call_1","type":"function","function":{"name":"read_file","arguments":"{}"}}]}}]}`)
					fmt.Fprintln(w)
					fmt.Fprintln(w, `data: {"choices":[{"delta":{},"finish_reason":"tool_calls"}]}`)
					fmt.Fprintln(w)
					fmt.Fprint(w, "data: [DONE]\n\n")
				default:
					http.NotFound(w, r)
				}
			}))
			defer srv.Close()

			options := app.BackendOptions{Kind: route.kind, URL: srv.URL + "/v1", APIKeyEnv: route.keyEnv}
			models, err := app.DiscoverModels(t.Context(), options)
			if err != nil || len(models) != 1 || models[0] != "fixture-model" {
				t.Fatal(models, err)
			}
			options.Model = models[0]
			backend, err := app.NewBackend(t.Context(), options)
			if err != nil {
				t.Fatal(err)
			}

			request := llm.ChatRequest{Messages: []llm.Message{{Role: llm.RoleUser, Content: "Read the file."}}}
			response, err := backend.Stream(t.Context(), request, nil, nil)
			if err != nil {
				t.Fatal(err)
			}
			calls := response.Message.ToolCalls
			if len(calls) != 1 || calls[0].Name != "read_file" {
				t.Fatal(calls)
			}
		})
	}
}
