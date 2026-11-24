package tui

import (
	"fmt"
	"strings"
	"testing"

	tea "charm.land/bubbletea/v2"
	"github.com/charmbracelet/x/ansi"

	"github.com/keshon/gektor/pkg/tui/components/table"
	"github.com/keshon/gektor/pkg/tui/screen"
)

func TestAppearanceScrollbarDragThroughBothScreens(t *testing.T) {
	for _, workflow := range []bool{false, true} {
		for _, width := range []int{40, 80, 120} {
			m := sizeModelTo(t, settingsModel(t), width, 25)
			if workflow {
				m.openWorkflows()
			}
			m.runAction("appearance")
			m.settleControls()
			dialog := m.chrome.DialogID()
			f := formSnapshot(m)
			values := f.Values()
			rows := strings.Split(ansi.Strip(m.View().Content), "\n")
			x, y := -1, -1
			for row, line := range rows {
				for col, glyph := range []rune(line) {
					if glyph == '█' {
						x, y = col, row
						break
					}
				}
				if x >= 0 {
					break
				}
			}
			if x < 0 {
				// At a wide enough layout all fields fit, so there is no scrollbar.
				if !strings.Contains(strings.Join(rows, "\n"), "Borders") {
					t.Fatal("Appearance has neither second field nor scrollbar", workflow, width)
				}
				continue
			}

			m.Update(tea.MouseClickMsg{X: x, Y: y, Button: tea.MouseLeft})
			m.Update(tea.MouseMotionMsg{X: 0, Y: m.termH + 10, Button: tea.MouseLeft})
			m.Update(tea.MouseReleaseMsg{X: 0, Y: m.termH - 1})
			f = formSnapshot(m)
			if m.chrome.DialogID() != dialog || f.Index != 2 || f.Values()["theme"] != values["theme"] || f.Values()["frame"] != values["frame"] || f.Values()["style"] != values["style"] {
				t.Fatal("drag missed the last field, changed a choice or activated toolbar", workflow, width, f.Index)
			}
			if !strings.Contains(ansi.Strip(m.View().Content), "UI style") {
				t.Fatal("style field remained hidden after dragging")
			}
		}
	}
}

func TestTableScrollbarMotionAndReleaseReachOffsetDialog(t *testing.T) {
	m := sizeModelTo(t, settingsModel(t), 120, 30)
	for _, workflow := range []bool{false, true} {
		if workflow {
			m.openWorkflows()
		}
		state := m.chrome.NewTable()
		var tableRows []table.Row
		for i := range 80 {
			tableRows = append(tableRows, table.Row{ID: fmt.Sprint(i), Cells: []string{fmt.Sprintf("row %02d", i)}})
		}
		m.chrome.PresentDialog(screen.Dialog{Title: "Rows", Table: &screen.Table{State: state, Rows: tableRows, Columns: []table.Column{{Label: "Name"}}}})
		m.settleControls()
		dialog := m.chrome.DialogID()
		p := m.chrome.TableProps()
		l := m.chrome.TableSnapshot(m.chrome.DialogID()).Layout
		x := m.chrome.Props.Area.X + max((p.Width-l.Width)/2, 0) + 2 + l.Scroll.X
		y := m.chrome.Props.Area.Y + max((p.Height-len(l.Rows)-2)/2, 0) + 1 + l.Scroll.Y
		rows := strings.Split(ansi.Strip(m.View().Content), "\n")
		if []rune(rows[y])[x] != '█' {
			t.Fatal("table track origin differs from drawn thumb", workflow, rows[y])
		}
		m.Update(tea.MouseClickMsg{X: x, Y: y, Button: tea.MouseLeft})
		m.Update(tea.MouseMotionMsg{X: 0, Y: m.termH + 10, Button: tea.MouseLeft})
		m.Update(tea.MouseReleaseMsg{X: 0, Y: m.termH - 1})
		if m.chrome.DialogID() != dialog || m.chrome.TableSnapshot(m.chrome.DialogID()).Offset != len(tableRows)-l.Capacity {
			t.Fatal("shared modal discarded motion/release or activated footer", workflow, m.chrome.TableSnapshot(m.chrome.DialogID()).Offset)
		}
		m.chrome.Close()
		m.settleControls()
	}
}
