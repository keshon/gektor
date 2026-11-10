package checks

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/keshon/gektor/internal/workspace"
)

const fixtureSecret = "AKIAIOSFODNN7EXAMPLE"

func checkWorkspace(t *testing.T, name, content string) *workspace.Workspace {
	t.Helper()
	ws, err := workspace.New(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { ws.Close() })
	if err := os.WriteFile(filepath.Join(ws.Root(), name), []byte(content), 0600); err != nil {
		t.Fatal(err)
	}
	return ws
}
func TestReporterSharesDeduplicationAndScopes(t *testing.T) {
	ws := checkWorkspace(t, "a.txt", fixtureSecret)
	if err := ws.WriteFile("b.txt", []byte(fixtureSecret), 0600); err != nil {
		t.Fatal(err)
	}
	var scopes []Scope
	var findings []Finding
	r := NewReporter(Secrets(), func(scope Scope, f Finding) { scopes = append(scopes, scope); findings = append(findings, f) })
	r.Check(ws, filepath.Join(ws.Root(), "a.txt"))
	r.Sweep(ws, []string{"a.txt", "b.txt", "gone.txt", "../escape.txt"})
	if len(findings) != 2 || scopes[0] != PerEdit || scopes[1] != SessionEnd || findings[0].Path != "a.txt" || findings[1].Path != "b.txt" {
		t.Fatalf("findings=%+v scopes=%v", findings, scopes)
	}
}
func TestReporterOversizedFilesAreExplicitlySkipped(t *testing.T) {
	for _, scope := range []Scope{PerEdit, SessionEnd} {
		t.Run(string(scope), func(t *testing.T) {
			ws := checkWorkspace(t, "large.txt", strings.Repeat(" ", scanMaxBytes)+fixtureSecret)
			var got []Finding
			r := NewReporter(Secrets(), func(_ Scope, f Finding) { got = append(got, f) })
			if scope == PerEdit {
				r.Check(ws, "large.txt")
			} else {
				r.Sweep(ws, []string{"large.txt"})
			}
			if len(got) != 1 || got[0].Rule != "scan-skipped" || got[0].Path != "large.txt" {
				t.Fatalf("silent or partial scan: %+v", got)
			}
		})
	}
}
func TestReporterAcceptsCompleteFileAtLimit(t *testing.T) {
	ws := checkWorkspace(t, "limit.txt", fixtureSecret+strings.Repeat(" ", scanMaxBytes-len(fixtureSecret)))
	var got []Finding
	NewReporter(Secrets(), func(_ Scope, f Finding) { got = append(got, f) }).Check(ws, "limit.txt")
	if len(got) != 1 || got[0].Rule != "secret-aws" {
		t.Fatalf("at-limit file was not checked: %+v", got)
	}
}
func TestReporterCapsBothPassesWithoutCachingWithheldFindings(t *testing.T) {
	for _, scope := range []Scope{PerEdit, SessionEnd} {
		t.Run(string(scope), func(t *testing.T) {
			ws := checkWorkspace(t, "many.txt", strings.Repeat(fixtureSecret+"\n", scanMaxFindings+13))
			var got []Finding
			r := NewReporter(Secrets(), func(_ Scope, f Finding) { got = append(got, f) })
			if scope == PerEdit {
				r.Check(ws, "many.txt")
			} else {
				r.Sweep(ws, []string{"many.txt"})
			}
			marker := "scan-truncated"
			if scope == SessionEnd {
				marker = "sweep-truncated"
			}
			if len(got) != scanMaxFindings+1 || got[len(got)-1].Rule != marker || !strings.Contains(got[len(got)-1].Summary, "13 more") {
				t.Fatalf("unbounded findings: %+v", got)
			}
			if len(r.cache.seen) != scanMaxFindings {
				t.Fatalf("cached withheld findings: %d", len(r.cache.seen))
			}
		})
	}
}
func TestReporterFileCountBoundAndDisabledScanning(t *testing.T) {
	ws := checkWorkspace(t, "a.txt", fixtureSecret)
	var got []Finding
	r := NewReporter(Secrets(), func(_ Scope, f Finding) { got = append(got, f) })
	r.Sweep(ws, make([]string, sweepMaxFiles+1))
	if len(got) != 1 || got[0].Rule != "sweep-skipped" {
		t.Fatalf("missing file limit marker: %+v", got)
	}
	var none *Reporter
	none.Check(ws, "a.txt")
	none.Sweep(ws, []string{"a.txt"})
	NewReporter(Secrets(), nil).Check(ws, "a.txt")
	NewReporter(nil, func(_ Scope, _ Finding) { t.Fatal("disabled check emitted") }).Check(ws, "a.txt")
}

func TestReporterAnnouncesEachTruncatedEdit(t *testing.T) {
	content := strings.Repeat(fixtureSecret+"\n", scanMaxFindings+1)
	ws := checkWorkspace(t, "first.txt", content)
	if err := ws.WriteFile("second.txt", []byte(content), 0600); err != nil {
		t.Fatal(err)
	}
	notices := 0
	r := NewReporter(Secrets(), func(_ Scope, f Finding) {
		if f.Rule == "scan-truncated" {
			notices++
		}
	})
	r.Check(ws, "first.txt")
	r.Check(ws, "second.txt")
	if notices != 2 {
		t.Fatalf("a previous edit suppressed the next limit notice: %d", notices)
	}
}
