package main

import (
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/keshon/gektor/internal/llm"
	"github.com/keshon/gektor/internal/prompts"
	"github.com/keshon/gektor/internal/session"
)

// Task state and spilled output live in the user's GEKTOR directory. A test
// that runs a real agent flow writes real task directories there, and the
// next run lists them as the user's own sessions. Redirect the home.
func TestMain(m *testing.M) {
	home, err := os.MkdirTemp("", "gektor-home")
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

func TestCLIForkUsesTheSubmittedTask(t *testing.T) {
	source := filepath.Join(t.TempDir(), "state.json")
	history := []llm.Message{{Role: llm.RoleSystem, Content: "sys"}, {Role: llm.RoleUser, Content: "old task"}, {Role: llm.RoleAssistant, Content: "old answer"}}
	if err := session.WriteSnapshot(source, history); err != nil {
		t.Fatal(err)
	}
	for _, task := range []string{"new task", ""} {
		t.Run(task, func(t *testing.T) {
			requests := make(chan []llm.Message, 1)
			srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				if !strings.HasSuffix(r.URL.Path, "/chat/completions") {
					http.NotFound(w, r)
					return
				}
				var body struct {
					Messages []llm.Message `json:"messages"`
				}
				if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
					t.Error(err)
					w.WriteHeader(http.StatusBadRequest)
					return
				}
				requests <- body.Messages
				fmt.Fprint(w, `{"choices":[{"message":{"role":"assistant","content":"done"},"finish_reason":"stop"}]}`)
			}))
			defer srv.Close()
			args := []string{"-fork", source, "-workspace", t.TempDir(), "-backend-kind", "openai", "-backend", srv.URL, "-model", "test", "-context-limit", "32768"}
			if task != "" {
				args = append(args, task)
			}
			launch := launchRequest{args: args}
			o, err := resolveLaunch(launch)
			if err != nil {
				t.Fatal(err)
			}
			if err := runAgentLaunch(launch, o); err != nil {
				t.Fatal(err)
			}
			got := <-requests
			want := task
			if want == "" {
				want = prompts.Resume
			}
			found := false
			for _, msg := range got[len(history):] {
				found = found || (msg.Role == llm.RoleUser && msg.Content == want)
			}
			if !found {
				t.Fatalf("fork lost continuation %q: %+v", want, got)
			}
		})
	}
}

func TestBackendContextUsesServerThenConfiguredFallback(t *testing.T) {
	for _, tt := range []struct {
		name                    string
		configured, probe, want int
	}{
		{"server-wins", 8192, 4096, 4096}, {"detected", 0, 4096, 4096}, {"probe-down", 8192, 0, 8192}, {"unknown", 0, 0, 0},
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
