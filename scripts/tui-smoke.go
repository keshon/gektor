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
	"time"
)

const smokeRecipe = `{"version":4,"name":"Smoke workflow","steps":[{"id":"transform","tool":"script_transform","node_version":1,"args":{"code":"return input + 1;","input":2}}]}`

func main() {
	binary := flag.String("binary", "bin/gektor.exe", "built GEKTOR executable")
	keep := flag.Bool("keep", false, "keep temporary fixtures after closing")
	delay := flag.Duration("delay", 350*time.Millisecond, "delay between smoke stream chunks (20 chunks)")
	flag.Parse()
	if *delay < 0 || *delay > 5*time.Second {
		fmt.Fprintln(os.Stderr, "delay must be between 0 and 5s")
		os.Exit(1)
	}
	if err := smoke(*binary, *keep, *delay); err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
}

func smoke(binary string, keep bool, delay time.Duration) error {
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
		for name, content := range map[string]string{
			"smoke-one.txt":   "First harmless tool-inspection fixture.\n",
			"smoke-two.txt":   "Second harmless tool-inspection fixture.\n",
			"smoke-allow.env": "SMOKE_VALUE=allow-fixture-only\n",
			"smoke-deny.env":  "SMOKE_VALUE=deny-fixture-only\n",
		} {
			if err := os.WriteFile(filepath.Join(dir, name), []byte(content), 0600); err != nil {
				return err
			}
		}
		if err := os.WriteFile(filepath.Join(dir, "smoke-workflow.json"), []byte(smokeRecipe), 0600); err != nil {
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
	server := &http.Server{Handler: smokeBackend(&requests, delay), ReadHeaderTimeout: 5 * time.Second}
	go server.Serve(listener)
	defer server.Close()
	url := "http://" + listener.Addr().String() + "/v1"
	if err := write(filepath.Join(root, ".gektor", "application.json"), map[string]any{"version": 1, "workspace": b, "mode": "act", "profile": map[string]any{
		"version": 1, "provider": map[string]string{"kind": "openai", "url": url, "model": "smoke"}, "generation": map[string]any{"max_tokens": 512, "context_limit": 32768},
	}}); err != nil {
		return err
	}
	fmt.Printf("Temporary data: %s\nOpen /sessions; fixtures cover two workspaces, legacy and missing roots.\n", root)
	fmt.Println("Send: smoke stream (queue/cancel), smoke tools (two read badges), smoke allow / smoke deny (fixture .env permission).")
	fmt.Println("Workflow: /workflows, Ctrl+O, smoke-workflow.json. Native review must be performed manually; this helper records no acceptance verdict.")
	cmd := exec.Command(binary, "-tui", "-api-key", "smoke")
	cmd.Dir = b
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

// smokeBackend selects a response from the latest user turn, rather than a global
// request index, so retrying or switching sessions does not consume another case.
func smokeBackend(requests *atomic.Int64, delay time.Duration) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		if r.Method == http.MethodGet && strings.HasSuffix(r.URL.Path, "/models") {
			_ = json.NewEncoder(w).Encode(map[string]any{"data": []map[string]string{{"id": "smoke"}}})
			return
		}
		if r.Method != http.MethodPost || !strings.HasSuffix(r.URL.Path, "/chat/completions") {
			http.NotFound(w, r)
			return
		}
		requests.Add(1)
		var body struct {
			Stream   bool `json:"stream"`
			Messages []struct {
				Role    string          `json:"role"`
				Content json.RawMessage `json:"content"`
			} `json:"messages"`
		}
		if err := json.NewDecoder(http.MaxBytesReader(w, r.Body, 4<<20)).Decode(&body); err != nil {
			http.Error(w, err.Error(), http.StatusBadRequest)
			return
		}
		prompt, toolResult := "", false
		for _, message := range body.Messages {
			if message.Role == "user" {
				_ = json.Unmarshal(message.Content, &prompt)
				toolResult = false
			}
			if message.Role == "tool" {
				toolResult = true
			}
		}
		prompt = strings.ToLower(strings.TrimSpace(prompt))
		message := map[string]any{"role": "assistant", "content": "Local smoke reply."}
		finish := "stop"
		var calls []map[string]any
		if !toolResult {
			paths := []string{}
			switch prompt {
			case "smoke tools":
				paths = []string{"smoke-one.txt", "smoke-two.txt"}
			case "smoke allow":
				paths = []string{"smoke-allow.env"}
			case "smoke deny":
				paths = []string{"smoke-deny.env"}
			}
			for index, path := range paths {
				args, _ := json.Marshal(map[string]string{"path": path})
				calls = append(calls, map[string]any{"id": fmt.Sprintf("smoke_%d", index), "type": "function", "function": map[string]string{"name": "read_file", "arguments": string(args)}})
			}
		}
		if len(calls) > 0 {
			message["content"] = "Inspecting local smoke fixtures."
			message["tool_calls"] = calls
			finish = "tool_calls"
		} else if toolResult {
			message["content"] = "Smoke tool results received. Inspect the individual badges for success or permission denial."
		}
		if !body.Stream {
			_ = json.NewEncoder(w).Encode(map[string]any{"choices": []any{map[string]any{"message": message, "finish_reason": finish}}, "usage": map[string]int{"prompt_tokens": 10, "completion_tokens": 5}})
			return
		}
		w.Header().Set("Content-Type", "text/event-stream")
		frame := func(delta map[string]any, reason any) {
			data, _ := json.Marshal(map[string]any{"choices": []any{map[string]any{"delta": delta, "finish_reason": reason}}})
			_, _ = fmt.Fprintf(w, "data: %s\n\n", data)
			if f, ok := w.(http.Flusher); ok {
				f.Flush()
			}
		}
		if prompt == "smoke stream" && !toolResult {
			for index := 0; index < 20; index++ {
				frame(map[string]any{"content": fmt.Sprintf("Smoke streaming chunk %02d.\n", index+1)}, nil)
				timer := time.NewTimer(delay)
				select {
				case <-r.Context().Done():
					timer.Stop()
					return
				case <-timer.C:
				}
			}
		} else {
			delta := map[string]any{"role": "assistant", "content": message["content"]}
			if len(calls) > 0 {
				for index, call := range calls {
					call["index"] = index
				}
				delta["tool_calls"] = calls
			}
			frame(delta, nil)
		}
		frame(map[string]any{}, finish)
		_, _ = fmt.Fprint(w, "data: [DONE]\n\n")
	})
}
