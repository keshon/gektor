package llm

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func TestResponsesRequestReplaysStateWithoutUnsupportedFields(t *testing.T) {
	c := newServer("https://api.openai.com/v1", "test-model", chatgptDialect{})
	reasoning := json.RawMessage(`{"type":"reasoning","id":"rs_1","summary":[],"encrypted_content":"opaque-state"}`)
	previous := Message{Role: RoleAssistant, Content: "Inspecting", ToolCalls: []ToolCall{{ID: "call_1", Name: "read_file", Arguments: json.RawMessage(`{"path":"README.md"}`)}}, ProviderState: &ResponseState{Model: "test-model", Phase: "commentary", Reasoning: []json.RawMessage{reasoning}}}
	// State must survive exactly the serialization used by saved sessions.
	encoded, _ := json.Marshal(previous)
	var restored Message
	if err := json.Unmarshal(encoded, &restored); err != nil {
		t.Fatal(err)
	}
	req := ChatRequest{Messages: []Message{{Role: RoleSystem, Content: "Use Gektor tools"}, {Role: RoleUser, Content: "Read"}, restored, {Role: RoleTool, ToolCallID: "call_1", Content: "file contents"}}, Tools: []ToolDef{{Name: "read_file", Parameters: json.RawMessage(`{"type":"object"}`)}}, MaxTokens: 100, Temperature: 0.7}
	raw, err := c.responseRequest(req)
	if err != nil {
		t.Fatal(err)
	}
	var body map[string]any
	if err = json.Unmarshal(raw, &body); err != nil {
		t.Fatal(err)
	}
	for _, key := range []string{"temperature", "max_tokens", "max_output_tokens", "previous_response_id", "conversation"} {
		if _, ok := body[key]; ok {
			t.Fatal("unsupported field", key)
		}
	}
	if body["store"] != false || body["stream"] != true {
		t.Fatal(string(raw))
	}
	input := body["input"].([]any)
	if input[0].(map[string]any)["role"] != "developer" || input[2].(map[string]any)["encrypted_content"] != "opaque-state" || input[3].(map[string]any)["phase"] != "commentary" || input[5].(map[string]any)["call_id"] != "call_1" {
		t.Fatal(string(raw))
	}
	if body["tools"].([]any)[0].(map[string]any)["type"] != "namespace" {
		t.Fatal(string(raw))
	}
	c.model = "another-model"
	raw, err = c.responseRequest(req)
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(string(raw), "opaque-state") {
		t.Fatal("reasoning leaked across models")
	}
}

func TestResponsesStreamCompletesToolCallAndRecordsUsage(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/responses" || r.Header.Get("Authorization") != "Bearer test-token" {
			t.Error("wrong request", r.URL.Path)
		}
		// Some OAuth Responses deployments omit Content-Type on valid SSE.
		w.Header()["Content-Type"] = nil
		fmt.Fprint(w, "data: {\"type\":\"response.output_text.delta\",\"delta\":\"Inspecting\"}\n\n")
		fmt.Fprint(w, `data: {"type":"response.completed","response":{"status":"completed","output":[{"type":"reasoning","id":"rs_1","summary":[],"encrypted_content":"opaque"},{"type":"message","role":"assistant","phase":"commentary","content":[{"type":"output_text","text":"Inspecting"}]},{"type":"function_call","call_id":"call_1","name":"read_file","namespace":"gektor","arguments":"{\"path\":\"README.md\"}"}],"usage":{"input_tokens":100,"output_tokens":20,"input_tokens_details":{"cached_tokens":80},"output_tokens_details":{"reasoning_tokens":12}}}}`+"\n\n")
	}))
	defer server.Close()
	c := newServer(server.URL, "test-model", chatgptDialect{})
	var streamed string
	out, err := c.sendResponses(context.Background(), ChatRequest{}, []byte(`{}`), "test-token", nil, func(s string) { streamed += s })
	if err != nil {
		t.Fatal(err)
	}
	if streamed != "Inspecting" || out.Message.Content != streamed || len(out.Message.ToolCalls) != 1 || out.Message.ProviderState.Phase != "commentary" || out.FinishReason != "tool_calls" {
		t.Fatalf("bad response: %+v", out)
	}
	if p, g, n := c.UsageTotals(); p != 100 || g != 20 || n != 1 || c.CachedTotal() != 80 {
		t.Fatal(p, g, n)
	}
}

func TestResponsesInterruptedOrFailedStreamNeverExecutesCalls(t *testing.T) {
	for _, event := range []string{
		`{"type":"response.output_item.added","item":{"type":"function_call","name":"write_file"}}`,
		`{"type":"response.failed","response":{"status":"failed","error":{"code":"subscription_sharing_usage_limit_exceeded"}}}`,
		`{"type":"response.incomplete","response":{"status":"incomplete","incomplete_details":{"reason":"max_output_tokens"}}}`,
		`{"type":"response.completed","response":{"status":"completed","output":[{"type":"function_call","name":"write_file","call_id":"c","arguments":"{"}]}}`,
	} {
		t.Run(event[:min(50, len(event))], func(t *testing.T) {
			srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				w.Header().Set("Content-Type", "text/event-stream")
				fmt.Fprintf(w, "data: %s\n\n", event)
			}))
			defer srv.Close()
			c := newServer(srv.URL, "test", chatgptDialect{})
			out, err := c.sendResponses(t.Context(), ChatRequest{}, []byte(`{}`), "test-token", nil, nil)
			if err == nil || len(out.Message.ToolCalls) > 0 {
				t.Fatal("partial call accepted", out, err)
			}
			if _, _, n := c.UsageTotals(); n != 0 {
				t.Fatal("failed response recorded as completed")
			}
		})
	}
	if _, err := ClientFor("chatgpt", "https://other.example/v1", "test"); err == nil {
		t.Fatal("OAuth endpoint override accepted")
	}
	err := responseFailure(429, []byte(`{"error":{"code":"subscription_sharing_usage_limit_exceeded"}}`), "request-1")
	if api, ok := AsAPIError(err); ok && api.Retryable() {
		t.Fatal("plan limit retries automatically")
	}
}

func TestResponsesDoneItemsAreRetainedWhenFinalOutputIsEmpty(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/event-stream")
		// Deliberately deliver out of order; output_index is authoritative.
		fmt.Fprint(w, `data: {"type":"response.output_item.done","output_index":1,"item":{"type":"function_call","name":"read_file","call_id":"call_1","namespace":"gektor","arguments":"{\"path\":\"probe.txt\"}"}}`+"\n\n")
		fmt.Fprint(w, `data: {"type":"response.output_item.done","output_index":0,"item":{"type":"reasoning","id":"rs_1","encrypted_content":"opaque","summary":[]}}`+"\n\n")
		fmt.Fprint(w, `data: {"type":"response.completed","response":{"status":"completed","output":[],"usage":{"input_tokens":20,"output_tokens":10}}}`+"\n\n")
	}))
	defer srv.Close()
	c := newServer(srv.URL, "test", chatgptDialect{})
	out, err := c.sendResponses(t.Context(), ChatRequest{}, []byte(`{}`), "test-token", nil, nil)
	if err != nil {
		t.Fatal(err)
	}
	if len(out.Message.ToolCalls) != 1 || out.Message.ToolCalls[0].Name != "read_file" || out.Message.ProviderState == nil || len(out.Message.ProviderState.Reasoning) != 1 {
		t.Fatal("completed output items lost", out)
	}
	raw, err := c.responseRequest(ChatRequest{Messages: []Message{out.Message}})
	if err != nil || strings.Index(string(raw), "encrypted_content") > strings.Index(string(raw), "function_call") {
		t.Fatal("output ordering lost", string(raw), err)
	}
}
