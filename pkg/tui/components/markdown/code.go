package markdown

import (
	"bytes"
	"encoding/json"
	"go/format"
	"io"
	"strings"

	"charm.land/lipgloss/v2"
	"github.com/alecthomas/chroma/v2"
	"github.com/alecthomas/chroma/v2/lexers"
	"github.com/charmbracelet/x/ansi"
	"github.com/yuin/goldmark/ast"
	"github.com/yuin/goldmark/parser"
	"github.com/yuin/goldmark/text"
	"go.yaml.in/yaml/v3"
	"mvdan.cc/sh/v3/syntax"
)

const maxCodeBytes = 64 * 1024
const closedFence = "gektor-closed-fence"

// Goldmark also creates fence nodes at EOF. Only a closing delimiter makes
// the block stable enough to format while a message is streaming.
type trackedFence struct{ parser.BlockParser }

func (p trackedFence) Continue(node ast.Node, reader text.Reader, context parser.Context) parser.State {
	state := p.BlockParser.Continue(node, reader, context)
	if state == parser.Close {
		node.SetAttributeString(closedFence, true)
	}
	return state
}

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

func formatCode(source, language string) string {
	if len(source) > maxCodeBytes {
		return source
	}

	switch canonicalLanguage(language) {
	case "json":
		if !jsonDepthFits(source) {
			return source
		}
		var formatted bytes.Buffer
		err := json.Indent(&formatted, []byte(source), "", "  ")
		if err == nil {
			return formatted.String()
		}
	case "go":
		formatted, err := format.Source([]byte(source))
		if err == nil {
			return string(formatted)
		}
	case "javascript":
		return formatPrettier(source, "babel", "snippet.js", prettierBabel, prettierEstree)
	case "jsx":
		return formatPrettier(source, "babel", "snippet.jsx", prettierBabel, prettierEstree)
	case "typescript":
		return formatPrettier(source, "babel-ts", "snippet.ts", prettierBabel, prettierEstree)
	case "tsx":
		return formatPrettier(source, "babel-ts", "snippet.tsx", prettierBabel, prettierEstree)
	case "html":
		return formatPrettier(source, "html", "snippet.html", prettierHTML)
	case "css":
		return formatPrettier(source, "css", "snippet.css", prettierPostCSS)
	case "scss":
		return formatPrettier(source, "scss", "snippet.scss", prettierPostCSS)
	case "graphql":
		return formatPrettier(source, "graphql", "snippet.graphql", prettierGraphQL)
	case "rust":
		return formatPrettier(source, "rust", "snippet.rs", prettierRust)
	case "c":
		return formatClang(source, "snippet.c")
	case "cpp":
		return formatClang(source, "snippet.cpp")
	case "yaml":
		return formatYAML(source)
	case "bash":
		return formatShell(source, syntax.LangBash)
	case "sh":
		return formatShell(source, syntax.LangPOSIX)
	}
	return source
}

// Limit indentation growth for deeply nested generated JSON.
func jsonDepthFits(source string) bool {
	const maxDepth = 32
	decoder := json.NewDecoder(strings.NewReader(source))
	decoder.UseNumber()
	depth := 0
	for {
		token, err := decoder.Token()
		if err != nil {
			return err == io.EOF
		}
		switch token {
		case json.Delim('{'), json.Delim('['):
			depth++
			if depth > maxDepth {
				return false
			}
		case json.Delim('}'), json.Delim(']'):
			depth--
		}
	}
}

func formatYAML(source string) string {
	for line := range strings.SplitSeq(source, "\n") {
		if strings.HasPrefix(line, "%") {
			return source
		}
	}

	decoder := yaml.NewDecoder(strings.NewReader(source))
	var document, extra yaml.Node
	err := decoder.Decode(&document)
	if err != nil {
		return source
	}
	err = decoder.Decode(&extra)
	if err != io.EOF || !yamlBlockStyle(&document, 0) {
		return source
	}

	var formatted bytes.Buffer
	encoder := yaml.NewEncoder(&formatted)
	encoder.SetIndent(2)
	err = encoder.Encode(&document)
	if err != nil {
		return source
	}
	err = encoder.Close()
	if err != nil {
		return source
	}
	return formatted.String()
}

func yamlBlockStyle(node *yaml.Node, depth int) bool {
	const maxDepth = 32
	if depth > maxDepth {
		return false
	}
	// Keep scalar styles, tags, anchors, and comments on their parsed nodes.
	node.Style &^= yaml.FlowStyle
	for _, child := range node.Content {
		if !yamlBlockStyle(child, depth+1) {
			return false
		}
	}
	return true
}

func formatShell(source string, dialect syntax.LangVariant) string {
	parser := syntax.NewParser(syntax.Variant(dialect), syntax.KeepComments(true))
	file, err := parser.Parse(strings.NewReader(source), "")
	if err != nil {
		return source
	}

	var formatted bytes.Buffer
	printer := syntax.NewPrinter(syntax.Indent(2))
	err = printer.Print(&formatted, file)
	if err != nil {
		return source
	}
	return formatted.String()
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
