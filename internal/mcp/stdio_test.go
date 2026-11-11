package mcp

import (
	"bufio"
	"context"
	"encoding/json"
	"errors"
	"io"
	"os"
	"strings"
	"sync"
	"testing"
	"time"
)

func startFakeMCP(t *testing.T, reader *io.PipeReader, writer *io.PipeWriter) {
	t.Helper()
	stopped := make(chan struct{})
	go func() { defer close(stopped); serveFakeMCP(reader, writer) }()
	t.Cleanup(func() {
		_ = reader.Close()
		_ = writer.Close()
		<-stopped
	})
}

// serveFakeMCP is the same server without a *testing.T, so it can run in a
// re-executed copy of this binary.
func serveFakeMCP(r io.Reader, w io.Writer) {
	sc := bufio.NewScanner(r)
	sc.Buffer(make([]byte, 1024*1024), 4*1024*1024)
	bw := bufio.NewWriter(w)
	respond := func(id int, result string) {
		msg, _ := json.Marshal(map[string]any{
			"jsonrpc": "2.0", "id": id, "result": json.RawMessage(result),
		})
		msg = append(msg, '\n')
		_, _ = bw.Write(msg)
		_ = bw.Flush()
	}
	for sc.Scan() {
		var req struct {
			ID     int             `json:"id"`
			Method string          `json:"method"`
			Params json.RawMessage `json:"params"`
		}
		if err := json.Unmarshal(sc.Bytes(), &req); err != nil {
			continue
		}
		if req.Method == "notifications/initialized" {
			continue
		}
		switch req.Method {
		case "initialize":
			respond(req.ID, `{"serverInfo":{"name":"fake","version":"0"}}`)
		case "tools/list":
			respond(req.ID, `{"tools":[
				{"name":"echo","description":"echo back","inputSchema":{"type":"object"}},
				{"name":"boom","description":"always errors","inputSchema":{"type":"object"}}
			]}`)
		case "tools/call":
			var p struct {
				Name string `json:"name"`
			}
			_ = json.Unmarshal(req.Params, &p)
			if p.Name == "boom" {
				respond(req.ID, `{"content":[{"type":"text","text":"kaput"}],"isError":true}`)
			} else {
				if os.Getenv("GEKTOR_MCP_ENV_TEST") == "1" {
					data, _ := json.Marshal(map[string]any{"content": []map[string]string{{"type": "text", "text": os.Getenv("READER_TOKEN") + os.Getenv("UNRELATED_API_KEY")}}})
					respond(req.ID, string(data))
				} else {
					respond(req.ID, `{"content":[{"type":"text","text":"hello from fake"}]}`)
				}
			}
		}
	}
}

func TestMCPRestartAfterCrash(t *testing.T) {

	// First transport: handshake succeeds, then the server dies (write
	// side closed). The next call must restart and succeed, not fail.
	clientR, serverW := io.Pipe()
	serverR, clientW := io.Pipe()
	startFakeMCP(t, serverR, serverW)

	ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
	defer cancel()
	client, _, err := connect(ctx, "flaky", clientW, clientR)
	if err != nil {
		t.Fatalf("connect: %v", err)
	}
	defer client.Close()
	// Kill both server ends: the client's next write fails (closed pipe)
	// instead of hanging on a server that will never answer.
	_ = serverW.Close()
	_ = serverR.Close()

	client.backoff = 0
	client.source = &pipeSource{t: t}
	var out struct {
		Tools []ToolDefinition `json:"tools"`
	}
	// tools/list round-trips through the dead transport, restarts, and
	// succeeds against the fresh server.
	if err := client.Call(ctx, "tools/list", map[string]any{}, &out); err != nil {
		t.Fatalf("call after crash: %v", err)
	}
	if len(out.Tools) != 2 {
		t.Fatalf("tools=%d, want 2", len(out.Tools))
	}
}

func TestMCPRestartDisabledWithoutRestarter(t *testing.T) {
	clientR, serverW := io.Pipe()
	serverR, clientW := io.Pipe()
	startFakeMCP(t, serverR, serverW)

	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	client, _, err := connect(ctx, "fake", clientW, clientR)
	if err != nil {
		t.Fatalf("connect: %v", err)
	}
	defer client.Close()
	_ = serverW.Close()
	// send on a closed pipe errors; without a restarter that error must
	// surface unchanged rather than hang or retry.
	if err := client.Call(ctx, "tools/list", map[string]any{}, nil); err == nil {
		t.Fatal("expected transport error, got nil")
	}
}

func TestMCPDoesNotReplayToolAfterDisconnect(t *testing.T) {
	cr, sw := io.Pipe()
	sr, cw := io.Pipe()
	startFakeMCP(t, sr, sw)
	client, _, err := connect(t.Context(), "flaky", cw, cr)
	if err != nil {
		t.Fatal(err)
	}
	defer client.Close()
	_ = sw.Close()
	_ = sr.Close()
	client.backoff = 0
	client.source = &pipeSource{t: t}
	var out json.RawMessage
	err = client.Call(t.Context(), "tools/call", map[string]any{"name": "echo", "arguments": map[string]any{}}, &out)
	if err == nil || !strings.Contains(err.Error(), "outcome unknown") {
		t.Fatal("tool replayed or missing recovery guidance", err, string(out))
	}
	if err = client.Call(t.Context(), "tools/list", map[string]any{}, &out); err != nil {
		t.Fatal("replacement server unavailable", err)
	}
}

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
	c := &Stdio{stdin: bufio.NewWriter(observed), stdout: bufio.NewReader(output), input: observed, output: output,
		writeGate: make(chan struct{}, 1), restartGate: make(chan struct{}, 1), pending: map[int]chan json.RawMessage{}}
	defer c.Close()
	readDone := make(chan struct{})
	go func() { c.readLoop(c.stdout); close(readDone) }()
	ctx, cancel := context.WithCancel(t.Context())
	defer cancel()
	done := make(chan error, 1)
	go func() { done <- c.Call(ctx, "tools/call", map[string]any{"text": strings.Repeat("x", 8192)}, nil) }()
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
	var oldWrites strings.Builder
	c := &Stdio{stdin: bufio.NewWriter(&oldWrites), stdout: bufio.NewReader(strings.NewReader("")),
		writeGate: make(chan struct{}, 1), restartGate: make(chan struct{}, 1), pending: map[int]chan json.RawMessage{}}
	c.readLoop(c.stdout) // Reader has exited, but the write side still accepts data.
	c.source = &pipeSource{t: t}
	defer c.Close()
	ctx, cancel := context.WithTimeout(t.Context(), 3*time.Second)
	defer cancel()
	var result struct {
		Tools []ToolDefinition `json:"tools"`
	}
	if err := c.Call(ctx, "tools/list", map[string]any{}, &result); err != nil {
		t.Fatal(err)
	}
	if oldWrites.Len() != 0 || len(result.Tools) != 2 {
		t.Fatalf("old writes=%d tools=%d", oldWrites.Len(), len(result.Tools))
	}
}

func TestMCPRestartWaitHonorsCancellation(t *testing.T) {
	c := &Stdio{restartGate: make(chan struct{}, 1)}
	c.restartGate <- struct{}{}
	ctx, cancel := context.WithCancel(t.Context())
	cancel()
	if err := c.restartTransport(ctx); !errors.Is(err, context.Canceled) {
		t.Fatalf("waiting for another restart: %v", err)
	}
}

type pipeSource struct {
	t       *testing.T
	streams []io.Closer
	workers sync.WaitGroup
}

func (s *pipeSource) Launch(ctx context.Context) (io.WriteCloser, io.ReadCloser, error) {
	clientReader, serverWriter := io.Pipe()
	serverReader, clientWriter := io.Pipe()
	s.streams = append(s.streams, serverWriter, serverReader)
	s.workers.Add(1)
	go func() { defer s.workers.Done(); serveFakeMCP(serverReader, serverWriter) }()
	return clientWriter, clientReader, nil
}

func (s *pipeSource) Close() {
	for _, stream := range s.streams {
		_ = stream.Close()
	}
	s.workers.Wait()
}
