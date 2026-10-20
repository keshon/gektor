package tui

import (
	"fmt"
	"strings"
	"time"

	"github.com/keshon/gektor/internal/tui/components/transcript"

	tea "charm.land/bubbletea/v2"
	"charm.land/lipgloss/v2"
	"github.com/charmbracelet/x/ansi"
	"github.com/keshon/gektor/internal/llm"
)

// role names the kind of a transcript block. Every role maps to one
// label tag and one speaker, so each has a stable display identity.
type role int

const (
	roleAnswer role = iota
	roleUser
	roleThink
	roleTool
	roleResult
	roleGate
	roleMarker
	roleError
	// roleWarning is a deterministic-check finding (gofmt, secrets):
	// report-only signal, never an error, so it takes the warning's
	// unbold yellow rather than the error's bold red.
	roleWarning
)

// block is one transcript entry: raw content plus its role. Styling
// happens at render time, so toggles (think expand) re-render without
// touching history.
type block struct {
	speaker string
	role    role
	text    string
	// at stamps when the block entered the transcript. A zero time means
	// "unstamped": reopened history records no display times, so inventing
	// one would claim a moment the transcript never observed.
	at time.Time
	// Tool card fields (roleTool only): callID pairs the result event,
	// result/output text lands on attach, open until it does, failed
	// when the result reports an error.
	callID string
	result string
	open   bool
	failed bool
}

// assistantBlocks normalizes both backend reasoning formats for display only.
// Stored messages remain unchanged, including incomplete think regions.
func assistantBlocks(content, reasoning string) []block {
	var answer strings.Builder
	thoughts := []string{}
	if strings.TrimSpace(reasoning) != "" {
		thoughts = append(thoughts, reasoning)
	}
	prev := 0
	regions := llm.ThinkRegions(content)
	for _, region := range regions {
		answer.WriteString(content[prev:region[0]])
		thought := strings.TrimSpace(strings.TrimSuffix(strings.TrimPrefix(content[region[0]:region[1]], "<think>"), "</think>"))
		if thought != "" {
			thoughts = append(thoughts, thought)
		}
		prev = region[1]
	}
	answer.WriteString(content[prev:])
	var blocks []block
	if len(thoughts) > 0 {
		blocks = append(blocks, thinkBlock(strings.Join(thoughts, "\n\n")))
	}
	text := answer.String()
	if len(regions) > 0 {
		text = strings.TrimSpace(text)
	}
	if strings.TrimSpace(text) != "" {
		blocks = append(blocks, answerBlock(text))
	}
	return blocks
}

func answerBlock(s string) block { return block{role: roleAnswer, text: s, at: time.Now()} }
func userBlock(s string) block   { return block{role: roleUser, text: s, at: time.Now()} }
func thinkBlock(s string) block  { return block{role: roleThink, text: s, at: time.Now()} }
func resultBlock(s string) block { return block{role: roleResult, text: s, at: time.Now()} }
func gateBlock(s string) block   { return block{role: roleGate, text: s, at: time.Now()} }
func markerBlock(s string) block { return block{role: roleMarker, text: s, at: time.Now()} }
func errorBlock(s string) block  { return block{role: roleError, text: s, at: time.Now()} }

// findingBlock renders one deterministic-check finding as plain text;
// the rule carries the meaning, the warning style the urgency.
func findingBlock(rule, path string, line int, summary string) block {
	text := "check " + rule
	if path != "" {
		text += fmt.Sprintf(" %s:%d", path, line)
	}
	return block{role: roleWarning, text: text + " " + summary, at: time.Now()}
}

// toolCardBlock opens a tool card: the header renders immediately, the
// result attaches by ID when its event lands. No arrow prefix — the role
// gutter already marks tool lines. One line always: models emit
// pretty-printed multi-line args, and a card header sprawling down the
// transcript reads stale next to speaker labels. Whitespace collapsed,
// never cut.
func toolCardBlock(callID, text string) block {
	return block{role: roleTool, text: strings.Join(strings.Fields(text), " "), at: time.Now(), callID: callID, open: true}
}

// attachResult lands a tool result on the most recent open card with a
// matching non-empty call ID. Unknown or empty IDs become a standalone
// block, so a parallel or leaked result can never corrupt the wrong card.
func (m *model) attachResult(callID, text string, failed ...bool) bool {
	if callID == "" {
		return false
	}
	for i := len(m.transcript.blocks) - 1; i >= 0; i-- {
		b := &m.transcript.blocks[i]
		if b.role == roleTool && b.open && b.callID == callID {
			b.result = text
			b.open = false
			b.failed = strings.HasPrefix(text, "error:") || strings.Contains(text, "\nerror:")
			if len(failed) > 0 {
				b.failed = failed[0]
			}
			m.refreshContent()
			return true
		}
	}
	return false
}

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
		return st.user
	case roleAnswer, roleThink, roleTool, roleResult:
		return st.gate
	default:
		return st.dim
	}
}

// speakerRow draws the name row that opens a run of blocks by one
// participant. Only the first block of a run carries it; the rest of
// the run is identified by its label rows alone.
func speakerRow(name string, r role, st styles, width int) string {
	return ansi.Truncate(speakerStyle(r, st).Render(name), max(width, 1), "…")
}

// thinkPreviewLen caps the collapsed thinking summary (runes, not bytes).
const thinkPreviewLen = 80

// thinkSummary shrinks reasoning to one line plus its size: the reader
// sees that thinking happened and how much, never the raw
// chain-of-thought as answer text.
func thinkSummary(s string) string {
	line, _, _ := strings.Cut(s, "\n")
	if runes := []rune(line); len(runes) > thinkPreviewLen {
		line = string(runes[:thinkPreviewLen]) + "…"
	}
	return fmt.Sprintf("%s [%d chars]", line, len([]rune(s)))
}

// renderOpts are the view choices that change how one block renders.
// They belong to the transcript rather than the block: the same block
// reads differently depending on what the reader has chosen to hide,
// and nothing about the stored message changed.
type renderOpts struct {
	// expand opens verbose content: full thinking, full tool output. It
	// is the inverse of the transcript's compact flag.
	expand bool
	// labels false drops the [TAG] row. The name row stays: it is what
	// says who spoke, and dropping it would leave unattributed prose.
	labels bool
	// raw draws the body as literal text instead of markdown. Only the
	// live stream uses it: a span revealed one character at a time would
	// show its own delimiters for several frames before closing.
	raw bool
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
	b.result = strings.TrimRight(cleanText(b.result), "\n")
	b = normalizeHarnessMarker(b)
	raw := b.text
	if b.role == roleThink && !opts.expand {
		// Collapsed keeps the summary-only rule (size here, never in
		// full view); the label row carries the tag either way, so the
		// body never repeats it.
		raw = thinkSummary(b.text)
	}
	var body []string
	switch {
	case b.role == roleTool:
		body = renderToolCard(b, st, opts.expand, width)
	case isMdRole(b.role) && !opts.raw:
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
	line := st.dim.Render("[" + label + "]")
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

// harnessPrefix is the marker the agent writes into nudge content. It
// names the same thing as the role's label row, so it is stripped at the
// display boundary rather than in the prompt: stored messages keep the text
// they were written with, and the transcript shows what the label row plus
// body actually say.
//
// Deliberately a second copy of agent's marker rather than an import: this
// is display vocabulary, and the two only have to agree on the literal.
const harnessPrefix = "[harness] "

// normalizeHarnessMarker strips the redundant [harness] prefix and the
// leading space a step tag carries for inline use. On its own display row
// the body column is already the indent that space stood in for, and
// keeping both pushed the line one cell right of every other row.
//
// It runs here rather than in the renderer because a marker is literal text
// taking a different path than an answer, and both fixes must hold
// everywhere.
func normalizeHarnessMarker(b block) block {
	if b.role != roleMarker || !strings.HasPrefix(b.text, harnessPrefix) {
		return b
	}
	lines := strings.Split(strings.TrimPrefix(b.text, harnessPrefix), "\n")
	for i, line := range lines {
		if strings.HasPrefix(strings.TrimLeft(line, " \t"), "(step ") {
			lines[i] = strings.TrimLeft(line, " \t")
		}
	}
	b.text = strings.Join(lines, "\n")
	return b
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

// cleanText normalizes line endings at the display boundary. Tool results
// carry the file's own CRLF and model deltas may echo them back. A stray
// \r moves the cursor to column 0 mid-line, so anything written after it
// (alignStamp's padding) overwrites the line head and the alt-screen
// repaint desyncs into ghost frames. A lone \r becomes a newline: it was a
// line break where it came from.
func cleanText(s string) string {
	s = strings.ReplaceAll(s, "\r\n", "\n")
	return strings.ReplaceAll(s, "\r", "\n")
}

// resultPreviewLines caps collapsed tool results (pi parity:
// FALLBACK_PREVIEW_LINES=10). The overflow note keeps the count, so
// compacted output never silently loses lines.
const resultPreviewLines = 10

// Tool output hangs off the call as a tree branch: every row keeps the
// rail so the card's edge never breaks, and the final row closes the
// branch on its own row so no text shares the closing corner, without
// counting rows.
const (
	toolRail  = "│ "
	toolClose = "└─"
)

// toolRailWidth is what the rail costs on every output row.
var toolRailWidth = lipgloss.Width(toolRail)

// renderToolCard draws the call and its output. The call is one line
// with collapsed arguments: models emit pretty-printed JSON, and a
// header sprawling down the transcript reads stale next to the label
// rows. Compact view previews long results; full view shows all.
func renderToolCard(b block, st styles, expand bool, width int) []string {
	glyph := st.add.Render("✓")
	if b.open {
		glyph = st.dim.Render("…")
	} else if b.failed {
		glyph = st.err.Render("✕")
	}
	call := ansi.Truncate(b.text, max(width-2, 1), "…")
	out := []string{glyph + " " + st.dim.Render(call)}
	// Before the first chunk, show a pending row. Once output arrives the
	// same card renders it while retaining its running glyph.
	result := b.result
	if b.open && result == "" {
		result = "…"
	}
	lines := strings.Split(result, "\n")
	if !expand && len(lines) > resultPreviewLines {
		hidden := len(lines) - resultPreviewLines
		if b.open {
			lines = append([]string{fmt.Sprintf("… (%d earlier lines)", hidden)}, lines[hidden:]...)
		} else {
			lines = append(lines[:resultPreviewLines], fmt.Sprintf("… (%d more lines)", hidden))
		}
	}
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
		out = append(out, st.dim.Render(toolRail)+resultLineStyle(orig[i], f, st))
	}
	out = append(out, st.dim.Render(toolClose))
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
	base := baseStyle(b.role, st)
	var out []string
	for _, ml := range mdBlockLines(raw) {
		span := &spanState{}
		for _, f := range reflow(ml.text, max(width, 1)) {
			var body string
			if ml.fence {
				body = st.dim.Render(f)
			} else {
				bb := base
				if ml.header {
					bb = base.Bold(true)
				}
				body = renderSpans(f, bb, bb.Bold(true), bb.Foreground(lipgloss.Color(themeColor(colorAccent))), span)
			}
			out = append(out, body)
		}
	}
	return out
}

// stampWidth is the " [15:04]" suffix reserved on the label row.
const stampWidth = 8

// alignStamp appends the timestamp right-aligned to width: content
// left, time as a clean right column. Padding sits between content and
// stamp, so no line ever ends in whitespace.
func alignStamp(line string, st styles, at time.Time, width int) string {
	stamp := st.dim.Render(" [" + at.Format("15:04") + "]")
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
		return st.dim.Render(frag)
	case roleResult:
		return resultLineStyle(origLine, frag, st)
	case roleGate:
		return st.gate.Render(frag)
	case roleMarker:
		return st.dim.Render(frag)
	case roleError:
		return st.err.Render(frag)
	case roleWarning:
		return st.warn.Render(frag)
	default:
		return frag
	}
}

// resultLineStyle colors one fragment by its logical line's diff prefix.
func resultLineStyle(origLine, frag string, st styles) string {
	switch {
	case strings.HasPrefix(origLine, "+") && !strings.HasPrefix(origLine, "++"):
		return st.add.Render(frag)
	case strings.HasPrefix(origLine, "-") && !strings.HasPrefix(origLine, "--"):
		return st.del.Render(frag)
	case strings.HasPrefix(origLine, "@@"):
		return st.hunk.Render(frag)
	default:
		return st.dim.Render(frag)
	}
}

// appendBlock records a block and re-renders the transcript.
func (m *model) appendBlock(b block) {
	m.transcript.blocks = append(m.transcript.blocks, b)
	m.refreshContent()
}

// renderLive uses the same label rows and Markdown blocks as completed
// replies. The live buffer is one assistant turn, so the name row opens
// it exactly once.
func renderLive(props transcriptProps, width int, opts renderOpts) []string {
	opts.raw = true
	var parts []string
	// Streamed deliberation renders first and stays put, so the turn reads
	// as THINK then RESPONSE. Wrapping in <think> lets assistantBlocks
	// build the same block a completed turn uses.
	if strings.TrimSpace(props.liveThink) != "" {
		for _, b := range assistantBlocks("<think>"+props.liveThink+"</think>", "") {
			b.at = time.Time{}
			b.speaker = props.speaker
			part := renderBlock(b, props.styles, opts, width)
			if len(parts) == 0 {
				part = speakerRow(speakerLabel(b, props), b.role, props.styles, width) + "\n" + part
			}
			parts = append(parts, part)
		}
	}
	for _, b := range assistantBlocks(liveContent(props.live), "") {
		b.at = time.Time{}
		b.speaker = props.speaker
		part := renderBlock(b, props.styles, opts, width)
		if len(parts) == 0 {
			part = speakerRow(speakerLabel(b, props), b.role, props.styles, width) + "\n" + part
		}
		parts = append(parts, part)
	}
	if len(parts) == 0 {
		return nil
	}
	return strings.Split(strings.Join(parts, "\n\n"), "\n")
}

// A tag split across SSE frames must not flash as response/thinking text.
// Hold only its unfinished suffix; the stored message is never changed.
func liveContent(text string) string {
	for _, tag := range []string{"<think>", "</think>"} {
		for n := min(len(text), len(tag)-1); n > 0; n-- {
			if strings.HasSuffix(text, tag[:n]) {
				return text[:len(text)-n]
			}
		}
	}
	return text
}

// refreshContent re-renders every block into the viewport. Called on
// append, on toggles, and on resize (reflow follows the width). YOffset
// survives SetContent, so a reading user is never yanked (pinned by
// TestNewBlocksRespectUnfollow).
func (m *model) refreshContent() {
	if !m.ready {
		return
	}
	if m.transcript.render.batch > 0 {
		m.transcript.render.dirty = true
		return
	}
	m.transcript.refresh(transcriptProps{
		speaker:   m.speakerName(),
		user:      m.userName(),
		styles:    m.styles,
		live:      m.live,
		liveThink: m.liveThink,
		hideSYS:   m.hideSYS,
		labels:    !m.hideLabels,
	})
}

// minWrapWidth guards absurdly narrow viewports: below it, lines pass
// through rather than shredding into slivers.
const minWrapWidth = 8

// liveMaxRunes bounds streamed answer text. revealFrame coalesces fragments for
// one frame without slowing generation to an artificial character rate.
const (
	liveMaxRunes = 64 * 1024
	revealFrame  = 16 * time.Millisecond
)

// resetLive clears both live buffers at a turn boundary. One method rather
// than the field list repeated at each site: forgetting a field strands the
// previous turn's text inside the next reply.
func (m *model) resetLive() {
	m.live, m.liveQueue, m.liveCut, m.liveRunes = "", "", false, 0
	m.liveThink, m.liveThinkQueue = "", ""
}

// Coalesce rapid network fragments for one frame, then paint all received text.
// This timer is independent of the idle/spinner heartbeat.
func (m *model) scheduleLiveFlush() {
	if m.liveFlushPending {
		return
	}
	m.liveFlushPending = true
	m.pendingCommands = append(m.pendingCommands, tea.Tick(revealFrame, func(time.Time) tea.Msg { return liveFlushMsg{} }))
}

// revealLive paints complete received chunks without a simulated typing rate.
func (m *model) revealLive() bool {
	pending := false
	if m.liveThinkQueue != "" {
		think := []rune(m.liveThinkQueue)
		take := len(think)
		m.liveThink += string(think[:take])
		m.liveThinkQueue = string(think[take:])
		pending = true
	}
	if m.liveQueue == "" {
		if pending {
			m.refreshContent()
		}
		return pending
	}
	queued := []rune(m.liveQueue)
	take := len(queued)
	m.live += string(queued[:take])
	m.liveQueue = string(queued[take:])
	m.refreshContent()
	return false
}

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

type transcriptRenderState struct {
	cache []renderedBlock
	batch int
	dirty bool
}

// Cached text belongs to the update loop; View only reads the viewport.
type renderedBlock struct {
	block    block
	width    int
	expanded bool
	labels   bool
	text     string
}

func (m *model) beginTranscriptUpdate() { m.transcript.render.batch++ }
func (m *model) endTranscriptUpdate() {
	m.transcript.render.batch--
	if m.transcript.render.batch == 0 && m.transcript.render.dirty {
		m.transcript.render.dirty = false
		m.refreshContent()
	}
}

func nonEmptySpeaker(name string) string {
	if name == "" {
		return "Assistant"
	}
	return name
}
func (m *model) speakerName() string {
	return nonEmptySpeaker(strings.Join(strings.Fields(cleanText(m.identityName)), " "))
}

func (m *model) userName() string {
	name := m.identityUser
	if m.config != nil && m.config.Profile.Identity != nil {
		name = m.config.Profile.Identity.UserName
	}
	name = strings.Join(strings.Fields(cleanText(name)), " ")
	if name == "" {
		return "You"
	}
	return name
}

type transcriptState struct {
	transcript.Model
	blocks  []block
	compact bool
	render  transcriptRenderState
}

func newTranscript() transcriptState { return transcriptState{Model: transcript.New(), compact: true} }

// The GEKTOR renderer receives presentation values, never the application model.
type transcriptProps struct {
	speaker, user, live string
	// liveThink is streamed deliberation, rendered above live so the turn
	// reads THINK then RESPONSE instead of one buffer that blanks the
	// answer to make room.
	liveThink string
	styles    styles
	// hideSYS drops harness notices from the rendered transcript, and
	// labels drops the [TAG] rows. Both are display-only: the blocks stay
	// in m.transcript.blocks and come back unchanged when a filter lifts.
	hideSYS, labels bool
}

// refresh renders every block and assembles the parts. The name row opens a
// run of blocks by one participant, and only its first block carries it —
// emitted here because it depends on the previous block, which a per-block
// cache keyed on the block value cannot know.
func (t *transcriptState) refresh(props transcriptProps) {
	width := t.Viewport.ContentWidth()
	opts := renderOpts{expand: !t.compact, labels: props.labels}
	parts := make([]string, 0, len(t.blocks))
	cache := make([]renderedBlock, len(t.blocks))
	prevSpeaker := ""
	for i, b := range t.blocks {
		b.speaker = props.speaker
		if b.role == roleUser {
			b.speaker = props.user
		}
		if props.hideSYS && b.role == roleMarker {
			// A filtered block must not satisfy a later lookup: the zero
			// renderedBlock equals an empty roleAnswer, so a filter that
			// was lifted would reuse it and render nothing.
			cache[i] = renderedBlock{block: b, width: -1}
			continue
		}
		var rendered renderedBlock
		if i < len(t.render.cache) && t.render.cache[i].block == b && t.render.cache[i].width == width && t.render.cache[i].expanded == opts.expand && t.render.cache[i].labels == opts.labels {
			rendered = t.render.cache[i]
		} else {
			rendered = renderedBlock{block: b, width: width, expanded: opts.expand, labels: opts.labels, text: renderBlock(b, props.styles, opts, width)}
		}
		cache[i] = rendered
		part := rendered.text
		if name := speakerLabel(b, props); name != prevSpeaker {
			part = speakerRow(name, b.role, props.styles, width) + "\n" + part
			prevSpeaker = name
		}
		parts = append(parts, part)
	}
	t.render.cache = cache

	tail := ""
	if props.live != "" || props.liveThink != "" {
		if live := renderLive(props, width, opts); len(live) > 0 {
			tail = strings.Join(live, "\n")
		}
	}
	t.SetParts(parts, tail)
}
