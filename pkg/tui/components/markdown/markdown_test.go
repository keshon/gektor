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

func TestClosedCodeFencesAreFormattedForDisplay(t *testing.T) {
	for _, sample := range []struct {
		name, source, want string
	}{
		{"json", "```json\n{\"args\":{\"code\":\"return input * 2;\"}}\n```", "│     \"code\": \"return input * 2;\""},
		{"go", "```go\nfunc main(){println(1)}\n```", "│ func main() { println(1) }"},
		{"open json", "```json\n{\"count\":42}", "│ {\"count\":42}"},
		{"invalid json", "~~~json\n{\"count\":}\n~~~", "│ {\"count\":}"},
		{"nested fence", "> ~~~json\n> {\"count\":42}\n> ~~~", "│ │   \"count\": 42"},
		{"yaml", "~~~yml\nconfig: {enabled: true, names: [demo, test]}\n~~~", "│   enabled: true"},
		{"bash", "~~~bash\nif [[ -n $NAME ]];then echo \"$NAME\";fi\n~~~", "; then echo \"$NAME\"; fi"},
		{"posix", "~~~posix-shell\nif [ -n \"$NAME\" ];then echo \"$NAME\";fi\n~~~", "; then echo \"$NAME\"; fi"},
		{"other language", "```python\nx=[1,2,3]\n```", "│ x=[1,2,3]"},
	} {
		t.Run(sample.name, func(t *testing.T) {
			rows := Render(sample.source, 80, testStyles())
			got := ansi.Strip(strings.Join(rows, "\n"))
			if !strings.Contains(got, sample.want) {
				t.Fatalf("missing %q in %q", sample.want, got)
			}
		})
	}
}

func TestShellAndYAMLFormattingPreserveContent(t *testing.T) {
	bash := "# keep comment\nif true;then cat <<'EOF'\n  $NAME\nEOF\nfi\n"
	formatted := formatCode(bash, "bash")
	if formatted == bash || !strings.Contains(formatted, "# keep comment") || !strings.Contains(formatted, "\n  $NAME\nEOF\n") {
		t.Fatalf("lost shell structure or literal content: %q", formatted)
	}

	document := "defaults: &defaults {name: \"yes\", count: 2} # keep comment\ncopy: *defaults\ntext: |\n  line one\n    line two\n"
	formatted = formatCode(document, "yaml")
	for _, literal := range []string{"&defaults", "*defaults", `"yes"`, "# keep comment", "  line one\n    line two"} {
		if !strings.Contains(formatted, literal) {
			t.Fatalf("missing %q after YAML formatting: %q", literal, formatted)
		}
	}

	for _, sample := range []struct{ language, source string }{
		{"bash", "if true; then"},
		{"sh", "values=(one two)"},
		{"yaml", "a: [1, 2"},
		{"yaml", "---\na: [1, 2]\n---\nb: 3\n"},
		{"yaml", "%YAML 1.1\n---\na: [yes, no]\n"},
	} {
		if got := formatCode(sample.source, sample.language); got != sample.source {
			t.Fatalf("%s fallback changed: %q", sample.language, got)
		}
	}
}

func TestJavaScriptFamilyFencesFormatWithoutRunningTheSnippet(t *testing.T) {
	for _, sample := range []struct {
		language, source, want string
	}{
		{"js", "throw new Error('only source');const x=(a)=>a+1", "const x = (a) => a + 1;"},
		{"ts", "const x=(a:number):number=>a+1", "const x = (a: number): number => a + 1;"},
		{"jsx", "const x=<div id='x'>{4+3}</div>", `const x = <div id="x">{4 + 3}</div>;`},
		{"tsx", "const x=(p:{value:number})=><div>{p.value}</div>", "const x = (p: { value: number }) => <div>{p.value}</div>;"},
		{"typescript", "const x=<number>1", "const x = <number>1;"},
	} {
		t.Run(sample.language, func(t *testing.T) {
			source := "~~~" + sample.language + "\n" + sample.source + "\n~~~"
			rows := Render(source, 100, testStyles())
			got := strings.Join(rows, "\n")
			if !strings.Contains(ansi.Strip(got), sample.want) {
				t.Fatalf("missing %q in %q", sample.want, got)
			}
			if got == ansi.Strip(got) {
				t.Fatal("formatted code lost syntax colors")
			}
		})
	}

	for _, source := range []string{"~~~ts\nconst x=(a:number)=>a+1", "~~~tsx\nconst x=<div>\n~~~"} {
		rows := Render(source, 100, testStyles())
		got := ansi.Strip(strings.Join(rows, "\n"))
		snippet := strings.Split(source, "\n")[1]
		if !strings.Contains(got, snippet) {
			t.Fatalf("unfinished snippet changed: %q", got)
		}
	}
}

func TestWebFencesUsePrettier(t *testing.T) {
	for _, sample := range []struct {
		language, source, want string
	}{
		{"html", "<div   class='card'>Hello</div>", `<div class="card">Hello</div>`},
		{"css", ".card{color:red}", "  color: red;"},
		{"scss", "$accent:red; .card{color:$accent}", "  color: $accent;"},
		{"graphql", "query{viewer{id}}", "  viewer {"},
		{"gql", "type Query{hello:String}", "  hello: String"},
	} {
		t.Run(sample.language, func(t *testing.T) {
			source := "~~~" + sample.language + "\n" + sample.source + "\n~~~"
			got := strings.Join(Render(source, 100, testStyles()), "\n")
			plain := ansi.Strip(got)
			if !strings.Contains(plain, sample.want) || got == plain {
				t.Fatalf("missing formatted and highlighted %q in %q", sample.want, got)
			}
		})
	}
}

func TestRustFencesFormatAndHighlight(t *testing.T) {
	const snippet = "async fn doubled(x:i32)->i32{x*2}"
	styles := testStyles()
	for _, language := range []string{"rust", "rs"} {
		source := "~~~" + language + "\n" + snippet
		rows := Render(source+"\n~~~", 100, styles)
		got := strings.Join(rows, "\n")
		if !strings.Contains(ansi.Strip(got), "async fn doubled(x: i32) -> i32 {") ||
			!strings.Contains(got, styles.Keyword.Render("async")) {
			t.Fatalf("%s fence did not format and highlight: %q", language, got)
		}
		open := strings.Join(Render(source, 100, styles), "\n")
		if !strings.Contains(ansi.Strip(open), snippet) {
			t.Fatalf("%s open fence changed: %q", language, open)
		}
	}
}

func TestCAndCPPFencesUseClangFormat(t *testing.T) {
	if !WarmClangFormat() {
		t.Fatal("could not compile ClangFormat")
	}

	for _, sample := range []struct {
		language, source, want string
	}{
		{"c", "int main(){return 0;}", "int main() { return 0; }"},
		{"cpp", "template<class T> T twice(T value){return value+value;}", "template <class T> T twice(T value) { return value + value; }"},
		{"c++", "auto twice=[](int value){return value+value;};", "auto twice = [](int value) { return value + value; };"},
	} {
		t.Run(sample.language, func(t *testing.T) {
			fence := "~~~" + sample.language + "\n" + sample.source + "\n~~~"
			rows := Render(fence, 100, testStyles())
			got := ansi.Strip(strings.Join(rows, "\n"))
			if !strings.Contains(got, sample.want) {
				t.Fatalf("missing %q in %q", sample.want, got)
			}
		})
	}

	source := "#include <z.h>\n#include <a.h>\n#define TWICE(x) ((x)+(x))\n"
	formatted := formatCode(source, "c")
	if formatted == source || !strings.HasPrefix(formatted, "#include <z.h>\n#include <a.h>") ||
		!strings.Contains(formatted, "#define TWICE(x) ((x) + (x))") {
		t.Fatalf("unexpected include or macro formatting: %q", formatted)
	}

	const fragment = "void f("
	if got := formatCode(fragment, "cpp"); got != fragment {
		t.Fatalf("unfinished declaration changed: %q", got)
	}
}
func TestCodeFormattingBounds(t *testing.T) {
	for _, source := range []string{
		strings.Repeat("[", 40) + "0" + strings.Repeat("]", 40),
		"[" + strings.Repeat("0,", maxCodeBytes/2) + "0]",
	} {
		if got := formatCode(source, "json"); got != source {
			t.Fatal("formatting expanded a bounded fallback")
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
