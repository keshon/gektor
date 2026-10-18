package llm

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// llama.cpp streams report no usage, so the prompt size is estimated from
// the request. Every budget downstream trusts that number: compaction fires
// on it and it caps the response allowance. An estimate that counts what is
// not sent, and misses what is, is not a rough figure — it is a wrong one
// that gets acted on.

// streamUsage runs one streamed turn over a fixed server and returns what
// the client reported.
func streamUsage(t *testing.T, req ChatRequest) Usage {
	t.Helper()
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "text/event-stream")
		for _, line := range []string{
			`data: {"choices":[{"delta":{"content":"ok"}}]}`,
			`data: [DONE]`,
		} {
			w.Write([]byte(line + "\n\n"))
		}
	}))
	defer srv.Close()

	c := NewLlamaClient(srv.URL, "test")
	resp, err := c.Stream(context.Background(), req, nil, nil)
	if err != nil {
		t.Fatal(err)
	}
	if !resp.Usage.Estimated {
		t.Skip("server reported real usage; there is no estimate to check")
	}
	return resp.Usage
}

func TestStreamingEstimateCountsToolCallArguments(t *testing.T) {
	usage := streamUsage(t, ChatRequest{Messages: []Message{
		{Role: RoleUser, Content: "write it"},
		{Role: RoleAssistant, ToolCalls: []ToolCall{{
			ID:        "c1",
			Name:      "write_file",
			Arguments: json.RawMessage(strings.Repeat("x", 400)),
		}}},
	}})
	if usage.PromptTokens < 100 {
		t.Fatalf("PromptTokens = %d; tool-call arguments were not counted", usage.PromptTokens)
	}
}

// Reasoning is decoded for display and never encoded back (see
// Message.Reasoning). Counting it inflated the prompt on every step of a
// reasoning run, which compacted early and shrank the response allowance
// for content the backend never received.
func TestStreamingEstimateIgnoresReasoning(t *testing.T) {
	plain := streamUsage(t, ChatRequest{Messages: []Message{
		{Role: RoleUser, Content: "same text"},
	}})
	withThinking := streamUsage(t, ChatRequest{Messages: []Message{{
		Role:      RoleUser,
		Content:   "same text",
		Reasoning: strings.Repeat("deliberation ", 500),
	}}})
	if withThinking.PromptTokens != plain.PromptTokens {
		t.Fatalf("Reasoning moved the estimate from %d to %d; it is never sent",
			plain.PromptTokens, withThinking.PromptTokens)
	}
}

// A picture costs more than its filename and far less than its base64.
// Counting neither would let an image-heavy turn overrun the window the
// meter claims it has.
func TestStreamingEstimateCountsImages(t *testing.T) {
	dir := t.TempDir()
	var images []string
	for _, name := range []string{"a.png", "b.png"} {
		path := filepath.Join(dir, name)
		if err := os.WriteFile(path, []byte("\x89PNG\r\n\x1a\n"), 0o644); err != nil {
			t.Fatal(err)
		}
		images = append(images, path)
	}
	usage := streamUsage(t, ChatRequest{Messages: []Message{{
		Role:   RoleUser,
		Images: images,
	}}})
	if want := 2 * imageEstimateTokens / 4; usage.PromptTokens < want {
		t.Fatalf("PromptTokens = %d, want at least %d for two pictures", usage.PromptTokens, want)
	}
}
