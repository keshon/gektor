package tui

import (
	"context"
	"errors"
	"strings"

	"github.com/keshon/gektor/internal/agent"
	"github.com/keshon/gektor/internal/llm"
)

func recoveryHint(err error) string {
	if errors.Is(err, agent.ErrMaxSteps) {
		return "Step budget exhausted. Narrow the remaining task or change /max-steps N, then /retry to continue from the saved checkpoint."
	}
	if errors.Is(err, agent.ErrRunBudget) {
		return "Time budget exhausted. Send a narrower follow-up to continue the remaining work."
	}
	if errors.Is(err, context.Canceled) {
		return "Run cancelled. Send a follow-up to continue; completed changes remain."
	}
	if errors.Is(err, context.DeadlineExceeded) {
		return "Request timed out. Check the model server or tool, then /retry to continue from the saved checkpoint."
	}
	if llm.IsOverflow(err) || strings.Contains(strings.ToLower(err.Error()), "insufficient") && strings.Contains(strings.ToLower(err.Error()), "context") {
		return "Context does not fit. Try /compact; if it cannot free enough room, use a larger model-server context and restart with the saved session."
	}
	if api, ok := llm.AsAPIError(err); ok && !api.Retryable() {
		if api.Status == 401 || api.Status == 403 {
			return "Check backend credentials and access before retrying. Connection settings are shown under Model."
		}
		return "Check the backend model, endpoint and request settings before retrying. Connection settings are shown under Model."
	}
	if strings.Contains(strings.ToLower(err.Error()), "checkpoint") {
		return "Context checkpoint failed; previous context was retained. Check the error above before retrying."
	}
	return "After resolving the cause, /retry continues from the saved checkpoint. Existing workspace changes are not rolled back."
}
