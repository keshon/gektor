package workflow

import (
	"bytes"
	"encoding/json"
	"strings"

	tea "charm.land/bubbletea/v2"

	"github.com/keshon/gektor/internal/tui/components/cell"
	"github.com/keshon/gektor/internal/tui/components/form"
)

type formField = form.Field
type scrollDrag struct {
	active bool
	grab   int
}

func cellFrame(s string, width, height int) string { return cell.Frame(s, width, height) }
func truncate(s string, n int) string              { return cell.Truncate(s, n) }
func cleanText(s string) string {
	return strings.ReplaceAll(strings.ReplaceAll(s, "\r\n", "\n"), "\r", "\n")
}
func nonEmpty(s string) string {
	if s == "" {
		return "—"
	}
	return s
}
func isMouseClick(msg tea.MouseMsg) bool { _, ok := msg.(tea.MouseClickMsg); return ok }
func prettyToolArguments(text string) string {
	name, args, ok := strings.Cut(text, " ")
	if !ok {
		return text
	}
	var raw bytes.Buffer
	if json.Indent(&raw, []byte(args), "", "  ") != nil {
		return text
	}
	return name + "\n" + raw.String()
}
