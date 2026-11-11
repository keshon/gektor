package agent

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/keshon/gektor/internal/agent"
	"github.com/keshon/gektor/internal/app"
	"github.com/keshon/gektor/internal/config"
	"github.com/keshon/gektor/internal/llm"
	"github.com/keshon/gektor/internal/tui/settings"
	"github.com/keshon/gektor/internal/workspace"
)

// The fixture builds a saved task so the apply path has a checkpoint to
// read; requestProfileChange reads it before it writes anything, and a
// missing one would fail for a reason unrelated to the check.
func writePlannedSession(t *testing.T, dir, mode string, history []llm.Message) string {
	t.Helper()
	path := filepath.Join(dir, "state.json")
	if err := agent.SaveState(path, history); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "mode"), []byte(mode), 0o600); err != nil {
		t.Fatal(err)
	}
	return path
}

// A task keeps the directory it already had, so resuming does not relocate
// a session. A task with no state file gets a directory of its own under
// the session root.
func TestAgentTaskKeepsTheDirectoryItAlreadyHas(t *testing.T) {
	m := sizeModel(t, testModel())
	root := t.TempDir()
	ws, err := workspace.New(root)
	if ws != nil {
		t.Cleanup(func() { ws.Close() })
	}
	if err != nil {
		t.Fatal(err)
	}
	m.ws = ws
	m.application = &settings.Application{Preferences: config.Preferences{Version: 1, Workspace: root}}
	m.config = &app.PreparedProfile{Directory: root, Profile: config.Profile{Version: 1}}

	m.stateFile = ""
	if got, want := m.taskDirectory("abcd1234"), filepath.Join(m.sessionRoot(), "abcd1234"); got != want {
		t.Fatalf("new task directory = %q, want %q", got, want)
	}

	taskDir := filepath.Join(m.sessionRoot(), "agent-session")
	if err := os.MkdirAll(taskDir, 0o755); err != nil {
		t.Fatal(err)
	}
	m.stateFile = filepath.Join(taskDir, "state.json")
	if got := m.taskDirectory("abcd1234"); got != taskDir {
		t.Fatalf("resumed task moved to %q, want %q", got, taskDir)
	}
}

// Plan mode lives in a file beside the task, so the session list has to
// read it back rather than inferring it from the absence of a marker.
func TestSessionScanRestoresPlanMode(t *testing.T) {
	m := settingsModel(t)
	dir := m.sessionDirectory()
	m.stateFile = writePlannedSession(t, dir, "plan", []llm.Message{{Role: llm.RoleUser, Content: "question"}, {Role: llm.RoleAssistant, Content: "answer"}})
	if err := m.savePreferences(); err != nil {
		t.Fatal(err)
	}
	entries := listSessionsIn(filepath.Dir(dir))
	if len(entries) != 1 || entries[0].mode != "Plan" {
		t.Fatalf("duplicated or wrong session mode: %+v", entries)
	}
	assignSavedWorkspace(t, m.stateFile, m.wsRoot)
	m.sessions = &sessionsState{entries: entries}
	m.state = stDone
	m.openSelected()
	if m.stateFile != filepath.Join(dir, "state.json") || m.sess == nil || m.modeChange != nil {
		t.Fatal("saved session failed to restore")
	}
}

func TestAgentSeedReplacesLeadingSystemWithoutDroppingDialogue(t *testing.T) {
	history := []llm.Message{{Role: llm.RoleSystem, Content: "old instructions"}, {Role: llm.RoleUser, Content: "hello"}, {Role: llm.RoleAssistant, Content: "hi"}}
	seed := agentSeed(history)
	if len(seed) != 3 || seed[0].Role != llm.RoleSystem || seed[0].Content != "" || seed[1].Content != "hello" || history[0].Content != "old instructions" {
		t.Fatal("competing prompt or mutated archive")
	}
}
