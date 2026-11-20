package agent

import (
	"context"
	"errors"
	"os/exec"
	"time"

	"github.com/keshon/gektor/internal/conversation"
)

func (a *Agent) verify(ctx context.Context) error {
	started := time.Now()
	output, err := a.cfg.Verify(ctx)
	receipt := a.report.VerificationReceipt
	receipt.DurationMS = time.Since(started).Milliseconds()
	receipt.Output = TruncateMiddle(output, 8192)
	a.report.Verification = receipt.Output

	// A callback that ignores cancellation cannot turn a stopped check into a pass.
	if ctx.Err() != nil {
		err = ctx.Err()
	}
	if err == nil {
		receipt.Disposition = conversation.VerificationPassed
		if receipt.Command != "" {
			code := 0
			receipt.ExitCode = &code
		}
		return nil
	}

	receipt.Error = TruncateMiddle(err.Error(), 8192)
	var exitError *exec.ExitError
	switch {
	case errors.Is(err, context.Canceled), errors.Is(err, context.DeadlineExceeded):
		receipt.Disposition = conversation.VerificationCancelled
	case errors.As(err, &exitError):
		receipt.Disposition = conversation.VerificationFailed
		if code := exitError.ExitCode(); code >= 0 {
			receipt.ExitCode = &code
		}
	case receipt.Command != "":
		receipt.Disposition = conversation.VerificationError
	default:
		// Non-shell validators report a rejected check through their error.
		receipt.Disposition = conversation.VerificationFailed
	}
	return err
}
