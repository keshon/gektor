package agent

import (
	"path/filepath"
	"strconv"
	"strings"

	"charm.land/lipgloss/v2"

	"github.com/keshon/gektor/internal/tui/components/popup"
	"github.com/keshon/gektor/internal/tui/screen"
	"github.com/keshon/gektor/internal/workspace"
)

// wsRootOf names the workspace root, empty when none (unit models
// render a dash via nonEmpty).
func wsRootOf(ws *workspace.Workspace) string {
	if ws == nil {
		return ""
	}
	return ws.Root()
}

// renderBox centers a standalone bordered dialog for previews. The frame
// itself is components/popup, shared with the completion list, the session
// search and the header menus so their chrome cannot drift.
func renderBox(rows []string, availW, availH int) string {
	return popup.FrameBox(rows, availW, availH, popupSurface())
}

// popupSurface hands the popup component this palette's three surface
// styles, so no overlay has to re-derive them.
func popupSurface() popup.Styles {
	normal, border, selected := popupStyles()
	return popup.Styles{Normal: normal, Border: border, Selected: selected}
}

// helpSection groups rows under a title for the two-column help:
// keys left, descriptions right, aligned per section. Single copy
// shared by the command and tests, so help can never rot in two.
type helpSection struct {
	title string
	rows  [][2]string
}

func (m *Model) openHelp() {
	m.chrome.Open("Help", nil)
	m.chrome.Dialog.Help = true
}

func helpSections() []helpSection {
	return []helpSection{
		{"Input", [][2]string{
			{"Enter", "Send a message; queue a follow-up while running"},
			{"Ctrl+O / Shift+Enter", "Insert a new line"},
			{"Ctrl+V", "Paste text or an image when forwarded by the terminal"},
			{"Ctrl+T / click tool", "Select tools; Enter opens the inspector"},
			{"Tab / Esc in inspector", "Switch to parent / close inspector"},
			{"Ctrl+Alt+V / /paste-image", "Attach a clipboard image"},
			{"Alt+1…8 / /detach N", "Remove an attached image"},
			{"Up / Down", "Move within the draft"},
			{"Alt+Up / Alt+Down", "Recall input history"},
			{"Ctrl+U", "Clear the draft"},
			{"/ or @", "Suggest commands or workspace paths"},
			{"Enter / Tab / Esc", "Run a command, insert a suggestion, or dismiss the picker"},
			{"Alt+Q", "Read, edit or remove a specific queued message"},
			{"Ctrl+E", "Return the latest pending message to the draft"},
			{"Ctrl+X", "Cancel the latest pending message"},
			{"@\"image path\"", "Attach a workspace file or image; quote paths with spaces"},
		}},
		{"Navigate", [][2]string{
			{"F1", "Open or close help"},
			{"F2 / Ctrl+P", "Search session names and history"},
			{"F3 / Ctrl+G", "Show full thinking or a one-line summary"},
			{"Ctrl+R / Ctrl+D", "Rename / delete the selected session in Open session"},
			{"PgUp / PgDn", "Scroll the transcript; mouse wheel also works"},
			{"F7 / Ctrl+End", "Jump to the latest message"},
			{"F9", "Focus header badges; Left / Right choose, Enter opens"},
			{"Header menus", "Click a badge; Up / Down browse, Enter choose, Esc back"},
		}},
		{"Run", [][2]string{
			{"F4 / Ctrl+N", "Start a new session when idle"},
			{"F5", "Switch Plan / Act mode when idle"},
			{"Esc", "Stop the current run; keep the session open"},
			{"Y", "Allow the requested tool once"},
			{"A", "Allow for this run, after confirmation"},
			{"N", "Reject a tool; optionally add a redirect note"},
			{"Ctrl+Q / F10", "Quit when idle; stop an active run first"},
			{"Ctrl+C", "Abort and quit, including during a run"},
		}},
		{"Commands", commandReference()},
	}
}

// Keep descriptions complete: narrow terminals stack them below the keys.
func (m *Model) helpRows(section helpSection, width int) []string {
	return m.chrome.HelpRows(screen.HelpSection{Title: section.title, Rows: section.rows}, width)
}

func (m *Model) statusLines() []string {
	taskDir := "—"
	if m.stateFile != "" {
		taskDir = filepath.Dir(m.stateFile)
	}
	// The toggle's own name comes from the bottom bar ("Ctrl+G
	// details"), not the history internals: compact here describes the
	// transcript view, never context compaction.
	details := "collapsed (Ctrl+G)"
	if !m.transcript.compact {
		details = "full (Ctrl+G)"
	}
	// Run-local scope matters: these grants die with the process.
	allowed := "none"
	m.alwaysMu.Lock()
	n := len(m.always)
	m.alwaysMu.Unlock()
	if n > 0 {
		allowed = strconv.Itoa(n) + " (this run only)"
	}
	return kvRows([]string{
		"mode: " + m.modeName(),
		"identity: " + m.speakerName(),
		"user: " + m.userName(),
		"profile file: " + m.profileSource(),
		"backend: " + nonEmpty(m.backendKind),
		"model: " + nonEmpty(m.modelName),
		"workspace: " + nonEmpty(m.wsRoot),
		"task dir: " + taskDir,
		"context: " + m.meter(),
		"run: " + strconv.Itoa(m.steps) + " steps  " + m.statusWord() + "  " + formatElapsed(m.elapsed),
		"thinking: " + details,
		"permissions: Y allow once  A always allow  N deny with note",
		"always allowed: " + allowed,
		"tools: " + strconv.Itoa(m.mcpCount) + " mcp  " + strconv.Itoa(m.policyRules) + " policy rules",
	})
}

func nonEmpty(s string) string {
	if s == "" {
		return "—"
	}
	return s
}

// kvRows aligns "label: value" rows so values start in one column:
// short labels are padded, never truncated. Borderless on purpose —
// status facts, not data grids (markdown tables already cover those).
//
// Padding is measured in cells, like every other column aligner in the
// package. It used to be measured in bytes, which meant a label containing
// anything non-ASCII pushed its own value out of line: "Сервер: x" measured
// six columns and padded by twelve spaces.
func kvRows(rows []string) []string {
	split := func(r string) (label, rest string, ok bool) {
		i := strings.Index(r, ":")
		if i < 0 {
			return "", r, false
		}
		return r[:i], r[i:], true
	}
	w := 0
	for _, r := range rows {
		if label, _, ok := split(r); ok {
			w = max(w, lipgloss.Width(label))
		}
	}
	out := make([]string, len(rows))
	for i, r := range rows {
		label, rest, ok := split(r)
		if !ok {
			out[i] = r
			continue
		}
		out[i] = cellLine(label, w) + rest
	}
	return out
}

// dialogHelpSections is shared by category navigation and rendering.
func (m *Model) dialogHelpSections() []helpSection {
	sections := m.helpSections()
	if m.chrome.Dialog.Title != "Keyboard shortcuts" && m.chrome.Dialog.Title != "Commands" {
		return sections
	}
	filtered := make([]helpSection, 0, len(sections))
	for _, section := range sections {
		if (section.title == "Commands") == (m.chrome.Dialog.Title == "Commands") {
			filtered = append(filtered, section)
		}
	}
	return filtered
}

func (m *Model) helpSections() []helpSection {
	return helpSections()
}

func (m *Model) profileSource() string {
	if m.application != nil && m.application.Preferences.ProfilePath != "" {
		return filepath.Base(m.application.Preferences.ProfilePath)
	}
	return "Not saved"
}
