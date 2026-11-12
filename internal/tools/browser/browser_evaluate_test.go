package browser

import (
	"encoding/json"
	"strings"
	"testing"

	"github.com/keshon/gektor/internal/agent"
)

func TestBrowserObserveChecksActualResultNotWrapperOrCode(t *testing.T) {
	var function string
	remote := &browserFixture{name: "mcp__browser__browser_evaluate", remote: "browser_evaluate", run: func(args json.RawMessage) (string, error) {
		var input struct {
			Function string `json:"function"`
		}
		if err := json.Unmarshal(args, &input); err != nil {
			return "", err
		}
		function = input.Function
		page := map[string]any{"url": "https://fixture.test", "title": "Article", "scope": "article", "text": "Observed fact: 42", "offset": 0, "total_characters": 17, "expectation_met": false, "check": "literal_visible_body_text"}
		encoded, _ := json.Marshal(page)
		return "### Result\n" + string(encoded) + "\n### Ran Playwright code\nExpected words: Saved successfully", nil
	}}
	observe := &BrowserObserve{Tools: agent.NewRegistry(remote)}
	out, err := observe.Run(t.Context(), json.RawMessage(`{"expect_text":"Saved successfully"}`))
	if err == nil || !strings.Contains(out, `"expectation_met":false`) || strings.Contains(out, "Expected words:") || !strings.Contains(function, "document.body.innerText") {
		t.Fatal(out, err)
	}
	out, err = observe.Run(t.Context(), json.RawMessage(`{}`))
	if err != nil || !strings.Contains(out, "Observed fact: 42") || strings.Contains(out, "Ran Playwright") {
		t.Fatal(out, err)
	}
	if _, err := observe.Run(t.Context(), json.RawMessage(`{"mode":"visual"}`)); err == nil {
		t.Fatal("text model accepted visual observation")
	}
	if _, err := observe.Run(t.Context(), json.RawMessage(`{"mode":"controls","offset":1}`)); err == nil {
		t.Fatal("accepted invalid paging")
	}
}
