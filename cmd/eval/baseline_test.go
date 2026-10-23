package main

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/keshon/gektor/internal/workspace"
)

func TestCoreSuiteIsExplicitAndComplete(t *testing.T) {
	probes := []Probe{}
	for _, name := range coreProbes {
		probes = append(probes, Probe{Name: name})
	}
	probes = append(probes, Probe{Name: "unrelated"})
	selected, err := selectSuite(probes, "core")
	if err != nil || len(selected) != 7 {
		t.Fatal(selected, err)
	}
	for i, p := range selected {
		if p.Name != coreProbes[i] {
			t.Fatal(selected)
		}
	}
	if _, err := selectSuite(probes[:2], "core"); err == nil {
		t.Fatal("missing baseline scenario accepted")
	}
	if _, err := selectSuite(probes, "typo"); err == nil {
		t.Fatal("unknown suite accepted")
	}
}

func TestLedgerAcceptanceRejectsSeedAndAcceptsRepair(t *testing.T) {
	root, err := filepath.Abs("../..")
	if err != nil {
		t.Fatal(err)
	}
	seed := filepath.Join(root, "eval", "fixtures-ledger")
	dir := t.TempDir()
	if err := copyTree(seed, dir); err != nil {
		t.Fatal(err)
	}
	ws, err := workspace.New(dir)
	if err != nil {
		t.Fatal(err)
	}
	defer ws.Close()
	check := Check{Type: "go_acceptance", Path: filepath.Join(root, "eval", "verifiers", "ledger.gotest")}
	if out, ok := RunCheck(context.Background(), check, ws); ok {
		t.Fatal("broken seed passed", out)
	}
	const fixed = `package ledger
import("fmt";"strconv";"strings";"regexp")
func TotalCents(values []string)(int,error){
total:=0
for _,raw:=range values {
s:=strings.TrimSpace(raw)
if !regexp.MustCompile("^[+-]?[0-9]+([.][0-9]{1,2})?$").MatchString(s){return 0,fmt.Errorf("invalid")}
sign:=1;if s[0]=='-'{sign=-1;s=s[1:]}else if s[0]=='+'{s=s[1:]}
p:=strings.Split(s,".");whole,err:=strconv.Atoi(p[0]);if err!=nil{return 0,err};frac:=0
if len(p)==2{part:=p[1];if len(part)==1{part+="0"};frac,err=strconv.Atoi(part);if err!=nil{return 0,err}}
total+=sign*(whole*100+frac)
};return total,nil}
`
	if err := ws.WriteFile("ledger.go", []byte(fixed), 0600); err != nil {
		t.Fatal(err)
	}
	cli, err := ws.ReadFile("cmd/ledger/main.go")
	if err != nil {
		t.Fatal(err)
	}
	if err := ws.WriteFile("cmd/ledger/main.go", []byte(strings.ReplaceAll(string(cli), "os.Args)", "os.Args[1:])")), 0600); err != nil {
		t.Fatal(err)
	}
	if out, ok := RunCheck(context.Background(), check, ws); !ok {
		t.Fatal("correct repair failed", out)
	}
	files, err := ws.ReadDir(".")
	if err != nil {
		t.Fatal(err)
	}
	for _, entry := range files {
		if strings.HasPrefix(entry.Name(), "gektor_acceptance_") {
			t.Fatal("verifier left behind", entry.Name())
		}
	}
}

func TestBaselineSummaryExcludesInfrastructureAndReportsFailures(t *testing.T) {
	rows := []Result{
		{Probe: "a", Passed: true, Seconds: 10, Steps: 2, PromptTokens: 100, CachedTokens: 40, CompletionTokens: 20},
		{Probe: "a", Seconds: 30, Steps: 8, PromptTokens: 300, ToolFailures: map[string]int{"execution_error": 2}, Tools: map[string]int{"ask_user": 1}},
		{Probe: "b", Errored: true, Seconds: 999, Steps: 999},
		{Probe: "c", Skipped: true},
	}
	s := aggregateRuns(rows)
	if s.Scored != 2 || s.Passed != 1 || s.Errors != 1 || s.Skipped != 1 || s.MedianSeconds != 20 || s.MedianSteps != 5 || s.ToolFailures != 2 || s.PromptTokens != 400 || s.CachedTokens != 40 || s.Clarifications != 1 {
		t.Fatal(s)
	}
	dir := t.TempDir()
	if err := writeSummary(dir, rows); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(filepath.Join(dir, "summary.json")); err != nil {
		t.Fatal(err)
	}
}

func TestTrustedVerifierChangesExperimentIdentity(t *testing.T) {
	file := filepath.Join(t.TempDir(), "acceptance.gotest")
	if err := os.WriteFile(file, []byte("first"), 0600); err != nil {
		t.Fatal(err)
	}
	probes := []Probe{{Name: "example", Verify: []Check{{Type: "go_acceptance", Path: file}}}}
	a, err := inputsDigest(probes)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(file, []byte("second"), 0600); err != nil {
		t.Fatal(err)
	}
	b, err := inputsDigest(probes)
	if err != nil || a == b {
		t.Fatal("verifier change was invisible", err)
	}
}
