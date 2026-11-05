package llm

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
)

// ReplayState is immutable once captured. Payload belongs to the protocol adapter.
// Old provider_state objects still load, but lack Version and cannot be replayed.
type ReplayState struct {
	Version          int             `json:"version"`
	Integration      string          `json:"integration"`
	Model            string          `json:"model"`
	Scope            string          `json:"scope"`
	Projection       string          `json:"projection"`
	Payload          json.RawMessage `json:"payload"`
	AdditionalTokens int             `json:"additional_tokens,omitempty"`
}

// ProjectionID ignores display/delivery metadata but includes rejected calls:
// filtering or repairing one must invalidate the original native output.
func ProjectionID(m Message) string {
	type call struct{ ID, Name, Arguments, InputError string }
	calls := make([]call, 0, len(m.ToolCalls))
	for _, c := range m.ToolCalls {
		calls = append(calls, call{c.ID, c.Name, string(c.Arguments), c.InputError})
	}
	raw, _ := json.Marshal(struct {
		Role    Role
		Content string
		Images  []string
		Calls   []call
	}{m.Role, m.Content, m.Images, calls})
	sum := sha256.Sum256(raw)
	return hex.EncodeToString(sum[:])
}

func (s *ReplayState) Matches(m Message) bool {
	return s != nil && s.Version == 1 && s.Projection == ProjectionID(m)
}

func CaptureReplay(m Message, integration, model, scope string, payload []byte, overhead int) *ReplayState {
	return &ReplayState{Version: 1, Integration: integration, Model: model, Scope: scope, Projection: ProjectionID(m), Payload: append(json.RawMessage(nil), payload...), AdditionalTokens: max(0, overhead)}
}
