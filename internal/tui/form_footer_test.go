package tui

import (
	"fmt"
	"regexp"
	"strconv"
	"strings"
	"testing"
	"unicode/utf8"

	tea "charm.land/bubbletea/v2"
	"charm.land/lipgloss/v2"
	"github.com/charmbracelet/x/ansi"
	"github.com/keshon/tars/internal/tui/components/form"
)

func TestFormsShareRightAlignedActionRowAndDivider(t *testing.T) {
	for _, width := range []int{40, 80, 120} {
		for _, workspace := range []bool{false, true} {
			m := settingsModel(t)
			m.width(tea.WindowSizeMsg{Width: width, Height: 24})
			if workspace {
				m.editSettings("generation")
			} else {
				m.openForm("Delete conversation", "Removes the saved conversation.", "Delete", nil, func(map[string]string) error { return nil })
			}
			rows, hits, _, _ := m.formRows()
			buttons := hits[len(hits)-2:]
			if buttons[0].Y0 != buttons[1].Y0 || buttons[0].X1 >= buttons[1].X0 {
				t.Fatal("buttons not on one row", buttons)
			}
			y := buttons[0].Y0
			line := ansi.Strip(rows[y])
			// Settings include the category rail; action coordinates exclude it.
			if !strings.HasSuffix(line, "[ Cancel ]") {
				t.Fatal("actions not right aligned", line)
			}
			if !strings.Contains(ansi.Strip(rows[y+1]), "────") || y+2 >= len(rows) {
				t.Fatal("missing divider or help line")
			}
		}
	}
}

func TestCompactDialogMouseAndHorizontalKeys(t *testing.T) {
	m := settingsModel(t)
	calls := 0
	m.openForm("Confirm", "", "Delete", nil, func(map[string]string) error { calls++; return nil })
	m.dialog.form.Destructive = true
	m.focusForm(1)
	m.formKey(tea.KeyPressMsg{Code: tea.KeyLeft})
	if m.dialog.form.Index != 0 {
		t.Fatal("left did not select primary")
	}
	m.formKey(tea.KeyPressMsg{Code: tea.KeyRight})
	if m.dialog.form.Index != 1 {
		t.Fatal("right did not select cancel")
	}
	rows, hits, w, h := m.formRows()
	bw := 0
	for _, row := range rows {
		bw = max(bw, lipgloss.Width(row))
	}
	x := m.conversationOffset() + max((w-bw-4)/2, 0) + 2
	y := paneTop + max((h-len(rows)-2)/2, 0) + 1
	cancel := hits[len(hits)-1]
	m.Update(tea.MouseClickMsg{X: x + cancel.X0, Y: y + cancel.Y0, Button: tea.MouseLeft})
	if m.dialog != nil || calls != 0 {
		t.Fatal("cancel mouse hit missed")
	}
}

// Track SGR background state at a rendered cell, including nested resets.
func backgroundAt(line string, column int) string {
	r, g, b, _ := lipgloss.Color(currentPalette().background).RGBA()
	fallback := fmt.Sprintf("%d;%d;%d", r>>8, g>>8, b>>8)
	background := fallback
	sgr := regexp.MustCompile(`^\x1b\[([0-9;]*)m`)
	x := 0
	for len(line) > 0 {
		match := sgr.FindStringSubmatch(line)
		if match != nil {
			parts := strings.Split(match[1], ";")
			for i := 0; i < len(parts); i++ {
				n, _ := strconv.Atoi(parts[i])
				if (n == 38 || n == 48) && i+4 < len(parts) && parts[i+1] == "2" {
					if n == 48 {
						background = strings.Join(parts[i+2:i+5], ";")
					}
					i += 4
					continue
				}
				if n == 0 || n == 49 {
					background = fallback
				}
			}
			line = line[len(match[0]):]
			continue
		}
		runeValue, size := utf8.DecodeRuneInString(line)
		if x >= column {
			return background
		}
		x += ansi.StringWidth(string(runeValue))
		line = line[size:]
	}
	return background
}

func TestPopupTitlePaddingKeepsPopupBackground(t *testing.T) {
	defer setTheme("dark")
	for _, theme := range []string{"dark", "light"} {
		setTheme(theme)
		m := settingsModel(t)
		m.openForm("Delete conversation", "Permanently removes this saved conversation.", "Delete", nil, func(map[string]string) error { return nil })
		r, g, b, _ := lipgloss.Color(themeColor(colorPopupBackground)).RGBA()
		want := fmt.Sprintf("%d;%d;%d", r>>8, g>>8, b>>8)
		found := false
		for _, line := range strings.Split(m.View().Content, "\n") {
			plain := ansi.Strip(line)
			at := strings.Index(plain, "Delete conversation")
			if at < 0 {
				continue
			}
			found = true
			column := ansi.StringWidth(plain[:at]) + len("Delete conversation") + 3
			if got := backgroundAt(line, column); got != want {
				t.Fatalf("%s: title padding bg %s want %s", theme, got, want)
			}
		}
		if !found {
			t.Fatal("missing title")
		}
	}
}

func TestActionRowsFitNarrowWidths(t *testing.T) {
	m := settingsModel(t)
	m.openForm("Confirm", "", "A very long primary action", nil, func(map[string]string) error { return nil })
	for _, width := range []int{12, 18, 24, 72} {
		row, hits := m.dialog.form.Actions(width, 0, m.dialog.form.ApplyLabel, true, form.Styles{})
		if ansi.StringWidth(row) != width || hits[1].X1 != width {
			t.Fatal(width, ansi.Strip(row), hits)
		}
	}
}
