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
		return b.visionProbe(ctx)
	}

	if b.inspector == nil {
		return false, fmt.Errorf("model vision support is unknown")
	}

	return b.inspector.SupportsVision(ctx)
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
