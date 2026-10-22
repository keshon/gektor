package tools

import (
	"bytes"
	"context"
	"encoding/base64"
	"encoding/json"
	"image"
	"image/png"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync/atomic"
	"testing"

	"github.com/keshon/gektor/internal/llm"
)

type contractMCP func(context.Context, string, any, any) error

func (f contractMCP) call(ctx context.Context, m string, p any, out any) error {
	return f(ctx, m, p, out)
}

func TestMCPCatalogReadsEveryPageAndRejectsCycles(t *testing.T) {
	for _, cycle := range []bool{false, true} {
		calls := 0
		c := contractMCP(func(_ context.Context, m string, p any, out any) error {
			calls++
			result := `{"tools":[{"name":"one"}],"nextCursor":"next"}`
			if calls > 1 && !cycle {
				if p.(map[string]any)["cursor"] != "next" {
					t.Fatal(p)
				}
				result = `{"tools":[{"name":"two"}]}`
			}
			return json.Unmarshal([]byte(result), out)
		})
		defs, err := listMCPTools(t.Context(), c)
		if cycle {
			if err == nil || calls != 2 {
				t.Fatal("cursor cycle not bounded", calls, err)
			}
		} else if err != nil || len(defs) != 2 {
			t.Fatal(defs, err)
		}
	}
}

func TestMCPReturnsStructuredTextAndValidatedImages(t *testing.T) {
	var pixel bytes.Buffer
	_ = png.Encode(&pixel, image.NewRGBA(image.Rect(0, 0, 1, 1)))
	encoded := base64.StdEncoding.EncodeToString(pixel.Bytes())
	c := contractMCP(func(_ context.Context, _ string, _ any, out any) error {
		data, _ := json.Marshal(map[string]any{"structuredContent": map[string]any{"answer": 42}, "content": []any{map[string]any{"type": "text", "text": "measured"}, map[string]any{"type": "image", "mimeType": "image/png", "data": encoded}}})
		return json.Unmarshal(data, out)
	})
	tool := &MCPTool{client: c, server: "test", def: MCPToolDef{Name: "capture"}}
	args := json.RawMessage(`{}`)
	text, err := tool.Run(VisionContext(t.Context(), false), args)
	if err != nil || !strings.Contains(text, `"answer":42`) || !strings.Contains(text, "no vision") {
		t.Fatal(text, err)
	}
	paths, _ := tool.Attach(args)
	if len(paths) != 0 {
		t.Fatal("text model received images")
	}
	text, err = tool.Run(VisionContext(t.Context(), true), args)
	if err != nil {
		t.Fatal(err)
	}
	paths, err = tool.Attach(args)
	if err != nil || len(paths) != 1 {
		t.Fatal(text, paths, err)
	}
	if err := llm.ValidateImage(paths[0]); err != nil {
		t.Fatal(err)
	}
	paths, _ = tool.Attach(args)
	if len(paths) != 0 {
		t.Fatal("attachment replayed")
	}
}

func TestMCPHTTPReinitializesWithoutReplayingMutation(t *testing.T) {
	var initializes, mutations atomic.Int32
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method == http.MethodDelete {
			w.WriteHeader(204)
			return
		}
		var req rpcRequest
		_ = json.NewDecoder(r.Body).Decode(&req)
		result := `{}`
		switch req.Method {
		case "initialize":
			initializes.Add(1)
			w.Header().Set("Mcp-Session-Id", "session")
			result = `{"serverInfo":{"name":"test"}}`
		case "notifications/initialized":
			w.WriteHeader(202)
			return
		case "tools/list":
			result = `{"tools":[{"name":"mutate","inputSchema":{"type":"object"}}]}`
		case "tools/call":
			if mutations.Add(1) == 1 {
				w.WriteHeader(404)
				return
			}
			result = `{"content":[{"type":"text","text":"ok"}]}`
		}
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(map[string]any{"jsonrpc": "2.0", "id": req.ID, "result": json.RawMessage(result)})
	}))
	defer srv.Close()
	tools, c, err := StartMCPHTTP(t.Context(), "test", srv.URL)
	if err != nil {
		t.Fatal(err)
	}
	defer c.Close()
	if _, err = tools[0].Run(t.Context(), json.RawMessage(`{}`)); err == nil || !strings.Contains(err.Error(), "not replayed") {
		t.Fatal(err)
	}
	if mutations.Load() != 1 || initializes.Load() != 2 {
		t.Fatal("mutation replayed or handshake skipped", mutations.Load(), initializes.Load())
	}
	if _, err = tools[0].Run(t.Context(), json.RawMessage(`{}`)); err != nil || mutations.Load() != 2 {
		t.Fatal("explicit retry failed", err)
	}
}
