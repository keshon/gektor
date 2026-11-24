package diff

import (
	"strings"
	"testing"

	"github.com/charmbracelet/x/ansi"
)

func TestUnifiedDiffTracksNumbersAcrossHunks(t *testing.T) {
	text := "ok: edited file\n--- a/file.go\n+++ b/file.go\n@@ -10,2 +10,3 @@\n context\n-old\n+new\n+extra\n@@ -30 +31 @@\n-last\n+replacement"
	view, ok := Render(text, 60, Styles{})
	if !ok {
		t.Fatal("unified diff not recognized")
	}
	for _, want := range []string{"  10   10   context", "  11      - old", "       11 + new", "       12 + extra", "  30      - last", "       31 + replacement"} {
		if !strings.Contains(ansi.Strip(view), want) {
			t.Fatal("diff line number changed", want, view)
		}
	}
	for _, width := range []int{8, 20, 60} {
		rendered, valid := Render(strings.ReplaceAll(text, "replacement", strings.Repeat("界", 40)), width, Styles{})
		if !valid {
			t.Fatal("wrapped diff not recognized")
		}
		for _, row := range strings.Split(rendered, "\n") {
			if ansi.StringWidth(row) > width {
				t.Fatal("diff overflows", width, row)
			}
		}
	}
	if _, valid := Render("@@ -1 +1 @@\n-untrusted text without file headers", 60, Styles{}); valid {
		t.Fatal("ordinary tool text became a diff")
	}
}
