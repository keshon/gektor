package tui

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	tea "charm.land/bubbletea/v2"
	"charm.land/lipgloss/v2"
	"github.com/charmbracelet/x/ansi"
	"github.com/keshon/tars/internal/api"
	"github.com/keshon/tars/internal/llm"
)

func TestHeaderMenusPreserveDraftAndFrame(t *testing.T) {
	for _, width := range []int{40, 80, 120} {
		m := sizeModel(t, testModel())
		m.state = stDone
		m.width(tea.WindowSizeMsg{Width: width, Height: 24})
		m.input.SetValue("draft\nsecond line")
		m.fitInput()
		m.fitBottom()
		m.handleKey(tea.KeyPressMsg{Code: tea.KeyF9})
		m.handleKey(tea.KeyPressMsg{Code: tea.KeyEnter})
		if m.header.menu != "session" {
			t.Fatal("session menu not opened")
		}
		for _, row := range strings.Split(m.View().Content, "\n") {
			if lipgloss.Width(row) != width {
				t.Fatalf("frame width %d: %q", width, ansi.Strip(row))
			}
		}
		m.handleKey(tea.KeyPressMsg{Code: 'x', Text: "x"})
		m.handleKey(tea.KeyPressMsg{Code: tea.KeyEscape})
		m.handleKey(tea.KeyPressMsg{Code: tea.KeyEscape})
		if m.header.focused || m.input.Value() != "draft\nsecond line" {
			t.Fatal("header altered draft or retained focus")
		}
	}
}

func TestHeaderMouseActionsAndGateIsolation(t *testing.T) {
	m := sizeModel(t, testModel())
	m.state = stDone
	m.Update(tea.MouseClickMsg{X: m.promptOffset() + m.headerBadges()[0].start + 1, Y: 2, Button: tea.MouseLeft})
	if m.header.menu != "" || m.header.focused {
		t.Fatal("status labels opened a dropdown")
	}
	m.state = stPermission
	m.Update(tea.MouseClickMsg{X: m.promptOffset() + m.headerBadges()[0].start + 1, Y: 2, Button: tea.MouseLeft})
	m.handleKey(tea.KeyPressMsg{Code: tea.KeyF9})
	if m.header.focused {
		t.Fatal("header hid permission prompt")
	}
}

func TestHeaderContextPersistsAndRestoresEstimate(t *testing.T) {
	m := sizeModel(t, testModel())
	m.modelName = "test-model"
	m.stateFile = filepath.Join(t.TempDir(), "state.json")
	m.handleEvent(api.Event{Name: "usage", Fields: map[string]any{"prompt": float64(4100)}})
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

func TestHeaderEndpointSanitizationAndLongDetails(t *testing.T) {
	got := safeEndpoint("https://user:secret@example.com/v1?api_key=secret#secret")
	if got != "https://example.com/v1" {
		t.Fatalf("unsafe endpoint: %q", got)
	}
	m := sizeModel(t, testModel())
	m.width(tea.WindowSizeMsg{Width: 40, Height: 14})
	m.header.menu = "model"
	m.modelName = strings.Repeat("long-model-", 10)
	m.header.focused = true
	items := m.headerItems()
	var joined string
	for _, item := range items {
		joined += item.text
	}
	if !strings.Contains(joined, m.modelName) {
		t.Fatal("long model lost from details")
	}
	m.header.row = len(items) - 1
	rows := strings.Split(m.overlayHeader(m.View().Content), "\n")
	if len(rows) != 14 {
		t.Fatal("menu changed frame height")
	}
}

func TestHeaderStopWorksFromSessionPaneAndGateClosesMenu(t *testing.T) {
	m := sizeModel(t, testModel())
	m.state = stRunning
	m.navFocused = true
	cancelled := false
	m.cancel = func() { cancelled = true }
	m.header.focused = true
	m.header.menu = "activity"
	m.header.row = len(m.headerItems()) - 1
	m.handleKey(tea.KeyPressMsg{Code: tea.KeyEnter})
	if !cancelled || m.state != stStopping || !m.interrupted {
		t.Fatal("Stop action was routed to the session pane")
	}
	m.header.focused = true
	m.header.menu = "model"
	m.handleEvent(api.Event{Name: "awaiting_input", Fields: map[string]any{"kind": "ask", "prompt": "Confirm?"}})
	if m.header.focused || m.header.menu != "" {
		t.Fatal("menu obscured new gate")
	}
}

func TestHeaderDetailColumns(t *testing.T) {
	m := sizeModel(t, testModel())
	for _, menu := range []string{"activity", "model", "context", "run"} {
		m.header.menu = menu
		column := -1
		for _, item := range m.headerItems() {
			colon := strings.Index(item.text, ":")
			if colon < 0 || item.command != "" {
				continue
			}
			value := colon + 1
			for value < len(item.text) && item.text[value] == ' ' {
				value++
			}
			if column < 0 {
				column = value
			}
			if value != column {
				t.Fatalf("%s: uneven value column: %q", menu, item.text)
			}
		}
	}
	m.header.menu = "context"
	m.tokens, m.limit, m.tokensEst = 100, 1000, true
	if got := m.headerItems()[0].text; !strings.HasPrefix(got, "Context:") || strings.Contains(got, "ctx") {
		t.Fatalf("context label: %q", got)
	}
}

// The View menu divides what the reader hides from what they can do.
// Listing a scroll action among display filters hid that they are
// different kinds of thing.
func TestViewMenuZonesAndItems(t *testing.T) {
	m := sizeModelTo(t, testModel(), 80, 30)
	m.header.menu = "view"
	items := m.headerItems()
	want := []string{"Hide Sessions", "Truncate AI reply", "Hide SYS messages", "Hide labels", "", "Jump to latest", "Select Theme…"}
	if len(items) != len(want) {
		t.Fatalf("view menu has %d rows, want %d: %+v", len(items), len(want), items)
	}
	for i, w := range want {
		if items[i].text != w {
			t.Fatalf("row %d = %q, want %q", i, items[i].text, w)
		}
	}
	// Exactly one separator, between the toggles and the commands.
	sep := -1
	for i, item := range items {
		if item.text == "" && item.command == "" {
			sep = i
		}
	}
	if sep != 4 {
		t.Fatalf("separator at row %d, want 4", sep)
	}
	cmds := []string{"toggle-sidebar", "toggle-details", "toggle-sys", "toggle-labels", "", "latest", "theme"}
	for i, c := range cmds {
		if items[i].command != c {
			t.Fatalf("row %d command = %q, want %q", i, items[i].command, c)
		}
	}
}

// A checkmark states the condition its own label names. Marking
// "Hide Sessions" on when the sidebar is visible would tell the reader
// the opposite of the truth.
func TestViewMenuCheckmarksFollowState(t *testing.T) {
	m := sizeModelTo(t, testModel(), 80, 30)
	m.header.menu = "view"
	if m.transcript.compact != true {
		t.Fatal("precondition: transcript starts truncated")
	}
	for _, tc := range []struct {
		name                     string
		sidebar, hideSYS, labels bool
		want                     string
	}{
		{"default", false, false, false, "│✓ Truncate AI reply   Ctrl+G│"},
		{"sessions hidden", true, false, false, "│✓ Hide Sessions       Ctrl+B│"},
		{"sys hidden", false, true, false, "│✓ Hide SYS messages         │"},
		{"labels hidden", false, false, true, "│✓ Hide labels               │"},
	} {
		m.sidebarHidden, m.hideSYS, m.hideLabels = tc.sidebar, tc.hideSYS, tc.labels
		rows := strings.Join(popupRows(t, m), "\n")
		if !strings.Contains(rows, tc.want) {
			t.Fatalf("%s: missing %q in:\n%s", tc.name, tc.want, rows)
		}
	}
}

// popupRows returns the rendered popup's own rows, cut out of the
// full-screen frame the overlay draws them over.
func popupRows(t *testing.T, m *model) []string {
	t.Helper()
	x, width, _, _ := m.headerGeometry()
	var out []string
	for _, row := range strings.Split(ansi.Strip(m.overlayHeader(m.View().Content)), "\n") {
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
		set  func(*model)
	}{
		{"view", func(m *model) { m.transcript.compact = true }},
		{"workflow", func(m *model) { m.plan = false }},
	} {
		m := sizeModelTo(t, testModel(), 80, 30)
		tc.set(m)
		m.header.menu = tc.menu
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
	m.header.menu = "view"
	m.sidebarHidden, m.hideSYS = false, false
	_, plain, _, _ := m.headerGeometry()
	m.sidebarHidden, m.hideSYS = true, true
	_, ticked, _, _ := m.headerGeometry()
	if plain != ticked {
		t.Fatalf("ticking rows changed menu width: %d -> %d", plain, ticked)
	}
	const want = 30
	if ticked != want {
		t.Fatalf("menu width %d, want %d — a mark column is being reserved", ticked, want)
	}
}

// A menu with nothing to toggle draws no mark and loses nothing.
func TestMenusWithoutTogglesDrawNoMark(t *testing.T) {
	for _, menu := range []string{"help", "session", "model"} {
		m := sizeModelTo(t, testModel(), 80, 30)
		m.header.menu = menu
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
	m.header.menu = "mode"
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
	m.header.focused = true
	m.header.global = true
	m.header.menu = "view"
	m.header.row = m.headerActionRow(0, 1)
	for _, want := range []string{"toggle-sidebar", "toggle-details", "toggle-sys", "toggle-labels"} {
		if got := m.headerItems()[m.header.row].command; got != want {
			t.Fatalf("row = %q, want %q", got, want)
		}
		m.header.row = m.headerActionRow(m.header.row+1, 1)
	}
}
