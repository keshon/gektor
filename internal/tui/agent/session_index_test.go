package agent

import (
	"strings"
	"testing"

	"github.com/keshon/gektor/internal/llm"
)

func TestSearchFoldIsComputedOnceAtIndexTime(t *testing.T) {
	history := []llm.Message{{Role: llm.RoleUser, Content: "Нужный ПАРСЕР работает"}}
	entry := conversationText(history)
	if len(entry) != 1 {
		t.Fatalf("indexed %d messages", len(entry))
	}
	if entry[0].lower == entry[0].Text {
		t.Fatalf("search text was not folded: %q", entry[0].lower)
	}
	if !strings.Contains(entry[0].lower, "парсер") {
		t.Fatalf("fold lost the match: %q", entry[0].lower)
	}
}
