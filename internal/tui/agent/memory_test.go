package agent

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"

	tea "charm.land/bubbletea/v2"
	"github.com/keshon/gektor/internal/memory"
	"github.com/keshon/gektor/pkg/tui/screen"
)

func isolateMemory(t *testing.T) {
	t.Helper()
	dir := t.TempDir()
	t.Setenv("HOME", dir)
	t.Setenv("USERPROFILE", dir)
}

func memoryModel(root string) *Model {
	m := testModel()
	m.state = stDone
	m.wsRoot = root
	return m
}

func savedMemories(t *testing.T, root string) []memory.Entry {
	t.Helper()
	store, err := openMemoryStore("", false)
	if err != nil {
		t.Fatal(err)
	}
	if store == nil {
		return nil
	}
	defer store.Close()
	entries, err := store.List(context.Background(), root, "")
	if err != nil {
		t.Fatal(err)
	}
	return entries
}

func TestMemorizePersistsAcrossModelsAndSeparatesProjects(t *testing.T) {
	isolateMemory(t)
	firstRoot, otherRoot := t.TempDir(), t.TempDir()
	first := memoryModel(firstRoot)
	first.command(`/memorize "Preserve single-row tool calls"`)
	first.command(`/memorize user "Prefer concise explanations"`)

	reopened := memoryModel(firstRoot)
	reopened.command("/memory")
	if len(reopened.memories.entries) != 2 {
		t.Fatalf("new session lost saved memories: %+v", reopened.memories.entries)
	}
	if reopened.memories.lastSaved.ID != "" {
		t.Fatal("new session inherited undo ownership")
	}
	other := memoryModel(otherRoot)
	other.command("/memory")
	if len(other.memories.entries) != 1 || other.memories.entries[0].Scope != memory.User || other.memories.entries[0].Text != "Prefer concise explanations" {
		t.Fatalf("project memory leaked or user memory missing: %+v", other.memories.entries)
	}
	for _, root := range []string{firstRoot, otherRoot} {
		files, err := os.ReadDir(root)
		if err != nil {
			t.Fatal(err)
		}
		if len(files) != 0 {
			t.Fatalf("memory created project files: %+v", files)
		}
	}
}

func TestMemoryBrowseDoesNotCreateStoreAndUserNeedsNoWorkspace(t *testing.T) {
	isolateMemory(t)
	m := memoryModel("")
	m.command("/memory")
	dir, err := memory.DefaultDir()
	if err != nil {
		t.Fatal(err)
	}
	_, err = os.Stat(dir)
	if !os.IsNotExist(err) {
		t.Fatalf("browsing created storage: %v", err)
	}
	m.command(`/memorize "Needs a project"`)
	if len(savedMemories(t, "")) != 0 {
		t.Fatal("default project save accepted absent workspace")
	}
	m.command(`/memorize user "Use plain language"`)
	if entries := savedMemories(t, ""); len(entries) != 1 || entries[0].Scope != memory.User {
		t.Fatalf("explicit user save without workspace: %+v", entries)
	}
}

func TestMemoryManagementEditsMovesAndDeletes(t *testing.T) {
	isolateMemory(t)
	root, other := t.TempDir(), t.TempDir()
	m := memoryModel(root)
	m.command(`/memorize "Use npm"`)
	m.command("/memory npm")
	if len(m.memories.entries) != 1 {
		t.Fatal("search did not return saved memory")
	}
	entry := m.memories.entries[0]
	m.HandleControl(screen.Event{Kind: screen.TableAction, ID: m.memories.list, Action: "memory-details", Row: entry.ID})
	m.HandleControl(screen.Event{Kind: screen.DialogActionRequested, ID: m.memories.detail, Action: "memory-edit"})
	err := m.applySubmission(m.form.dialog, map[string]string{"scope": "user", "text": "Use pnpm"})
	if err != nil {
		t.Fatal(err)
	}
	entries := savedMemories(t, other)
	if len(entries) != 1 || entries[0].Text != "Use pnpm" || entries[0].Scope != memory.User || entries[0].Revision <= entry.Revision {
		t.Fatalf("edit/move did not persist: %+v", entries)
	}
	m.command("/memory")
	m.memoryTableAction("memory-details", entries[0].ID)
	m.memoryDetailAction("memory-delete")
	err = m.applySubmission(m.form.dialog, nil)
	if err != nil {
		t.Fatal(err)
	}
	if len(savedMemories(t, root)) != 0 || len(savedMemories(t, other)) != 0 {
		t.Fatal("deleted memory remained visible")
	}
}

func TestMemoryUndoDoesNotDeleteEditedEntry(t *testing.T) {
	isolateMemory(t)
	root := t.TempDir()
	m := memoryModel(root)
	m.command(`/memorize "Initial fact"`)
	entry := m.memories.lastSaved
	store, err := openMemoryStore("", false)
	if err != nil {
		t.Fatal(err)
	}
	defer store.Close()
	_, err = store.Update(context.Background(), root, entry.ID, entry.Revision, memory.Input{Scope: memory.Project, Text: "Corrected fact"})
	if err != nil {
		t.Fatal(err)
	}
	m.command("/memory undo")
	entries := savedMemories(t, root)
	if len(entries) != 1 || entries[0].Text != "Corrected fact" {
		t.Fatal("undo deleted a subsequently edited memory")
	}
	m.command(`/memorize "Another fact"`)
	m.command("/memory undo")
	if len(savedMemories(t, root)) != 1 {
		t.Fatal("undo did not remove newly saved memory")
	}
}

func TestMemoryBusySaveDoesNotWriteAndCompletionAllowsText(t *testing.T) {
	isolateMemory(t)
	m := memoryModel(t.TempDir())
	m.state = stRunning
	m.command(`/memorize "Not yet"`)
	if len(savedMemories(t, m.wsRoot)) != 0 {
		t.Fatal("active run changed memory")
	}
	if !commandTakesArgument("/memorize") || !runsImmediate("/memorize user text") {
		t.Fatal("memory command cannot accept text or reject active writes")
	}
	m.state = stDone
	m.input.SetValue("/memor")
	m.testUpdate(tea.KeyPressMsg{Code: tea.KeyTab})
	if m.memories.lastSaved.ID != "" {
		t.Fatal("completion saved memory")
	}
}

func TestMemorizeArguments(t *testing.T) {
	for _, tc := range []struct {
		arg, text string
		scope     memory.Scope
	}{
		{`"user prefers short answers"`, "user prefers short answers", memory.Project},
		{`user "Prefer short answers"`, "Prefer short answers", memory.User},
		{`project 'Use pnpm'`, "Use pnpm", memory.Project},
		{`"Use \"quotes\" carefully"`, `"Use \"quotes\" carefully"`, memory.Project},
		{"user\nRemember this", "Remember this", memory.User},
	} {
		scope, text := memorizeArguments(tc.arg)
		if scope != tc.scope || text != tc.text {
			t.Fatalf("%q: %s %q", tc.arg, scope, text)
		}
	}
}

func TestMemoryReconnectMovedProject(t *testing.T) {
	isolateMemory(t)
	base := t.TempDir()
	old, moved := filepath.Join(base, "old"), filepath.Join(base, "moved")
	err := os.Mkdir(old, 0700)
	if err != nil {
		t.Fatal(err)
	}
	m := memoryModel(old)
	m.command(`/memorize "Keep the old project decision"`)
	err = os.Rename(old, moved)
	if err != nil {
		t.Fatal(err)
	}
	m = memoryModel(moved)
	m.command("/memory")
	if len(m.memories.entries) != 0 {
		t.Fatal("moved project attached automatically")
	}
	m.command(`/memory reconnect "` + old + `"`)
	if !strings.Contains(m.chrome.DialogSnapshot(m.form.dialog).Title, "Reconnect") {
		t.Fatal("reconnect did not open explicit form")
	}
	m.wsRoot = t.TempDir()
	err = m.applySubmission(m.form.dialog, map[string]string{"old": old})
	if err == nil {
		t.Fatal("stale reconnect accepted a different workspace")
	}
	m.wsRoot = moved
	err = m.applySubmission(m.form.dialog, map[string]string{"old": old})
	if err != nil {
		t.Fatal(err)
	}
	entries := savedMemories(t, moved)
	if len(entries) != 1 || entries[0].Text != "Keep the old project decision" {
		t.Fatalf("reconnect lost memory: %+v", entries)
	}
}

func TestMemoryFormsRejectWorkspaceChanges(t *testing.T) {
	for _, action := range []string{"memory-edit", "memory-delete"} {
		t.Run(action, func(t *testing.T) {
			isolateMemory(t)
			first, second := t.TempDir(), t.TempDir()
			m := memoryModel(first)
			m.command(`/memorize user "Keep this global preference"`)
			m.command("/memory")
			entry := m.memories.entries[0]
			m.memoryTableAction("memory-details", entry.ID)
			m.memoryDetailAction(action)
			m.wsRoot = second
			err := m.applySubmission(m.form.dialog, map[string]string{"scope": "project", "text": "Incorrect new project preference"})
			if err == nil || !strings.Contains(err.Error(), "workspace changed") {
				t.Fatalf("stale form accepted changed workspace: %v", err)
			}
			for _, root := range []string{first, second} {
				entries := savedMemories(t, root)
				if len(entries) != 1 || entries[0].Scope != memory.User || entries[0].Text != entry.Text || entries[0].Revision != entry.Revision {
					t.Fatalf("stale form changed memory: %+v", entries)
				}
			}
		})
	}
}

func TestMemoryDetailsRejectWorkspaceChanges(t *testing.T) {
	isolateMemory(t)
	m := memoryModel(t.TempDir())
	m.command(`/memorize user "Keep this global preference"`)
	m.command("/memory")
	m.memoryTableAction("memory-details", m.memories.entries[0].ID)
	m.wsRoot = t.TempDir()
	m.memoryDetailAction("memory-edit")
	if m.form.apply != nil || m.chrome.DialogSnapshot(m.chrome.DialogID()).Title != "Memory context changed" {
		t.Fatal("stale detail opened edit against a different workspace")
	}
}

func TestMemoryUIUsesConfiguredStoreDirectory(t *testing.T) {
	isolateMemory(t)
	root, directory := t.TempDir(), filepath.Join(t.TempDir(), "memory")
	m := memoryModel(root)
	m.baseEnv.MemoryDir = directory
	m.command(`/memorize "Use the configured store"`)
	reopened := memoryModel(root)
	reopened.baseEnv.MemoryDir = directory
	reopened.command("/memory")
	if len(reopened.memories.entries) != 1 || reopened.memories.entries[0].Text != "Use the configured store" {
		t.Fatal("UI ignored the recall store override")
	}
	if len(savedMemories(t, root)) != 0 {
		t.Fatal("UI wrote the default store despite an override")
	}
	reopened.memoryTableAction("memory-details", reopened.memories.entries[0].ID)
	reopened.memoryDetailAction("memory-edit")
	reopened.baseEnv.MemoryDir = t.TempDir()
	err := reopened.applySubmission(reopened.form.dialog, map[string]string{"scope": "user", "text": "Wrong store"})
	if err == nil {
		t.Fatal("stale edit accepted changed memory directory")
	}
}
