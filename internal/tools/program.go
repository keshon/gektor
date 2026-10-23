package tools

import (
	"context"
	"encoding/json"
	"fmt"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"

	"github.com/keshon/gektor/internal/agent"
	"github.com/keshon/gektor/internal/workspace"
)

// RunProgram starts a native executable directly, without a shell. Keeping it
// separate from run_shell gives both tools a single, unambiguous input shape:
// run_shell has command, run_program has program + args.
type RunProgram struct {
	WS *workspace.Workspace
}

func (RunProgram) Name() string { return "run_program" }

// A direct process can mutate files, hold a lock or depend on ordering.
func (RunProgram) Mode() agent.ToolMode { return agent.Exclusive }
func (RunProgram) Description() string {
	if runtime.GOOS == "windows" {
		return "Run a finite native executable inside the workspace without a shell. " +
			"Prefer this for go, git, python and other native programs. args are separate literal arguments, without shell quoting or expansion. " +
			"Use run_shell for PowerShell syntax, builtins, pipelines, redirection or scripts. " +
			"Use workdir for a workspace-relative working directory and timeout_seconds (1–600) to bound execution. " +
			"Returns exit_code and combined stdout/stderr. Nonzero exits remain failures, including expected negative-test results."
	}
	return "Run a finite native executable inside the workspace without a shell. " +
		"Prefer this for go, git, python and other native programs. args are separate literal arguments, without shell quoting or expansion. " +
		"Use run_shell for POSIX sh syntax, builtins, pipelines or redirection. " +
		"Use workdir for a workspace-relative working directory and timeout_seconds (1–600) to bound execution. " +
		"Returns exit_code and combined stdout/stderr. Nonzero exits remain failures, including expected negative-test results."
}
func (RunProgram) Schema() json.RawMessage {
	return programSchema(true)
}

// programInput is shared by run_program and start_program. args are passed to
// the OS verbatim, never joined into a command line by us.
type programInput struct {
	Program        string   `json:"program"`
	Args           []string `json:"args"`
	Workdir        string   `json:"workdir"`
	TimeoutSeconds *int     `json:"timeout_seconds"`
}

func programSchema(timeout bool) json.RawMessage {
	extra := ""
	if timeout {
		extra = `,"timeout_seconds":{"type":"integer","minimum":1,"maximum":600,"default":600}`
	}
	return json.RawMessage(`{"type":"object","properties":{
		"program":{"type":"string","minLength":1,"description":"Native executable name or path, without shell quoting."},
		"args":{"type":"array","items":{"type":"string"},"description":"Literal arguments passed verbatim; no shell expansion."},
		"workdir":{"type":"string","description":"Workspace-relative working directory; defaults to root."}` + extra +
		`},"required":["program"],"additionalProperties":false}`)
}

func (in programInput) validate() error {
	if strings.TrimSpace(in.Program) == "" {
		return fmt.Errorf("program is required")
	}
	return nil
}

func (in programInput) cmd(ctx context.Context) (*exec.Cmd, error) {
	cmd := exec.CommandContext(ctx, in.Program, in.Args...)
	if ext := strings.ToLower(filepath.Ext(cmd.Path)); runtime.GOOS == "windows" && (ext == ".cmd" || ext == ".bat" || ext == ".ps1") {
		return nil, fmt.Errorf("%s is a shell script, not a native executable; use run_shell or start_background with PowerShell syntax instead", in.Program)
	}
	return cmd, nil
}

func (t RunProgram) Run(ctx context.Context, args json.RawMessage) (string, error) {
	return t.RunStreaming(ctx, args, nil)
}

func (t RunProgram) RunStreaming(ctx context.Context, args json.RawMessage, onOutput func(string)) (string, error) {
	var in programInput
	if err := json.Unmarshal(args, &in); err != nil {
		return "", fmt.Errorf("bad arguments: %w", err)
	}
	if err := rejectUnknownFields(args, "program", "args", "workdir", "timeout_seconds"); err != nil {
		return "", err
	}
	if err := in.validate(); err != nil {
		return "", err
	}
	timeout, err := executionTimeout(in.TimeoutSeconds)
	if err != nil {
		return "", err
	}
	dir, err := t.WS.Resolve(in.Workdir)
	if err != nil {
		return "", err
	}
	ctx, cancel := context.WithTimeout(ctx, timeout)
	defer cancel()
	cmd, err := in.cmd(ctx)
	if err != nil {
		return "", err
	}
	return runExecution(ctx, cmd, dir, onOutput)
}
