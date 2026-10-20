package tui

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	tea "charm.land/bubbletea/v2"
	"charm.land/lipgloss/v2"
	"github.com/charmbracelet/x/ansi"
	"github.com/keshon/tars/internal/events"
	"github.com/keshon/tars/internal/llm"
	"github.com/keshon/tars/internal/workspace"
)

func TestPermissionDecisionAlwaysHasVisibleContext(t *testing.T) {
	m := sizeModel(t, testModel())
	for i := 0; i < 30; i++ {
		m.appendBlock(userBlock(fmt.Sprintf("History %d", i)))
	}
	m.transcript.Viewport.GotoTop()
	m.transcript.Follow = false
	m.handleEvent(events.Event{Name: "awaiting_input", Fields: map[string]any{"kind": "permission", "prompt": "TARGET_SCOPE\n" + strings.Repeat("preview line\n", 40)}})
	view := m.View().Content
	// The popup pins the question head with the options; the full
	// prompt lives in the transcript behind it.
	for _, want := range []string{"TARGET_SCOPE", "Permission required", "Allow once", "Allow this run", "Deny with note"} {
		if !strings.Contains(view, want) {
			t.Fatalf("gate popup missing %q:\n%s", want, view)
		}
	}
	// History stays mounted behind the popup, not replaced by it.
	if !strings.Contains(view, "History 0") && !strings.Contains(view, "History 29") {
		t.Fatal("gate popup replaced the transcript instead of overlaying it")
	}
	m.handleKey(tea.KeyPressMsg{Text: string("n")})
	if !strings.Contains(m.View().Content, "note>") {
		t.Fatal("rejection input clipped")
	}
	m.handleKey(tea.KeyPressMsg{Code: tea.KeyEsc})
	if m.gstage != gsPermit || strings.Contains(m.View().Content, "note>") {
		t.Fatal("rejection did not close")
	}
	if len(strings.Split(m.View().Content, "\n")) > m.termH {
		t.Fatal("gate overflowed terminal")
	}
}
func TestQueueCanBeEditedCancelledAndSurvivesFailure(t *testing.T) {
	m := sizeModel(t, testModel())
	m.queued = "Queued task"
	m.fitBottom()
	if !strings.Contains(m.View().Content, "Ctrl+X cancel") {
		t.Fatal("queue controls missing")
	}
	m.handleKey(tea.KeyPressMsg{Code: 'e', Mod: tea.ModCtrl})
	if m.input.Value() != "Queued task" || m.queued != "" {
		t.Fatal("queue not editable")
	}
	m.queued = "Another task"
	m.handleKey(tea.KeyPressMsg{Code: 'x', Mod: tea.ModCtrl})
	if m.queued != "" || m.input.Value() != "Queued task" {
		t.Fatal("queue cancel changed draft")
	}
	m.queued = "Keep me"
	m.Update(doneMsg{err: errors.New("backend down")})
	if m.queued != "Keep me" || m.input.Value() != "Queued task" {
		t.Fatal("failure overwrote queued work or draft")
	}
}

func TestGlobalQuitWorksInEveryPermissionStage(t *testing.T) {
	for _, stage := range []gateStage{gsPermit, gsAlways, gsReject} {
		m := sizeModel(t, testModel())
		m.state = stPermission
		m.gstage = stage
		m.handleKey(tea.KeyPressMsg{Code: 'c', Mod: tea.ModCtrl})
		if !m.quit {
			t.Fatalf("quit ignored in stage %d", stage)
		}
	}
}

func TestSessionBrowserMouseSelects(t *testing.T) {
	m := sizeModel(t, testModel())
	m.state = stDone
	m.sessions = &sessionsState{filter: newSearchInput(80), entries: []sessionEntry{{id: "a"}, {id: "b"}}}
	g := m.searchGeometry()
	m.Update(tea.MouseClickMsg{X: g.x + 2, Y: g.y + 5, Button: tea.MouseLeft})
	if m.sessions.Cursor != 1 {
		t.Fatal("click ignored")
	}
}

func TestEmptyFoldersHiddenAndUnreadableMarked(t *testing.T) {
	root := t.TempDir()
	os.MkdirAll(filepath.Join(root, "empty", "snapshots"), 0755)
	os.MkdirAll(filepath.Join(root, "broken"), 0755)
	os.WriteFile(filepath.Join(root, "broken", "state.json"), []byte("broken"), 0644)
	entries := listSessionsIn(root)
	if len(entries) != 1 || entries[0].status != "Unreadable" {
		t.Fatalf("entries: %+v", entries)
	}
}

func TestJumpToLatestWorksWhenIdle(t *testing.T) {
	m := sizeModel(t, testModel())
	m.state = stDone
	for i := 0; i < 30; i++ {
		m.appendBlock(userBlock(fmt.Sprintf("Message %d", i)))
	}
	m.transcript.Viewport.GotoTop()
	m.transcript.Follow = false
	m.handleKey(tea.KeyPressMsg{Code: tea.KeyEnd, Mod: tea.ModCtrl})
	if !m.transcript.Follow || !m.transcript.Viewport.AtBottom() {
		t.Fatal("jump ignored")
	}
}

func TestOpeningOldPlanSessionKeepsCurrentActMode(t *testing.T) {
	root := t.TempDir()
	dir := writeSessionState(t, root, "plan", userHistory("Plan changes"))
	if err := writeSavedMode(filepath.Join(dir, "state.json"), true); err != nil {
		t.Fatal(err)
	}
	m := sessionsTestModel(t, listSessionsIn(root))
	m.openSelected()
	if m.plan || m.modeName() != "Act" {
		t.Fatal("saved history replaced the current global mode")
	}
}

func TestQuestionHasScrollablePreviewAndPreservesDraftOnStop(t *testing.T) {
	m := sizeModel(t, testModel())
	m.input.SetValue("Original draft")
	m.handleEvent(events.Event{Name: "awaiting_input", Fields: map[string]any{"kind": "question", "prompt": "QUESTION_SCOPE\n" + strings.Repeat("question detail\n", 40)}})
	view := m.View().Content
	// The popup pins the question head; the full prompt lives in the
	// transcript behind it, which keeps scrolling underneath.
	for _, want := range []string{"QUESTION_SCOPE", "Answer required"} {
		if !strings.Contains(view, want) {
			t.Fatalf("question popup missing %q", want)
		}
	}
	m.handleKey(tea.KeyPressMsg{Code: tea.KeyPgDown})
	if m.transcript.Viewport.YOffset() == 0 {
		t.Fatal("transcript behind the question cannot scroll")
	}
	m.input.SetValue("Partial answer")
	m.handleKey(tea.KeyPressMsg{Code: tea.KeyEsc})
	m.Update(doneMsg{})
	if m.input.Value() != "Original draft\nPartial answer" {
		t.Fatalf("lost draft: %q", m.input.Value())
	}
}

func TestQueueHeldWhenSuccessfulRunHasAnotherDraft(t *testing.T) {
	m := sizeModel(t, testModel())
	m.queued = "Next task"
	m.input.SetValue("Still composing")
	m.Update(doneMsg{})
	if m.state != stDone || m.queued != "Next task" || m.input.Value() != "Still composing" {
		t.Fatal("queue ran over draft")
	}
	m.handleKey(tea.KeyPressMsg{Code: 'e', Mod: tea.ModCtrl})
	if m.input.Value() != "Still composing" || m.queued != "Next task" {
		t.Fatal("queue editing overwrote draft")
	}
}

func TestInvalidAttachmentPreservesSubmission(t *testing.T) {
	m := sizeModel(t, testModel())
	ws, err := workspace.New(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	m.ws = ws
	m.state = stDone
	draft := `Inspect @"missing-file.png"`
	m.input.SetValue(draft)
	m.followUp()
	if m.input.Value() != draft {
		t.Fatal("failed attachment erased task")
	}
}

func TestPromptTransitionsFitWideAndNarrowTerminals(t *testing.T) {
	for _, width := range []int{40, 80, 120} {
		m := sizeModel(t, testModel())
		m.width(tea.WindowSizeMsg{Width: width, Height: 20})
		m.handleEvent(events.Event{Name: "awaiting_input", Fields: map[string]any{"kind": "permission", "prompt": "Review target"}})
		m.gstage = gsReject
		m.fitBottom()
		if !strings.Contains(m.View().Content, "note>") {
			t.Fatalf("note hidden at width %d", width)
		}
		m.handleEvent(events.Event{Name: "input_answered"})
		if m.sidebarVisible() && m.transcript.Viewport.Width() != width-navigatorWidth-5 {
			t.Fatalf("transcript did not refit: %d", m.transcript.Viewport.Width())
		}
	}
}

func TestBrowserActionsRemainInsideTerminalFrame(t *testing.T) {
	m := sizeModel(t, testModel())
	m.state = stDone
	m.sessions = &sessionsState{filter: newSearchInput(80), entries: []sessionEntry{{id: "a", title: "Saved session"}}}
	m.fitBottom()
	if !strings.Contains(m.View().Content, "╭─ Search") || !strings.Contains(m.View().Content, "Enter open") {
		t.Fatal("search popup actions clipped")
	}
}

func TestInlineThinkingUsesThinkingBlockLiveAndOnReopen(t *testing.T) {
	content := "<think>Проверяю ответ.\nSecond line.</think>\n\nI'm TARS."
	m := sizeModel(t, testModel())
	m.handleEvent(events.Event{Name: "step", Fields: map[string]any{"text": content}})
	history := renderHistory([]llm.Message{{Role: llm.RoleAssistant, Content: content}})
	for _, blocks := range [][]block{m.transcript.blocks, history} {
		if len(blocks) != 2 || blocks[0].role != roleThink || blocks[1].role != roleAnswer {
			t.Fatalf("incorrect reasoning split: %+v", blocks)
		}
		if blocks[0].text != "Проверяю ответ.\nSecond line." || blocks[1].text != "I'm TARS." {
			t.Fatalf("content changed: %+v", blocks)
		}
	}
	if strings.Contains(m.View().Content, "<think>") || !strings.Contains(m.View().Content, "[THINKING]") {
		t.Fatal("raw thinking shown as response")
	}
}

func TestThinkingSplitSupportsIncompleteAndMultipleRegions(t *testing.T) {
	for _, tc := range []struct{ content, reasoning, thought, answer string }{
		{"<think>unfinished", "", "unfinished", ""},
		{"<think></think>Answer", "", "", "Answer"},
		{"<think>first</think>Answer<think>second</think>", "", "first\n\nsecond", "Answer"},
		{"<think>inline</think>Answer", "separate", "separate\n\ninline", "Answer"},
	} {
		blocks := assistantBlocks(tc.content, tc.reasoning)
		var thought, answer string
		for _, b := range blocks {
			if b.role == roleThink {
				thought = b.text
			}
			if b.role == roleAnswer {
				answer = b.text
			}
		}
		if thought != tc.thought || answer != tc.answer {
			t.Fatalf("%q: thought %q, answer %q", tc.content, thought, answer)
		}
	}
}

// Notices share the Identity's three-row shape: a tag row, then the
// body at the left edge. A fixed 6-cell label column used to indent every
// notice body and re-shrink the wrap budget for no readable gain.
func TestSystemNoticesShareConversationLayout(t *testing.T) {
	for _, tc := range []struct {
		b     block
		label string
	}{
		{markerBlock("— new task —"), "HARNESS"},
		{markerBlock("[harness] " + strings.Repeat("review this carefully ", 20)), "HARNESS"},
		{errorBlock("Cannot resume\nTry again"), "ERROR"},
		{findingBlock("gofmt", "a.go", 3, "format required"), "WARNING"},
		{gateBlock("Allow this tool?"), "QUESTION"},
	} {
		for _, width := range []int{30, 80, 120} {
			rendered := ansi.Strip(renderBlock(tc.b, defaultStyles(), tagged(), width))
			rows := strings.Split(rendered, "\n")
			if !strings.HasPrefix(rows[0], "["+tc.label+"]") {
				t.Fatalf("label offset: %q", rows[0])
			}
			for _, row := range rows {
				if lipgloss.Width(row) > width {
					t.Fatalf("notice overflow: %q", row)
				}
			}
			for _, row := range rows[1:] {
				if row != "" && strings.HasPrefix(row, " ") {
					t.Fatalf("notice body indented: %q", row)
				}
			}
		}
	}
}

// The harness step line keeps its trim in both paths: renderHistory
// rebuilds markers from stored messages, and a rebuilt block would drop
// the indent the live path applies. Timestamps are normalized away —
// reopened history records none, which is a separate rule.
func TestHarnessNoticeMatchesLiveAndRebuiltHistory(t *testing.T) {
	text := "[harness] Verify your work.\n (step 2/25, 23 left)"
	m := sizeModel(t, testModel())
	m.handleEvent(events.Event{Name: "nudge", Fields: map[string]any{"kind": "verify", "text": text}})
	rebuilt := renderHistory([]llm.Message{{Role: llm.RoleUser, Content: text}})
	if len(m.transcript.blocks) != 1 || len(rebuilt) != 1 || rebuilt[0].role != roleMarker {
		t.Fatalf("harness omitted: %+v", rebuilt)
	}
	liveBlock, restoredBlock := m.transcript.blocks[0], rebuilt[0]
	liveBlock.at, restoredBlock.at = time.Time{}, time.Time{}
	live := ansi.Strip(renderBlock(liveBlock, defaultStyles(), tagged(), 80))
	restored := ansi.Strip(renderBlock(restoredBlock, defaultStyles(), tagged(), 80))
	if live != restored || !strings.HasPrefix(restored, "[HARNESS]") || !strings.Contains(restored, "\n(step 2/25, 23 left)") {
		t.Fatalf("harness rendering differs: %q / %q", live, restored)
	}
}

// The SYS filter hides harness notices and nothing else. Errors,
// warnings, permission questions and tool results are the reader”s
// record of what happened; hiding them would let a failed run scroll
// away with no trace.
func TestHideSYSKeepsFailuresAndQuestions(t *testing.T) {
	m := sizeModelTo(t, testModel(), 80, 30)
	m.transcript.blocks = []block{
		answerBlock("answer"),
		markerBlock("harness nudge"),
		errorBlock("run failed"),
		findingBlock("gofmt", "a.go", 3, "not formatted"),
		gateBlock("Allow this tool?"),
	}
	m.refreshContent()
	full := ansi.Strip(m.transcript.Viewport.View())
	for _, want := range []string{"harness nudge", "run failed", "not formatted", "Allow this tool?"} {
		if !strings.Contains(full, want) {
			t.Fatalf("unfiltered transcript missing %q: %q", want, full)
		}
	}
	m.hideSYS = true
	m.refreshContent()
	hidden := ansi.Strip(m.transcript.Viewport.View())
	if strings.Contains(hidden, "harness nudge") {
		t.Fatalf("harness notice survived the SYS filter: %q", hidden)
	}
	for _, want := range []string{"run failed", "not formatted", "Allow this tool?", "[ERROR]", "[WARNING]", "[QUESTION]"} {
		if !strings.Contains(hidden, want) {
			t.Fatalf("SYS filter took %q with it: %q", want, hidden)
		}
	}
	// Hiding is display-only: lifting it restores the block verbatim.
	if !strings.HasPrefix(m.transcript.blocks[1].text, "harness nudge") {
		t.Fatal("filter mutated stored text")
	}
	m.hideSYS = false
	m.refreshContent()
	if got := ansi.Strip(m.transcript.Viewport.View()); !strings.Contains(got, "harness nudge") {
		t.Fatalf("lifting the filter lost the block: %q", got)
	}
}

// Hiding labels drops the tag row and keeps the name row. Dropping both
// would leave unattributed prose with no way to tell a refusal from a
// plan.
func TestHideLabelsKeepsSpeakerRows(t *testing.T) {
	m := sizeModelTo(t, testModel(), 80, 30)
	m.identityName = "Ada"
	m.transcript.blocks = []block{userBlock("question"), answerBlock("answer"), markerBlock("harness nudge")}
	m.refreshContent()
	full := ansi.Strip(m.transcript.Viewport.View())
	for _, want := range []string{"YOU", "ADA", "SYSTEM", "[INPUT]", "[RESPONSE]", "[HARNESS]"} {
		if !strings.Contains(full, want) {
			t.Fatalf("default transcript missing %q: %q", want, full)
		}
	}
	m.hideLabels = true
	m.refreshContent()
	bare := ansi.Strip(m.transcript.Viewport.View())
	for _, want := range []string{"YOU", "ADA", "SYSTEM", "question", "answer", "harness nudge"} {
		if !strings.Contains(bare, want) {
			t.Fatalf("label-free transcript missing %q: %q", want, bare)
		}
	}
	for _, unwanted := range []string{"[INPUT]", "[RESPONSE]", "[HARNESS]"} {
		if strings.Contains(bare, unwanted) {
			t.Fatalf("tag row survived: %q in %q", unwanted, bare)
		}
	}
	m.hideLabels = false
	m.refreshContent()
	if got := ansi.Strip(m.transcript.Viewport.View()); !strings.Contains(got, "[RESPONSE]") {
		t.Fatalf("lifting the filter lost the tag row: %q", got)
	}
}

// The render cache is keyed on the view choices. A stale entry would
// leave one block tagged and the next not, after a single toggle.
func TestViewTogglesInvalidateRenderCache(t *testing.T) {
	m := sizeModelTo(t, testModel(), 80, 30)
	m.transcript.blocks = []block{answerBlock("one"), answerBlock("two")}
	m.refreshContent()
	tagged := ansi.Strip(m.transcript.Viewport.View())
	m.hideLabels = true
	m.refreshContent()
	bare := ansi.Strip(m.transcript.Viewport.View())
	if tagged == bare || strings.Contains(bare, "[RESPONSE]") {
		t.Fatalf("cache served stale tagged text: %q", bare)
	}
	m.hideLabels = false
	m.refreshContent()
	if got := ansi.Strip(m.transcript.Viewport.View()); strings.Count(got, "[RESPONSE]") != 2 {
		t.Fatalf("restoring labels must re-tag every block: %q", got)
	}
}

// A filtered block must not poison the cache: its placeholder must never
// satisfy a lookup once the filter lifts, or the block comes back blank.
func TestHiddenBlockDoesNotPoisonCache(t *testing.T) {
	m := sizeModelTo(t, testModel(), 80, 30)
	m.transcript.blocks = []block{answerBlock("kept"), markerBlock("filtered")}
	m.hideSYS = true
	m.refreshContent()
	m.hideSYS = false
	m.refreshContent()
	got := ansi.Strip(m.transcript.Viewport.View())
	if !strings.Contains(got, "filtered") || !strings.Contains(got, "kept") {
		t.Fatalf("lifting the filter lost or blanked a block: %q", got)
	}
}

// The menu rows and the transcript read the same two flags.
func TestViewMenuCommandsDriveTheTranscript(t *testing.T) {
	m := sizeModelTo(t, testModel(), 80, 30)
	m.transcript.blocks = []block{answerBlock("answer"), markerBlock("harness nudge")}
	m.refreshContent()
	m.header.focused = true
	m.header.global = true
	m.header.menu = "view"
	for _, command := range []string{"toggle-labels", "toggle-sys"} {
		// Enter closes the menu, so each action reopens it.
		m.header.menu, m.header.focused = "view", true
		row := -1
		for i, item := range m.headerItems() {
			if item.command == command {
				row = i
			}
		}
		if row < 0 {
			t.Fatalf("%s missing from View menu", command)
		}
		m.header.row = row
		m.handleKey(tea.KeyPressMsg{Code: tea.KeyEnter})
	}
	if !m.hideLabels || !m.hideSYS {
		t.Fatalf("menu did not set the filters: labels=%v sys=%v", m.hideLabels, m.hideSYS)
	}
	got := ansi.Strip(m.transcript.Viewport.View())
	if strings.Contains(got, "harness nudge") || strings.Contains(got, "[RESPONSE]") {
		t.Fatalf("menu action did not reach the renderer: %q", got)
	}
	if !strings.Contains(got, "answer") {
		t.Fatalf("filter removed Identity text: %q", got)
	}
}
