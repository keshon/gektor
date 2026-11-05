package tools

import (
	"encoding/json"
	"path/filepath"

	"github.com/keshon/gektor/internal/agent"
	"github.com/keshon/gektor/internal/checks"
	"github.com/keshon/gektor/internal/workspace"
)

// PathResources gives permissions the same relative paths used by filesystem
// access. In particular, an absolute spelling cannot bypass a relative rule,
// and a move must be authorized for its destination as well as its source.
func PathResources(ws *workspace.Workspace, tool string, args json.RawMessage) ([]string, error) {
	if ws == nil {
		return nil, nil
	}
	var in struct{ Path, From, To string }
	var paths []string
	switch tool {
	case "read_file", "write_file", "patch_file", "list_files", "grep_files", "git", "git_add":
		if err := json.Unmarshal(args, &in); err != nil {
			return nil, err
		}
		paths = []string{in.Path}
	case "move_file":
		if err := json.Unmarshal(args, &in); err != nil {
			return nil, err
		}
		paths = []string{in.From, in.To}
	default:
		return nil, nil
	}
	for i, path := range paths {
		rel, err := ws.Relative(path)
		if err != nil {
			return nil, err
		}
		paths[i] = filepath.ToSlash(rel)
	}
	return paths, nil
}

// Base returns the standard tool set shared by the main agent and
// subagents. Pass extra tools (ask_user, delegate_task, read_history) for the main
// agent only. File-mutating tools run post-write deterministic checks
// from rules, reporting to sink; a nil sink or no rules builds unwrapped.
// vision enables reading pictures through read_file; it must come from a
// backend capability probe, not a guess.
func Base(ws *workspace.Workspace, procs *BackgroundProcesses, report FindingSink, rules []checks.Rule, vision bool, extra ...agent.Tool) *agent.Registry {
	return agent.NewRegistry(nativeTools(packageDependencies{ws, procs, report, rules, vision, extra}, false)...)
}

// ReadOnly returns a tool set that can inspect the workspace and running
// processes but cannot change anything — no writes, no shell, no process
// control. For workers whose job is to look and report (codebase mapping,
// final review): removing the mutating tools entirely beats prompting a
// weak model not to use them.
func ReadOnly(ws *workspace.Workspace, procs *BackgroundProcesses, vision bool) *agent.Registry {
	return agent.NewRegistry(nativeTools(packageDependencies{WS: ws, Procs: procs, Vision: vision}, true)...)
}
