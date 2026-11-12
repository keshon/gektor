package roles

import (
	"fmt"

	"github.com/keshon/gektor/internal/agent"
	"github.com/keshon/gektor/internal/config"
	"github.com/keshon/gektor/internal/conversation"
	"github.com/keshon/gektor/internal/prompts"
	commandrun "github.com/keshon/gektor/internal/tools/command"
	toolhistory "github.com/keshon/gektor/internal/tools/history"
)

const (
	// subagentSteps bounds one delegated unit independently of the parent's
	// limit, including when the parent has an unlimited budget.
	subagentSteps = config.DefaultDelegateSteps
)

// Subagent is what delegate_task spawns. It gets the full tool set minus
// ask_user and delegate_task: no subagent can spawn subagents, and none
// can block the process waiting on a human who is watching the parent.
// The inherited System text names both withheld tools, so the scope
// correction is composed here, where the restricted registry is chosen.
// Parent observation hooks remain muted: child steps and usage must never
// appear as parent evidence. Interactive delegation adds a separate child event
// route and archive. Gates and workspace findings still constrain shared work.
func Subagent(e Env, role string) (*agent.Agent, error) { return inspectedSubagent(e, role, "", "") }

func inspectedSubagent(e Env, role, callID, parentState string) (*agent.Agent, error) {
	platform := prompts.PlatformLine(commandrun.ShellName())
	system := prompts.WithRole(role) + "\n\n" + prompts.SubagentScope + "\n\n" + platform
	cfg := e.executionConfig(system, "")
	cfg.StateFile = conversation.ChildStatePath(parentState, callID)
	extra := append([]agent.Tool{toolhistory.Tool{Path: cfg.StateFile}}, e.MCPTools...)
	err := e.configureTools(&cfg, false, false, extra...)
	if err != nil {
		return nil, err
	}

	steps := e.DelegateSteps
	if steps <= 0 {
		steps = subagentSteps
	}
	cfg.MaxSteps = steps
	cfg.System += fmt.Sprintf("\n\nYour budget is %d model steps, including your final answer. Keep the subtask focused and report unfinished work honestly.", steps)

	if callID != "" && e.ChildEvents != nil {
		observeSubagent(&cfg, e, callID)
	}

	return agent.New(cfg), nil
}
