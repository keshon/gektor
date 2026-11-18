package process

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"strings"
	"testing"
	"time"

	"github.com/keshon/gektor/internal/workflow"
	artifactnode "github.com/keshon/gektor/internal/workflow/nodes/artifact"
	"github.com/keshon/gektor/internal/workflow/service"
	"github.com/keshon/gektor/internal/workflow/store"
	"github.com/keshon/gektor/internal/workspace"
)

func TestNodeProcessHelper(t *testing.T) {
	for _, arg := range os.Args {
		if arg == "--node-process-overflow" {
			fmt.Fprint(os.Stdout, strings.Repeat("x", (1<<20)+1))
			os.Exit(0)
		}
		if arg == "--node-process-helper" {
			fmt.Fprint(os.Stdout, "stdout evidence")
			fmt.Fprint(os.Stderr, "stderr evidence")
			os.Exit(9)
		}
	}
}

func TestNodePreservesFailureArtifacts(t *testing.T) {
	ws, err := workspace.New(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	defer ws.Close()
	db, err := store.OpenStore(t.TempDir(), ws.Root())
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	w, err := db.NewRun()
	if err != nil {
		t.Fatal(err)
	}
	defer w.Close()
	exe, _ := os.Executable()
	raw, _ := json.Marshal(input{Program: exe, Args: []string{"-test.run=^TestNodeProcessHelper$", "--", "--node-process-helper"}})
	node := Definition(ws)
	ctx := workflow.WithArtifacts(t.Context(), w.Artifacts("process"))
	out, err := node.Execute(ctx, raw)
	failure := workflow.OperationFailure(err)
	if failure == nil || failure.Code != "process_exit" {
		t.Fatal("nonzero exit lost failure code", out, err)
	}
	var result struct {
		ExitCode       int `json:"exit_code"`
		Stdout, Stderr workflow.Artifact
	}
	if err := json.Unmarshal([]byte(out), &result); err != nil || result.ExitCode != 9 {
		t.Fatal(out, err)
	}
	for id, want := range map[string]string{result.Stdout.ID: "stdout evidence", result.Stderr.ID: "stderr evidence"} {
		_, data, err := w.Artifacts("reader").Read(t.Context(), id)
		if err != nil || string(data) != want {
			t.Fatal(string(data), err)
		}
	}
	if node.RetrySafe {
		t.Fatal("process retries enabled")
	}
	resources, err := node.Resources(raw)
	if err != nil || !strings.HasPrefix(resources[0], exe+" ") {
		t.Fatal(resources, err)
	}
	if err := node.Validate(json.RawMessage(`{"program":"go","workdir":"../outside"}`)); err == nil {
		t.Fatal("outside workdir accepted")
	}
	// A cancelled operation still produces inspectable diagnostics through its
	// independent, bounded persistence context.
	cancelled, cancel := context.WithCancel(ctx)
	cancel()
	start := time.Now()
	out, err = node.Execute(cancelled, raw)
	if !errors.Is(err, context.Canceled) || workflow.OperationFailure(err) != nil || time.Since(start) > 5*time.Second || !strings.Contains(out, `"stdout"`) {
		t.Fatal(out, err)
	}
}

// Failed artifact storage must stop execution even if the process exit is known.
type failingArtifacts struct {
	workflow.ArtifactAccess
	err error
}

func (a failingArtifacts) Put(context.Context, string, io.Reader) (workflow.Artifact, error) {
	return workflow.Artifact{}, a.err
}

func TestProcessFailureDoesNotHideArtifactErrors(t *testing.T) {
	ws, err := workspace.New(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	defer ws.Close()
	exe, err := os.Executable()
	if err != nil {
		t.Fatal(err)
	}
	raw, err := json.Marshal(input{Program: exe, Args: []string{"-test.run=^TestNodeProcessHelper$", "--", "--node-process-helper"}})
	if err != nil {
		t.Fatal(err)
	}
	diskErr := errors.New("artifact disk full")
	ctx := workflow.WithArtifacts(t.Context(), failingArtifacts{err: diskErr})
	node := Definition(ws)

	_, err = node.Execute(ctx, raw)
	if !errors.Is(err, diskErr) || workflow.OperationFailure(err) != nil {
		t.Fatal("artifact error became routable", err)
	}
}

func TestProcessOverflowHasTypedFailure(t *testing.T) {
	ws, err := workspace.New(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	defer ws.Close()
	db, err := store.OpenStore(t.TempDir(), ws.Root())
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	writer, err := db.NewRun()
	if err != nil {
		t.Fatal(err)
	}
	defer writer.Close()
	exe, err := os.Executable()
	if err != nil {
		t.Fatal(err)
	}
	raw, err := json.Marshal(input{Program: exe, Args: []string{"-test.run=^TestNodeProcessHelper$", "--", "--node-process-overflow"}})
	if err != nil {
		t.Fatal(err)
	}
	node := Definition(ws)
	ctx := workflow.WithArtifacts(t.Context(), writer.Artifacts("process"))

	out, err := node.Execute(ctx, raw)
	failure := workflow.OperationFailure(err)
	if failure == nil || failure.Code != "process_output_too_large" || !strings.Contains(out, `"truncated":true`) {
		t.Fatal(out, err)
	}
}

func TestProcessExitRoutesThroughPersistedWorkflow(t *testing.T) {
	ws, err := workspace.New(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	defer ws.Close()
	nodes := append(artifactnode.Definitions(ws), Definition(ws))
	registry, err := workflow.NewRegistry(nodes...)
	if err != nil {
		t.Fatal(err)
	}
	exe, err := os.Executable()
	if err != nil {
		t.Fatal(err)
	}
	args, err := json.Marshal(struct {
		Program string   `json:"program"`
		Args    []string `json:"args"`
	}{Program: exe, Args: []string{"-test.run=^TestNodeProcessHelper$", "--", "--node-process-helper"}})
	if err != nil {
		t.Fatal(err)
	}
	definition := workflow.Definition{Version: 4, Name: "process failure", Steps: []workflow.Step{
		{ID: "process", Tool: "run_program", Args: args},
		{ID: "report", Tool: "artifact_write", OnFailure: "process", Args: json.RawMessage(`{"name":"failure.txt","text":{"$step":"process","pointer":"/error/code"}}`)},
		{ID: "finish", Tool: "artifact_write", Finally: true, Args: json.RawMessage(`{"name":"finished.txt","text":"done"}`)},
	}}
	source, err := json.Marshal(definition)
	if err != nil {
		t.Fatal(err)
	}
	runID := ""
	svc := service.Service{WS: ws, StoreDir: t.TempDir(), Registry: registry,
		OnRun:     func(id string) error { runID = id; return nil },
		Authorize: func(context.Context, string, json.RawMessage) error { return nil },
	}

	out, err := svc.RunSource(t.Context(), source, "process.json", nil)
	failure := workflow.OperationFailure(err)
	if failure == nil || failure.Code != "process_exit" {
		t.Fatal(out, err)
	}
	db, err := store.OpenStore(svc.StoreDir, ws.Root())
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	state, err := db.Run(runID)
	if err != nil {
		t.Fatal(err)
	}
	record := state.Record
	if record.Status != "failed" || len(record.Steps) != 3 || record.Steps[1].Status != "completed" || record.Steps[2].Status != "completed" {
		t.Fatalf("unexpected result: %+v", record)
	}
	attempts := record.Steps[0].Attempts
	if len(attempts) != 1 || attempts[0].Failure == nil || attempts[0].Failure.Code != "process_exit" || !strings.Contains(record.Steps[0].Output, `"stdout"`) {
		t.Fatalf("lost diagnostics: %+v", record.Steps[0])
	}
}
