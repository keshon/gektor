package tools

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"time"

	"github.com/keshon/gektor/internal/agent"
	"github.com/keshon/gektor/internal/workspace"
)

// StartBackground launches a long-running command (dev server, watcher)
// without blocking on it. This exists specifically because run_shell
// waits for the command to exit — correct for "go build", wrong for
// "npm run dev", which never exits by design and gets killed the moment
// a blocking call's timeout fires.
type StartBackground struct {
	WS *workspace.Workspace
	// Procs must be the same *BackgroundProcesses pointer shared with
	// CheckBackground and StopBackground.
	Procs *BackgroundProcesses
	// SettleTime is how long to wait before returning, so a command that
	// fails immediately (port already in use, command not found) shows
	// that in the result instead of falsely reporting "running". Zero
	// defaults to 1.5s.
	SettleTime time.Duration
}

func (StartBackground) Name() string { return "start_background" }

// Starting an arbitrary command is a mutation barrier.
func (StartBackground) Mode() agent.ToolMode { return agent.Exclusive }
func (StartBackground) Description() string {
	return "Start a server, watcher or background command without blocking. Prefer program + args for executables (literal arguments, no shell escaping); use command for shell syntax. Returns id and next_cursor for check_background. Use wait_background for finite builds/tests instead of polling. Stop only your processes via stop_background with its returned id."
}
func (StartBackground) Schema() json.RawMessage {
	return executionSchema(false)
}

func (t StartBackground) Run(ctx context.Context, args json.RawMessage) (string, error) {
	var in executionInput
	if err := json.Unmarshal(args, &in); err != nil {
		return "", fmt.Errorf("bad arguments: %w", err)
	}
	if err := rejectUnknownFields(args, "command", "program", "args", "workdir"); err != nil {
		return "", err
	}
	if err := in.validate(); err != nil {
		return "", err
	}
	if t.Procs == nil {
		return "", fmt.Errorf("no background process manager")
	}
	dir, err := t.WS.Resolve(in.Workdir)
	if err != nil {
		return "", err
	}

	// Deliberately context.Background(), not the tool call's ctx — this
	// process must outlive the call that started it.
	cmd, err := in.cmd(context.Background())
	if err != nil {
		return "", err
	}
	cmd.Dir = dir
	cmd.Env = scrubEnv(os.Environ())
	id, _ := t.Procs.start(ctx, cmd)

	settle := t.SettleTime
	if settle == 0 {
		settle = 1500 * time.Millisecond
	}
	timer := time.NewTimer(settle)
	defer timer.Stop()
	select {
	case <-timer.C:
	case <-ctx.Done():
		_ = t.Procs.stop(id)
		return "", ctx.Err()
	}

	output, next, exited, exitErr, err := t.Procs.status(id, 0)
	if err != nil {
		return "", err
	}
	status := "running in background"
	if exited {
		status = fmt.Sprintf("already exited: %v", exitErr)
	}
	return fmt.Sprintf("id: %s\nstatus: %s\nnext_cursor: %d\noutput:\n%s", id, status, next, output), nil
}

type CheckBackground struct{ Procs *BackgroundProcesses }

type WaitBackground struct{ Procs *BackgroundProcesses }

func (WaitBackground) Name() string         { return "wait_background" }
func (WaitBackground) Mode() agent.ToolMode { return agent.Concurrent }
func (WaitBackground) Description() string {
	return "Wait for a finite background build/test without model polling. timeout_seconds defaults to 30, maximum 60. Servers remain running. Completion after this agent run ends is delivered on the next run, never by a hidden model invocation."
}
func (WaitBackground) Schema() json.RawMessage {
	return json.RawMessage(`{"type":"object","properties":{"id":{"type":"string"},"timeout_seconds":{"type":"integer","minimum":1,"maximum":60}},"required":["id"],"additionalProperties":false}`)
}
func (t WaitBackground) Run(ctx context.Context, args json.RawMessage) (string, error) {
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
	if store := outputStore(ctx); store != nil && proc.scope != store.dir {
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
	return (CheckBackground{Procs: t.Procs}).Run(ctx, readArgs)
}

func (CheckBackground) Name() string         { return "check_background" }
func (CheckBackground) Mode() agent.ToolMode { return agent.Concurrent }
func (CheckBackground) Description() string {
	return "Check the current output and status (running/exited) of a process started with " +
		"start_background. Pass its next_cursor as cursor to receive only new output. " +
		"Use cursor=0 to read from the beginning; evicted output is marked explicitly."
}
func (CheckBackground) Schema() json.RawMessage {
	return json.RawMessage(`{
		"type": "object",
		"properties": {"id": {"type": "string"}, "cursor": {"type":"integer","minimum":0}},
		"required": ["id", "cursor"]
	}`)
}

func (t CheckBackground) Run(_ context.Context, args json.RawMessage) (string, error) {
	var in struct {
		ID     string `json:"id"`
		Cursor *int   `json:"cursor"`
	}
	if err := json.Unmarshal(args, &in); err != nil {
		return "", fmt.Errorf("bad arguments: %w", err)
	}
	if err := rejectUnknownFields(args, "id", "cursor"); err != nil {
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

type StopBackground struct{ Procs *BackgroundProcesses }

func (StopBackground) Name() string         { return "stop_background" }
func (StopBackground) Mode() agent.ToolMode { return agent.Exclusive }
func (StopBackground) Description() string  { return "Stop a process started with start_background." }
func (StopBackground) Schema() json.RawMessage {
	return json.RawMessage(`{
		"type": "object",
		"properties": {"id": {"type": "string"}},
		"required": ["id"]
	}`)
}

func (t StopBackground) Run(_ context.Context, args json.RawMessage) (string, error) {
	var in struct {
		ID string `json:"id"`
	}
	if err := json.Unmarshal(args, &in); err != nil {
		return "", fmt.Errorf("bad arguments: %w", err)
	}
	if err := t.Procs.stop(in.ID); err != nil {
		return "", err
	}
	return "stopped", nil
}
