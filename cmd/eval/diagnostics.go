package main

import (
	"context"
	"errors"
	"os/exec"
	"time"

	"github.com/keshon/gektor/internal/workspace"
)

// CheckResult retains the check's bounded combined stdout/stderr. ExitCode is
// absent for non-command checks and commands that did not exit normally.
type CheckResult struct {
	Index    int     `json:"index"`
	Check    Check   `json:"check"`
	Passed   bool    `json:"passed"`
	Output   string  `json:"output"`
	ExitCode *int    `json:"exit_code,omitempty"`
	Seconds  float64 `json:"seconds"`
}

type requestTiming struct {
	Seconds float64 `json:"seconds"`
	// Omitted when the backend reports no incremental generation progress.
	FirstTokenSeconds float64 `json:"first_token_seconds,omitempty"`
	PromptTokens      int     `json:"prompt_tokens"`
	CompletionTokens  int     `json:"completion_tokens"`
	FinishReason      string  `json:"finish_reason,omitempty"`
	Error             string  `json:"error,omitempty"`
}

// Acceptance has its own bounded budget. Parent cancellation still stops it.
func checkArtifacts(ctx context.Context, p Probe, ws *workspace.Workspace) []CheckResult {
	ctx, cancel := context.WithTimeout(ctx, 2*time.Minute)
	defer cancel()
	checks := make([]CheckResult, 0, len(p.Verify))
	for index, check := range p.Verify {
		started := time.Now()
		result := CheckResult{Index: index, Check: check}
		if check.Type == "shell" || check.Type == "go_acceptance" {
			output, err := runCommandCheck(ctx, check, ws)
			result.Output = output
			result.Passed = err == nil
			result.ExitCode = checkExitCode(err)
		} else {
			result.Output, result.Passed = RunCheck(ctx, check, ws)
		}
		result.Seconds = time.Since(started).Seconds()
		checks = append(checks, result)
	}
	return checks
}

func checkExitCode(err error) *int {
	if err == nil {
		return new(0)
	}
	var exit *exec.ExitError
	if errors.As(err, &exit) {
		if code := exit.ExitCode(); code >= 0 {
			return &code
		}
	}
	return nil
}
