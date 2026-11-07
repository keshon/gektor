package completion

import (
	"fmt"
	"strings"
	"testing"

	tea "charm.land/bubbletea/v2"
	"charm.land/lipgloss/v2"
	"github.com/charmbracelet/x/ansi"
	"github.com/keshon/gektor/internal/tui/components/popup"
)

func completionProps(width, height int) Props {
	normal := lipgloss.NewStyle().Background(lipgloss.Color("#111111"))
	return Props{Width: width, Height: height, Surface: popup.Styles{Normal: normal, Border: normal, Selected: normal}, Runnable: func(Item) bool { return true }}
}

func TestCompletionLayoutKeepsRowsAndHitsInsideBothSizes(t *testing.T) {
	s := State{}
	items := make([]Item, 40)
	for i := range items {
		items[i] = Item{Value: fmt.Sprintf("/命令-%02d", i), Description: "Details"}
	}
	s.Open(items, "commands")
	s.Selected = 25
	for _, width := range []int{24, 40, 78} {
		for _, height := range []int{4, 6, 7, 24} {
			l := s.Layout(completionProps(width, height))
			if len(l.Rows) > height || len(l.Rows) == 0 {
				t.Fatal("completion escaped its available area", width, height)
			}
			for _, row := range l.Rows {
				if ansi.StringWidth(row) != width {
					t.Fatal("completion row has wrong width", row)
				}
			}
			for i := 0; i < l.BodyHeight; i++ {
				if l.Hit(2, l.BodyTop+i) != l.Offset+i || l.Hit(0, l.BodyTop+i) != -1 {
					t.Fatal("list hit diverges from visible rows")
				}
			}
			if l.Hit(2, len(l.Rows)-2) != -1 {
				t.Fatal("footer is a suggestion")
			}
		}
	}
}

func TestCompletionUsesInjectedActionWithoutSharedGlobalState(t *testing.T) {
	s := State{}
	s.Open([]Item{{Value: "/help"}}, "help")
	p := completionProps(60, 12)
	if !strings.Contains(ansi.Strip(strings.Join(s.Layout(p).Rows, "\n")), "Enter run") {
		t.Fatal("runnable command has wrong hint")
	}
	p.Runnable = func(Item) bool { return false }
	if !strings.Contains(ansi.Strip(strings.Join(s.Layout(p).Rows, "\n")), "Enter insert") {
		t.Fatal("insert-only command has wrong hint")
	}
	s.Close()
	if s.Current() != (Item{}) {
		t.Fatal("empty completion retained a candidate")
	}
}

func TestCompletionScrollbarCapturesDragOutsideFrame(t *testing.T) {
	s := State{}
	items := make([]Item, 80)
	for i := range items {
		items[i] = Item{Value: fmt.Sprintf("item-%02d", i)}
	}
	s.Open(items, "items")
	p := completionProps(60, 18)
	l := s.Layout(p)
	s.Mouse(tea.MouseClickMsg{X: l.Width - 3, Y: l.BodyTop, Button: tea.MouseLeft}, l)
	if !s.Dragging() {
		t.Fatal("scrollbar thumb did not capture input")
	}
	s.Mouse(tea.MouseMotionMsg{X: 100, Y: 100, Button: tea.MouseLeft}, l)
	if s.Selected != len(items)-1 {
		t.Fatal("drag did not reach final candidate")
	}
	s.Mouse(tea.MouseReleaseMsg{X: 100, Y: 100, Button: tea.MouseLeft}, s.Layout(p))
	if s.Dragging() || !s.Pending() {
		t.Fatal("release lost candidates or retained drag capture")
	}
	s.Open(items[:2], "items")
	if s.Selected != 1 {
		t.Fatal("candidate refresh retained an invalid index")
	}
	s.Selected = 0
	l = s.Layout(p)
	s.Mouse(tea.MouseClickMsg{X: l.Width - 3, Y: l.BodyTop, Button: tea.MouseLeft}, l)
	// Restore overflow before testing a resize during an active drag.
	s.Open(items, "items")
	l = s.Layout(p)
	s.Mouse(tea.MouseClickMsg{X: l.Width - 3, Y: l.BodyTop, Button: tea.MouseLeft}, l)
	p.Height = 2
	s.Mouse(tea.MouseMotionMsg{X: 100, Y: 100, Button: tea.MouseLeft}, s.Layout(p))
	if s.Dragging() || s.Selected < 0 {
		t.Fatal("resize left a drag with no visible viewport")
	}
}
