package process

import (
	"context"
	"io"
	"os"
	"testing"
	"time"
)

func TestProcessHelper(t *testing.T) {
	if os.Args[len(os.Args)-1] != "owned-helper" {
		return
	}
	_, _ = os.Stdout.WriteString("ready")
	time.Sleep(30 * time.Second)
	os.Exit(0)
}
func TestOwnedChildCancellationIsJoined(t *testing.T) {
	binary, err := os.Executable()
	if err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	c, err := Start(ctx, binary, t.TempDir(), []string{"-test.run=TestProcessHelper", "--", "owned-helper"}, io.Discard)
	if err != nil {
		t.Fatal(err)
	}
	if c.Exited() {
		t.Fatal("live child reported as exited")
	}
	cancel()
	done := make(chan error, 1)
	go func() { done <- c.Wait() }()
	select {
	case err := <-done:
		if err == nil {
			t.Fatal("canceled child succeeded")
		}
	case <-time.After(5 * time.Second):
		t.Fatal("child not joined")
	}
	if !c.Exited() {
		t.Fatal("joined child reported as live")
	}
	c.Stop()
}
