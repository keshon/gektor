package tools

import (
	"context"
	"encoding/json"
	"os"
	"strings"
	"testing"
	"time"
)

// mcpHelperEnv marks a re-executed copy of this binary as the MCP server.
const mcpHelperEnv = "GEKTOR_MCP_HELPER"

func TestMain(m *testing.M) {
	if os.Getenv("GEKTOR_EXEC_HELPER") == "1" {
		writeLiteralExecutionArgs()
		return
	}
	if os.Getenv(mcpHelperEnv) == "stderr" {
		os.Stderr.WriteString("browser startup failed: test package missing\n")
		return
	}
	if os.Getenv(mcpHelperEnv) == "1" {
		serveFakeMCP(os.Stdin, os.Stdout)
		return
	}
	// Spilled tool output lands in the user's GEKTOR directory. A test that
	// exercises spilling must not write into the developer's real one.
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

func TestMCPStartupFailureIncludesServerStderr(t *testing.T) {
	t.Setenv(mcpHelperEnv, "stderr")
	_, _, err := StartMCP(t.Context(), "broken", t.TempDir(), os.Args[0])
	if err == nil || !strings.Contains(err.Error(), "test package missing") {
		t.Fatalf("startup diagnostic lost: %v", err)
	}
}

// StartMCP spawns a real child process, which the in-process pipe tests
// cannot cover. The server has to outlive its launcher's context: the
// handshake timeout owns a context that is cancelled the moment StartMCP
// returns, and using that context for the process killed the server before
// the first tools/call.
func TestStartMCPKeepsServerAliveAfterLauncherCancels(t *testing.T) {
	t.Setenv(mcpHelperEnv, "1")
	launch, cancelLaunch := context.WithCancel(context.Background())

	tools, client, err := StartMCP(launch, "helper", t.TempDir(), os.Args[0])
	if err != nil {
		t.Fatalf("StartMCP: %v", err)
	}
	defer client.Close()
	if len(tools) == 0 {
		t.Fatal("no tools discovered")
	}

	// The launcher's context is done; only the client's own lifetime may
	// keep the server up.
	cancelLaunch()
	time.Sleep(100 * time.Millisecond)

	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	var out json.RawMessage
	if err := client.call(ctx, "tools/call", map[string]any{
		"name":      "echo",
		"arguments": map[string]string{"text": "ping"},
	}, &out); err != nil {
		t.Fatalf("call after launcher cancelled: %v", err)
	}
	if !strings.Contains(string(out), "hello from fake") {
		t.Fatalf("unexpected reply: %s", out)
	}
}
