package roles

import (
	"context"
	"regexp"
	"sort"
	"strings"
	"testing"

	"github.com/keshon/gektor/internal/agent"
	"github.com/keshon/gektor/internal/prompts"
	"github.com/keshon/gektor/internal/tools"
)

// Evaluations and frontends must construct the same Interactive tool contract.
func TestInteractive_HasTheToolsThatShip(t *testing.T) {
	a := Interactive(testEnv(t), "", "", func(context.Context, string) (string, error) { return "", nil }, nil)
	for _, want := range []string{"ask_user", "delegate_task", "write_file", "run_shell", "discover_capabilities", "enable_capability"} {
		if !hasTool(a, want) {
			t.Errorf("interactive agent is missing %s — got %v", want, a.ToolNames())
		}
	}
}

// Passing no responder removes the tool rather than offering one that
// cannot be answered.
func TestInteractive_WithoutResponder_HasNoAskUser(t *testing.T) {
	a := Interactive(testEnv(t), "", "", nil, nil)
	if hasTool(a, "ask_user") {
		t.Error("ask_user offered with no responder to answer it")
	}
	if !hasTool(a, "delegate_task") {
		t.Error("delegate_task should not depend on the ask responder")
	}
}

// A system text that names a tool promises that tool exists: every
// static tool-name token in a role's prompt must resolve in that role's
// registry, or a weak model emits calls to tools it was never given
// ("unknown tool", burned steps). Nudge and task texts are excluded on
// purpose — they are role-agnostic by design, and the loop conditions
// their tool-specific clauses on its own registry (WriteHint) instead of
// promising statically. The common prompt also makes optional tools conditional.
func TestRoleSystemToolsResolve(t *testing.T) {
	e := testEnv(t)
	universe := map[string]bool{"ask_user": true, "delegate_task": true}
	for _, n := range tools.Base(e.WS, e.Procs, nil, false).Names() {
		universe[n] = true
	}
	for _, n := range tools.ReadOnly(e.WS, e.Procs, false).Names() {
		universe[n] = true
	}
	var alts []string
	for n := range universe {
		alts = append(alts, regexp.QuoteMeta(n))
	}
	sort.Strings(alts)
	mentioned := regexp.MustCompile(`\b(` + strings.Join(alts, "|") + `)\b`)

	interactive := Interactive(e, "", "", func(context.Context, string) (string, error) { return "", nil }, nil)
	subagent := Subagent(e, "tester")
	planner := Planner(e, "p", "", func(context.Context, string) (string, error) { return "", nil })
	// System texts as their roles actually ship them: the subagent
	// composition includes its scope correction (mirrors Subagent).
	subagentSystem := prompts.WithRole("tester") + "\n\n" + prompts.SubagentScope
	cases := []struct {
		role  string
		texts []string
		names []string
	}{
		{"interactive", []string{prompts.System}, interactive.ToolNames()},
		{"subagent", []string{subagentSystem}, subagent.ToolNames()},
		{"planner", []string{prompts.System, prompts.PlanMode}, planner.ToolNames()},
	}
	for _, c := range cases {
		have := map[string]bool{}
		for _, n := range c.names {
			have[n] = true
		}
		for _, text := range c.texts {
			for _, m := range mentioned.FindAllString(text, -1) {
				// A withheld tool may be named only to forbid it
				// ("no <tool>"): a promise and a prohibition read the
				// same to a regex but opposite to a model.
				if !have[m] && !strings.Contains(text, "no "+m) {
					t.Errorf("%s prompt names %q, missing from its registry", c.role, m)
				}
			}
		}
	}
}

func TestInteractive_HonorsEnvMaxSteps(t *testing.T) {
	e := testEnv(t)
	e.Client = textStub{"done"}
	e.MaxSteps = 40
	a := Interactive(e, "", "", nil, nil)
	if _, err := a.Run(context.Background(), "task"); err != nil {
		t.Fatalf("Run: %v", err)
	}
	if got := a.Report().MaxSteps; got != 40 {
		t.Fatalf("Report().MaxSteps = %d, want 40", got)
	}

	e2 := testEnv(t)
	e2.Client = textStub{"done"}
	a2 := Interactive(e2, "", "", nil, nil)
	if _, err := a2.Run(context.Background(), "task"); err != nil {
		t.Fatalf("Run: %v", err)
	}
	if got := a2.Report().MaxSteps; got != agent.DefaultMaxSteps {
		t.Fatalf("Report().MaxSteps = %d, want default %d", got, agent.DefaultMaxSteps)
	}
}

func TestRolesDescribeTheSelectedShell(t *testing.T) {
	env := testEnv(t)
	client := &subagentBudgetClient{}
	env.Client = client
	for _, a := range []*agent.Agent{Interactive(env, "", "", nil, nil), Subagent(env, "")} {
		if _, err := a.Run(t.Context(), "Describe the platform"); err != nil {
			t.Fatal(err)
		}
		if !strings.Contains(client.history[0].Content, prompts.PlatformLine(tools.ShellName())) {
			t.Fatal("role does not describe the executor's shell")
		}
	}
}
