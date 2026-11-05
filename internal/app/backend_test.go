package app_test

import (
	"fmt"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/keshon/gektor/internal/app"
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
