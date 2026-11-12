// Package run exposes finite shell and native program execution.
package run

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

type Shell struct {
	WS *workspace.Workspace
}

func (Shell) Name() string { return "run_shell" }

// Mode is Exclusive: a shell command can mutate files, hold a lock or
// depend on ordering, and none of that is visible in the string.
func (Shell) Mode() agent.ToolMode { return agent.Exclusive }
func (Shell) Description() string {
	if runtime.GOOS == "windows" {
		return "Run finite PowerShell source using " + commandrun.ShellName() + ". For builtins, pipelines, redirection and scripts; prefer run_program for native executables. Servers use start_background. Returns whole-command exit_code and combined stdout/stderr."
	}
	return "Run finite POSIX sh source for builtins, pipelines, redirection and scripts; prefer run_program for native executables. Servers use start_background. Returns whole-command exit_code and combined stdout/stderr."
}
func (Shell) Schema() json.RawMessage {
	return json.RawMessage(`{"type":"object","properties":{
		"command":{"type":"string","minLength":1,"description":"Complete PowerShell source on Windows or POSIX sh source elsewhere."},
		"workdir":{"type":"string","description":"Workspace-relative working directory; defaults to root."},"timeout_seconds":{"type":"integer","minimum":1,"maximum":600,"default":600}},"required":["command"],"additionalProperties":false}`)
}

// Shell input is deliberately a single mode: command is always shell source.
// Native programs use their own argument shape.
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

func (t Shell) Run(ctx context.Context, args json.RawMessage) (string, error) {
	return t.RunStreaming(ctx, args, nil)
}

func (t Shell) RunStreaming(ctx context.Context, args json.RawMessage, onOutput func(string)) (string, error) {
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

// Program starts a native executable directly, without a shell. Keeping it
// separate from run_shell gives both tools a single, unambiguous input shape:
// run_shell has command, run_program has program + args.
type Program struct {
	WS *workspace.Workspace
}

func (Program) Name() string { return "run_program" }

// A direct process can mutate files, hold a lock or depend on ordering.
func (Program) Mode() agent.ToolMode { return agent.Exclusive }
func (Program) Description() string {
	if runtime.GOOS == "windows" {
		return "Run a finite native executable without a shell. Pass literal args, without quoting or expansion. Use run_shell for PowerShell syntax/scripts. Returns exit_code and combined stdout/stderr; nonzero exits are failures."
	}
	return "Run a finite native executable without a shell. Pass literal args, without quoting or expansion. Use run_shell for POSIX sh syntax/scripts. Returns exit_code and combined stdout/stderr; nonzero exits are failures."
}
func (Program) Schema() json.RawMessage {
	return json.RawMessage(`{"type":"object","properties":{
		"program":{"type":"string","minLength":1,"description":"Native executable name or path, without shell quoting."},
		"args":{"type":"array","items":{"type":"string"},"description":"Literal arguments passed verbatim; no shell expansion."},
		"workdir":{"type":"string","description":"Workspace-relative working directory; defaults to root."},"timeout_seconds":{"type":"integer","minimum":1,"maximum":600,"default":600}},"required":["program"],"additionalProperties":false}`)
}

// Native arguments are passed to the OS verbatim, never joined into shell source.
type programInput struct {
	Program        string   `json:"program"`
	Args           []string `json:"args"`
	Workdir        string   `json:"workdir"`
	TimeoutSeconds *int     `json:"timeout_seconds"`
}

func (in programInput) validate() error {
	if strings.TrimSpace(in.Program) == "" {
		return fmt.Errorf("program is required")
	}
	return nil
}

func (t Program) Run(ctx context.Context, args json.RawMessage) (string, error) {
	return t.RunStreaming(ctx, args, nil)
}

func (t Program) RunStreaming(ctx context.Context, args json.RawMessage, onOutput func(string)) (string, error) {
	var in programInput
	if err := json.Unmarshal(args, &in); err != nil {
		return "", fmt.Errorf("bad arguments: %w", err)
	}
	if err := agent.RejectUnknownFields(args, "program", "args", "workdir", "timeout_seconds"); err != nil {
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
	cmd, err := commandrun.Native(ctx, in.Program, in.Args)
	if err != nil {
		return "", err
	}
	return commandrun.Result(ctx, cmd, dir, onOutput)
}
