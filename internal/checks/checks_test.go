package checks

import (
	"strings"
	"testing"
)

// A rule decides for itself whether it applies. checks no longer knows
// what a .go file is, which is the whole point: the secret scan runs on
// every file, and a language-specific rule opts in by inspecting the path.
func TestScan_RunsEveryRuleAndLetsEachOptOut(t *testing.T) {
	always := stubRule{name: "always", find: true}
	never := stubRule{name: "never", find: true, onlyFor: ".rs"}
	clean := stubRule{name: "clean"}

	out := Scan([]Rule{always, never, clean}, "main.rs", []byte("x"))
	if len(out) != 2 {
		t.Fatalf("both applicable rules must fire for a .rs file: %+v", out)
	}
	out = Scan([]Rule{always, never, clean}, "main.go", []byte("x"))
	if len(out) != 1 || out[0].Rule != "always" {
		t.Fatalf("the .rs-only rule must opt out of a .go file: %+v", out)
	}
}

func TestScan_NilRulesAndNilEntries(t *testing.T) {
	if out := Scan(nil, "a.go", []byte("x")); len(out) != 0 {
		t.Fatalf("nil rules must scan nothing: %+v", out)
	}
	if out := Scan([]Rule{nil}, "a.go", []byte("x")); len(out) != 0 {
		t.Fatalf("a nil rule must be skipped: %+v", out)
	}
}

type stubRule struct {
	name    string
	find    bool
	onlyFor string // when set, fires only for this suffix
	line    int    // 0 means line 1
}

func (s stubRule) Name() string { return s.name }

func (s stubRule) Check(path string, _ []byte) []Finding {
	if s.onlyFor != "" && !strings.HasSuffix(path, s.onlyFor) {
		return nil
	}
	if !s.find {
		return nil
	}
	line := s.line
	if line == 0 {
		line = 1
	}
	return []Finding{{Rule: s.name, Path: path, Line: line, Summary: "stub"}}
}

func TestSecrets_HitsWithLines(t *testing.T) {
	src := "ok line\nkey = AKIAIOSFODNN7EXAMPLE\n-----BEGIN RSA PRIVATE KEY-----\n"
	out := Scan(Secrets(), "k.env", []byte(src))
	if len(out) != 2 {
		t.Fatalf("got %+v", out)
	}
	if out[0].Line != 2 || out[1].Line != 3 {
		t.Fatalf("lines wrong: %+v", out)
	}
}

func TestSecrets_ProseIsClean(t *testing.T) {
	src := "the secret sauce is patience\napi_key = placeholder\n"
	if out := Scan(Secrets(), "notes.md", []byte(src)); len(out) != 0 {
		t.Fatalf("prose flagged: %+v", out)
	}
}

func TestClamp_CountsDropped(t *testing.T) {
	in := []Finding{{Rule: "a"}, {Rule: "b"}, {Rule: "c"}}
	out, dropped := Clamp(in, 2)
	if len(out) != 2 || dropped != 1 {
		t.Fatalf("clamp = %d findings, %d dropped", len(out), dropped)
	}
}
