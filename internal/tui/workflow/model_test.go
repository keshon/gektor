package workflow

import (
	"errors"
	"strings"
	"testing"

	"github.com/keshon/gektor/internal/tui/components/pane"
)

func TestWorkflowUpdatesReplaceNoticeSeverity(t *testing.T) {
	m := workflowTestModel(t)
	w := m.workflows
	m.workflows.acceptResult(workflowResult{err: errors.New("cannot save recipe")})
	if w.noticeLevel != pane.NoticeError || w.notice != "cannot save recipe" || m.chrome.Dialog == nil {
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
