package gorules

import (
	"testing"

	"github.com/keshon/tars/internal/checks"
)

func gofmtCheck(path, src string) []checks.Finding {
	r := gofmt{}
	out := r.Check(path, []byte(src))
	if r.Name() != "gofmt" {
		panic("rule name is " + r.Name())
	}
	return out
}

func TestGofmt_CleanPasses(t *testing.T) {
	if out := gofmtCheck("a.go", "package a\n"); len(out) != 0 {
		t.Fatalf("clean flagged: %+v", out)
	}
}

func TestGofmt_DirtyPointsAtLine(t *testing.T) {
	out := gofmtCheck("a.go", "package a\n\nfunc A()  {}\n")
	if len(out) != 1 || out[0].Rule != "gofmt" || out[0].Line != 3 {
		t.Fatalf("got %+v", out)
	}
}

func TestGofmt_SyntaxError(t *testing.T) {
	out := gofmtCheck("a.go", "package a\nfunc {\n")
	if len(out) != 1 || out[0].Rule != "gofmt-syntax" || out[0].Line != 2 {
		t.Fatalf("got %+v", out)
	}
}

// The rule decides applicability itself. checks has no idea what a .go
// file is, which is what makes adding another language a new package
// rather than an edit here.
func TestGofmt_IgnoresOtherLanguages(t *testing.T) {
	if out := gofmtCheck("main.rs", "fn main() {  }\n"); len(out) != 0 {
		t.Fatalf("a Rust file must not be gofmt'd: %+v", out)
	}
	if len(All()) == 0 {
		t.Fatal("All must register the gofmt rule")
	}
}