package screen

import (
	"strings"
	"testing"

	tea "charm.land/bubbletea/v2"
	"github.com/charmbracelet/x/ansi"
)

func TestNotificationPreservesFocusAndIgnoresStaleExpiry(t *testing.T) {
	palette := NewPalette("dark")
	applyModern(palette, true)
	controls := &Controls{Props: ControlProps{Width: 80, Height: 24, Palette: palette}}
	cmd := controls.Notify("Session path copied")
	if cmd == nil {
		t.Fatal("notification has no expiry command")
	}
	first := controls.notice.version
	controls.Notify("New notification")
	view := tea.NewView(strings.Repeat(strings.Repeat(" ", 80)+"\n", 23) + strings.Repeat(" ", 80))
	view.Cursor = &tea.Cursor{Position: tea.Position{X: 2, Y: 22}}
	painted := controls.Compose(view)
	if controls.DialogInfo().Kind != NoDialog || painted.Cursor == nil || painted.Cursor.Y != 22 || !strings.Contains(ansi.Strip(painted.Content), "New notification") {
		t.Fatal("notification stole focus or disappeared")
	}
	controls.UpdateNotification(notificationExpired{version: first})
	if controls.notice == nil {
		t.Fatal("stale timeout dismissed a newer message")
	}
	controls.UpdateNotification(notificationExpired{version: controls.notice.version})
	if controls.notice != nil {
		t.Fatal("notification did not expire")
	}
}
