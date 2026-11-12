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
	m.testKey(tea.KeyPressMsg{Code: 'q', Mod: tea.ModAlt})
	if m.chrome.Dialog == nil || m.chrome.Dialog.Table == nil {
		t.Fatal("queue did not open")
	}
	m.testKey(tea.KeyPressMsg{Code: tea.KeyDown})
	if m.chrome.Dialog.Table.Model.Selected() != fmt.Sprint(before[1].ID) {
		t.Fatal("wrong selected message")
	}
	m.testKey(tea.KeyPressMsg{Code: 'e', Mod: tea.ModCtrl})
	if m.chrome.Dialog.Form == nil {
		t.Fatal("editor did not open")
	}
	if len(m.chrome.Dialog.Form.Fields) != 2 || !m.chrome.Dialog.Form.Fields[1].ReadOnly || m.chrome.Dialog.Form.Fields[1].Value != "shot.png" {
		t.Fatal("editor has no attachment evidence")
	}
	m.chrome.Dialog.Form.Editor.SetValue("edited middle")
	m.applyTestForm()
	after := m.sess.PendingInputs()
	if len(after) != 3 || after[0].ID != before[0].ID || after[1].ID != before[1].ID || after[1].Message.Content != "edited middle" || after[1].Message.Images[0] != "shot.png" || after[2].ID != before[2].ID {
		t.Fatalf("wrong edited queue: %+v", after)
	}
	if m.input.Value() != "unfinished draft" || len(m.draftImages) != 1 || m.draftImages[0] != "draft.png" {
		t.Fatal("editor changed composer draft")
	}
	m.openInputQueue()
	m.testKey(tea.KeyPressMsg{Code: 'e', Mod: tea.ModCtrl})
	m.chrome.Dialog.Form.Editor.SetValue("cancelled edit")
	m.testKey(tea.KeyPressMsg{Code: tea.KeyEscape})
	if m.sess.PendingInputs()[0].Message.Content != before[0].Message.Content {
		t.Fatal("cancel applied changes")
	}
}
func TestQueueRefreshKeepsIdentityAndDoesNotInventReceipt(t *testing.T) {
	m := queuedModel(t)
	m.openInputQueue()
	m.testKey(tea.KeyPressMsg{Code: tea.KeyEnd})
	selected := m.chrome.Dialog.Table.Model.Selected()
	m.sess.RecallInput() // Recall removes it without delivering it to the agent.
	props := m.testControls().TableProps()
	if m.chrome.Dialog.Table.Model.Selected() != selected {
		t.Fatal("refresh moved selection to a different message")
	}
	if !strings.Contains(m.chrome.Dialog.Table.Model.Rows[2].Detail, "No longer queued") || strings.Contains(m.chrome.Dialog.Table.Description, "received") {
		t.Fatal("removal was mislabeled as receipt")
	}
	if !props.Actions[0].Enabled || props.Actions[1].Enabled || props.Actions[2].Enabled {
		t.Fatal("received input is editable")
	}
	m.testKey(tea.KeyPressMsg{Code: tea.KeyDelete})
	if m.sess.PendingInputCount() != 2 {
		t.Fatal("delete affected another message")
	}
	m.testKey(tea.KeyPressMsg{Code: tea.KeyEnter})
	if m.chrome.Dialog.Title != "Message no longer queued" || !strings.Contains(strings.Join(m.chrome.Dialog.Lines, " "), "shot.png") {
		t.Fatal("read lost message or image name")
	}
}
func TestQueueStaleEditorKeepsTextAndCannotChangeAnotherMessage(t *testing.T) {
	m := queuedModel(t)
	m.openInputQueue()
	m.testKey(tea.KeyPressMsg{Code: tea.KeyEnd})
	m.testKey(tea.KeyPressMsg{Code: 'e', Mod: tea.ModCtrl})
	m.chrome.Dialog.Form.Editor.SetValue("keep this edit")
	m.sess.RecallInput()
	m.sess.Submit("replacement")
	m.applyTestForm()
	if m.chrome.Dialog == nil || m.chrome.Dialog.Form == nil || m.chrome.Dialog.Form.Editor.Value() != "keep this edit" || m.chrome.Dialog.Form.Error == "" {
		t.Fatal("failed save lost edit or explanation")
	}
	if m.sess.PendingInputs()[2].Message.Content != "replacement" {
		t.Fatal("stale save changed replacement")
	}
	m.sess = &harness.Session{}
	m.applyTestForm()
	if !strings.Contains(m.chrome.Dialog.Form.Error, "session has changed") {
		t.Fatal("editor crossed sessions")
	}
}
func TestQueueRemoveOnlySelectedAndLegacyIdentity(t *testing.T) {
	m := queuedModel(t)
	before := m.sess.PendingInputs()
	m.openInputQueue()
	m.testKey(tea.KeyPressMsg{Code: tea.KeyDown})
	m.testKey(tea.KeyPressMsg{Code: tea.KeyDelete})
	after := m.sess.PendingInputs()
	if len(after) != 2 || after[0].ID != before[0].ID || after[1].ID != before[2].ID {
		t.Fatal("wrong message removed")
	}
	if m.input.Value() != "unfinished draft" {
		t.Fatal("remove changed draft")
	}
	m.chrome.Close()
	m.syncControls()
	m.queued = "next run"
	m.queuedVersion++
	m.openInputQueue()
	m.testKey(tea.KeyPressMsg{Code: tea.KeyEnd})
	m.testKey(tea.KeyPressMsg{Code: 'e', Mod: tea.ModCtrl})
	m.chrome.Dialog.Form.Editor.SetValue("stale")
	m.queued = "replacement"
	m.queuedVersion++
	m.applyTestForm()
	if m.queued != "replacement" || m.chrome.Dialog.Form.Error == "" {
		t.Fatal("stale fallback save succeeded")
	}
}
func TestComposerQueueLayoutAndMouse(t *testing.T) {
	for _, width := range []int{40, 80, 120} {
		m := queuedModel(t)
		m.width(tea.WindowSizeMsg{Width: width, Height: 24})
		rows := strings.Split(ansi.Strip(m.testView().Content), "\n")
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
		if m.testView().Cursor == nil || m.testView().Cursor.Y != m.termH-m.footerRows()-m.input.Height() {
			t.Fatal("composer caret moved")
		}
		layout := m.panelGeometry(m.mainWidth(), len(m.composerRows()), false, true)
		m.handleMouse(tea.MouseClickMsg{X: layout.Footer.X, Y: 1 + layout.Footer.Y, Button: tea.MouseLeft})
		if m.chrome.Dialog == nil || m.chrome.Dialog.Table == nil {
			t.Fatal("summary count did not open queue")
		}
		props := m.testControls().TableProps()
		l := m.chrome.Dialog.Table.Model.Layout(props)
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
			if err := os.WriteFile(filepath.Join(dir, fmt.Sprintf("%s-%d.ansi", name, width)), []byte(m.testView().Content), 0600); err != nil {
				t.Fatal(err)
			}
		}
		save("composer")
		m.openInputQueue()
		save("queue")
		m.testKey(tea.KeyPressMsg{Code: 'e', Mod: tea.ModCtrl})
		save("queue-editor")
	}
}

func TestQueueEditorSurvivesAgentGate(t *testing.T) {
	m := queuedModel(t)
	m.openInputQueue()
	m.testKey(tea.KeyPressMsg{Code: 'e', Mod: tea.ModCtrl})
	m.chrome.Dialog.Form.Editor.SetValue("unsaved edit")
	m.acceptGateEvent(harness.Event{Name: "gate", Fields: map[string]any{"kind": string(agent.SuspendPermission), "prompt": "Allow this tool?"}})
	if m.chrome.Dialog != nil || m.suspendedInputEdit == nil || m.input.Value() != "unfinished draft" {
		t.Fatal("gate lost editor or main draft")
	}
	// Resolve the permission gate, then return to the queue editor.
	m.resolveGate("n")
	m.openInputQueue()
	if m.chrome.Dialog == nil || m.chrome.Dialog.Form == nil || m.chrome.Dialog.Form.Editor.Value() != "unsaved edit" {
		t.Fatal("editor was not restored")
	}
	m.applyTestForm()
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
			rows := strings.Split(m.testView().Content, "\n")
			if len(rows) != size[1] {
				t.Fatal("queue height", theme, size)
			}
			for _, row := range rows {
				if lipgloss.Width(row) != size[0] {
					t.Fatal("queue width", theme, size)
				}
			}
			m.testKey(tea.KeyPressMsg{Code: tea.KeyEscape})
			if m.chrome.Dialog != nil || m.input.Value() != "unfinished draft" {
				t.Fatal("queue close lost draft", theme, size)
			}
		}
	}
}

func TestQueueShortcutLeavesTextareaDeleteBindingAlone(t *testing.T) {
	m := queuedModel(t)
	m.input.SetValue("keep remove")
	m.input.CursorStart()
	m.testKey(tea.KeyPressMsg{Code: 'k', Mod: tea.ModCtrl})
	if m.chrome.Dialog != nil || m.input.Value() != "" || m.sess.PendingInputCount() != 3 {
		t.Fatal("CTRL+K stopped being a text edit")
	}
	m.testKey(tea.KeyPressMsg{Code: 'q', Mod: tea.ModAlt})
	if m.chrome.Dialog == nil || m.chrome.Dialog.Table == nil {
		t.Fatal("ALT+Q did not open queue")
	}
}

func TestRunningPlaceholderClearsWhenTurnFinishes(t *testing.T) {
	m := runningModel(t)
	m.fitBottom()
	if m.input.Placeholder != "Send another message…" {
		t.Fatal("running placeholder missing")
	}
	m.testUpdate(doneMsg{answer: "done"})
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
			if !strings.Contains(ansi.Strip(m.testControls().ActionBar()), "ALT+Q Queue") {
				t.Fatal("empty inbox hid Queue", width, m.testControls().ActionBar())
			}
		}
		check()
		m.testKey(tea.KeyPressMsg{Code: 'q', Mod: tea.ModAlt})
		if m.chrome.Dialog == nil || !strings.Contains(ansi.Strip(m.testControls().DialogView()), "No messages waiting") {
			t.Fatal("empty queue has no explanation")
		}
		m.testKey(tea.KeyPressMsg{Code: tea.KeyEscape})
		check()
		m.sess.Submit("temporary")
		m.openInputQueue()
		m.sess.RecallInput()
		m.testControls().TableProps()
		m.testKey(tea.KeyPressMsg{Code: tea.KeyEscape})
		check()
		m.testKey(tea.KeyPressMsg{Code: 'q', Mod: tea.ModAlt})
		if !strings.Contains(ansi.Strip(m.testControls().DialogView()), "No messages waiting") {
			t.Fatal("reopening displayed stale queued data")
		}
	}
}
func TestQueueIsReachableFromRunMenuWhenIdle(t *testing.T) {
	m := runningModel(t)
	m.state = stDone
	found := false
	for _, item := range m.menuItems("execution") {
		if item.Command == "message-queue" {
			found = item.Enabled
		}
	}
	if !found || menuShortcut("message-queue") != "Alt+Q" {
		t.Fatal("Run menu has no queue entry point")
	}
	m.activateMenuAction("message-queue")
	if m.chrome.Dialog == nil || m.chrome.Dialog.Table == nil {
		t.Fatal("menu did not open queue")
	}
}
func TestQueueRetainsKnownReceiptWithoutAllowingEdits(t *testing.T) {
	m := runningModel(t)
	q := &inputQueue{session: m.sess, stateFile: m.stateFile, view: m.testControls().NewTable(), entries: map[string]queueEntry{"1": {QueuedInput: agent.QueuedInput{ID: 1, Received: true}}}, order: []string{"1"}}
	m.showInputQueue(q)
	p := m.testControls().TableProps()
	if p.Actions[1].Enabled || p.Actions[2].Enabled || !strings.Contains(q.view.Rows[0].Detail, "Received by agent") {
		t.Fatal("known receipt was lost or became editable")
	}
}

func TestQueueGateRetainsItsOwnFocusFrame(t *testing.T) {
	m := queuedModel(t)
	m.openInputQueue()
	m.testKey(tea.KeyPressMsg{Code: 'e', Mod: tea.ModCtrl})
	if len(m.overlays) != 1 || !m.overlays[0].controls {
		t.Fatal("queue editor has no controls focus frame")
	}
	m.acceptGateEvent(harness.Event{Name: "gate", Fields: map[string]any{"kind": string(agent.SuspendPermission), "prompt": "Allow?"}})
	m.syncControls()
	if len(m.overlays) != 1 || m.overlays[0].controls {
		t.Fatal("dialog teardown removed the gate's focus frame")
	}
	m.resolveGate("n")
	if len(m.overlays) != 0 || !m.input.Focused() {
		t.Fatal("resolved gate retained an overlay or lost composer focus")
	}
}

func TestSuspendedQueueEditRetainsSubmissionSeparately(t *testing.T) {
	m := queuedModel(t)
	m.openInputQueue()
	m.testKey(tea.KeyPressMsg{Code: 'e', Mod: tea.ModCtrl})
	original := m.chrome.Dialog
	m.chrome.Dialog.Form.Editor.SetValue("keep this draft")
	m.acceptGateEvent(harness.Event{Name: "gate", Fields: map[string]any{"kind": string(agent.SuspendPermission), "prompt": "Allow?"}})
	m.syncControls()
	if m.form.apply != nil || m.suspendedInputEdit == nil || m.suspendedInputEdit.form.dialog != original || m.suspendedInputEdit.form.apply == nil {
		t.Fatal("suspended submission was lost or remained active")
	}

	m.resolveGate("n")
	m.openInputQueue()
	m.syncControls()
	if m.form.dialog != original || m.form.apply == nil || m.suspendedInputEdit != nil {
		t.Fatal("resume did not transfer the pending submission back")
	}
	m.chrome.Close()
	m.syncControls()
	if m.form.apply != nil || m.sess.PendingInputs()[0].Message.Content == "keep this draft" {
		t.Fatal("dismissal retained or applied the suspended submission")
	}
}
