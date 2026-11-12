package shell

import (
	"context"
	"encoding/json"
	"fmt"
	"runtime"
	"strings"

	"github.com/keshon/gektor/internal/agent"
	commandrun "github.com/keshon/gektor/internal/tools/command"
	"github.com/keshon/gektor/internal/workspace"
)

type Tool struct {
	WS *workspace.Workspace
}

func (Tool) Name() string { return "run_shell" }

// Mode is Exclusive: a shell command can mutate files, hold a lock or
// depend on ordering, and none of that is visible in the string.
func (Tool) Mode() agent.ToolMode { return agent.Exclusive }
func (Tool) Description() string {
	if runtime.GOOS == "windows" {
		return "Run finite PowerShell source using " + commandrun.ShellName() + ". For builtins, pipelines, redirection and scripts; prefer run_program for native executables. Servers use start_background. Returns whole-command exit_code and combined stdout/stderr."
	}
	return "Run finite POSIX sh source for builtins, pipelines, redirection and scripts; prefer run_program for native executables. Servers use start_background. Returns whole-command exit_code and combined stdout/stderr."
}
func (Tool) Schema() json.RawMessage {
	return json.RawMessage(`{"type":"object","properties":{
		"command":{"type":"string","minLength":1,"description":"Complete PowerShell source on Windows or POSIX sh source elsewhere."},
		"workdir":{"type":"string","description":"Workspace-relative working directory; defaults to root."},"timeout_seconds":{"type":"integer","minimum":1,"maximum":600,"default":600}},"required":["command"],"additionalProperties":false}`)
}

// Shell input is deliberately a single mode: command is always shell source.
// Native processes use programInput and the separate run_program/start_program
// tools, so neither schema needs a top-level oneOf.
type shellInput struct {
	Command        string `json:"command"`
	Workdir        string `json:"workdir"`
	TimeoutSeconds *int   `json:"timeout_seconds"`
}

func (in shellInput) validate() error {
	if strings.TrimSpace(in.Command) == "" {
		return fmt.Errorf("command is required")
	}
	return nil
}

func (t Tool) Run(ctx context.Context, args json.RawMessage) (string, error) {
	return t.RunStreaming(ctx, args, nil)
}

func (t Tool) RunStreaming(ctx context.Context, args json.RawMessage, onOutput func(string)) (string, error) {
	var in shellInput
	if err := json.Unmarshal(args, &in); err != nil {
		return "", fmt.Errorf("bad arguments: %w", err)
	}

	if err := agent.RejectUnknownFields(args, "command", "workdir", "timeout_seconds"); err != nil {
		return "", err
	}
	if err := in.validate(); err != nil {
		return "", err
	}
	timeout, err := commandrun.Timeout(in.TimeoutSeconds)
	if err != nil {
		return "", err
	}
	dir, err := t.WS.Resolve(in.Workdir)
	if err != nil {
		return "", err
	}
	ctx, cancel := context.WithTimeout(ctx, timeout)
	defer cancel()
	return commandrun.Result(ctx, commandrun.Shell(ctx, in.Command), dir, onOutput)
}
