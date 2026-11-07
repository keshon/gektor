// Package process adapts native process execution to workflow nodes.
package process

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/keshon/gektor/internal/automation"
	runner "github.com/keshon/gektor/internal/integrations/process"
	"github.com/keshon/gektor/internal/workspace"
)

type input struct {
	Program string   `json:"program"`
	Args    []string `json:"args"`
	Workdir string   `json:"workdir"`
	Timeout int      `json:"timeout_seconds"`
	Stdin   string   `json:"stdin_artifact"`
}

func Definition(ws *workspace.Workspace) automation.Node {
	return automation.Node{Name: "run_program", Version: 1, Description: "Run a finite native executable with literal arguments and optional stdin_artifact. Saves stdout/stderr as artifacts. Nonzero exits, timeout and output overflow fail the step. Runs with OS account permissions; not a sandbox. No automatic retries.",
		InputSchema:  json.RawMessage(`{"type":"object","properties":{"program":{"type":"string","minLength":1},"args":{"type":"array","maxItems":256,"items":{"type":"string"}},"workdir":{"type":"string"},"timeout_seconds":{"type":"integer","minimum":1,"maximum":600},"stdin_artifact":{"type":"string","minLength":1,"maxLength":64}},"required":["program"],"additionalProperties":false}`),
		OutputSchema: json.RawMessage(`{"type":"object","properties":{"exit_code":{"type":"integer"},"truncated":{"type":"boolean"},"stdout":{"type":"object","properties":{"id":{"type":"string"},"run_id":{"type":"string"},"step_id":{"type":"string"},"name":{"type":"string"},"media_type":{"type":"string"},"sha256":{"type":"string"},"created":{"type":"string"},"size":{"type":"integer"}},"required":["id","run_id","step_id","name","media_type","size","sha256","created"]},"stderr":{"type":"object","properties":{"id":{"type":"string"},"run_id":{"type":"string"},"step_id":{"type":"string"},"name":{"type":"string"},"media_type":{"type":"string"},"sha256":{"type":"string"},"created":{"type":"string"},"size":{"type":"integer"}},"required":["id","run_id","step_id","name","media_type","size","sha256","created"]},"error":{"type":"string"}},"required":["exit_code","truncated"]}`),
		Resources: func(raw json.RawMessage) ([]string, error) {
			var in input
			if err := json.Unmarshal(raw, &in); err != nil {
				return nil, err
			}
			// Keep the same resource spelling as the native run_program tool.
			return []string{in.Program + " " + strings.Join(in.Args, " ")}, nil
		},
		Validate: func(raw json.RawMessage) error {
			var in input
			if err := json.Unmarshal(raw, &in); err != nil {
				return err
			}
			if strings.TrimSpace(in.Program) == "" {
				return fmt.Errorf("program is required")
			}
			if ws == nil {
				return fmt.Errorf("workspace unavailable")
			}
			_, err := ws.Relative(in.Workdir)
			return err
		},
		Execute: func(ctx context.Context, raw json.RawMessage) (string, error) {
			var in input
			if err := json.Unmarshal(raw, &in); err != nil {
				return "", err
			}
			if ws == nil {
				return "", fmt.Errorf("workspace unavailable")
			}
			access, err := automation.Artifacts(ctx)
			if err != nil {
				return "", err
			}
			info, err := ws.Stat(in.Workdir)
			if err != nil {
				return "", err
			}
			if !info.IsDir() {
				return "", fmt.Errorf("workdir must be a workspace directory")
			}
			dir, err := ws.Resolve(in.Workdir)
			if err != nil {
				return "", err
			}
			timeout := in.Timeout
			if timeout == 0 {
				timeout = 60
			}
			req := runner.Request{Program: in.Program, Args: in.Args, Directory: dir, Timeout: time.Duration(timeout) * time.Second}
			if in.Stdin != "" {
				_, data, err := access.Read(ctx, in.Stdin)
				if err != nil {
					return "", err
				}
				req.Stdin = bytes.NewReader(data)
			}
			result, runErr := runner.Run(ctx, req)
			// Preserve diagnostics even when the process consumed its deadline or
			// the operator cancelled it. This does not start another process.
			saveCtx, cancel := context.WithTimeout(context.WithoutCancel(ctx), 5*time.Second)
			defer cancel()
			stdout, outErr := access.Put(saveCtx, "stdout.txt", bytes.NewReader(result.Stdout))
			stderr, errErr := access.Put(saveCtx, "stderr.txt", bytes.NewReader(result.Stderr))
			out := map[string]any{"exit_code": result.ExitCode, "truncated": result.Truncated}
			if outErr == nil {
				out["stdout"] = stdout
			}
			if errErr == nil {
				out["stderr"] = stderr
			}
			if runErr != nil {
				out["error"] = runErr.Error()
			}
			encoded, encodeErr := json.Marshal(out)
			return string(encoded), errors.Join(runErr, outErr, errErr, encodeErr)
		}}
}
