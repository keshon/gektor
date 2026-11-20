//go:build ignore

// Explicit invocation: go test scripts/tui-smoke.go scripts/tui-smoke_test.go
package main

import (
	"context"
	"encoding/json"
	"errors"
	"net/http/httptest"
	"os"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/keshon/gektor/internal/app"
	"github.com/keshon/gektor/internal/llm"
	"github.com/keshon/gektor/internal/llm/chatcompletions"
	"github.com/keshon/gektor/internal/permission"
	"github.com/keshon/gektor/internal/workflow/script"
	"github.com/keshon/gektor/internal/workspace"
)

func TestMain(m *testing.M) {
	if len(os.Args) > 2 && os.Args[1] == "workflow" && os.Args[2] == "internal-script-worker" {
		if script.Serve(os.Stdin, os.Stdout) != nil {
			os.Exit(1)
		}
		os.Exit(0)
	}
	os.Exit(m.Run())
}

func TestSmokeToolProtocolThroughProductionClient(t *testing.T) {
	for _, stream := range []bool{false, true} {
		var requests atomic.Int64
		server := httptest.NewServer(smokeBackend(&requests, 0))
		client := chatcompletions.NewOpenAIClient(server.URL, "smoke")
		req := llm.ChatRequest{Messages: []llm.Message{{Role: llm.RoleUser, Content: "smoke tools"}}}
		var response llm.ChatResponse
		var err error
		if stream {
			response, err = client.Stream(t.Context(), req, nil, func(string) {})
		} else {
			response, err = client.Chat(t.Context(), req)
		}
		if err != nil || response.FinishReason != "tool_calls" || len(response.Message.ToolCalls) != 2 {
			t.Fatalf("stream=%v: tool response: %+v, %v", stream, response, err)
		}
		for index, call := range response.Message.ToolCalls {
			var args struct {
				Path string `json:"path"`
			}
			if err := json.Unmarshal(call.Arguments, &args); err != nil || call.Name != "read_file" || args.Path != []string{"smoke-one.txt", "smoke-two.txt"}[index] {
				t.Fatalf("stream=%v: invalid fixture tool: %+v", stream, call)
			}
		}
		req.Messages = append(req.Messages, response.Message)
		for _, call := range response.Message.ToolCalls {
			req.Messages = append(req.Messages, llm.Message{Role: llm.RoleTool, ToolCallID: call.ID, Content: "fixture read result"})
		}
		final, err := client.Chat(t.Context(), req)
		if err != nil || len(final.Message.ToolCalls) != 0 || !strings.Contains(final.Message.Content, "results received") || requests.Load() != 2 {
			t.Fatalf("stream=%v: tool follow-up repeated tools: %+v, %v", stream, final, err)
		}
		server.Close()
	}
}

func TestSmokePermissionFixturesUseShippingAskPolicy(t *testing.T) {
	var requests atomic.Int64
	server := httptest.NewServer(smokeBackend(&requests, 0))
	defer server.Close()
	client := chatcompletions.NewOpenAIClient(server.URL, "smoke")
	for _, decision := range []string{"allow", "deny"} {
		response, err := client.Chat(t.Context(), llm.ChatRequest{Messages: []llm.Message{{Role: llm.RoleUser, Content: "smoke " + decision}}})
		if err != nil || len(response.Message.ToolCalls) != 1 {
			t.Fatalf("permission response: %+v, %v", response, err)
		}
		call := response.Message.ToolCalls[0]
		var args struct {
			Path string `json:"path"`
		}
		if err := json.Unmarshal(call.Arguments, &args); err != nil || args.Path != "smoke-"+decision+".env" || permission.Default().Evaluate(call.Name, args.Path) != permission.Ask {
			t.Fatalf("permission fixture bypasses default gate: %+v", call)
		}
	}
}

func TestSmokeStreamingFlushesAndHonorsCancellation(t *testing.T) {
	var requests atomic.Int64
	server := httptest.NewServer(smokeBackend(&requests, time.Second))
	defer server.Close()
	client := chatcompletions.NewOpenAIClient(server.URL, "smoke")
	ctx, cancel := context.WithCancel(t.Context())
	defer cancel()
	chunks := 0
	_, err := client.Stream(ctx, llm.ChatRequest{Messages: []llm.Message{{Role: llm.RoleUser, Content: "smoke stream"}}}, nil, func(text string) {
		if strings.Contains(text, "chunk 01") {
			chunks++
			cancel()
		}
	})
	if chunks != 1 || !errors.Is(err, context.Canceled) {
		t.Fatalf("stream did not flush before completion or failed cancellation: chunks=%d err=%v", chunks, err)
	}
}

func TestSmokeWorkflowRunsThroughProductionService(t *testing.T) {
	ws, err := workspace.New(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	defer ws.Close()
	service, err := app.NewWorkflow(ws, t.TempDir(), permission.Default(), nil)
	if err != nil {
		t.Fatal(err)
	}
	result, err := service.RunSource(t.Context(), []byte(smokeRecipe), "smoke-workflow.json", nil)
	if err != nil || result.Status != "completed" {
		t.Fatalf("smoke workflow failed: %+v, %v", result, err)
	}
}
