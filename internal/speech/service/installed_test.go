package service

import (
	"context"
	"errors"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/keshon/tars/internal/speech"
)

func TestInstalledSpeechAcceptance(t *testing.T) {
	root := os.Getenv("TARS_SPEECH_TEST_ROOT")
	if root == "" {
		t.Skip("installed model opt-in")
	}
	cfg := speech.Config{Microphone: "Microphone (Storm Pro)", Recorder: "ffmpeg", PlayerBinary: "ffplay", MaxSeconds: 120, Listening: speech.Listening{Ownership: "managed", URL: "http://127.0.0.1:19082", Binary: filepath.Join(root, "assets", "runtimes", "whisper-cuda", "Release", "whisper-server.exe"), Model: filepath.Join(root, "assets", "models", "listening", "whisper", "ggml-small-q5_1.bin")}, Speaking: speech.Speaking{Provider: "chatterbox", Variant: "nano", Binary: filepath.Join(root, "assets", "runtimes", "chatterbox", "python.exe"), Model: filepath.Join(root, "assets", "models", "speaking", "chatterbox", "nano")}}
	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Minute)
	defer cancel()
	dir := filepath.Join(root, "var", "speech-acceptance")
	m, err := New(ctx, cfg, dir)
	if err != nil {
		t.Fatal(err)
	}
	defer m.Close()
	wav, err := m.Synthesize(ctx, "Hello. The new voice service is ready.")
	if err != nil {
		t.Fatal(err)
	}
	path := filepath.Join(dir, "normal.wav")
	os.WriteFile(path, wav, 0600)
	input := filepath.Join(dir, "recognition-input.wav")
	command := exec.CommandContext(ctx, "ffmpeg", "-hide_banner", "-loglevel", "error", "-y", "-i", path, "-ac", "1", "-ar", "16000", "-c:a", "pcm_s16le", input)
	if err = command.Run(); err != nil {
		t.Fatal(err)
	}
	bytes, err := os.ReadFile(input)
	if err != nil {
		t.Fatal(err)
	}
	tx, err := m.Recognize(ctx, bytes)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(strings.ToLower(tx.Text), "voice") || !strings.Contains(strings.ToLower(tx.Text), "ready") {
		t.Fatalf("unexpected round-trip: %s", tx.Text)
	}
	t.Logf("recognized: %s", tx.Text)
	interrupted, stop := context.WithCancel(ctx)
	timer := time.AfterFunc(100*time.Millisecond, stop)
	start := time.Now()
	_, err = m.Synthesize(interrupted, "This longer response should be interrupted before it completes. We are checking that cancellation leaves the worker ready for a fresh response.")
	timer.Stop()
	stop()
	if !errors.Is(err, context.Canceled) {
		t.Fatalf("cancel result=%v", err)
	}
	t.Logf("cancellation: %s", time.Since(start))
	fresh, err := m.Synthesize(ctx, "A fresh reply after cancellation.")
	if err != nil {
		t.Fatal(err)
	}
	os.WriteFile(filepath.Join(dir, "fresh.wav"), fresh, 0600)
	t.Logf("normal=%d bytes, recovered=%d bytes", len(wav), len(fresh))
}
