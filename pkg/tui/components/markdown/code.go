package markdown

import (
	"strings"

	"charm.land/lipgloss/v2"
	"github.com/alecthomas/chroma/v2"
	"github.com/alecthomas/chroma/v2/lexers"
	"github.com/charmbracelet/x/ansi"
)

const maxCodeBytes = 64 * 1024

func canonicalLanguage(language string) string {
	language = strings.ToLower(language)
	switch language {
	case "golang":
		return "go"
	case "js":
		return "javascript"
	case "ts":
		return "typescript"
	case "gql":
		return "graphql"
	case "rs":
		return "rust"
	case "c++", "cc", "cxx":
		return "cpp"
	case "yml":
		return "yaml"
	case "shell":
		return "bash"
	case "posix-shell":
		return "sh"
	case "patch":
		return "diff"
	default:
		return language
	}
}

func (r renderer) code(source, language string, width int) []string {
	source = strings.TrimSuffix(source, "\n")
	source = strings.ReplaceAll(source, "\t", "    ")
	content := r.highlight(source, language)
	label := language
	if label == "" {
		label = "code"
	}
	rows := wrap(r.styles.Muted.Render(label), width)
	contentWidth := width
	if width >= 4 {
		contentWidth -= 2
	}
	for _, line := range strings.Split(content, "\n") {
		for _, fragment := range hardWrap(line, contentWidth) {
			if width >= 4 {
				fragment = r.styles.Muted.Render("│ ") + fragment
			}
			rows = append(rows, fragment)
		}
	}
	return rows
}

func (r renderer) highlight(source, language string) string {
	// Large outputs remain readable without spending unbounded time lexing them.
	if len(source) > maxCodeBytes {
		return r.styles.Code.Render(source)
	}
	language = canonicalLanguage(language)
	if language == "diff" {
		lines := strings.Split(source, "\n")
		for i, line := range lines {
			style := r.styles.Code
			switch {
			case strings.HasPrefix(line, "+++") || strings.HasPrefix(line, "---") || strings.HasPrefix(line, "@@"):
				style = r.styles.Accent
			case strings.HasPrefix(line, "+"):
				style = r.styles.Added
			case strings.HasPrefix(line, "-"):
				style = r.styles.Deleted
			}
			lines[i] = style.Render(line)
		}
		return strings.Join(lines, "\n")
	}
	lexer := lexers.Get(language)
	if lexer == nil || language == "" {
		return r.styles.Code.Render(source)
	}
	iterator, err := lexer.Tokenise(nil, source)
	if err != nil {
		// Highlighting is optional; lexer failures must never hide source text.
		return r.styles.Code.Render(source)
	}
	var out strings.Builder
	for token := iterator(); token != chroma.EOF; token = iterator() {
		style := r.tokenStyle(token.Type)
		out.WriteString(style.Render(token.Value))
	}
	highlighted := out.String()
	// Some lexers add a trailing newline. Preserve the caller's exact code.
	if ansi.Strip(highlighted) != source {
		return r.styles.Code.Render(source)
	}
	return highlighted
}

func (r renderer) tokenStyle(token chroma.TokenType) lipgloss.Style {
	switch token {
	case chroma.KeywordType, chroma.NameClass, chroma.NameTag:
		if r.styles.TypeName != nil {
			return *r.styles.TypeName
		}
	case chroma.NameFunction, chroma.NameBuiltin, chroma.NameAttribute, chroma.NameProperty:
		if r.styles.Symbol != nil {
			return *r.styles.Symbol
		}
	}

	switch {
	case token.InCategory(chroma.Keyword):
		return r.styles.Keyword
	case token.InCategory(chroma.Comment):
		return r.styles.Comment
	case token.InSubCategory(chroma.LiteralString):
		return r.styles.String
	case token.InSubCategory(chroma.LiteralNumber):
		return r.styles.Number
	case token == chroma.NameFunction || token == chroma.NameBuiltin:
		return r.styles.Accent
	default:
		return r.styles.Code
	}
}
