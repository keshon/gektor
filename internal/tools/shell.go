package tools

import (
	"context"
	"encoding/json"
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
		return "Run a finite shell command inside the workspace. command is complete cmd.exe source. " +
			"Use it for builtins, pipelines, redirection and .cmd/.bat scripts. " +
			"For native executables with literal arguments, use run_program instead. " +
			"Use workdir for a workspace-relative working directory and timeout_seconds (1–600) to bound execution. " +
			"Use file tools for listings, edits and moves; use start_background for servers and watchers. " +
			"Return combined stdout and stderr; inspect the result before claiming success."
	}
	return "Run a finite shell command inside the workspace. command is complete POSIX sh source. " +
		"Use it for builtins, pipelines and redirection. " +
		"For native executables with literal arguments, use run_program instead. " +
		"Use workdir for a workspace-relative working directory and timeout_seconds (1–600) to bound execution. " +
		"Use file tools for listings, edits and moves; use start_background for servers and watchers. " +
		"Return combined stdout and stderr; inspect the result before claiming success."
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
		"command":{"type":"string","minLength":1,"description":"Complete shell source for cmd.exe on Windows or sh on POSIX."},
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
	if err != nil {
		return result, fmt.Errorf("command failed: %w", err)
	}
	return result, nil
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
		fmt.Sprintf("\n\n(output truncated: %d bytes total. Re-run narrowed — a pipe through "+
			"findstr/grep, a smaller path, or head/tail — rather than asking for all of it again.)",
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
			strings.Contains(upper, "AUTH_TOKEN") || strings.Contains(upper, "SECRET") {
			continue
		}
		out = append(out, kv)
	}
	return out
}
