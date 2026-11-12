package command

import (
	"context"
	"errors"
	"fmt"
	"os/exec"
	"time"

	tooloutput "github.com/keshon/gektor/internal/tools/output"
)

const defaultTimeout = 10 * time.Minute

func Timeout(seconds *int) (time.Duration, error) {
	if seconds == nil {
		return defaultTimeout, nil
	}
	if *seconds < 1 || *seconds > 600 {
		return 0, fmt.Errorf("timeout_seconds must be between 1 and 600")
	}
	return time.Duration(*seconds) * time.Second, nil
}

func Result(ctx context.Context, cmd *exec.Cmd, dir string, onOutput func(string)) (string, error) {
	full, err := Execute(ctx, cmd, dir, onOutput)
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

const shellMaxBytes = 48 * 1024

func capShellOutput(s string) string {
	if len(s) <= shellMaxBytes {
		return s
	}
	return tooloutput.TruncateMiddle(s, shellMaxBytes) +
		fmt.Sprintf("\n\n(output truncated: %d bytes total. Narrow the command's paths or output options; use read_output if a saved output id is provided.)",
			len(s))
}
