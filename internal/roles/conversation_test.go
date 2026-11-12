package roles

import (
	"context"
	"regexp"
	"sort"
	"strings"
	"testing"

	"github.com/keshon/gektor/internal/agent"
	"github.com/keshon/gektor/internal/llm"
	"github.com/keshon/gektor/internal/prompts"
	"github.com/keshon/gektor/internal/tools"
	commandrun "github.com/keshon/gektor/internal/tools/command"
)

// Evaluations and frontends must construct the same Interactive tool contract.
func TestInteractive_HasTheToolsThatShip(t *testing.T) {
	a, aErr := Interactive(testEnv(t), "", "", func(context.Context, string) (string, error) { return "", nil }, nil)
	if aErr != nil {
		t.Fatal(aErr)
	}
	for _, want := range []string{"ask_user", "delegate_task", "write_file", "run_shell", "discover_capabilities", "enable_capability"} {
		if !hasTool(a, want) {
			t.Errorf("interactive agent is missing %s — got %v", want, a.ToolNames())
		}
	}
}

// Passing no responder removes the tool rather than offering one that
// cannot be answered.
func TestInteractive_WithoutResponder_HasNoAskUser(t *testing.T) {
	a, aErr := Interactive(testEnv(t), "", "", nil, nil)
	if aErr != nil {
		t.Fatal(aErr)
	}
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

	interactive, interactiveErr := Interactive(e, "", "", func(context.Context, string) (string, error) { return "", nil }, nil)
	if interactiveErr != nil {
		t.Fatal(interactiveErr)
	}
	subagent, subagentErr := Subagent(e, "tester")
	if subagentErr != nil {
		t.Fatal(subagentErr)
	}
	planner, plannerErr := Planner(e, "p", "", func(context.Context, string) (string, error) { return "", nil })
	if plannerErr != nil {
		t.Fatal(plannerErr)
	}
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
	a, aErr := Interactive(e, "", "", nil, nil)
	if aErr != nil {
		t.Fatal(aErr)
	}
	if _, err := a.Run(context.Background(), "task"); err != nil {
		t.Fatalf("Run: %v", err)
	}
	if got := a.Report().MaxSteps; got != 40 {
		t.Fatalf("Report().MaxSteps = %d, want 40", got)
	}

	e2 := testEnv(t)
	e2.Client = textStub{"done"}
	a2, a2Err := Interactive(e2, "", "", nil, nil)
	if a2Err != nil {
		t.Fatal(a2Err)
	}
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
	interactiveAgent, interactiveAgentErr := Interactive(env, "", "", nil, nil)
	if interactiveAgentErr != nil {
		t.Fatal(interactiveAgentErr)
	}
	childAgent, childAgentErr := Subagent(env, "")
	if childAgentErr != nil {
		t.Fatal(childAgentErr)
	}
	for _, a := range []*agent.Agent{interactiveAgent, childAgent} {
		if _, err := a.Run(t.Context(), "Describe the platform"); err != nil {
			t.Fatal(err)
		}
		if !strings.Contains(client.history[0].Content, prompts.PlatformLine(commandrun.ShellName())) {
			t.Fatal("role does not describe the executor's shell")
		}
	}
}

func TestParentRolesKeepInputAndObservationRouting(t *testing.T) {
	cases := []struct {
		name    string
		label   string
		build   func(Env) (*agent.Agent, error)
		unsaved bool
	}{
		{
			name: "interactive", label: "conversation",
			build: func(e Env) (*agent.Agent, error) { return Interactive(e, "conversation", "", nil, nil) },
		},
		{
			name: "planner", label: "plan",
			build: func(e Env) (*agent.Agent, error) { return Planner(e, "plan", "", nil) },
		},
		{
			name: "workflow author", label: "workflow", unsaved: true,
			build: func(e Env) (*agent.Agent, error) { return WorkflowAuthor(e, "recipe.json", "") },
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			e := testEnv(t)
			e.Client = &seqStub{steps: []llm.ChatResponse{
				missingRead("read"),
				{Message: llm.Message{Role: llm.RoleAssistant, Content: "done"}},
			}}
			e.Inbox = &agent.Inbox{}
			err := e.Inbox.Send("operator steering")
			if err != nil {
				t.Fatal(err)
			}

			seen := make(map[string]int)
			e.OnInput = func(msg llm.Message) {
				seen["input"]++
				if msg.Content != "operator steering" {
					t.Errorf("unexpected input: %q", msg.Content)
				}
			}
			e.OnStep = func(label string, _ int, _ llm.Message) {
				seen["step"]++
				if label != tc.label {
					t.Errorf("label = %q, want %q", label, tc.label)
				}
			}
			e.OnResult = func(agent.ToolResult) { seen["result"]++ }
			e.OnToolFinished = func(string) { seen["tool finished"]++ }
			e.OnUsage = func(int, llm.Usage) { seen["usage"]++ }
			e.OnProgress = func(llm.Progress) { seen["progress"]++ }
			e.OnPrompt = func(llm.ContextReport) { seen["prompt"]++ }

			a, err := tc.build(e)
			if err != nil {
				t.Fatal(err)
			}
			_, err = a.Run(t.Context(), "inspect")
			if tc.unsaved {
				if err == nil || !strings.Contains(err.Error(), "did not save a recipe") {
					t.Fatalf("expected unsaved-recipe verification error, got %v", err)
				}
			} else if err != nil {
				t.Fatal(err)
			}

			for _, hook := range []string{"input", "step", "result", "tool finished", "usage", "progress", "prompt"} {
				if seen[hook] == 0 {
					t.Errorf("parent %s hook did not fire", hook)
				}
			}
			if e.Inbox.Len() != 0 {
				t.Fatal("parent left operator input unread")
			}
		})
	}
}
