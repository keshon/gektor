package tui

import (
	"path/filepath"
	"strconv"
	"strings"

	"charm.land/lipgloss/v2"
	"github.com/charmbracelet/x/ansi"
	"github.com/keshon/gektor/internal/tui/components/popup"
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

// infoDialog is a centered modal over the transcript: title, body lines
// and optional help tabs. Its frame overlays the document without clearing
// the surrounding history or changing the underlying reading position.
//
// A dialog carries lines or a form, never both. Anything that needs an
// answer from the user is a form: a destructive confirmation is a form with
// no fields, which is what makes it consistent with the delete prompt.
type infoDialog struct {
	form   *settingsForm
	title  string
	lines  []string
	help   bool
	page   int
	offset int
}

// openDialog pushes the overlay and shows an info dialog; closeDialog
// pops it. Balanced by construction: every open path pairs with esc/enter
// or run start.
func (m *model) openDialog(title string, lines []string) {
	m.cancelEvaluation()
	m.dialog = &infoDialog{title: title, lines: lines}
	m.pushOverlay()
	m.input.Blur()
	m.fitBottom()
}

func (m *model) closeDialog() {
	m.cancelEvaluation()
	m.dialog = nil
	m.popOverlay()
	m.fitBottom()
}

// dialogView renders the open dialog centered in the viewport area:
// title, body, close hint. Widths are clamped to the viewport; heights
// are top-padded to center, the viewport clips the rest.
type dialogLayout struct {
	rows, lines                    []string
	hint                           string
	inner, capacity, width, height int
}

func (m *model) dialogView() string {
	if m.dialog.form != nil {
		return m.formView()
	}
	rows, width, height := m.dialogRows()
	return renderBox(rows, width, height)
}

func (m *model) overlayDialog(background string) string {
	if m.dialog.form != nil {
		return m.dialog.form.Overlay(background, m.settingsProps())
	}
	rows, width, height := m.dialogRows()
	return popup.OverlayBox(background, rows, width, height, popupSurface())
}

func (m *model) dialogRows() ([]string, int, int) {
	layout := m.layoutDialog()
	offset := min(max(m.dialog.offset, 0), max(len(layout.lines)-layout.capacity, 0))
	rows := layout.rows
	for _, line := range layout.lines[offset:min(offset+layout.capacity, len(layout.lines))] {
		rows = append(rows, cellLine(line, layout.inner))
	}
	rows = append(rows, "")
	if len(layout.lines) > layout.capacity {
		rows = append(rows, m.styles.dim.Render(strconv.Itoa(offset+1)+"–"+strconv.Itoa(min(offset+layout.capacity, len(layout.lines)))+" / "+strconv.Itoa(len(layout.lines))))
	}
	rows = append(rows, m.styles.dim.Render(truncate(layout.hint, layout.inner)))
	return rows, layout.width, layout.height
}

func (m *model) scrollDialog(delta int) {
	if m.dialog == nil || m.dialog.form != nil {
		return
	}
	layout := m.layoutDialog()
	m.dialog.offset = min(max(m.dialog.offset+delta, 0), max(len(layout.lines)-layout.capacity, 0))
}

func (m *model) layoutDialog() dialogLayout {
	d := m.dialog
	availW, availH := m.termW, m.termH-3
	if m.ready {
		availW, availH = m.transcript.Viewport.Width(), m.transcript.Viewport.Height()
	}
	inner := max(popup.ModalWidth(availW)-4, 1)
	// Info dialog: title, lines, close hint (with help tabs if applicable).
	rows := []string{m.styles.gate.Render(truncate(d.title, inner))}
	lines := []string{}
	for _, line := range d.lines {
		lines = append(lines, strings.Split(ansi.Wrap(cleanText(line), max(inner, 1), ""), "\n")...)
	}
	hint := "Esc / Enter close"
	if m.evaluation != nil {
		hint = "Esc / Enter cancel evaluation"
	}
	if d.help {
		sections := m.dialogHelpSections()
		page := min(max(d.page, 0), len(sections)-1)
		tabs := make([]string, len(sections))
		for i, section := range sections {
			label := section.title
			if i == page {
				label = m.styles.gate.Render("[" + label + "]")
			} else {
				label = m.styles.dim.Render(label)
			}
			tabs[i] = label
		}
		if len(sections) > 1 {
			rows = append(rows, strings.Split(lipgloss.NewStyle().Width(inner).Render(strings.Join(tabs, "  ")), "\n")...)
		}
		if availH >= 10 {
			rows = append(rows, "")
		}
		lines = m.helpRows(sections[page], inner)
		hint = "Tab / ← → category   ↑ ↓ scroll   Esc close"
		if len(sections) == 1 {
			hint = "↑ ↓ scroll   Esc close"
		}
	}
	capacity := max(availH-2-len(rows)-3, 1)
	// Reserve an extra row for the scroll-position indicator when needed.
	if len(lines) > capacity {
		capacity = max(capacity-1, 1)
	}
	return dialogLayout{rows: rows, lines: lines, hint: hint, inner: inner,
		capacity: capacity, width: availW, height: availH}
}

// renderBox centers rows in a bordered box: shared by the dialog and
// the sessions screen, so overlay chrome can never drift in two.
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

func (m *model) openHelp() {
	m.openDialog("Help", nil)
	m.dialog.help = true
}

func helpSections() []helpSection {
	return []helpSection{
		{"Input", [][2]string{
			{"Enter", "Send a message; queue a follow-up while running"},
			{"Ctrl+O / Shift+Enter", "Insert a new line"},
			{"Up / Down", "Move within the draft"},
			{"Alt+Up / Alt+Down", "Recall input history"},
			{"Ctrl+U", "Clear the draft"},
			{"/ or @", "Suggest commands or workspace paths"},
			{"Enter / Tab / Esc", "Run a command, insert a suggestion, or dismiss the picker"},
			{"Ctrl+E", "Move the queued follow-up back to the draft"},
			{"Ctrl+X", "Cancel the queued follow-up"},
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
func (m *model) helpRows(section helpSection, width int) []string {
	keyWidth := 0
	for _, row := range section.rows {
		keyWidth = max(keyWidth, lipgloss.Width(row[0]))
	}
	var rows []string
	for _, row := range section.rows {
		if width-keyWidth-3 < 24 {
			rows = append(rows, m.styles.gate.Render(row[0]))
			for _, line := range strings.Split(lipgloss.NewStyle().Width(max(width-2, 1)).Render(row[1]), "\n") {
				rows = append(rows, "  "+line)
			}
		} else {
			description := strings.Split(lipgloss.NewStyle().Width(width-keyWidth-3).Render(row[1]), "\n")
			rows = append(rows, m.styles.gate.Render(cellLine(row[0], keyWidth))+"   "+description[0])
			for _, line := range description[1:] {
				rows = append(rows, strings.Repeat(" ", keyWidth+3)+line)
			}
		}
		rows = append(rows, "")
	}
	return rows[:max(len(rows)-1, 0)]
}

// statusLines snapshots run facts for the /status dialog. Everything
// shown is model state or construction facts; the TUI never re-reads
// Env, so nothing here can disagree with the run.
func (m *model) statusLines() []string {
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
func (m *model) dialogHelpSections() []helpSection {
	sections := m.helpSections()
	if m.dialog.title != "Keyboard shortcuts" && m.dialog.title != "Commands" {
		return sections
	}
	filtered := make([]helpSection, 0, len(sections))
	for _, section := range sections {
		if (section.title == "Commands") == (m.dialog.title == "Commands") {
			filtered = append(filtered, section)
		}
	}
	return filtered
}

func (m *model) helpSections() []helpSection {
	return helpSections()
}

func (m *model) profileSource() string {
	if m.application != nil && m.application.Preferences.ProfilePath != "" {
		return filepath.Base(m.application.Preferences.ProfilePath)
	}
	return "Not saved"
}
