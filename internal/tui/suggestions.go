package tui

import (
	"io"
	"os"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
	"unicode"

	tea "charm.land/bubbletea/v2"
	"charm.land/lipgloss/v2"
)

type suggestion struct {
	value, description string
	directory          bool
}

type suggestionState struct {
	items                []suggestion
	selected, start, end int
	key, dismissed       string
}

// completionToken finds the slash command or @path at the caret.
//
// Only an enumerated argument is treated as one. "/mode " completes
// against plan|act, but the text after "/new " or "/rename " is free form,
// and an @path reference inside it has to keep completing as a path rather
// than being swallowed as an argument.
func (m *model) completionToken() (start, end int, prefix string) {
	start, end, prefix = m.input.Token()
	runes := []rune(m.input.Value())
	pos := start + len([]rune(prefix))
	before := string(runes[:pos])
	trimmed := strings.TrimLeftFunc(before, unicode.IsSpace)
	if name, _, ok := strings.Cut(trimmed, " "); ok && strings.HasPrefix(name, "/") {
		if c := lookupCommand(name); c != nil && len(c.choices) > 0 {
			start = len([]rune(before)) - len([]rune(trimmed))
			prefix = string(runes[start:pos])
		}
	}
	if prefix == "" || (prefix[0] != '@' && (prefix[0] != '/' || strings.TrimSpace(string(runes[:start])) != "")) {
		return 0, 0, ""
	}
	return
}

func (m *model) syncSuggestions() {
	s := &m.suggestions
	if m.activeInputOwner() != focusInput || (m.ready && (m.transcript.Viewport.Height() < 4 || m.transcript.Viewport.Width() < 24)) || (m.state != stDone && m.state != stRunning) {
		s.items = nil
		s.key = ""
		return
	}
	start, end, prefix := m.completionToken()
	key := m.input.Value() + "\x00" + strconv.Itoa(start) + ":" + strconv.Itoa(end) + ":" + prefix
	if prefix == "" {
		s.items, s.key, s.dismissed = nil, "", ""
		return
	}
	if key == s.key {
		return
	}
	s.key, s.start, s.end, s.selected = key, start, end, 0
	s.items = nil
	if key == s.dismissed {
		return
	}
	if prefix[0] == '/' {
		s.items = nil
		for _, c := range commands() {
			// Past the name: offer the argument's fixed values.
			if len(c.choices) > 0 && strings.HasPrefix(prefix, c.name+" ") {
				arg := strings.ToLower(prefix[len(c.name)+1:])
				for _, choice := range c.choices {
					if strings.HasPrefix(choice, arg) {
						s.items = append(s.items, suggestion{value: c.name + " " + choice, description: c.summary})
					}
				}
				break
			}
			// Insert the bare name, never the reference: "/max-steps N" is
			// how help describes it, and accepting that would type the
			// placeholder into the prompt.
			if strings.HasPrefix(c.name, strings.ToLower(prefix)) {
				s.items = append(s.items, suggestion{value: c.name, description: c.summary})
			}
		}
		return
	}
	if m.ws == nil {
		return
	}
	path := strings.TrimPrefix(strings.TrimPrefix(prefix, "@"), "\"")
	path = strings.ReplaceAll(path, "\\", "/")
	dir, name := "", path
	if i := strings.LastIndex(path, "/"); i >= 0 {
		dir, name = path[:i+1], path[i+1:]
	}
	full, err := referencePath(m.ws, dir)
	if err != nil {
		return
	}
	f, err := os.Open(full)
	if err != nil {
		return
	}
	// Bound work on each edit; browse one directory rather than walking the repo.
	entries, err := f.ReadDir(2048)
	f.Close()
	if err != nil && err != io.EOF {
		return
	}
	sort.Slice(entries, func(i, j int) bool {
		if entries[i].IsDir() != entries[j].IsDir() {
			return entries[i].IsDir()
		}
		return strings.ToLower(entries[i].Name()) < strings.ToLower(entries[j].Name())
	})
	for _, entry := range entries {
		if entry.Type()&os.ModeSymlink != 0 || (!strings.HasPrefix(name, ".") && strings.HasPrefix(entry.Name(), ".")) || !strings.HasPrefix(strings.ToLower(entry.Name()), strings.ToLower(name)) {
			continue
		}
		value := filepath.ToSlash(dir + entry.Name())
		if !entry.IsDir() && path == value {
			continue
		}
		description := "File reference"
		if entry.IsDir() {
			value += "/"
			description = "Browse directory"
		}
		s.items = append(s.items, suggestion{value, description, entry.IsDir()})
		if len(s.items) == 64 {
			break
		}
	}
}

// suggestionKey routes a key to an open popup. The second result is true
// when the popup consumed it, so the composer and the run state never see
// the same Enter.
func (m *model) suggestionKey(msg tea.KeyPressMsg) (tea.Cmd, bool) {
	s := &m.suggestions
	if len(s.items) == 0 {
		return nil, false
	}
	switch msg.String() {
	case "up":
		s.selected = (s.selected + len(s.items) - 1) % len(s.items)
	case "down":
		s.selected = (s.selected + 1) % len(s.items)
	case "esc":
		s.dismissed, s.items = s.key, nil
	case "tab":
		return nil, m.acceptSuggestion()
	case "enter":
		// A command that takes no argument is complete the moment it is
		// named, so Enter runs it. One that takes an argument hands the
		// text to the composer, which is how the value gets typed.
		item := s.items[s.selected]
		if !item.directory && strings.HasPrefix(item.value, "/") && !commandTakesArgument(item.value) {
			text := item.value
			s.items, s.key = nil, ""
			m.input.SetValue("")
			m.fitInput()
			m.fitBottom()
			_, cmd := m.command(text)
			return cmd, true
		}
		return nil, m.acceptSuggestion()
	default:
		return nil, false
	}
	return nil, true
}

// acceptSuggestion writes the selected item into the composer. File paths
// are inserted too: referencing a file is something the user finishes in
// the prompt, not an action to run.
func (m *model) acceptSuggestion() bool {
	s := &m.suggestions
	item := s.items[s.selected]
	value := item.value
	runes := []rune(m.input.Value())
	if runes[s.start] == '@' {
		if strings.ContainsAny(value, " \t") {
			value = "@\"" + value + "\""
		} else {
			value = "@" + value
		}
	}
	if !item.directory {
		value += " "
	}
	end := s.end
	if !item.directory && end < len(runes) && runes[end] == ' ' {
		end++
	}
	back := 0
	if item.directory && strings.HasSuffix(value, "\"") {
		back = 1
	}
	m.input.Replace(s.start, end, value, back)
	s.key = ""
	m.fitInput()
	m.fitBottom()
	return true
}

// Replace the bottom of the transcript; the composer and its caret stay put.
func (m *model) suggestionView(body string) string {
	s := &m.suggestions
	if len(s.items) == 0 || m.transcript.Viewport.Height() < 4 {
		return body
	}
	width := min(m.transcript.Viewport.Width(), 78)
	if width < 24 {
		return body
	}
	height := max(m.transcript.Viewport.Height(), lipgloss.Height(body))
	count := min(12, height-3, len(s.items))
	start := min(max(s.selected-count+1, 0), len(s.items)-count)
	normal, border, selected := popupStyles()
	title, action := "Files", "Enter / Tab insert"
	if strings.HasPrefix(s.items[0].value, "/") {
		title, action = "Commands", "Enter run  Tab insert"
	}
	rows := []string{border.Render("╭─ " + title + " " + strings.Repeat("─", width-len(title)-5) + "╮")}
	valueWidth := 0
	for _, item := range s.items {
		valueWidth = max(valueWidth, lipgloss.Width(item.value))
	}
	valueWidth = min(valueWidth, max((width-6)/2, 1))
	for i := start; i < start+count; i++ {
		item := s.items[i]
		mark, style := "  ", normal
		if i == s.selected {
			mark, style = "› ", selected
		}
		label := mark + cellLine(item.value, valueWidth) + "  " + item.description
		rows = append(rows, border.Render("│")+popupRow(style, label, width-2)+border.Render("│"))
	}
	hint := "↑↓ select  " + action + "  Esc dismiss"
	rows = append(rows, border.Render("│")+popupRow(normal, "  "+hint, width-2)+border.Render("│"))
	rows = append(rows, border.Render("╰"+strings.Repeat("─", width-2)+"╯"))

	lines := strings.Split(cellFrame(body, m.transcript.Viewport.Width(), height), "\n")
	return strings.Join(append(lines[:len(lines)-len(rows)], rows...), "\n")
}
