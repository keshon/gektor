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
	procs := NewBackgroundProcesses()
	defer procs.StopAll()
	out, err := (StartBackground{WS: ws, Procs: procs, SettleTime: 3 * time.Second}).Run(t.Context(), json.RawMessage(`{"command":"exit 7"}`))
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
	procs := NewBackgroundProcesses()

	start := StartBackground{WS: ws, Procs: procs, SettleTime: 100 * time.Millisecond}
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

	check := CheckBackground{Procs: procs}
	cArgs, _ := json.Marshal(map[string]any{"id": id, "cursor": 0})
	checkOut, err := check.Run(context.Background(), cArgs)
	if err != nil {
		t.Fatalf("check: %v", err)
	}
	if !strings.Contains(checkOut, "still running") {
		t.Fatalf("expected still running, got: %q", checkOut)
	}

	stop := StopBackground{Procs: procs}
	if _, err := stop.Run(context.Background(), json.RawMessage(`{"id":"`+id+`"}`)); err != nil {
		t.Fatalf("stop: %v", err)
	}

	time.Sleep(200 * time.Millisecond) // let Wait() observe the kill
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
	procs := NewBackgroundProcesses()

	start := StartBackground{WS: ws, Procs: procs, SettleTime: 300 * time.Millisecond}
	args, _ := json.Marshal(map[string]string{"command": "echo hello-from-bg"})
	out, err := start.Run(context.Background(), args)
	if err != nil {
		t.Fatalf("start: %v", err)
	}
	deadline := time.Now().Add(3 * time.Second)
	for !strings.Contains(out, "hello-from-bg") && time.Now().Before(deadline) {
		time.Sleep(20 * time.Millisecond)
		out, err = (CheckBackground{Procs: procs}).Run(context.Background(), json.RawMessage(`{"id":"bg1","cursor":0}`))
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
	out, err = (CheckBackground{Procs: procs}).Run(t.Context(), args)
	if err != nil || cursor == 0 || strings.Contains(out, "hello-from-bg") {
		t.Fatalf("cursor repeated the startup output: %v %s", err, out)
	}
}

func TestCheckBackground_UnknownID(t *testing.T) {
	check := CheckBackground{Procs: NewBackgroundProcesses()}
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

	tool := CheckURL{}
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
	tool := CheckURL{}
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

	tool := CheckURL{}
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
func TestBackgroundCursorRetainsDeltasAndReportsEviction(t *testing.T) {
	buffer := &syncBuffer{}
	buffer.Write([]byte("first\n"))
	out, cursor, err := buffer.since(0, false)
	if err != nil || out != "first\n" {
		t.Fatalf("initial output: %q %v", out, err)
	}
	buffer.Write([]byte("second\n"))
	for range 2 {
		delta, next, err := buffer.since(cursor, false)
		if err != nil || delta != "second\n" || next != len("first\nsecond\n") {
			t.Fatalf("cursor was destructive or repeated old output: %q %d %v", delta, next, err)
		}
	}
	buffer.Write([]byte(strings.Repeat("x", shellMaxBytes*3) + "last"))
	out, cursor, err = buffer.since(cursor, false)
	if err != nil || !strings.Contains(out, "output truncated") || !strings.HasSuffix(out, "last") || len(out) > shellMaxBytes+200 {
		t.Fatalf("eviction lost its diagnostic or newest output: bytes=%d %v", len(out), err)
	}
	buffer.Write([]byte("new"))
	out, next, err := buffer.since(cursor, false)
	if err != nil || out != "new" || next != cursor+3 {
		t.Fatalf("delta after eviction: %q %d %v", out, next, err)
	}
	out, _, err = buffer.since(next, false)
	if err != nil || out != "" {
		t.Fatalf("idle poll repeated output: %q %v", out, err)
	}
}

func TestBackgroundCursorPreservesSplitUTF8AcrossBufferSegments(t *testing.T) {
	buffer := &syncBuffer{}
	buffer.Write([]byte(strings.Repeat("a", shellMaxBytes/2-1)))
	_, cursor, _ := buffer.since(0, false)
	for i, b := range []byte("😀") {
		buffer.Write([]byte{b})
		out, next, err := buffer.since(cursor, false)
		want := ""
		if i == len("😀")-1 {
			want = "😀"
		}
		if err != nil || out != want {
			t.Fatalf("split character at byte %d: %q %v", i, out, err)
		}
		cursor = next
	}
	buffer.Write([]byte{0xf0})
	out, next, _ := buffer.since(cursor, false)
	if out != "" || next != cursor {
		t.Fatal("acknowledged an incomplete UTF-8 character")
	}
	out, next, _ = buffer.since(cursor, true)
	if out != "�" || next != cursor+1 {
		t.Fatal("final incomplete character was not flushed")
	}
}

func TestCheckBackgroundRequiresValidExplicitCursor(t *testing.T) {
	procs := NewBackgroundProcesses()
	procs.procs["bg1"] = &bgProc{output: &syncBuffer{}}
	for _, args := range []string{`{"id":"bg1"}`, `{"id":"bg1","cursor":-1}`, `{"id":"bg1","cursor":1}`, `{"id":"bg1","cursor":0,"offset":1}`} {
		if _, err := (CheckBackground{Procs: procs}).Run(t.Context(), json.RawMessage(args)); err == nil {
			t.Fatalf("accepted an invalid cursor contract: %s", args)
		}
	}
}
