package tools

import (
	"context"
	"encoding/json"
	"fmt"
	"os/exec"
	"path/filepath"
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
		return "Run a finite process inside the workspace. command uses cmd.exe syntax. " +
			"Prefer program + args for executables (including paths with spaces); no shell quoting or expansion applies. Use command for shell builtins and .cmd/.bat scripts; never combine the two. " +
			"Use workdir for a workspace-relative working directory and timeout_seconds (1–600) to bound execution. " +
			"Use file tools for listings, edits and moves; use start_background for servers and watchers. " +
			"Return combined stdout and stderr; inspect the result before claiming success."
	}
	return "Run a finite process inside the workspace. command uses POSIX sh syntax. " +
		"Prefer program + args for executables; use command only for shell syntax. Never combine the two. " +
		"Use workdir for a workspace-relative working directory and timeout_seconds (1–600) to bound execution. " +
		"Use file tools for listings, edits and moves; use start_background for servers and watchers. " +
		"Return combined stdout and stderr; inspect the result before claiming success."
}
func (RunShell) Schema() json.RawMessage {
	return executionSchema(true)
}

// Both process tools share argument boundaries and validation. command is shell
// source; program/args are passed verbatim to the OS, never joined for execution.
type executionInput struct {
	Command        string   `json:"command"`
	Program        string   `json:"program"`
	Args           []string `json:"args"`
	Workdir        string   `json:"workdir"`
	TimeoutSeconds *int     `json:"timeout_seconds"`
}

func executionSchema(timeout bool) json.RawMessage {
	extra := ""
	if timeout {
		extra = `,"timeout_seconds":{"type":"integer","minimum":1,"maximum":600,"default":600}`
	}
	return json.RawMessage(`{"type":"object","properties":{
		"command":{"type":"string","minLength":1,"description":"Shell source; use either command OR program + args."},
		"program":{"type":"string","minLength":1,"description":"Executable name or path without wrapping quotes."},
		"args":{"type":"array","items":{"type":"string"},"description":"Literal arguments. Spaces, quotes and shell metacharacters need no escaping."},
		"workdir":{"type":"string","description":"Workspace-relative working directory; defaults to root."}` + extra +
		`},"oneOf":[{"required":["command"]},{"required":["program"]}],"additionalProperties":false}`)
}

func (in executionInput) validate() error {
	if (strings.TrimSpace(in.Command) == "") == (strings.TrimSpace(in.Program) == "") {
		return fmt.Errorf("provide exactly one of command or program; use program + args for executables")
	}
	if in.Command != "" && in.Args != nil {
		return fmt.Errorf("args requires program; command is complete shell source")
	}
	return nil
}

func (in executionInput) cmd(ctx context.Context) (*exec.Cmd, error) {
	if in.Program != "" {
		cmd := exec.CommandContext(ctx, in.Program, in.Args...)
		if ext := strings.ToLower(filepath.Ext(cmd.Path)); runtime.GOOS == "windows" && (ext == ".cmd" || ext == ".bat") {
			return nil, fmt.Errorf("%s is a shell script, not a native executable; use command with CMD syntax instead of program + args", in.Program)
		}
		return cmd, nil
	}
	return shellCommand(ctx, in.Command), nil
}

func (t RunShell) Run(ctx context.Context, args json.RawMessage) (string, error) {
	return t.RunStreaming(ctx, args, nil)
}

func (t RunShell) RunStreaming(ctx context.Context, args json.RawMessage, onOutput func(string)) (string, error) {
	var in executionInput
	if err := json.Unmarshal(args, &in); err != nil {
		return "", fmt.Errorf("bad arguments: %w", err)
	}

	if err := rejectUnknownFields(args, "command", "program", "args", "workdir", "timeout_seconds"); err != nil {
		return "", err
	}
	if err := in.validate(); err != nil {
		return "", err
	}
	timeout := shellTimeout
	if in.TimeoutSeconds != nil {
		if *in.TimeoutSeconds < 1 || *in.TimeoutSeconds > 600 {
			return "", fmt.Errorf("timeout_seconds must be between 1 and 600")
		}
		timeout = time.Duration(*in.TimeoutSeconds) * time.Second
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
	full, err := runProcess(ctx, cmd, dir, onOutput)
	result := capShellOutput(full)

	if err != nil {
		return result, fmt.Errorf("command failed: %w", err)
	}
	return result, nil
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
