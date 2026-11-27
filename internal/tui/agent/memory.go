package agent

import (
	"context"
	"fmt"
	"path/filepath"
	"strings"
	"unicode"

	"github.com/keshon/gektor/internal/memory"
	"github.com/keshon/gektor/pkg/tui/components/form"
	"github.com/keshon/gektor/pkg/tui/components/table"
	"github.com/keshon/gektor/pkg/tui/screen"
)

type memoryView struct {
	list, detail screen.DialogID
	query        string
	workspace    string
	directory    string
	entries      []memory.Entry
	selected     memory.Entry
	lastSaved    memory.Entry
}

func memoryText(text string) string {
	text = strings.TrimSpace(text)
	if len(text) >= 2 && (text[0] == '"' || text[0] == '\'') && text[len(text)-1] == text[0] && !strings.ContainsRune(text[1:len(text)-1], rune(text[0])) {
		return text[1 : len(text)-1]
	}
	return text
}

func memorizeArguments(arg string) (memory.Scope, string) {
	arg = strings.TrimSpace(arg)
	first, rest := arg, ""
	if i := strings.IndexFunc(arg, unicode.IsSpace); i >= 0 {
		first, rest = arg[:i], arg[i:]
	}
	if first == string(memory.User) || first == string(memory.Project) {
		return memory.Scope(first), memoryText(rest)
	}
	return memory.Project, memoryText(arg)
}

func (m *Model) memoryWorkspace() string {
	if m.ws != nil {
		return m.ws.Root()
	}
	return m.wsRoot
}

func (m *Model) memoryContext() context.Context {
	if m.base != nil {
		return m.base
	}
	return context.Background()
}

func (m *Model) memoryIdle() bool {
	if m.state != stDone || m.evaluation != nil {
		m.appendBlock(markerBlock("Finish the current run or evaluation before changing memory."))
		return false
	}
	return true
}

func openMemoryStore(dir string, create bool) (*memory.Store, error) {
	if dir == "" {
		var err error
		dir, err = memory.DefaultDir()
		if err != nil {
			return nil, err
		}
	}
	if create {
		return memory.Open(dir)
	}
	return memory.OpenExisting(dir)
}

func memoryScopeLabel(entry memory.Entry) string {
	if entry.Scope == memory.User {
		return "User · all projects"
	}
	return "Project · " + filepath.Base(entry.ProjectPath)
}

func (m *Model) memorize(arg string) {
	if !m.memoryIdle() {
		return
	}
	scope, text := memorizeArguments(arg)
	if strings.TrimSpace(text) == "" {
		m.appendBlock(errorBlock("Usage: /memorize [user|project] \"fact or preference\""))
		return
	}
	root := m.memoryWorkspace()
	if scope == memory.Project && root == "" {
		m.appendBlock(errorBlock("Select a workspace or use /memorize user \"fact or preference\"."))
		return
	}
	store, err := openMemoryStore(m.baseEnv.MemoryDir, true)
	if err != nil {
		m.appendBlock(errorBlock(err.Error()))
		return
	}
	defer store.Close()
	entry, created, err := store.Add(m.memoryContext(), root, memory.Input{Scope: scope, Text: text, SourceSession: m.stateFile})
	if err != nil {
		m.appendBlock(errorBlock(err.Error()))
		return
	}
	if !created {
		m.appendBlock(markerBlock("Already remembered in " + memoryScopeLabel(entry) + ". No duplicate saved."))
		return
	}
	m.memories.lastSaved = entry
	m.appendBlock(markerBlock("Remembered in " + memoryScopeLabel(entry) + ". /memory undo removes this save. Available on the next request."))
}

func (m *Model) memoryCommand(arg string) {
	arg = strings.TrimSpace(arg)
	if arg == "undo" {
		m.undoMemory()
		return
	}
	first, rest, _ := strings.Cut(arg, " ")
	if first == "reconnect" {
		m.reconnectMemory(memoryText(rest))
		return
	}
	m.openMemory(memoryText(arg))
}

func (m *Model) openMemory(query string) {
	store, err := openMemoryStore(m.baseEnv.MemoryDir, false)
	if err != nil {
		m.chrome.Open("Cannot read memory", []string{err.Error()})
		return
	}
	entries := []memory.Entry{}
	snapshot := memory.Snapshot{}
	if store != nil {
		defer store.Close()
		entries, err = store.List(m.memoryContext(), m.memoryWorkspace(), query)
		if err == nil {
			snapshot, err = store.Snapshot(m.memoryContext(), m.memoryWorkspace(), memory.DefaultBudget)
		}
		if err != nil {
			m.chrome.Open("Cannot read memory", []string{err.Error()})
			return
		}
	}
	included := make(map[string]bool, len(snapshot.Entries))
	for _, entry := range snapshot.Entries {
		included[entry.ID] = true
	}
	rows := make([]table.Row, 0, len(entries))
	for _, entry := range entries {
		recall := "Stored"
		if included[entry.ID] {
			recall = "Included"
		}
		rows = append(rows, table.Row{ID: entry.ID, Cells: []string{memoryScopeLabel(entry), recall, strings.Join(strings.Fields(entry.Text), " ")}})
	}
	description := "Local user and current-project memory. Included means selected for the next request; current instructions take precedence."
	if snapshot.Omitted > 0 {
		description += fmt.Sprintf(" %d entries remain stored outside the recall budget.", snapshot.Omitted)
	}
	if query != "" {
		description += " Search: " + query
	}
	m.chrome.PresentDialog(screen.Dialog{Title: "Memory", Table: &screen.Table{
		Rows: rows, Description: description,
		Columns:      []table.Column{{Label: "Scope", Width: 24}, {Label: "Recall", Width: 10}, {Label: "Memory"}},
		EmptyMessage: "No matching memories. Save one with /memorize [user|project] \"text\".",
		Actions: []table.Action{
			{ID: "memory-details", Label: "Details", Key: "enter", Enabled: len(rows) > 0},
			{ID: "memory-search", Label: "Search", Key: "/", Enabled: true},
			{ID: "memory-refresh", Label: "Refresh", Key: "r", Enabled: true},
			{ID: "memory-reconnect", Label: "Reconnect…", Key: "c", Enabled: m.memoryWorkspace() != "" && m.state == stDone},
			{ID: "close", Label: "Close", Key: "esc", Enabled: true},
		},
	}})
	m.memories.list, m.memories.query, m.memories.entries = m.chrome.DialogID(), query, entries
	m.memories.workspace, m.memories.directory = m.memoryWorkspace(), m.baseEnv.MemoryDir
}

func (m *Model) memoryTableAction(action, id string) {
	if m.memories.workspace != m.memoryWorkspace() || m.memories.directory != m.baseEnv.MemoryDir {
		m.openMemory(m.memories.query)
		return
	}
	switch action {
	case "memory-refresh":
		m.openMemory(m.memories.query)
	case "memory-search":
		m.openForm(screen.Form{Title: "Search memory", ApplyLabel: "Search", Fields: []form.Field{{Key: "query", Label: "Text", Value: m.memories.query}}}, func(values map[string]string) error {
			m.openMemory(strings.TrimSpace(values["query"]))
			return nil
		})
	case "memory-reconnect":
		m.reconnectMemory("")
	case "memory-details":
		for _, entry := range m.memories.entries {
			if entry.ID == id {
				m.inspectMemory(entry)
				return
			}
		}
	}
}

func (m *Model) reconnectMemory(oldPath string) {
	if !m.memoryIdle() {
		return
	}
	root := m.memoryWorkspace()
	directory := m.baseEnv.MemoryDir
	if root == "" {
		m.appendBlock(errorBlock("Select the moved project's workspace before reconnecting memory."))
		return
	}
	m.openForm(screen.Form{Title: "Reconnect moved project", ApplyLabel: "Reconnect", Hint: "Reconnect memory from a folder that no longer exists to " + root + ". Existing project memories are never merged.", Fields: []form.Field{{Key: "old", Label: "Previous folder", Value: oldPath}}}, func(values map[string]string) error {
		if !m.memoryIdle() || m.memoryWorkspace() != root || m.baseEnv.MemoryDir != directory {
			return fmt.Errorf("workspace changed; reopen memory in the moved project")
		}
		store, err := openMemoryStore(directory, false)
		if err != nil {
			return err
		}
		if store == nil {
			return memory.ErrNotFound
		}
		defer store.Close()
		err = store.Reconnect(m.memoryContext(), strings.TrimSpace(values["old"]), root)
		if err != nil {
			return err
		}
		m.openMemory("")
		return nil
	})
}

func (m *Model) inspectMemory(entry memory.Entry) {
	lines := []string{memoryScopeLabel(entry), "", entry.Text, "", "Saved: " + entry.Created.Local().Format("2006-01-02 15:04"), "Updated: " + entry.Updated.Local().Format("2006-01-02 15:04"), fmt.Sprintf("Revision: %d", entry.Revision)}
	if entry.ProjectPath != "" {
		lines = append(lines, "Project: "+entry.ProjectPath)
	}
	if entry.SourceSession != "" {
		lines = append(lines, "Source session: "+entry.SourceSession)
	}
	lines = append(lines, "", "Deleting memory excludes it from future recall; the original conversation remains.")
	actions := []screen.DialogAction{{ID: "memory-back", Key: 'b', Label: "back"}}
	if m.state == stDone && m.evaluation == nil {
		actions = append(actions, screen.DialogAction{ID: "memory-edit", Key: 'e', Label: "edit / move"}, screen.DialogAction{ID: "memory-delete", Key: 'd', Label: "delete"})
	}
	m.chrome.PresentDialog(screen.Dialog{Title: "Memory details", Lines: lines, Actions: actions})
	m.memories.detail, m.memories.selected = m.chrome.DialogID(), entry
	m.memories.workspace, m.memories.directory = m.memoryWorkspace(), m.baseEnv.MemoryDir
}

func (m *Model) memoryDetailAction(action string) {
	entry := m.memories.selected
	if action == "memory-back" {
		m.openMemory(m.memories.query)
		return
	}
	if !m.memoryIdle() {
		return
	}
	root, directory := m.memories.workspace, m.memories.directory
	if root != m.memoryWorkspace() || directory != m.baseEnv.MemoryDir {
		m.chrome.Open("Memory context changed", []string{"Workspace changed; reopen memory before editing or deleting an entry."})
		return
	}
	switch action {
	case "memory-edit":
		m.openForm(screen.Form{Title: "Edit memory", ApplyLabel: "Save", Hint: "Project stays local to this workspace; User applies across projects.", Fields: []form.Field{
			{Key: "scope", Label: "Scope", Value: string(entry.Scope)},
			{Key: "text", Label: "Memory", Value: entry.Text, Multiline: true},
		}, Choices: map[string][]string{"scope": {string(memory.Project), string(memory.User)}}}, func(values map[string]string) error {
			if !m.memoryIdle() {
				return fmt.Errorf("finish the current run before changing memory")
			}
			if root != m.memoryWorkspace() || directory != m.baseEnv.MemoryDir {
				return fmt.Errorf("workspace changed; reopen memory before editing")
			}
			store, err := openMemoryStore(directory, true)
			if err != nil {
				return err
			}
			defer store.Close()
			_, err = store.Update(m.memoryContext(), root, entry.ID, entry.Revision, memory.Input{Scope: memory.Scope(values["scope"]), Text: values["text"], SourceSession: entry.SourceSession, SourceMessage: entry.SourceMessage})
			if err != nil {
				return err
			}
			m.openMemory(m.memories.query)
			return nil
		})
	case "memory-delete":
		m.openForm(screen.Form{Title: "Delete memory", ApplyLabel: "Delete", Destructive: true, StartOnCancel: true, Hint: memoryScopeLabel(entry) + "\n" + entry.Text}, func(map[string]string) error {
			if !m.memoryIdle() {
				return fmt.Errorf("finish the current run before changing memory")
			}
			if root != m.memoryWorkspace() || directory != m.baseEnv.MemoryDir {
				return fmt.Errorf("workspace changed; reopen memory before deleting")
			}
			err := m.deleteMemory(entry)
			if err != nil {
				return err
			}
			m.openMemory(m.memories.query)
			return nil
		})
	}
}

func (m *Model) deleteMemory(entry memory.Entry) error {
	store, err := openMemoryStore(m.baseEnv.MemoryDir, false)
	if err != nil {
		return err
	}
	if store == nil {
		return memory.ErrNotFound
	}
	defer store.Close()
	return store.Delete(m.memoryContext(), m.memoryWorkspace(), entry.ID, entry.Revision)
}

func (m *Model) undoMemory() {
	if !m.memoryIdle() {
		return
	}
	entry := m.memories.lastSaved
	if entry.ID == "" {
		m.appendBlock(markerBlock("No new memory save to undo."))
		return
	}
	err := m.deleteMemory(entry)
	if err != nil {
		m.appendBlock(errorBlock(err.Error()))
		return
	}
	m.memories.lastSaved = memory.Entry{}
	m.appendBlock(markerBlock("Removed the last saved memory from future recall."))
}
