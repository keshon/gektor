package roles

import (
	"context"

	"github.com/keshon/gektor/internal/agent"
	"github.com/keshon/gektor/internal/prompts"
	"github.com/keshon/gektor/internal/tools"
)

// Planner is the read-only conversation. It deliberately shares
// Interactive's system prompt and tool set: a narrower registry would change
// the prompt prefix, and a backend that caches prompts would re-read the
// whole conversation every time the operator switches modes. Capability is
// removed where it is used - the loop refuses every call outside
// tools.ReadOnly - rather than by asking the model to hold back, so nothing
// can write in this role whatever the model decides to try.
func Planner(e Env, label, stateFile string, ask func(ctx context.Context, question string) (string, error)) *agent.Agent {
	cfg := e.conversationConfig(label, stateFile, ask, nil)
	cfg.ReadOnly = tools.ReadOnly(e.WS, e.Procs, e.Vision).Names()
	cfg.AllowReadOnlyExtensions = true
	cfg.Directive = prompts.PlanMode
	cfg.Supersedes = prompts.ActMode
	return agent.New(cfg)
}

// Interactive is the top-level agent a person talks to: the full tool set
// plus ask_user and delegate_task.
//
// ask performs the blocking question. Passing nil removes ask_user from
// the tool set entirely, which changes what the agent can do — an
// unattended caller should usually pass a responder that says plainly no
// human is available rather than dropping the tool, so that the agent
// under test has the same tools as the one that ships and reaching for
// ask_user stays visible in the trace.
//
// verify, if set, runs the final build/test command without another model turn.
func Interactive(e Env, label, stateFile string,
	ask func(ctx context.Context, question string) (string, error),
	verify func(ctx context.Context) (string, error)) *agent.Agent {
	cfg := e.conversationConfig(label, stateFile, ask, verify)
	cfg.Directive = prompts.ActMode
	cfg.Supersedes = prompts.PlanMode
	return agent.New(cfg)
}

// conversationConfig is everything Interactive and Planner share, which is
// everything except the stance. One builder keeps their prompt prefix
// identical by construction: the tests assert it, because a divergence there
// is invisible until the operator pays for a full re-read.
func (e Env) conversationConfig(label, stateFile string,
	ask func(ctx context.Context, question string) (string, error),
	verify func(ctx context.Context) (string, error)) agent.Config {
	if e.Outputs == nil {
		e.Outputs = tools.NewOutputStore(e.statePath(stateFile))
	}
	extra := []agent.Tool{&tools.Delegate{DefaultSteps: e.DelegateSteps, MaxSteps: e.DelegateMaxSteps, Spawn: func(ctx context.Context, role string, steps int) *agent.Agent {
		childEnv := e
		childEnv.DelegateSteps = steps
		return inspectedSubagent(childEnv, role, agent.ToolCallID(ctx), e.statePath(stateFile))
	}}}
	extra = append(extra, e.MCPTools...)
	extra = append(extra, tools.ReadHistory{Path: e.statePath(stateFile)})
	if ask != nil {
		extra = append(extra, tools.AskUser{AskFn: ask})
	}
	cfg := e.agentConfig(prompts.System+"\n\n"+prompts.PlatformLine(tools.ShellName()), label, stateFile)
	e.configureTools(&cfg, true, ask != nil, extra...)
	cfg.MaxSteps = e.MaxSteps
	if e.Browser != nil {
		cfg.SaveBrowser = e.Browser.Reference
		cfg.RestoreBrowser = e.Browser.SelectProfile
	}
	cfg.RunTimeout = e.RunTimeout
	cfg.Verify = verify
	cfg.Stream, cfg.OnDelta, cfg.OnReasoning = e.Stream, e.OnDelta, e.OnReasoning
	return cfg
}
