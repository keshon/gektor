package agent

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

	"github.com/keshon/gektor/internal/tui/components/form"
	"github.com/keshon/gektor/internal/tui/screen"
)

func TestCompactDialogMouseAndHorizontalKeys(t *testing.T) {
	m := settingsModel(t)
	calls := 0
	m.openForm(screen.Form{Title: "Confirm", ApplyLabel: "Delete"}, formProbe{calls: &calls})
	m.chrome.Dialog.Form.Destructive = true
	m.testControls().FocusForm(1)
	m.testKey(tea.KeyPressMsg{Code: tea.KeyLeft})
	if m.chrome.Dialog.Form.Index != 0 {
		t.Fatal("left did not select primary")
	}
	m.testKey(tea.KeyPressMsg{Code: tea.KeyRight})
	if m.chrome.Dialog.Form.Index != 1 {
		t.Fatal("right did not select cancel")
	}
	// Click where Cancel is drawn. Working the position out here duplicated
	// the layout maths, which made it a second opinion about where the
	// renderer put the button rather than a check that it did.
	clickVisible(t, m, "[ Cancel ]")
	if m.chrome.Dialog != nil || calls != 0 {
		t.Fatal("cancel mouse hit missed")
	}
}

// Track SGR background state at a rendered cell, including nested resets.
func backgroundAt(line string, column int) string {
	r, g, b, _ := lipgloss.Color(currentPalette().Background).RGBA()
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
		m.testControls().OpenForm("Delete Identity", "Permanently removes this saved Identity.", "Delete", nil)
		r, g, b, _ := lipgloss.Color(themeColor(colorPopupBackground)).RGBA()
		want := fmt.Sprintf("%d;%d;%d", r>>8, g>>8, b>>8)
		found := false
		for _, line := range strings.Split(m.testView().Content, "\n") {
			plain := ansi.Strip(line)
			at := strings.Index(plain, "Delete Identity")
			if at < 0 {
				continue
			}
			found = true
			column := ansi.StringWidth(plain[:at]) + len("Delete Identity") + 3
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
	m.testControls().OpenForm("Confirm", "", "A very long primary action", nil)
	for _, width := range []int{12, 18, 24, 72} {
		row, hits := m.chrome.Dialog.Form.Actions(width, 0, m.chrome.Dialog.Form.ApplyLabel, true, false, form.Styles{})
		if ansi.StringWidth(row) != width || hits[1].X1 != width {
			t.Fatal(width, ansi.Strip(row), hits)
		}
	}
}

type formProbe struct{ calls *int }

func (probe formProbe) apply(_ *Model, _ map[string]string) error { *probe.calls++; return nil }
