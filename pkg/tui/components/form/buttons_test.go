package form

import (
	"strings"
	"testing"

	"charm.land/lipgloss/v2"
	"github.com/charmbracelet/x/ansi"
)

func TestFilledButtonsKeepPaddingAndHitRegions(t *testing.T) {
	styles := Styles{FilledButtons: true,
		Normal:   lipgloss.NewStyle().Foreground(lipgloss.Color("#eeeeee")).Background(lipgloss.Color("#111111")),
		Button:   lipgloss.NewStyle().Foreground(lipgloss.Color("#eeeeee")).Background(lipgloss.Color("#334455")),
		Focus:    lipgloss.NewStyle().Foreground(lipgloss.Color("#88dddd")),
		Disabled: lipgloss.NewStyle().Foreground(lipgloss.Color("#777777")),
	}
	model := Model{}
	for _, width := range []int{12, 20, 40, 80} {
		row, hits := model.Actions(width, 3, "Apply", true, false, styles)
		plain := ansi.Strip(row)
		if ansi.StringWidth(row) != width || strings.ContainsAny(plain, "[]") || len(hits) != 2 {
			t.Fatal("filled action row overflow or legacy brackets", width, plain)
		}
		for _, hit := range hits {
			if hit.X0 < 0 || hit.X1 > width || hit.X1 <= hit.X0 || hit.Y0 != 3 {
				t.Fatal("button hit target differs from its row", width, hit)
			}
			text := string([]rune(plain)[hit.X0:hit.X1])
			if !strings.HasPrefix(text, " ") || !strings.HasSuffix(text, " ") {
				t.Fatal("button lost its horizontal padding", width, text)
			}
		}
	}
	label := ButtonLabel("Apply", 20, styles)
	idle := Button(label, false, true, false, styles)
	focused := Button(label, true, true, false, styles)
	disabled := Button(label, true, false, false, styles)
	if idle == focused || focused == disabled || idle == disabled {
		t.Fatal("button states lost their visual distinction")
	}
}
