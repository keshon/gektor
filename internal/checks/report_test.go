package checks

import (
	"fmt"
	"os"
	"path/filepath"
	"testing"

	"github.com/keshon/gektor/internal/workspace"
)

// dirty is a file with gofmt drift and nothing else.
func dirty(t *testing.T, dir, name string) *workspace.Workspace {
	t.Helper()
	ws, err := workspace.New(dir)
	if ws != nil {
		t.Cleanup(func() { ws.Close() })
	}
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, name), []byte("package a\n\nfunc A()  {}\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	return ws
}

// Post-write and deep-pass findings share one cache, so a file dirty
// throughout a run is reported once rather than once per pass that
// noticed it.
func TestReporter_DedupesAcrossTiers(t *testing.T) {
	var got []Finding
	r := NewReporter(testRules(), func(_ Scope, f Finding) { got = append(got, f) })
	ws := dirty(t, t.TempDir(), "a.go")

	r.PerEdit("gofmt", "a.go", 3, "not gofmt-clean")
	r.Sweep(ws, []string{"a.go"})

	if len(got) != 1 {
		t.Fatalf("%d findings, want exactly 1 (cross-tier dedupe): %+v", len(got), got)
	}
	if got[0].Summary != "not gofmt-clean" {
		t.Fatalf("finding = %+v", got[0])
	}
}

// The deep pass is the only tier that sees changes made outside a tool
// call, so a finding it finds first has to be emitted. It used to filter
// its own results and then re-enter the reporting path, which found them
// all already seen and emitted nothing.
func TestReporter_SweepEmitsWhenItIsTheFirstReporter(t *testing.T) {
	var got []Finding
	r := NewReporter(testRules(), func(_ Scope, f Finding) { got = append(got, f) })
	ws := dirty(t, t.TempDir(), "stray.go")

	r.Sweep(ws, []string{"stray.go"})

	if len(got) == 0 {
		t.Fatal("sweep emitted nothing")
	}
	for _, f := range got {
		if f.Rule != "gofmt" {
			t.Fatalf("unexpected finding %+v", f)
		}
	}
}

// Each pass labels its own findings, so a reader can tell a post-write
// scan from the deep pass.
func TestReporter_ScopesItsFindings(t *testing.T) {
	type pair struct {
		scope Scope
		f     Finding
	}
	var got []pair
	r := NewReporter(testRules(), func(s Scope, f Finding) { got = append(got, pair{s, f}) })
	ws := dirty(t, t.TempDir(), "a.go")
	if err := os.WriteFile(filepath.Join(ws.Root(), "b.go"), []byte("package a\n\nfunc B()  {}\n"), 0o644); err != nil {
		t.Fatal(err)
	}

	r.PerEdit("gofmt", "a.go", 3, "not gofmt-clean")
	r.Sweep(ws, []string{"b.go"})

	if len(got) != 2 {
		t.Fatalf("%d findings, want 2 (one per pass): %+v", len(got), got)
	}
	if got[0].scope != PerEdit {
		t.Fatalf("first scope = %q, want %q", got[0].scope, PerEdit)
	}
	if got[1].scope != SessionEnd {
		t.Fatalf("second scope = %q, want %q", got[1].scope, SessionEnd)
	}
}

// A bound that truncates silently is worse than no bound: the reader
// concludes the file is clean.
func TestReporter_BoundsAnnounceThemselves(t *testing.T) {
	var got []Finding
	r := NewReporter(testRules(), func(_ Scope, f Finding) { got = append(got, f) })
	ws, err := workspace.New(t.TempDir())
	if ws != nil {
		t.Cleanup(func() { ws.Close() })
	}
	if err != nil {
		t.Fatal(err)
	}
	paths := make([]string, 0, sweepMaxFiles+6)
	for i := 0; i <= sweepMaxFiles+5; i++ {
		paths = append(paths, "f.go")
	}

	r.Sweep(ws, paths)

	if len(got) != 1 || got[0].Rule != "sweep-skipped" {
		t.Fatalf("bound must announce itself, got %+v", got)
	}
}

func TestReporter_BoundsFindings(t *testing.T) {
	var got []Finding
	r := NewReporter(testRules(), func(_ Scope, f Finding) { got = append(got, f) })
	dir := t.TempDir()
	ws, err := workspace.New(dir)
	if ws != nil {
		t.Cleanup(func() { ws.Close() })
	}
	if err != nil {
		t.Fatal(err)
	}
	// gofmt reports one line per file, so the count bound needs distinct
	// files rather than a long single one.
	paths := make([]string, 0, sweepMaxFindings+5)
	for i := 0; i < sweepMaxFindings+5; i++ {
		name := fmt.Sprintf("f%02d.go", i)
		if err := os.WriteFile(filepath.Join(dir, name), []byte("package a\n\nfunc A()  {}\n"), 0o644); err != nil {
			t.Fatal(err)
		}
		paths = append(paths, name)
	}

	r.Sweep(ws, paths)

	last := got[len(got)-1]
	if last.Rule != "sweep-truncated" {
		t.Fatalf("withheld findings must be announced, got %d findings ending %+v", len(got), last)
	}
}

// Files that vanished or escaped the workspace mid-run are skipped, not
// fatal: the sweep reports on what is still there.
func TestReporter_SweepSkipsUnreadablePaths(t *testing.T) {
	var got []Finding
	r := NewReporter(testRules(), func(_ Scope, f Finding) { got = append(got, f) })
	ws := dirty(t, t.TempDir(), "a.go")

	r.Sweep(ws, []string{"a.go", "gone.go", "../escape.go"})

	if len(got) != 1 || got[0].Path != "a.go" {
		t.Fatalf("got %+v, want only the readable file", got)
	}
}

// A nil reporter is inert, and a nil sink emits nowhere. Both must be
// safe: a run with no checks configured must not scan, must not panic,
// and must not spend time proving it.
func TestReporter_NilIsInert(t *testing.T) {
	var nilReporter *Reporter
	nilReporter.PerEdit("gofmt", "a.go", 1, "x")
	nilReporter.Sweep(nil, nil)

	r := NewReporter(testRules(), nil)
	r.PerEdit("gofmt", "a.go", 1, "x")
	ws := dirty(t, t.TempDir(), "a.go")
	r.Sweep(ws, []string{"a.go"})

	// Nothing to assert but survival; a panic here fails the test.
}

// testRules is what a Go project runs: the universal secret scan plus
// gofmt. checks.Secrets alone would find nothing in a .go file.
func testRules() []Rule { return append(Secrets(), stubGofmt()) }

// stubGofmt stands in for the real rule. It reports line 3, because the
// dedupe cache keys on rule + path + line: a stub reporting a different
// line would look like a second, distinct finding and the cross-tier
// test would pass for the wrong reason.
func stubGofmt() Rule {
	return stubRule{name: "gofmt", find: true, onlyFor: ".go", line: 3}
}
