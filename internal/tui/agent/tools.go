package agent

import (
	"context"
	"fmt"
	"os"

	"github.com/keshon/gektor/internal/conversation"
	"github.com/keshon/gektor/internal/roles"
)

// ToolsetChange captures an idle conversation before background loading begins.
type ToolsetChange struct {
	env      roles.Env
	snapshot conversation.Snapshot
	path, id string
	loaded   bool
}

func (m *Model) ToolsetState() ([]conversation.Capability, bool, error) {
	if m.stateFile == "" {
		return m.drafts[""].tools.Capabilities, true, nil
	}
	snapshot, err := conversation.Load(m.stateFile)
	if os.IsNotExist(err) {
		return nil, false, nil
	}
	if err != nil {
		return nil, false, err
	}
	return snapshot.Capabilities, true, nil
}

func (m *Model) ToolsetChange(id string, loaded bool) (ToolsetChange, error) {
	if m.state != stDone || m.evaluation != nil {
		return ToolsetChange{}, fmt.Errorf("wait for the conversation to become idle before changing its tools")
	}
	snapshot := conversation.New(nil)
	if m.stateFile == "" {
		snapshot = m.drafts[""].tools
	} else {
		var err error
		snapshot, err = conversation.Load(m.stateFile)
		if err != nil {
			return ToolsetChange{}, err
		}
	}
	return ToolsetChange{env: m.sessionEnv(), snapshot: snapshot, path: m.stateFile, id: id, loaded: loaded}, nil
}

// Apply changes only catalog metadata; conversation history and shared connections stay intact.
func (change ToolsetChange) Apply(ctx context.Context) (conversation.Snapshot, error) {
	snapshot, err := change.env.SelectToolset(ctx, change.snapshot, change.id, change.loaded)
	if err != nil {
		return conversation.Snapshot{}, err
	}
	if change.path != "" {
		err = snapshot.Save(change.path)
	}
	return snapshot, err
}

// AcceptToolsetState runs on the UI loop after a successful background change.
func (m *Model) AcceptToolsetState(snapshot conversation.Snapshot) {
	if m.stateFile != "" {
		return
	}
	if m.drafts == nil {
		m.drafts = make(Drafts)
	}
	d := m.drafts[""]
	d.tools = snapshot
	m.drafts[""] = d
}
