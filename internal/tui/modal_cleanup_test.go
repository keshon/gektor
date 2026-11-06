package tui

import (
	tea "charm.land/bubbletea/v2"
	"github.com/charmbracelet/x/ansi"
	"github.com/keshon/gektor/internal/tui/components/popup"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestModalCleanupPreview(t *testing.T) {
	dir := os.Getenv("GEKTOR_TUI_PREVIEW_DIR")
	if dir == "" {
		t.Skip("optional rendered fixtures")
	}
	if err := os.MkdirAll(dir, 0700); err != nil {
		t.Fatal(err)
	}
	defer setTheme("dark")
	for _, theme := range []string{"light", "dark"} {
		m := sizeModelTo(t, testModel(), 100, 32)
		m.applyTheme(theme)
		m.openTheme()
		if err := os.WriteFile(filepath.Join(dir, "modal-theme-"+theme+".ansi"), []byte(m.View().Content), 0600); err != nil {
			t.Fatal(err)
		}
		m.focusForm(0)
		m.dialog.form.OpenPicker(m.settingsProps())
		if err := os.WriteFile(filepath.Join(dir, "modal-picker-"+theme+".ansi"), []byte(m.View().Content), 0600); err != nil {
			t.Fatal(err)
		}
		m.closeDialog()
		m.openDialog("Long prompt", []string{strings.Repeat("A long prompt with enough content to scroll.\n", 70)})
		if err := os.WriteFile(filepath.Join(dir, "modal-scroll-"+theme+".ansi"), []byte(m.View().Content), 0600); err != nil {
			t.Fatal(err)
		}
	}
}

func TestDialogScrollbarMatchesDrawnTrack(t *testing.T) {
	m := sizeModelTo(t, testModel(), 100, 30)
	m.openDialog("Long content", []string{strings.Repeat("line\n", 100)})
	l := m.layoutDialog()
	rows, _, _ := m.dialogRows()
	x := m.promptOffset() + max((l.width-popup.ModalWidth(l.width))/2, 0) + 2 + l.inner - 1
	y := paneTop + max((l.height-len(rows)-2)/2, 0) + 1 + len(l.rows)
	view := strings.Split(ansi.Strip(m.View().Content), "\n")
	if []rune(view[y])[x] != '█' {
		t.Fatal("thumb and hit target disagree", view[y])
	}
	m.handleMouse(tea.MouseClickMsg{X: x, Y: y, Button: tea.MouseLeft})
	m.handleMouse(tea.MouseMotionMsg{X: x, Y: y + l.capacity, Button: tea.MouseLeft})
	m.handleMouse(tea.MouseReleaseMsg{X: x, Y: y + l.capacity, Button: tea.MouseLeft})
	if m.dialog.offset != len(l.lines)-l.capacity || m.dialog.drag.active {
		t.Fatal(m.dialog.offset)
	}
}

func TestModalCloseButtonCancelsWithoutApplying(t *testing.T) {
	m := settingsModel(t)
	applied := false
	m.openForm("Edit", "", "Apply", []formField{{Key: "value", Label: "Value", Value: "old"}}, func(map[string]string) error { applied = true; return nil })
	m.focusForm(0)
	m.dialog.form.Editor.SetValue("unsaved")
	clickVisible(t, m, "×")
	if m.dialog != nil || applied {
		t.Fatal("close applied changes")
	}
	m.openDialog("Info", []string{"body"})
	clickVisible(t, m, "×")
	if m.dialog != nil {
		t.Fatal("info close missed")
	}
}
