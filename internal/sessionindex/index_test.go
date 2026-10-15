package sessionindex

import (
	"testing"

	"github.com/keshon/tars/internal/llm"
	"github.com/keshon/tars/internal/session"
)

func TestSearchUsesSourceMessagesAndExcludesPrivateText(t *testing.T) {
	history := []llm.Message{
		{Role: llm.RoleSystem, Content: "system secret"},
		{Role: llm.RoleUser, Content: "question" + session.ReferenceStart + "file secret" + session.ReferenceEnd},
		{Role: llm.RoleAssistant, Content: "<think>private secret</think> answer", Reasoning: "reasoning secret"},
		{Role: llm.RoleUser, Content: "[harness] operational secret"},
		{Role: llm.RoleTool, Content: "tool secret"},
	}
	texts := ConversationText(history)
	if len(texts) != 2 || texts[0].Text != "question" || texts[0].Message != 1 || texts[1].Text != "answer" || texts[1].Message != 2 {
		t.Fatalf("index: %+v", texts)
	}
}
