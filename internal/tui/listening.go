package tui

import (
	"context"
	"fmt"
	"github.com/keshon/tars/internal/speech"
	"os"
	"path/filepath"
	"strings"
	"time"
)

type liveFrameMsg struct {
	capture    *speech.LiveCapture
	pcm        []byte
	classified *bool
}
type liveEndedMsg struct {
	capture *speech.LiveCapture
	err     error
}
type liveRecognizedMsg struct {
	capture *speech.LiveCapture
	epoch   uint64
	text    string
	err     error
}

func (m *model) stopListening() {
	if m.listening != nil {
		m.listening.Close()
	}
	m.listening = nil
	m.liveDetector = nil
	m.livePending = nil
	m.liveASRWave = nil
	m.liveRecognizing = false
	m.liveEpoch++
}
func (m *model) toggleListening() {
	if m.listening != nil {
		m.stopListening()
		m.appendBlock(markerBlock("Hands-free listening off"))
		return
	}
	if m.audio() == nil || m.audio().Profile.Speech == nil || m.state != stDone {
		return
	}
	if err := m.enableVoice(); err != nil {
		m.appendBlock(errorBlock(err.Error()))
		return
	}
	cfg := m.audio().Profile.Speech
	exe, err := os.Executable()
	if err != nil {
		m.appendBlock(errorBlock(err.Error()))
		return
	}
	base := m.base
	if base == nil {
		base = context.Background()
	}
	capture, err := speech.NewLive(base, cfg.Recorder, cfg.Microphone, filepath.Dir(exe), cfg.VAD != "energy")
	if err != nil {
		m.appendBlock(errorBlock(err.Error()))
		return
	}
	m.listening = capture
	m.liveLastFrame = time.Time{}
	m.liveDetector = speech.NewDetector(cfg.MicThreshold, cfg.MicStartMS, cfg.MicEndMS, cfg.MaxSeconds)
	m.appendBlock(markerBlock("Hands-free microphone opening · wait for Listening · Ctrl+L disables"))
	go func() {
		err := capture.Run(func(pcm []byte, classified *bool) { m.send(liveFrameMsg{capture, pcm, classified}) })
		m.send(liveEndedMsg{capture, err})
	}()
}
func (m *model) acceptLiveFrame(msg liveFrameMsg) {
	if msg.capture != m.listening || m.liveDetector == nil {
		return
	}
	m.liveLastFrame = time.Now()
	onset, wav := m.liveDetector.PushFrame(msg.pcm, msg.classified)
	if onset {
		m.liveEpoch++
		if m.state == stRunning {
			m.interrupted = true
			m.cancel()
			m.state = stStopping
			m.fitBottom()
		}
	}
	if len(wav) > 0 {
		merged, err := speech.MergeAudio(m.livePending, wav, m.audio().Profile.Speech.MaxSeconds)
		if err != nil {
			m.appendBlock(errorBlock(err.Error()))
		} else {
			m.livePending = merged
		}
	}
	m.submitLiveAudio()
}
func (m *model) submitLiveAudio() {
	if m.listening == nil || m.liveRecognizing || m.liveDetector.Active() || m.state != stDone || len(m.livePending) == 0 || m.dialog != nil || m.sessions != nil || m.header.focused {
		return
	}
	wav := m.livePending
	m.livePending = nil
	m.liveASRWave = wav
	m.liveRecognizing = true
	capture, epoch, service := m.listening, m.liveEpoch, m.runtime().Voice
	go func() {
		tx, err := service.Recognize(capture.Context(), wav)
		m.send(liveRecognizedMsg{capture, epoch, tx.Text, err})
	}()
}
func (m *model) acceptLiveRecognized(msg liveRecognizedMsg) {
	if msg.capture != m.listening {
		return
	}
	m.liveRecognizing = false
	if msg.epoch != m.liveEpoch {
		wav := m.liveASRWave
		if len(m.livePending) > 0 {
			var err error
			wav, err = speech.MergeAudio(wav, m.livePending, m.audio().Profile.Speech.MaxSeconds)
			if err != nil {
				m.appendBlock(errorBlock(err.Error()))
			}
		}
		m.livePending = wav
		m.liveASRWave = nil
		m.submitLiveAudio()
		return
	}
	if m.state != stDone {
		merged, err := speech.MergeAudio(m.liveASRWave, m.livePending, m.audio().Profile.Speech.MaxSeconds)
		if err != nil {
			m.appendBlock(errorBlock(err.Error()))
		} else {
			m.livePending = merged
		}
		m.liveASRWave = nil
		return
	}
	m.liveASRWave = nil
	if msg.err != nil {
		m.appendBlock(errorBlock("Voice recognition: " + msg.err.Error()))
		m.submitLiveAudio()
		return
	}
	if strings.TrimSpace(msg.text) == "" {
		m.submitLiveAudio()
		return
	}
	draft := m.input.Value()
	m.input.SetValue(msg.text)
	m.followUp()
	m.input.SetValue(draft)
	m.fitInput()
	m.fitBottom()
}
func (m *model) listeningStatus() string {
	if m.listening == nil {
		return ""
	}
	if m.liveLastFrame.IsZero() {
		return "Mic opening · wait before speaking"
	}
	if time.Since(m.liveLastFrame) > 2*time.Second {
		return "Mic stalled · Ctrl+L off/on to reconnect"
	}
	if m.liveRecognizing {
		return "Recognizing speech"
	}
	if m.liveDetector.Active() {
		return "Listening · speech detected"
	}
	if len(m.livePending) > 0 {
		return "Speech queued"
	}
	return fmt.Sprintf("Listening · %s · Ctrl+L off", m.audio().Profile.Speech.Microphone)
}
