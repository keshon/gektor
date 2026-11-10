package agent

import (
	"fmt"
	"regexp"
	"strconv"
	"strings"
	"time"
	"unicode/utf8"

	"charm.land/lipgloss/v2"
	"github.com/charmbracelet/x/ansi"
	"github.com/keshon/gektor/internal/tui/components/popup"
)

// sessionText is one searchable message and its index in the source history.
// Positions refer to source messages, never UI blocks or geometry.
//
// lower is the case-folded copy the filter matches against; it is folded
// once when the session is indexed, not per keystroke.
type sessionText struct {
	text    string
	message int
	lower   string
}
type searchGeometry struct{ x, y, width, height, noteRow int }

// Search geometry owns render, caret and hit testing in terminal cells.
func (m *Model) searchGeometry() searchGeometry {
	width := popup.ModalWidth(m.termW - 2)
	count := min(m.sessVisible(), max(len(m.sessions.entries), 1))
	height := m.sessChrome() + 4*count
	top := paneTop + max((m.transcript.Viewport.Height()-height)/2, 0)
	noteRow := top + 4 + 4*count
	if m.sessions.flash != "" {
		noteRow++
	}
	return searchGeometry{max((m.termW-width)/2, 0), top, width, height, noteRow}
}

func (m *Model) searchView() string {
	state := *m.sessions
	state.clamp(m.sessVisible())
	s := &state
	g := m.searchGeometry()
	styles := popupSurface()
	inner := max(g.width-4, 1)
	query := strings.TrimSpace(s.filter.Value())
	re := searchRegex(query)

	// Rows are rendered here rather than by the frame: the list styles the
	// selection and the match highlight, and only the frame's border is
	// uniform.
	line := func(style lipgloss.Style, text string) string {
		return popup.Row(style, text, inner)
	}
	rule := line(styles.Normal, strings.Repeat("─", inner))
	scope := "All workspaces"
	if s.currentOnly {
		scope = "Current workspace"
	}
	title := "Open session · " + scope + " · CTRL+F filter"
	if inner < 55 {
		scope = "All"
		if s.currentOnly {
			scope = "Current"
		}
		title = "Sessions · " + scope + " · ^F"
	}
	header := popup.Header(title, inner, m.styles.title)
	rows := []string{line(styles.Normal, header[0]), line(styles.Normal, s.filter.View()), rule}
	bodyStart := len(rows)
	contentWidth := inner
	if len(s.entries) > m.sessVisible() {
		contentWidth = max(inner-2, 1)
	}
	if len(s.entries) == 0 {
		label := "No saved sessions"
		if query != "" {
			label = "No matching sessions"
		}
		if s.loading {
			label = "Loading history…"
		}
		rows = append(rows, line(styles.Normal, label), line(styles.Normal, ""), line(styles.Normal, ""))
	}
	for i := s.Offset; i < min(s.Offset+m.sessVisible(), len(s.entries)); i++ {
		e := s.entries[i]
		mark, style := "  ", styles.Normal
		if i == s.Cursor {
			mark, style = "› ", styles.Selected
		}
		entry := mark + highlightSearch(cellLine(e.title, max(contentWidth-2, 1)), re, style)
		rows = append(rows, line(style, entry))
		metadata := "Date unavailable"
		if !e.updated.IsZero() {
			days := max(int(time.Since(e.updated).Hours()/24), 0)
			metadata = fmt.Sprintf("%s · %dd ago", e.updated.Local().Format("2006-01-02 15:04"), days)
		}
		metadata += fmt.Sprintf(" · %d messages", e.msgs)
		muted := style
		if i != s.Cursor {
			muted = muted.Foreground(lipgloss.Color(themeColor(colorSecondaryText)))
		}
		path := e.workspace
		if path == "" {
			path = "Unassigned workspace"
		}
		rows = append(rows, line(muted, "  "+path))

		text := metadata + " · " + e.preview
		if e.matchText != "" {
			text = e.matchText
		}
		if text == "" {
			text = e.mode
		}
		snippet := muted
		rows = append(rows, line(snippet, "  "+searchExcerpt(text, re, max(contentWidth-2, 1))))
		if i+1 < min(s.Offset+m.sessVisible(), len(s.entries)) {
			rows = append(rows, line(styles.Normal, ""))
		}
	}
	rows = append(rows[:bodyStart], popup.ScrollRows(rows[bodyStart:], inner, s.Offset*4, len(s.entries)*4-1, styles.Normal)...)
	if s.flash != "" {
		rows = append(rows, line(styles.Normal.Foreground(lipgloss.Color(themeColor(colorPopupError))), s.flash))
	}
	switch s.mode {
	case sessRename:
		rows = append(rows, line(styles.Normal, "Rename session · ENTER save · ESC back"), line(styles.Normal, m.note.View()))
	case sessReassign:
		destination := m.wsRoot
		if s.reassign != nil {
			destination = s.reassign.workspace
		}
		if extra := ansi.StringWidth(destination) - inner; extra > 0 {
			destination = ansi.TruncateLeft(destination, extra+1, "…")
		}
		rows = append(rows, line(styles.Normal, "Assign saved workspace to:"), line(styles.Normal, destination))
	case sessConfirm:
		title := "session"
		if len(s.entries) > 0 {
			title = s.entries[s.Cursor].title
		}
		rows = append(rows, line(styles.Normal, "Delete "+title+"? Y delete · N cancel"))
	}
	hintWidth, count := inner, ""
	if len(s.entries) > 0 {
		count = "  " + strconv.Itoa(s.Cursor+1) + "/" + strconv.Itoa(len(s.entries))
		hintWidth = max(inner-lipgloss.Width(count), 0)
	}
	shortcuts := []popup.Shortcut{{Key: "Esc", Action: "close"}, {Key: "Enter", Action: "open"}, {Key: "↑↓", Action: "select"}}
	if hintWidth < 55 {
		// Scope remains in the title. Keep room for navigation before metadata.
		hintWidth, count = inner, ""
		shortcuts[2].Action = "move"
	} else {
		shortcuts = append(shortcuts, popup.Shortcut{Key: "^F", Action: "scope"}, popup.Shortcut{Key: "^W", Action: "assign"})
	}
	switch s.mode {
	case sessRename:
		shortcuts = []popup.Shortcut{{Key: "Esc", Action: "back"}, {Key: "Enter", Action: "save"}}
	case sessReassign:
		shortcuts = []popup.Shortcut{{Key: "Esc", Action: "back"}, {Key: "Y", Action: "assign"}, {Key: "N", Action: "cancel"}}
	case sessConfirm:
		shortcuts = []popup.Shortcut{{Key: "Esc", Action: "back"}, {Key: "Y", Action: "delete"}, {Key: "N", Action: "cancel"}}
	}
	hint := popup.Shortcuts(hintWidth, m.shortcutStyles(), shortcuts...) + m.styles.dim.Render(count)
	for _, row := range popup.Footer(inner, hint, m.styles.dim) {
		rows = append(rows, line(styles.Normal, row))
	}
	return strings.Join(popup.Frame(rows, g.width, styles), "\n")
}

// searchRegex builds the matcher once per frame. Compiling it per visible
// row per frame cost two compiles for every row of the popup on every
// repaint, and the popup repaints on the same tick as everything else.
func searchRegex(query string) *regexp.Regexp {
	if query == "" {
		return nil
	}
	return regexp.MustCompile("(?i)" + regexp.QuoteMeta(query))
}

func searchExcerpt(text string, re *regexp.Regexp, width int) string {
	text = strings.Join(strings.Fields(cleanText(text)), " ")
	runes := []rune(text)
	start := 0
	if re != nil {
		if loc := re.FindStringIndex(text); loc != nil {
			start = max(utf8.RuneCountInString(text[:loc[0]])-width/4, 0)
		}
	}
	if start > 0 {
		text = "…" + string(runes[start:])
	}
	return strings.TrimRight(cellLine(text, width), " ")
}

func highlightSearch(text string, re *regexp.Regexp, style lipgloss.Style) string {
	if re == nil {
		return style.Render(text)
	}
	var out strings.Builder
	pos := 0
	for _, loc := range re.FindAllStringIndex(text, -1) {
		out.WriteString(style.Render(text[pos:loc[0]]))
		match := style.Bold(true)
		if style.GetBackground() == popupSurface().Selected.GetBackground() {
			match = match.Underline(true)
		} else {
			match = match.Foreground(lipgloss.Color(themeColor(colorSearchMatch)))
		}
		out.WriteString(match.Render(text[loc[0]:loc[1]]))
		pos = loc[1]
	}
	out.WriteString(style.Render(text[pos:]))
	return out.String()
}

func (m *Model) overlaySearch(background string) string {
	g := m.searchGeometry()
	return popup.Overlay(background, strings.Split(m.searchView(), "\n"), g.x, g.y, g.width, m.termW)
}
