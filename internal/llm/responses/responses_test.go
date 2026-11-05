package responses

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/keshon/gektor/internal/llm"
)

func TestResponsesRequestReplaysStateWithoutUnsupportedFields(t *testing.T) {
	c := newTestClient("https://api.openai.com/v1", "test-model")
	reasoning := json.RawMessage(`{"type":"reasoning","id":"rs_1","summary":[],"encrypted_content":"opaque-state"}`)
	c.replayScope = "registration:subject"
	previousResponse, err := c.decodeResponse(responseEnvelope{Output: []json.RawMessage{reasoning, json.RawMessage(`{"type":"message","role":"assistant","phase":"commentary","content":[{"type":"output_text","text":"Inspecting"}]}`), json.RawMessage(`{"type":"function_call","call_id":"call_1","name":"read_file","namespace":"gektor","arguments":"{\"path\":\"README.md\"}"}`)}})
	if err != nil {
		t.Fatal(err)
	}
	previous := previousResponse.Message

	// State must survive exactly the serialization used by saved sessions.
	encoded, _ := json.Marshal(previous)
	var restored llm.Message
	if err := json.Unmarshal(encoded, &restored); err != nil {
		t.Fatal(err)
	}
	req := llm.ChatRequest{Messages: []llm.Message{{Role: llm.RoleSystem, Content: "Use Gektor tools"}, {Role: llm.RoleUser, Content: "Read"}, restored, {Role: llm.RoleTool, ToolCallID: "call_1", Content: "file contents"}}, Tools: []llm.ToolDef{{Name: "read_file", Parameters: json.RawMessage(`{"type":"object"}`)}}, MaxTokens: 100, Temperature: 0.7}
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
	c := newTestClient(server.URL, "test-model")
	c.replayScope = "test-scope"
	var streamed string
	out, err := c.sendResponses(context.Background(), llm.ChatRequest{}, []byte(`{}`), "test-token", nil, func(s string) { streamed += s })
	if err != nil {
		t.Fatal(err)
	}
	if streamed != "Inspecting" || out.Message.Content != streamed || len(out.Message.ToolCalls) != 1 || !strings.Contains(string(out.Message.ProviderState.Payload), `"phase":"commentary"`) || out.FinishReason != "tool_calls" {
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
			c := newTestClient(srv.URL, "test")
			c.replayScope = "test-scope"
			out, err := c.sendResponses(t.Context(), llm.ChatRequest{}, []byte(`{}`), "test-token", nil, nil)
			if err == nil || len(out.Message.ToolCalls) > 0 {
				t.Fatal("partial call accepted", out, err)
			}
			if _, _, n := c.UsageTotals(); n != 0 {
				t.Fatal("failed response recorded as completed")
			}
		})
	}

	err := responseFailure(429, []byte(`{"error":{"code":"subscription_sharing_usage_limit_exceeded"}}`), "request-1")
	if api, ok := llm.AsAPIError(err); ok && api.Retryable() {
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
	c := newTestClient(srv.URL, "test")
	c.replayScope = "test-scope"
	out, err := c.sendResponses(t.Context(), llm.ChatRequest{}, []byte(`{}`), "test-token", nil, nil)
	if err != nil {
		t.Fatal(err)
	}
	if len(out.Message.ToolCalls) != 1 || out.Message.ToolCalls[0].Name != "read_file" || out.Message.ProviderState == nil || !strings.Contains(string(out.Message.ProviderState.Payload), "encrypted_content") {
		t.Fatal("completed output items lost", out)
	}
	raw, err := c.responseRequest(llm.ChatRequest{Messages: []llm.Message{out.Message}})
	if err != nil || strings.Index(string(raw), "encrypted_content") > strings.Index(string(raw), "function_call") {
		t.Fatal("output ordering lost", string(raw), err)
	}
}

func TestResponsesRetainsDisplayedOutputOnEveryFailure(t *testing.T) {
	for _, tail := range []string{
		"",
		"data: not-json\n\n",
		"data: {\"type\":\"response.failed\",\"response\":{\"error\":{\"code\":\"failed\"}}}\n\n",
		"data: {\"type\":\"response.incomplete\",\"response\":{\"incomplete_details\":{\"reason\":\"max_output_tokens\"}}}\n\n",
		"data: {\"type\":\"response.completed\",\"response\":{\"status\":\"completed\",\"output\":[{\"type\":\"function_call\",\"name\":\"write_file\",\"call_id\":\"c\",\"arguments\":\"{\"}]}}\n\n",
	} {
		t.Run(fmt.Sprint(len(tail)), func(t *testing.T) {
			srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				w.Header().Set("Content-Type", "text/event-stream")
				fmt.Fprint(w, "data: {\"type\":\"response.output_text.delta\",\"delta\":\"visible\"}\n\n")
				fmt.Fprint(w, "data: {\"type\":\"response.reasoning_summary_text.delta\",\"delta\":\"thinking\"}\n\n")
				fmt.Fprint(w, tail)
			}))
			defer srv.Close()
			c := newTestClient(srv.URL, "test")
			c.replayScope = "test-scope"
			out, err := c.sendResponses(t.Context(), llm.ChatRequest{}, []byte(`{}`), "test", nil, nil)
			if err == nil || out.Message.Content != "visible" || out.Message.Reasoning != "thinking" || len(out.Message.ToolCalls) != 0 || out.Message.ProviderState != nil {
				t.Fatalf("partial output lost or unsafe: %+v, %v", out, err)
			}
		})
	}
}

func TestResponsesReplayOrderAndMutationFallback(t *testing.T) {
	c := newTestClient("https://api.openai.com/v1", "test")
	c.replayScope = "registration:subject"
	items := []json.RawMessage{
		json.RawMessage(`{"type":"message","role":"assistant","phase":"commentary","content":[{"type":"output_text","text":"Checking"}]}`),
		json.RawMessage(`{"type":"reasoning","encrypted_content":"opaque","summary":[]}`),
		json.RawMessage(`{"type":"message","role":"assistant","phase":"final_answer","content":[{"type":"output_text","text":"Done"}]}`),
		json.RawMessage(`{"type":"function_call","call_id":"c","name":"read_file","arguments":"{}"}`),
	}
	out, err := c.decodeResponse(responseEnvelope{Output: items})
	if err != nil {
		t.Fatal(err)
	}
	saved, _ := json.Marshal(out.Message)
	var restored llm.Message
	if err = json.Unmarshal(saved, &restored); err != nil {
		t.Fatal(err)
	}
	raw, err := c.responseRequest(llm.ChatRequest{Messages: []llm.Message{restored}})
	if err != nil {
		t.Fatal(err)
	}
	var wire struct {
		Input []json.RawMessage `json:"input"`
	}
	json.Unmarshal(raw, &wire)
	if len(wire.Input) != 4 || !strings.Contains(string(wire.Input[0]), "commentary") || !strings.Contains(string(wire.Input[1]), "opaque") || !strings.Contains(string(wire.Input[2]), "final_answer") {
		t.Fatal(string(raw))
	}
	for _, mutation := range []string{"content", "calls", "rejected", "model", "scope", "endpoint", "version", "corrupt"} {
		t.Run(mutation, func(t *testing.T) {
			var m llm.Message
			json.Unmarshal(saved, &m)
			other := newTestClient(c.baseURL, c.model)
			other.replayScope = c.replayScope
			switch mutation {
			case "content":
				m.Content = "Changed"
			case "calls":
				m.ToolCalls = nil
			case "rejected":
				m.ToolCalls[0].InputError = "rejected"
			case "model":
				other.model = "other"
			case "scope":
				other.replayScope = "other"
			case "endpoint":
				other.baseURL = "https://other.example"
			case "version":
				m.ProviderState.Version = 99
			case "corrupt":
				m.ProviderState.Payload = json.RawMessage(`[{"type":"function_call","call_id":"hidden","name":"write_file","arguments":"{}"}]`)
			}
			raw, err := other.responseRequest(llm.ChatRequest{Messages: []llm.Message{m}})
			if err != nil {
				t.Fatal(err)
			}
			if strings.Contains(string(raw), "opaque") || strings.Contains(string(raw), "phase") || strings.Contains(string(raw), "hidden") {
				t.Fatal(string(raw))
			}
			if (mutation == "calls" || mutation == "rejected") && strings.Contains(string(raw), "function_call") {
				t.Fatal("removed call resurrected", string(raw))
			}
		})
	}
	items[0][0] = 'x'
	if !json.Valid(restored.ProviderState.Payload) {
		t.Fatal("mutable native bytes shared")
	}
}

func newTestClient(base, model string) *Client { return New(base, model, "", nil) }

func TestResponsesCancellationKeepsPartialText(t *testing.T) {
	ctx, cancel := context.WithCancel(t.Context())
	defer cancel()
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/event-stream")
		fmt.Fprint(w, "data: {\"type\":\"response.output_text.delta\",\"delta\":\"partial\"}\n\n")
		w.(http.Flusher).Flush()
		<-r.Context().Done()
	}))
	defer srv.Close()
	c := newTestClient(srv.URL, "test")
	out, err := c.sendResponses(ctx, llm.ChatRequest{}, []byte(`{}`), "test", nil, func(string) { cancel() })
	if err != context.Canceled || out.Message.Content != "partial" || out.Message.ProviderState != nil || len(out.Message.ToolCalls) > 0 {
		t.Fatal(out, err)
	}
}

func TestLegacyStateIsIgnoredWithoutLosingToolContinuation(t *testing.T) {
	var m llm.Message
	err := json.Unmarshal([]byte(`{"Role":"assistant","Content":"Checking","ToolCalls":[{"ID":"c","Name":"read_file","Arguments":{}}],"provider_state":{"model":"test","reasoning":[{"type":"reasoning","encrypted_content":"legacy"}],"phase":"commentary"}}`), &m)
	if err != nil {
		t.Fatal(err)
	}
	c := newTestClient("https://api.openai.com/v1", "test")
	c.replayScope = "scope"
	raw, err := c.responseRequest(llm.ChatRequest{Messages: []llm.Message{m, {Role: llm.RoleTool, ToolCallID: "c", Content: "result"}}})
	if err != nil || strings.Contains(string(raw), "legacy") || strings.Contains(string(raw), "phase") || !strings.Contains(string(raw), "function_call_output") || !strings.Contains(string(raw), "read_file") {
		t.Fatal(string(raw), err)
	}
}

func TestRejectedNormalizedContinuationExplainsRecovery(t *testing.T) {
	calls := 0
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		calls++
		w.WriteHeader(http.StatusBadRequest)
		fmt.Fprint(w, `{"error":{"code":"invalid_request_error"}}`)
	}))
	defer server.Close()
	c := New(server.URL, "model", "scope", func(context.Context) (string, error) { return "test", nil })
	req := llm.ChatRequest{Messages: []llm.Message{{Role: llm.RoleAssistant, Content: "previous answer", ProviderState: &llm.ReplayState{Version: 0}}}}
	_, err := llm.ChatWithRetry(t.Context(), c, req, llm.RetryPolicy{})
	if err == nil || !strings.Contains(err.Error(), "start a new session") || calls != 1 {
		t.Fatalf("calls=%d error=%v", calls, err)
	}
}
