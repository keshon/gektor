package tools

import (
	"bufio"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"os/exec"
	"strings"
	"sync"
	"testing"
	"time"
)

type observedMCPWriter struct {
	*io.PipeWriter
	started chan struct{}
	once    sync.Once
}

func (w *observedMCPWriter) Write(p []byte) (int, error) {
	w.once.Do(func() { close(w.started) })
	return w.PipeWriter.Write(p)
}

func TestMCPBlockedWriteCancellation(t *testing.T) {
	input, writer := io.Pipe()
	output, server := io.Pipe()
	defer input.Close()
	defer server.Close()
	observed := &observedMCPWriter{PipeWriter: writer, started: make(chan struct{})}
	c := &MCPClient{stdin: bufio.NewWriter(observed), stdout: bufio.NewReader(output), input: observed, output: output,
		writeGate: make(chan struct{}, 1), restartGate: make(chan struct{}, 1), pending: map[int]chan json.RawMessage{}}
	defer c.Close()
	readDone := make(chan struct{})
	go func() { c.readLoop(c.stdout); close(readDone) }()
	ctx, cancel := context.WithCancel(t.Context())
	defer cancel()
	done := make(chan error, 1)
	go func() { done <- c.call(ctx, "tools/call", map[string]any{"text": strings.Repeat("x", 8192)}, nil) }()
	select {
	case <-observed.started:
	case <-time.After(3 * time.Second):
		t.Fatal("write did not start")
	}
	// A caller waiting for the write slot must also be cancellable.
	queued, cancelQueued := context.WithCancel(t.Context())
	cancelQueued()
	if err := c.send(queued, rpcRequest{ID: 2}); !errors.Is(err, context.Canceled) {
		t.Fatalf("queued write: %v", err)
	}
	cancel()
	select {
	case err := <-done:
		if !errors.Is(err, context.Canceled) {
			t.Fatalf("cancelled call: %v", err)
		}
	case <-time.After(3 * time.Second):
		t.Fatal("write ignored cancellation")
	}
	select {
	case <-readDone:
	case <-time.After(3 * time.Second):
		t.Fatal("reader survived cancelled transport")
	}
}

func TestMCPReadEOFReconnectsWithWritableInput(t *testing.T) {
	previous := mcpRestartBackoff
	mcpRestartBackoff = 0
	defer func() { mcpRestartBackoff = previous }()
	var oldWrites strings.Builder
	c := &MCPClient{stdin: bufio.NewWriter(&oldWrites), stdout: bufio.NewReader(strings.NewReader("")),
		writeGate: make(chan struct{}, 1), restartGate: make(chan struct{}, 1), pending: map[int]chan json.RawMessage{}}
	c.readLoop(c.stdout) // Reader has exited, but the write side still accepts data.
	c.restarter = func(context.Context) (io.Writer, io.Reader, *exec.Cmd, error) {
		cr, sw := io.Pipe()
		sr, cw := io.Pipe()
		t.Cleanup(func() { sr.Close(); sw.Close() })
		go serveFakeMCP(sr, sw)
		return cw, cr, nil, nil
	}
	defer c.Close()
	ctx, cancel := context.WithTimeout(t.Context(), 3*time.Second)
	defer cancel()
	var result struct {
		Tools []MCPToolDef `json:"tools"`
	}
	if err := c.call(ctx, "tools/list", map[string]any{}, &result); err != nil {
		t.Fatal(err)
	}
	if oldWrites.Len() != 0 || len(result.Tools) != 2 {
		t.Fatalf("old writes=%d tools=%d", oldWrites.Len(), len(result.Tools))
	}
}

func TestMCPSSECompletesBeforeEOF(t *testing.T) {
	reader, writer := io.Pipe()
	defer reader.Close()
	defer writer.Close()
	done := make(chan error, 1)
	go func() {
		raw, err := scanSSE(reader, 7)
		if err == nil && !strings.Contains(string(raw), `"ok":true`) {
			err = fmt.Errorf("wrong result: %s", raw)
		}
		done <- err
	}()
	_, err := io.WriteString(writer, "data: {\"jsonrpc\":\"2.0\",\"method\":\"notifications/progress\"}\n\ndata: {\"jsonrpc\":\"2.0\",\"id\":8,\"result\":{}}\n\ndata: {\"jsonrpc\":\"2.0\",\"id\":7,\n"+
		"data: \"result\":{\"ok\":true}}\n\n")
	if err != nil {
		t.Fatal(err)
	}
	select {
	case err := <-done:
		if err != nil {
			t.Fatal(err)
		}
	case <-time.After(3 * time.Second):
		t.Fatal("matching response waited for EOF")
	}
}

func TestMCPSSERequiresMatchingResponse(t *testing.T) {
	_, err := scanSSE(strings.NewReader(`data: {"jsonrpc":"2.0","id":2,"result":{}}`+"\n\n"), 1)
	if err == nil {
		t.Fatal("unrelated response completed the request")
	}
}

func TestMCPHTTPRejectsPrivateRedirect(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		http.Redirect(w, r, "http://10.0.0.1/mcp", http.StatusTemporaryRedirect)
	}))
	defer srv.Close()
	ctx, cancel := context.WithTimeout(t.Context(), time.Second)
	defer cancel()
	_, _, err := StartMCPHTTP(ctx, "redirect", srv.URL)
	if err == nil || !strings.Contains(err.Error(), "public") {
		t.Fatalf("private redirect was not refused: %v", err)
	}
}

func TestMCPHTTPFailedDiscoveryClosesSession(t *testing.T) {
	deleted := make(chan string, 1)
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method == http.MethodDelete {
			deleted <- r.Header.Get("Mcp-Session-Id")
			w.WriteHeader(204)
			return
		}
		var req rpcRequest
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
			t.Error(err)
			return
		}
		switch req.Method {
		case "initialize":
			w.Header().Set("Mcp-Session-Id", "failed-discovery")
			fmt.Fprintf(w, `{"jsonrpc":"2.0","id":%d,"result":{}}`, req.ID)
		case "notifications/initialized":
			w.WriteHeader(202)
		case "tools/list":
			http.Error(w, "catalog unavailable", 500)
		}
	}))
	defer srv.Close()
	_, c, err := StartMCPHTTP(t.Context(), "failure", srv.URL)
	if err == nil || c != nil {
		t.Fatalf("client=%v error=%v", c, err)
	}
	select {
	case id := <-deleted:
		if id != "failed-discovery" {
			t.Fatal(id)
		}
	default:
		t.Fatal("failed discovery leaked the initialized session")
	}
}

func TestMCPRestartWaitHonorsCancellation(t *testing.T) {
	c := &MCPClient{restartGate: make(chan struct{}, 1)}
	c.restartGate <- struct{}{}
	ctx, cancel := context.WithCancel(t.Context())
	cancel()
	if err := c.restartTransport(ctx); !errors.Is(err, context.Canceled) {
		t.Fatalf("waiting for another restart: %v", err)
	}
}

func TestMCPSSEBoundsMultilineEvent(t *testing.T) {
	frame := strings.Repeat("data: "+strings.Repeat("x", 1024)+"\n", mcpWireMax/1024+1)
	_, err := scanSSE(strings.NewReader(frame), 1)
	if err == nil || !strings.Contains(err.Error(), "exceeds") {
		t.Fatalf("unbounded event: %v", err)
	}
}
