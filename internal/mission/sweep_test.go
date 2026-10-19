package mission

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/keshon/tars/internal/checks"
	"github.com/keshon/tars/internal/llm"
	"github.com/keshon/tars/internal/roles"
	"github.com/keshon/tars/internal/rules"
	"github.com/keshon/tars/internal/tools"
)

// A mission must sweep what it changed, not just what a worker wrote
// through a file tool: shell redirections and MCP writes never appear in
// a post-write scan. The sweep used to run only for api.Session, so
// missions and CLI runs silently skipped it.
func TestRunner_SweepsWhatTheMissionMutated(t *testing.T) {
	ws := testWS(t)
	dir := t.TempDir()
	// Written the way a shell redirection writes it: no tool ever reports
	// this path, so only the deep pass can see it.
	if err := os.WriteFile(filepath.Join(ws.Root(), "stray.go"), []byte("package a\n\nfunc A()  {}\n"), 0o644); err != nil {
		t.Fatal(err)
	}

	var got []checks.Finding
	r := &Runner{
		WS:    ws,
		Dir:   dir,
		Procs: tools.NewBackgroundProcesses(),
		Env: roles.Env{
			WS:     ws,
			Rules:  sweepRules(),
			Checks: checks.NewReporter(sweepRules(), func(_ checks.Scope, f checks.Finding) { got = append(got, f) }),
		},
	}
	m := &Mission{Phase: PhaseDone}
	m.Mutated = []string{"stray.go"}

	if _, err := r.Run(t.Context(), m); err != nil {
		t.Fatalf("Run: %v", err)
	}

	if len(got) != 1 || got[0].Rule != "gofmt" {
		t.Fatalf("mission sweep found %+v, want the gofmt finding in stray.go", got)
	}
}

// The sweep covers every exit path. A mission that fails has usually
// touched more files than one that succeeds, so skipping the sweep there
// would leave exactly the runs worth checking unchecked.
func TestRunner_SweepsWhenTheMissionFails(t *testing.T) {
	ws := testWS(t)
	if err := os.WriteFile(filepath.Join(ws.Root(), "stray.go"), []byte("package a\n\nfunc A()  {}\n"), 0o644); err != nil {
		t.Fatal(err)
	}

	var got []checks.Finding
	r := &Runner{
		WS:    ws,
		Dir:   t.TempDir(),
		Procs: tools.NewBackgroundProcesses(),
		Env: roles.Env{
			WS:     ws,
			Rules:  sweepRules(),
			Checks: checks.NewReporter(sweepRules(), func(_ checks.Scope, f checks.Finding) { got = append(got, f) }),
		},
	}
	m := &Mission{Phase: PhaseFailed}
	m.Mutated = []string{"stray.go"}

	if _, err := r.Run(t.Context(), m); err == nil {
		t.Fatal("a failed mission must still return an error")
	}

	if len(got) != 1 {
		t.Fatalf("failed mission swept %+v, want the sweep to have run", got)
	}
}

// sweepRules is a Go project's rule set: the mission writes a .go file
// and expects the sweep to notice it is not gofmt-clean.
func sweepRules() []checks.Rule { return append(checks.Secrets(), rules.All()...) }

func TestRunnerSweepsFilesCreatedDuringTheMission(t *testing.T) {
	client := &runnerClient{responses: []llm.ChatResponse{text(validPlanJSON), toolCall("list_files", map[string]string{"path": "."}), text("done")}}
	r, _ := newRunner(t, client)
	findings := 0
	r.Env = roles.Env{Client: client, WS: r.WS, Rules: checks.Secrets(), Checks: checks.NewReporter(checks.Secrets(), func(checks.Scope, checks.Finding) { findings++ })}
	r.Env.OnStep = func(_ string, _ int, msg llm.Message) {
		if len(msg.ToolCalls) > 0 {
			if err := os.WriteFile(filepath.Join(r.WS.Root(), "index.html"), []byte("<html>hello</html>\n-----BEGIN PRIVATE KEY-----"), 0600); err != nil {
				t.Fatal(err)
			}
		}
	}
	m := &Mission{ID: "sweep", Task: "make a hello page", Phase: PhasePlan}
	if _, err := r.Run(t.Context(), m); err != nil {
		t.Fatal(err)
	}
	if len(m.Mutated) != 1 || findings != 1 {
		t.Fatalf("mutated=%v findings=%d", m.Mutated, findings)
	}
}
