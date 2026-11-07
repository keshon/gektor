package tools

import (
	"bytes"
	"crypto/sha256"
	"encoding/json"
	"image"
	"image/color"
	"image/png"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/keshon/gektor/internal/agent"
	"github.com/keshon/gektor/internal/llm"
	"github.com/keshon/gektor/internal/workspace"
)

func imageViewFixture(t *testing.T) (*workspace.Workspace, string, []byte) {
	t.Helper()
	t.Setenv("USERPROFILE", t.TempDir())
	t.Setenv("HOME", t.TempDir())
	ws, err := workspace.New(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { ws.Close() })
	original := image.NewNRGBA(image.Rect(0, 0, 20, 10))
	for y := 0; y < 10; y++ {
		for x := 10; x < 20; x++ {
			original.Set(x, y, color.NRGBA{R: 255, A: 255})
		}
	}
	var b bytes.Buffer
	if err := png.Encode(&b, original); err != nil {
		t.Fatal(err)
	}
	path := filepath.Join(t.TempDir(), "pasted.jpg")
	if err := os.WriteFile(path, b.Bytes(), 0600); err != nil {
		t.Fatal(err)
	}
	return ws, path, b.Bytes()
}

func TestImageViewCropsAttachmentWithoutWorkspaceCopy(t *testing.T) {
	ws, path, data := imageViewFixture(t)
	ctx := llm.WithAttachments(t.Context(), []llm.Message{{Role: llm.RoleUser, Images: []string{path}}})
	listed, err := (ImageAttachments{}).Run(ctx, json.RawMessage(`{}`))
	if err != nil || !strings.Contains(listed, `"width":20`) || !strings.Contains(listed, `"height":10`) {
		t.Fatal(listed, err)
	}
	args := json.RawMessage(`{"path":"attachment:1","crop":{"x":10,"y":0,"width":10,"height":10},"width":40}`)
	tool := &ImageView{WS: ws, Vision: true}
	out, err := tool.Run(ctx, args)
	if err != nil || !strings.Contains(out, `"view_width":40`) {
		t.Fatal(out, err)
	}
	paths, err := tool.Attach(args)
	if err != nil || len(paths) != 1 {
		t.Fatal(paths, err)
	}
	f, err := os.Open(paths[0])
	if err != nil {
		t.Fatal(err)
	}
	defer f.Close()
	viewed, _, err := image.Decode(f)
	if err != nil || viewed.Bounds().Dx() != 40 || viewed.Bounds().Dy() != 40 {
		t.Fatal(viewed, err)
	}
	r, g, b, _ := viewed.At(20, 20).RGBA()
	if r == 0 || g != 0 || b != 0 {
		t.Fatal("wrong crop pixels", r, g, b)
	}
	after, _ := os.ReadFile(path)
	if sha256.Sum256(after) != sha256.Sum256(data) {
		t.Fatal("source changed")
	}
	entries, _ := os.ReadDir(ws.Root())
	if len(entries) != 0 {
		t.Fatal("polluted workspace", entries)
	}
	for _, bad := range []string{`{"path":"attachment:2"}`, `{"path":"attachment:1","crop":{"x":19,"y":0,"width":2,"height":1}}`, `{"path":"attachment:1","crop":{"x":-1,"y":0,"width":1,"height":1}}`} {
		if _, err := tool.Run(ctx, json.RawMessage(bad)); err == nil {
			t.Fatal("accepted", bad)
		}
	}
	if _, err := tool.Run(t.Context(), json.RawMessage(`{"path":"attachment:1"}`)); err == nil {
		t.Fatal("attachment leaked to another session")
	}
	raw, _ := json.Marshal(map[string]string{"path": path})
	if _, err := tool.Run(ctx, raw); err == nil {
		t.Fatal("accepted arbitrary path outside workspace")
	}
	if _, err := (&ImageView{WS: ws}).Run(ctx, args); err == nil {
		t.Fatal("allowed view without vision")
	}
	resources, err := PathResources(ws, "image_view", args)
	if err != nil || len(resources) != 1 || resources[0] != "attachment:1" {
		t.Fatal(resources, err)
	}
}

func TestReadFileMisnamedImageReportsActualFormat(t *testing.T) {
	ws, _, data := imageViewFixture(t)
	if err := ws.WriteFile("reference.jpg", data, 0600); err != nil {
		t.Fatal(err)
	}
	reader := ReadFile{WS: ws, Vision: true}
	args := json.RawMessage(`{"path":"reference.jpg"}`)
	out, err := reader.Run(t.Context(), args)
	if err != nil || !strings.Contains(out, "format: png") {
		t.Fatal(out, err)
	}
	paths, err := reader.Attach(args)
	if err != nil || len(paths) != 1 || filepath.Ext(paths[0]) != ".png" {
		t.Fatal(paths, err)
	}
}

func TestImageDownloadDetectsContentAndRejectsInvalidDestinations(t *testing.T) {
	ws, _, data := imageViewFixture(t)
	tool := &ImageDownload{Vision: true, transport: roundTripFunc(func(r *http.Request) (*http.Response, error) {
		if r.Header.Get("User-Agent") == "" {
			t.Fatal("missing user agent")
		}
		return &http.Response{StatusCode: 200, Header: http.Header{"Content-Type": []string{"image/jpeg"}}, Body: io.NopCloser(bytes.NewReader(data)), Request: r}, nil
	})}
	args := json.RawMessage(`{"url":"https://example.org/photo.jpg"}`)
	out, err := tool.Run(t.Context(), args)
	if err != nil || !strings.Contains(out, `"mime":"image/png"`) || !strings.Contains(out, `"final_url":"https://example.org/photo.jpg"`) {
		t.Fatal(out, err)
	}
	paths, err := tool.Attach(args)
	if err != nil || len(paths) != 1 || filepath.Ext(paths[0]) != ".png" {
		t.Fatal(paths, err)
	}
	entries, _ := os.ReadDir(ws.Root())
	if len(entries) != 0 {
		t.Fatal(entries)
	}
	for _, url := range []string{"http://127.0.0.1/image.png", "http://169.254.169.254/image.png", "file:///secret.png"} {
		raw, _ := json.Marshal(map[string]string{"url": url})
		if _, err := tool.Run(t.Context(), raw); err == nil {
			t.Fatal("accepted", url)
		}
	}
	tool.transport = roundTripFunc(func(r *http.Request) (*http.Response, error) {
		return &http.Response{StatusCode: 200, Header: http.Header{}, Body: io.NopCloser(strings.NewReader("<html>error page</html>")), Request: r}, nil
	})
	if _, err := tool.Run(t.Context(), args); err == nil {
		t.Fatal("downloaded HTML as image")
	}
	tool.transport = roundTripFunc(func(r *http.Request) (*http.Response, error) {
		return &http.Response{StatusCode: 302, Header: http.Header{"Location": []string{"http://127.0.0.1/secret"}}, Body: io.NopCloser(strings.NewReader("")), Request: r}, nil
	})
	if _, err := tool.Run(t.Context(), args); err == nil {
		t.Fatal("followed private redirect")
	}
}

func TestImageToolsStayLazy(t *testing.T) {
	ws, _, _ := imageViewFixture(t)
	catalog := &Capabilities{}
	reg, err := Compose(ws, nil, nil, nil, true, catalog, nil)
	if err != nil {
		t.Fatal(err)
	}
	catalog.Registry = reg
	for _, name := range []string{"image_view", "image_download", "image_attachments"} {
		if reg.Has(name) {
			t.Fatal("image schema added to startup", name)
		}
	}
	if _, err := catalog.Enable(t.Context(), "native_images", nil); err != nil {
		t.Fatal(err)
	}
	for _, name := range []string{"image_view", "image_download", "image_attachments"} {
		if !reg.Has(name) {
			t.Fatal("missing lazy image tool", name)
		}
	}
	if err := agent.NewRegistry(&ImageView{WS: ws, Vision: true}, &ImageDownload{Vision: true}, ImageAttachments{}).Err(); err != nil {
		t.Fatal(err)
	}
}
