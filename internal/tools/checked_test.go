package tools

import (
	"context"
	"encoding/json"
	"errors"
	"strings"
	"testing"

	"github.com/keshon/gektor/internal/agent"
	"github.com/keshon/gektor/internal/checks"
	tooledit "github.com/keshon/gektor/internal/tools/edit"
	"github.com/keshon/gektor/internal/workspace"
)

type stubWrite struct {
	out string
	err error
}

func (stubWrite) Name() string        { return "write_file" }
func (stubWrite) Description() string { return "stub" }
func (stubWrite) Schema() json.RawMessage {
	return json.RawMessage(`{}`)
}
func (stubWrite) Mode() agent.ToolMode { return agent.Exclusive }
func (s stubWrite) Run(context.Context, json.RawMessage) (string, error) {
	return s.out, s.err
}

func checkedWS(t *testing.T) *workspace.Workspace {
	t.Helper()
	ws, err := workspace.New(t.TempDir())
	if ws != nil {
		t.Cleanup(func() { ws.Close() })
	}
	if err != nil {
		t.Fatal(err)
	}
	return ws
}

func TestChecked_FiresOnSecretWrite(t *testing.T) {
	ws := checkedWS(t)
	var got []string
	sink := func(_ checks.Scope, f checks.Finding) {
		got = append(got, f.Rule+" "+f.Path)
	}
	tool := Checked(tooledit.Write{WS: ws}, ws, checks.NewReporter(checks.Secrets(), sink))
	out, err := tool.Run(context.Background(), json.RawMessage(`{"path":"a.go","content":"AKIAIOSFODNN7EXAMPLE\n"}`))
	if err != nil {
		t.Fatalf("run: %v", err)
	}
	if !strings.Contains(out, "wrote a.go") {
		t.Fatalf("result altered: %q", out)
	}
	if len(got) != 1 || got[0] != "secret-aws a.go" {
		t.Fatalf("findings = %v", got)
	}
}

func TestCheckedDoesNotChooseLanguageChecks(t *testing.T) {
	ws := checkedWS(t)
	t.Setenv("PATH", "")
	reporter := checks.NewReporter(checks.Secrets(), func(checks.Scope, checks.Finding) {
		t.Fatal("language syntax produced a built-in finding")
	})
	tool := Checked(tooledit.Write{WS: ws}, ws, reporter)
	for _, path := range []string{"broken.go", "broken.py", "broken.rs"} {
		args, _ := json.Marshal(map[string]string{"path": path, "content": "invalid source {{{"})
		out, err := tool.Run(t.Context(), args)
		if err != nil || out != "wrote "+path+" (18 bytes)" {
			t.Fatalf("unexpected language-specific result: %q, %v", out, err)
		}
	}
}

func TestChecked_CleanIsSilent(t *testing.T) {
	ws := checkedWS(t)
	fired := false
	tool := Checked(tooledit.Write{WS: ws}, ws, checks.NewReporter(checks.Secrets(), func(checks.Scope, checks.Finding) { fired = true }))
	if _, err := tool.Run(context.Background(), json.RawMessage(`{"path":"a.go","content":"package a\n"}`)); err != nil {
		t.Fatalf("run: %v", err)
	}
	if fired {
		t.Fatal("clean write must not report")
	}
}

func TestChecked_FailOpen(t *testing.T) {
	ws := checkedWS(t)
	fired := false
	sink := func(checks.Scope, checks.Finding) { fired = true }
	// Tool error: result passes through, no scan.
	broken := Checked(stubWrite{err: errCheckedBoom}, ws, checks.NewReporter(checks.Secrets(), sink))
	if _, err := broken.Run(context.Background(), json.RawMessage(`{"path":"a.go"}`)); err == nil {
		t.Fatal("inner error must pass through")
	}
	// Unparseable args: passthrough.
	ok := Checked(stubWrite{out: "ok"}, ws, checks.NewReporter(checks.Secrets(), sink))
	if _, err := ok.Run(context.Background(), json.RawMessage(`{oops`)); err != nil {
		t.Fatalf("bad args must pass through: %v", err)
	}
	// Missing file post-write: passthrough (deleted between write and scan).
	if _, err := ok.Run(context.Background(), json.RawMessage(`{"path":"nope.go"}`)); err != nil {
		t.Fatalf("missing file must pass through: %v", err)
	}
	// Nil sink or workspace: unwrapped behavior, no scan possible.
	plain := Checked(stubWrite{out: "ok"}, ws, nil)
	if out, err := plain.Run(context.Background(), json.RawMessage(`{}`)); out != "ok" || err != nil {
		t.Fatalf("nil sink changed behavior: %q %v", out, err)
	}
	if fired {
		t.Fatal("nothing may report on these paths")
	}
}

var errCheckedBoom = errors.New("boom")
