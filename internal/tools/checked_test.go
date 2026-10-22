package tools

import (
	"context"
	"encoding/json"
	"errors"
	"strings"
	"testing"

	"github.com/keshon/gektor/internal/agent"
	"github.com/keshon/gektor/internal/checks"
	"github.com/keshon/gektor/internal/rules"
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

// checkedRules is a Go project's rule set. The wrapper no longer knows
// what a .go file is, so a test that wants a gofmt finding has to supply
// the rule that produces one.
func checkedRules() []checks.Rule { return append(checks.Secrets(), rules.All()...) }

func TestChecked_FiresOnDirtyWrite(t *testing.T) {
	ws := checkedWS(t)
	var got []string
	sink := func(rule, path string, line int, summary string) {
		got = append(got, rule+" "+path)
	}
	tool := Checked(WriteFile{WS: ws}, ws, sink, checkedRules())
	out, err := tool.Run(context.Background(), json.RawMessage(`{"path":"a.go","content":"package a\n\nfunc A()  {}\n"}`))
	if err != nil {
		t.Fatalf("run: %v", err)
	}
	if !strings.Contains(out, "wrote a.go") {
		t.Fatalf("result altered: %q", out)
	}
	if len(got) != 1 || got[0] != "gofmt a.go" {
		t.Fatalf("findings = %v", got)
	}
}

func TestChecked_CleanIsSilent(t *testing.T) {
	ws := checkedWS(t)
	fired := false
	tool := Checked(WriteFile{WS: ws}, ws, func(string, string, int, string) { fired = true }, checkedRules())
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
	sink := func(string, string, int, string) { fired = true }
	// Tool error: result passes through, no scan.
	broken := Checked(stubWrite{err: errCheckedBoom}, ws, sink, checkedRules())
	if _, err := broken.Run(context.Background(), json.RawMessage(`{"path":"a.go"}`)); err == nil {
		t.Fatal("inner error must pass through")
	}
	// Unparseable args: passthrough.
	ok := Checked(stubWrite{out: "ok"}, ws, sink, checkedRules())
	if _, err := ok.Run(context.Background(), json.RawMessage(`{oops`)); err != nil {
		t.Fatalf("bad args must pass through: %v", err)
	}
	// Missing file post-write: passthrough (deleted between write and scan).
	if _, err := ok.Run(context.Background(), json.RawMessage(`{"path":"nope.go"}`)); err != nil {
		t.Fatalf("missing file must pass through: %v", err)
	}
	// Nil sink or workspace: unwrapped behavior, no scan possible.
	plain := Checked(stubWrite{out: "ok"}, ws, nil, checkedRules())
	if out, err := plain.Run(context.Background(), json.RawMessage(`{}`)); out != "ok" || err != nil {
		t.Fatalf("nil sink changed behavior: %q %v", out, err)
	}
	if fired {
		t.Fatal("nothing may report on these paths")
	}
}

var errCheckedBoom = errors.New("boom")
