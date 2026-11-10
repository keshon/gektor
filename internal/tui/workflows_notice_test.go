package tui

import (
	"errors"
	"strings"
	"testing"

	"github.com/charmbracelet/x/ansi"

	"github.com/keshon/gektor/internal/config"
	"github.com/keshon/gektor/internal/tui/components/pane"
)

func TestWorkflowUpdatesReplaceNoticeSeverity(t *testing.T) {
	m := workflowTestModel(t)
	w := m.workflows
	m.workflows.acceptResult(workflowResult{err: errors.New("cannot save recipe")})
	if w.noticeLevel != pane.NoticeError || w.notice != "cannot save recipe" || m.dialog == nil {
		t.Fatal("failure lost its error notice or details dialog")
	}
	m.closeDialog()
	m.workflows.acceptResult(workflowResult{})
	if w.noticeLevel != pane.NoticeInfo || w.notice != "Ready" {
		t.Fatal("normal update inherited the previous failure")
	}
	for _, item := range []struct {
		status string
		level  pane.NoticeLevel
	}{{"completed", pane.NoticeSuccess}, {"failed", pane.NoticeError}, {"cancelled", pane.NoticeWarning}, {"running", pane.NoticeInfo}} {
		m.workflows.acceptResult(workflowResult{text: `{"status":"` + item.status + `"}`})
		if w.noticeLevel != item.level || !strings.Contains(w.notice, item.status) {
			t.Fatalf("run status %s has the wrong notice: %s (%d)", item.status, w.notice, w.noticeLevel)
		}
	}
}

func TestWorkflowErrorFooterFitsEveryTheme(t *testing.T) {
	defer setTheme("dark")
	m := workflowTestModel(t)
	m.workflows.inspectorOpen = false
	for _, theme := range config.ThemeNames() {
		m.applyTheme(theme)
		for _, size := range [][2]int{{40, 20}, {80, 24}, {120, 40}, {160, 50}} {
			sizeModelTo(t, m, size[0], size[1])
			m.workflows.acceptResult(workflowResult{text: `{"status":"failed"}`})
			view := ansi.Strip(m.View().Content)
			if !strings.Contains(view, "Error: Run: failed") {
				t.Fatalf("%s %v: workflow failure is missing", theme, size)
			}
			rows := strings.Split(view, "\n")
			if len(rows) != size[1] {
				t.Fatal("notice added height to the screen")
			}
			for _, row := range rows {
				if ansi.StringWidth(row) > size[0] {
					t.Fatal("notice overflowed the screen")
				}
			}
		}
	}
}
