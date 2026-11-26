package agent

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"

	tea "charm.land/bubbletea/v2"
	"github.com/charmbracelet/x/ansi"
	"github.com/keshon/gektor/internal/config"
	"github.com/keshon/gektor/internal/llm"
	"github.com/keshon/gektor/pkg/tui/screen"
)

func TestModernConversationGeometry(t *testing.T) {
	defer setTheme("dark")
	defer screen.SetModern(false)
	for _, theme := range []string{"dark", "light", "one-half-dark", "solarized-light"} {
		for _, width := range []int{40, 80, 120} {
			m := sizeModelTo(t, testModel(), width, 30)
			m.applyAppearance(config.Appearance{Style: "modern", Frame: "minimal"})
			m.state = stDone
			m.applyTheme(theme)
			m.input.SetValue("Keep the changes small")
			m.fitBottom()
			view := m.testView()
			rows := strings.Split(ansi.Strip(view.Content), "\n")
			if len(rows) != 30 || !strings.Contains(view.Content, "What are we working on?") || m.chrome.ActionBarVisible() {
				t.Fatal("conversation lost its empty state or row budget", width, theme)
			}
			for _, row := range rows {
				if ansi.StringWidth(row) != width {
					t.Fatal("conversation overflow", width, theme, row)
				}
			}
			if view.Cursor == nil || view.Cursor.Y != 25 {
				t.Fatal("composer caret does not match the input", width, theme, view.Cursor)
			}
		}
	}
}

func TestModernToolPointerUsesIndentedRows(t *testing.T) {
	defer screen.SetModern(false)
	for _, width := range []int{40, 120} {
		for _, compact := range []bool{false, true} {
			m := inspectorModel(t, width)
			m.applyAppearance(config.Appearance{Style: "modern", Frame: "minimal", HideTools: compact})
			m.state = stDone
			m.transcript.Viewport.GotoTop()
			clicked := false
			for part, index := range m.transcript.render.partBlocks {
				if m.transcript.blocks[index].callID != "read-1" {
					continue
				}
				hit, ok := m.transcript.render.toolHits[part]
				if !ok {
					t.Fatal("tool has no hit target", width, compact)
				}
				x := m.promptOffset() + hit.start
				y := m.transcriptTop() + m.transcript.PartOffset(part) + hit.row
				m.testUpdate(tea.MouseClickMsg{X: x, Y: y, Button: tea.MouseLeft})
				clicked = true
				break
			}
			if !clicked || !m.inspectorVisible() {
				t.Fatal("indented tool did not open its inspector", width, compact)
			}
		}
	}
}

func TestModernHistoryKeepsEvidenceOutOfRoutineChrome(t *testing.T) {
	defer screen.SetModern(false)
	for _, width := range []int{40, 80, 120} {
		m := sizeModelTo(t, testModel(), width, 40)
		m.applyAppearance(config.Appearance{Style: "modern", Frame: "minimal", HideTools: true})
		m.state = stDone
		note := contextBlock(llm.Message{Role: llm.RoleUser, Content: "[harness] Restored the workspace", Request: 7})
		m.appendBlock(userBlock("Keep *literal* user input and 界 characters"))
		m.appendBlock(note)
		m.appendBlock(thinkBlock("Check the shared contract first."))
		m.appendBlock(toolCardBlock("read", `read_file {"path":"media.go"}`))
		m.attachResult("read", "PRIVATE_TOOL_OUTPUT", false)
		path := "sessions/106db964/state.json"
		m.appendBlock(block{role: roleReceipt, receipt: runReceipt{outcome: "completed", resume: path, checks: "go test passed"}})
		view := ansi.Strip(m.testView().Content)
		for _, absent := range []string{"[role=", "included in request", "[INPUT]", "[HARNESS]", "chars]", "PRIVATE_TOOL_OUTPUT", "Resume: gektor"} {
			if strings.Contains(view, absent) {
				t.Fatal("routine history contains technical or private output", width, absent)
			}
		}
		for _, present := range []string{"*literal*", "Restored the workspace", "Thinking", "Read file", "Completed", "go test passed"} {
			if !strings.Contains(view, present) {
				t.Fatal("history lost meaningful content", width, present)
			}
		}
		for _, line := range strings.Split(view, "\n") {
			if ansi.StringWidth(line) != width {
				t.Fatal("history overflow", width, line)
			}
		}
		if note.contextText != "[harness] Restored the workspace" || m.latestReceipt().receipt.resume != path {
			t.Fatal("presentation changed context or run evidence")
		}
		m.activateMenuAction("run-details")
		details := ansi.Strip(m.testControls().DialogView())
		if !strings.Contains(details, "Last run details") || !strings.Contains(details, "106db964") {
			t.Fatal("full evidence is no longer reachable", width, details)
		}
	}
}

func TestModernMetadataStaysBelowMultilineDraft(t *testing.T) {
	defer screen.SetModern(false)
	for _, width := range []int{40, 80, 120} {
		for _, frame := range []string{"minimal", "outlined"} {
			m := inspectorModel(t, width)
			m.applyAppearance(config.Appearance{Style: "modern", Frame: frame})
			m.openInspector(len(m.transcript.blocks) - 1)
			m.testUpdate(tea.KeyPressMsg{Code: tea.KeyTab})
			m.input.SetValue("Keep this draft\nAnd this second line")
			m.fitBottom()
			rows := strings.Split(ansi.Strip(m.testView().Content), "\n")
			inputRow, metadataRow := -1, -1
			for i, row := range rows {
				if strings.Contains(row, "Keep this draft") {
					inputRow = i
				}
				if strings.Contains(row, " Act ") {
					metadataRow = i
				}
			}
			// A covered inspector owns the screen; closing restores the same draft.
			if m.screenPanes(true).Covered {
				m.testUpdate(tea.KeyPressMsg{Code: tea.KeyEscape})
				rows = strings.Split(ansi.Strip(m.testView().Content), "\n")
				for i, row := range rows {
					if strings.Contains(row, "Keep this draft") {
						inputRow = i
					}
					if strings.Contains(row, " Act ") {
						metadataRow = i
					}
				}
			}
			if inputRow < 0 || metadataRow <= inputRow || m.testView().Cursor == nil || m.testView().Cursor.Y != inputRow+1 {
				t.Fatal("metadata or cursor is detached from the draft", width, frame, inputRow, metadataRow, m.testView().Cursor)
			}
			if strings.Contains(strings.Join(rows[:inputRow], "\n"), "STEP 0/") {
				t.Fatal("idle metadata still occupies the history header")
			}
			for _, row := range rows {
				if ansi.StringWidth(row) != width {
					t.Fatal("split layout overflow", width, frame)
				}
			}
		}
	}
}

func TestModernInspectorShowsLiteralArguments(t *testing.T) {
	args, err := json.Marshal(map[string]any{"path": "report.md", "content": "first line\nsecond line", "overwrite": false, "options": map[string]any{"mode": "text"}})
	if err != nil {
		t.Fatal(err)
	}
	text := "write_file " + string(args)
	rendered := modernToolArguments(text)
	for _, field := range []string{"path\nreport.md", "content\nfirst line\nsecond line", "overwrite\nfalse", "options\n{", `"mode": "text"`} {
		if !strings.Contains(rendered, field) {
			t.Fatal("argument content lost", field, rendered)
		}
	}
	if !strings.Contains(prettyToolArguments(text), `first line\nsecond line`) {
		t.Fatal("Commander no longer preserves JSON presentation")
	}
}

func TestModernInspectorPreview(t *testing.T) {
	dir := os.Getenv("GEKTOR_TUI_PREVIEW_DIR")
	if dir == "" {
		t.Skip("preview export opt-in")
	}
	defer screen.SetModern(false)
	for _, width := range []int{80, 120} {
		m := inspectorModel(t, width)
		m.applyAppearance(config.Appearance{Style: "modern", Frame: "minimal", HideTools: true})
		m.modelName = "Ornith-1.5-35B-A3B"
		m.appendBlock(userBlock("Write the report to a file so I can inspect it later."))
		args := `write_file {"path":"report.md","content":"# Review\n\nThe shared capability serves tools and workflow nodes.\n\nKeep source selection explicit.","overwrite":false}`
		m.appendBlock(toolCardBlock("write", args))
		m.attachResult("write", "wrote report.md (120 bytes)", false)
		m.openInspector(len(m.transcript.blocks) - 1)
		m.input.SetValue("Check the final result")
		m.fitBottom()
		filename := "modern-inspector-wide.ansi"
		if width == 80 {
			filename = "modern-inspector-narrow.ansi"
		}
		err := os.WriteFile(filepath.Join(dir, filename), []byte(m.testView().Content), 0600)
		if err != nil {
			t.Fatal(err)
		}
	}
}

func TestModernStatusMenuOpensAboveComposer(t *testing.T) {
	defer screen.SetModern(false)
	m := sizeModelTo(t, testModel(), 80, 24)
	m.applyAppearance(config.Appearance{Style: "modern", Frame: "minimal"})
	m.chrome.Header = screen.MenuState{Focused: true, Menu: "activity"}
	m.syncControls()
	layout := m.chrome.HeaderMenuLayout()
	if len(layout.Rows) < 4 || layout.Y+len(layout.Rows) > m.chrome.Props.BadgeRow {
		t.Fatal("status menu overlaps or clips at the composer", layout.Y, len(layout.Rows), m.chrome.Props.BadgeRow)
	}
}

func TestModernLatestWorksWithoutComposerHint(t *testing.T) {
	defer screen.SetModern(false)
	m := inspectorModel(t, 80)
	m.applyAppearance(config.Appearance{Style: "modern", Frame: "minimal"})
	m.transcript.Follow = false
	m.transcript.Viewport.GotoTop()
	m.fitBottom()
	scrolled := ansi.Strip(m.testView().Content)
	if strings.Contains(scrolled, "Scrolled up") || strings.Contains(scrolled, "ENTER send") || m.transcript.Follow {
		t.Fatal("scrolled conversation showed composer hints or followed automatically")
	}
	m.testUpdate(tea.KeyPressMsg{Code: tea.KeyEnd, Mod: tea.ModCtrl})
	latest := ansi.Strip(m.testView().Content)
	if !m.transcript.Follow || !m.transcript.Viewport.AtBottom() || strings.Contains(latest, "Scrolled up") || strings.Contains(latest, "ENTER send") || strings.Contains(latest, "CTRL+O newline") {
		t.Fatal("Latest did not return to the reply and clear the indicator")
	}
}
