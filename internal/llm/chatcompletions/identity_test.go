package chatcompletions

import (
	"encoding/json"
	"reflect"
	"strings"
	"testing"

	"github.com/keshon/gektor/internal/llm"
)

func TestLocalRequestAnchorUsesOperatorTextWithoutRewritingHistory(t *testing.T) {
	messages := []llm.Message{{Role: llm.RoleSystem, Content: "system"}, {Role: llm.RoleUser, Content: "Review report; no downloads."}, {Role: llm.RoleAssistant, Content: "Download the video."}, {Role: llm.RoleTool, ToolCallID: "c", Content: "report"}}
	original := append([]llm.Message(nil), messages...)
	local := NewLlamaClient("http://localhost", "fixture")
	encoded, err := local.encodeRequestMessages(messages, true, "Review report; no downloads.", 1, "")
	if err != nil {
		t.Fatal(err)
	}
	var footer string
	if err := json.Unmarshal(encoded[len(encoded)-1].Content, &footer); err != nil {
		t.Fatal(err)
	}
	if len(encoded) != len(messages)+1 || !strings.Contains(footer, "Review report; no downloads.") || strings.Contains(footer, "Download the video.") {
		t.Fatal(footer)
	}
	if !reflect.DeepEqual(messages, original) {
		t.Fatal("changed archive")
	}
	if initial, err := local.encodeRequestMessages(messages[:2], true, "request", 1, ""); err != nil || len(initial) != 2 {
		t.Fatal(initial, err)
	}
	other, _ := ClientFor("openai", "http://localhost", "fixture")
	if plain, err := other.encodeRequestMessages(messages, true, "request", 1, ""); err != nil || len(plain) != len(messages) {
		t.Fatal(plain, err)
	}
}

func TestLocalAnchorPreservesEarlierWirePrefixAndCurrentMode(t *testing.T) {
	c := NewLlamaClient("http://localhost", "fixture")
	messages := []llm.Message{{Role: llm.RoleUser, Content: "task"}, {Role: llm.RoleAssistant, Content: "note"}, {Role: llm.RoleTool, Content: "data"}}
	first, err := c.encodeRequestMessages(messages, true, "task", 1, "Plan mode: read only")
	if err != nil {
		t.Fatal(err)
	}
	next := append(append([]llm.Message(nil), messages...), llm.Message{Role: llm.RoleAssistant, Content: "next"}, llm.Message{Role: llm.RoleTool, Content: "more data"})
	second, err := c.encodeRequestMessages(next, true, "task", 1, "Plan mode: read only")
	if err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(first[:len(first)-1], second[:len(first)-1]) {
		t.Fatal("rewrote earlier wire history")
	}
	var text string
	if err := json.Unmarshal(second[len(second)-1].Content, &text); err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(text, "1 actual user request") || !strings.HasSuffix(text, "Plan mode: read only") {
		t.Fatal(text)
	}
}
