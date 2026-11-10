package images

import (
	"bytes"
	"context"
	"errors"
	"image"
	"image/png"
	"os"
	"testing"

	"github.com/keshon/gektor/internal/workspace"
)

type cancellingInputs struct {
	ws     *workspace.Workspace
	cancel context.CancelFunc
	opens  int
}

func (*cancellingInputs) IsReference(string) bool { return false }
func (s *cancellingInputs) Open(ctx context.Context, path string) (*os.File, error) {
	s.opens++
	if s.opens == 3 {
		s.cancel()
	}
	return s.ws.Open(path)
}

func TestComparisonDoesNotReportSuccessAfterCancellation(t *testing.T) {
	ws, err := workspace.New(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	defer ws.Close()
	var imageBytes bytes.Buffer
	if err := png.Encode(&imageBytes, image.NewNRGBA(image.Rect(0, 0, 20, 10))); err != nil {
		t.Fatal(err)
	}
	if err := ws.WriteFile("input.png", imageBytes.Bytes(), 0600); err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithCancel(t.Context())
	defer cancel()
	sources := &cancellingInputs{ws: ws, cancel: cancel}
	_, err = (Service{WS: ws, Inputs: sources}).Run(ctx, "compare", Request{Paths: []string{"input.png", "input.png"}}, nil)
	if !errors.Is(err, context.Canceled) {
		t.Fatalf("cancelled comparison returned %v", err)
	}
}
