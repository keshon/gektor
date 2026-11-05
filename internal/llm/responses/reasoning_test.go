package responses

import (
	"encoding/json"
	"testing"

	"github.com/keshon/gektor/internal/llm"
)

func TestReasoningRequestControls(t *testing.T) {
	for _, tc := range []struct {
		name, effort string
		summary      bool
	}{
		{"default", "", false},
		{"effort", "high", false},
		{"summary", "", true},
		{"both", "low", true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			c := newTestClient("https://api.openai.com/v1", "test-model")
			c.ConfigureReasoning(tc.effort, tc.summary)
			raw, err := c.responseRequest(llm.ChatRequest{})
			if err != nil {
				t.Fatal(err)
			}
			var body map[string]json.RawMessage
			if err := json.Unmarshal(raw, &body); err != nil {
				t.Fatal(err)
			}
			if tc.effort == "" && !tc.summary {
				if _, exists := body["reasoning"]; exists {
					t.Fatal("default must omit reasoning")
				}
				return
			}
			var reasoning map[string]string
			if err := json.Unmarshal(body["reasoning"], &reasoning); err != nil {
				t.Fatal(err)
			}
			if reasoning["effort"] != tc.effort {
				t.Fatalf("effort: %v", reasoning)
			}
			if tc.summary && reasoning["summary"] != "auto" {
				t.Fatalf("summary: %v", reasoning)
			}
			if !tc.summary && reasoning["summary"] != "" {
				t.Fatalf("unexpected summary: %v", reasoning)
			}
		})
	}
}
