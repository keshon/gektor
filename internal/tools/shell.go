package tools

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os/exec"
	"runtime"
	"strings"
	"time"

	"github.com/keshon/gektor/internal/agent"
	"github.com/keshon/gektor/internal/workspace"
)

// shellTimeout bounds one command. Builds and installs outlive anything
// shorter, so a 30s default silently killed ordinary work.
const shellTimeout = 10 * time.Minute

type RunShell struct {
	WS *workspace.Workspace
}

func (RunShell) Name() string { return "run_shell" }

// Mode is Exclusive: a shell command can mutate files, hold a lock or
// depend on ordering, and none of that is visible in the string.
func (RunShell) Mode() agent.ToolMode { return agent.Exclusive }
func (RunShell) Description() string {
	if runtime.GOOS == "windows" {
		return "Run finite PowerShell source using " + ShellName() + ". For builtins, pipelines, redirection and scripts; prefer run_program for native executables. Servers use start_background. Returns whole-command exit_code and combined stdout/stderr."
	}
	return "Run finite POSIX sh source for builtins, pipelines, redirection and scripts; prefer run_program for native executables. Servers use start_background. Returns whole-command exit_code and combined stdout/stderr."
}
func (RunShell) Schema() json.RawMessage {
	return shellSchema(true)
}

// Shell input is deliberately a single mode: command is always shell source.
// Native processes use programInput and the separate run_program/start_program
// tools, so neither schema needs a top-level oneOf.
type shellInput struct {
	Command        string `json:"command"`
	Workdir        string `json:"workdir"`
	TimeoutSeconds *int   `json:"timeout_seconds"`
}

func shellSchema(timeout bool) json.RawMessage {
	extra := ""
	if timeout {
		extra = `,"timeout_seconds":{"type":"integer","minimum":1,"maximum":600,"default":600}`
	}
	return json.RawMessage(`{"type":"object","properties":{
		"command":{"type":"string","minLength":1,"description":"Complete PowerShell source on Windows or POSIX sh source elsewhere."},
		"workdir":{"type":"string","description":"Workspace-relative working directory; defaults to root."}` + extra +
		`},"required":["command"],"additionalProperties":false}`)
}

func (in shellInput) validate() error {
	if strings.TrimSpace(in.Command) == "" {
		return fmt.Errorf("command is required")
	}
	return nil
}

func executionTimeout(seconds *int) (time.Duration, error) {
	if seconds == nil {
		return shellTimeout, nil
	}
	if *seconds < 1 || *seconds > 600 {
		return 0, fmt.Errorf("timeout_seconds must be between 1 and 600")
	}
	return time.Duration(*seconds) * time.Second, nil
}

func runExecution(ctx context.Context, cmd *exec.Cmd, dir string, onOutput func(string)) (string, error) {
	full, err := runProcess(ctx, cmd, dir, onOutput)
	result := capShellOutput(full)
	if err == nil {
		return "exit_code: 0\n" + result, nil
	}
	if cmd.Process == nil {
		return result, fmt.Errorf("process did not start: %w", err)
	}
	if errors.Is(err, context.Canceled) || errors.Is(err, context.DeadlineExceeded) {
		return result, fmt.Errorf("process interrupted: %w", err)
	}
	var exited *exec.ExitError
	if errors.As(err, &exited) {
		return fmt.Sprintf("exit_code: %d\n%s", exited.ExitCode(), result), fmt.Errorf("process exited unsuccessfully: %w", err)
	}
	return result, fmt.Errorf("process execution failed: %w", err)
}

func (t RunShell) Run(ctx context.Context, args json.RawMessage) (string, error) {
	return t.RunStreaming(ctx, args, nil)
}

func (t RunShell) RunStreaming(ctx context.Context, args json.RawMessage, onOutput func(string)) (string, error) {
	var in shellInput
	if err := json.Unmarshal(args, &in); err != nil {
		return "", fmt.Errorf("bad arguments: %w", err)
	}

	if err := rejectUnknownFields(args, "command", "workdir", "timeout_seconds"); err != nil {
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
	return runExecution(ctx, shellCommand(ctx, in.Command), dir, onOutput)
}

// shellMaxBytes caps one run_shell result, matching the read_file cap for
// the same reason: no single tool result may consume most of a small
// context window.
//
// This was the last uncapped way into the context, and capping the others
// is what found it. read_file truncates and grep_files caps its matches,
// so a model denied a large read reaches for the shell instead: live, a
// probe that reads a 205KB fixture ran "type big.txt", put the whole file
// in the prompt, and drove a 3.4k prompt to 57k in one step. The read cap
// had been tightened an hour earlier and was simply not in the path.
//
// Middle-truncated rather than head-truncated: a command's first lines
// carry what it is doing and its last lines carry how it failed, and the
// failure is usually the point.
const shellMaxBytes = 48 * 1024

func capShellOutput(s string) string {
	if len(s) <= shellMaxBytes {
		return s
	}
	return agent.TruncateMiddle(s, shellMaxBytes) +
		fmt.Sprintf("\n\n(output truncated: %d bytes total. Narrow the command's paths or output options; use read_output if a saved output id is provided.)",
			len(s))
}

// scrubEnv removes bearer tokens from the child environment so a model
// that prints env or exfils it via curl gets nothing useful.
func scrubEnv(env []string) []string {
	out := make([]string, 0, len(env))
	for _, kv := range env {
		k := kv
		if i := strings.Index(kv, "="); i >= 0 {
			k = kv[:i]
		}
		upper := strings.ToUpper(k)
		if strings.Contains(upper, "API_KEY") || strings.Contains(upper, "APIKEY") ||
			strings.Contains(upper, "TOKEN") || strings.Contains(upper, "SECRET") ||
			strings.Contains(upper, "PASSWORD") || strings.Contains(upper, "CREDENTIAL") || strings.Contains(upper, "COOKIE") {
			continue
		}
		out = append(out, kv)
	}
	return out
}
