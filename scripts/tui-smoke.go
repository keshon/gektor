//go:build ignore

// Run with go run scripts/tui-smoke.go -binary <built gektor executable>.
// All user data and workspaces are temporary; the backend is local and fake.
package main

import (
	"encoding/json"
	"flag"
	"fmt"
	"net"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"sync/atomic"
)

func main() {
	binary := flag.String("binary", "bin/gektor.exe", "built GEKTOR executable")
	keep := flag.Bool("keep", false, "keep temporary fixtures after closing")
	flag.Parse()
	if err := smoke(*binary, *keep); err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
}

func smoke(binary string, keep bool) error {
	binary, err := filepath.Abs(binary)
	if err != nil {
		return err
	}
	root, err := os.MkdirTemp("", "gektor-tui-smoke-")
	if err != nil {
		return err
	}
	if !keep {
		defer os.RemoveAll(root)
	}
	write := func(path string, value any) error {
		data, err := json.MarshalIndent(value, "", "  ")
		if err != nil {
			return err
		}
		if err = os.MkdirAll(filepath.Dir(path), 0700); err != nil {
			return err
		}
		return os.WriteFile(path, data, 0600)
	}
	a, b := filepath.Join(root, "workspace-A"), filepath.Join(root, "workspace-B")
	for _, dir := range []string{a, b} {
		if err := os.MkdirAll(dir, 0700); err != nil {
			return err
		}
	}
	for _, fixture := range []struct{ id, title, workspace string }{
		{"a", "Saved in workspace A", a}, {"b", "Saved in workspace B", b},
		{"legacy", "Unassigned session", ""}, {"missing", "Missing workspace", filepath.Join(root, "missing")},
	} {
		path := filepath.Join(root, ".gektor", "sessions", fixture.id, "state.json")
		if err := write(path, map[string]any{"version": 1, "workspace": fixture.workspace, "history": []map[string]string{
			{"role": "system", "content": "Smoke fixture."}, {"role": "user", "content": fixture.title}, {"role": "assistant", "content": "Saved reply."},
		}}); err != nil {
			return err
		}
	}
	listener, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		return err
	}
	var requests atomic.Int64
	server := &http.Server{Handler: http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		if r.Method == http.MethodGet && strings.HasSuffix(r.URL.Path, "/models") {
			json.NewEncoder(w).Encode(map[string]any{"data": []map[string]string{{"id": "smoke"}}})
			return
		}
		if r.Method != http.MethodPost || !strings.HasSuffix(r.URL.Path, "/chat/completions") {
			http.NotFound(w, r)
			return
		}
		requests.Add(1)
		var body struct {
			Stream bool `json:"stream"`
		}
		if err := json.NewDecoder(http.MaxBytesReader(w, r.Body, 1<<20)).Decode(&body); err != nil {
			http.Error(w, err.Error(), 400)
			return
		}
		if body.Stream {
			w.Header().Set("Content-Type", "text/event-stream")
			fmt.Fprint(w, "data: {\"choices\":[{\"delta\":{\"role\":\"assistant\",\"content\":\"Local smoke reply.\"},\"finish_reason\":null}]}\n\ndata: {\"choices\":[{\"delta\":{},\"finish_reason\":\"stop\"}]}\n\ndata: [DONE]\n\n")
			return
		}
		json.NewEncoder(w).Encode(map[string]any{"choices": []any{map[string]any{"message": map[string]string{"role": "assistant", "content": "Local smoke reply."}, "finish_reason": "stop"}}, "usage": map[string]int{"prompt_tokens": 10, "completion_tokens": 5}})
	})}
	go server.Serve(listener)
	defer server.Close()
	url := "http://" + listener.Addr().String() + "/v1"
	if err := write(filepath.Join(root, ".gektor", "application.json"), map[string]any{"version": 1, "workspace": b, "mode": "act", "profile": map[string]any{
		"version": 1, "provider": map[string]string{"kind": "openai", "url": url, "model": "smoke"}, "generation": map[string]any{"max_tokens": 512, "context_limit": 32768},
	}}); err != nil {
		return err
	}
	fmt.Printf("Temporary data: %s\nOpen /sessions; fixtures cover two workspaces, legacy and missing roots.\n", root)
	cmd := exec.Command(binary, "-tui", "-api-key", "smoke")
	cmd.Env = os.Environ()
	for _, key := range []string{"HOME", "USERPROFILE"} {
		prefix := key + "="
		for i := len(cmd.Env) - 1; i >= 0; i-- {
			if strings.HasPrefix(strings.ToUpper(cmd.Env[i]), prefix) {
				cmd.Env = append(cmd.Env[:i], cmd.Env[i+1:]...)
			}
		}
		cmd.Env = append(cmd.Env, prefix+root)
	}
	cmd.Stdin, cmd.Stdout, cmd.Stderr = os.Stdin, os.Stdout, os.Stderr
	err = cmd.Run()
	fmt.Printf("\nMock model requests: %d\n", requests.Load())
	return err
}
