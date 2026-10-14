package tui

import (
	"context"
	"encoding/binary"
	"github.com/keshon/tars/internal/speech"
	"runtime"
	"testing"
)

func liveTestModel(t *testing.T) *model {
	t.Helper()
	if runtime.GOOS != "windows" {
		t.Skip("DirectShow capture is Windows-specific")
	}
	m := settingsModel(t)
	m.chat.config.Profile.Speech = &speech.Config{MaxSeconds: 120}
	c, err := speech.NewLive(context.Background(), "ffmpeg", "test", "", false)
	if err != nil {
		t.Fatal(err)
	}
	m.listening = c
	m.liveDetector = speech.NewDetector(0, 200, 1100, 120)
	t.Cleanup(m.stopListening)
	return m
}
func TestSpeechOnsetCancelsResponseAndKeepsListening(t *testing.T) {
	m := liveTestModel(t)
	ctx, cancel := context.WithCancel(context.Background())
	m.cancel = cancel
	m.state = stRunning
	yes := true
	frame := make([]byte, 640)
	for i := 0; i < 640; i += 2 {
		binary.LittleEndian.PutUint16(frame[i:], 12000)
	}
	for i := 0; i < 10; i++ {
		m.acceptLiveFrame(liveFrameMsg{m.listening, frame, &yes})
	}
	if ctx.Err() == nil || m.state != stStopping || !m.interrupted || m.listening == nil {
		t.Fatal("barge-in did not stop response and preserve capture")
	}
}
func TestLiveResultsAfterDisableCannotSubmit(t *testing.T) {
	m := liveTestModel(t)
	capture := m.listening
	before := m.chat.current.History()
	m.stopListening()
	m.acceptLiveRecognized(liveRecognizedMsg{capture, 0, "late speech", nil})
	if len(m.chat.current.History()) != len(before) || m.state != stDone {
		t.Fatal("late result submitted")
	}
}
func TestContinuationDuringRecognitionRetainsEarlierAudio(t *testing.T) {
	m := liveTestModel(t)
	wave := make([]byte, 44+640)
	m.liveASRWave = wave
	m.livePending = append([]byte(nil), wave...)
	m.liveRecognizing = true
	m.liveEpoch = 2
	m.liveDetector = speech.NewDetector(0, 200, 1100, 120)
	m.state = stStopping
	m.acceptLiveRecognized(liveRecognizedMsg{m.listening, 1, "first phrase", nil})
	if len(m.livePending) != 44+1280 || m.liveRecognizing {
		t.Fatal("continuation lost an earlier segment")
	}
}
