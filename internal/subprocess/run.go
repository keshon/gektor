// Package subprocess runs bounded JSON workers in the current executable.
// It is a resource limit, not a general operating system sandbox.
package subprocess

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

func (b *boundedBuffer) Write(data []byte) (int, error) {
	if b.buffer.Len()+len(data) > b.limit {
		return 0, fmt.Errorf("worker output exceeds limit")
	}

	return b.buffer.Write(data)
}

// Run starts this executable with "workflow <mode>" and sends data on stdin.
// The executable must dispatch that mode to a worker that writes one JSON value
// to stdout. Output is bounded by maxOutput bytes and diagnostics by 4 KiB.
// Run supplies GOMEMLIMIT=128MiB instead of inheriting the parent's environment.
// Cancellation or timeout terminates the child; Run waits for its exit and
// releases its process resources. Workers call Limit before processing input.
func Run(ctx context.Context, mode string, data []byte, timeout time.Duration, maxOutput int) (string, error) {
	executable, err := os.Executable()
	if err != nil {
		return "", err
	}

	workerCtx, cancel := context.WithTimeout(ctx, timeout)
	defer cancel()

	output := &boundedBuffer{limit: maxOutput}
	diagnostic := &boundedBuffer{limit: 4096}
	cmd := exec.CommandContext(workerCtx, executable, "workflow", mode)
	configure(cmd)
	cmd.Env = []string{"GOMEMLIMIT=128MiB"}
	cmd.Stdin = bytes.NewReader(data)
	cmd.Stdout = output
	cmd.Stderr = diagnostic

	err = cmd.Run()
	if err != nil {
		contextErr := workerCtx.Err()
		if contextErr != nil {
			return "", fmt.Errorf("%s: %w", mode, contextErr)
		}

		diagnostics := diagnostic.buffer.String()
		return "", fmt.Errorf("%s: %w: %s", mode, err, diagnostics)
	}

	result := output.buffer.Bytes()
	if !json.Valid(result) {
		return "", fmt.Errorf("worker returned invalid JSON")
	}

	return string(result), nil
}
