package tools

import (
	"github.com/keshon/gektor/internal/agent"
	"github.com/keshon/gektor/internal/checks"
	"github.com/keshon/gektor/internal/workspace"
)

// Base returns the standard tool set shared by the main agent and
// subagents. Pass extra tools (ask_user, delegate_task, read_history) for the main
// agent only. File-mutating tools run post-write deterministic checks
// through the supplied reporter; a nil reporter builds unwrapped.
// vision enables reading pictures through read_file; it must come from a
// backend capability probe, not a guess.
func Base(ws *workspace.Workspace, procs *BackgroundProcesses, report *checks.Reporter, vision bool, extra ...agent.Tool) *agent.Registry {
	return agent.NewRegistry(nativeTools(Dependencies{WS: ws, Procs: procs, Checks: report, Vision: vision, Extra: extra}, false)...)
}

// ReadOnly returns a tool set that can inspect the workspace and running
// processes but cannot change anything — no writes, no shell, no process
// control. For workers whose job is to look and report (codebase mapping,
// final review): removing the mutating tools entirely beats prompting a
// weak model not to use them.
func ReadOnly(ws *workspace.Workspace, procs *BackgroundProcesses, vision bool) *agent.Registry {
	return agent.NewRegistry(nativeTools(Dependencies{WS: ws, Procs: procs, Vision: vision}, true)...)
}
