package agent

import (
	"encoding/json"
	"strings"

	"github.com/keshon/gektor/internal/llm"
)

// PausedOnQuestion reports whether history ends on an assistant message
// with an unanswered question to a human — how to tell "interrupted by a
// question" from "interrupted by a crash". A nil registry falls back to
// matching a "question" argument, which is all a caller reading a saved
// session off disk can do: the tool set that produced it is long gone.
func PausedOnQuestion(reg *Registry, history []llm.Message) (callID string, question string, ok bool) {
	if len(history) == 0 {
		return "", "", false
	}
	last := history[len(history)-1]
	if last.Role != llm.RoleAssistant {
		return "", "", false
	}
	q := reg.Questioner()
	for _, tc := range last.ToolCalls {
		if q != nil {
			if tc.Name == q.Name() {
				return tc.ID, q.Question(tc.Arguments), true
			}
			continue
		}
		var args struct {
			Question string `json:"question"`
		}
		if json.Unmarshal(tc.Arguments, &args) == nil && strings.TrimSpace(args.Question) != "" {
			return tc.ID, strings.TrimSpace(args.Question), true
		}
	}
	return "", "", false
}
