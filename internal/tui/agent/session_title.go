package agent

import (
	"context"
	"path/filepath"
	"strings"
	"time"

	"github.com/keshon/gektor/internal/app"
	"github.com/keshon/gektor/internal/conversation"
)

type sessionTitleJob struct {
	stateFile, request, fallback string
	cancel                       context.CancelFunc
}

type sessionTitleMsg struct {
	job   *sessionTitleJob
	title string
}

func (m *Model) prepareSessionTitle(request string) {
	m.cancelSessionTitle()
	if m.stateFile == "" || strings.TrimSpace(request) == "" || m.baseEnv.Client == nil {
		return
	}
	dir := filepath.Dir(m.stateFile)
	if conversation.ReadSessionTitle(dir) != m.sessionTitle {
		return
	}
	m.titleJob = &sessionTitleJob{stateFile: m.stateFile, request: request, fallback: m.sessionTitle}
}

func (m *Model) cancelSessionTitle() {
	if m.titleJob != nil && m.titleJob.cancel != nil {
		m.titleJob.cancel()
	}
	m.titleJob = nil
}

// sessionTitleRequest captures the title job on the UI thread.
func (m *Model) sessionTitleRequest(ctx context.Context) func() sessionTitleMsg {
	j := m.titleJob
	if j == nil || j.cancel != nil {
		return nil
	}
	if j.stateFile != m.stateFile || m.sessionTitle != j.fallback || conversation.ReadSessionTitle(filepath.Dir(j.stateFile)) != j.fallback {
		m.cancelSessionTitle()
		return nil
	}

	ctx, cancel := context.WithTimeout(ctx, 8*time.Second)
	j.cancel = cancel
	client := m.baseEnv.Client
	return func() sessionTitleMsg {
		defer cancel()
		title, _ := app.GenerateSessionTitle(ctx, client, j.request)
		return sessionTitleMsg{job: j, title: title}
	}
}

func (m *Model) acceptSessionTitle(msg sessionTitleMsg) {
	if m.titleJob != msg.job || msg.job == nil {
		return
	}
	m.cancelSessionTitle()
	j := msg.job
	if msg.title == "" || m.stateFile != j.stateFile || m.sessionTitle != j.fallback {
		return
	}
	dir := filepath.Dir(j.stateFile)
	// Read the existing file only; never recreate a deleted session or replace
	// a title changed outside this UI while the request was in flight.
	if conversation.ReadSessionTitle(dir) != j.fallback {
		return
	}
	if err := conversation.WriteSessionTitle(dir, msg.title); err != nil {
		return
	}
	m.updateSessionTitle(dir, msg.title)
	m.refreshSessions()
}
