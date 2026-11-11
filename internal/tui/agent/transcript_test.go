package agent

import (
	"context"
	"regexp"
	"strings"
	"testing"
	"time"

	tea "charm.land/bubbletea/v2"
	"charm.land/lipgloss/v2"
	"github.com/charmbracelet/x/ansi"

	"github.com/keshon/gektor/internal/events"
)

func TestStepReasoningRendersDim(t *testing.T) {
	m := sizeModel(t, testModel())
	updated, _ := m.testUpdate(eventMsg(events.Event{
		Name:   "step",
		Fields: map[string]any{"text": "done", "reasoning": "hmm, tricky"},
	}))
	mm := updated.(*Model)
	if len(mm.transcript.blocks) != 2 {
		t.Fatalf("blocks = %v", mm.transcript.blocks)
	}
	if mm.transcript.blocks[0].role != roleThink {
		t.Fatalf("reasoning must be a think block: %+v", mm.transcript.blocks[0])
	}
	if !strings.Contains(mm.transcript.blocks[0].text, "hmm, tricky") {
		t.Fatalf("reasoning not kept: %v", mm.transcript.blocks)
	}
	if mm.transcript.blocks[1].role != roleAnswer {
		t.Fatalf("answer must follow its thinking: %+v", mm.transcript.blocks[1])
	}
}

func TestToolCallAndResultRender(t *testing.T) {
	m := sizeModel(t, testModel())
	updated, _ := m.testUpdate(eventMsg(events.Event{
		Name: "step",
		Fields: map[string]any{
			"tool_calls": []any{map[string]any{"name": "read_file", "args": `{"path":"a"}`}},
		},
	}))
	mm := updated.(*Model)
	if len(mm.transcript.blocks) != 1 || !strings.Contains(mm.transcript.blocks[0].text, "read_file") || mm.transcript.blocks[0].role != roleTool {
		t.Fatalf("blocks = %+v", mm.transcript.blocks)
	}
	updated, _ = mm.testUpdate(eventMsg(events.Event{
		Name:   "tool_result",
		Fields: map[string]any{"call_id": "c1", "text": "-old\n+new"},
	}))
	mm = updated.(*Model)
	if len(mm.transcript.blocks) != 2 {
		t.Fatalf("blocks = %v", mm.transcript.blocks)
	}
}

func TestTranscriptMeetsDividerWithoutReservedBlankRow(t *testing.T) {
	m := sizeModel(t, testModel())
	m.state = stDone
	m.fitBottom()
	for _, b := range renderHistory(compactTestHistory(15)) {
		m.appendBlock(b)
	}
	m.appendBlock(answerBlock("last transcript line"))
	m.transcript.Viewport.GotoBottom()
	lines := strings.Split(m.testView().Content, "\n")
	div := paneTop + m.transcript.Viewport.Height()
	if div < 1 {
		t.Fatalf("no divider in view:\n%s", m.testView().Content)
	}
	if !strings.Contains(ansi.Strip(lines[div-1]), "last transcript line") {
		t.Fatalf("last transcript row lost above divider: %q", lines[div-1])
	}
}

func TestNewBlocksRespectUnfollow(t *testing.T) {
	m := sizeModel(t, testModel())
	for i := 0; i < 40; i++ {
		m.appendBlock(markerBlock("block"))
	}
	m.transcript.Follow = false
	y0 := m.transcript.Viewport.YOffset()
	m.appendBlock(markerBlock("new block while reading"))
	if m.transcript.Viewport.YOffset() != y0 {
		t.Fatal("new block yanked a scrolled-up viewport")
	}
}

func TestRolesAssignedAcrossEvent(t *testing.T) {
	m := sizeModel(t, testModel())
	updated, _ := m.testUpdate(eventMsg(events.Event{
		Name: "step",
		Fields: map[string]any{
			"text":      "answer",
			"reasoning": "...musing about the task",
			"tool_calls": []any{
				map[string]any{"name": "read_file", "args": "{}"},
			},
		},
	}))
	mm := updated.(*Model)
	want := []role{roleThink, roleAnswer, roleTool}
	if len(mm.transcript.blocks) != len(want) {
		t.Fatalf("blocks = %+v", mm.transcript.blocks)
	}
	for i, r := range want {
		if mm.transcript.blocks[i].role != r {
			t.Fatalf("block %d role = %v, want %v", i, mm.transcript.blocks[i].role, r)
		}
	}
}

// Body text starts at the left edge. The label row already named the
// block, so a per-line gutter repeated that answer at two cells a line.
func TestBodyStartsAtLeftEdge(t *testing.T) {
	st := defaultStyles()
	for _, b := range []block{
		answerBlock("answer body"), userBlock("user body"), thinkBlock("thought"),
		markerBlock("harness note"), errorBlock("boom"), gateBlock("allow?"),
		resultBlock("result body"),
	} {
		for _, width := range []int{30, 80, 120} {
			rows := strings.Split(ansi.Strip(renderBlock(b, st, tagged(), width)), "\n")
			if rows[0] != "["+roleLabel(b.role)+"]" && !strings.HasPrefix(rows[0], "["+roleLabel(b.role)+"] ") {
				t.Fatalf("role %v label row = %q", b.role, rows[0])
			}
			for _, row := range rows[1:] {
				if row == "" {
					continue
				}
				if strings.HasPrefix(row, " ") || strings.HasPrefix(row, "│ ") || strings.HasPrefix(row, "> ") {
					t.Fatalf("role %v body is indented at %d cols: %q", b.role, width, row)
				}
				if n := lipgloss.Width(row); n > width {
					t.Fatalf("role %v body %d cols past %d: %q", b.role, n, width, row)
				}
			}
		}
	}
}

// Tool card headers collapse multi-line model args to one line
// (the open card's pending row is structure, not args).
func TestToolCardHeader_SingleLine(t *testing.T) {
	st := defaultStyles()
	out := renderBlock(toolCardBlock("c1", "write_file {\n  \"path\": \"a.go\"\n}"), st, renderOpts{expand: true, labels: true}, 80)
	lines := strings.Split(out, "\n")
	if len(lines) != 4 || !strings.Contains(lines[1], `{"path":"a.go"}`) {
		t.Fatalf("card call must hold collapsed args: %q", out)
	}
}

func TestTimestampRendered(t *testing.T) {
	st := defaultStyles()
	at := time.Date(2026, 10, 3, 14, 22, 0, 0, time.Local)
	out := renderBlock(block{role: roleUser, text: "❯ hi", at: at}, st, tagged(), 80)
	if !strings.Contains(out, "[14:22]") {
		t.Fatalf("user block missing timestamp: %q", out)
	}
	// Unstamped blocks (zero time) still carry speaker identity, just
	// no timestamp: headers are structure, not chrome.
	plain := renderBlock(block{role: roleAnswer, text: "hi"}, st, tagged(), 80)
	if !strings.Contains(plain, "[RESPONSE]") {
		t.Fatalf("answer missing speaker header: %q", plain)
	}
	if strings.Contains(plain, "[14:22]") {
		t.Fatalf("zero-time block must not stamp: %q", plain)
	}
}

func TestReflow(t *testing.T) {
	for _, tc := range []struct {
		in    string
		width int
		want  []string
	}{
		{"aaa bbb ccc ddd eee", 8, []string{"aaa bbb", "ccc ddd", "eee"}},
		{"  indented line here", 10, []string{"  indented", "line here"}},
		{"abcdefghij", 8, []string{"abcdefgh", "ij"}},
		{"a\tb", 80, []string{"a    b"}},
		{"", 80, []string{""}},
		{"short", 2, []string{"short"}},
	} {
		got := reflow(tc.in, tc.width)
		if len(got) != len(tc.want) {
			t.Fatalf("reflow(%q, %d) = %q, want %q", tc.in, tc.width, got, tc.want)
		}
		for i := range got {
			if got[i] != tc.want[i] {
				t.Fatalf("reflow(%q, %d) = %q, want %q", tc.in, tc.width, got, tc.want)
			}
		}
	}
}

func TestRenderBlockWrapsToWidth(t *testing.T) {
	st := defaultStyles()
	text := "lorem ipsum dolor sit amet consectetur adipiscing elit sed do"
	out := renderBlock(answerBlock(text), st, tagged(), 20)
	for _, line := range strings.Split(out, "\n") {
		if n := lipgloss.Width(line); n > 20 {
			t.Fatalf("line %d cols past 20: %q", n, line)
		}
	}
	// The label row names the block; the body below wraps in column 0.
	lines := strings.Split(ansi.Strip(out), "\n")
	if len(lines) < 2 || !strings.HasPrefix(lines[0], "[RESPONSE]") || lines[1] != "lorem ipsum dolor" {
		t.Fatalf("label row then left-edge body expected: %q", out)
	}
	out = renderBlock(userBlock("aaa bbb ccc ddd eee fff"), st, tagged(), 20)
	lines = strings.Split(ansi.Strip(out), "\n")
	if len(lines) < 2 || !strings.HasPrefix(lines[0], "[INPUT]") || lines[1] != "aaa bbb ccc ddd eee" {
		t.Fatalf("label row then left-edge body expected: %q", out)
	}
}

func TestPreReadyBlocksRender(t *testing.T) {
	m := testModel() // never sized: not ready
	m.appendBlock(markerBlock("early"))
	mm := sizeModel(t, m)
	if view := mm.testView().Content; !strings.Contains(view, "early") {
		t.Fatalf("pre-ready block missing: %q", view)
	}
}

func TestTruncateRuneSafe(t *testing.T) {
	if got := truncate(strings.Repeat("—", 10), 8); got != strings.Repeat("—", 7)+"…" {
		t.Fatalf("truncate = %q", got)
	}
	if truncate("abc", 8) != "abc" {
		t.Fatal("short text altered")
	}
}

func TestDividerExactWidth(t *testing.T) {
	m := sizeModel(t, testModel())
	m.appendBlock(userBlock("hi"))
	m.appendBlock(answerBlock("there"))
	rows := strings.Split(ansi.Strip(m.screenContent()), "\n")
	line := ansi.Cut(rows[paneTop+m.transcript.Viewport.Height()], m.promptOffset(), m.promptOffset()+m.transcript.Viewport.Width())
	if n := lipgloss.Width(line); n != m.transcript.Viewport.Width() {
		t.Fatalf("divider %d cols, want viewport %d", n, m.transcript.Viewport.Width())
	}
}

func TestStampsRightAligned(t *testing.T) {
	st := defaultStyles()
	b := answerBlock("short")
	b.at = time.Date(2026, 10, 3, 14, 22, 0, 0, time.Local)
	out := renderBlock(b, st, tagged(), 40)
	lines := strings.Split(out, "\n")
	if len(lines) != 2 {
		t.Fatalf("block is label row + body: %q", out)
	}
	// The stamp rides the label row, so the body keeps its full width.
	if n := lipgloss.Width(lines[0]); n != 40 {
		t.Fatalf("stamp line %d cols, want full 40", n)
	}
	if !strings.Contains(lines[0], "[14:22]") || !strings.HasPrefix(ansi.Strip(lines[0]), "[RESPONSE]") {
		t.Fatalf("stamp missing or label not flush left: %q", lines[0])
	}
	if n := lipgloss.Width(lines[1]); n != 5 {
		t.Fatalf("body must keep full width, got %d cols", n)
	}
}

// A narrow terminal cannot hold a long tag and a stamp. The tag is
// structure, so it wins and the time is dropped rather than pushed past
// the edge.
func TestNarrowLabelRowDropsStamp(t *testing.T) {
	st := defaultStyles()
	b := toolCardBlock("c1", "run x")
	b.result = "ok"
	b.open = false
	out := renderBlock(b, st, tagged(), 12)
	if strings.Contains(out, ":") && strings.Count(out, "]") != 1 {
		t.Fatalf("narrow label row must not stamp: %q", out)
	}
	for _, line := range strings.Split(out, "\n") {
		if n := lipgloss.Width(line); n > 12 {
			t.Fatalf("line %d cols past 12: %q", n, line)
		}
	}
}

func TestCleanTextStripsCR(t *testing.T) {
	if got := cleanText("a\r\nb\rc"); got != "a\nb\nc" {
		t.Fatalf("cleanText = %q", got)
	}
}

// CR belongs nowhere near the frame. A \r mid-line returns the cursor to
// column 0, so the stamp padding overwrites the line head and the
// alt-screen repaint desyncs into ghost text over stale rows.
func TestCRLFContentStaysInWidth(t *testing.T) {
	st := defaultStyles()
	b := toolCardBlock("c1", "read_file a.md")
	b.result = "FILE\npath: a.md\n----\n# Title\r\n\r\nbody line\r\n"
	b.open = false
	for _, line := range strings.Split(renderBlock(b, st, renderOpts{expand: true, labels: true}, 80), "\n") {
		if strings.Contains(line, "\r") {
			t.Fatalf("CR survived render: %q", line)
		}
		if n := lipgloss.Width(line); n > 80 {
			t.Fatalf("line %d cols past 80: %q", n, line)
		}
	}
	out := renderBlock(answerBlock("integrity.\r\nsecond line"), st, tagged(), 40)
	if strings.Contains(out, "\r") {
		t.Fatalf("CR survived stamped render: %q", out)
	}
	lines := strings.Split(ansi.Strip(out), "\n")
	if len(lines) < 2 || !strings.HasPrefix(lines[0], "[RESPONSE]") || !strings.Contains(lines[1], "integrity.") {
		t.Fatalf("stamped CRLF answer lost its label or body: %q", out)
	}
}

func TestToolOutputBranchesAndCloses(t *testing.T) {
	st := defaultStyles()
	b := toolCardBlock("c1", `read_file {"path":"internal/config/profile.go"}`)
	b.result = "file content ....\nfile content ....\nfile content ...."
	b.open = false
	rows := strings.Split(ansi.Strip(renderBlock(b, st, tagged(), 80)), "\n")
	if !strings.HasPrefix(rows[0], "[TOOL CALL]") || !strings.HasPrefix(rows[1], "✓ read_file ") {
		t.Fatalf("label row then call row expected: %q", renderBlock(b, st, tagged(), 80))
	}
	for i, want := range []string{"│ file content ....", "│ file content ....", "│ file content ....", "└─"} {
		if rows[i+2] != want {
			t.Fatalf("row %d = %q, want %q", i+2, rows[i+2], want)
		}
	}
	if n := strings.Count(strings.Join(rows, "\n"), "└─"); n != 1 {
		t.Fatalf("branch must close exactly once, got %d:\n%s", n, strings.Join(rows, "\n"))
	}
	// An open card keeps the same shape, pending rather than empty.
	open := toolCardBlock("c2", "read_file {}")
	openRows := strings.Split(ansi.Strip(renderBlock(open, st, tagged(), 80)), "\n")
	if len(openRows) != 4 || !strings.HasPrefix(openRows[1], "… ") || openRows[2] != "│ …" || openRows[3] != "└─" {
		t.Fatalf("open card must show one pending row: %q", renderBlock(open, st, tagged(), 80))
	}
}

// A wrapped output line contributes several rows, and only the very last
// one may close the branch.
func TestToolBranchClosesOnFinalFragment(t *testing.T) {
	st := defaultStyles()
	b := toolCardBlock("c1", "run x")
	b.result = strings.Repeat("word ", 40)
	b.open = false
	rows := strings.Split(ansi.Strip(renderBlock(b, st, renderOpts{expand: true, labels: true}, 40)), "\n")
	for _, row := range rows {
		if n := lipgloss.Width(row); n > 40 {
			t.Fatalf("row %d cols past 40: %q", n, row)
		}
	}
	last := len(rows) - 1
	if rows[last] != "└─" {
		t.Fatalf("last row must close the branch: %q", rows[last])
	}
	for i, row := range rows[2:last] {
		if !strings.HasPrefix(row, "│ ") {
			t.Fatalf("row %d must stay railed: %q", i+2, row)
		}
	}
}

// Blocks are separated by one blank row and by nothing else. Horizontal
// rules belong to pane boundaries, and turn-specific extra spacing made
// the gap between blocks depend on which role happened to open a turn.
func TestBlocksSeparateWithOneBlankRow(t *testing.T) {
	m := sizeModel(t, testModel())
	m.appendBlock(answerBlock("first"))
	m.appendBlock(userBlock("second"))
	view := ansi.Strip(m.transcript.Viewport.View())
	if strings.Contains(view, "─") {
		t.Fatalf("transcript must use no rules: %q", view)
	}
	// The viewport pads every row to the pane width, so the gap is measured
	// with the padding tolerated rather than against a bare line ending.
	if !regexp.MustCompile(`\n *\nYOU *\n\[INPUT\]`).MatchString(view) {
		t.Fatalf("expected one blank row before the next block: %q", view)
	}
	m2 := sizeModel(t, testModel())
	m2.appendBlock(markerBlock("new task"))
	m2.appendBlock(userBlock("go"))
	if n := strings.Count(m2.transcript.Viewport.View(), "─"); n != 0 {
		t.Fatalf("marker/echo pair must not add rules, got %d", n)
	}
}

func TestToolCardAttachesResult(t *testing.T) {
	m := sizeModel(t, testModel())
	updated, _ := m.testUpdate(eventMsg(events.Event{
		Name: "step",
		Fields: map[string]any{
			"tool_calls": []any{map[string]any{"id": "c1", "name": "read_file", "args": "{}"}},
		},
	}))
	mm := updated.(*Model)
	if len(mm.transcript.blocks) != 1 || !mm.transcript.blocks[0].open {
		t.Fatalf("call must open a card: %+v", mm.transcript.blocks)
	}
	if view := mm.transcript.Viewport.View(); !strings.Contains(view, "read_file") || !strings.Contains(view, "…") {
		t.Fatalf("open card missing call/pending: %q", view)
	}
	updated, _ = mm.testUpdate(eventMsg(events.Event{
		Name:   "tool_result",
		Fields: map[string]any{"call_id": "c1", "text": "ok"},
	}))
	mm = updated.(*Model)
	if len(mm.transcript.blocks) != 1 || mm.transcript.blocks[0].open {
		t.Fatalf("result must land on the card: %+v", mm.transcript.blocks)
	}
	if view := mm.transcript.Viewport.View(); !strings.Contains(view, "ok") || !strings.Contains(view, "✓") {
		t.Fatalf("closed card missing result/check: %q", view)
	}
	// Unknown IDs never corrupt a card: standalone block instead.
	updated, _ = mm.testUpdate(eventMsg(events.Event{
		Name:   "tool_result",
		Fields: map[string]any{"call_id": "zz", "text": "stray"},
	}))
	if mm := updated.(*Model); len(mm.transcript.blocks) != 2 || mm.transcript.blocks[1].role != roleResult {
		t.Fatalf("stray result must stand alone: %+v", mm.transcript.blocks)
	}
}

func TestToolCardFailedGlyph(t *testing.T) {
	m := sizeModel(t, testModel())
	updated, _ := m.testUpdate(eventMsg(events.Event{
		Name: "step",
		Fields: map[string]any{
			"tool_calls": []any{map[string]any{"id": "c2", "name": "run_shell", "args": "{}"}},
		},
	}))
	mm := updated.(*Model)
	updated, _ = mm.testUpdate(eventMsg(events.Event{
		Name:   "tool_result",
		Fields: map[string]any{"call_id": "c2", "text": "error: boom"},
	}))
	mm = updated.(*Model)
	if !mm.transcript.blocks[0].failed {
		t.Fatal("error-prefixed result must flag the card")
	}
	if view := mm.transcript.Viewport.View(); !strings.Contains(view, "×") {
		t.Fatalf("failed card missing cross: %q", view)
	}
}

func TestFindingRendersWarning(t *testing.T) {
	m := sizeModel(t, testModel())
	updated, _ := m.testUpdate(eventMsg(events.Event{
		Name: "finding",
		Fields: map[string]any{
			"scope": "per-edit", "rule": "secret-aws", "path": "a.go",
			"line": 3, "summary": "possible secret (secret-aws)",
		},
	}))
	mm := updated.(*Model)
	if len(mm.transcript.blocks) != 1 || mm.transcript.blocks[0].role != roleWarning {
		t.Fatalf("finding must be a warning block: %+v", mm.transcript.blocks)
	}
	if view := mm.transcript.Viewport.View(); !strings.Contains(view, "[WARNING]") || !strings.Contains(view, "a.go:3") {
		t.Fatalf("warning missing label/content: %q", view)
	}
	// Rule-less events append nothing, never a blank warning.
	updated, _ = mm.testUpdate(eventMsg(events.Event{Name: "finding", Fields: map[string]any{}}))
	if mm := updated.(*Model); len(mm.transcript.blocks) != 1 {
		t.Fatal("malformed finding must not append")
	}
}

func TestNudgeRendersMarker(t *testing.T) {
	m := sizeModel(t, testModel())
	updated, _ := m.testUpdate(eventMsg(events.Event{
		Name:   "nudge",
		Fields: map[string]any{"kind": "verify", "text": "[harness] check your work"},
	}))
	mm := updated.(*Model)
	if len(mm.transcript.blocks) != 1 || mm.transcript.blocks[0].role != roleMarker {
		t.Fatalf("nudge must be a marker block: %+v", mm.transcript.blocks)
	}
	// Provenance is carried by the SYSTEM name row and the [HARNESS]
	// label, not by a second "[harness]" tag inside the body.
	view := ansi.Strip(mm.transcript.Viewport.View())
	if !strings.Contains(view, "SYSTEM") || !strings.Contains(view, "[HARNESS]") || !strings.Contains(view, "check your work") {
		t.Fatalf("provenance mark missing: %q", view)
	}
	if strings.Contains(view, "[harness]") {
		t.Fatalf("label row already says HARNESS: %q", view)
	}
	// The stored text keeps the prefix: only the display drops it.
	if !strings.HasPrefix(mm.transcript.blocks[0].text, "[harness] ") {
		t.Fatalf("stored marker text mutated: %q", mm.transcript.blocks[0].text)
	}
	// Empty nudges append nothing.
	updated, _ = mm.testUpdate(eventMsg(events.Event{Name: "nudge", Fields: map[string]any{}}))
	if mm := updated.(*Model); len(mm.transcript.blocks) != 1 {
		t.Fatal("empty nudge must not append")
	}
}

func TestHarnessReplyRendersNormally(t *testing.T) {
	m := sizeModel(t, testModel())
	m.state = stDone
	updated, _ := m.testUpdate(eventMsg(events.Event{
		Name: "step", Fields: map[string]any{"text": "real answer"},
	}))
	mm := updated.(*Model)
	updated, _ = mm.testUpdate(eventMsg(events.Event{
		Name: "nudge", Fields: map[string]any{"kind": "verify", "text": "[harness] check"},
	}))
	mm = updated.(*Model)
	updated, _ = mm.testUpdate(eventMsg(events.Event{
		Name: "step", Fields: map[string]any{"text": "noted", "harness_reply": true},
	}))
	mm = updated.(*Model)
	if len(mm.transcript.blocks) != 3 {
		t.Fatalf("blocks = %d", len(mm.transcript.blocks))
	}
	// No hiding anymore: nudges and harness replies render like
	// everything else (hiding model-visible content misleads). The nudge
	// opens its own SYSTEM run, so the reply after it reopens ASSISTANT.
	view := mm.transcript.Viewport.View()
	for _, want := range []string{"real answer", "[HARNESS]", "check", "noted"} {
		if !strings.Contains(view, want) {
			t.Fatalf("missing %q: %q", want, view)
		}
	}
}

// Every block carries a time when the transcript knows one. A reader
// correlating a slow tool call with a reply needs all of them, not just
// the conversational ones.
func TestStampsPolicy(t *testing.T) {
	allRoles := []role{roleAnswer, roleUser, roleThink, roleTool, roleResult,
		roleGate, roleMarker, roleError, roleWarning}
	st := defaultStyles()
	stampPattern := regexp.MustCompile(`\[[0-2][0-9]:[0-5][0-9]\]`)
	at := time.Date(2026, 10, 3, 14, 22, 0, 0, time.Local)
	for _, r := range allRoles {
		b := block{role: r, text: "body", at: at}
		b.callID = "c1"
		if r == roleTool {
			b.open = true
		}
		if out := renderBlock(b, st, tagged(), 80); !stampPattern.MatchString(out) {
			t.Fatalf("role %v must stamp: %q", r, out)
		}
	}
	// Reopened history records no display times, so nothing is invented.
	for _, r := range allRoles {
		if out := renderBlock(block{role: r, text: "body"}, st, tagged(), 80); stampPattern.MatchString(out) {
			t.Fatalf("zero-time role %v must not stamp: %q", r, out)
		}
	}
}

// Time to first token appears in the details panel, beside elapsed, once
// a token has actually arrived. Fixed clock offsets, no tick flakes.
// The first streamed chunk stamps firstToken; a new turn clears it.
func TestFirstTokenStampedAndReset(t *testing.T) {
	m := sizeModel(t, testModel())
	m.state = stRunning
	updated, _ := m.testUpdate(eventMsg(events.Event{Name: "delta", Fields: map[string]any{"text": "hel"}}))
	mm := updated.(*Model)
	if mm.firstToken.IsZero() {
		t.Fatal("first delta did not stamp firstToken")
	}
	mm.send = func(tea.Msg) {}
	mm.startRun(func(context.Context) (string, error) { return "", nil })
	if !mm.firstToken.IsZero() {
		t.Fatal("new turn did not reset firstToken")
	}
}

func TestToolCardUsesTypedStatus(t *testing.T) {
	m := sizeModel(t, testModel())
	m.appendBlock(toolCardBlock("test", "run_shell"))
	m.handleEvent(events.Event{Name: "tool_result", Fields: map[string]any{"call_id": "test", "text": "ordinary compiler output", "failed": true}})
	if !m.transcript.blocks[len(m.transcript.blocks)-1].failed {
		t.Fatal("typed failure lost")
	}
	m.appendBlock(toolCardBlock("read", "read_file"))
	m.handleEvent(events.Event{Name: "tool_result", Fields: map[string]any{"call_id": "read", "text": "error: is valid file content", "failed": false}})
	if m.transcript.blocks[len(m.transcript.blocks)-1].failed {
		t.Fatal("file prose overrode successful status")
	}
}
