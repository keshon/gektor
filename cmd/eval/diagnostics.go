package main

import (
	"context"
	"time"

	"github.com/keshon/gektor/internal/workspace"
)

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
func checkArtifacts(ctx context.Context, p Probe, ws *workspace.Workspace) []string {
	ctx, cancel := context.WithTimeout(ctx, 2*time.Minute)
	defer cancel()
	return checkWorkspace(ctx, p, ws)
}
