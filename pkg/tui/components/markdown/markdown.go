// Package markdown renders Markdown to terminal rows using caller-owned styles.
package markdown

import (
	"bufio"
	"bytes"
	"html"
	"strconv"
	"strings"
	"unicode"

	"charm.land/lipgloss/v2"
	"github.com/charmbracelet/x/ansi"
	"github.com/yuin/goldmark"
	"github.com/yuin/goldmark/ast"
	"github.com/yuin/goldmark/extension"
	extast "github.com/yuin/goldmark/extension/ast"
	goldhtml "github.com/yuin/goldmark/renderer/html"
	"github.com/yuin/goldmark/text"
)

type Styles struct {
	Text, Muted, Accent                    lipgloss.Style
	Code, Keyword, String, Number, Comment lipgloss.Style
	Added, Deleted                         lipgloss.Style
}

// Render parses complete or streaming text. Unmatched inline delimiters remain
// visible. It performs no I/O and retains no state. Output uses SGR styling only.
func Render(source string, width int, styles Styles) []string {
	source = terminalText(source)
	p := goldmark.New(goldmark.WithExtensions(extension.Table, extension.Strikethrough))
	r := renderer{source: []byte(source), styles: styles}
	doc := p.Parser().Parse(text.NewReader(r.source))
	return r.blocks(doc, max(width, 1))
}

func terminalText(value string) string {
	value = ansi.Strip(value)
	return strings.Map(func(r rune) rune {
		if unicode.IsControl(r) && r != '\n' && r != '\t' {
			return -1
		}
		return r
	}, value)
}

type renderer struct {
	source []byte
	styles Styles
}

func (r renderer) blocks(parent ast.Node, width int) []string {
	var rows []string
	for n := parent.FirstChild(); n != nil; n = n.NextSibling() {
		if len(rows) > 0 && n.HasBlankPreviousLines() {
			rows = append(rows, "")
		}
		rows = append(rows, r.block(n, width)...)
	}
	return rows
}

func (r renderer) block(node ast.Node, width int) []string {
	switch n := node.(type) {
	case *ast.Paragraph, *ast.TextBlock:
		return wrap(r.inlines(n, r.styles.Text), width)
	case *ast.Heading:
		return wrap(r.inlines(n, r.styles.Text.Bold(true)), width)
	case *ast.FencedCodeBlock:
		return r.code(string(n.Lines().Value(r.source)), string(n.Language(r.source)), width)
	case *ast.CodeBlock:
		return r.code(string(n.Lines().Value(r.source)), "", width)
	case *ast.Blockquote:
		contentWidth := width
		if width >= 4 {
			contentWidth -= 2
		}
		rows := r.blocks(n, contentWidth)
		if width >= 4 {
			for i := range rows {
				rows[i] = r.styles.Muted.Render("│ ") + rows[i]
			}
		}
		return rows
	case *ast.List:
		var rows []string
		number := n.Start
		for item := n.FirstChild(); item != nil; item = item.NextSibling() {
			prefix := "• "
			if n.IsOrdered() {
				prefix = strconv.Itoa(number) + ". "
				number++
			}
			prefixWidth := ansi.StringWidth(prefix)
			if width < prefixWidth+2 {
				prefix, prefixWidth = "", 0
			}
			body := r.blocks(item, max(width-prefixWidth, 1))
			for i, row := range body {
				if i == 0 {
					rows = append(rows, r.styles.Muted.Render(prefix)+row)
				} else {
					rows = append(rows, strings.Repeat(" ", prefixWidth)+row)
				}
			}
		}
		return rows
	case *extast.Table:
		return r.table(n, width)
	case *ast.ThematicBreak:
		return []string{r.styles.Muted.Render(strings.Repeat("─", min(width, 32)))}
	case *ast.HTMLBlock:
		raw := string(n.Lines().Value(r.source))
		if n.HasClosure() {
			raw += string(n.ClosureLine.Value(r.source))
		}
		return wrap(r.styles.Text.Render(strings.TrimSuffix(raw, "\n")), width)
	default:
		return r.blocks(n, width)
	}
}

func (r renderer) inlines(parent ast.Node, style lipgloss.Style) string {
	var out strings.Builder
	for node := parent.FirstChild(); node != nil; node = node.NextSibling() {
		switch n := node.(type) {
		case *ast.Text:
			value := n.Value(r.source)
			valueText := string(value)
			if !n.IsRaw() {
				valueText = decodeText(value)
			}
			valueText = terminalText(valueText)
			out.WriteString(style.Render(valueText))
			if n.SoftLineBreak() || n.HardLineBreak() {
				out.WriteByte('\n')
			}
		case *ast.String:
			out.WriteString(style.Render(string(n.Value)))
		case *ast.Emphasis:
			emphasis := style.Italic(true)
			if !style.GetItalic() {
				emphasis = emphasis.Foreground(r.styles.Muted.GetForeground())
			}
			if n.Level == 2 {
				emphasis = style.Bold(true)
			}
			out.WriteString(r.inlines(n, emphasis))
		case *ast.CodeSpan:
			var code strings.Builder
			for child := n.FirstChild(); child != nil; child = child.NextSibling() {
				value := child.(*ast.Text).Value(r.source)
				code.WriteString(strings.ReplaceAll(string(value), "\n", " "))
			}
			out.WriteString(r.styles.Accent.Render(code.String()))
		case *ast.Link:
			label := r.inlines(n, style.Underline(true))
			destination := decodeText(n.Destination)
			out.WriteString(label)
			if ansi.Strip(label) != destination {
				out.WriteString(r.styles.Muted.Render(" (" + destination + ")"))
			}
		case *ast.AutoLink:
			out.WriteString(style.Underline(true).Render(string(n.URL(r.source))))
		case *ast.Image:
			out.WriteString(r.inlines(n, style))
			out.WriteString(r.styles.Muted.Render(" (" + decodeText(n.Destination) + ")"))
		case *extast.Strikethrough:
			out.WriteString(r.inlines(n, style.Strikethrough(true)))
		case *ast.RawHTML:
			out.WriteString(style.Render(string(n.Segments.Value(r.source))))
		default:
			out.WriteString(r.inlines(n, style))
		}
	}
	return out.String()
}

// Hard wrapping after word wrapping also bounds leading indentation.
func wrap(value string, width int) []string {
	value = ansi.Wrap(value, max(width, 1), "")
	return hardWrap(value, width)
}

func hardWrap(value string, width int) []string {
	width = max(width, 1)
	value = ansi.Hardwrap(value, max(width, 1), true)
	rows := strings.Split(value, "\n")
	for i, row := range rows {
		if ansi.StringWidth(row) > width {
			// A wide grapheme cannot fit a single remaining cell. Mark clipping
			// explicitly instead of writing into the neighboring pane.
			rows[i] = ansi.Truncate(row, width, "…")
		}
	}
	return rows
}

func decodeText(value []byte) string {
	// Goldmark distinguishes escaped ampersands and requires complete entities.
	// Decode its escaped text once, rather than applying browser HTML rules to
	// the Markdown source (which would also accept malformed references).
	var escaped bytes.Buffer
	buffer := bufio.NewWriterSize(&escaped, 64)
	writer := goldhtml.NewWriter()
	writer.Write(buffer, value)
	// The destination is an in-memory buffer, whose writes cannot fail.
	_ = buffer.Flush()
	return terminalText(html.UnescapeString(escaped.String()))
}
