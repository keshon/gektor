package screen

import (
	"strings"
	"time"

	tea "charm.land/bubbletea/v2"

	"github.com/keshon/gektor/pkg/tui/components/cell"
	"github.com/keshon/gektor/pkg/tui/components/popup"
)

type notification struct {
	text    string
	version uint64
}

type notificationExpired struct{ version uint64 }

// Notify replaces the current temporary message without taking input focus.
func (m *Controls) Notify(text string) tea.Cmd {
	m.noticeVersion++
	version := m.noticeVersion
	m.notice = &notification{text: cleanText(text), version: version}
	return tea.Tick(4*time.Second, func(time.Time) tea.Msg { return notificationExpired{version: version} })
}

// UpdateNotification consumes expiry messages from the application's update loop.
func (m *Controls) UpdateNotification(msg tea.Msg) bool {
	expired, ok := msg.(notificationExpired)
	if !ok {
		return false
	}
	if m.notice != nil && m.notice.version == expired.version {
		m.notice = nil
	}
	return true
}

func (m *Controls) overlayNotification(content string) string {
	if m.notice == nil || m.dialog != nil || m.Header.Menu != "" || m.Props.Width < 12 || m.Props.Height < 6 {
		return content
	}
	width := min(m.Props.Width-4, 54)
	normal, _, _ := m.palette().PopupStyles()
	text := cell.Wrap(m.notice.text, max(width-4, 1))
	rows := []string{normal.Render(strings.Repeat(" ", width))}
	for _, line := range text[:min(len(text), 3)] {
		rows = append(rows, popup.Row(normal, "  "+cell.Line(line, width-4)+"  ", width))
	}
	rows = append(rows, normal.Render(strings.Repeat(" ", width)))
	return popup.Overlay(content, rows, m.Props.Width-width-2, 2, width, m.Props.Width)
}
