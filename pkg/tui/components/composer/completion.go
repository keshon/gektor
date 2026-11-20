package composer

import (
	"strings"
	"unicode"
)

// Token identifies the quoted or whitespace-delimited token at the caret.
// Positions are rune offsets, matching the editor, rather than byte offsets.
func (m Model) Token() (start, end int, prefix string) {
	runes := []rune(m.Value())
	pos := m.Column()
	for _, line := range strings.Split(m.Value(), "\n")[:m.Line()] {
		pos += len([]rune(line)) + 1
	}
	pos = min(pos, len(runes))
	quoted := false
	for i, r := range runes[:pos] {
		if r == '"' {
			quoted = !quoted
		} else if unicode.IsSpace(r) && !quoted {
			start = i + 1
		}
	}
	prefix = string(runes[start:pos])
	end = pos
	for end < len(runes) {
		if unicode.IsSpace(runes[end]) && !quoted {
			break
		}
		if runes[end] == '"' {
			quoted = !quoted
		}
		end++
	}
	return
}

// Replace replaces a rune range and positions the caret after the inserted text.
// back positions the caret before a closing quote when completing a directory.
func (m *Model) Replace(start, end int, value string, back int) {
	runes := []rune(m.Value())
	start = min(max(start, 0), len(runes))
	end = min(max(end, start), len(runes))
	before := string(runes[:start]) + value
	m.SetValue(before + string(runes[end:]))
	m.MoveToBegin()
	line := strings.Count(before, "\n")
	for m.Line() < line {
		m.CursorDown()
	}
	column := len([]rune(before[strings.LastIndex(before, "\n")+1:]))
	m.SetCursorColumn(max(column-back, 0))
}
