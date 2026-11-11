package agent

import (
	"context"
	"testing"
	"time"

	"github.com/keshon/gektor/internal/events"
)

func TestSessionEventQueueCancellationReleasesPublisher(t *testing.T) {
	ctx, cancel := context.WithCancel(t.Context())
	defer cancel()
	queue := make(chan events.Event)
	sink := sessionEventQueue{ctx: ctx, queue: queue}
	done := make(chan struct{})
	go func() {
		sink.Handle(events.Event{Name: "delta"})
		close(done)
	}()

	cancel()
	select {
	case <-done:
	case <-time.After(5 * time.Second):
		// Release and join even a publisher that regressed to an unconditional send.
		select {
		case <-queue:
		case <-done:
		}
		<-done
		t.Fatal("cancelled screen retained a blocked publisher")
	}
}
