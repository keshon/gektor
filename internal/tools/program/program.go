package program

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

// Tool starts a native executable directly, without a shell. Keeping it
// separate from run_shell gives both tools a single, unambiguous input shape:
// run_shell has command, run_program has program + args.
type Tool struct {
	WS *workspace.Workspace
}

func (Tool) Name() string { return "run_program" }

// A direct process can mutate files, hold a lock or depend on ordering.
func (Tool) Mode() agent.ToolMode { return agent.Exclusive }
func (Tool) Description() string {
	if runtime.GOOS == "windows" {
		return "Run a finite native executable without a shell. Pass literal args, without quoting or expansion. Use run_shell for PowerShell syntax/scripts. Returns exit_code and combined stdout/stderr; nonzero exits are failures."
	}
	return "Run a finite native executable without a shell. Pass literal args, without quoting or expansion. Use run_shell for POSIX sh syntax/scripts. Returns exit_code and combined stdout/stderr; nonzero exits are failures."
}
func (Tool) Schema() json.RawMessage {
	return json.RawMessage(`{"type":"object","properties":{
		"program":{"type":"string","minLength":1,"description":"Native executable name or path, without shell quoting."},
		"args":{"type":"array","items":{"type":"string"},"description":"Literal arguments passed verbatim; no shell expansion."},
		"workdir":{"type":"string","description":"Workspace-relative working directory; defaults to root."},"timeout_seconds":{"type":"integer","minimum":1,"maximum":600,"default":600}},"required":["program"],"additionalProperties":false}`)
}

// programInput is shared by run_program and start_program. args are passed to
// the OS verbatim, never joined into a command line by us.
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

func (t Tool) Run(ctx context.Context, args json.RawMessage) (string, error) {
	return t.RunStreaming(ctx, args, nil)
}

func (t Tool) RunStreaming(ctx context.Context, args json.RawMessage, onOutput func(string)) (string, error) {
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
