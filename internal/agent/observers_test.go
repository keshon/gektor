package agent

import (
	"context"
	"encoding/json"
	"sort"
	"sync"
	"testing"

	"github.com/keshon/gektor/internal/llm"
)

func TestAgent_OnResultSeesEveryCall(t *testing.T) {
	client := &stubClient{responses: []llm.ChatResponse{
		{Message: llm.Message{Role: llm.RoleAssistant, ToolCalls: []llm.ToolCall{
			{ID: "c1", Name: "echo", Arguments: json.RawMessage(`{}`)},
			{ID: "c2", Name: "echo", Arguments: json.RawMessage(`{}`)},
		}}},
		{Message: llm.Message{Role: llm.RoleAssistant, Content: "done"}},
	}}
	var mu sync.Mutex
	var got []string
	a := New(Config{Client: client, Tools: NewRegistry(echoToolStub{}), System: "sys",

		OnResult: func(r ToolResult) {
			mu.Lock()
			defer mu.Unlock()
			got = append(got, r.CallID+":"+r.Output)
		},
	})
	if _, err := a.Run(context.Background(), "task"); err != nil {
		t.Fatalf("Run: %v", err)
	}
	// Concurrent calls complete in goroutine order, so compare as a set.
	mu.Lock()
	defer mu.Unlock()
	sort.Strings(got)
	if len(got) != 2 || got[0] != "c1:ok" || got[1] != "c2:ok" {
		t.Fatalf("results = %v", got)
	}
}

func TestAgent_OnUsageReportsCounts(t *testing.T) {
	client := &stubClient{responses: []llm.ChatResponse{
		{Message: llm.Message{Role: llm.RoleAssistant, Content: "done"},
			Usage: llm.Usage{PromptTokens: 1500, CompletionTokens: 20, CachedTokens: 300}},
	}}
	var got []llm.Usage
	a := New(Config{Client: client, Tools: NewRegistry(), System: "sys",

		OnUsage: func(step int, usage llm.Usage) {
			got = append(got, usage)
		},
	})
	if _, err := a.Run(context.Background(), "task"); err != nil {
		t.Fatalf("Run: %v", err)
	}
	if len(got) != 1 || got[0].PromptTokens != 1500 || got[0].CachedTokens != 300 {
		t.Fatalf("usage = %+v", got)
	}
}
