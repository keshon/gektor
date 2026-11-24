package agent

import (
	"strings"
	"testing"

	"github.com/charmbracelet/x/ansi"
	"github.com/keshon/gektor/pkg/tui/screen"
)

func TestModernPackedToolNamesKeepRowsAndHitTargets(t *testing.T) {
	defer screen.SetModern(false)
	screen.SetModern(true)
	props := transcriptProps{modern: true, styles: defaultStyles()}
	for _, width := range []int{20, 40, 80} {
		for _, length := range []int{width - 4, width, width + 5} {
			state := newTranscript()
			part := ""
			b := toolCardBlock("first", strings.Repeat("x", length)+" {}")
			state.appendToolName(&part, 0, 0, b, props, width)
			second := toolCardBlock("second", "read_file {}")
			state.appendToolName(&part, 0, 1, second, props, width)
			rows := strings.Split(ansi.Strip(part), "\n")
			for _, row := range rows {
				if strings.TrimSpace(row) == "" || ansi.StringWidth(row) > width || !strings.HasPrefix(row, "  ") || strings.HasPrefix(row, "   ") {
					t.Fatal("packed tool introduced empty, overflowing or unaligned row", width, length, row)
				}
			}
			for _, hit := range state.render.inlineTools {
				if hit.row >= len(rows) || hit.start < 2 || hit.end > ansi.StringWidth(rows[hit.row]) || hit.start >= hit.end {
					t.Fatal("packed tool hit target missed its rendered row", width, length, hit)
				}
			}
			if len(state.render.inlineTools) < 2 {
				t.Fatal("packed tool lost its hit target")
			}
		}
	}
}

func TestModernArgumentsPreserveJSONValueTypes(t *testing.T) {
	text := modernToolArguments(`tool {"null_value":null,"empty_string":"","literal":"null","number":1.25,"boolean":false,"array":[null,"x"]}`)
	for _, field := range []string{"null_value\nnull", "empty_string\n\n", "literal\nnull", "number\n1.25", "boolean\nfalse", "array\n[\n  null,"} {
		if !strings.Contains(text, field) {
			t.Fatal("argument value changed during rendering", field, text)
		}
	}
}
