package process

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"strings"
	"testing"
	"time"

	"github.com/keshon/gektor/internal/workflow"
	"github.com/keshon/gektor/internal/workflow/store"
	"github.com/keshon/gektor/internal/workspace"
)

func TestNodeProcessHelper(t *testing.T) {
	for _, arg := range os.Args {
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
	if err == nil {
		t.Fatal("nonzero exit accepted")
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
	if err == nil || time.Since(start) > 5*time.Second || !strings.Contains(out, `"stdout"`) {
		t.Fatal(out, err)
	}
}
