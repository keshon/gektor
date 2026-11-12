package tools

import (
	"context"
	"encoding/json"
	"fmt"
	"image"
	_ "image/gif"
	_ "image/jpeg"
	"image/png"
	"io"
	"os"
	"path/filepath"
	"sync"

	_ "golang.org/x/image/bmp"
	"golang.org/x/image/draw"
	_ "golang.org/x/image/webp"

	"github.com/keshon/gektor/internal/agent"
	"github.com/keshon/gektor/internal/llm"
	"github.com/keshon/gektor/internal/userdirs"
	"github.com/keshon/gektor/internal/workspace"
)

type ImageAttachments struct{}

func (ImageAttachments) Name() string         { return "image_attachments" }
func (ImageAttachments) ReadOnly() bool       { return true }
func (ImageAttachments) Mode() agent.ToolMode { return agent.Concurrent }
func (ImageAttachments) Description() string {
	return "List session image IDs and original pixel dimensions for image tools, including pasted pictures and browser screenshots. No workspace copy needed."
}
func (ImageAttachments) Schema() json.RawMessage {
	return json.RawMessage(`{"type":"object","properties":{},"additionalProperties":false}`)
}
func (ImageAttachments) Run(ctx context.Context, args json.RawMessage) (string, error) {
	if err := agent.RejectUnknownFields(args); err != nil {
		return "", err
	}
	type item struct {
		llm.Attachment
		Width  int    `json:"width,omitempty"`
		Height int    `json:"height,omitempty"`
		Error  string `json:"error,omitempty"`
	}
	items := []item{}
	for _, a := range llm.Attachments(ctx) {
		if err := ctx.Err(); err != nil {
			return "", err
		}
		entry := item{Attachment: a}
		path, _ := llm.ResolveAttachment(ctx, a.ID)
		f, err := os.Open(path)
		if err == nil {
			var config image.Config
			config, _, err = image.DecodeConfig(io.LimitReader(f, llm.MaxImageBytes))
			f.Close()
			entry.Width, entry.Height = config.Width, config.Height
		}
		if err != nil {
			entry.Error = "Image unavailable: " + err.Error()
		}
		items = append(items, entry)
	}
	b, err := json.Marshal(items)
	return string(b), err
}

// ImageView keeps generated display copies out of the user's project.
type ImageView struct {
	WS     *workspace.Workspace
	Vision bool
	mu     sync.Mutex
	images map[string][][]string
}

func (*ImageView) Name() string         { return "image_view" }
func (*ImageView) ReadOnly() bool       { return true }
func (*ImageView) Mode() agent.ToolMode { return agent.Exclusive }
func (*ImageView) Description() string {
	return "View pixels from a workspace path or attachment:N. Optional crop uses original pixel coordinates; width resizes the view with aspect ratio preserved. Originals stay unchanged. Requires vision."
}
func (*ImageView) Schema() json.RawMessage {
	return json.RawMessage(`{"type":"object","properties":{"path":{"type":"string","minLength":1},"crop":{"type":"object","properties":{"x":{"type":"integer","minimum":0},"y":{"type":"integer","minimum":0},"width":{"type":"integer","minimum":1},"height":{"type":"integer","minimum":1}},"required":["x","y","width","height"],"additionalProperties":false},"width":{"type":"integer","minimum":1,"maximum":4096}},"required":["path"],"additionalProperties":false}`)
}
func (t *ImageView) Run(ctx context.Context, args json.RawMessage) (string, error) {
	if !t.Vision {
		return "", fmt.Errorf("image_view requires a vision-capable model")
	}
	if err := agent.NewRegistry(t).ValidateCall(t.Name(), args); err != nil {
		return "", err
	}
	if err := ctx.Err(); err != nil {
		return "", err
	}
	var in struct {
		Path  string
		Crop  *struct{ X, Y, Width, Height int }
		Width int
	}
	if err := json.Unmarshal(args, &in); err != nil {
		return "", err
	}
	if t.WS == nil {
		return "", fmt.Errorf("workspace unavailable")
	}
	source, err := llm.OpenImage(ctx, in.Path, t.WS.Open)
	if err != nil {
		return "", err
	}
	defer source.Close()
	info, err := source.Stat()
	if err != nil {
		return "", err
	}
	if !info.Mode().IsRegular() || info.Size() > llm.MaxImageBytes {
		return "", fmt.Errorf("image must be a regular file of at most 20 MiB")
	}
	config, _, err := image.DecodeConfig(source)
	if err != nil {
		return "", err
	}
	if config.Width <= 0 || config.Height <= 0 || int64(config.Width)*int64(config.Height) > 32_000_000 {
		return "", fmt.Errorf("image exceeds 32 megapixels")
	}
	if _, err = source.Seek(0, 0); err != nil {
		return "", err
	}
	var saved string
	width, height := config.Width, config.Height
	if in.Crop == nil && in.Width == 0 {
		mime, err := llm.ImageMIME(source)
		if err != nil {
			return "", err
		}
		name := filepath.Base(source.Name())
		if !llm.IsImagePath(name) {
			name = "view" + llm.ImageExtension(mime)
		}
		saved, err = llm.SnapshotImageFile(source, name, userdirs.Sub("images"))
		if err != nil {
			return "", err
		}
	} else {
		area := image.Rect(0, 0, width, height)
		if c := in.Crop; c != nil {
			if c.X >= width || c.Y >= height || c.Width > width-c.X || c.Height > height-c.Y {
				return "", fmt.Errorf("crop exceeds image bounds %dx%d", width, height)
			}
			area = image.Rect(c.X, c.Y, c.X+c.Width, c.Y+c.Height)
		}
		width, height = area.Dx(), area.Dy()
		if in.Width > 0 {
			height = max(1, int(int64(height)*int64(in.Width)/int64(width)))
			width = in.Width
		}
		if int64(width)*int64(height) > 16_000_000 {
			return "", fmt.Errorf("view exceeds 16 megapixels")
		}
		original, _, err := image.Decode(source)
		if err != nil {
			return "", err
		}
		view := image.NewNRGBA(image.Rect(0, 0, width, height))
		draw.CatmullRom.Scale(view, view.Bounds(), original, area, draw.Src, nil)
		if err := ctx.Err(); err != nil {
			return "", err
		}
		if err := os.MkdirAll(userdirs.Sub("images"), 0700); err != nil {
			return "", err
		}
		f, err := os.CreateTemp(userdirs.Sub("images"), ".view-*.png")
		if err != nil {
			return "", err
		}
		defer os.Remove(f.Name())
		defer f.Close()
		if err := png.Encode(f, view); err != nil {
			return "", err
		}
		saved, err = llm.SnapshotImageFile(f, "view.png", userdirs.Sub("images"))
		if err != nil {
			return "", err
		}
	}
	t.mu.Lock()
	if t.images == nil {
		t.images = map[string][][]string{}
	}
	t.images[string(args)] = append(t.images[string(args)], []string{saved})
	t.mu.Unlock()
	b, err := json.Marshal(map[string]any{"source": in.Path, "source_width": config.Width, "source_height": config.Height, "view_width": width, "view_height": height, "source_written": false})
	return string(b), err
}
func (t *ImageView) Attach(args json.RawMessage) ([]string, error) {
	t.mu.Lock()
	defer t.mu.Unlock()
	queue := t.images[string(args)]
	if len(queue) == 0 {
		return nil, fmt.Errorf("image view unavailable")
	}
	paths := queue[0]
	if len(queue) == 1 {
		delete(t.images, string(args))
	} else {
		t.images[string(args)] = queue[1:]
	}
	return paths, nil
}
