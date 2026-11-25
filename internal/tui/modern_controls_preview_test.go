package tui

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/charmbracelet/colorprofile"
	"github.com/charmbracelet/x/ansi"
	"github.com/keshon/gektor/internal/config"
	"github.com/keshon/gektor/pkg/tui/screen"
)

func TestModernAppearanceUsesFilledButtons(t *testing.T) {
	defer screen.SetModern(false)
	m := sizeModelTo(t, settingsModel(t), 100, 30)
	m.colorProfile = colorprofile.TrueColor
	m.applyAppearance(screen.CurrentPalette().Name, config.Appearance{Style: "modern", Frame: "minimal"})
	m.runAction("appearance")
	m.settleControls()
	rows, hits, _, _ := m.chrome.FormRows()
	f := formSnapshot(m)
	count := 0
	for _, hit := range hits {
		if (hit.Index != len(f.Fields) && hit.Index != len(f.Fields)+1) || hit.Y0 == 0 || hit.Y0 >= len(rows) {
			continue
		}
		text := ansi.Strip(rows[hit.Y0])
		if strings.Contains(text, "[ Apply ]") || strings.Contains(text, "[ Cancel ]") {
			t.Fatal("Modern action uses Commander brackets")
		}
		count++
	}
	if count != 2 {
		t.Fatal("form lost an action", count)
	}
	if dir := os.Getenv("GEKTOR_TUI_PREVIEW_DIR"); dir != "" {
		err := os.WriteFile(filepath.Join(dir, "modern-appearance.ansi"), []byte(m.View().Content), 0600)
		if err != nil {
			t.Fatal(err)
		}
	}
}
