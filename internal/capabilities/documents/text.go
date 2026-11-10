package documents

import (
	"fmt"
	"unicode/utf8"
)

func (in Request) textStart() int {
	if in.TextOffset != nil {
		return *in.TextOffset
	}
	return 0
}

// sliceText takes a contiguous window across an item's ordered textual fields.
// Positions count Unicode code points; budget and used count UTF-8 bytes.
// Empty slots preserve field/cell identity when a chunk crosses field boundaries.
// A nil slice with no error means the remaining budget cannot fit a character.
func sliceText(parts []string, offset, budget int) (out []string, next, used int, err error) {
	total := 0
	for _, part := range parts {
		total += utf8.RuneCountInString(part)
	}
	if offset < 0 || offset > total {
		return nil, 0, 0, fmt.Errorf("text_offset exceeds item text length (0–%d)", total)
	}
	out = make([]string, len(parts))
	skip, position := offset, offset
	for i, part := range parts {
		n := utf8.RuneCountInString(part)
		if skip >= n {
			skip -= n
			continue
		}
		start := 0
		for skip > 0 {
			_, width := utf8.DecodeRuneInString(part[start:])
			start += width
			skip--
		}
		chunk, cut := clip(part[start:], budget-used)
		out[i] = chunk
		used += len(chunk)
		position += utf8.RuneCountInString(chunk)
		if cut {
			break
		}
	}
	if position < total {
		if used == 0 {
			return nil, 0, 0, nil
		}
		next = position
	}
	return out, next, used, nil
}
