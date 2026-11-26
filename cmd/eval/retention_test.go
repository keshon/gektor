package main

import (
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

func TestRetainedWorkspaceSeparatesAcceptanceChanges(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		fmt.Fprint(w, `{"choices":[{"message":{"role":"assistant","content":"done"},"finish_reason":"stop"}]}`)
	}))
	defer server.Close()
	seed := t.TempDir()
	err := os.WriteFile(filepath.Join(seed, "model.txt"), []byte("model work"), 0600)
	if err != nil {
		t.Fatal(err)
	}
	err = os.WriteFile(filepath.Join(seed, "artifact.go"), []byte("package artifact\n"), 0600)
	if err != nil {
		t.Fatal(err)
	}
	probe := Probe{
		Name: "retention", Task: "say done", Seed: seed,
		Verify: []Check{{Type: "shell", Cmd: "echo acceptance > acceptance.txt"}},
	}
	runDir := t.TempDir()
	result := runOnce(t.Context(), probe, 1, runDir, "llama", server.URL, "local", 131072, 1024, false, "", "", false, "baseline", true)
	if !result.Passed || result.Errored || result.ModelWorkspace == "" || result.Workspace == "" {
		t.Fatalf("retained run: %+v", result)
	}
	for _, directory := range []string{result.Workspace, result.ModelWorkspace} {
		body, err := os.ReadFile(filepath.Join(directory, "model.txt"))
		if err != nil || string(body) != "model work" {
			t.Fatalf("model evidence missing from %s: %q, %v", directory, body, err)
		}
		relative, err := filepath.Rel(runDir, directory)
		if err != nil || strings.HasPrefix(relative, "..") {
			t.Fatalf("retained directory outside result directory: %s", directory)
		}
		if !strings.HasPrefix(filepath.ToSlash(relative), "_workspaces/") {
			t.Fatalf("retained source is exposed to recursive Go package discovery: %s", relative)
		}
	}
	_, err = os.Stat(filepath.Join(result.Workspace, "acceptance.txt"))
	if err != nil {
		t.Fatal("acceptance did not run", err)
	}
	_, err = os.Stat(filepath.Join(result.ModelWorkspace, "acceptance.txt"))
	if !os.IsNotExist(err) {
		t.Fatal("acceptance contaminated model snapshot", err)
	}

	// Retained source without its own module must not join the surrounding one.
	for path, body := range map[string]string{
		"go.mod":  "module example.com/evidence\n\ngo 1.26\n",
		"root.go": "package evidence\n",
	} {
		err = os.WriteFile(filepath.Join(runDir, path), []byte(body), 0600)
		if err != nil {
			t.Fatal(err)
		}
	}
	command := exec.CommandContext(t.Context(), "go", "list", "./...")
	command.Dir = runDir
	command.Env = append(os.Environ(), "GOWORK=off")
	output, err := command.CombinedOutput()
	if err != nil || strings.TrimSpace(string(output)) != "example.com/evidence" {
		t.Fatalf("Go discovered retained model source: %s, %v", output, err)
	}
}

func TestTraceCreationFailureStopsBeforeModelCall(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		t.Errorf("unexpected backend request: %s", r.URL.Path)
		w.WriteHeader(http.StatusInternalServerError)
	}))
	defer server.Close()
	runDir := t.TempDir()
	err := os.Mkdir(filepath.Join(runDir, "blocked-run1.log"), 0700)
	if err != nil {
		t.Fatal(err)
	}
	result := runOnce(t.Context(), Probe{Name: "blocked", Task: "say done"}, 1, runDir, "llama", server.URL, "local", 131072, 1024, false, "", "", false, "baseline", false)
	if !result.Errored || result.Passed || !strings.HasPrefix(result.RunError, "create trace:") || result.ModelCalls != 0 || result.Trace != "" {
		t.Fatalf("trace failure was not an infrastructure error: %+v", result)
	}
}

func TestSnapshotFailurePreservesModelUsageAndSolverError(t *testing.T) {
	for _, solverFails := range []bool{false, true} {
		t.Run(fmt.Sprintf("solver_failure=%t", solverFails), func(t *testing.T) {
			runDir := t.TempDir()
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				roots, err := filepath.Glob(filepath.Join(runDir, "_workspaces", "gektor-eval-*"))
				if err != nil || len(roots) != 1 {
					t.Errorf("locate retained run root: %v, %v", roots, err)
					w.WriteHeader(http.StatusInternalServerError)
					return
				}
				err = os.WriteFile(filepath.Join(roots[0], "model-workspace"), []byte("blocks snapshot directory"), 0600)
				if err != nil {
					t.Error(err)
					w.WriteHeader(http.StatusInternalServerError)
					return
				}
				w.Header().Set("Content-Type", "application/json")
				if solverFails {
					w.WriteHeader(http.StatusBadRequest)
					fmt.Fprint(w, `{"error":{"message":"solver rejected input"}}`)
					return
				}
				fmt.Fprint(w, `{"choices":[{"message":{"role":"assistant","content":"done"},"finish_reason":"stop"}],"usage":{"prompt_tokens":101,"completion_tokens":7}}`)
			}))
			defer server.Close()
			probe := Probe{Name: "snapshot-failure", Task: "say done"}
			result := runOnce(t.Context(), probe, 1, runDir, "llama", server.URL, "local", 131072, 1024, false, "", "", false, "baseline", true)
			if !result.Errored || result.Passed || !strings.Contains(result.RunError, "snapshot model workspace:") || result.ModelWorkspace != "" {
				t.Fatalf("snapshot failure was not recorded: %+v", result)
			}
			if result.ModelCalls != 1 || len(result.Requests) != 1 {
				t.Fatalf("lost model calls after snapshot failure: %+v", result)
			}
			if solverFails {
				if !strings.Contains(result.RunError, "solver rejected input") {
					t.Fatal("snapshot failure replaced solver error", result.RunError)
				}
				return
			}
			if result.PromptTokens != 101 || result.CompletionTokens != 7 || result.PeakPromptTokens != 101 {
				t.Fatalf("lost usage after snapshot failure: %+v", result)
			}
		})
	}
}
