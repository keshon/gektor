package checks

import (
	"regexp"
	"strings"
)

// Package checks runs deterministic checks over files an agent just
// wrote, and reports each finding once. No LLM, no network, stdlib only.
// Findings are report-only data; the loop decides what to do with them
// (report, never repair as a side effect).
//
// Nothing here knows a language. Rules are supplied by whoever builds
// the run -- Go lives in internal/gorules, which registers itself like
// any other -- so adding a formatter for another language is a new file
// and no change to this package. It used to switch on a ".go" suffix and
// import go/format, which put one language inside a generically named
// package and made every other language someone else's problem.

// Finding is one deterministic check result.
type Finding struct {
	Rule    string
	Path    string
	Line    int
	Summary string
}

// Rule is one check over a file's bytes. Name appears in findings so a
// reader can tell which rule fired; Check returns nothing when the file
// is clean. A rule decides for itself whether it applies -- by extension,
// by content, by anything -- so this package never has to know.
type Rule interface {
	Name() string
	Check(path string, src []byte) []Finding
}

// Scan runs every rule over path. A rule that does not apply returns
// nothing, so the caller passes all of them for every file.
func Scan(rules []Rule, path string, src []byte) []Finding {
	var out []Finding
	for _, r := range rules {
		if r != nil {
			out = append(out, r.Check(path, src)...)
		}
	}
	return out
}

// secretRule scans for known credential prefixes. Deliberately short and
// low-false-positive: well-known prefixes, not generic key=value shapes
// (those flag prose). Curated, not exhaustive -- a tripwire, not a vault
// audit.
type secretRule struct{}

func (secretRule) Name() string { return "secrets" }

var secretPatterns = []*regexp.Regexp{
	regexp.MustCompile(`AKIA[0-9A-Z]{16}`),
	regexp.MustCompile(`(?:github_pat_|ghp_)[A-Za-z0-9_]{10,}`),
	regexp.MustCompile(`-----BEGIN (?:RSA |EC |OPENSSH |DSA )?PRIVATE KEY(?: BLOCK)?-----`),
	regexp.MustCompile(`xox[bpas]-[A-Za-z0-9-]+`),
}

func (secretRule) Check(path string, src []byte) []Finding {
	var out []Finding
	for i, line := range strings.Split(string(src), "\n") {
		for j, re := range secretPatterns {
			if re.MatchString(line) {
				out = append(out, Finding{
					Rule:    secretRuleNames[j],
					Path:    path,
					Line:    i + 1,
					Summary: "possible secret (" + secretRuleNames[j] + ")",
				})
			}
		}
	}
	return out
}

// secretRuleNames parallels secretPatterns; the pattern text itself is
// not a useful thing to print.
var secretRuleNames = []string{
	"secret-aws", "secret-github", "secret-private-key", "secret-slack",
}

// Secrets is the built-in rule set: checks that apply to every text file
// in any language.
func Secrets() []Rule { return []Rule{secretRule{}} }