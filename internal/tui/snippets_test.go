package tui

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"charm.land/lipgloss/v2"
	uv "github.com/charmbracelet/ultraviolet"
	"github.com/charmbracelet/x/ansi"
	"github.com/keshon/gektor/internal/events"
)

func TestCompactToolActionAndFailure(t *testing.T) {
	for _, tc := range []struct{ call, action, target string }{
		{`system_info {"group":"skills"}`, "List skills", "skills"},
		{`read_file {"path":"AGENTS.md"}`, "Read file", "AGENTS.md"},
		{`webfetch {"url":"https://example.com/guide"}`, "Fetch page", "example.com/guide"},
		{`custom_tool {"value":"hello"}`, "custom_tool", `{"value":"hello"}`},
	} {
		b := toolCardBlock("call", tc.call)
		b.open = false
		b.result = "PRIVATE_OUTPUT"
		for _, width := range []int{20, 40, 80, 140} {
			text := ansi.Strip(compactTool(b, defaultStyles(), width))
			if strings.Contains(text, "\n") || lipgloss.Width(text) > width || strings.Contains(text, b.result) {
				t.Fatal(width, text)
			}
			if width >= 80 && (!strings.Contains(text, tc.action) || !strings.Contains(text, tc.target)) {
				t.Fatal("action/target missing", text)
			}
		}
		b.failed, b.result = true, "error: HTTP 404\nPRIVATE_OUTPUT"
		text := ansi.Strip(compactTool(b, defaultStyles(), 100))
		if !strings.Contains(text, "×") || !strings.HasSuffix(text, "HTTP 404") || strings.Contains(text, "PRIVATE_OUTPUT") {
			t.Fatal("failure hidden or result leaked", text)
		}
	}
}

func TestDetailedToolArgumentsRemainComplete(t *testing.T) {
	args := `{"path":"a  b.go","content":"` + strings.Repeat("COMPLETE_ARGUMENT ", 30) + `"}`
	m := sizeModel(t, testModel())
	m.handleEvent(events.Event{Name: "step", Fields: map[string]any{"tool_calls": []any{map[string]any{"id": "c", "name": "write_file", "args": args}}}})
	b := m.transcript.blocks[0]
	if b.text != "write_file "+args {
		t.Fatal("arguments changed or cut", b.text)
	}
	text := ansi.Strip(renderBlock(b, defaultStyles(), renderOpts{}, 1000))
	if !strings.Contains(text, args) {
		t.Fatal("detailed view cut arguments", text)
	}
}

func TestCompletionPanelKeepsEvidenceAndFits(t *testing.T) {
	defer setTheme("dark")
	for _, theme := range []string{"dark", "light"} {
		for _, frame := range []string{"minimal", "outlined"} {
			for _, width := range []int{20, 40, 80, 120} {
				m := sizeModelTo(t, testModel(), width, 34)
				m.minimal = frame == "minimal"
				m.applyTheme(theme)
				m.hideNames, m.hideLabels, m.hideTools = false, true, true
				m.appendBlock(toolCardBlock("c", `system_info {"group":"skills"}`))
				m.attachResult("c", "skill list")
				m.handleEvent(events.Event{Name: "outcome", Fields: map[string]any{"outcome": "completed", "resume": filepath.Join("sessions", "47003e38", "state.json")}})
				before := m.transcript.blocks[len(m.transcript.blocks)-1].text
				view := m.View().Content
				for _, line := range strings.Split(view, "\n") {
					if lipgloss.Width(line) > width {
						t.Fatal("panel overflow", theme, frame, width, line)
					}
				}
				if width >= 80 {
					text := ansi.Strip(view)
					for _, want := range []string{"Completed", "No verification result", "Files: no changes observed", "Checks: no current command verdict", "Session: 47003e38"} {
						if !strings.Contains(text, want) {
							t.Fatal("lost evidence", want, text)
						}
					}
					if strings.Contains(text, "-resume") {
						t.Fatal("resume command still occupies transcript", text)
					}
				}
				if m.transcript.blocks[len(m.transcript.blocks)-1].text != before {
					t.Fatal("display changed saved evidence")
				}
				if width == 120 {
					if dir := os.Getenv("GEKTOR_TUI_PREVIEW_DIR"); dir != "" {
						if err := os.MkdirAll(dir, 0700); err != nil {
							t.Fatal(err)
						}
						if err := os.WriteFile(filepath.Join(dir, fmt.Sprintf("snippets-%s-%s.ansi", theme, frame)), []byte(view), 0600); err != nil {
							t.Fatal(err)
						}
					}
				}
			}
		}
	}
	text := strings.Join(receiptText("outcome: failed\nfiles: main.go\nchecks: tests failed\nFAIL: assertion X", defaultStyles(), 80), "\n")
	if !strings.Contains(ansi.Strip(text), "FAIL: assertion X") {
		t.Fatal("multiline verification lost", text)
	}
}

func TestCompletionUsesPageBackground(t *testing.T) {
	defer setTheme("dark")
	for _, theme := range []string{"dark", "light"} {
		setTheme(theme)
		st := defaultStyles()
		body := strings.Join(receiptText("outcome: completed\nfiles: none observed\nchecks: no current command verdict", st, 78), "\n")
		for _, line := range strings.Split(ansi.Strip(body), "\n") {
			if strings.HasPrefix(line, " ") {
				t.Fatal("completion details are indented past the title", line)
			}
		}
		panel := paintScreen(railText(body, st, 80))
		height := strings.Count(panel, "\n") + 1
		screen := uv.NewScreenBuffer(80, height)
		uv.NewStyledString(panel).Draw(screen, uv.Rect(0, 0, 80, height))
		for y := 0; y < height; y++ {
			for x := 0; x < 80; x++ {
				if c := screen.CellAt(x, y); c == nil || !sameColor(c.Style.Bg, currentPalette().background) {
					t.Fatalf("completion introduced a separate background: %s (%d,%d)", theme, x, y)
				}
			}
		}
	}
}
