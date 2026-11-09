package tools

import (
	"context"
	"encoding/json"
	"fmt"
	"image"
	"io"
	"net/http"
	"os"
	"sync"

	"github.com/keshon/gektor/internal/agent"
	"github.com/keshon/gektor/internal/gektor"
	"github.com/keshon/gektor/internal/httptransport"
	"github.com/keshon/gektor/internal/llm"
)

type ImageDownload struct {
	Vision    bool
	transport http.RoundTripper
	mu        sync.Mutex
	images    map[string][][]string
}

func (*ImageDownload) Name() string         { return "image_download" }
func (*ImageDownload) ReadOnly() bool       { return true }
func (*ImageDownload) Mode() agent.ToolMode { return agent.Exclusive }
func (*ImageDownload) Description() string {
	return "Download a public image to session storage and view it. Reports source/final URL, detected format and dimensions. List image_attachments afterward to reuse its ID. No project files written. Requires vision."
}
func (*ImageDownload) Schema() json.RawMessage {
	return json.RawMessage(`{"type":"object","properties":{"url":{"type":"string","minLength":1}},"required":["url"],"additionalProperties":false}`)
}
func (t *ImageDownload) Run(ctx context.Context, args json.RawMessage) (string, error) {
	if !t.Vision {
		return "", fmt.Errorf("image_download requires a vision-capable model")
	}
	if err := agent.NewRegistry(t).ValidateCall(t.Name(), args); err != nil {
		return "", err
	}
	var in struct{ URL string }
	if err := json.Unmarshal(args, &in); err != nil {
		return "", err
	}
	url, err := httptransport.Normalize(in.URL, false)
	if err != nil {
		return "", err
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
	if err != nil {
		return "", err
	}
	req.Header.Set("User-Agent", "GEKTOR/1.0 (image reader)")
	req.Header.Set("Accept", "image/png,image/jpeg,image/webp,image/gif,image/bmp")
	resp, err := httptransport.Client(false, t.transport).Do(req)
	if err != nil {
		return "", err
	}
	defer resp.Body.Close()
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return "", fmt.Errorf("image download: HTTP %d", resp.StatusCode)
	}
	dir := gektor.Sub("images")
	if err := os.MkdirAll(dir, 0700); err != nil {
		return "", err
	}
	f, err := os.CreateTemp(dir, ".download-*")
	if err != nil {
		return "", err
	}
	defer os.Remove(f.Name())
	defer f.Close()
	n, err := io.Copy(f, io.LimitReader(resp.Body, llm.MaxImageBytes+1))
	if err != nil {
		return "", err
	}
	if n > llm.MaxImageBytes {
		return "", fmt.Errorf("image download exceeds 20 MiB")
	}
	mime, err := llm.ImageMIME(f)
	if err != nil {
		return "", err
	}
	if _, err := f.Seek(0, 0); err != nil {
		return "", err
	}
	config, _, err := image.DecodeConfig(f)
	if err != nil {
		return "", err
	}
	if config.Width <= 0 || config.Height <= 0 || int64(config.Width)*int64(config.Height) > 32_000_000 {
		return "", fmt.Errorf("image exceeds 32 megapixels")
	}
	saved, err := llm.SnapshotImageFile(f, "download"+llm.ImageExtension(mime), dir)
	if err != nil {
		return "", err
	}
	t.mu.Lock()
	if t.images == nil {
		t.images = map[string][][]string{}
	}
	t.images[string(args)] = append(t.images[string(args)], []string{saved})
	t.mu.Unlock()
	b, err := json.Marshal(map[string]any{"source_url": in.URL, "final_url": resp.Request.URL.String(), "mime": mime, "bytes": n, "width": config.Width, "height": config.Height})
	return string(b), err
}
func (t *ImageDownload) Attach(args json.RawMessage) ([]string, error) {
	t.mu.Lock()
	defer t.mu.Unlock()
	queue := t.images[string(args)]
	if len(queue) == 0 {
		return nil, fmt.Errorf("downloaded image unavailable")
	}
	paths := queue[0]
	if len(queue) == 1 {
		delete(t.images, string(args))
	} else {
		t.images[string(args)] = queue[1:]
	}
	return paths, nil
}
