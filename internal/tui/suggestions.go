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
	"github.com/keshon/gektor/internal/tui/components/completion"
	"github.com/keshon/gektor/internal/tui/components/popup"
)

// The picker itself — its items, its selection and its frame — lives in
// components/completion. What stays here is GEKTOR's side: deciding which
// commands and paths match the token at the caret, and what accepting one
// means.

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
	off := m.activeInputOwner() != focusInput ||
		(m.ready && (m.transcript.Viewport.Height() < 4 || m.transcript.Viewport.Width() < 24)) ||
		(m.state != stDone && m.state != stRunning)
	start, end, prefix := m.completionToken()
	key := m.input.Value() + "\x00" + strconv.Itoa(start) + ":" + strconv.Itoa(end) + ":" + prefix

	if off || prefix == "" {
		s.Close()
		return
	}
	if !s.SetKey(key) {
		return
	}
	s.Start, s.End = start, end
	s.Open(m.completionItems(prefix), key)
}

// completionItems matches the token at the caret against the command
// registry or the workspace, whichever the token is for.
func (m *model) completionItems(prefix string) []completion.Item {
	if prefix[0] == '/' {
		return commandItems(prefix)
	}
	return pathItems(m.ws, prefix)
}

// commandItems offers command names, or a command's fixed values once its
// name is typed. The registry decides both, so a new enumerated command
// gets its values completed without a second edit.
func commandItems(prefix string) []completion.Item {
	var items []completion.Item
	for _, c := range commands() {
		if len(c.choices) > 0 && strings.HasPrefix(prefix, c.name+" ") {
			arg := strings.ToLower(prefix[len(c.name)+1:])
			for _, choice := range c.choices {
				if strings.HasPrefix(choice, arg) {
					items = append(items, completion.Item{Value: c.name + " " + choice, Description: c.summary})
				}
			}
			return items
		}
		// Offer the bare name, never the reference: "/max-steps N" is how
		// help describes it, and accepting that would type the placeholder
		// into the prompt.
		if strings.HasPrefix(c.name, strings.ToLower(prefix)) {
			items = append(items, completion.Item{Value: c.name, Description: c.summary})
		}
	}
	return items
}

// pathItems browses one directory rather than walking the repo: completion
// runs on every keystroke, and a repository walk per keypress is a stall.
func pathItems(ws workspaceResolver, prefix string) []completion.Item {
	if ws == nil {
		return nil
	}
	path := strings.ReplaceAll(strings.TrimPrefix(strings.TrimPrefix(prefix, "@"), "\""), "\\", "/")
	dir, name := "", path
	if i := strings.LastIndex(path, "/"); i >= 0 {
		dir, name = path[:i+1], path[i+1:]
	}
	f, err := ws.Open(dir)
	if err != nil {
		return nil
	}
	entries, err := f.ReadDir(2048)
	f.Close()
	if err != nil && err != io.EOF {
		return nil
	}
	sort.Slice(entries, func(i, j int) bool {
		if entries[i].IsDir() != entries[j].IsDir() {
			return entries[i].IsDir()
		}
		return strings.ToLower(entries[i].Name()) < strings.ToLower(entries[j].Name())
	})

	var items []completion.Item
	for _, entry := range entries {
		if entry.Type()&os.ModeSymlink != 0 ||
			(!strings.HasPrefix(name, ".") && strings.HasPrefix(entry.Name(), ".")) ||
			!strings.HasPrefix(strings.ToLower(entry.Name()), strings.ToLower(name)) {
			continue
		}
		value := filepath.ToSlash(dir + entry.Name())
		description := "File reference"
		dirEntry := entry.IsDir()
		if dirEntry {
			value += "/"
			description = "Browse directory"
		} else if path == value {
			continue
		}
		items = append(items, completion.Item{Value: value, Description: description, Directory: dirEntry})
		if len(items) == 64 {
			break
		}
	}
	return items
}

// workspaceResolver is the slice of *workspace.Workspace completion needs,
// named so this file does not import the workspace package for one method.
type workspaceResolver interface {
	Open(string) (*os.File, error)
}

// suggestionKey routes a key to an open picker. The second result is true
// when the picker consumed it, so the composer and the run state never see
// the same Enter.
func (m *model) suggestionKey(msg tea.KeyPressMsg) (tea.Cmd, bool) {
	s := &m.suggestions
	if !s.Pending() {
		return nil, false
	}
	switch msg.String() {
	case "up", "down", "pgup", "pgdown", "esc":
		layout, _, _ := m.suggestionLayout()
		s.Keypress(msg, layout.BodyHeight)
		return nil, true
	case "tab":
		m.acceptSuggestion()
		return nil, true
	case "enter":
		// A command that takes no argument is complete the moment it is
		// named, so Enter runs it — unless a run is in progress and it
		// acts on that run, in which case it waits. One that takes an
		// argument hands the text to the composer, which is how the
		// value gets typed.
		item := s.Current()
		if !item.Directory && strings.HasPrefix(item.Value, "/") && m.runsOnEnter(item.Value) {
			text := item.Value
			s.Close()
			m.input.SetValue("")
			m.fitInput()
			m.fitBottom()
			_, cmd := m.command(text)
			return cmd, true
		}
		m.acceptSuggestion()
		return nil, true
	}
	return nil, false
}

// acceptSuggestion writes the selected item into the composer. File paths
// are inserted too: referencing a file is something the user finishes in
// the prompt, not an action to run.
func (m *model) acceptSuggestion() bool {
	s := &m.suggestions
	item := s.Current()
	value := item.Value
	runes := []rune(m.input.Value())
	if runes[s.Start] == '@' {
		if strings.ContainsAny(value, " \t") {
			value = "@\"" + value + "\""
		} else {
			value = "@" + value
		}
	}
	if !item.Directory {
		value += " "
	}
	end := s.End
	if !item.Directory && end < len(runes) && runes[end] == ' ' {
		end++
	}
	back := 0
	if item.Directory && strings.HasSuffix(value, "\"") {
		back = 1
	}
	m.input.Replace(s.Start, end, value, back)
	s.Close()
	m.fitInput()
	m.fitBottom()
	return true
}

// suggestionLayout shares the popup's frame and screen origin with mouse input.
func (m *model) suggestionLayout() (completion.Layout, int, int) {
	width, height := min(m.transcript.Viewport.Width(), 78), m.transcript.Viewport.Height()
	l := m.suggestions.Layout(completion.Props{Width: width, Height: height, Surface: popupSurface(), Title: m.styles.title, Shortcuts: m.shortcutStyles(),
		Runnable: func(item completion.Item) bool {
			return !item.Directory && strings.HasPrefix(item.Value, "/") && m.runsOnEnter(item.Value)
		}})
	geometry := m.panelGeometry(m.mainWidth(), 2, false, true)
	return l, geometry.Body.X, paneTop + height - len(l.Rows)
}

// suggestionView overlays the transcript while keeping the composer in place.
func (m *model) suggestionView(body string) string {
	l, _, _ := m.suggestionLayout()
	if len(l.Rows) == 0 {
		return body
	}
	height := m.transcript.Viewport.Height()
	return popup.Overlay(cellFrame(body, m.transcript.Viewport.Width(), height), l.Rows, 0, height-len(l.Rows), l.Width, m.transcript.Viewport.Width())
}

func (m *model) suggestionMouse(msg tea.MouseMsg) bool {
	if m.activeInputOwner() != focusInput || !m.suggestions.Pending() || (m.state != stDone && m.state != stRunning) {
		return false
	}
	l, x, y := m.suggestionLayout()
	mouse := msg.Mouse()
	mouse.X -= x
	mouse.Y -= y
	switch msg.(type) {
	case tea.MouseClickMsg:
		return m.suggestions.Mouse(tea.MouseClickMsg(mouse), l)
	case tea.MouseWheelMsg:
		return m.suggestions.Mouse(tea.MouseWheelMsg(mouse), l)
	case tea.MouseMotionMsg:
		return m.suggestions.Mouse(tea.MouseMotionMsg(mouse), l)
	case tea.MouseReleaseMsg:
		return m.suggestions.Mouse(tea.MouseReleaseMsg(mouse), l)
	}
	return false
}
