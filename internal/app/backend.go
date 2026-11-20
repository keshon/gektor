package app

import (
	"context"
	"fmt"
	"io"
	"time"

	"github.com/keshon/gektor/internal/llm"
)

type usageClient interface {
	llm.Client

	llm.Streamer

	UsageTotals() (int, int, int)

	CachedTotal() int
}

func (b *Backend) SetDebug(w io.Writer) {

	if b.setDebug != nil {

		b.setDebug(w)

	}

}

func (b *Backend) SetDRY(v bool) {

	if b.setDRY != nil {

		b.setDRY(v)

	}

}

func (b *Backend) MaxContextLength(ctx context.Context) (int, error) {
	ctx, cancel := context.WithTimeout(ctx, 5*time.Second)
	defer cancel()

	if b.inspector == nil {
		return 0, fmt.Errorf("provider does not report context size")
	}

	return b.inspector.MaxContextLength(ctx)
}

func (b *Backend) ModelName(ctx context.Context) (string, error) {
	ctx, cancel := context.WithTimeout(ctx, 5*time.Second)
	defer cancel()

	if b.descriptor.SelectModel {
		return b.model, nil
	}

	if b.inspector == nil {
		return "", fmt.Errorf("model name is unknown")
	}

	return b.inspector.ModelName(ctx)
}

func (b *Backend) SupportsVision(ctx context.Context) (bool, error) {
	ctx, cancel := context.WithTimeout(ctx, 5*time.Second)
	defer cancel()

	if b.visionProbe != nil {
		return b.accountVision(ctx)
	}

	if b.inspector == nil {
		return false, fmt.Errorf("model vision support is unknown")
	}

	return b.inspector.SupportsVision(ctx)
}

// Successful account facts remain stable for this bound backend. Failures leave
// discovery retryable; concurrent callers wait without losing cancellation.
func (b *Backend) accountVision(ctx context.Context) (bool, error) {
	for {
		err := ctx.Err()
		if err != nil {
			return false, err
		}

		b.visionMu.Lock()
		if b.visionKnown {
			vision := b.vision
			b.visionMu.Unlock()
			return vision, nil
		}
		if pending := b.visionLoading; pending != nil {
			b.visionMu.Unlock()
			select {
			case <-pending:
				continue
			case <-ctx.Done():
				return false, ctx.Err()
			}
		}

		pending := make(chan struct{})
		b.visionLoading = pending
		b.visionMu.Unlock()

		vision, err := b.visionProbe(ctx)
		b.visionMu.Lock()
		if err == nil {
			b.vision, b.visionKnown = vision, true
		}
		b.visionLoading = nil
		close(pending)
		b.visionMu.Unlock()
		return vision, err
	}
}

func (b *Backend) checkImages(ctx context.Context, req llm.ChatRequest) error {
	for _, message := range req.Messages {
		if len(message.Images) > 0 {
			vision, err := b.SupportsVision(ctx)
			if err == nil && !vision {
				return fmt.Errorf("model %s does not support image input; select a vision model", b.model)
			}

			return nil
		}
	}

	return nil
}

func (b *Backend) Chat(ctx context.Context, req llm.ChatRequest) (llm.ChatResponse, error) {

	if err := b.checkImages(ctx, req); err != nil {

		return llm.ChatResponse{}, err

	}

	return b.usageClient.Chat(ctx, req)

}

func (b *Backend) Stream(ctx context.Context, req llm.ChatRequest, reasoning, text func(string)) (llm.ChatResponse, error) {

	if err := b.checkImages(ctx, req); err != nil {

		return llm.ChatResponse{}, err

	}

	return b.usageClient.Stream(ctx, req, reasoning, text)

}
