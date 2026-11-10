package chatcompletions

import (
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"reflect"
	"testing"
	"time"

	"github.com/keshon/gektor/internal/llm"
)

func TestStreamAndUnaryRequestPreparationMatch(t *testing.T) {
	for _, constructor := range []struct {
		name string
		new  func(string, string) *Client
	}{
		{"llama", NewLlamaClient}, {"kobold", NewKoboldClient}, {"openai", NewOpenAIClient},
	} {
		t.Run(constructor.name, func(t *testing.T) {
			c := constructor.new("http://localhost:8080", "model")
			c.APIKey = "test-key"
			c.ExtraHeaders = map[string]string{"X-Test": "value"}
			for _, structured := range []bool{false, true} {
				req := llm.ChatRequest{Messages: []llm.Message{{Role: llm.RoleUser, Content: "task"}}, MaxTokens: 123, Temperature: 0.73, Tools: []llm.ToolDef{{Name: "test", Parameters: json.RawMessage(`{"type":"object"}`)}}}
				if structured {
					req.Grammar = `root ::= "ok"`
					req.JSONSchema = `{"type":"object"}`
				}
				var bodies []map[string]any
				for _, stream := range []bool{false, true} {
					r, err := c.request(t.Context(), req, stream)
					if err != nil {
						t.Fatal(err)
					}
					if r.Header.Get("Authorization") != "Bearer test-key" || r.Header.Get("X-Test") != "value" {
						t.Fatal("transport lost connection headers")
					}
					var body map[string]any
					if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
						t.Fatal(err)
					}
					_ = r.Body.Close()
					for _, key := range []string{"stream", "stream_options", "return_progress", "timings_per_token"} {
						delete(body, key)
					}
					bodies = append(bodies, body)
				}
				if !reflect.DeepEqual(bodies[0], bodies[1]) {
					t.Fatal("stream/unary request drift", bodies)
				}
			}
		})
	}
}

func TestStreamAndUnaryHTTPFailuresPreserveRetryDelay(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("retry-after-ms", "25")
		w.WriteHeader(429)
		_, _ = io.WriteString(w, `{"error":"limited"}`)
	}))
	defer srv.Close()
	c := NewOpenAIClient(srv.URL, "model")
	for _, stream := range []bool{false, true} {
		var err error
		if stream {
			_, err = c.Stream(t.Context(), llm.ChatRequest{}, nil, nil)
		} else {
			_, err = c.Chat(t.Context(), llm.ChatRequest{})
		}
		api, ok := llm.AsAPIError(err)
		if !ok || api.Status != 429 || api.RetryAfter != 25*time.Millisecond {
			t.Fatal("transport dropped rate-limit metadata", stream, err)
		}
	}
}
