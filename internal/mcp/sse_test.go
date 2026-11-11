package mcp

import (
	"fmt"
	"io"
	"strings"
	"testing"
	"time"
)

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

func TestMCPSSEBoundsMultilineEvent(t *testing.T) {
	frame := strings.Repeat("data: "+strings.Repeat("x", 1024)+"\n", wireMax/1024+1)
	_, err := scanSSE(strings.NewReader(frame), 1)
	if err == nil || !strings.Contains(err.Error(), "exceeds") {
		t.Fatalf("unbounded event: %v", err)
	}
}
