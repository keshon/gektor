package pane

import (
	"strings"
	"testing"

	"github.com/charmbracelet/x/ansi"

	"github.com/keshon/gektor/pkg/tui/components/cell"
)

func TestSplitKeepsOddWidthsAndCoversNarrowScreens(t *testing.T) {
	for _, width := range []int{40, 99, 100, 121} {
		area := Rect{Y: 1, Width: width, Height: 28}
		l := Split(area, true, SplitProps{MinimumWidth: 100})
		if width < 100 {
			if !l.Covered || l.Main != area || l.Side != area {
				t.Fatal("narrow split squeezed the parent", l)
			}
		} else if l.Covered || l.Main.Width+l.Side.Width != width || l.Side.X != l.Main.Width {
			t.Fatal("split lost columns", l)
		}
	}
}

func TestSplitHonorsRequestedInspectorWidth(t *testing.T) {
	area := Rect{X: 2, Y: 1, Width: 120, Height: 28}
	props := SplitProps{MinimumWidth: 100, SideWidth: 75}
	l := Split(area, true, props)
	if l.Covered || l.Main.Width != 45 || l.Side.Width != 75 || l.Side.X != 47 {
		t.Fatal("requested side width lost geometry", l)
	}
	props.SideWidth = area.Width
	l = Split(area, true, props)
	if !l.Covered || l.Main != area || l.Side != area {
		t.Fatal("expanded inspector did not cover the parent", l)
	}
}

func TestPaneRegionsKeepFooterCompactAndCloseHitOnTitle(t *testing.T) {
	for _, outlined := range []bool{true, false} {
		for _, width := range []int{40, 61, 121} {
			l := Measure(Props{Width: width, Height: 28, FooterHeight: 1, LeftInset: 1, RightInset: 1, Outlined: outlined, BottomEdge: true, Close: true})
			rows := Render(l, "Inspect", "body", []string{"Esc close"}, Styles{}, true)
			if len(rows) != 28 || l.Footer.Height != 1 || l.Footer.Y != 26 || l.FooterDivider != 25 {
				t.Fatal("pane added vertical footer padding", l)
			}
			for _, row := range rows {
				if cell.Width(row) != width {
					t.Fatal("pane exceeds width", row)
				}
			}
			if !strings.Contains(ansi.Strip(rows[l.Footer.Y]), "Esc close") {
				t.Fatal("footer text not in footer region")
			}
			if !strings.Contains(ansi.Strip(rows[l.FooterDivider]), "────") {
				t.Fatal("missing footer divider")
			}
			close := ansi.Cut(ansi.Strip(rows[l.CloseHit.Y]), l.CloseHit.X, l.CloseHit.X+l.CloseHit.Width)
			if !strings.Contains(close, "×") {
				t.Fatal("close hit does not cover the drawn control")
			}
			composer := Measure(Props{Width: width, Height: 28, FooterHeight: 5, LeftInset: 1, RightInset: 1, Outlined: outlined, BottomEdge: true})
			if composer.Body.Height != l.Body.Height-4 || composer.Height != l.Height {
				t.Fatal("composer height changed sibling pane bounds")
			}
		}
	}
}

func TestFooterlessPaneUsesRecoveredRowsForBody(t *testing.T) {
	for _, outlined := range []bool{false, true} {
		props := Props{Width: 60, Height: 24, FooterHeight: 1, LeftInset: 1, RightInset: 1, Outlined: outlined, BottomEdge: true, Close: true}
		before := Measure(props)
		props.Footerless = true
		layout := Measure(props)
		body := strings.Repeat("body\n", layout.Body.Height-1) + "LAST_BODY_ROW"
		rows := Render(layout, "Inspect", body, []string{"FOOTER_SHOULD_BE_ABSENT"}, Styles{}, false)
		if layout.Body.Height != before.Body.Height+2 || layout.Footer.Height != 0 || len(rows) != 24 {
			t.Fatal("footer space was not returned to body", outlined, layout)
		}
		if !strings.Contains(ansi.Strip(rows[layout.Body.Y+layout.Body.Height-1]), "LAST_BODY_ROW") || strings.Contains(strings.Join(rows, "\n"), "FOOTER_SHOULD_BE_ABSENT") {
			t.Fatal("footerless pane lost content or rendered a footer", outlined)
		}
		if layout.CloseHit != before.CloseHit {
			t.Fatal("removing footer moved the close control")
		}
	}
}
