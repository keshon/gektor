package popup

import (
	"github.com/charmbracelet/x/ansi"
	"strings"
	"testing"
)

func TestModalDividersJoinFrame(t *testing.T) {
	rows := ModalBox([]string{"title", strings.Repeat("─", 20), "footer"}, 40, Styles{})
	row := ansi.Strip(rows[2])
	if !strings.HasPrefix(row, "├─") || !strings.HasSuffix(row, "─┤") || strings.Contains(row, " ") {
		t.Fatal(row)
	}
}
