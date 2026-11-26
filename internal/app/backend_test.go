package app_test

import (
	"fmt"
	"net/http"
	"net/http/httptest"
	"runtime"
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

func TestBearerProvidersDiscoverAndStreamWithRouteCredentials(t *testing.T) {
	homeVariable := "HOME"
	if runtime.GOOS == "windows" {
		homeVariable = "USERPROFILE"
	}
	t.Setenv(homeVariable, t.TempDir())
	t.Setenv("OPENROUTER_API_KEY", "default-hosted-key")
	t.Setenv("GEKTOR_PRESET_TEST_KEY", "explicit-gateway-key")

	for _, route := range []struct {
		kind, keyEnv, authorization string
		saved                       bool
	}{
		{kind: "openrouter", keyEnv: "GEKTOR_PRESET_TEST_KEY", authorization: "Bearer explicit-gateway-key"},
		{kind: "openrouter"},
		{kind: "lmstudio"},
		{kind: "llama", saved: true, authorization: "Bearer saved-fixture-key"},
		{kind: "kobold", saved: true, authorization: "Bearer saved-fixture-key"},
		{kind: "openai", saved: true, authorization: "Bearer saved-fixture-key"},
		{kind: "lmstudio", saved: true, authorization: "Bearer saved-fixture-key"},
		{kind: "openrouter", saved: true, authorization: "Bearer saved-fixture-key"},
	} {
		t.Run(route.kind+"/"+route.keyEnv, func(t *testing.T) {
			srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				if got := r.Header.Get("Authorization"); got != route.authorization {
					t.Error("unexpected authorization for", r.URL.Path)
				}
				switch r.URL.Path {
				case "/v1/models":
					fmt.Fprint(w, `{"data":[{"id":"fixture-model"}]}`)
				case "/api/v1/model":
					fmt.Fprint(w, `{"result":"fixture-model"}`)
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

			options := app.BackendOptions{Kind: route.kind, URL: srv.URL, APIKeyEnv: route.keyEnv}
			if route.saved {
				err := app.SaveProviderKey(route.kind, options.URL, "saved-fixture-key")
				if err != nil {
					t.Fatal(err)
				}
				options.KeyMode = providers.KeySaved
			}
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

func TestCatalogContextWindowAndWorkingCap(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/api/v1/models" {
			http.NotFound(w, r)
			return
		}
		fmt.Fprint(w, `{"data":[{"id":"big","context_length":1048576},{"id":"small","context_length":32768},{"id":"unknown"}]}`)
	}))
	defer server.Close()

	for _, tc := range []struct {
		model               string
		cap, window, budget int
		estimated           bool
	}{
		{model: "big", window: 1048576, budget: 1048576},
		{model: "big", cap: 65536, window: 1048576, budget: 65536},
		{model: "small", cap: 65536, window: 32768, budget: 32768},
		{model: "unknown", budget: 128000, estimated: true},
		{model: "unknown", cap: 65536, budget: 65536},
	} {
		options := app.BackendOptions{Kind: "openrouter", URL: server.URL + "/api/v1", Model: tc.model}
		backend, err := app.NewBackend(t.Context(), options)
		if err != nil {
			t.Fatal(err)
		}

		facts, err := backend.Inspect(t.Context(), options.URL, tc.cap, false)
		if err != nil {
			t.Fatal(err)
		}
		if facts.ContextWindow != tc.window || facts.ContextLimit != tc.budget || facts.ContextEstimated != tc.estimated {
			t.Fatalf("%s cap %d: unexpected context facts %+v", tc.model, tc.cap, facts)
		}
	}
}
