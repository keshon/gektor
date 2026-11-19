package popup

import (
	"testing"

	tea "charm.land/bubbletea/v2"
)

func TestScrollDragHoldsPositionPagesAndCancelsChangedTrack(t *testing.T) {
	track := ScrollTrack{X: 20, Y: 3, Height: 10, Total: 100, Offset: 13}
	var drag ScrollDrag
	top, _ := ScrollThumb(track.Height, track.Total, track.Offset)
	press := tea.MouseClickMsg{X: track.X, Y: track.Y + top, Button: tea.MouseLeft}
	drag.Mouse(press, track)
	offset, handled := drag.Mouse(tea.MouseReleaseMsg{X: press.X, Y: press.Y}, track)
	if !handled || drag.Active || offset != 13 {
		t.Fatal("clicking the thumb changed an offset between track cells")
	}

	offset, handled = drag.Mouse(tea.MouseClickMsg{X: track.X, Y: track.Y + track.Height - 1, Button: tea.MouseLeft}, track)
	if !handled || drag.Active || offset != 23 {
		t.Fatal("track click did not page once")
	}

	for _, change := range []string{"move", "resize", "content", "no overflow"} {
		drag.Mouse(press, track)
		changed := track
		switch change {
		case "move":
			changed.Y++
		case "resize":
			changed.Height++
		case "content":
			changed.Total++
		case "no overflow":
			changed.Total = changed.Height
			changed.Offset = 0
		}

		offset, handled = drag.Mouse(tea.MouseMotionMsg{X: 0, Y: 100, Button: tea.MouseLeft}, changed)
		if !handled || drag.Active || offset != changed.Offset {
			t.Fatal("changed track kept an old grab or moved content", change)
		}
	}
}

func TestOneCellScrollbarDragsToEitherEnd(t *testing.T) {
	track := ScrollTrack{X: 10, Y: 3, Height: 1, Total: 30}
	var drag ScrollDrag
	drag.Mouse(tea.MouseClickMsg{X: 10, Y: 3, Button: tea.MouseLeft}, track)
	offset, handled := drag.Mouse(tea.MouseMotionMsg{X: 0, Y: 20, Button: tea.MouseLeft}, track)
	if !handled || offset != 29 {
		t.Fatal("one-cell track could not reach bottom")
	}
	track.Offset = offset
	offset, handled = drag.Mouse(tea.MouseReleaseMsg{X: 0, Y: 0}, track)
	if !handled || drag.Active || offset != 0 {
		t.Fatal("one-cell track could not reach top and release")
	}
}
