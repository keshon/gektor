package tools

import (
	"context"
	"encoding/base64"
	"fmt"
	"os"

	"github.com/keshon/gektor/internal/gektor"
	"github.com/keshon/gektor/internal/llm"
)

type visionContextKey struct{}

func VisionContext(ctx context.Context, enabled bool) context.Context {
	return context.WithValue(ctx, visionContextKey{}, enabled)
}
func saveMCPImage(ctx context.Context, encoded, mime string) (string, error) {
	if enabled, _ := ctx.Value(visionContextKey{}).(bool); !enabled {
		return "", fmt.Errorf("current model has no vision capability; request textual DOM/data instead")
	}
	ext := map[string]string{"image/png": ".png", "image/jpeg": ".jpg", "image/webp": ".webp", "image/gif": ".gif", "image/bmp": ".bmp"}[mime]
	if ext == "" {
		return "", fmt.Errorf("unsupported MIME %s", mime)
	}
	if len(encoded) > base64.StdEncoding.EncodedLen(llm.MaxImageBytes) {
		return "", fmt.Errorf("image exceeds size limit")
	}
	data, err := base64.StdEncoding.DecodeString(encoded)
	if err != nil {
		return "", err
	}
	f, err := os.CreateTemp("", "gektor-mcp-*"+ext)
	if err != nil {
		return "", err
	}
	defer os.Remove(f.Name())
	_, writeErr := f.Write(data)
	closeErr := f.Close()
	if writeErr != nil {
		return "", writeErr
	}
	if closeErr != nil {
		return "", closeErr
	}
	return llm.SnapshotImage(f.Name(), gektor.Sub("images"))
}
