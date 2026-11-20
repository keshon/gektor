package agent

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"

	tea "charm.land/bubbletea/v2"
	"github.com/charmbracelet/x/ansi"

	"github.com/keshon/gektor/internal/events"
)

// These synthetic frames check terminal-cell geometry and model interactions.
// Exported ANSI is review material, not evidence of native terminal rendering.
func TestInteractionPresentationFixtures(t *testing.T) {
	defer setTheme("dark")
	for _, theme := range []string{"dark", "light"} {
		for _, size := range [][2]int{{40, 20}, {80, 24}, {120, 40}} {
			for _, scene := range []string{"inspection", "permission", "permission-note", "queue", "queue-editor"} {
				name := fmt.Sprintf("interaction-%s-%s-%dx%d", scene, theme, size[0], size[1])
				t.Run(name, func(t *testing.T) {
					m := queuedModel(t)
					m.width(tea.WindowSizeMsg{Width: size[0], Height: size[1]})
					m.applyTheme(theme)
					want := ""
					switch scene {
					case "inspection":
						m.appendBlock(toolCardBlock("check", `run_program {"program":"go","args":["test","./ledger"]}`))
						m.attachResult("check", "FAIL: invalid amount --1\n"+strings.Repeat("diagnostic line\n", 50), true)
						m.openInspector(0)
						want = "run_program"
					case "permission", "permission-note":
						m.handleEvent(events.Event{Name: "awaiting_input", Fields: map[string]any{"kind": "permission", "prompt": "TARGET_SCOPE\nrun_program go test ./ledger\n" + strings.Repeat("preview line\n", 40)}})
						want = "TARGET_SCOPE"
						if scene == "permission-note" {
							m.testKey(tea.KeyPressMsg{Text: "n"})
							want = "note>"
						}
					case "queue", "queue-editor":
						m.openInputQueue()
						want = "Compare these screenshots"
						if scene == "queue-editor" {
							m.testKey(tea.KeyPressMsg{Code: 'e', Mod: tea.ModCtrl})
						}
					}
					view := m.testView().Content
					rows := strings.Split(view, "\n")
					if len(rows) != size[1] {
						t.Fatalf("height %d != %d", len(rows), size[1])
					}
					for _, row := range rows {
						if ansi.StringWidth(row) != size[0] {
							t.Fatalf("wrong cell width: %q", row)
						}
					}
					if !strings.Contains(ansi.Strip(view), want) {
						t.Fatalf("missing active context %q:\n%s", want, ansi.Strip(view))
					}
					if m.input.Value() != "unfinished draft" {
						t.Fatal("interaction replaced composer draft")
					}
					if dir := os.Getenv("GEKTOR_TUI_PREVIEW_DIR"); dir != "" {
						if err := os.MkdirAll(dir, 0700); err != nil {
							t.Fatal(err)
						}
						if err := os.WriteFile(filepath.Join(dir, name+".ansi"), []byte(view), 0600); err != nil {
							t.Fatal(err)
						}
					}
				})
			}
		}
	}
}
