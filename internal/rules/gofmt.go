// Package rules holds the language-specific checks, one language per
// file: rustfmt, prettier and a Python formatter join gofmt here. They
// live outside internal/checks so that package stays language neutral —
// a rule decides for itself whether it applies, rather than a generic
// one switching on a file extension.
package rules

import (
	"bytes"
	"go/format"
	"go/scanner"
	"strings"

	"github.com/keshon/tars/internal/checks"
)

// gofmt reports when Go source is not gofmt-clean, or does not parse.
// Line points at the first differing line, or the syntax error.
type gofmt struct{}

func (gofmt) Name() string { return "gofmt" }

func (gofmt) Check(path string, src []byte) []checks.Finding {
	if !strings.HasSuffix(path, ".go") {
		return nil
	}
	formatted, err := format.Source(src)
	if err != nil {
		line := 0
		msg := "does not parse"
		if list, ok := err.(scanner.ErrorList); ok && len(list) > 0 {
			line = list[0].Pos.Line
			msg = "does not parse: " + list[0].Msg
		}
		return []checks.Finding{{Rule: "gofmt-syntax", Path: path, Line: line, Summary: msg}}
	}
	if bytes.Equal(formatted, src) {
		return nil
	}
	return []checks.Finding{{Rule: "gofmt", Path: path, Line: firstDiffLine(src, formatted), Summary: "not gofmt-clean"}}
}

// firstDiffLine returns the 1-based first differing line of a and b.
func firstDiffLine(a, b []byte) int {
	as := bytes.Split(a, []byte{'\n'})
	bs := bytes.Split(b, []byte{'\n'})
	for i := 0; i < len(as) && i < len(bs); i++ {
		if !bytes.Equal(as[i], bs[i]) {
			return i + 1
		}
	}
	return min(len(as), len(bs)) + 1
}

// All returns the language rules. Append to checks.Secrets() where a run
// decides what it checks.
func All() []checks.Rule { return []checks.Rule{gofmt{}} }