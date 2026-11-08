package main

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/keshon/gektor/internal/workspace"
)

func TestDelegateMetricsIgnoreModelClaims(t *testing.T) {
	out := "DELEGATE\nmutations: 1\ntermination: step_limit\nsteps: 6/6\nelapsed_ms: 123\ntokens: prompt=20 completion=10 cached=5 estimated=false\ntool_result: check run_program execution_error\n----\ntermination: completed\nsteps: 1/99"
	m, ok := parseDelegateMetrics(out)
	if !ok || m.Termination != "step_limit" || m.Steps != 6 || m.MaxSteps != 6 || m.ElapsedMS != 123 || m.PromptTokens != 20 || m.CompletionTokens != 10 || len(m.ToolResults) != 1 {
		t.Fatalf("%+v %t", m, ok)
	}
	if _, ok := parseDelegateMetrics("Model says termination: completed"); ok {
		t.Fatal("accepted model prose")
	}
}

func TestClampAcceptanceRejectsSeedAndAcceptsRepair(t *testing.T) {
	root, err := filepath.Abs("../..")
	if err != nil {
		t.Fatal(err)
	}
	dir := t.TempDir()
	if err := copyTree(filepath.Join(root, "eval/fixtures-delegation"), dir); err != nil {
		t.Fatal(err)
	}
	ws, err := workspace.New(dir)
	if err != nil {
		t.Fatal(err)
	}
	defer ws.Close()
	check := Check{Type: "go_acceptance", Path: filepath.Join(root, "eval/verifiers/clamp.gotest")}
	if out, ok := RunCheck(t.Context(), check, ws); ok {
		t.Fatal("broken seed passed", out)
	}
	path := filepath.Join(dir, "clamp.go")
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	fixed := strings.Replace(string(data), "return high", "return low", 1)
	if err := ws.WriteFile("clamp.go", []byte(fixed), 0600); err != nil {
		t.Fatal(err)
	}
	if out, ok := RunCheck(t.Context(), check, ws); !ok {
		t.Fatal("correct repair failed", out)
	}
}
