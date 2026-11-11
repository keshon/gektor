package agent

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	tea "charm.land/bubbletea/v2"
	"charm.land/lipgloss/v2"
	"github.com/charmbracelet/x/ansi"

	"github.com/keshon/gektor/internal/events"
	"github.com/keshon/gektor/internal/llm"
)

func TestHeaderMenusPreserveDraftAndFrame(t *testing.T) {
	for _, width := range []int{40, 80, 120} {
		m := sizeModel(t, testModel())
		m.state = stDone
		m.width(tea.WindowSizeMsg{Width: width, Height: 24})
		m.input.SetValue("draft\nsecond line")
		m.fitInput()
		m.fitBottom()
		m.testKey(tea.KeyPressMsg{Code: tea.KeyF9})
		m.testKey(tea.KeyPressMsg{Code: tea.KeyEnter})
		if m.chrome.Header.Menu != "session" {
			t.Fatal("session menu not opened")
		}
		for _, row := range strings.Split(m.testView().Content, "\n") {
			if lipgloss.Width(row) != width {
				t.Fatalf("frame width %d: %q", width, ansi.Strip(row))
			}
		}
		m.testKey(tea.KeyPressMsg{Code: 'x', Text: "x"})
		m.testKey(tea.KeyPressMsg{Code: tea.KeyEscape})
		m.testKey(tea.KeyPressMsg{Code: tea.KeyEscape})
		if m.chrome.Header.Focused || m.input.Value() != "draft\nsecond line" {
			t.Fatal("header altered draft or retained focus")
		}
	}
}

func TestHeaderMouseActionsAndGateIsolation(t *testing.T) {
	m := sizeModel(t, testModel())
	m.state = stDone
	m.testUpdate(tea.MouseClickMsg{X: m.promptOffset() + m.headerBadges()[0].Start + 1, Y: 2, Button: tea.MouseLeft})
	if m.chrome.Header.Menu != "" || m.chrome.Header.Focused {
		t.Fatal("status labels opened a dropdown")
	}
	m.state = stPermission
	m.testUpdate(tea.MouseClickMsg{X: m.promptOffset() + m.headerBadges()[0].Start + 1, Y: 2, Button: tea.MouseLeft})
	m.testKey(tea.KeyPressMsg{Code: tea.KeyF9})
	if m.chrome.Header.Focused {
		t.Fatal("header hid permission prompt")
	}
}

func TestHeaderContextPersistsAndRestoresEstimate(t *testing.T) {
	m := sizeModel(t, testModel())
	m.modelName = "test-model"
	m.stateFile = filepath.Join(t.TempDir(), "state.json")
	m.handleEvent(events.Event{Name: "usage", Fields: map[string]any{"prompt": 4100}})
	if m.tokens != 4100 || !m.usage.thisRun {
		t.Fatal("usage not captured")
	}
	m.persistUsage()
	if _, err := os.Stat(filepath.Join(filepath.Dir(m.stateFile), "usage.json")); err != nil {
		t.Fatal(err)
	}
	m.restoreContext([]llm.Message{{Role: llm.RoleUser, Content: "hello", Reasoning: strings.Repeat("ignored", 1000)}})
	if !m.tokensEst || m.tokens != 6 || m.usage.last.Tokens != 4100 || m.usage.source != "Saved history estimate" {
		t.Fatalf("restored context: %d %+v", m.tokens, m.usage.last)
	}
	m.clearContext()
	if m.tokens != 0 || !m.usage.at.IsZero() || m.usage.last.Tokens != 0 {
		t.Fatal("new session retained context")
	}
}

func TestContextBreakdownDistinguishesEstimatesAndMeasuredCounts(t *testing.T) {
	m := sizeModel(t, testModel())
	m.handleEvent(events.Event{Name: "prompt_budget", Fields: map[string]any{"report": llm.ContextReport{EstimatedTokens: 500, BackendTokens: 600, CachedTokens: 400}}})
	if m.usage.breakdown.BackendTokens != 600 {
		t.Fatal("measurement missing")
	}
	m.handleEvent(events.Event{Name: "prompt_budget", Fields: map[string]any{"report": llm.ContextReport{EstimatedTokens: 700}}})
	if m.usage.breakdown.BackendTokens != 0 || m.usage.breakdown.EstimatedTokens != 700 {
		t.Fatal("old measurement attributed to new request")
	}
}

func TestEmptyHeaderShowsZeroMetricsAndTwoSpaceGaps(t *testing.T) {
	m := sizeModelTo(t, testModel(), 140, 25)
	m.state, m.limit, m.modelName = stDone, 65536, "model.gguf"
	line := ansi.Strip(m.badgeLine())
	if !strings.Contains(line, "ACT  READY  0/65.5k  model.gguf") || !strings.HasSuffix(line, "STEP 0/25  00:00 ") {
		t.Fatalf("header: %q", line)
	}
}

func TestHeaderModelUsesSpaceAndRunIsRightAligned(t *testing.T) {
	name := "NVIDIA-Nemotron-3.5-Bolt-74B-A9B-long-model-name.gguf"
	for _, width := range []int{40, 80, 160} {
		m := sizeModelTo(t, testModel(), width, 25)
		m.modelName = name
		line := ansi.Strip(m.badgeLine())
		if width == 160 && !strings.Contains(line, name) {
			t.Fatalf("model truncated despite available space: %q", line)
		}
		previousEnd := 0
		for _, b := range m.headerBadges() {
			if b.Start < previousEnd || b.End > m.transcript.Viewport.Width() {
				t.Fatalf("overlapping or overflowing badge: %+v", b)
			}
			previousEnd = b.End
			if b.ID == "run" && (b.End != m.transcript.Viewport.Width() || !strings.HasSuffix(line, b.Label+" ")) {
				t.Fatalf("run not right aligned: %q %+v", line, b)
			}
		}
	}
}

func TestHistoricalModelAndContextBecomeLiveIndependently(t *testing.T) {
	m := sizeModel(t, testModel())
	m.stateFile = filepath.Join(t.TempDir(), "state.json")
	m.modelName = `D:\models\one\two\old.gguf`
	m.handleEvent(events.Event{Name: "usage", Fields: map[string]any{"prompt": 4100}})
	m.persistUsage()
	m.modelName = "/models/new/current.gguf"
	m.restoreContext([]llm.Message{{Role: llm.RoleUser, Content: "previous task"}})
	if !m.usage.historical || m.usage.liveModel || modelLabel(m.displayedModel()) != "old.gguf" || m.tokens == 0 {
		t.Fatalf("historical header: %+v model=%q", m.usage, m.displayedModel())
	}
	m.handleEvent(events.Event{Name: "delta", Fields: map[string]any{"text": "reply"}})
	if modelLabel(m.displayedModel()) != "current.gguf" || !m.usage.historical {
		t.Fatal("model should be live while context awaits fresh usage")
	}
	m.handleEvent(events.Event{Name: "usage", Fields: map[string]any{"prompt": 5200}})
	if m.usage.historical || m.tokens != 5200 || m.usage.last.Model != m.modelName {
		t.Fatal("fresh usage did not replace historical values")
	}
	m.persistUsage()
	m.restoreContext(nil)
	if modelLabel(m.displayedModel()) != "current.gguf" {
		t.Fatal("saved model was not updated")
	}
}

func TestHeaderEndpointSanitizationAndLongDetails(t *testing.T) {
	got := safeEndpoint("https://user:secret@example.com/v1?api_key=secret#secret")
	if got != "https://example.com/v1" {
		t.Fatalf("unsafe endpoint: %q", got)
	}
	m := sizeModel(t, testModel())
	m.width(tea.WindowSizeMsg{Width: 40, Height: 14})
	m.chrome.Header.Menu = "model"
	m.modelName = strings.Repeat("long-model-", 10)
	m.chrome.Header.Focused = true
	items := m.testControls().HeaderItems()
	var joined string
	for _, item := range items {
		joined += item.Text
	}
	if !strings.Contains(joined, m.modelName) {
		t.Fatal("long model lost from details")
	}
	m.chrome.Header.Row = len(items) - 1
	rows := strings.Split(m.testControls().OverlayHeader(m.testView().Content), "\n")
	if len(rows) != 14 {
		t.Fatal("menu changed frame height")
	}
}

func TestHeaderStopWorksFromSessionPaneAndGateClosesMenu(t *testing.T) {
	m := sizeModel(t, testModel())
	m.state = stRunning
	cancelled := false
	m.cancel = func() { cancelled = true }
	m.chrome.Header.Focused = true
	m.chrome.Header.Menu = "activity"
	m.chrome.Header.Row = len(m.testControls().HeaderItems()) - 1
	m.testKey(tea.KeyPressMsg{Code: tea.KeyEnter})
	if !cancelled || m.state != stStopping || !m.interrupted {
		t.Fatal("Stop action was routed to the session pane")
	}
	m.chrome.Header.Focused = true
	m.chrome.Header.Menu = "model"
	m.handleEvent(events.Event{Name: "awaiting_input", Fields: map[string]any{"kind": "ask", "prompt": "Confirm?"}})
	if m.chrome.Header.Focused || m.chrome.Header.Menu != "" {
		t.Fatal("menu obscured new gate")
	}
}

func TestHeaderDetailColumns(t *testing.T) {
	m := sizeModel(t, testModel())
	for _, menu := range []string{"activity", "model", "context", "run"} {
		m.chrome.Header.Menu = menu
		column := -1
		for _, item := range m.testControls().HeaderItems() {
			colon := strings.Index(item.Text, ":")
			if colon < 0 || item.Command != "" {
				continue
			}
			value := colon + 1
			for value < len(item.Text) && item.Text[value] == ' ' {
				value++
			}
			if column < 0 {
				column = value
			}
			if value != column {
				t.Fatalf("%s: uneven value column: %q", menu, item.Text)
			}
		}
	}
	m.chrome.Header.Menu = "context"
	m.tokens, m.limit, m.tokensEst = 100, 1000, true
	if got := m.testControls().HeaderItems()[0].Text; !strings.HasPrefix(got, "Context:") || strings.Contains(got, "ctx") {
		t.Fatalf("context label: %q", got)
	}
}

// The View menu divides what the reader hides from what they can do.
// Listing a scroll action among display filters hid that they are
// different kinds of thing.
func TestViewMenuZonesAndItems(t *testing.T) {
	m := sizeModelTo(t, testModel(), 80, 30)
	m.chrome.Header.Menu = "view"
	items := m.testControls().HeaderItems()
	want := []string{"Thinking", "Tool calls", "System messages", "Speaker names", "Message labels", "", "Jump to latest"}
	if len(items) != len(want) {
		t.Fatalf("view menu has %d rows, want %d: %+v", len(items), len(want), items)
	}
	for i, w := range want {
		if items[i].Text != w {
			t.Fatalf("row %d = %q, want %q", i, items[i].Text, w)
		}
	}
	// Exactly one separator, between the toggles and the commands.
	sep := -1
	for i, item := range items {
		if item.Text == "" && item.Command == "" {
			sep = i
		}
	}
	if sep != 5 {
		t.Fatalf("separator at row %d, want 5", sep)
	}
	cmds := []string{"toggle-details", "toggle-tools", "toggle-sys", "toggle-names", "toggle-labels", "", "latest"}
	for i, c := range cmds {
		if items[i].Command != c {
			t.Fatalf("row %d command = %q, want %q", i, items[i].Command, c)
		}
	}
}

// A checkmark states the condition its own label names. Marking
// "Hide Sessions" on when the sidebar is visible would tell the reader
// the opposite of the truth.
func TestViewMenuCheckmarksFollowState(t *testing.T) {
	m := sizeModelTo(t, testModel(), 80, 30)
	m.chrome.Header.Menu = "view"
	if m.transcript.compact != true {
		t.Fatal("precondition: transcript starts truncated")
	}
	for _, tc := range []struct {
		name            string
		hideSYS, labels bool
		want            string
	}{
		{"default", false, false, "│  Thinking"},
		{"sys shown", false, false, "│✓ System messages"},
		{"sys hidden", true, false, "│  System messages"},
		{"labels hidden", false, true, "│  Message labels"},
	} {
		m.hideSYS, m.hideLabels = tc.hideSYS, tc.labels
		rows := strings.Join(popupRows(t, m), "\n")
		if !strings.Contains(rows, tc.want) {
			t.Fatalf("%s: missing %q in:\n%s", tc.name, tc.want, rows)
		}
	}
}

// popupRows returns the rendered popup's own rows, cut out of the
// full-screen frame the overlay draws them over.
func popupRows(t *testing.T, m *Model) []string {
	t.Helper()
	x, width, _, _ := m.testControls().HeaderGeometry()
	var out []string
	for _, row := range strings.Split(ansi.Strip(m.testControls().OverlayHeader(m.testView().Content)), "\n") {
		runes := []rune(row)
		if x+width > len(runes) {
			continue
		}
		out = append(out, string(runes[x:x+width]))
	}
	return out
}

// A checkmark fills the label indent every menu row already carries. It
// must sit one cell in from the border, and every label must start in
// the same column whether its row is ticked or not.
func TestCheckmarkUsesTheLabelIndent(t *testing.T) {
	// Each menu is put in a state where at least one row is ticked;
	// otherwise there is nothing to measure.
	for _, tc := range []struct {
		menu string
		set  func(*Model)
	}{
		{"view", func(m *Model) { m.transcript.compact = true }},
		{"execution", func(m *Model) { m.plan = false }},
	} {
		m := sizeModelTo(t, testModel(), 80, 30)
		tc.set(m)
		m.chrome.Header.Menu = tc.menu
		rows := popupRows(t, m)
		marks, labels := 0, 0
		for _, row := range rows {
			if !strings.HasPrefix(row, "│") || !strings.HasSuffix(row, "│") {
				continue // frame and separator rows
			}
			body := strings.TrimSuffix(strings.TrimPrefix(row, "│"), "│")
			if strings.TrimSpace(body) == "" {
				continue
			}
			labels++
			if strings.Contains(body, "✓") {
				marks++
				if !strings.HasPrefix(body, "✓ ") {
					t.Fatalf("%s: tick is not in the indent: %q", tc.menu, row)
				}
			}
			// "✓ " and "  " are both two cells, so the label never moves:
			// the offset of the first real character is the same either way.
			label := strings.TrimLeft(body, " ✓")
			if at := len([]rune(body)) - len([]rune(label)); at != 2 {
				t.Fatalf("%s: label starts at %d, want 2: %q", tc.menu, at, row)
			}
		}
		if marks == 0 || labels == 0 {
			t.Fatalf("%s: measured %d rows, %d ticked:\n%s", tc.menu, labels, marks, strings.Join(rows, "\n"))
		}
	}
}

// The mark never reserves a column. Ticking rows must not widen the
// popup, and the popup must stay at its natural width: the longest
// label-plus-shortcut pair ("Truncate AI reply" + "Ctrl+G") plus the
// frame. A reserved mark column would make it 32 instead of 30.
func TestMarkCostsNoWidth(t *testing.T) {
	m := sizeModelTo(t, testModel(), 80, 30)
	m.chrome.Header.Menu = "view"
	m.hideSYS = false
	_, plain, _, _ := m.testControls().HeaderGeometry()
	m.hideSYS = true
	_, ticked, _, _ := m.testControls().HeaderGeometry()
	if plain != ticked {
		t.Fatalf("ticking rows changed menu width: %d -> %d", plain, ticked)
	}
	const want = 29
	if ticked != want {
		t.Fatalf("menu width %d, want %d — a mark column is being reserved", ticked, want)
	}
}

// A menu with nothing to toggle draws no mark and loses nothing.
func TestMenusWithoutTogglesDrawNoMark(t *testing.T) {
	for _, menu := range []string{"help", "session", "model"} {
		m := sizeModelTo(t, testModel(), 80, 30)
		m.chrome.Header.Menu = menu
		for _, row := range popupRows(t, m) {
			if strings.Contains(row, "✓") {
				t.Fatalf("%s drew a mark it has no toggle for: %q", menu, row)
			}
		}
	}
}

// Plan/Act moved from an inline tick to the shared indent.
func TestModeMenuUsesSharedCheckmarkIndent(t *testing.T) {
	m := sizeModelTo(t, testModel(), 80, 30)
	m.chrome.Header.Menu = "mode"
	m.plan = false
	rows := strings.Join(popupRows(t, m), "\n")
	if !strings.Contains(rows, "✓ ACT") || strings.Contains(rows, "✓  ACT") {
		t.Fatalf("act mode tick: %s", rows)
	}
	m.plan = true
	rows = strings.Join(popupRows(t, m), "\n")
	if !strings.Contains(rows, "✓ PLAN") || strings.Contains(rows, "✓ ACT") {
		t.Fatalf("plan mode tick: %s", rows)
	}
}

// A separator is structure, not a target: keyboard navigation must skip
// it or Down would stop on a row that does nothing.
func TestKeyboardNavigationSkipsViewSeparator(t *testing.T) {
	// 110 columns is where the Sessions pane exists, so the sidebar
	// toggle is enabled and reachable by keyboard.
	m := sizeModelTo(t, testModel(), 120, 30)
	m.chrome.Header.Focused = true
	m.chrome.Header.Global = true
	m.chrome.Header.Menu = "view"
	m.chrome.Header.Row = m.testControls().HeaderActionRow(0, 1)
	for _, want := range []string{"toggle-details", "toggle-tools", "toggle-sys"} {
		if got := m.testControls().HeaderItems()[m.chrome.Header.Row].Command; got != want {
			t.Fatalf("row = %q, want %q", got, want)
		}
		m.chrome.Header.Row = m.testControls().HeaderActionRow(m.chrome.Header.Row+1, 1)
	}
}
