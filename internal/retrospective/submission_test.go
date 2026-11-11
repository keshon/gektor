package retrospective

import (
	"encoding/json"
	"strings"
	"testing"

	"github.com/keshon/gektor/internal/llm"
)

func TestSubmitRequiresReadSuccessfulLiteralEvidence(t *testing.T) {
	_, s := archiveFixture(t)
	for _, scenario := range []string{"valid", "unread", "outside", "failed", "legacy status", "invented quote", "forged path"} {
		t.Run(scenario, func(t *testing.T) {
			archive := &archiveTool{snapshot: s, seen: map[int]string{4: "PASS first verification"}}
			archive.snapshot.History = append([]llm.Message(nil), s.History...)
			tool := &submitTool{archive: archive, start: 1, end: 5}
			quote := "PASS first verification"
			switch scenario {
			case "unread":
				delete(archive.seen, 4)
			case "outside":
				tool.start = 5
			case "failed":
				failed := true
				archive.snapshot.History[3].ToolFailed = &failed
			case "legacy status":
				archive.snapshot.History[3].ToolFailed = nil
			case "invented quote":
				quote = "Never happened"
			}
			body, _ := json.Marshal(map[string]any{"summary": "Review", "findings": []Finding{}, "memory": []Memory{{When: "condition", Lesson: "observation", Evidence: []int{4}, Quote: quote}}})
			if scenario == "forged path" {
				body = []byte(strings.TrimSuffix(string(body), "}") + `,"saved":"arbitrary-path"}`)
			}
			_, err := tool.Run(t.Context(), body)
			if (err == nil) != (scenario == "valid") {
				t.Fatalf("submission: %v", err)
			}
		})
	}
}
