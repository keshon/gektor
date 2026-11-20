package tui

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"

	tea "charm.land/bubbletea/v2"
	"github.com/charmbracelet/colorprofile"
	"github.com/charmbracelet/x/ansi"

	"github.com/keshon/gektor/internal/mcpmanager"
	"github.com/keshon/gektor/internal/tui/screen"
)

func TestCompactDialogsKeepChromeAndMatchMouseRows(t *testing.T) {
	t.Cleanup(func() { screen.SetTheme("dark") })
	for _, theme := range []string{"dark", "light"} {
		for _, mode := range []string{"agent", "workflows"} {
			t.Run(theme+"/"+mode, func(t *testing.T) {
				root := t.TempDir()
				manager, err := mcpmanager.New(filepath.Join(root, "mcp"), root, filepath.Join(root, "credentials"), nil)
				if err != nil {
					t.Fatal(err)
				}
				t.Cleanup(manager.Close)
				for _, id := range []string{"alpha", "bravo", "charlie", "delta"} {
					err = manager.Save(mcpmanager.Server{ID: id, Description: "Reference tools", URL: "https://example.com/" + id, Enabled: true})
					if err != nil {
						t.Fatal(err)
					}
				}

				m := shellTestModel(t, Config{AgentConfig: AgentConfig{MCP: manager, InitialDraft: "draft-kept"}})
				m.colorProfile = colorprofile.TrueColor
				m.applyTheme(theme)
				m.runAction("screen:" + mode)
				sizeModelTo(t, m, 40, 20)
				m.runAction("/mcp")
				m.settleControls()
				if m.chrome.Dialog == nil || m.chrome.Dialog.Table == nil {
					t.Fatal("management command did not open a table")
				}
				dialogID := m.chrome.DialogID()
				m.syncPresentation()
				layout := m.chrome.TableSnapshot(dialogID).Layout
				if layout.Capacity < 3 {
					t.Fatalf("compact dialog shows only %d rows", layout.Capacity)
				}
				painted := m.View().Content
				rows := strings.Split(ansi.Strip(painted), "\n")
				if len(rows) != 20 || !strings.Contains(rows[0], "GEKTOR") || !strings.Contains(rows[19], "ESC") {
					t.Fatal("modal covered application navigation or active hints", strings.Join(rows, "\n"))
				}
				if mode == "agent" && strings.Contains(strings.Join(rows, "\n"), "draft-kept") {
					t.Fatal("compact modal left the composer exposed")
				}
				m.Update(tea.KeyPressMsg{Code: tea.KeyDown})
				if m.chrome.TableSelectedID(dialogID) != "bravo" {
					t.Fatal("keyboard did not select the second row", m.chrome.TableSelectedID(dialogID))
				}
				clicked := false
				for y, row := range rows {
					if at := strings.Index(row, "charlie"); at >= 0 {
						x := ansi.StringWidth(row[:at])
						m.Update(tea.MouseClickMsg{X: x, Y: y, Button: tea.MouseLeft})
						clicked = true
						break
					}
				}
				if !clicked || m.chrome.TableSelectedID(dialogID) != "charlie" {
					t.Fatal("painted row and mouse selection disagree", m.chrome.TableSelectedID(dialogID))
				}
				if dir := os.Getenv("GEKTOR_TUI_PREVIEW_DIR"); dir != "" {
					err = os.MkdirAll(dir, 0700)
					if err != nil {
						t.Fatal(err)
					}
					path := filepath.Join(dir, fmt.Sprintf("compact-%s-%s-40x20.ansi", mode, theme))
					err = os.WriteFile(path, []byte(painted), 0600)
					if err != nil {
						t.Fatal(err)
					}
				}
				m.Update(tea.KeyPressMsg{Code: tea.KeyEscape})
				if m.chrome.Dialog != nil || draftOf(m) != "draft-kept" {
					t.Fatal("closing the modal lost the underlying draft or input ownership")
				}
			})
		}
	}
}
