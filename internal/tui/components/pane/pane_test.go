package pane

import (
	"strings"
	"testing"

	"github.com/charmbracelet/x/ansi"
	"github.com/keshon/gektor/internal/tui/components/cell"
)

func TestSplitKeepsOddWidthsAndCoversNarrowScreens(t *testing.T) {
	for _, width := range []int{40, 99, 100, 121} {
		area := Rect{Y: 1, Width: width, Height: 28}
		l := Split(area, true, 100)
		if width < 100 {
			if !l.Covered || l.Main != area || l.Side != area {
				t.Fatal("narrow split squeezed the parent", l)
			}
		} else if l.Covered || l.Main.Width+l.Side.Width != width || l.Side.X != l.Main.Width {
			t.Fatal("split lost columns", l)
		}
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
