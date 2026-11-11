package agent

import (
	"path/filepath"
	"reflect"
	"strings"
	"testing"

	"github.com/keshon/gektor/internal/conversation"
	"github.com/keshon/gektor/internal/llm"
)

func TestCompactActionUsesSavedContextAndPreservesArchive(t *testing.T) {
	m := sizeModel(t, testModel())
	m.state = stDone
	m.stateFile = filepath.Join(t.TempDir(), "state.json")
	history := []llm.Message{{Role: llm.RoleSystem, Content: "sys"}, {Role: llm.RoleUser, Content: "task"}}
	for i := 0; i < 30; i++ {
		history = append(history, llm.Message{Role: llm.RoleAssistant, ToolCalls: []llm.ToolCall{{ID: "c", Name: "read_file"}}}, llm.Message{Role: llm.RoleTool, ToolCallID: "c", Content: "contents"})
	}
	if err := conversation.WriteSnapshot(m.stateFile, history); err != nil {
		t.Fatal(err)
	}
	beforeArchive, err := conversation.ReadHistory(m.stateFile)
	if err != nil {
		t.Fatal(err)
	}
	m.transcript.blocks = renderHistory(history)
	m.compactNow()
	context, err := conversation.ReadContext(m.stateFile)
	if err != nil {
		t.Fatal(err)
	}
	if len(context) >= len(history) {
		t.Fatal("compaction did not reduce saved model context")
	}
	archive, err := conversation.ReadHistory(m.stateFile)
	if err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(archive, beforeArchive) {
		t.Fatal("compaction changed archived messages")
	}
	if m.tokens <= 0 || !m.transcript.Follow || !strings.Contains(m.testView().Content, "compacted:") {
		t.Fatal("compaction did not refresh the Agent context and transcript")
	}
}
