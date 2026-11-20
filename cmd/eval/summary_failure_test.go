package main

import (
	"errors"
	"flag"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
)

func TestEvaluationSummaryFailureProcess(t *testing.T) {
	if os.Getenv("GEKTOR_SUMMARY_FAILURE_TEST") != "1" {
		return
	}
	flag.CommandLine = flag.NewFlagSet("eval", flag.ExitOnError)
	os.Args = []string{"eval", "-probes", os.Getenv("GEKTOR_TEST_PROBES"), "-out", os.Getenv("GEKTOR_TEST_RESULTS"), "-backend-kind", "llama", "-backend", os.Getenv("GEKTOR_TEST_BACKEND")}
	os.Exit(runEvaluation())
}

func TestSummaryWriteFailureReturnsAndReleasesRunLock(t *testing.T) {
	dir := t.TempDir()
	probes, out := filepath.Join(dir, "probes"), filepath.Join(dir, "results")
	if err := os.Mkdir(probes, 0700); err != nil {
		t.Fatal(err)
	}
	otherOS := "windows"
	if runtime.GOOS == "windows" {
		otherOS = "linux"
	}
	probe := fmt.Sprintf(`{"task":"task","tier":"signal","oses":[%q],"verify":[{"type":"none"}]}`, otherOS)
	if err := os.WriteFile(filepath.Join(probes, "probe.json"), []byte(probe), 0600); err != nil {
		t.Fatal(err)
	}
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		entries, err := os.ReadDir(out)
		if err != nil {
			t.Error(err)
			return
		}
		// Startup discovery happens after the run directory exists. A directory
		// at the summary filename forces a portable write failure after results close.
		for _, entry := range entries {
			if entry.IsDir() {
				if err := os.MkdirAll(filepath.Join(out, entry.Name(), "summary.json"), 0700); err != nil {
					t.Error(err)
				}
			}
		}
		w.Header().Set("Content-Type", "application/json")
		fmt.Fprint(w, `{"model_path":"test.gguf","default_generation_settings":{"n_ctx":8192},"modalities":{"vision":false}}`)
	}))
	defer server.Close()
	command := exec.Command(os.Args[0], "-test.run=^TestEvaluationSummaryFailureProcess$")
	command.Dir = dir
	command.Env = append(os.Environ(), "GEKTOR_SUMMARY_FAILURE_TEST=1", "GEKTOR_TEST_PROBES="+probes, "GEKTOR_TEST_RESULTS="+out, "GEKTOR_TEST_BACKEND="+server.URL)
	output, err := command.CombinedOutput()
	var exit *exec.ExitError
	if !errors.As(err, &exit) || exit.ExitCode() != 2 || !strings.Contains(string(output), "write summary:") {
		t.Fatalf("exit=%v output=%s", err, output)
	}
	if _, err := os.Stat(filepath.Join(out, ".eval-lock")); !os.IsNotExist(err) {
		t.Fatalf("run lock was not released: %v", err)
	}
}
