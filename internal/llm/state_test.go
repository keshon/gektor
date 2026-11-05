package llm

import "testing"

func TestReplayOverheadCountsOnlyUnchangedProjection(t *testing.T) {
	m := Message{Role: RoleAssistant, Content: "answer", ToolCalls: []ToolCall{{ID: "c", Name: "read", Arguments: []byte(`{}`)}}}
	base := EstimatePromptTokens(ChatRequest{Messages: []Message{m}})
	m.ProviderState = CaptureReplay(m, "route", "model", "scope", []byte(`[]`), 25)
	if got := EstimatePromptTokens(ChatRequest{Messages: []Message{m}}); got != base+25 {
		t.Fatal(got, base)
	}
	m.Request = 17
	if !m.ProviderState.Matches(m) {
		t.Fatal("delivery metadata invalidates replay")
	}
	m.Content = "edited"
	state := m.ProviderState
	m.ProviderState = nil
	expected := EstimatePromptTokens(ChatRequest{Messages: []Message{m}})
	m.ProviderState = state
	if got := EstimatePromptTokens(ChatRequest{Messages: []Message{m}}); got != expected {
		t.Fatal("invalid projection counted", got, expected)
	}
}
