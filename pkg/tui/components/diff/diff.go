// Package diff renders unified changes without reading files or applying edits.
package diff

import (
	"fmt"
	"regexp"
	"strconv"
	"strings"

	"charm.land/lipgloss/v2"
	"github.com/keshon/gektor/pkg/tui/components/cell"
)

type Styles struct{ Text, Muted, Added, Removed, Header lipgloss.Style }

var hunkHeader = regexp.MustCompile(`^@@ -(\d+)(?:,(\d+))? \+(\d+)(?:,(\d+))? @@`)

// Render preserves the tool's text and adds old/new line numbers inside hunks.
// False means the input did not contain a complete unified-diff header.
func Render(text string, width int, styles Styles) (string, bool) {
	lines := strings.Split(strings.ReplaceAll(text, "\r\n", "\n"), "\n")
	oldHeader, newHeader, hunk := false, false, false
	for _, line := range lines {
		if strings.HasPrefix(line, "--- ") {
			oldHeader = true
		}
		if oldHeader && strings.HasPrefix(line, "+++ ") {
			newHeader = true
		}
		if newHeader && hunkHeader.MatchString(line) {
			hunk = true
			break
		}
	}
	if !hunk {
		return "", false
	}
	width = max(width, 1)
	rows := []string{}
	oldLine, newLine, oldLeft, newLeft := 0, 0, 0, 0
	for _, line := range lines {
		if match := hunkHeader.FindStringSubmatch(line); match != nil {
			var err error
			oldLine, err = strconv.Atoi(match[1])
			if err != nil {
				return "", false
			}
			newLine, err = strconv.Atoi(match[3])
			if err != nil {
				return "", false
			}
			oldLeft, newLeft = 1, 1
			if match[2] != "" {
				oldLeft, err = strconv.Atoi(match[2])
				if err != nil {
					return "", false
				}
			}
			if match[4] != "" {
				newLeft, err = strconv.Atoi(match[4])
				if err != nil {
					return "", false
				}
			}
			rows = append(rows, styles.Header.Render(cell.Line(line, width)))
			continue
		}
		if oldLeft+newLeft <= 0 || len(line) == 0 || !strings.ContainsRune(" +-", rune(line[0])) {
			for _, fragment := range cell.Wrap(line, width) {
				rows = append(rows, styles.Muted.Render(cell.Line(fragment, width)))
			}
			continue
		}
		oldNumber, newNumber := "", ""
		style := styles.Text
		switch line[0] {
		case '-':
			oldNumber = strconv.Itoa(oldLine)
			oldLine++
			oldLeft--
			style = styles.Removed
		case '+':
			newNumber = strconv.Itoa(newLine)
			newLine++
			newLeft--
			style = styles.Added
		case ' ':
			oldNumber = strconv.Itoa(oldLine)
			newNumber = strconv.Itoa(newLine)
			oldLine++
			newLine++
			oldLeft--
			newLeft--
		}
		prefix := fmt.Sprintf("%4s %4s %c ", oldNumber, newNumber, line[0])
		prefixWidth := cell.Width(prefix)
		if width <= prefixWidth+1 {
			for _, fragment := range cell.Wrap(line, width) {
				rows = append(rows, style.Render(cell.Line(fragment, width)))
			}
			continue
		}
		fragments := cell.Wrap(line[1:], width-prefixWidth)
		for i, fragment := range fragments {
			gutter := prefix
			if i > 0 {
				gutter = strings.Repeat(" ", prefixWidth)
			}
			rows = append(rows, styles.Muted.Render(gutter)+style.Render(cell.Line(fragment, width-prefixWidth)))
		}
	}
	return strings.Join(rows, "\n"), true
}
