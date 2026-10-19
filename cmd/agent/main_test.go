package main

import (
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"testing"

	"github.com/keshon/tars/internal/llm"
)

// Task state and spilled output live in the user's TARS directory. A test
// that runs a real agent flow writes real task directories there, and the
// next run lists them as the user's own sessions. Redirect the home.
func TestMain(m *testing.M) {
	home, err := os.MkdirTemp("", "tars-home")
	if err != nil {
		panic(err)
	}
	// os.UserHomeDir reads USERPROFILE on Windows and HOME elsewhere.
	os.Setenv("USERPROFILE", home)
	os.Setenv("HOME", home)
	code := m.Run()
	os.RemoveAll(home)
	os.Exit(code)
}

func TestBackendContextHonorsConfiguredLimitAndProbeFallback(t *testing.T) {
	for _, tt := range []struct {
		name                    string
		configured, probe, want int
	}{
		{"override", 8192, 4096, 8192}, {"detected", 0, 4096, 4096}, {"probe-down", 8192, 0, 8192}, {"unknown", 0, 0, 0},
	} {
		t.Run(tt.name, func(t *testing.T) {
			srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				if r.URL.Path != "/props" || tt.probe == 0 {
					http.NotFound(w, r)
					return
				}
				fmt.Fprintf(w, `{"default_generation_settings":{"n_ctx":%d}}`, tt.probe)
			}))
			defer srv.Close()
			o := resolvedLaunch{launchOptions: launchOptions{backendKind: "llama", backendURL: srv.URL, contextLimit: tt.configured}}
			got, err := backendContext(t.Context(), llm.NewLlamaClient(srv.URL, "local"), o)
			if err != nil || got != tt.want {
				t.Fatalf("limit=%d err=%v want=%d", got, err, tt.want)
			}
		})
	}
}
