package agent

import (
	"strings"

	"charm.land/lipgloss/v2"
	"github.com/charmbracelet/x/ansi"
)

type transcriptRenderState struct {
	partBlocks []int
	toolHits   map[int]toolNameHit
	cache      []renderedBlock
	batch      int
	dirty      bool
}

// Cached text belongs to the update loop; View only reads the viewport.
type renderedBlock struct {
	block     block
	width     int
	expanded  bool
	labels    bool
	hideTools bool
	text      string
}

// toolNameHit is relative to a rendered transcript part.
type toolNameHit struct{ row, start, end int }

// refresh renders every block and assembles the parts. The name row opens a
// run of blocks by one participant, and only its first block carries it —
// emitted here because it depends on the previous block, which a per-block
// cache keyed on the block value cannot know.
func (t *transcriptState) refresh(props transcriptProps) {
	t.Viewport.SetScrollbarStyle(props.styles.Dim, props.styles.Dim)
	width := t.Viewport.ContentWidth()
	opts := renderOpts{
		expand:    !t.compact,
		labels:    props.labels,
		hideTools: props.hideTools,
	}
	parts := make([]string, 0, len(t.blocks))
	t.render.partBlocks = nil
	t.render.toolHits = make(map[int]toolNameHit)
	gaps := make([]int, 0, len(t.blocks))
	cache := make([]renderedBlock, len(t.blocks))
	prevSpeaker := ""
	previousTool := false

	for i, b := range t.blocks {
		b.speaker = props.speaker
		if b.role == roleUser {
			b.speaker = props.user
		}
		if props.hideSYS && systemBlock(b.role) {
			previousTool = false
			// A filtered block must not satisfy a later lookup: the zero
			// renderedBlock equals an empty roleAnswer, so a filter that
			// was lifted would reuse it and render nothing.
			cache[i] = renderedBlock{block: b, width: -1}
			continue
		}

		rendered := t.renderBlock(i, b, props.styles, opts, width)
		cache[i] = rendered
		part := rendered.text
		if systemBlock(b.role) {
			part = railText(part, props.styles, width)
		} else if b.role == roleUser && props.hideNames {
			part = lipgloss.NewStyle().Background(lipgloss.Color(themeColor(colorPopupBackground))).Render(part)
		}
		if name := speakerLabel(b, props); name != prevSpeaker {
			if !props.hideNames {
				part = speakerRow(name, b.role, props.styles, width) + "\n" + part
			}
			prevSpeaker = name
		}
		if b.role == roleTool && b.callID != "" && b.callID == props.selectedCall {
			part = lipgloss.NewStyle().Background(lipgloss.Color(themeColor(colorPopupBackground))).Bold(true).Render(part)
		}
		if b.role == roleTool && b.callID != "" {
			t.render.recordToolHit(len(parts), b, part, opts)
		}

		gap := 1
		if props.hideTools && b.role == roleTool && previousTool {
			gap = 0
		}
		gaps = append(gaps, gap)
		previousTool = b.role == roleTool
		parts = append(parts, part)
		t.render.partBlocks = append(t.render.partBlocks, i)
	}
	t.render.cache = cache

	tail := ""
	if props.live != "" || props.liveThink != "" {
		if live := renderLive(props, width, opts); len(live) > 0 {
			tail = strings.Join(live, "\n")
		}
	}
	if props.prefill != nil && !props.hideSYS {
		progressWidth := max(width-2, 1)
		progress := historyProgressText(*props.prefill, props.styles, progressWidth, props.progressFrame)
		progress = railText(progress, props.styles, width)
		if !props.hideNames {
			progress = speakerRow(systemSpeaker, roleStatus, props.styles, width) + "\n" + progress
		}
		if tail != "" {
			tail += "\n\n"
		}
		tail += progress
	}
	t.SetPartsWithGaps(parts, tail, gaps)
	if t.Viewport.ContentWidth() != width {
		// Reflow once if this content added or removed the scrollbar column.
		t.refresh(props)
	}
}

func (t *transcriptState) renderBlock(index int, b block, st styles, opts renderOpts, width int) renderedBlock {
	if index < len(t.render.cache) {
		cached := t.render.cache[index]
		if cached.matches(b, opts, width) {
			return cached
		}
	}

	renderWidth := width
	if systemBlock(b.role) {
		renderWidth = max(width-2, 1)
	}
	text := renderBlock(b, st, opts, renderWidth)
	return renderedBlock{
		block:     b,
		width:     width,
		expanded:  opts.expand,
		labels:    opts.labels,
		hideTools: opts.hideTools,
		text:      text,
	}
}

func (b renderedBlock) matches(value block, opts renderOpts, width int) bool {
	return b.block == value && b.width == width && b.expanded == opts.expand &&
		b.labels == opts.labels && b.hideTools == opts.hideTools
}

func (r *transcriptRenderState) recordToolHit(partIndex int, b block, part string, opts renderOpts) {
	name, action, _ := toolSummary(b.text)
	if !opts.hideTools {
		action = name
	}

	// Only the actual card's status row is clickable, excluding speaker, label,
	// and wrapped output rows. Coordinates are terminal cells, not UTF-8 bytes.
	rows := strings.Split(ansi.Strip(part), "\n")
	for row, line := range rows {
		status := strings.HasPrefix(line, "· ") || strings.HasPrefix(line, "– ") || strings.HasPrefix(line, "✓ ") || strings.HasPrefix(line, "… ") || strings.HasPrefix(line, "✕ ") || strings.HasPrefix(line, "× ")
		if !status {
			continue
		}
		actionWidth := ansi.StringWidth(action)
		available := max(ansi.StringWidth(line)-2, 0)
		width := min(actionWidth, available)
		r.toolHits[partIndex] = toolNameHit{row: row, start: 2, end: 2 + width}
		return
	}
}
