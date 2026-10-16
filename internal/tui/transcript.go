package tui

import (
	"fmt"
	"strings"
	"time"

	"github.com/keshon/tars/internal/tui/components/transcript"

	"charm.land/lipgloss/v2"
	"github.com/charmbracelet/x/ansi"
	"github.com/keshon/tars/internal/llm"
)

// thinkToggleHint names the thinking expand/collapse key in UI text.
// The binding lives in handleKey; both stay in sync through this.
const thinkToggleHint = "Ctrl+G"

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
	roleMission
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

func answerBlock(s string) block  { return block{role: roleAnswer, text: s, at: time.Now()} }
func userBlock(s string) block    { return block{role: roleUser, text: s, at: time.Now()} }
func thinkBlock(s string) block   { return block{role: roleThink, text: s, at: time.Now()} }
func resultBlock(s string) block  { return block{role: roleResult, text: s, at: time.Now()} }
func gateBlock(s string) block    { return block{role: roleGate, text: s, at: time.Now()} }
func missionBlock(s string) block { return block{role: roleMission, text: s, at: time.Now()} }
func markerBlock(s string) block  { return block{role: roleMarker, text: s, at: time.Now()} }
func errorBlock(s string) block   { return block{role: roleError, text: s, at: time.Now()} }

// findingBlock renders one deterministic-check finding as plain text;
// the rule carries the meaning, the warning style the urgency.
func findingBlock(rule, path string, line int, summary string) block {
	text := "check " + rule
	if path != "" {
		text += fmt.Sprintf(" %s:%d", path, line)
	}
	return block{role: roleWarning, text: text + " " + summary, at: time.Now()}
}

// toolCardBlock opens a tool card: the call header renders immediately,
// the result attaches when its tool_result event lands (matched by ID
// in attachResult). Text carries no arrow prefix: the role gutter
// already marks tool lines.
func toolCardBlock(callID, text string) block {
	// One line, always: models emit pretty-printed multi-line args,
	// and a card header sprawling down the transcript reads stale
	// next to the speaker labels. Whitespace collapsed, never cut.
	return block{role: roleTool, text: strings.Join(strings.Fields(text), " "), at: time.Now(), callID: callID, open: true}
}

// attachResult lands a tool result on its card: the most recent open
// card with a matching non-empty call ID. Unknown IDs (and empty ones)
// fall back to a standalone result block so parallel or leaked results
// can never corrupt the wrong card. Reports whether it attached.
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

// roleLabel is the bracketed tag printed on a block's label row. One
// tag per role keeps the transcript systematic: the reader learns the
// whole vocabulary from a few lines and never has to parse speaker
// prose to tell a refusal from a plan.
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
	case roleMission:
		return "MISSION"
	case roleMarker:
		return "HARNESS"
	case roleError:
		return "ERROR"
	case roleWarning:
		return "WARNING"
	}
	return "OUTPUT"
}

// systemSpeaker names every participant that is not a conversational
// voice: the harness, permission gates, mission notes and failures. They
// share one name because they share one source, and a reader asking
// "who said that" should never have to distinguish four shades of app.
const systemSpeaker = "SYSTEM"

// speakerLabel names the participant a block belongs to, uppercased.
// User and assistant roles carry the configured character/user names;
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

// renderBlock draws one transcript block: a label row carrying the
// role's tag and (when known) its time, then the body at the left edge.
//
// Body text starts in column 0 because the label row already said what
// the block is. A per-line gutter repeating that answer cost two cells
// of every row to say nothing the label did not.
//
// Answer, user and think roles run the markdown-lite path
// (fences/headers/spans); tool args, diffs, gates and markers stay raw
// so a glob `*.go` can never toggle bold and diff prefixes survive
// styling. Lines reflow to width columns: the viewport never wraps, so
// unbroken long lines would vanish past the right edge.
func renderBlock(b block, st styles, opts renderOpts, width int) string {
	// Blocks are passed by value: normalizing here covers every path
	// to the terminal (compact/full, cards, tests) without touching
	// stored history.
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

// harnessPrefix is the marker text the agent writes into nudge content.
// It names the same thing as the role's label row, so it is stripped at
// the display boundary rather than in the agent's prompt: stored
// messages keep the text they were written with, and the transcript
// shows what the label row plus the body together actually say.
const harnessPrefix = "[harness] "

// normalizeHarnessMarker strips the redundant [harness] prefix and drops
// the leading space a step tag carries for inline use inside harness
// prompts. On its own display row the body column is already the indent
// that space stood in for, and keeping both pushed the line one cell
// right of every other row.
//
// It runs here rather than in the renderer because a marker is literal
// text that takes a different path than an answer, and both fixes have
// to hold on every path.
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

// cleanText normalizes line endings at the display boundary. Tool
// results carry the file's own CRLF (a 11672-byte read brought 239
// CRs into the renderer), and model deltas may echo them back. A
// stray \r moves the terminal cursor to column 0 mid-line, so
// anything written after it (alignStamp's padding) overwrites the
// line head, and the alt-screen repaint desyncs into ghost frames.
// Lone \r becomes a newline rather than vanishing: it was a line
// break where it came from.
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
// branch so a reader can see where a call's output stopped without
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
	// An open card has produced nothing yet. One pending row keeps the
	// branch shape identical whether the call is still running or done,
	// so the layout does not jump when the result lands.
	result := b.result
	if b.open {
		result = "…"
	}
	lines := strings.Split(result, "\n")
	if !expand && len(lines) > resultPreviewLines {
		kept := lines[:resultPreviewLines]
		kept = append(kept, fmt.Sprintf("… (%d more lines)", len(lines)-resultPreviewLines))
		lines = kept
	}
	// Fragments are flattened first: a wrapped line contributes several
	// rows, and only the very last one may close the branch.
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
		rail := toolRail
		if i == len(frags)-1 {
			rail = toolClose + " "
		}
		out = append(out, st.dim.Render(rail)+resultLineStyle(orig[i], f, st))
	}
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
	case roleMission:
		return st.dim.Render(frag)
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
func renderLive(props transcriptProps, width int) []string {
	opts := renderOpts{labels: props.labels, raw: true}
	var parts []string
	for _, b := range assistantBlocks(props.live, "") {
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
		speaker: m.speakerName(),
		user:    m.userName(),
		styles:  m.styles,
		live:    m.live,
		hideSYS: m.hideSYS,
		labels:  !m.hideLabels,
	})
}

// minWrapWidth guards absurdly narrow viewports: below it, lines pass
// through rather than shredding into slivers.
const minWrapWidth = 8

// liveMaxRunes bounds the live streamed buffer. revealFrame paces the
// reveal: 16ms is about sixty frames a second, and a repaint costs
// 0.3-1.3ms for a large transcript, so the frame budget is never the
// limiting factor.
const (
	liveMaxRunes = 64 * 1024
	revealFrame  = 16 * time.Millisecond
)

// revealLive moves queued text into the transcript and reports whether
// more is waiting.
//
// The rate is adaptive on purpose. A slow backend queues one or two
// characters between frames and reveals them one at a time, which reads
// as the model typing. A fast one queues thousands, and a fixed rate
// would leave the display trailing the model by seconds, so each frame
// drains a quarter of the backlog and catches up within a few frames.
//
// A reader who has scrolled away is not watching the tail, so the queue
// is emptied whole: animating text nobody can see spends frames on it.
func (m *model) revealLive() bool {
	if m.liveQueue == "" {
		return false
	}
	queued := []rune(m.liveQueue)
	take := len(queued)
	if m.transcript.Follow {
		take = max(1, len(queued)/4)
	}
	m.live += string(queued[:take])
	m.liveQueue = string(queued[take:])
	m.refreshContent()
	return m.liveQueue != ""
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

// allRoles lists every role for exhaustive tests (gutter width, etc.).
var allRoles = []role{
	roleAnswer, roleUser, roleThink, roleTool, roleResult,
	roleGate, roleMission, roleMarker, roleError, roleWarning,
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
	name := m.identityName
	if m.audio() != nil && m.audio().Settings.CharacterName != "" {
		name = m.audio().Settings.CharacterName
	}
	return nonEmptySpeaker(strings.Join(strings.Fields(cleanText(name)), " "))
}

func (m *model) userName() string {
	name := m.identityUser
	if m.audio() != nil && m.audio().Profile.Conversation != nil {
		name = m.audio().Profile.Conversation.UserName
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

// The TARS renderer receives presentation values, never the application model.
type transcriptProps struct {
	speaker, user, live string
	styles              styles
	// hideSYS drops harness notices from the rendered transcript, and
	// labels drops the [TAG] rows. Both are display-only: the blocks stay
	// in m.transcript.blocks and come back unchanged when a filter lifts.
	hideSYS, labels bool
}

// refresh renders every block and assembles the parts. The name row
// opens a run of blocks by one participant and only its first block
// carries it. It is emitted here rather than inside renderBlock because
// it depends on the previous block, not on this one: a per-block render
// cache keyed on the block value cannot know who spoke before it.
func (t *transcriptState) refresh(props transcriptProps) {
	width := t.Viewport.Width()
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
	if props.live != "" {
		if live := renderLive(props, width); len(live) > 0 {
			tail = strings.Join(live, "\n")
		}
	}
	t.SetParts(parts, tail)
}
