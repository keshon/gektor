package workflow_test

import (
	"context"
	"testing"

	"github.com/keshon/gektor/internal/permission"
	"github.com/keshon/gektor/internal/tui/components/pane"
	"github.com/keshon/gektor/internal/tui/screen"
	"github.com/keshon/gektor/internal/tui/workflow"
	"github.com/keshon/gektor/internal/workspace"
)

func TestScreenUsesCurrentSurfaceAndSharedDialogs(t *testing.T) {
	t.Setenv("USERPROFILE", t.TempDir())
	t.Setenv("HOME", t.TempDir())
	ws, err := workspace.New(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	defer ws.Close()
	surface := screen.Surface{Area: pane.Rect{Y: 1, Width: 120, Height: 28}, Inset: 1, SplitWidth: 100}
	controls := &screen.Controls{}
	child, err := workflow.New(context.Background(), workflow.Config{WS: ws, Policy: permission.Default()}, controls, surface)
	if err != nil {
		t.Fatal(err)
	}
	defer child.Close()
	if child.State().Active || child.State().Busy || child.Dirty() {
		t.Fatal("connector started work or edited the draft")
	}
	child.Activate()
	child.Command("help")
	if controls.Dialog.Title != "Workflow keys" {
		t.Fatal("shared dialog host was not connected", controls.Dialog.Title)
	}
	width, height := child.CanvasSize()
	surface.Area.Width, surface.Area.Height = 60, 18
	child.SetSurface(surface)
	narrow, short := child.CanvasSize()
	if narrow >= width || short >= height {
		t.Fatalf("screen retained stale geometry: %dx%d -> %dx%d", width, height, narrow, short)
	}
	child.Hide()
	if child.State().Active {
		t.Fatal("hidden screen retained input ownership")
	}
}
