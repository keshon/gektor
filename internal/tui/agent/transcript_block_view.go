package agent

import (
	"fmt"
	"strings"
	"time"

	"charm.land/lipgloss/v2"
	"github.com/charmbracelet/x/ansi"

	"github.com/keshon/gektor/pkg/tui/components/cell"
	diffview "github.com/keshon/gektor/pkg/tui/components/diff"
	"github.com/keshon/gektor/pkg/tui/components/transcript"
	"github.com/keshon/gektor/pkg/tui/screen"
)

// roleLabel names optional message metadata in the transcript.
func roleLabel(r role) string {
	switch r {
	case roleUser:
		return "INPUT"
	case roleAnswer:
		return "RESPONSE"
	case roleThink:
		return "THINKING"
	case roleTool:
		return "TOOL CALL"
	case roleResult:
		return "TOOL RESULT"
	case roleGate:
		return "QUESTION"
	case roleMarker:
		return "HARNESS"
	case roleError:
		return "ERROR"
	case roleWarning:
		return "WARNING"
	case roleStatus:
		return "STATUS"
	case roleReceipt:
		return "OUTCOME"
	}
	return "OUTPUT"
}

func modernRoleLabel(r role) string {
	switch r {
	case roleUser:
		return "Request"
	case roleAnswer:
		return "Reply"
	case roleThink:
		return "Thinking"
	case roleTool:
		return "Tool"
	case roleResult:
		return "Result"
	case roleGate:
		return "Question"
	case roleMarker:
		return "System"
	case roleError:
		return "Error"
	case roleWarning:
		return "Warning"
	case roleStatus:
		return "Status"
	case roleReceipt:
		return "Run"
	}
	return "Output"
}

const systemSpeaker = "SYSTEM"

// speakerLabel keeps the configured participant name.
// User and assistant roles carry the configured assistant/user names;
// everything else is machine chrome.
func speakerLabel(b block, props transcriptProps) string {
	switch b.role {
	case roleUser:
		if props.modern {
			return props.user
		}
		return strings.ToUpper(props.user)
	case roleAnswer, roleThink, roleTool, roleResult:
		if props.modern {
			return props.speaker
		}
		return strings.ToUpper(props.speaker)
	case roleMarker:
		if props.modern {
			return "System"
		}
		return systemSpeaker
	default:
		if props.modern {
			return "System"
		}
		return systemSpeaker
	}
}

// speakerStyle colors the group name row, reusing the colors the body
// already carries: user blue, assistant gold, machine grey. A second
// palette here would drift from the one the roles already use.
func speakerStyle(r role, st styles) lipgloss.Style {
	switch r {
	case roleUser:
		return st.User
	case roleAnswer, roleThink, roleTool, roleResult:
		return st.Title
	default:
		return st.Dim
	}
}

// speakerRow draws the name row that opens a run of blocks by one
// participant. Only the first block of a run carries it; the rest of
// the run is identified by its label rows alone.
func speakerRow(name string, r role, st styles, width int) string {
	return ansi.Truncate(speakerStyle(r, st).Render(name), max(width, 1), "…")
}

// thinkSummary fits reasoning and its size into one terminal row.
func thinkSummary(s string, width int) string {
	line := strings.Join(strings.Fields(s), " ")
	suffix := fmt.Sprintf(" [%d chars]", len([]rune(s)))
	return cellLine(ansi.Truncate(line, max(width-lipgloss.Width(suffix), 1), "…")+suffix, max(width, 1))
}

// renderOpts are the view choices that change how one block renders.
// They belong to the transcript rather than the block: the same block
// reads differently depending on what the reader has chosen to hide,
// and nothing about the stored message changed.
type renderOpts struct {
	modern     bool
	disclosure bool
	inset      int
	// expand controls thinking only; tool content has its own preference.
	expand    bool
	hideTools bool
	// labels false drops the [TAG] row, independently of speaker names.
	labels bool
}

// renderBlock draws one transcript block: a label row carrying the role's
// tag and (when known) its time, then the body at the left edge. The body
// starts in column 0 because the label row already said what the block is —
// a per-line gutter repeating it cost two cells of every row to say nothing.
//
// Answer, user and think roles run the Markdown path; tool args,
// diffs, gates and markers stay raw so a glob `*.go` can never toggle bold.
// Lines reflow to width, since the viewport never wraps and an unbroken
// long line would vanish past the right edge.
func renderBlock(b block, st styles, opts renderOpts, width int) string {
	// Normalizing on the by-value copy covers every path to the terminal
	// without touching stored history.
	b.text = strings.TrimRight(cleanText(b.text), "\n")
	if b.role == roleAnswer || b.role == roleThink {
		b.text = strings.Trim(b.text, "\n")
	}
	b.result = strings.TrimRight(cleanText(b.result), "\n")
	if opts.modern && b.contextText != "" && b.role != roleTool {
		b.text = b.contextText
	}
	b = normalizeHarnessMarker(b)
	raw := b.text
	if b.role == roleThink && !opts.expand {
		if opts.modern {
			raw = cellLine(ansi.Truncate(strings.Join(strings.Fields(b.text), " "), max(width, 1), "…"), width)
		} else {
			raw = thinkSummary(b.text, width)
		}
	}
	var body []string
	switch {
	case opts.modern && b.role == roleUser:
		body = renderRaw(b, raw, st, width)
	case b.role == roleThink && !opts.expand:
		body = []string{st.Think.Render(raw)}
	case b.role == roleTool && opts.hideTools:
		body = []string{compactTool(b, st, width)}
	case b.role == roleReceipt:
		if opts.modern {
			body = receiptSummary(b, st, width)
		} else {
			body = receiptText(b, st, width)
		}
	case b.role == roleResult && opts.hideTools:
		body = []string{st.Dim.Render(cellLine(fmt.Sprintf("Tool result"+screen.Separator()+"%d bytes", len(b.text)), width))}
	case opts.modern && b.role == roleResult:
		if view, ok := diffview.Render(raw, width, diffStyles(st)); ok {
			body = strings.Split(view, "\n")
		} else {
			body = renderRaw(b, raw, st, width)
		}
	case b.role == roleTool:
		body = renderToolCard(b, st, width, opts.modern)
	case isMdRole(b.role):
		body = renderMdBlock(b, raw, st, width)
	default:
		body = renderRaw(b, raw, st, width)
	}
	if !opts.labels || (b.role == roleTool && opts.hideTools) {
		return strings.Join(body, "\n")
	}
	label := roleLabel(b.role)
	if opts.modern {
		label = modernRoleLabel(b.role)
	}
	return strings.Join(append([]string{labelRow(label, b.at, st, width, opts.modern)}, body...), "\n")
}

// labelRow draws an optional message label at the left edge, with the
// timestamp flush right. The body owns no label column, so this row is
// the block's only chrome and has the full width to work with.
func labelRow(label string, at time.Time, st styles, width int, modern bool) string {
	if !modern {
		label = "[" + label + "]"
	}
	line := st.Dim.Render(label)
	// A narrow terminal cannot hold both. The tag is structure and the
	// time is decoration, so chrome yields rather than pushing past the
	// edge or shredding the label into two wrapped rows.
	if at.IsZero() || !stampFits(line, width) {
		return line
	}
	return alignStamp(line, st, at, width)
}

// stampFits reports whether a line can carry a right-aligned stamp: the
// suffix is stampWidth cells and needs at least one cell of padding
// between itself and the content.
func stampFits(line string, width int) bool {
	return lipgloss.Width(line)+stampWidth+1 <= width
}

// renderRaw draws literal text at the left edge, one role-colored
// fragment per line. Harness prompts, paths, diagnostics, tool args and
// diffs stay raw so a glob cannot toggle bold and a `+` prefix survives
// as a diff rather than becoming prose.
func renderRaw(b block, raw string, st styles, width int) []string {
	var out []string
	for _, line := range strings.Split(raw, "\n") {
		for _, fragment := range reflow(line, max(width, 1)) {
			out = append(out, applyRoleStyle(b.role, line, fragment, st))
		}
	}
	return out
}

const (
	toolRail  = "│ "
	toolClose = "└─"
)

// toolRailWidth is what the rail costs on every output row.
var toolRailWidth = lipgloss.Width(toolRail)

// renderToolCard draws the complete call and output, wrapping arguments
// without discarding them. Compact summaries use a separate renderer.
func renderToolCard(b block, st styles, width int, modern bool) []string {
	glyph := transcript.ActionGlyph(toolState(b), toolActionStyles(st))
	var out []string
	call := b.text
	if modern {
		_, action, target := toolSummary(b.text)
		call = strings.TrimSpace(action + " " + target)
	}
	for i, call := range reflow(call, max(width-2, 1)) {
		prefix := "  "
		if i == 0 {
			prefix = glyph + " "
		}
		out = append(out, prefix+st.Dim.Render(call))
	}
	// Before the first chunk, show a pending row. Once output arrives the
	// same card renders it while retaining its running glyph.
	result := b.result
	if b.open && result == "" {
		result = "…"
	}
	if modern {
		if view, ok := diffview.Render(result, max(width-2, 1), diffStyles(st)); ok {
			for _, line := range strings.Split(view, "\n") {
				out = append(out, "  "+line)
			}
			return out
		}
	}
	lines := strings.Split(result, "\n")
	// Reflow every output line inside the rail; the closing corner has
	// a separate row and never consumes the final content row.
	wrapW := max(width-toolRailWidth, 1)
	orig := make([]string, 0, len(lines))
	frags := make([]string, 0, len(lines))
	for _, line := range lines {
		for _, f := range reflow(line, wrapW) {
			orig = append(orig, line)
			frags = append(frags, f)
		}
	}
	prefix := toolRail
	if modern {
		prefix = "  "
	}
	for i, f := range frags {
		out = append(out, st.Dim.Render(prefix)+resultLineStyle(orig[i], f, st))
	}
	if !modern {
		out = append(out, st.Dim.Render(toolClose))
	}
	return out
}

func diffStyles(st styles) diffview.Styles {
	return diffview.Styles{Text: lipgloss.NewStyle(), Muted: st.Dim, Added: st.Add, Removed: st.Del, Header: st.Hunk}
}

// isMdRole reports whether the role renders Markdown. Tool args
// (`*.go` globs), diffs and notices stay raw: a harness prompt, a path
// or a diagnostic is literal text, and reformatting it would invent
// structure its author never wrote.
func isMdRole(r role) bool {
	return r == roleAnswer || r == roleUser || r == roleThink
}

// stampWidth is the " [15:04]" suffix reserved on the label row.
const stampWidth = 8

// alignStamp appends the timestamp right-aligned to width: content
// left, time as a clean right column. Padding sits between content and
// stamp, so no line ever ends in whitespace.
func alignStamp(line string, st styles, at time.Time, width int) string {
	stamp := st.Dim.Render(" [" + at.Format("15:04") + "]")
	pad := width - lipgloss.Width(line) - stampWidth
	if pad < 1 {
		pad = 1
	}
	return line + strings.Repeat(" ", pad) + stamp
}

// applyRoleStyle colors one fragment for the raw (non-markdown) roles.
// Answer/user/think/marker never reach it: they render through renderMdBlock.
func applyRoleStyle(r role, origLine, frag string, st styles) string {
	switch r {
	case roleTool:
		return st.Dim.Render(frag)
	case roleResult:
		return resultLineStyle(origLine, frag, st)
	case roleGate:
		return st.Gate.Render(frag)
	case roleMarker, roleStatus:
		return st.Dim.Render(frag)
	case roleError:
		return st.Err.Render(frag)
	case roleWarning:
		return st.Warn.Render(frag)
	case roleReceipt:
		if strings.HasPrefix(origLine, "outcome:") {
			return st.Gate.Bold(true).Render(frag)
		}
		return st.Dim.Render(frag)
	default:
		return frag
	}
}

// resultLineStyle colors one fragment by its logical line's diff prefix.
func resultLineStyle(origLine, frag string, st styles) string {
	switch {
	case strings.HasPrefix(origLine, "+") && !strings.HasPrefix(origLine, "++"):
		return st.Add.Render(frag)
	case strings.HasPrefix(origLine, "-") && !strings.HasPrefix(origLine, "--"):
		return st.Del.Render(frag)
	case strings.HasPrefix(origLine, "@@"):
		return st.Hunk.Render(frag)
	default:
		return st.Dim.Render(frag)
	}
}

// reflow word-wraps one logical line to width columns. Leading
// whitespace survives on the first fragment; long words hard-cut
// rune-safe; tabs expand to 4. ANSI-free input only: callers wrap raw
// text before styling.
func reflow(line string, width int) []string {
	line = strings.ReplaceAll(line, "\t", "    ")
	return cell.Wrap(line, width)
}

func systemBlock(r role) bool {
	switch r {
	case roleUser, roleAnswer, roleThink, roleTool, roleResult:
		return false
	}
	return true
}

func railText(text string, st styles, width int) string {
	lines := strings.Split(text, "\n")
	for i := range lines {
		lines[i] = lipgloss.NewStyle().Foreground(lipgloss.Color(themeColor(colorMode))).Render("│ ") + cellLine(lines[i], max(width-2, 1))
	}
	return strings.Join(lines, "\n")
}

func messageWidth(r role, width, inset int) int {
	if inset == 0 {
		inset = 2
	}
	if r == roleUser {
		inset = 4
	}
	return max(width-inset, 1)
}

// presentMessage is shared by saved turns and streaming parts. The body is
// already wrapped to messageWidth, so adding its surface never changes tool rows.
func presentMessage(body string, b block, props transcriptProps, width int, showName bool, opts renderOpts) string {
	heading := ""
	if showName {
		heading = speakerLabel(b, props)
	}
	if props.labels && b.role != roleReceipt {
		label := modernRoleLabel(b.role)
		if heading != "" && heading != label {
			heading += " ▪ "
		}
		if heading != label {
			heading += label
		}
	}
	if b.role == roleThink && !props.labels {
		if heading != "" {
			heading += " ▪ "
		}
		heading += "Thinking"
	}
	if b.role == roleThink && opts.disclosure {
		if opts.expand {
			heading = "− " + heading
		} else {
			heading = "+ " + heading
		}
	}
	if b.role == roleReceipt {
		heading = ""
	}
	inner := messageWidth(b.role, width, opts.inset)
	if heading != "" {
		body = cellLine(props.styles.Dim.Render(heading), inner) + "\n" + body
	}
	if b.role == roleUser && width >= 6 {
		surface := lipgloss.NewStyle().Background(lipgloss.Color(themeColor(colorPopupBackground)))
		rail := props.styles.Hunk.Background(lipgloss.Color(themeColor(colorPopupBackground))).Render("▎ ")
		lines := []string{strings.Repeat(" ", inner)}
		lines = append(lines, strings.Split(body, "\n")...)
		lines = append(lines, strings.Repeat(" ", inner))
		for i, line := range lines {
			style := surface.Foreground(lipgloss.Color(themeColor(colorPopupText)))
			if heading != "" && i == 1 {
				style = style.Foreground(lipgloss.Color(themeColor(colorDim)))
			}
			lines[i] = rail + style.Render(cellLine(ansi.Strip(line), inner)+"  ")
		}
		return strings.Join(lines, "\n")
	}
	prefix := strings.Repeat(" ", max(width-inner, 0))
	lines := strings.Split(body, "\n")
	for i := range lines {
		lines[i] = prefix + lines[i]
	}
	return strings.Join(lines, "\n")
}
