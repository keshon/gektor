package main

import (
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
)

func writeProbe(t *testing.T, dir, name, body string) {
	t.Helper()
	if err := os.WriteFile(filepath.Join(dir, name), []byte(body), 0o644); err != nil {
		t.Fatal(err)
	}
}

// The nudge table exists to replace an argument with numbers, so it must
// actually appear and must separate the runs that passed from those that
// failed. A summary that silently reported nothing would look exactly
// like "no nudge ever fires", which is the conclusion it exists to test.
func TestSummarize_ReportsNudgesSplitByOutcome(t *testing.T) {
	out := summarize([]Result{
		{Probe: "a", Passed: true, Steps: 4, Nudges: []string{"stuck", "budget"}},
		{Probe: "a", Passed: true, Steps: 3, Nudges: []string{"stuck"}},
		{Probe: "a", Passed: false, Steps: 9, Nudges: []string{"stuck", "todo"}},
		// Excluded from the rate, so also from the nudge table: a run that
		// never reached the model cannot show what a nudge did.
		{Probe: "a", Errored: true, Nudges: []string{"verify"}},
		{Probe: "a", Skipped: true, Nudges: []string{"verify"}},
	})
	for _, want := range []string{"=== harness nudges", "stuck", "budget", "todo"} {
		if !strings.Contains(out, want) {
			t.Fatalf("summary missing %q:\n%s", want, out)
		}
	}
	if strings.Contains(out, "verify") {
		t.Fatalf("errored and skipped runs must not count:\n%s", out)
	}
	// stuck was heard 3 times: twice on passing runs, once on a failure.
	if !strings.Contains(out, "  stuck") || !strings.Contains(out, "  2") {
		t.Fatalf("stuck totals wrong:\n%s", out)
	}
}

func TestSummarize_NoNudgeLineWhenNoneFired(t *testing.T) {
	out := summarize([]Result{{Probe: "a", Passed: true, Steps: 2}})
	if strings.Contains(out, "harness nudges") {
		t.Fatalf("a run with no nudges must not print the table:\n%s", out)
	}
	if !strings.Contains(out, "pass rate") {
		t.Fatalf("pass rate must still be reported:\n%s", out)
	}
}

func TestOsSkipReason(t *testing.T) {
	if skip, _ := osSkipReason(nil); skip {
		t.Error("empty oses must run everywhere")
	}
	if skip, _ := osSkipReason([]string{runtime.GOOS}); skip {
		t.Errorf("host OS %s must not skip", runtime.GOOS)
	}
	other := "windows"
	if runtime.GOOS == "windows" {
		other = "linux"
	}
	skip, reason := osSkipReason([]string{other})
	if !skip || !strings.Contains(reason, other) {
		t.Errorf("want skip for %s, got %v %q", other, skip, reason)
	}
}

func TestLoadProbes_RejectsBadOses(t *testing.T) {
	dir := t.TempDir()
	writeProbe(t, dir, "x.json", `{"task":"t","tier":"smoke","oses":["plan9"],
		"trace":[{"tool":"read_file","mode":"required"}]}`)
	if _, err := loadProbes(dir, ""); err == nil ||
		!strings.Contains(err.Error(), "oses") {
		t.Fatalf("want oses rejection, got %v", err)
	}
}

func TestLoadProbes_AcceptsOsesAndNewCheckType(t *testing.T) {
	dir := t.TempDir()
	writeProbe(t, dir, "x.json", `{"task":"t","tier":"smoke","oses":["windows","linux"],
		"verify":[{"type":"file_absent","path":"gone.txt"}],
		"trace":[{"tool":"write_file","mode":"required"}]}`)
	probes, err := loadProbes(dir, "")
	if err != nil {
		t.Fatalf("load: %v", err)
	}
	if len(probes) != 1 || len(probes[0].Oses) != 2 {
		t.Fatalf("probes=%+v", probes)
	}
	if probes[0].Verify[0].Type != "file_absent" {
		t.Fatalf("verify=%+v", probes[0].Verify)
	}
}

func TestLoadProbes_RejectsBadVerify(t *testing.T) {
	dir := t.TempDir()
	writeProbe(t, dir, "x.json", `{"task":"t","tier":"smoke",
		"verify":[{"type":"content_containz","path":"a","contains":"b"}]}`)
	if _, err := loadProbes(dir, ""); err == nil ||
		!strings.Contains(err.Error(), "verify[0]") {
		t.Fatalf("want verify rejection, got %v", err)
	}
	writeProbe(t, dir, "y.json", `{"task":"t","tier":"smoke",
		"verify":[{"type":"content_contains","path":"a"}]}`)
	if _, err := loadProbes(dir, ""); err == nil ||
		!strings.Contains(err.Error(), "verify[") {
		t.Fatalf("want missing-field rejection, got %v", err)
	}
}

func TestLoadProbes_RejectsBadRegex(t *testing.T) {
	dir := t.TempDir()
	writeProbe(t, dir, "x.json", `{"task":"t","tier":"smoke",
		"trace":[{"tool":"read_file","args_regex":"([unclosed"}]}`)
	if _, err := loadProbes(dir, ""); err == nil ||
		!strings.Contains(err.Error(), "args_regex") {
		t.Fatalf("want regex rejection, got %v", err)
	}
}
