package agent

import (
	"strings"

	"charm.land/lipgloss/v2"
	"github.com/charmbracelet/x/ansi"
	"github.com/keshon/gektor/internal/tui/components/transcript"
)

type transcriptRenderState struct {
	partBlocks  []int
	toolHits    map[int]toolNameHit
	inlineTools []inlineToolHit
	cache       []renderedBlock
	batch       int
	dirty       bool
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

type inlineToolHit struct {
	part, block int
	toolNameHit
}

// refresh renders every block and assembles the parts. The name row opens a
// run of blocks by one participant, and only its first block carries it —
// emitted here because it depends on the previous block, which a per-block
// cache keyed on the block value cannot know.
func (t *transcriptState) refresh(props transcriptProps) {
	t.Viewport.SetScrollbarStyle(props.styles.Dim, props.styles.Dim)
	width := t.Viewport.ContentWidth()
	// Filtering and packing change part counts; anchor to the original block.
	anchor, within := -1, 0
	previousParts := len(t.render.partBlocks)
	if !t.Follow {
		for part, index := range t.render.partBlocks {
			offset := t.PartOffset(part)
			if offset > t.Viewport.YOffset() {
				break
			}
			anchor, within = index, t.Viewport.YOffset()-offset
		}
	}
	opts := renderOpts{
		expand:    !t.compact,
		labels:    props.labels,
		hideTools: props.hideTools || props.toolNamesOnly,
	}
	parts := make([]string, 0, len(t.blocks))
	t.render.partBlocks = nil
	t.render.toolHits = make(map[int]toolNameHit)
	t.render.inlineTools = nil
	gaps := make([]int, 0, len(t.blocks))
	cache := make([]renderedBlock, len(t.blocks))
	prevSpeaker := ""
	previousTool := false

	for i, b := range t.blocks {
		b.speaker = props.speaker
		if b.role == roleUser {
			b.speaker = props.user
		}
		if (props.hideSYS && systemBlock(b.role)) || (props.hideThinking && b.role == roleThink) {
			// A filtered block must not satisfy a later lookup: the zero
			// renderedBlock equals an empty roleAnswer, so a filter that
			// was lifted would reuse it and render nothing.
			cache[i] = renderedBlock{block: b, width: -1}
			continue
		}

		if props.toolNamesOnly && b.role == roleTool {
			first := !previousTool
			partIndex := len(parts) - 1
			if first {
				partIndex = len(parts)
				parts = append(parts, "")
				gaps = append(gaps, 1)
				t.render.partBlocks = append(t.render.partBlocks, i)
			}
			t.appendToolName(&parts[partIndex], partIndex, i, b, props, width)
			previousTool = true
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
	if !t.Follow && previousParts != len(t.render.partBlocks) && anchor >= 0 {
		for part, index := range t.render.partBlocks {
			if index == anchor {
				t.Viewport.SetYOffset(t.PartOffset(part) + within)
				break
			}
		}
	}
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

// Each packed name retains its own hit range and original block for inspection.
func (t *transcriptState) appendToolName(part *string, partIndex, index int, b block, props transcriptProps, width int) {
	name, _, _ := toolSummary(b.text)
	background := lipgloss.Color(themeColor(colorPopupBackground))
	glyphStyles := toolActionStyles(props.styles)
	glyphStyles.Secondary = glyphStyles.Secondary.Background(background)
	glyphStyles.Success = glyphStyles.Success.Background(background)
	glyphStyles.Warning = glyphStyles.Warning.Background(background)
	glyphStyles.Error = glyphStyles.Error.Background(background)
	state := toolState(b)
	glyph := transcript.ActionGlyph(state, glyphStyles)
	plainGlyph := ansi.Strip(glyph)
	text := plainGlyph + name
	fragments := reflow(text, max(width, 1))
	badgeStyle := lipgloss.NewStyle().Background(background)
	if b.callID == props.selectedCall && b.callID != "" {
		badgeStyle = badgeStyle.Bold(true)
	}
	for fragmentIndex, fragment := range fragments {
		rows := strings.Split(*part, "\n")
		row := len(rows) - 1
		column := ansi.StringWidth(rows[row])
		separator := ""
		if column > 0 {
			separator = " "
			if column+1+ansi.StringWidth(fragment) > width {
				separator = "\n"
				row++
				column = 0
			} else {
				column++
			}
		}
		if fragmentIndex == 0 {
			nameFragment := strings.TrimPrefix(fragment, plainGlyph)
			fragment = glyph + badgeStyle.Render(nameFragment)
		} else {
			fragment = badgeStyle.Render(fragment)
		}
		*part += separator + fragment
		if b.callID != "" {
			t.render.inlineTools = append(t.render.inlineTools, inlineToolHit{
				part: partIndex, block: index,
				toolNameHit: toolNameHit{row: row, start: column, end: column + ansi.StringWidth(fragment)},
			})
		}
	}
}
