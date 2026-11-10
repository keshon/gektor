package tui

import (
	"os"
	"strings"

	"github.com/keshon/gektor/internal/conversation"
	"github.com/keshon/gektor/internal/harness"
	"github.com/keshon/gektor/internal/llm"
)

type childTranscript struct {
	history        []llm.Message
	live, thinking string
	phase          string
	output         map[string]string
	revision       uint64
	loaded         bool
	notice         string
}

type childLoadedMsg struct {
	parent, callID string
	revision       uint64
	snapshot       conversation.Snapshot
	err            error
}

func (m *model) child(id string) *childTranscript {
	if m.inspector.children == nil {
		m.inspector.children = map[string]*childTranscript{}
	}
	if m.inspector.children[id] == nil {
		m.inspector.children[id] = &childTranscript{output: map[string]string{}}
	}
	return m.inspector.children[id]
}
func (m *model) acceptChildHistory(msg childLoadedMsg) {
	if msg.parent != m.stateFile {
		return
	}
	child := m.child(msg.callID)
	if child.revision != msg.revision {
		return
	}
	if msg.err != nil {
		if os.IsNotExist(msg.err) {
			child.notice = "Child history is not available for this call."
		} else {
			child.notice = "Cannot load child history: " + msg.err.Error()
		}
	} else {
		child.history = msg.snapshot.History
		child.loaded = true
		child.notice = ""
	}
	m.refreshInspector()
}
func (m *model) acceptChildEvent(ev harness.Event) {
	id, _ := ev.Fields["call_id"].(string)
	kind, _ := ev.Fields["kind"].(string)
	fields, _ := ev.Fields["fields"].(map[string]any)
	if id == "" || fields == nil {
		return
	}
	child := m.child(id)
	child.revision++
	child.loaded = true
	child.notice = ""
	text, _ := fields["text"].(string)
	switch kind {
	case "progress":
		child.phase, _ = fields["detail"].(string)
		if child.phase == "" {
			child.phase, _ = fields["phase"].(string)
		}
	case "notice":
		child.history = append(child.history, llm.Message{Role: llm.RoleUser, Content: "[harness] " + text})
	case "message":
		msg, ok := fields["message"].(llm.Message)
		if !ok {
			return
		}
		child.history = append(child.history, msg)
		if msg.Role == llm.RoleAssistant {
			child.live = ""
			child.thinking = ""
		}
		if msg.Role == llm.RoleTool {
			delete(child.output, msg.ToolCallID)
		}
	case "delta":
		child.live = boundedInspectorText(child.live + text)
	case "reasoning":
		child.thinking = boundedInspectorText(child.thinking + text)
	case "tool_output":
		id, _ := fields["tool_call_id"].(string)
		child.output[id] = boundedInspectorText(child.output[id] + text)
	}
}
func boundedInspectorText(text string) string {
	const limit = 256 << 10
	if len(text) > limit {
		return strings.ToValidUTF8(text[:limit], "") + "\n[Live preview limit reached; completed history follows.]"
	}
	return text
}
