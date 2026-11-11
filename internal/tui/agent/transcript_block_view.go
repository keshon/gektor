package agent

import (
	"fmt"
	"strings"
	"time"

	"charm.land/lipgloss/v2"
	"github.com/charmbracelet/x/ansi"

	"github.com/keshon/gektor/internal/tui/components/transcript"
)

// roleLabel is the bracketed tag on a block's label row. One tag per role
// keeps the transcript systematic: the reader learns the whole vocabulary
// from a few lines and never parses speaker prose to tell a refusal from a
// plan.
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

const systemSpeaker = "SYSTEM"

// speakerLabel names the participant a block belongs to, uppercased.
// User and assistant roles carry the configured assistant/user names;
// everything else is machine chrome.
func speakerLabel(b block, props transcriptProps) string {
	switch b.role {
	case roleUser:
		return strings.ToUpper(props.user)
	case roleAnswer, roleThink, roleTool, roleResult:
		return strings.ToUpper(props.speaker)
	case roleMarker:
		return systemSpeaker
	default:
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
		return st.Gate
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
// Answer, user and think roles run the markdown-lite path; tool args,
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
	b = normalizeHarnessMarker(b)
	raw := b.text
	if b.role == roleThink && !opts.expand {
		// Collapsed keeps the summary-only rule (size here, never in
		// full view); the label row carries the tag either way, so the
		// body never repeats it.
		raw = thinkSummary(b.text, width)
	}
	var body []string
	switch {
	case b.role == roleThink && !opts.expand:
		body = []string{st.Think.Render(raw)}
	case b.role == roleTool && opts.hideTools:
		body = []string{compactTool(b, st, width)}
	case b.role == roleReceipt:
		body = receiptText(raw, st, width)
	case b.role == roleResult && opts.hideTools:
		body = []string{st.Dim.Render(cellLine(fmt.Sprintf("Tool result · %d bytes", len(b.text)), width))}
	case b.role == roleTool:
		body = renderToolCard(b, st, width)
	case isMdRole(b.role):
		body = renderMdBlock(b, raw, st, width)
	default:
		body = renderRaw(b, raw, st, width)
	}
	if !opts.labels {
		return strings.Join(body, "\n")
	}
	return strings.Join(append([]string{labelRow(roleLabel(b.role), b.at, st, width)}, body...), "\n")
}

// labelRow draws a block's tag line: [LABEL] at the left edge, the
// timestamp flush right. The body owns no label column, so this row is
// the block's only chrome and has the full width to work with.
func labelRow(label string, at time.Time, st styles, width int) string {
	line := st.Dim.Render("[" + label + "]")
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
func renderToolCard(b block, st styles, width int) []string {
	glyph := transcript.ActionGlyph(toolState(b), toolActionStyles(st))
	var out []string
	for i, call := range reflow(b.text, max(width-2, 1)) {
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
	for i, f := range frags {
		out = append(out, st.Dim.Render(toolRail)+resultLineStyle(orig[i], f, st))
	}
	out = append(out, st.Dim.Render(toolClose))
	return out
}

// isMdRole reports whether the role renders markdown-lite. Tool args
// (`*.go` globs), diffs and notices stay raw: a harness prompt, a path
// or a diagnostic is literal text, and reformatting it would invent
// structure its author never wrote.
func isMdRole(r role) bool {
	return r == roleAnswer || r == roleUser || r == roleThink
}

// renderMdBlock interprets fences/headers/spans, then reflows and
// styles. Span state is fresh per logical line: a wrap-split span
// survives fragments, a stray marker stains one line at most.
func renderMdBlock(b block, raw string, st styles, width int) []string {
	var rows []string
	for _, line := range mdBlockLines(raw) {
		span := &spanState{}
		fragments := reflow(line.text, max(width, 1))
		for _, fragment := range fragments {
			rendered := line.renderFragment(fragment, b.role, st, span)
			rows = append(rows, rendered)
		}
	}
	return rows
}

func (line mdLine) renderFragment(fragment string, r role, st styles, span *spanState) string {
	base := baseStyle(r, st)
	if line.fence {
		if r == roleThink {
			return base.Render(fragment)
		}
		return st.Dim.Render(fragment)
	}

	if line.header {
		base = base.Bold(true)
	}
	code := base.Foreground(lipgloss.Color(themeColor(colorAccent)))
	if r == roleThink {
		code = base
	}
	bold := base.Bold(true)
	if !line.table {
		return renderSpans(fragment, base, bold, code, span)
	}

	cells := strings.Split(fragment, "|")
	for i, cell := range cells {
		// Spans survive wraps within a cell, but cannot bleed into another cell.
		if i > 0 {
			*span = spanState{}
		}
		cells[i] = renderSpans(cell, base, bold, code, span)
	}
	separator := st.Dim.Render("|")
	return strings.Join(cells, separator)
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

// minWrapWidth guards absurdly narrow viewports: below it, lines pass
// through rather than shredding into slivers.
const minWrapWidth = 8

// reflow word-wraps one logical line to width columns. Leading
// whitespace survives on the first fragment; long words hard-cut
// rune-safe; tabs expand to 4. ANSI-free input only: callers wrap raw
// text before styling.
func reflow(line string, width int) []string {
	if width < minWrapWidth {
		return []string{line}
	}
	line = strings.ReplaceAll(line, "\t", "    ")
	return strings.Split(ansi.Wrap(line, width, ""), "\n")
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
