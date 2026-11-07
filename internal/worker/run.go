// Package worker runs bounded native parser workers in the current executable.
// It is a resource limit, not a general operating system sandbox.
package worker

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"os"
	"os/exec"
	"time"
)

type boundedBuffer struct {
	buffer bytes.Buffer
	limit  int
}

func (b *boundedBuffer) Write(p []byte) (int, error) {
	if b.buffer.Len()+len(p) > b.limit {
		return 0, fmt.Errorf("worker output exceeds limit")
	}
	return b.buffer.Write(p)
}

func Run(ctx context.Context, mode string, data []byte, timeout time.Duration, maxOutput int) (string, error) {
	exe, err := os.Executable()
	if err != nil {
		return "", err
	}
	ctx, cancel := context.WithTimeout(ctx, timeout)
	defer cancel()
	cmd := exec.CommandContext(ctx, exe, "workflow", mode)
	Configure(cmd)
	cmd.Env = []string{"GOMEMLIMIT=128MiB"}
	cmd.Stdin = bytes.NewReader(data)
	output := &boundedBuffer{limit: maxOutput}
	diagnostic := &boundedBuffer{limit: 4096}
	cmd.Stdout = output
	cmd.Stderr = diagnostic
	if err := cmd.Run(); err != nil {
		if ctx.Err() != nil {
			return "", fmt.Errorf("%s: %w", mode, ctx.Err())
		}
		return "", fmt.Errorf("%s: %w: %s", mode, err, diagnostic.buffer.String())
	}
	if !json.Valid(output.buffer.Bytes()) {
		return "", fmt.Errorf("worker returned invalid JSON")
	}
	return output.buffer.String(), nil
}
