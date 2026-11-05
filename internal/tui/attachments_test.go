package tui

import (
	tea "charm.land/bubbletea/v2"
	"errors"
	"github.com/charmbracelet/x/ansi"
	"github.com/keshon/gektor/internal/run"
	"image"
	"image/png"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func attachmentFixture(t *testing.T) string {
	t.Helper()
	path := filepath.Join(t.TempDir(), "shot.png")
	f, e := os.Create(path)
	if e != nil {
		t.Fatal(e)
	}
	if e = png.Encode(f, image.NewRGBA(image.Rect(0, 0, 2, 2))); e != nil {
		t.Fatal(e)
	}
	f.Close()
	return path
}
func TestClipboardDraftAccumulatesAndRemovesImages(t *testing.T) {
	m := sizeModel(t, testModel())
	m.state = stDone
	one, two := attachmentFixture(t), attachmentFixture(t)
	m.acceptClipboard(clipboardMsg{path: one})
	m.acceptClipboard(clipboardMsg{path: two})
	m.input.SetValue("compare")
	text, images, err := m.draftAttachments(m.input.Value())
	if err != nil || text != "compare" || len(images) != 2 || images[0] != one || images[1] != two {
		t.Fatalf("%s %v %v", text, images, err)
	}
	if !strings.Contains(ansi.Strip(m.View().Content), "Alt+N remove") {
		t.Fatal("attachments missing from composer")
	}
	m.detachImage("1")
	if len(m.draftImages) != 1 || m.draftImages[0] != two {
		t.Fatal("wrong image removed")
	}
	m.detachImage("0")
	if len(m.draftImages) != 1 {
		t.Fatal("invalid detach changed attachments")
	}
}
func TestClipboardDraftSurvivesFailureAndSessionSwitch(t *testing.T) {
	m := sizeModel(t, testModel())
	m.state = stDone
	m.draftImages = []string{attachmentFixture(t)}
	m.input.SetValue("look")
	m.newSession = func(string, string, []string) (*run.Session, error) { return nil, errors.New("unavailable") }
	m.followUp()
	if m.input.Value() != "look" || len(m.draftImages) != 1 {
		t.Fatal("failure lost draft")
	}
	m.stateFile = "one"
	m.saveDraft()
	m.stateFile = "two"
	m.restoreDraft()
	if len(m.draftImages) != 0 {
		t.Fatal("images leaked into another session")
	}
	m.stateFile = "one"
	m.restoreDraft()
	if len(m.draftImages) != 1 || m.input.Value() != "look" {
		t.Fatal("draft not restored")
	}
}
func TestClipboardCaptureRejectsStaleOrUnfocusedResult(t *testing.T) {
	m := sizeModel(t, testModel())
	m.state = stDone
	path := attachmentFixture(t)
	m.acceptClipboard(clipboardMsg{path: path, session: "old"})
	if len(m.draftImages) != 0 {
		t.Fatal("stale image attached")
	}
	m.Update(tea.KeyPressMsg{Code: 'u', Mod: tea.ModCtrl})
	m.acceptClipboard(clipboardMsg{path: path})
	if len(m.draftImages) != 0 {
		t.Fatal("cleared capture reappeared")
	}
	m.state = stAsk
	m.acceptClipboard(clipboardMsg{path: path, revision: m.attachmentEpoch})
	if len(m.draftImages) != 0 {
		t.Fatal("image attached to question answer")
	}
}
func TestImageOnlyDraftAndTextOnlyModel(t *testing.T) {
	m := sizeModel(t, testModel())
	m.state = stDone
	m.draftImages = []string{attachmentFixture(t)}
	text, images, err := m.draftAttachments("")
	if err != nil || text != "" || len(images) != 1 {
		t.Fatalf("%q %v %v", text, images, err)
	}
	m.baseEnv.VisionKnown = true
	m.baseEnv.Vision = false
	if _, _, err = m.draftAttachments(""); err == nil {
		t.Fatal("text-only model accepted images")
	}
	m.baseEnv.Vision = true
	m.clipboardBusy = true
	if _, _, err = m.draftAttachments("look"); err == nil {
		t.Fatal("sent before capture completed")
	}
}
func TestAttachmentLimitAndQueueEditing(t *testing.T) {
	m := sizeModel(t, testModel())
	m.state = stRunning
	m.draftImages = []string{attachmentFixture(t)}
	m.Update(tea.KeyPressMsg{Code: tea.KeyEnter})
	if !m.hasQueued() || len(m.queuedImages) != 1 || len(m.draftImages) != 0 {
		t.Fatal("image-only draft not queued")
	}
	m.Update(tea.KeyPressMsg{Code: 'e', Mod: tea.ModCtrl})
	if m.queued != "" || len(m.draftImages) != 1 || len(m.queuedImages) != 0 {
		t.Fatal("queue edit lost images")
	}
	paths := make([]string, maxDraftImages+1)
	if validateAttachments(paths) == nil {
		t.Fatal("no image count limit")
	}
}
func TestAttachmentRenderSizes(t *testing.T) {
	for _, width := range []int{40, 80, 120} {
		m := sizeModelTo(t, testModel(), width, 24)
		m.state = stDone
		m.draftImages = []string{attachmentFixture(t), attachmentFixture(t)}
		m.input.SetValue("Compare these screenshots")
		m.fitBottom()
		view := ansi.Strip(m.View().Content)
		if !strings.Contains(view, "[1]") || !strings.Contains(view, "Compare") {
			t.Fatalf("composer missing at %d: %s", width, view)
		}
		t.Logf("width %d:\n%s", width, view)
	}
}
