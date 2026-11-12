package background

import (
	"encoding/json"
	"runtime"
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
