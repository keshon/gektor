package pane

import (
	"strings"
	"testing"

	"charm.land/lipgloss/v2"
	"github.com/charmbracelet/x/ansi"
)

func TestNoticeRowKeepsSeverityAndFitsTerminalCells(t *testing.T) {
	styles := NoticeStyles{
		Info:    lipgloss.NewStyle().Foreground(lipgloss.Color("#888888")),
		Success: lipgloss.NewStyle().Foreground(lipgloss.Color("#00ff00")),
		Warning: lipgloss.NewStyle().Foreground(lipgloss.Color("#ffff00")),
		Error:   lipgloss.NewStyle().Foreground(lipgloss.Color("#ff0000")),
	}
	for _, item := range []struct {
		level  NoticeLevel
		prefix string
		style  lipgloss.Style
	}{{NoticeInfo, "", styles.Info}, {NoticeSuccess, "", styles.Success}, {NoticeWarning, "Warning: ", styles.Warning}, {NoticeError, "Error: ", styles.Error}} {
		for _, width := range []int{0, 1, 8, 20, 80} {
			row := NoticeRow("文件 saved\nsecond line", item.level, width, styles)
			plain := ansi.Strip(row)
			if ansi.StringWidth(row) != width || strings.Contains(plain, "\n") {
				t.Fatalf("notice escaped %d cells: %q", width, row)
			}
			if width == 80 && (!strings.HasPrefix(plain, item.prefix+"文件 saved second line") || row != item.style.Render(plain)) {
				t.Fatalf("notice lost its severity text or style: %q", row)
			}
			if blank := ansi.Strip(NoticeRow("", item.level, width, styles)); strings.TrimSpace(blank) != "" {
				t.Fatal("empty notice added a severity label")
			}
		}
	}
}
