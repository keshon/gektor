package markdown

import (
	"strings"
	"testing"

	"charm.land/lipgloss/v2"
	uv "github.com/charmbracelet/ultraviolet"
	"github.com/charmbracelet/x/ansi"
)

func testStyles() Styles {
	return Styles{
		Text:    lipgloss.NewStyle(),
		Muted:   lipgloss.NewStyle().Foreground(lipgloss.Color("#8895a6")),
		Accent:  lipgloss.NewStyle().Foreground(lipgloss.Color("#79c8d0")),
		Code:    lipgloss.NewStyle(),
		Keyword: lipgloss.NewStyle().Bold(true),
		String:  lipgloss.NewStyle().Foreground(lipgloss.Color("#a2c29b")),
		Number:  lipgloss.NewStyle().Foreground(lipgloss.Color("#f3d58a")),
		Comment: lipgloss.NewStyle().Italic(true),
		Added:   lipgloss.NewStyle().Foreground(lipgloss.Color("#a2c29b")),
		Deleted: lipgloss.NewStyle().Foreground(lipgloss.Color("#ea9393")),
	}
}

func TestLiteralAndPartialMarkdown(t *testing.T) {
	for _, source := range []string{
		"inspect *.go", "2 * 3", "unclosed `code", "partial **bold",
		"find **/*.go", "a_b_c", "two * globs *.go", "C:\\src\\main.go",
	} {
		got := ansi.Strip(strings.Join(Render(source, 80, testStyles()), "\n"))
		if got != source {
			t.Errorf("%q rendered as %q", source, got)
		}
	}
}

func TestEntitiesRespectMarkdownEscapes(t *testing.T) {
	for source, want := range map[string]string{
		`\&copy;`:                 "&copy;",
		"x &notit; y":             "x &notit; y",
		"copyright &copy":         "copyright &copy",
		"&copy; &#169; &#xA9;":    "© © ©",
		"&#38;copy;":              "&copy;",
		"![image](hello&amp;bye)": "image (hello&bye)",
		"[link](hello&amp;bye)":   "link (hello&bye)",
	} {
		got := ansi.Strip(strings.Join(Render(source, 80, testStyles()), "\n"))
		if got != want {
			t.Errorf("%q rendered %q; want %q", source, got, want)
		}
	}
}

func TestWrappedCodeRetainsEverySpace(t *testing.T) {
	for _, source := range []string{"a  b    c  ", `value := "a  b    c  "`, "    indented    value"} {
		for _, width := range []int{3, 8, 20} {
			rows := Render("```text\n"+source+"\n```", width, testStyles())
			// The language label may itself wrap at tiny widths.
			labelRows := len(wrap("text", width))
			var joined strings.Builder
			for _, row := range rows[labelRows:] {
				plain := ansi.Strip(row)
				if width >= 4 {
					plain = strings.TrimPrefix(plain, "│ ")
				}
				joined.WriteString(plain)
			}
			if joined.String() != source {
				t.Errorf("width %d changed %q into %q", width, source, joined.String())
			}
		}
	}
}

func TestWideGraphemesRespectNarrowPanes(t *testing.T) {
	for _, source := range []string{"界", "> 界", "- 界", "```\n界\n```", "| A | B |\n| --- | --- |\n| 界 | 文 |"} {
		for _, width := range []int{1, 2, 3, 4, 12, 20} {
			rows := Render(source, width, testStyles())
			assertFits(t, rows, width)
			if width >= 2 && !strings.Contains(ansi.Strip(strings.Join(rows, "")), "界") {
				t.Errorf("wide glyph lost with available space: width %d source %q", width, source)
			}
		}
	}
}

func TestNestedSpansAndLinks(t *testing.T) {
	source := "**strong *nested* text** and [guide](https://example.com/guide). `2 * 3`"
	rows := Render(source, 80, testStyles())
	plain := ansi.Strip(strings.Join(rows, "\n"))
	for _, want := range []string{"strong nested text", "guide (https://example.com/guide)", "2 * 3"} {
		if !strings.Contains(plain, want) {
			t.Fatalf("missing %q in %q", want, plain)
		}
	}
	if !strings.Contains(strings.Join(rows, ""), testStyles().Text.Bold(true).Italic(true).Foreground(testStyles().Muted.GetForeground()).Render("nested")) {
		t.Fatal("nested emphasis did not compose")
	}
}

func TestTablesKeepCellsAndFit(t *testing.T) {
	source := "| Name | Amount |\n| --- | ---: |\n| a\\|b | 123456789 |\n| **long words here** | `2 * 3` |"
	for _, width := range []int{1, 12, 24, 40, 80} {
		rows := Render(source, width, testStyles())
		assertFits(t, rows, width)
		plain := ansi.Strip(strings.Join(rows, "\n"))
		if width >= 12 && !strings.Contains(plain, "a|b") {
			t.Errorf("escaped pipe split at width %d: %s", width, plain)
		}
		for _, want := range []string{"123", "789"} {
			if width >= 12 && !strings.Contains(plain, want) {
				t.Fatalf("cell content disappeared at %d: %s", width, plain)
			}
		}
	}
}

func TestCodeHighlightingPreservesSource(t *testing.T) {
	r := renderer{styles: testStyles()}
	for _, sample := range []struct{ language, source string }{
		{"go", "func main() {\n    println(\"hello\")\n}"},
		{"json", "{\"count\": 42}"},
		{"unknown", "literal ** *.go ` <tag>"},
		{"diff", "--- a/main.go\n+++ b/main.go\n@@ -1 +1 @@\n-old\n+new"},
	} {
		got := r.highlight(sample.source, sample.language)
		if ansi.Strip(got) != sample.source {
			t.Errorf("%s altered code: %q", sample.language, got)
		}
		if sample.language != "unknown" && got == sample.source {
			t.Errorf("%s not highlighted", sample.language)
		}
	}
}

func TestFencesListsAndQuotesFit(t *testing.T) {
	source := "> nested **quote**\n\n- first\n  - nested item\n\n```go\nfunc hello() {\n\tprintln(\"long code value\")\n}\n```"
	for _, width := range []int{1, 3, 12, 40, 80} {
		assertFits(t, Render(source, width, testStyles()), width)
	}
}

func TestStylesAreIndependent(t *testing.T) {
	a, b := testStyles(), testStyles()
	b.Accent = b.Accent.Foreground(lipgloss.Color("#0000ff"))
	first := strings.Join(Render("`code`", 40, a), "\n")
	second := strings.Join(Render("`code`", 40, b), "\n")
	if first == second || first != strings.Join(Render("`code`", 40, a), "\n") {
		t.Fatal("styles leaked across renders")
	}
}

func TestSourceCannotEmitTerminalCommands(t *testing.T) {
	source := "before\x1b[2Jafter\x1b]52;c;ZXZpbA==\a &#27;[2J [link](https://example.com)"
	got := strings.Join(Render(source, 80, Styles{}), "\n")
	if strings.Contains(got, "\x1b[2J") || strings.Contains(got, "\x1b]52") || strings.Contains(got, "\a") || !strings.Contains(got, "beforeafter") {
		t.Fatalf("unsafe rendering: %q", got)
	}
}

func assertFits(t *testing.T, rows []string, width int) {
	t.Helper()
	for _, row := range rows {
		if ansi.StringWidth(row) > width {
			t.Errorf("%d-column row exceeds %d: %q", ansi.StringWidth(row), width, row)
		}
	}
}

func TestWrappedRowsRetainStylesWhenRenderedSeparately(t *testing.T) {
	styles := testStyles()
	styles.Text = styles.Muted.Italic(true)
	source := "A paragraph that wraps across several visual rows. **" + strings.Repeat("B", 70) + "** After bold text."
	for _, width := range []int{12, 40} {
		rows := Render(source, width, styles)
		if len(rows) < 3 {
			t.Fatal("fixture did not wrap")
		}
		for y, row := range rows {
			buffer := uv.NewScreenBuffer(width, 1)
			uv.NewStyledString(row).Draw(buffer, uv.Rect(0, 0, width, 1))
			for x := range width {
				cell := buffer.CellAt(x, 0)
				if cell == nil || strings.TrimSpace(cell.Content) == "" {
					continue
				}
				if cell.Style.Attrs&uv.AttrItalic == 0 || cell.Style.Fg == nil || cell.Style.Fg != styles.Muted.GetForeground() {
					t.Fatalf("width %d row %d lost base style: %+v", width, y, cell)
				}
				if cell.Content == "B" && cell.Style.Attrs&uv.AttrBold == 0 {
					t.Fatalf("row %d lost wrapped bold", y)
				}
			}
		}
	}
}
