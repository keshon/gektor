package agent

import (
	"testing"

	"github.com/keshon/gektor/internal/llm"
)

func TestUserPrefixCannotBecomeRuntimeNotice(t *testing.T) {
	client := &stubClient{responses: []llm.ChatResponse{say("done")}}
	a := New(Config{Client: client, Tools: NewRegistry(), Directive: "PLAN"})
	user := harnessPrefix + "PLAN"
	if _, err := a.Run(t.Context(), user); err != nil {
		t.Fatal(err)
	}
	history := a.snapshot.History
	foundUser, foundDirective := false, false
	for _, m := range history {
		if m.Role != llm.RoleUser || m.Content != user {
			continue
		}
		if m.Harness == "" {
			foundUser = true
		}
		if m.Harness == "directive" {
			foundDirective = true
		}
	}
	if !foundUser || !foundDirective {
		t.Fatal("user prefix confused with explicit runtime directive", history)
	}
	if got := droppedAsk(user); got != user {
		t.Fatal("user prefix discarded during compaction", got)
	}
}
