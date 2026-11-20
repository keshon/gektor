package agent

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"

	tea "charm.land/bubbletea/v2"
	"github.com/charmbracelet/x/ansi"

	"github.com/keshon/gektor/internal/config"
)

func TestChatPresentationFixtures(t *testing.T) {
	defer setTheme("dark")
	for _, theme := range []string{"dark", "light"} {
		for _, size := range [][2]int{{40, 20}, {80, 24}, {120, 40}} {
			for _, scene := range []string{"conversation", "stream", "failure", "diff", "long-history", "badges"} {
				name := fmt.Sprintf("chat-%s-%s-%dx%d", scene, theme, size[0], size[1])
				t.Run(name, func(t *testing.T) {
					m := sizeModelTo(t, testModel(), size[0], size[1])
					m.state = stDone
					m.applyTheme(theme)
					m.applyAppearance(config.Appearance{Frame: "outlined", HideLabels: true, HideTools: true, ToolNamesOnly: true})
					m.input.SetValue("Add regression coverage for the parser")
					m.transcript.blocks = []block{
						userBlock("Check why the ledger accepts --1. Keep the public API."),
						toolCardBlock("read", `read_file {"path":"ledger/amount.go"}`),
						toolCardBlock("test", `run_program {"program":"go","args":["test","./ledger"]}`),
					}
					m.attachResult("read", "amount parser loaded", false)
					m.attachResult("test", "ok example/ledger", false)
					answer := "The parser accepts a second sign. Reject it before converting the digits.\n\n```go\nfunc validAmount(raw string) bool {\n    return !strings.HasPrefix(raw, \"--\")\n}\n```\n\nThe existing API stays the same. The new case covers `--1`; `*.go` remains literal."
					switch scene {
					case "stream":
						m.state = stRunning
						m.live = "I found the sign handling in `ledger/amount.go`. Checking **the boundary"
						m.liveThink = "Compare the parser with the independent acceptance case."
					case "failure":
						m.transcript.blocks[2].failed = true
						m.transcript.blocks[2].result = "error: accepted invalid amount --1"
						m.appendBlock(block{role: roleError, text: "Acceptance check failed: accepted invalid amount --1"})
						m.appendBlock(answerBlock("The check failed. I have not verified the fix yet."))
					case "diff":
						m.appendBlock(answerBlock("Reject a second sign before parsing.\n\n```diff\n--- a/ledger/amount.go\n+++ b/ledger/amount.go\n@@ -18,2 +18,5 @@\n-    amount, err := strconv.Atoi(raw)\n+    if strings.ContainsAny(raw, \"+-\") {\n+        return 0, ErrAmount\n+    }\n+    amount, err := strconv.Atoi(raw)\n```"))
					case "long-history":
						for i := range 50 {
							m.appendBlock(answerBlock(fmt.Sprintf("Checked case %d: no additional API changes.", i+1)))
						}
						m.appendBlock(answerBlock(answer))
					case "badges":
						m.transcript.blocks = []block{userBlock("Inspect the environment and available capabilities."), toolCardBlock("info", `system_info {}`), toolCardBlock("discover", `discover_capabilities {"query":"files"}`)}
						m.attachResult("info", "system ready", false)
						m.attachResult("discover", "files enabled", false)
						m.appendBlock(answerBlock("The workspace is ready. What would you like to work on?"))
					default:
						m.appendBlock(answerBlock(answer))
					}
					m.fitBottom()
					m.refreshContent()
					m.transcript.Viewport.GotoTop()
					if scene == "long-history" {
						m.transcript.Viewport.GotoBottom()
					}
					view := m.testView().Content
					rows := strings.Split(view, "\n")
					if len(rows) != size[1] {
						t.Fatalf("height %d != %d", len(rows), size[1])
					}
					for _, row := range rows {
						if ansi.StringWidth(row) != size[0] {
							t.Fatalf("row exceeds layout: %q", row)
						}
					}
					if scene == "badges" && size[0] >= 80 {
						packed := false
						for _, row := range rows {
							packed = packed || (strings.Contains(row, "system_info") && strings.Contains(row, "discover_capabilities"))
						}
						if !packed || len(m.transcript.render.inlineTools) != 2 {
							t.Fatal("packed tool row or independent inspect targets lost")
						}
					}
					if scene == "badges" {
						hits := append([]inlineToolHit(nil), m.transcript.render.inlineTools...)
						if len(hits) != 2 {
							t.Fatalf("got %d badge targets, want 2", len(hits))
						}
						for i, id := range []string{"info", "discover"} {
							hit := hits[i]
							y := paneTop + m.transcript.PartOffset(hit.part) + hit.row - m.transcript.Viewport.YOffset()
							m.handleMouse(tea.MouseClickMsg{X: m.promptOffset() + hit.start, Y: y, Button: tea.MouseLeft})
							if m.inspector.callID != id {
								t.Fatalf("badge %s inspected %s", id, m.inspector.callID)
							}
							m.closeInspector()
							m.transcript.Viewport.GotoTop()
							m.refreshContent()
							// Opening the inspector can change the split geometry.
							hits = append(hits[:0], m.transcript.render.inlineTools...)
						}
					}
					dir := os.Getenv("GEKTOR_TUI_PREVIEW_DIR")
					if dir != "" {
						err := os.MkdirAll(dir, 0700)
						if err != nil {
							t.Fatal(err)
						}
						err = os.WriteFile(filepath.Join(dir, name+".ansi"), []byte(view), 0600)
						if err != nil {
							t.Fatal(err)
						}
					}
				})
			}
		}
	}
}
