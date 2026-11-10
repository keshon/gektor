package agent

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"unicode/utf8"

	tea "charm.land/bubbletea/v2"
	"charm.land/lipgloss/v2"
	"github.com/charmbracelet/x/ansi"

	"github.com/keshon/gektor/internal/agent"
	"github.com/keshon/gektor/internal/config"
	"github.com/keshon/gektor/internal/harness"
)

func queuedModel(t *testing.T) *Model {
	t.Helper()
	m := runningModel(t)
	m.sess = &harness.Session{}
	for _, text := range []string{"Compare these screenshots", "Check the inspector spacing", "Check the light theme too"} {
		if err := m.sess.Submit(text, "shot.png"); err != nil {
			t.Fatal(err)
		}
	}
	m.input.SetValue("unfinished draft")
	m.draftImages = []string{"draft.png"}
	m.fitInput()
	m.fitBottom()
	return m
}
func TestQueueEditKeepsDraftAttachmentsAndOrder(t *testing.T) {
	m := queuedModel(t)
	before := m.sess.PendingInputs()
	m.handleKey(tea.KeyPressMsg{Code: 'q', Mod: tea.ModAlt})
	if m.dialog == nil || m.dialog.table == nil {
		t.Fatal("queue did not open")
	}
	m.handleKey(tea.KeyPressMsg{Code: tea.KeyDown})
	if m.dialog.table.model.Selected() != fmt.Sprint(before[1].ID) {
		t.Fatal("wrong selected message")
	}
	m.handleKey(tea.KeyPressMsg{Code: 'e', Mod: tea.ModCtrl})
	if m.dialog.form == nil {
		t.Fatal("editor did not open")
	}
	if len(m.dialog.form.Fields) != 2 || !m.dialog.form.Fields[1].ReadOnly || m.dialog.form.Fields[1].Value != "shot.png" {
		t.Fatal("editor has no attachment evidence")
	}
	m.dialog.form.Editor.SetValue("edited middle")
	m.applyForm()
	after := m.sess.PendingInputs()
	if len(after) != 3 || after[0].ID != before[0].ID || after[1].ID != before[1].ID || after[1].Message.Content != "edited middle" || after[1].Message.Images[0] != "shot.png" || after[2].ID != before[2].ID {
		t.Fatalf("wrong edited queue: %+v", after)
	}
	if m.input.Value() != "unfinished draft" || len(m.draftImages) != 1 || m.draftImages[0] != "draft.png" {
		t.Fatal("editor changed composer draft")
	}
	m.openInputQueue()
	m.handleKey(tea.KeyPressMsg{Code: 'e', Mod: tea.ModCtrl})
	m.dialog.form.Editor.SetValue("cancelled edit")
	m.handleKey(tea.KeyPressMsg{Code: tea.KeyEscape})
	if m.sess.PendingInputs()[0].Message.Content != before[0].Message.Content {
		t.Fatal("cancel applied changes")
	}
}
func TestQueueRefreshKeepsIdentityAndDoesNotInventReceipt(t *testing.T) {
	m := queuedModel(t)
	m.openInputQueue()
	m.handleKey(tea.KeyPressMsg{Code: tea.KeyEnd})
	selected := m.dialog.table.model.Selected()
	m.sess.RecallInput() // Recall removes it without delivering it to the agent.
	props := m.tableProps()
	if m.dialog.table.model.Selected() != selected {
		t.Fatal("refresh moved selection to a different message")
	}
	if !strings.Contains(m.dialog.table.model.Rows[2].Detail, "No longer queued") || strings.Contains(m.dialog.table.description, "received") {
		t.Fatal("removal was mislabeled as receipt")
	}
	if !props.Actions[0].Enabled || props.Actions[1].Enabled || props.Actions[2].Enabled {
		t.Fatal("received input is editable")
	}
	m.handleKey(tea.KeyPressMsg{Code: tea.KeyDelete})
	if m.sess.PendingInputCount() != 2 {
		t.Fatal("delete affected another message")
	}
	m.handleKey(tea.KeyPressMsg{Code: tea.KeyEnter})
	if m.dialog.title != "Message no longer queued" || !strings.Contains(strings.Join(m.dialog.lines, " "), "shot.png") {
		t.Fatal("read lost message or image name")
	}
}
func TestQueueStaleEditorKeepsTextAndCannotChangeAnotherMessage(t *testing.T) {
	m := queuedModel(t)
	m.openInputQueue()
	m.handleKey(tea.KeyPressMsg{Code: tea.KeyEnd})
	m.handleKey(tea.KeyPressMsg{Code: 'e', Mod: tea.ModCtrl})
	m.dialog.form.Editor.SetValue("keep this edit")
	m.sess.RecallInput()
	m.sess.Submit("replacement")
	m.applyForm()
	if m.dialog == nil || m.dialog.form == nil || m.dialog.form.Editor.Value() != "keep this edit" || m.dialog.form.Error == "" {
		t.Fatal("failed save lost edit or explanation")
	}
	if m.sess.PendingInputs()[2].Message.Content != "replacement" {
		t.Fatal("stale save changed replacement")
	}
	m.sess = &harness.Session{}
	m.applyForm()
	if !strings.Contains(m.dialog.form.Error, "session has changed") {
		t.Fatal("editor crossed sessions")
	}
}
func TestQueueRemoveOnlySelectedAndLegacyIdentity(t *testing.T) {
	m := queuedModel(t)
	before := m.sess.PendingInputs()
	m.openInputQueue()
	m.handleKey(tea.KeyPressMsg{Code: tea.KeyDown})
	m.handleKey(tea.KeyPressMsg{Code: tea.KeyDelete})
	after := m.sess.PendingInputs()
	if len(after) != 2 || after[0].ID != before[0].ID || after[1].ID != before[2].ID {
		t.Fatal("wrong message removed")
	}
	if m.input.Value() != "unfinished draft" {
		t.Fatal("remove changed draft")
	}
	m.closeDialog()
	m.queued = "next run"
	m.queuedVersion++
	m.openInputQueue()
	m.handleKey(tea.KeyPressMsg{Code: tea.KeyEnd})
	m.handleKey(tea.KeyPressMsg{Code: 'e', Mod: tea.ModCtrl})
	m.dialog.form.Editor.SetValue("stale")
	m.queued = "replacement"
	m.queuedVersion++
	m.applyForm()
	if m.queued != "replacement" || m.dialog.form.Error == "" {
		t.Fatal("stale fallback save succeeded")
	}
}
func TestComposerQueueLayoutAndMouse(t *testing.T) {
	for _, width := range []int{40, 80, 120} {
		m := queuedModel(t)
		m.width(tea.WindowSizeMsg{Width: width, Height: 24})
		rows := strings.Split(ansi.Strip(m.View().Content), "\n")
		if len(rows) != 24 {
			t.Fatal("wrong screen height")
		}
		for _, row := range rows {
			if lipgloss.Width(row) != width || !utf8.ValidString(row) {
				t.Fatal("invalid screen row", row)
			}
		}
		view := strings.Join(rows, "\n")
		if strings.Count(view, "Queued 3") != 1 || strings.Contains(view, "edit latest") || strings.Contains(view, "Pending ") {
			t.Fatal("crowded composer", view)
		}
		if !strings.Contains(rows[len(rows)-1], "ALT+Q Queue") {
			t.Fatal("queue action clipped", rows[len(rows)-1])
		}
		if m.View().Cursor == nil || m.View().Cursor.Y != m.termH-m.footerRows()-m.input.Height() {
			t.Fatal("composer caret moved")
		}
		layout := m.panelGeometry(m.mainWidth(), len(m.composerRows()), false, true)
		m.handleMouse(tea.MouseClickMsg{X: layout.Footer.X, Y: 1 + layout.Footer.Y, Button: tea.MouseLeft})
		if m.dialog == nil || m.dialog.table == nil {
			t.Fatal("summary count did not open queue")
		}
		props := m.tableProps()
		l := m.dialog.table.model.Layout(props)
		contents := ansi.Strip(strings.Join(l.Rows, "\n"))
		for _, key := range []string{"ENTER Read", "CTRL+E Edit", "DEL Remove", "ESC Close"} {
			if !strings.Contains(contents, key) {
				t.Fatal("queue action clipped", width, key, contents)
			}
		}
		if l.Capacity < 1 || strings.Contains(contents, "Filter:") {
			t.Fatal("queue list used table chrome")
		}
	}
}

// Optional previews are produced by the real renderer, without a backend.
func TestQueuePreview(t *testing.T) {
	dir := os.Getenv("GEKTOR_TUI_PREVIEW_DIR")
	if dir == "" {
		t.Skip("preview export opt-in")
	}
	if err := os.MkdirAll(dir, 0700); err != nil {
		t.Fatal(err)
	}
	for _, width := range []int{40, 80} {
		m := queuedModel(t)
		m.width(tea.WindowSizeMsg{Width: width, Height: 24})
		save := func(name string) {
			t.Helper()
			if err := os.WriteFile(filepath.Join(dir, fmt.Sprintf("%s-%d.ansi", name, width)), []byte(m.View().Content), 0600); err != nil {
				t.Fatal(err)
			}
		}
		save("composer")
		m.openInputQueue()
		save("queue")
		m.handleKey(tea.KeyPressMsg{Code: 'e', Mod: tea.ModCtrl})
		save("queue-editor")
	}
}

func TestQueueEditorSurvivesAgentGate(t *testing.T) {
	m := queuedModel(t)
	m.openInputQueue()
	m.handleKey(tea.KeyPressMsg{Code: 'e', Mod: tea.ModCtrl})
	m.dialog.form.Editor.SetValue("unsaved edit")
	m.acceptGateEvent(harness.Event{Name: "gate", Fields: map[string]any{"kind": string(agent.SuspendPermission), "prompt": "Allow this tool?"}})
	if m.dialog != nil || m.suspendedInputEdit == nil || m.input.Value() != "unfinished draft" {
		t.Fatal("gate lost editor or main draft")
	}
	// Resolve the permission gate, then return to the queue editor.
	m.resolveGate("n")
	m.openInputQueue()
	if m.dialog == nil || m.dialog.form == nil || m.dialog.form.Editor.Value() != "unsaved edit" {
		t.Fatal("editor was not restored")
	}
	m.applyForm()
	if m.sess.PendingInputs()[0].Message.Content != "unsaved edit" {
		t.Fatal("restored editor could not save")
	}
}
func TestQueueAcrossThemesAndSmallTerminals(t *testing.T) {
	defer setTheme("dark")
	for _, theme := range config.ThemeNames() {
		for _, size := range [][2]int{{30, 12}, {40, 20}, {80, 24}, {120, 34}} {
			m := queuedModel(t)
			m.applyTheme(theme)
			m.width(tea.WindowSizeMsg{Width: size[0], Height: size[1]})
			m.openInputQueue()
			rows := strings.Split(m.View().Content, "\n")
			if len(rows) != size[1] {
				t.Fatal("queue height", theme, size)
			}
			for _, row := range rows {
				if lipgloss.Width(row) != size[0] {
					t.Fatal("queue width", theme, size)
				}
			}
			m.handleKey(tea.KeyPressMsg{Code: tea.KeyEscape})
			if m.dialog != nil || m.input.Value() != "unfinished draft" {
				t.Fatal("queue close lost draft", theme, size)
			}
		}
	}
}

func TestQueueShortcutLeavesTextareaDeleteBindingAlone(t *testing.T) {
	m := queuedModel(t)
	m.input.SetValue("keep remove")
	m.input.CursorStart()
	m.handleKey(tea.KeyPressMsg{Code: 'k', Mod: tea.ModCtrl})
	if m.dialog != nil || m.input.Value() != "" || m.sess.PendingInputCount() != 3 {
		t.Fatal("CTRL+K stopped being a text edit")
	}
	m.handleKey(tea.KeyPressMsg{Code: 'q', Mod: tea.ModAlt})
	if m.dialog == nil || m.dialog.table == nil {
		t.Fatal("ALT+Q did not open queue")
	}
}

func TestRunningPlaceholderClearsWhenTurnFinishes(t *testing.T) {
	m := runningModel(t)
	m.fitBottom()
	if m.input.Placeholder != "Send another message…" {
		t.Fatal("running placeholder missing")
	}
	m.Update(doneMsg{answer: "done"})
	if m.input.Placeholder != "" {
		t.Fatal("idle composer kept running placeholder")
	}
}

func TestQueueEntryPointSurvivesEmptyInboxAndModalClose(t *testing.T) {
	for _, width := range []int{40, 80, 120} {
		m := runningModel(t)
		m.sess = &harness.Session{}
		m.width(tea.WindowSizeMsg{Width: width, Height: 24})
		check := func() {
			t.Helper()
			if !strings.Contains(ansi.Strip(m.actionBar()), "ALT+Q Queue") {
				t.Fatal("empty inbox hid Queue", width, m.actionBar())
			}
		}
		check()
		m.handleKey(tea.KeyPressMsg{Code: 'q', Mod: tea.ModAlt})
		if m.dialog == nil || !strings.Contains(ansi.Strip(m.dialogView()), "No messages waiting") {
			t.Fatal("empty queue has no explanation")
		}
		m.handleKey(tea.KeyPressMsg{Code: tea.KeyEscape})
		check()
		m.sess.Submit("temporary")
		m.openInputQueue()
		m.sess.RecallInput()
		m.tableProps()
		m.handleKey(tea.KeyPressMsg{Code: tea.KeyEscape})
		check()
		m.handleKey(tea.KeyPressMsg{Code: 'q', Mod: tea.ModAlt})
		if !strings.Contains(ansi.Strip(m.dialogView()), "No messages waiting") {
			t.Fatal("reopening displayed stale queued data")
		}
	}
}
func TestQueueIsReachableFromRunMenuWhenIdle(t *testing.T) {
	m := runningModel(t)
	m.state = stDone
	found := false
	for _, item := range m.menuItems("execution") {
		if item.command == "message-queue" {
			found = item.enabled
		}
	}
	if !found || menuShortcut("message-queue") != "Alt+Q" {
		t.Fatal("Run menu has no queue entry point")
	}
	m.activateMenuAction("message-queue")
	if m.dialog == nil || m.dialog.table == nil {
		t.Fatal("menu did not open queue")
	}
}
func TestQueueRetainsKnownReceiptWithoutAllowingEdits(t *testing.T) {
	m := runningModel(t)
	q := &inputQueue{session: m.sess, stateFile: m.stateFile, view: m.newTable(), entries: map[string]queueEntry{"1": {QueuedInput: agent.QueuedInput{ID: 1, Received: true}}}, order: []string{"1"}}
	m.showInputQueue(q)
	p := m.tableProps()
	if p.Actions[1].Enabled || p.Actions[2].Enabled || !strings.Contains(q.view.Rows[0].Detail, "Received by agent") {
		t.Fatal("known receipt was lost or became editable")
	}
}
