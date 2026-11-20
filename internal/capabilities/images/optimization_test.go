package images

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"image"
	"image/color"
	"image/png"
	"os"
	"path/filepath"
	"testing"

	"github.com/keshon/gektor/internal/workspace"
	_ "golang.org/x/image/webp"
)

func optimizationFixture(t *testing.T) (*workspace.Workspace, []byte) {
	t.Helper()
	ws, err := workspace.New(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { ws.Close() })
	img := image.NewNRGBA(image.Rect(0, 0, 128, 128))
	for y := range 128 {
		for x := range 128 {
			img.SetNRGBA(x, y, color.NRGBA{uint8(x / 8 * 16), uint8(y / 8 * 16), 128, 255})
		}
	}
	var buf bytes.Buffer
	encoder := png.Encoder{CompressionLevel: png.NoCompression}
	if err := encoder.Encode(&buf, img); err != nil {
		t.Fatal(err)
	}
	if err := ws.WriteFile("input.png", buf.Bytes(), 0600); err != nil {
		t.Fatal(err)
	}
	return ws, buf.Bytes()
}

func TestOptimizePublishesOriginalSizeConversionAndQuantization(t *testing.T) {
	for _, format := range []string{"keep", "webp"} {
		t.Run(format, func(t *testing.T) {
			ws, before := optimizationFixture(t)
			service := Service{WS: ws}
			preview, err := service.Run(t.Context(), "preview", Request{Paths: []string{"input.png"}, Format: format}, nil)
			if err != nil {
				t.Fatal(err)
			}
			var planned struct{ Items []struct{ Output string } }
			if err := json.Unmarshal([]byte(preview), &planned); err != nil {
				t.Fatal(err)
			}
			if len(planned.Items) != 1 || planned.Items[0].Output == "" {
				t.Fatalf("no preview output: %s", preview)
			}
			var authorized []string
			report, err := service.Run(t.Context(), "optimize", Request{Paths: []string{"input.png"}, Format: format, OutDir: "optimized"}, func(_ context.Context, path string) error {
				authorized = append(authorized, filepath.ToSlash(path))
				return nil
			})
			if err != nil {
				t.Fatalf("%v: %s", err, report)
			}
			output := "optimized/" + planned.Items[0].Output
			if len(authorized) != 1 || authorized[0] != output {
				t.Fatalf("authorized %v, expected %s", authorized, output)
			}
			f, err := ws.Open(output)
			if err != nil {
				t.Fatal(err)
			}
			decoded, actualFormat, err := image.Decode(f)
			f.Close()
			if err != nil {
				t.Fatal(err)
			}
			if decoded.Bounds().Dx() != 128 || decoded.Bounds().Dy() != 128 {
				t.Fatalf("dimensions %v", decoded.Bounds())
			}
			if format == "keep" {
				if _, ok := decoded.(*image.Paletted); !ok || actualFormat != "png" {
					t.Fatalf("not quantized PNG: %T / %s", decoded, actualFormat)
				}
			} else if actualFormat != "webp" {
				t.Fatalf("format %s", actualFormat)
			}
			after, err := ws.ReadFile("input.png")
			if err != nil || !bytes.Equal(before, after) {
				t.Fatalf("source changed: %v", err)
			}
		})
	}
}

func TestOptimizeRequiresOutputPermissionAndPreservesDestination(t *testing.T) {
	ws, _ := optimizationFixture(t)
	service := Service{WS: ws}
	request := Request{Paths: []string{"input.png"}, OutDir: "optimized"}
	denied := errors.New("denied")
	if _, err := service.Run(t.Context(), "optimize", request, func(context.Context, string) error { return denied }); !errors.Is(err, denied) {
		t.Fatalf("permission error %v", err)
	}
	if _, err := ws.Stat("optimized"); !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("denied operation created output: %v", err)
	}
	if err := ws.Mkdir("optimized", 0700); err != nil {
		t.Fatal(err)
	}
	if err := ws.WriteFile("optimized/existing.txt", []byte("preserve"), 0600); err != nil {
		t.Fatal(err)
	}
	if _, err := service.Run(t.Context(), "optimize", request, func(context.Context, string) error { return nil }); !errors.Is(err, os.ErrExist) {
		t.Fatalf("existing destination error %v", err)
	}
	data, err := ws.ReadFile("optimized/existing.txt")
	if err != nil || string(data) != "preserve" {
		t.Fatalf("existing destination changed: %q / %v", data, err)
	}
}

func TestOptimizeDeclinedBatchPublishesNothing(t *testing.T) {
	ws, _ := optimizationFixture(t)
	var tiny bytes.Buffer
	if err := png.Encode(&tiny, image.NewNRGBA(image.Rect(0, 0, 1, 1))); err != nil {
		t.Fatal(err)
	}
	if err := ws.WriteFile("tiny.png", tiny.Bytes(), 0600); err != nil {
		t.Fatal(err)
	}
	report, err := (Service{WS: ws}).Run(t.Context(), "optimize", Request{Paths: []string{"input.png", "tiny.png"}, OutDir: "optimized"}, func(context.Context, string) error {
		t.Fatal("declined batch requested publication permission")
		return nil
	})
	if err == nil {
		t.Fatalf("declined batch succeeded: %s", report)
	}
	var result struct {
		Items []struct {
			Output  string
			Written bool
		}
	}
	if err := json.Unmarshal([]byte(report), &result); err != nil {
		t.Fatal(err)
	}
	for _, item := range result.Items {
		if item.Written || item.Output != "" {
			t.Fatalf("unpublished output reported as written: %s", report)
		}
	}
	if _, err := ws.Stat("optimized"); !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("declined batch created output: %v", err)
	}
}
