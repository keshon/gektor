package agent

import (
	"path/filepath"
	"strings"
	"testing"

	"github.com/keshon/gektor/internal/llm"
)

// Resumed image turns render their marker from history.
func TestRenderHistory_ShowsImageMarkers(t *testing.T) {
	history := []llm.Message{
		{Role: llm.RoleUser, Content: "look", Images: []string{filepath.Join("d", "shot.png")}},
	}
	blocks := renderHistory(history)
	if len(blocks) != 1 || !strings.Contains(blocks[0].text, "[image: shot.png]") {
		t.Fatalf("blocks = %+v", blocks)
	}
}

func TestLoadedHistoryDoesNotInventTimestamps(t *testing.T) {
	for _, b := range renderHistory(userHistory("saved")) {
		if !b.at.IsZero() {
			t.Fatal("history was stamped with reopen time")
		}
	}
	if userBlock("live").at.IsZero() {
		t.Fatal("live messages lost their timestamps")
	}
}

func TestHistoryMatchTranslationSkipsReasoningAndToolCards(t *testing.T) {
	history := []llm.Message{
		{Role: llm.RoleSystem, Content: "system"},
		{Role: llm.RoleUser, Content: "question"},
		{Role: llm.RoleAssistant, ToolCalls: []llm.ToolCall{{ID: "lookup", Name: "read_file"}}},
		{Role: llm.RoleTool, ToolCallID: "lookup", Content: "result"},
		{Role: llm.RoleAssistant, Content: "<think>private</think> answer"},
	}
	blocks, boundary := renderHistoryAt(history)
	index := historyMatchBlock(blocks, boundary, 4)
	if index >= len(blocks) || blocks[index].role != roleAnswer || blocks[index].text != "answer" {
		t.Fatalf("search landed on wrong block: %d %+v", index, blocks)
	}
}

// Opening from a search hit points at one message. It must not render the
// whole prefix again to count it.
func TestHistoryMatchReusesTheRenderItAlreadyHas(t *testing.T) {
	history := userHistory("one", "two", "three", "four")
	blocks, boundary := renderHistoryAt(history)

	for message := range history {
		got := historyMatchBlock(blocks, boundary, message)
		if got < 0 || got >= len(blocks) {
			t.Fatalf("message %d mapped outside the transcript: %d", message, got)
		}
		// A message must not point before its own blocks start.
		if got < boundary[message] {
			t.Fatalf("message %d mapped to %d, before its own block %d", message, got, boundary[message])
		}
	}
	if got := historyMatchBlock(blocks, boundary, len(history)); got != len(blocks) {
		t.Fatalf("end of history mapped to %d, want %d", got, len(blocks))
	}
}
