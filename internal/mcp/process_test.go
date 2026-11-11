package mcp

import (
	"context"
	"encoding/json"
	"os"
	"testing"
	"time"
)

func TestMain(m *testing.M) {
	if os.Getenv("GEKTOR_PROTOCOL_HELPER") == "1" {
		serveFakeMCP(os.Stdin, os.Stdout)
		return
	}
	os.Exit(m.Run())
}

func TestProcessOutlivesDiscoveryAndCloseIsFinal(t *testing.T) {
	t.Setenv("GEKTOR_PROTOCOL_HELPER", "1")
	launch, cancel := context.WithCancel(t.Context())
	config := StdioConfig{
		Server:    "fixture",
		Directory: t.TempDir(),
		Command:   os.Args[0],
		Env:       os.Environ(),
	}
	client, defs, err := StartStdio(launch, config)
	if err != nil {
		cancel()
		t.Fatal(err)
	}
	defer client.Close()
	cancel()
	if len(defs) != 2 {
		t.Fatal("catalog lost", defs)
	}

	ctx, stop := context.WithTimeout(t.Context(), 3*time.Second)
	defer stop()
	var result json.RawMessage
	err = client.Call(ctx, "tools/call", map[string]any{"name": "echo"}, &result)
	if err != nil {
		t.Fatal("launcher cancellation stopped the server", err)
	}
	client.Close()

	closed := make(chan error, 1)
	go func() { closed <- client.Call(t.Context(), "tools/list", nil, nil) }()
	select {
	case err = <-closed:
		if err == nil {
			t.Fatal("closed session accepted a request")
		}
	case <-time.After(time.Second):
		t.Fatal("closed session waited for restart")
	}
}
