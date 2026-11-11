package agent

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/charmbracelet/x/ansi"

	"github.com/keshon/gektor/internal/llm"
)

func TestCompactToolsWithInspectorThemes(t *testing.T) {
	defer setTheme("dark")
	for _, theme := range []string{"light", "dark"} {
		for _, minimal := range []bool{false, true} {
			m := inspectorModel(t, 120)
			m.minimal = minimal
			m.applyTheme(theme)
			m.hideTools = true
			m.hideNames = true
			m.hideLabels = true
			m.appendBlock(toolCardBlock("fail", `run_program {"program":"go","args":["test","./..."]}`))
			m.attachResult("fail", "PRIVATE_LOG\nerror: process timed out", true)
			m.appendBlock(contextBlock(llm.Message{Role: llm.RoleUser, Content: harnessPrefix + "Background process bg1 completed successfully. Historical command output:\nPRIVATE_BACKGROUND"}))
			m.openInspector(0)
			m.inspector.pane.Viewport.GotoTop()
			m.transcript.Viewport.GotoTop()
			view := m.testView().Content
			for _, line := range strings.Split(view, "\n") {
				if ansi.StringWidth(line) > 120 {
					t.Fatal("pane overflow", theme, minimal)
				}
			}
			if strings.Contains(ansi.Strip(view), "PRIVATE_LOG") || strings.Contains(ansi.Strip(view), "PRIVATE_BACKGROUND") {
				t.Fatal("collapsed output leaked")
			}
			if dir := os.Getenv("GEKTOR_TUI_PREVIEW_DIR"); dir != "" {
				if err := os.MkdirAll(dir, 0700); err != nil {
					t.Fatal(err)
				}
				if err := os.WriteFile(filepath.Join(dir, fmt.Sprintf("cleanup-%s-%t.ansi", theme, minimal)), []byte(view), 0600); err != nil {
					t.Fatal(err)
				}
			}
		}
	}
}
