package background

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"os/exec"
	"strings"
	"time"

	"github.com/keshon/gektor/internal/agent"
	"github.com/keshon/gektor/internal/process"
	commandrun "github.com/keshon/gektor/internal/tools/command"
	tooloutput "github.com/keshon/gektor/internal/tools/output"
	"github.com/keshon/gektor/internal/workspace"
)

// StartBackground launches a long-running command (dev server, watcher)
// without blocking on it. This exists specifically because run_shell
// waits for the command to exit — correct for "go build", wrong for
// "npm run dev", which never exits by design and gets killed the moment
// a blocking call's timeout fires.
type StartShell struct {
	WS *workspace.Workspace
	// Procs must be the same *BackgroundProcesses pointer shared with
	// CheckBackground and StopBackground.
	Procs *Processes
	// SettleTime is how long to wait before returning, so a command that
	// fails immediately (port already in use, command not found) shows
	// that in the result instead of falsely reporting "running". Zero
	// defaults to 1.5s.
	SettleTime time.Duration
}

func (StartShell) Name() string { return "start_background" }

// Starting an arbitrary command is a mutation barrier.
func (StartShell) Mode() agent.ToolMode { return agent.Exclusive }
func (StartShell) Description() string {
	return "Start a server, watcher or background command using " + commandrun.ShellName() + " without blocking. command is complete shell source. Returns id and next_cursor for check_background. Use start_program for a native executable with literal arguments. Use wait_background for finite builds/tests instead of polling. Stop only your processes via stop_background with its returned id."
}
func (StartShell) Schema() json.RawMessage {
	return json.RawMessage(`{"type":"object","properties":{
		"command":{"type":"string","minLength":1,"description":"Complete PowerShell source on Windows or POSIX sh source elsewhere."},
		"workdir":{"type":"string","description":"Workspace-relative working directory; defaults to root."}},"required":["command"],"additionalProperties":false}`)
}

func (t StartShell) Run(ctx context.Context, args json.RawMessage) (string, error) {
	var in shellInput
	if err := json.Unmarshal(args, &in); err != nil {
		return "", fmt.Errorf("bad arguments: %w", err)
	}
	if err := agent.RejectUnknownFields(args, "command", "workdir"); err != nil {
		return "", err
	}
	if err := in.validate(); err != nil {
		return "", err
	}
	dir, err := t.WS.Resolve(in.Workdir)
	if err != nil {
		return "", err
	}

	// Deliberately context.Background(), not the tool call's ctx — this
	// process must outlive the call that started it.
	return startBackgroundProcess(ctx, t.Procs, commandrun.Shell(context.Background(), in.Command), dir, t.SettleTime, in.Command)
}

// StartProgram is the direct-exec counterpart of StartBackground. It exists
// as a separate tool so neither schema has to switch modes on optional fields.
type StartProgram struct {
	WS         *workspace.Workspace
	Procs      *Processes
	SettleTime time.Duration
}

func (StartProgram) Name() string { return "start_program" }

// Starting an arbitrary process is a mutation barrier.
func (StartProgram) Mode() agent.ToolMode { return agent.Exclusive }
func (StartProgram) Description() string {
	return "Start a native server or watcher without a shell. For a local static page use program=python, args=[-m,http.server,PORT,--bind,127.0.0.1]. Arguments are literal. Returns an owned id for check_background and stop_background; save it. Use start_background for shell syntax or .cmd/.bat scripts. Do not detach servers through run_shell/Start-Process."
}
func (StartProgram) Schema() json.RawMessage {
	return json.RawMessage(`{"type":"object","properties":{
		"program":{"type":"string","minLength":1,"description":"Native executable name or path, without shell quoting."},
		"args":{"type":"array","items":{"type":"string"},"description":"Literal arguments passed verbatim; no shell expansion."},
		"workdir":{"type":"string","description":"Workspace-relative working directory; defaults to root."}},"required":["program"],"additionalProperties":false}`)
}

func (t StartProgram) Run(ctx context.Context, args json.RawMessage) (string, error) {
	var in programInput
	if err := json.Unmarshal(args, &in); err != nil {
		return "", fmt.Errorf("bad arguments: %w", err)
	}
	if err := agent.RejectUnknownFields(args, "program", "args", "workdir"); err != nil {
		return "", err
	}
	if err := in.validate(); err != nil {
		return "", err
	}
	dir, err := t.WS.Resolve(in.Workdir)
	if err != nil {
		return "", err
	}

	// Deliberately context.Background(), not the tool call's ctx — this
	// process must outlive the call that started it.
	cmd, err := commandrun.Native(context.Background(), in.Program, in.Args)
	if err != nil {
		return "", err
	}
	return startBackgroundProcess(ctx, t.Procs, cmd, dir, t.SettleTime, "")
}

func startBackgroundProcess(ctx context.Context, procs *Processes, cmd *exec.Cmd, dir string, settle time.Duration, command string) (string, error) {
	if procs == nil {
		return "", fmt.Errorf("no background process manager")
	}
	cmd.Dir = dir
	cmd.Env = process.ScrubEnv(os.Environ())
	id, _ := procs.start(ctx, cmd)
	if command != "" {
		procs.mu.Lock()
		procs.procs[id].info.Command = command
		procs.mu.Unlock()
	}

	if settle == 0 {
		settle = 1500 * time.Millisecond
	}
	timer := time.NewTimer(settle)
	defer timer.Stop()
	select {
	case <-timer.C:
	case <-ctx.Done():
		_ = procs.stop(id)
		return "", ctx.Err()
	}

	output, next, exited, exitErr, err := procs.status(id, 0)
	if err != nil {
		return "", err
	}
	status := "running in background"
	if exited {
		status = fmt.Sprintf("already exited: %v", exitErr)
	}
	result := fmt.Sprintf("id: %s\nstatus: %s\nnext_cursor: %d\nstop: stop_background {\"id\":%q}\noutput:\n%s", id, status, next, id, output)
	if exited && exitErr != nil {
		return result, fmt.Errorf("background process %s failed: %w", id, exitErr)
	}
	return result, nil
}

type Check struct{ Procs *Processes }

type Wait struct{ Procs *Processes }

func (Wait) Name() string         { return "wait_background" }
func (Wait) Mode() agent.ToolMode { return agent.Concurrent }
func (Wait) Description() string {
	return "Wait for a background build/test and return status/output; timeout_seconds defaults to 30 (max 60). Does not stop servers. Completion after this run ends is reported on the next; it never starts a model run."
}
func (Wait) Schema() json.RawMessage {
	return json.RawMessage(`{"type":"object","properties":{"id":{"type":"string"},"timeout_seconds":{"type":"integer","minimum":1,"maximum":60}},"required":["id"],"additionalProperties":false}`)
}
func (t Wait) Run(ctx context.Context, args json.RawMessage) (string, error) {
	var in struct {
		ID             string
		TimeoutSeconds int `json:"timeout_seconds"`
	}
	if err := json.Unmarshal(args, &in); err != nil {
		return "", err
	}
	if in.TimeoutSeconds == 0 {
		in.TimeoutSeconds = 30
	}
	if in.TimeoutSeconds < 1 || in.TimeoutSeconds > 60 {
		return "", fmt.Errorf("timeout_seconds must be 1–60")
	}
	if t.Procs == nil {
		return "", fmt.Errorf("no background processes")
	}
	t.Procs.mu.Lock()
	proc := t.Procs.procs[in.ID]
	t.Procs.mu.Unlock()
	if proc == nil {
		return "", fmt.Errorf("unknown background process %q", in.ID)
	}
	if store := tooloutput.FromContext(ctx); store != nil && proc.scope != store.Scope() {
		return "", fmt.Errorf("background process belongs to another session")
	}
	timer := time.NewTimer(time.Duration(in.TimeoutSeconds) * time.Second)
	defer timer.Stop()
	select {
	case <-ctx.Done():
		return "", ctx.Err()
	case <-proc.done:
	case <-timer.C:
	}
	readArgs, _ := json.Marshal(map[string]any{"id": in.ID, "cursor": 0})
	return Check(t).Run(ctx, readArgs)
}

func (Check) Name() string         { return "check_background" }
func (Check) Mode() agent.ToolMode { return agent.Concurrent }
func (Check) Description() string {
	return "Read status and output of a process started with start_background or start_program. " +
		"Pass next_cursor as cursor for new output, or 0 for the beginning. Older output discarded from the buffer is marked."
}
func (Check) Schema() json.RawMessage {
	return json.RawMessage(`{
		"type": "object",
		"properties": {"id": {"type": "string"}, "cursor": {"type":"integer","minimum":0}},
		"required": ["id", "cursor"]
	}`)
}

func (t Check) Run(_ context.Context, args json.RawMessage) (string, error) {
	var in struct {
		ID     string `json:"id"`
		Cursor *int   `json:"cursor"`
	}
	if err := json.Unmarshal(args, &in); err != nil {
		return "", fmt.Errorf("bad arguments: %w", err)
	}
	if err := agent.RejectUnknownFields(args, "id", "cursor"); err != nil {
		return "", err
	}
	if in.Cursor == nil {
		return "", fmt.Errorf("cursor is required; use next_cursor from the previous result, or 0 to read from the beginning")
	}
	output, next, exited, exitErr, err := t.Procs.status(in.ID, *in.Cursor)
	if err != nil {
		return "", err
	}
	status := "still running"
	if exited {
		status = "exited cleanly"
		if exitErr != nil {
			status = fmt.Sprintf("exited with error: %v", exitErr)
		}
	}
	return fmt.Sprintf("status: %s\nnext_cursor: %d\noutput:\n%s", status, next, output), nil
}

type Stop struct{ Procs *Processes }

func (Stop) Name() string         { return "stop_background" }
func (Stop) Mode() agent.ToolMode { return agent.Exclusive }
func (Stop) Description() string {
	return "Stop a process started with start_background or start_program."
}
func (Stop) Schema() json.RawMessage {
	return json.RawMessage(`{
		"type": "object",
		"properties": {"id": {"type": "string"}},
		"required": ["id"]
	}`)
}

func (t Stop) Run(ctx context.Context, args json.RawMessage) (string, error) {
	var in struct {
		ID string `json:"id"`
	}
	if err := json.Unmarshal(args, &in); err != nil {
		return "", fmt.Errorf("bad arguments: %w", err)
	}
	if err := agent.RejectUnknownFields(args, "id"); err != nil {
		return "", err
	}
	if t.Procs == nil {
		return "", fmt.Errorf("no background process manager")
	}
	// IDs are supplied by the manager, never inferred from executable names.
	t.Procs.mu.Lock()
	proc := t.Procs.procs[in.ID]
	t.Procs.mu.Unlock()
	if proc == nil {
		return "", fmt.Errorf("unknown background process id %q; use the exact id returned by start_program/start_background. Processes launched through run_shell or Start-Process have no managed id; do not invent an id or kill processes by application name", in.ID)
	}
	if store := tooloutput.FromContext(ctx); store != nil && proc.scope != store.Scope() {
		return "", fmt.Errorf("background process belongs to another session")
	}
	if err := t.Procs.stop(in.ID); err != nil {
		return "", err
	}
	return "stopped", nil
}

type shellInput struct {
	Command string `json:"command"`
	Workdir string `json:"workdir"`
}

func (in shellInput) validate() error {
	if strings.TrimSpace(in.Command) == "" {
		return fmt.Errorf("command is required")
	}
	return nil
}

type programInput struct {
	Program string   `json:"program"`
	Args    []string `json:"args"`
	Workdir string   `json:"workdir"`
}

func (in programInput) validate() error {
	if strings.TrimSpace(in.Program) == "" {
		return fmt.Errorf("program is required")
	}
	return nil
}
