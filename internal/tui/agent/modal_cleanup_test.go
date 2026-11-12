package agent

import (
	"strings"
	"testing"

	tea "charm.land/bubbletea/v2"
	"github.com/charmbracelet/x/ansi"

	"github.com/keshon/gektor/internal/tui/components/form"
	"github.com/keshon/gektor/internal/tui/components/popup"
	"github.com/keshon/gektor/internal/tui/screen"
)

func TestDialogScrollbarMatchesDrawnTrack(t *testing.T) {
	m := sizeModelTo(t, testModel(), 100, 30)
	m.testControls().Open("Long content", []string{strings.Repeat("line\n", 100)})
	l := m.testControls().LayoutDialog()
	rows, _, _ := m.testControls().DialogRows()
	x := m.promptOffset() + max((l.Width-popup.ModalWidth(l.Width))/2, 0) + 2 + l.Inner - 1
	y := paneTop + max((l.Height-len(rows)-2)/2, 0) + 1 + len(l.Rows)
	view := strings.Split(ansi.Strip(m.testView().Content), "\n")
	if []rune(view[y])[x] != '█' {
		t.Fatal("thumb and hit target disagree", view[y])
	}
	m.handleMouse(tea.MouseClickMsg{X: x, Y: y, Button: tea.MouseLeft})
	m.handleMouse(tea.MouseMotionMsg{X: x, Y: y + l.Capacity, Button: tea.MouseLeft})
	m.handleMouse(tea.MouseReleaseMsg{X: x, Y: y + l.Capacity, Button: tea.MouseLeft})
	if m.chrome.Dialog.Offset != len(l.Lines)-l.Capacity || m.chrome.Dialog.Drag.Active {
		t.Fatal(m.chrome.Dialog.Offset)
	}
}

func TestModalCloseButtonCancelsWithoutApplying(t *testing.T) {
	m := settingsModel(t)
	applied := false
	m.openForm(screen.Form{Title: "Edit", ApplyLabel: "Apply", Fields: []form.Field{{Key: "value", Label: "Value", Value: "old"}}}, restartProbe{ran: &applied})
	m.testControls().FocusForm(0)
	m.chrome.Dialog.Form.Editor.SetValue("unsaved")
	clickVisible(t, m, "×")
	if m.chrome.Dialog != nil || applied {
		t.Fatal("close applied changes")
	}
	m.testControls().Open("Info", []string{"body"})
	clickVisible(t, m, "×")
	if m.chrome.Dialog != nil {
		t.Fatal("info close missed")
	}
}
