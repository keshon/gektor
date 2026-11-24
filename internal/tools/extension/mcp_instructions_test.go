package extension

import (
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func TestMCPServerInstructionsReachActivatedCapability(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var req rpcRequest
		err := json.NewDecoder(r.Body).Decode(&req)
		if err != nil {
			t.Error(err)
			return
		}
		w.Header().Set("Content-Type", "application/json")
		var result string
		switch req.Method {
		case "initialize":
			result = `{"serverInfo":{"name":"fixture","version":"1"},"instructions":"Call get_help before editing. Never resend a body to change a cover."}`
		case "tools/list":
			result = `{"tools":[{"name":"get_help","description":"Read first","inputSchema":{"type":"object"}}]}`
		default:
			w.WriteHeader(http.StatusAccepted)
			return
		}
		_, err = fmt.Fprintf(w, `{"jsonrpc":"2.0","id":%d,"result":%s}`, req.ID, result)
		if err != nil {
			t.Error(err)
		}
	}))
	defer server.Close()
	config := CapabilityConfig{Packs: []CapabilityPack{{ID: "blog", Type: "mcp", URL: server.URL, Instructions: "Configured local guidance."}}}
	sources, closeSources := BuildCapabilities(config, t.TempDir())
	defer closeSources()
	tools, err := sources[0].load(t.Context(), func(string) {})
	if err != nil {
		t.Fatal(err)
	}
	state := capabilityState(sources[0], tools)
	if !strings.Contains(state.Instructions, "Call get_help") || !strings.Contains(state.Instructions, "Configured local guidance") {
		t.Fatalf("server/local instructions lost: %q", state.Instructions)
	}
	_, err = sources[0].load(t.Context(), func(string) {})
	if err != nil || sources[0].Instructions != state.Instructions {
		t.Fatal("cached activation changed instructions")
	}
}
