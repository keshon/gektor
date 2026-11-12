package tools

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"runtime"
	"strings"
	"sync"
	"testing"
	"time"

	toolbackground "github.com/keshon/gektor/internal/tools/background"
	toolcheckurl "github.com/keshon/gektor/internal/tools/checkurl"
	"github.com/keshon/gektor/internal/workspace"
)

// longRunningCommand uses the selected platform shell.
var longRunningCommand = func() string {
	if runtime.GOOS == "windows" {
		return "Start-Sleep -Seconds 5"
	}
	return "sleep 5"
}()

func TestBackgroundStartupFailureIsNotSuccessfulToolResult(t *testing.T) {
	ws, err := workspace.New(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	defer ws.Close()
	procs := toolbackground.New()
	defer procs.StopAll()
	out, err := (toolbackground.StartShell{WS: ws, Procs: procs, SettleTime: 3 * time.Second}).Run(t.Context(), json.RawMessage(`{"command":"exit 7"}`))
	if err == nil || !strings.Contains(out, "already exited") || !strings.Contains(err.Error(), "failed") {
		t.Fatal(out, err)
	}
}

func TestBackgroundProcesses_StartCheckStop(t *testing.T) {
	dir := t.TempDir()
	ws, _ := workspace.New(dir)
	if ws != nil {
		t.Cleanup(func() { ws.Close() })
	}
	procs := toolbackground.New()

	start := toolbackground.StartShell{WS: ws, Procs: procs, SettleTime: 100 * time.Millisecond}
	args, _ := json.Marshal(map[string]string{"command": longRunningCommand})
	out, err := start.Run(context.Background(), args)
	if err != nil {
		t.Fatalf("start: %v", err)
	}
	if !strings.Contains(out, "running in background") {
		t.Fatalf("expected still running right after start, got: %q", out)
	}

	var id string
	for _, line := range strings.Split(out, "\n") {
		if strings.HasPrefix(line, "id: ") {
			id = strings.TrimPrefix(line, "id: ")
		}
	}
	if id == "" {
		t.Fatalf("could not parse id from: %q", out)
	}
	if info, _, err := procs.Inspect(id); err != nil || info.Command != longRunningCommand {
		t.Fatal("lost original shell command", info, err)
	}

	check := toolbackground.Check{Procs: procs}
	cArgs, _ := json.Marshal(map[string]any{"id": id, "cursor": 0})
	checkOut, err := check.Run(context.Background(), cArgs)
	if err != nil {
		t.Fatalf("check: %v", err)
	}
	if !strings.Contains(checkOut, "still running") {
		t.Fatalf("expected still running, got: %q", checkOut)
	}

	stop := toolbackground.Stop{Procs: procs}
	if _, err := stop.Run(context.Background(), json.RawMessage(`{"id":"`+id+`"}`)); err != nil {
		t.Fatalf("stop: %v", err)
	}

	time.Sleep(200 * time.Millisecond) // let toolbackground.Wait() observe the kill
	checkOut2, err := check.Run(context.Background(), cArgs)
	if err != nil {
		t.Fatalf("check after stop: %v", err)
	}
	if strings.Contains(checkOut2, "still running") {
		t.Fatalf("expected process to be stopped, got: %q", checkOut2)
	}
}

func TestBackgroundProcesses_CapturesOutput(t *testing.T) {
	dir := t.TempDir()
	ws, _ := workspace.New(dir)
	if ws != nil {
		t.Cleanup(func() { ws.Close() })
	}
	procs := toolbackground.New()

	start := toolbackground.StartShell{WS: ws, Procs: procs, SettleTime: 300 * time.Millisecond}
	args, _ := json.Marshal(map[string]string{"command": "echo hello-from-bg"})
	out, err := start.Run(context.Background(), args)
	if err != nil {
		t.Fatalf("start: %v", err)
	}
	deadline := time.Now().Add(3 * time.Second)
	for !strings.Contains(out, "hello-from-bg") && time.Now().Before(deadline) {
		time.Sleep(20 * time.Millisecond)
		out, err = (toolbackground.Check{Procs: procs}).Run(context.Background(), json.RawMessage(`{"id":"bg1","cursor":0}`))
		if err != nil {
			t.Fatal(err)
		}
	}
	if !strings.Contains(out, "hello-from-bg") {
		t.Fatalf("expected captured output, got: %q", out)
	}
	var cursor int
	for _, line := range strings.Split(out, "\n") {
		if strings.HasPrefix(line, "next_cursor:") {
			fmt.Sscanf(line, "next_cursor: %d", &cursor)
		}
	}
	args, _ = json.Marshal(map[string]any{"id": "bg1", "cursor": cursor})
	out, err = (toolbackground.Check{Procs: procs}).Run(t.Context(), args)
	if err != nil || cursor == 0 || strings.Contains(out, "hello-from-bg") {
		t.Fatalf("cursor repeated the startup output: %v %s", err, out)
	}
}

func TestCheckBackground_UnknownID(t *testing.T) {
	check := toolbackground.Check{Procs: toolbackground.New()}
	args, _ := json.Marshal(map[string]any{"id": "bg999", "cursor": 0})
	if _, err := check.Run(context.Background(), args); err == nil {
		t.Fatal("expected error for unknown id")
	}
}

func TestCheckURL_Success(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte("ok"))
	}))
	defer srv.Close()

	tool := toolcheckurl.Tool{}
	args, _ := json.Marshal(map[string]string{"url": srv.URL})
	out, err := tool.Run(context.Background(), args)
	if err != nil {
		t.Fatalf("Run: %v", err)
	}
	if !strings.Contains(out, "HTTP\n") || !strings.Contains(out, "status: 200") {
		t.Fatalf("expected structured HTTP header with 200, got: %q", out)
	}
	if !strings.Contains(out, "body-prefix:\nok") {
		t.Fatalf("expected body-prefix with response body, got: %q", out)
	}
}

func TestCheckURL_ConnectionRefused(t *testing.T) {
	tool := toolcheckurl.Tool{}
	// Port 1 is reserved and essentially guaranteed nothing is listening.
	args, _ := json.Marshal(map[string]string{"url": "http://127.0.0.1:1"})
	out, err := tool.Run(context.Background(), args)
	if err != nil {
		t.Fatalf("Run returned a Go error, want a normal diagnostic result: %v", err)
	}
	if !strings.Contains(out, "error:") {
		t.Fatalf("expected a connection failure message, got: %q", out)
	}
}

// Health probes must not leak credentials: neither check_url nor webfetch
// sends Authorization, Cookie, or Proxy-Authorization, so a malicious
// redirect target or a compromised page cannot harvest the operator's
// tokens. The child-process env is scrubbed separately (see scrubEnv);
// this is the wire half of the same rule.
func TestHealthProbes_SendNoCredentials(t *testing.T) {
	var mu sync.Mutex
	var seen http.Header
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		mu.Lock()
		seen = r.Header.Clone()
		mu.Unlock()
		w.Header().Set("Content-Type", "text/html")
		_, _ = w.Write([]byte("<p>hi</p>"))
	}))
	defer srv.Close()

	tool := toolcheckurl.Tool{}
	args, _ := json.Marshal(map[string]string{"url": srv.URL})
	if _, err := tool.Run(context.Background(), args); err != nil {
		t.Fatalf("check_url: %v", err)
	}
	webfetchTransport = roundTripFunc(func(r *http.Request) (*http.Response, error) {
		mu.Lock()
		seen = r.Header.Clone()
		mu.Unlock()
		return cannedResponse(r, "text/html", "<p>hi</p>")
	})
	defer func() { webfetchTransport = nil }()
	if _, err := wfRun("https://example.com/"); err != nil {
		t.Fatalf("webfetch: %v", err)
	}
	mu.Lock()
	defer mu.Unlock()
	for _, h := range []string{"Authorization", "Cookie", "Proxy-Authorization"} {
		if v := seen.Get(h); v != "" {
			t.Errorf("health probe sent %s: %q", h, v)
		}
	}
}

func TestBackgroundProcesses_StopKillsWholeProcessTree(t *testing.T) {
	// The tracked native wrapper starts a separate HTTP child. Verify the child
	// really serves before stopping its parent, and refuses connections after.
	// This exercises descendant cleanup on Windows and Unix without shell syntax.
	testManagedNativeHTTPServerLifecycle(t, []string{"--spawn-http-child"})
}
