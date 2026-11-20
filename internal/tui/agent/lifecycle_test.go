package agent

import (
	"context"
	"sync"
	"testing"
	"time"

	tea "charm.land/bubbletea/v2"
	"github.com/keshon/gektor/internal/events"
)

func TestCloseJoinsRunFinalizationWithFullEventQueue(t *testing.T) {
	m, err := New(t.Context(), Config{})
	if err != nil {
		t.Fatal(err)
	}
	for range cap(m.events) {
		m.events <- events.Event{Name: "queued"}
	}

	entered := make(chan struct{})
	published := make(chan struct{})
	release := make(chan struct{})
	finalized := make(chan struct{})
	closed := make(chan struct{})
	var unblock sync.Once
	queue := sessionEventQueue{ctx: m.base, queue: m.events}
	m.startRun(func(ctx context.Context) (string, error) {
		close(entered)
		<-ctx.Done()
		queue.Handle(events.Event{Name: "outcome"})
		close(published)
		<-release
		close(finalized)
		return "", ctx.Err()
	})
	t.Cleanup(func() {
		unblock.Do(func() { close(release) })
		select {
		case <-closed:
			m.Close()
		case <-time.After(5 * time.Second):
			t.Error("cleanup did not join Close")
		}
	})
	<-entered
	go func() {
		m.Close()
		close(closed)
	}()

	select {
	case <-published:
	case <-time.After(5 * time.Second):
		t.Fatal("shutdown left final event publication blocked")
	}
	select {
	case <-closed:
		t.Fatal("Close returned before run finalization")
	case <-time.After(50 * time.Millisecond):
	}

	unblock.Do(func() { close(release) })
	select {
	case <-closed:
	case <-time.After(5 * time.Second):
		t.Fatal("Close did not join the completed run")
	}
	select {
	case <-finalized:
	default:
		t.Fatal("Close returned before finalization completed")
	}
	m.startRun(func(context.Context) (string, error) {
		t.Error("closed screen started another run")
		return "", nil
	})
}

func TestCloseRejectsUnstartedBackgroundCommand(t *testing.T) {
	m, err := New(t.Context(), Config{})
	if err != nil {
		t.Fatal(err)
	}
	command := m.workerCommand(func() tea.Msg {
		t.Error("discarded command accessed its services after Close")
		return nil
	})
	m.Close()
	command()
	m.Close()
}
