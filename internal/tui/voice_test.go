package tui

import (
	"context"
	"errors"
	"sync"
	"testing"
	"time"

	tea "charm.land/bubbletea/v2"
	"github.com/keshon/tars/internal/speech"
)

type voiceCapture struct {
	ctx         context.Context
	ready, stop chan struct{}
	once        sync.Once
}

func (c *voiceCapture) Ready() <-chan struct{} { return c.ready }
func (c *voiceCapture) Stop()                  { c.once.Do(func() { close(c.stop) }) }
func (c *voiceCapture) Run() ([]byte, error) {
	select {
	case <-c.ctx.Done():
		return nil, c.ctx.Err()
	case <-c.stop:
		return []byte{1, 2, 3}, nil
	}
}

type voicePlayer struct{}

func (*voicePlayer) Enqueue(context.Context, []byte) error { return nil }
func (*voicePlayer) Finish() error                         { return nil }
func (*voicePlayer) Cancel()                               {}

type voiceService struct {
	capture    *voiceCapture
	failSpeech bool
}

func (v *voiceService) Record(ctx context.Context) (speech.Capture, error) {
	v.capture = &voiceCapture{ctx: ctx, ready: make(chan struct{}), stop: make(chan struct{})}
	return v.capture, nil
}
func (v *voiceService) Recognize(_ context.Context, wav []byte) (speech.Transcript, error) {
	if len(wav) != 3 {
		return speech.Transcript{}, errors.New("capture was truncated")
	}
	return speech.Transcript{Text: "I just wanted to know how are you today", Language: "en"}, nil
}
func (v *voiceService) Synthesize(context.Context, string) ([]byte, error) {
	if v.failSpeech {
		return nil, errors.New("speaker failed")
	}
	return []byte{1}, nil
}
func (v *voiceService) Player(context.Context) speech.Player { return &voicePlayer{} }
func (v *voiceService) Close()                               {}
func TestPushToTalkReadinessAndTranscript(t *testing.T) {
	m := settingsModel(t)
	m.input.SetValue("unsent text draft")
	voice := &voiceService{}
	m.chat.config.NewVoice = func(context.Context, string) (speech.Service, error) { return voice, nil }
	messages := make(chan tea.Msg, 8)
	m.send = func(msg tea.Msg) { messages <- msg }
	m.handleKey(tea.KeyPressMsg{Code: tea.KeyF8})
	if m.state != stRecording || m.voicePhase != "Starting microphone — wait for ready" {
		t.Fatal("capture did not wait for readiness")
	}
	close(voice.capture.ready)
	select {
	case msg := <-messages:
		m.Update(msg)
	case <-time.After(time.Second):
		t.Fatal("no readiness signal")
	}
	if m.voicePhase != "Recording · F8/Ctrl+R finishes" {
		t.Fatal("microphone readiness missing")
	}
	m.handleKey(tea.KeyPressMsg{Code: tea.KeyF8})
	deadline := time.After(2 * time.Second)
	for m.state != stDone {
		select {
		case msg := <-messages:
			m.Update(msg)
		case <-deadline:
			t.Fatal("PTT did not complete")
		}
	}
	if m.input.Value() != "unsent text draft" {
		t.Fatal("recording lost the text draft")
	}
	history := m.chat.current.History()
	if len(history) != 4 || history[2].Content != "I just wanted to know how are you today" {
		t.Fatalf("recognition missing first words: %+v", history)
	}
}
func TestCanceledRecordingCannotSubmitLateTranscript(t *testing.T) {
	m := settingsModel(t)
	m.input.SetValue("")
	voice := &voiceService{}
	m.chat.config.NewVoice = func(context.Context, string) (speech.Service, error) { return voice, nil }
	messages := make(chan tea.Msg, 8)
	m.send = func(msg tea.Msg) { messages <- msg }
	m.recordVoice()
	revision := m.captureRevision
	m.cancelRecording()
	m.Update(recordedMsg{revision: revision, transcript: speech.Transcript{Text: "stale"}})
	if m.state != stDone || m.input.Value() != "" || len(m.chat.current.History()) != 2 {
		t.Fatal("canceled capture submitted stale transcript")
	}
	select {
	case msg := <-messages:
		m.Update(msg)
	case <-time.After(time.Second):
		t.Fatal("capture did not stop")
	}
	if m.state != stDone {
		t.Fatal("late completion changed state")
	}
}
func TestSpeechFailureKeepsCompletedTextAndCanRecordAgain(t *testing.T) {
	m := settingsModel(t)
	voice := &voiceService{failSpeech: true}
	m.voice = voice
	m.voiceEnabled = true
	m.input.SetValue("")
	messages := make(chan tea.Msg, 8)
	m.send = func(msg tea.Msg) { messages <- msg }
	m.runConversation("Next question")
	select {
	case msg := <-messages:
		m.Update(msg)
	case <-time.After(2 * time.Second):
		t.Fatal("speech failure blocked conversation")
	}
	if m.state != stDone || m.runErr != nil || len(m.chat.current.History()) != 4 {
		t.Fatal("speech failure discarded text or blocked next turn")
	}
	m.recordVoice()
	if m.state != stRecording {
		t.Fatal("could not record after speech failure")
	}
	m.cancelRecording()
	select {
	case <-messages:
	case <-time.After(time.Second):
		t.Fatal("capture did not stop")
	}
}

func TestStoppingSpeechRetainsCompletedAssistantText(t *testing.T) {
	m := settingsModel(t)
	m.state = stRunning
	m.interrupted = true
	m.Update(doneMsg{answer: "Completed text that was not entirely spoken"})
	found := false
	for _, b := range m.blocks {
		if b.text == "Completed text that was not entirely spoken" {
			found = true
		}
	}
	if !found || m.state != stDone || m.runErr != nil {
		t.Fatal("stopping playback hid the completed text reply")
	}
}

func TestGreetingPlaybackDoesNotCreateConversationTurns(t *testing.T) {
	m := settingsModel(t)
	voice := &voiceService{}
	m.voice = voice
	m.voiceEnabled = true
	m.chat.config.Settings.Greeting = "Hello Alex"
	messages := make(chan tea.Msg, 2)
	m.send = func(msg tea.Msg) { messages <- msg }
	before := len(m.chat.current.History())
	m.playGreeting()
	select {
	case msg := <-messages:
		m.Update(msg)
	case <-time.After(time.Second):
		t.Fatal("greeting playback blocked")
	}
	if m.state != stDone || len(m.chat.current.History()) != before {
		t.Fatal("greeting created a fake model turn")
	}
}
