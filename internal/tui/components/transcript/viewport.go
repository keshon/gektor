package transcript

import (
	"sort"
	"strings"

	tea "charm.land/bubbletea/v2"

	"github.com/keshon/gektor/internal/tui/components/cell"
)

// Viewport is a scrolling window over a document made of parts.
//
// It replaces a general-purpose viewport because this document is already
// structured: each part is rendered text of a known height, and only a
// screenful is ever visible. A general viewport wants the whole document
// joined into one string and re-splits it on every frame — at 500 blocks
// that meant splitting and measuring two megabytes to draw thirty lines,
// which is the 9ms-per-frame cost the window exists to remove.
//
// starts records each part's first row, including any gap before it, so
// locating a visible row does not require walking the preceding parts.
type Viewport struct {
	width, height int
	offset        int
	parts         []string
	// starts[i] is the document line where part i begins. Blank
	// separators are implicit: starts[i+1] is ends-of-part-i plus one.
	starts []int
	total  int
}

// NewViewport returns an empty window of the given size.
func NewViewport(width, height int) Viewport {
	return Viewport{width: max(width, 0), height: max(height, 0)}
}

func (v Viewport) Width() int  { return v.width }
func (v Viewport) Height() int { return v.height }

func (v *Viewport) SetWidth(w int) {
	v.width = max(w, 0)
	v.clamp()
}

func (v *Viewport) SetHeight(h int) {
	v.height = max(h, 0)
	v.clamp()
}

// Scrollable reports whether the document is taller than the window. The
// scrollbar appears only then: a bar over a document that already fits
// says nothing except that the layout lost a column.
func (v Viewport) Scrollable() bool { return v.total > v.height }

// ContentWidth reserves the scrollbar and a blank cell separating it from text.
func (v Viewport) ContentWidth() int {
	if v.Scrollable() {
		return max(v.width-2, 0)
	}
	return v.width
}

// Set installs a new document. starts must be the line each part begins at,
// and total the document's line count.
func (v *Viewport) Set(parts []string, starts []int, total int) {
	v.parts, v.starts, v.total = parts, starts, total
	v.clamp()
}

// Total is the document height in lines.
func (v Viewport) Total() int { return v.total }

func (v Viewport) YOffset() int { return v.offset }

func (v *Viewport) SetYOffset(n int) {
	v.offset = max(n, 0)
	v.clamp()
}

func (v *Viewport) GotoTop() { v.offset = 0 }

func (v *Viewport) GotoBottom() { v.offset = v.maxOffset() }

// AtBottom reports whether the last line is showing, which is what "follow
// the newest output" is anchored on.
func (v Viewport) AtBottom() bool { return v.offset >= v.maxOffset() }

func (v Viewport) maxOffset() int { return max(v.total-v.height, 0) }

func (v *Viewport) clamp() { v.offset = min(max(v.offset, 0), v.maxOffset()) }

// partAt returns the index of the part containing document line, or
// len(parts) if the line is past the end.
func (v Viewport) partAt(line int) int {
	return sort.Search(len(v.starts), func(i int) bool { return v.starts[i] > line }) - 1
}

func (v *Viewport) lineUp(n int)   { v.SetYOffset(v.offset - n) }
func (v *Viewport) lineDown(n int) { v.SetYOffset(v.offset + n) }

// page moves by almost a screen: a full page leaves no context for where
// the reader came from, and on a short document moves them nowhere at all.
func (v *Viewport) page(dir int) {
	n := max(v.height-1, 1)
	if dir < 0 {
		n = -n
	}
	v.SetYOffset(v.offset + n)
}

// PageUp moves up by almost a screen. It is page(-1) for callers
// outside the package, which drive paging from a scrollbar track click.
func (v *Viewport) PageUp() { v.page(-1) }

// PageDown moves down by almost a screen. It is page(1) for callers
// outside the package, which drive paging from a scrollbar track click.
func (v *Viewport) PageDown() { v.page(1) }

// Update handles scrolling. Only the keys the application routes here and
// the wheel are bound; the input keeps arrows and typing for itself.
func (v *Viewport) Update(msg tea.Msg) tea.Cmd {
	switch msg := msg.(type) {
	case tea.KeyPressMsg:
		switch msg.Code {
		case tea.KeyPgUp:
			v.page(-1)
		case tea.KeyPgDown:
			v.page(1)
		}
	case tea.MouseMsg:
		switch msg.Mouse().Button {
		case tea.MouseWheelUp:
			v.lineUp(3)
		case tea.MouseWheelDown:
			v.lineDown(3)
		}
	}
	return nil
}

// View draws the window: the visible rows only, a scrollbar in the last
// column when the document overflows, and every row padded to the full
// width so the caller's framing has nothing left to do.
func (v Viewport) View() string {
	width, height := v.width, v.height
	if width <= 0 || height <= 0 {
		return ""
	}
	rows := v.rows(height)
	text := max(v.ContentWidth(), 0)
	for i, row := range rows {
		rows[i] = fit(row, text)
	}
	if !v.Scrollable() {
		return strings.Join(rows, "\n")
	}
	bar := v.scrollbar(height)
	rows = rows[:height]
	for i := range rows {
		rows[i] += strings.Repeat(" ", max(width-text-1, 0)) + bar[i]
	}
	return strings.Join(rows, "\n")
}

// rows materialises the document lines in [offset, offset+height) without
// splitting any part that is not visible. Splitting is the expensive step
// this avoids: a part is a rendered block, and there are hundreds.
func (v Viewport) rows(height int) []string {
	out := make([]string, 0, height)
	end := v.offset + height
	line := v.offset
	for i := max(v.partAt(line), 0); i < len(v.parts) && line < end; i++ {
		start := v.starts[i]
		if start >= end {
			break
		}
		// The blank line separating this part from the last.
		if line < start {
			n := min(start-line, end-line)
			for range n {
				out = append(out, "")
			}
			line += n
			if line >= end {
				break
			}
		}
		lines := strings.Split(v.parts[i], "\n")
		from := max(line-start, 0)
		to := min(len(lines), end-start)
		for j := from; j < to; j++ {
			out = append(out, lines[j])
		}
		line = start + to
	}
	for len(out) < height {
		out = append(out, "")
	}
	return out[:height]
}

// ThumbRange reports where the thumb sits in a window of the given
// height: its top row and its size. It mirrors scrollbar() so hit
// testing and dragging agree with what is drawn.
func (v Viewport) ThumbRange(height int) (top, size int) {
	size = max(height*v.height/max(v.total, 1), 1)
	if size >= height {
		return 0, height
	}
	free := height - size
	pos := 0
	if free > 0 {
		pos = v.offset * free / max(v.maxOffset(), 1)
	}
	return pos, size
}

// DragOffset converts a row inside the window to a document offset,
// so a grabbed thumb follows the pointer instead of jumping to it.
func (v Viewport) DragOffset(row, height, grab int) int {
	_, size := v.ThumbRange(height)
	free := height - size
	if free <= 0 {
		return 0
	}
	row -= grab
	if row < 0 {
		row = 0
	}
	if row > free {
		row = free
	}
	return row * v.maxOffset() / free
}

// scrollbar draws a track with a thumb sized to the visible fraction and
// placed at the reader's position, so it reads as a position in the
// document and not merely as "there is more".
func (v Viewport) scrollbar(height int) []string {
	bar := make([]string, height)
	// A light rule, not a heavy one: this column sits directly against the
	// pane border, and the same glyph in both reads as a doubled rule
	// rather than as a position in the document.
	track := '╎'
	thumb := '█'
	if v.total <= 0 {
		for i := range bar {
			bar[i] = string(track)
		}
		return bar
	}
	// The thumb is at least one row, and never so large that it hides the
	// fact that there is somewhere left to go.
	pos, size := v.ThumbRange(height)
	for i := range bar {
		if i >= pos && i < pos+size {
			bar[i] = string(thumb)
		} else {
			bar[i] = string(track)
		}
	}
	return bar
}

// fit truncates to width and pads to it, measuring in terminal cells so
// styled text is not cut mid-escape.
func fit(s string, width int) string {
	if width <= 0 {
		return ""
	}
	return cell.Line(s, width)
}
