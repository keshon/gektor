package agent

import (
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"

	tea "charm.land/bubbletea/v2"
	"github.com/charmbracelet/x/ansi"

	"github.com/keshon/gektor/internal/config"
	"github.com/keshon/gektor/pkg/tui/screen"
)

func TestThinkingBlocksExpandIndependentlyWithoutChangingHistory(t *testing.T) {
	defer screen.SetModern(false)
	m := sizeModelTo(t, testModel(), 80, 40)
	m.state = stDone
	m.applyAppearance(config.Appearance{Style: "modern", Frame: "minimal"})
	m.transcript.blocks = []block{thinkBlock(strings.Repeat("first reason ", 20) + "\n\nFIRST_DETAIL"), thinkBlock(strings.Repeat("second reason ", 20) + "\n\nSECOND_DETAIL")}
	before := append([]block(nil), m.transcript.blocks...)
	m.refreshContent()
	m.transcript.Viewport.GotoTop()
	x := m.promptOffset() + 2
	y := m.transcriptTop() + m.transcript.PartOffset(0)
	m.testUpdate(tea.MouseClickMsg{X: x, Y: y, Button: tea.MouseLeft})
	view := ansi.Strip(m.testView().Content)
	if !strings.Contains(view, "FIRST_DETAIL") || strings.Contains(view, "SECOND_DETAIL") || !m.blockExpanded(0) || m.blockExpanded(1) {
		t.Fatal("thinking expansion affected another block", view)
	}
	m.toggleHistoryBlock(1)
	if !m.blockExpanded(0) || !m.blockExpanded(1) || !reflect.DeepEqual(before, m.transcript.blocks) {
		t.Fatal("per-block expansion changed history or its neighbor")
	}
	m.openHistoryBlocks()
	m.testUpdate(tea.KeyPressMsg{Code: tea.KeyEnter})
	if m.blockExpanded(0) || !m.blockExpanded(1) {
		t.Fatal("keyboard block picker did not toggle only its chosen row")
	}
	m.applyAppearance(config.Appearance{Style: "modern", Frame: "minimal", ExpandThinking: true})
	if len(m.transcript.expanded) != 0 {
		t.Fatal("global display choice did not reset local overrides")
	}
	m.toggleHistoryBlock(0)
	m.applyAppearance(config.Appearance{Style: "modern", Frame: "minimal", ExpandThinking: true, HideThinking: true})
	if len(m.transcript.expanded) != 0 {
		t.Fatal("hiding thinking did not clear individual expansion choices")
	}
}

func TestPackedToolsExpandInlineAndStillOpenInspector(t *testing.T) {
	defer screen.SetModern(false)
	m := sizeModelTo(t, testModel(), 120, 40)
	m.state = stDone
	m.applyAppearance(config.Appearance{Style: "modern", Frame: "minimal", HideTools: true, ToolNamesOnly: true})
	m.appendBlock(toolCardBlock("read", `read_file {"path":"one.go"}`))
	m.attachResult("read", "VISIBLE_AFTER_EXPANSION", false)
	m.appendBlock(toolCardBlock("second", `read_file {"path":"two.go"}`))
	m.attachResult("second", "STILL_COLLAPSED", false)
	m.transcript.Viewport.GotoTop()
	hit := m.transcript.render.inlineTools[0]
	m.testUpdate(tea.MouseClickMsg{X: m.promptOffset() + hit.start, Y: m.transcriptTop() + m.transcript.PartOffset(hit.part) + hit.row, Button: tea.MouseLeft})
	view := ansi.Strip(m.testView().Content)
	if !strings.Contains(view, "VISIBLE_AFTER_EXPANSION") || strings.Contains(view, "STILL_COLLAPSED") || m.inspectorVisible() {
		t.Fatal("tool status did not expand only its own output", view)
	}
	for part, index := range m.transcript.render.partBlocks {
		if index != 0 {
			continue
		}
		target := m.transcript.render.toolHits[part]
		m.testUpdate(tea.MouseClickMsg{X: m.promptOffset() + target.start, Y: m.transcriptTop() + m.transcript.PartOffset(part) + target.row, Button: tea.MouseLeft})
		break
	}
	if !m.inspectorVisible() || m.inspector.callID != "read" {
		t.Fatal("tool name lost its inspector action")
	}
}

func TestFoldedPasteQueuesOriginalTextAndModernShowsQueueHint(t *testing.T) {
	defer screen.SetModern(false)
	m := queuedModel(t)
	m.applyAppearance(config.Appearance{Style: "modern", Frame: "minimal"})
	m.draftImages = nil
	m.input.SetValue("")
	text := "Start\n" + strings.Repeat("Original pasted text\n", 42) + "End"
	m.testUpdate(tea.PasteMsg{Content: text})
	if !m.input.Folded() || !strings.Contains(ansi.Strip(m.testView().Content), "Alt+Q") || m.composerStatus() != "" {
		t.Fatal("folded paste should show the queue shortcut without the running hints")
	}
	m.testUpdate(tea.KeyPressMsg{Code: tea.KeyEnter})
	pending := m.sess.PendingInputs()
	if pending[len(pending)-1].Message.Content != text || m.input.Value() != "" || m.input.Folded() {
		t.Fatal("sending a folded paste changed its contents or retained the draft")
	}
}

func TestModernCopyConfirmationDoesNotOpenDialog(t *testing.T) {
	defer screen.SetModern(false)
	m := sizeModelTo(t, testModel(), 80, 30)
	m.state = stDone
	m.applyAppearance(config.Appearance{Style: "modern", Frame: "minimal"})
	m.input.SetValue("Unsent draft")
	_, cmd := m.testUpdate(sessionCopiedMsg{label: "Session path", text: "PRIVATE_PATH"})
	view := ansi.Strip(m.testView().Content)
	if cmd == nil || m.chrome.DialogInfo().Kind != screen.NoDialog || !strings.Contains(view, "Session path copied") || strings.Contains(view, "PRIVATE_PATH") || m.input.Value() != "Unsent draft" {
		t.Fatal("copy notice interrupted input or exposed clipboard contents")
	}
}

func TestModernInspectorRendersReturnedUnifiedDiff(t *testing.T) {
	defer screen.SetModern(false)
	m := sizeModelTo(t, testModel(), 120, 40)
	m.state = stDone
	m.applyAppearance(config.Appearance{Style: "modern", Frame: "minimal", HideTools: true})
	m.appendBlock(toolCardBlock("patch", `patch_file {"path":"example.go","old_content":"old","new_content":"new"}`))
	m.attachResult("patch", "ok: edited file\n--- a/example.go\n+++ b/example.go\n@@ -10,2 +10,2 @@\n context\n-old\n+new", false)
	m.openInspector(0)
	view := ansi.Strip(m.testView().Content)
	for _, want := range []string{"  10   10", "  11      - old", "       11 + new"} {
		if !strings.Contains(view, want) {
			t.Fatal("inspector lost diff numbers or content", want, view)
		}
	}
}

func TestModernInteractionsPreview(t *testing.T) {
	dir := os.Getenv("GEKTOR_TUI_PREVIEW_DIR")
	if dir == "" {
		t.Skip("preview export opt-in")
	}
	defer screen.SetModern(false)
	m := sizeModelTo(t, testModel(), 120, 40)
	m.state = stDone
	m.applyAppearance(config.Appearance{Style: "modern", Frame: "minimal", HideTools: true})
	m.modelName = "Ornith-1.5-35B-A3B"
	m.appendBlock(userBlock("Review the change and keep the original pasted text."))
	m.appendBlock(thinkBlock("Check the patch before discussing the result.\n\nThe expanded reasoning stays independent of other blocks."))
	m.appendBlock(toolCardBlock("patch", `patch_file {"path":"example.go","old_content":"old","new_content":"new"}`))
	m.attachResult("patch", "ok: edited file\n--- a/example.go\n+++ b/example.go\n@@ -10,2 +10,2 @@\n context\n-old\n+new", false)
	m.toggleHistoryBlock(1)
	m.toggleHistoryBlock(2)
	m.input.Paste(strings.Repeat("Original pasted text\n", 42))
	m.fitBottom()
	err := os.WriteFile(filepath.Join(dir, "modern-interactions.ansi"), []byte(m.testView().Content), 0600)
	if err != nil {
		t.Fatal(err)
	}
	m.openInspector(2)
	err = os.WriteFile(filepath.Join(dir, "modern-interactions-inspector.ansi"), []byte(m.testView().Content), 0600)
	if err != nil {
		t.Fatal(err)
	}
	m.closeInspector()
	m.chrome.Notify("Session path copied")
	err = os.WriteFile(filepath.Join(dir, "modern-interactions-notice.ansi"), []byte(m.testView().Content), 0600)
	if err != nil {
		t.Fatal(err)
	}
}

func TestLongPasteExceedingOldVisualRowLimitStaysIntact(t *testing.T) {
	defer screen.SetModern(false)
	m := sizeModelTo(t, testModel(), 40, 30)
	m.state = stDone
	m.applyAppearance(config.Appearance{Style: "modern", Frame: "minimal"})
	text := strings.Repeat("Original input line\n", 4200) + "Last line"
	m.testUpdate(tea.PasteMsg{Content: text})
	if m.input.Value() != text || !m.input.Folded() || m.input.Height() != 1 {
		t.Fatal("long pasted draft was truncated")
	}
	m.testUpdate(tea.KeyPressMsg{Code: 'v', Mod: tea.ModAlt})
	if m.input.Value() != text || m.input.Folded() || m.input.Height() > m.input.MaxHeight {
		t.Fatal("expanding a long paste lost content or overflowed the editor")
	}
}
