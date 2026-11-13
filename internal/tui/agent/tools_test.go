package agent

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"reflect"
	"testing"

	"github.com/keshon/gektor/internal/conversation"
	"github.com/keshon/gektor/internal/harness"
)

func TestDraftToolsetsBecomeInitialConversationState(t *testing.T) {
	m := settingsModel(t)
	m.stateFile = ""
	m.sess = nil
	for _, loaded := range []bool{true, false, true} {
		change, err := m.ToolsetChange("native_web", loaded)
		if err != nil {
			t.Fatal(err)
		}
		states, err := change.Apply(context.Background())
		if err != nil {
			t.Fatal(err)
		}
		m.AcceptToolsetState(states)
		m.saveDraft()
		current, available, err := m.ToolsetState()
		if err != nil || !available || len(current) != 1 || current[0].Unloaded == loaded {
			t.Fatalf("draft selection: %v, %v, %v", current, available, err)
		}
	}
	want := m.drafts[""].tools.Capabilities
	m.newSession = func(task, path string, images []string) (*harness.Session, error) {
		snapshot, err := conversation.Load(path)
		if err != nil {
			t.Fatal(err)
		}
		gotJSON, err := json.Marshal(snapshot.Capabilities)
		if err != nil {
			t.Fatal(err)
		}
		wantJSON, err := json.Marshal(want)
		if err != nil {
			t.Fatal(err)
		}
		if !bytes.Equal(gotJSON, wantJSON) || len(snapshot.History) != 0 {
			t.Fatalf("initial snapshot: %+v", snapshot)
		}
		return nil, fmt.Errorf("stop before running model")
	}
	err := m.startFreshPrompt("first message", "first message", nil)
	if err == nil {
		t.Fatal("expected construction stop")
	}
	if !reflect.DeepEqual(m.drafts[""].tools.Capabilities, want) {
		t.Fatal("failed construction discarded selection")
	}
	// Opening an existing conversation must read its own state.
	m.stateFile = settingsModel(t).stateFile
	states, _, err := m.ToolsetState()
	if err != nil {
		t.Fatal(err)
	}
	if len(states) != 0 {
		t.Fatal("draft selections leaked into existing conversation")
	}
}
