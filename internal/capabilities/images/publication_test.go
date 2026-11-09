package images

import (
	"context"
	"os"
	"testing"

	"github.com/keshon/gektor/internal/workspace"
)

func TestCancelledPublicationCreatesNothing(t *testing.T) {
	ws, err := workspace.New(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	defer ws.Close()
	ctx, cancel := context.WithCancel(t.Context())
	cancel()
	if err := publish(ctx, ws, t.TempDir(), "new/output"); err == nil {
		t.Fatal("cancelled publication succeeded")
	}
	if _, err := ws.Stat("new"); !os.IsNotExist(err) {
		t.Fatal("cancelled publication changed workspace")
	}
}
