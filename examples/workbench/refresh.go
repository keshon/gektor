package main

import (
	"cmp"
	"context"
	"slices"
	"time"

	tea "charm.land/bubbletea/v2"
)

type refreshJob struct {
	cancel context.CancelFunc
	result chan refreshDone
}

type refreshDone struct {
	job   *refreshJob
	books []book
	err   error
}

func (m *model) cancelRefresh() {
	if m.pending != nil {
		m.pending.cancel()
		m.pending = nil
		m.status = "Refresh cancelled. Books stay in memory."
	}
}

// The delay makes cancellation visible without files or external services.
// Workers receive a snapshot and a buffered result channel, never UI state.
func (m *model) startRefresh() tea.Cmd {
	m.cancelRefresh()
	ctx, cancel := context.WithCancel(context.Background())
	job := &refreshJob{cancel: cancel, result: make(chan refreshDone, 1)}
	snapshot := slices.Clone(m.books)
	m.pending = job
	m.status = "Refreshing… Ctrl+R replaces this refresh; opening a book cancels it."

	m.workers.Add(1)
	workers := &m.workers
	go func() {
		defer workers.Done()
		timer := time.NewTimer(250 * time.Millisecond)
		defer timer.Stop()
		select {
		case <-ctx.Done():
			job.result <- refreshDone{job: job, err: ctx.Err()}
		case <-timer.C:
			slices.SortFunc(snapshot, func(a, b book) int { return cmp.Compare(a.Title, b.Title) })
			job.result <- refreshDone{job: job, books: snapshot}
		}
	}()
	return func() tea.Msg { return <-job.result }
}
