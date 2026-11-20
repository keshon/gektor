package background

import (
	"encoding/json"
	"path/filepath"
	"runtime"
	"strings"
	"testing"

	tooloutput "github.com/keshon/gektor/internal/tools/output"
)

// longRunningCommand uses the selected platform shell.
var longRunningCommand = func() string {
	if runtime.GOOS == "windows" {
		return "Start-Sleep -Seconds 5"
	}
	return "sleep 5"
}()

func TestCheckBackgroundRequiresValidExplicitCursor(t *testing.T) {
	procs := New()
	procs.procs["bg1"] = &bgProc{output: &tooloutput.Buffer{}}
	for _, args := range []string{`{"id":"bg1"}`, `{"id":"bg1","cursor":-1}`, `{"id":"bg1","cursor":1}`, `{"id":"bg1","cursor":0,"offset":1}`} {
		if _, err := (Check{Procs: procs}).Run(t.Context(), json.RawMessage(args)); err == nil {
			t.Fatalf("accepted an invalid cursor contract: %s", args)
		}
	}
}

func TestCheckBackgroundEnforcesSessionOwnership(t *testing.T) {
	procs := New()
	owner := tooloutput.New(filepath.Join(t.TempDir(), "owner.json"))
	other := tooloutput.New(filepath.Join(t.TempDir(), "other.json"))
	buf := &tooloutput.Buffer{}
	_, err := buf.Write([]byte("private process output"))
	if err != nil {
		t.Fatal(err)
	}
	procs.procs["bg1"] = &bgProc{output: buf, scope: owner.Scope()}
	check := Check{Procs: procs}
	args := json.RawMessage(`{"id":"bg1","cursor":0}`)

	out, err := check.Run(other.Context(t.Context()), args)
	if err == nil || out != "" || !strings.Contains(err.Error(), "another session") {
		t.Fatalf("another session could read process output: %q, %v", out, err)
	}
	out, err = check.Run(owner.Context(t.Context()), args)
	if err != nil || !strings.Contains(out, "private process output") {
		t.Fatalf("owner could not read process output: %q, %v", out, err)
	}
	out, err = check.Run(t.Context(), args)
	if err != nil || !strings.Contains(out, "private process output") {
		t.Fatalf("unscoped caller could not read process output: %q, %v", out, err)
	}
}
