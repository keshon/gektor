package tui

import (
	"context"
	"fmt"
	"time"

	tea "charm.land/bubbletea/v2"
	"github.com/keshon/tars/internal/api"
	"github.com/keshon/tars/internal/speech"
)

type microphoneReadyMsg struct{ revision uint64 }
type captureFinishedMsg struct{ revision uint64 }
type recordedMsg struct {
	revision   uint64
	transcript speech.Transcript
	err        error
}

func (m *model) emitVoice(ctx context.Context, phase string) {
	select {
	case m.events <- api.Event{Name: "voice_phase", Fields: map[string]any{"text": phase}}:
	case <-ctx.Done():
	}
}
func (m *model) enableVoice() error {
	if m.audio() == nil || m.audio().NewVoice == nil {
		return fmt.Errorf("this profile has no speech configuration")
	}
	base := m.base
	if base == nil {
		base = context.Background()
	}
	if err := m.runtime().EnableVoice(base); err != nil {
		return err
	}

	m.voiceEnabled = true
	if m.audio().Profile.Speech != nil {
		m.audio().Profile.Speech.Enabled = true
	}
	return nil
}
func (m *model) toggleVoice() {
	if m.state != stDone {
		return
	}
	if m.voiceEnabled {
		m.stopListening()
		m.voiceEnabled = false
		if m.audio().Profile.Speech != nil {
			m.audio().Profile.Speech.Enabled = false
		}
		m.runtime().DisableVoice()
		m.appendBlock(markerBlock("Voice disabled; text remains available."))
		return
	}
	if err := m.enableVoice(); err != nil {
		m.appendBlock(errorBlock("Cannot enable speech: " + err.Error()))
		return
	}
	m.appendBlock(markerBlock("Speech enabled · English · F8/Ctrl+R record · first reply loads the speech model"))
}
func (m *model) recordVoice() {
	if m.listening != nil {
		m.stopListening()
	}
	m.closeHeader()
	if m.state == stRecording {
		m.recording.Stop()
		m.state = stRecognizing
		m.voicePhase = "Recognizing your speech"
		m.fitBottom()
		return
	}
	if m.state != stDone || m.audio() == nil || m.dialog != nil || m.sessions != nil {
		return
	}
	if !m.voiceEnabled {
		if err := m.enableVoice(); err != nil {
			m.appendBlock(errorBlock(err.Error()))
			return
		}
	}
	base := m.base
	if base == nil {
		base = context.Background()
	}
	ctx, cancel := context.WithCancel(base)
	m.cancel()
	m.ctx = ctx
	m.cancel = cancel
	capture, err := m.runtime().Voice.Record(ctx)
	if err != nil {
		cancel()
		m.appendBlock(errorBlock(err.Error()))
		return
	}
	m.recording = capture
	m.captureRevision++
	revision := m.captureRevision
	service := m.runtime().Voice
	m.started = time.Now()
	m.elapsed = 0
	m.state = stRecording
	m.voicePhase = "Starting microphone — wait for ready"
	m.fitBottom()
	done := make(chan struct{})
	if ready, ok := capture.(interface{ Ready() <-chan struct{} }); ok {
		go func() {
			select {
			case <-ready.Ready():
				m.send(microphoneReadyMsg{revision})
			case <-ctx.Done():
			case <-done:
			}
		}()
	}
	go func() {
		wav, err := capture.Run()
		close(done)
		if err == nil {
			m.send(captureFinishedMsg{revision})
		}
		var tx speech.Transcript
		if err == nil {
			tx, err = service.Recognize(ctx, wav)
		}
		m.send(recordedMsg{revision, tx, err})
	}()
}
func (m *model) finishRecording(msg recordedMsg) (tea.Model, tea.Cmd) {
	if msg.revision != m.captureRevision {
		return m, nil
	}
	m.recording = nil
	m.state = stDone
	m.voicePhase = ""
	m.fitBottom()
	if msg.err != nil {
		if m.ctx.Err() == nil {
			m.appendBlock(errorBlock("Voice input failed: " + msg.err.Error()))
		}
		return m, nil
	}
	draft := m.input.Value()
	m.input.SetValue(msg.transcript.Text)
	model, cmd := m.followUp()
	m.input.SetValue(draft)
	m.fitInput()
	m.fitBottom()
	return model, cmd
}
func (m *model) cancelRecording() {
	m.cancel()
	m.captureRevision++
	m.recording = nil
	m.state = stDone
	m.voicePhase = ""
	m.fitBottom()
	m.appendBlock(markerBlock("Recording cancelled."))
}

func (m *model) playGreeting() {
	if m.state != stDone || m.chat == nil || m.chat.config.Settings.Greeting == "" {
		return
	}
	if !m.voiceEnabled {
		if err := m.enableVoice(); err != nil {
			m.appendBlock(errorBlock(err.Error()))
			return
		}
	}
	service, text := m.runtime().Voice, m.chat.config.Settings.Greeting
	m.startRun(func(ctx context.Context) (string, error) {
		output := speech.NewOutput(ctx, service, func(phase string) { m.emitVoice(ctx, phase) })
		if err := output.Delta(text); err != nil {
			output.Cancel()
			output.Finish()
			return "", err
		}
		return "", output.Finish()
	})
}

func (m *model) registerSpeech(cancel context.CancelFunc) func() {
	m.speechMu.Lock()
	m.speechCancel = cancel
	m.speechRevision++
	revision := m.speechRevision
	m.speechMu.Unlock()
	return func() {
		m.speechMu.Lock()
		if m.speechRevision == revision {
			m.speechCancel = nil
		}
		m.speechMu.Unlock()
		cancel()
	}
}
func (m *model) hasPlayback() bool {
	m.speechMu.Lock()
	defer m.speechMu.Unlock()
	return m.speechCancel != nil
}
func (m *model) stopPlayback() {
	m.speechMu.Lock()
	cancel := m.speechCancel
	m.speechCancel = nil
	m.speechMu.Unlock()
	if cancel != nil {
		cancel()
		m.voicePhase = "Speech stopped; text generation continues"
	}
}
