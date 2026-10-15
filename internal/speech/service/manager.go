// Package service composes owned speech processes and external recognition endpoints.
package service

import (
	"context"
	"fmt"
	"net"
	"net/url"
	"os"
	"os/exec"
	"path/filepath"
	"sync"
	"time"
	"unicode"

	"github.com/keshon/tars/internal/process"
	"github.com/keshon/tars/internal/speech"
	"github.com/keshon/tars/internal/speech/chatterbox"
	"github.com/keshon/tars/internal/speech/whisper"
)

type Manager struct {
	ctx    context.Context
	cancel context.CancelFunc
	config speech.Config
	mu     sync.Mutex
	asr    *process.Child
	tts    *chatterbox.Synthesizer
	log    *os.File
}

func New(ctx context.Context, cfg speech.Config, directory string) (*Manager, error) {
	if err := cfg.Validate(); err != nil {
		return nil, err
	}
	for _, binary := range []string{cfg.Recorder, cfg.PlayerBinary, cfg.Speaking.Binary} {
		if _, err := exec.LookPath(binary); err != nil {
			return nil, fmt.Errorf("speech executable %s: %w", binary, err)
		}
	}
	for _, name := range []string{"t3_" + cfg.Speaking.Variant + "_v1.safetensors", "s3gen_meanflow.safetensors", "ve.safetensors", "conds.pt", "vocab.json", "merges.txt", "added_tokens.json", "special_tokens_map.json", "tokenizer_config.json"} {
		path := filepath.Join(cfg.Speaking.Model, name)
		info, err := os.Stat(path)
		if err != nil {
			return nil, fmt.Errorf("speech assets: %w", err)
		}
		if info.IsDir() {
			return nil, fmt.Errorf("speech model file is a directory: %s", path)
		}
	}
	if cfg.Listening.Ownership == "managed" {
		for _, path := range []string{cfg.Listening.Binary, cfg.Listening.Model} {
			if _, err := os.Stat(path); err != nil {
				return nil, fmt.Errorf("recognition assets: %w", err)
			}
		}
	}
	if err := os.MkdirAll(directory, 0700); err != nil {
		return nil, err
	}
	directory, err := os.MkdirTemp(directory, "session-")
	if err != nil {
		return nil, err
	}
	log, err := os.OpenFile(filepath.Join(directory, "speech.log"), os.O_CREATE|os.O_APPEND|os.O_WRONLY, 0600)
	if err != nil {
		return nil, err
	}
	childCtx, cancel := context.WithCancel(ctx)
	tts, err := chatterbox.NewVariant(childCtx, cfg.Speaking.Binary, cfg.Speaking.Model, directory, cfg.Speaking.Variant, log)
	if err != nil {
		cancel()
		log.Close()
		return nil, err
	}
	return &Manager{ctx: childCtx, cancel: cancel, config: cfg, tts: tts, log: log}, nil
}
func (m *Manager) Close() {
	m.cancel()
	m.tts.Close()
	m.mu.Lock()
	defer m.mu.Unlock()
	if m.asr != nil {
		m.asr.Stop()
		m.asr = nil
	}
	m.log.Close()
}
func (m *Manager) Record(ctx context.Context) (speech.Capture, error) {
	return speech.NewRecording(ctx, m.config.Recorder, m.config.Microphone, m.config.MaxSeconds)
}
func (m *Manager) Synthesize(ctx context.Context, text string) ([]byte, error) {
	for _, r := range text {
		if unicode.IsLetter(r) && !unicode.In(r, unicode.Latin) {
			return nil, fmt.Errorf("Chatterbox speech is English-only; this reply remains available as text")
		}
	}
	return m.tts.Synthesize(ctx, text, "en", m.config.Speaking.Variant)
}
func (m *Manager) Player(ctx context.Context) speech.Player {
	return speech.NewPlayback(ctx, m.config.PlayerBinary)
}
func (m *Manager) Recognize(ctx context.Context, wav []byte) (speech.Transcript, error) {
	if err := speech.ValidateInput(wav); err != nil {
		return speech.Transcript{}, err
	}
	if err := m.ensureRecognition(ctx); err != nil {
		return speech.Transcript{}, err
	}
	return whisper.NewEnglish(m.config.Listening.URL).Recognize(ctx, wav)
}
func (m *Manager) ensureRecognition(ctx context.Context) error {
	if err := m.ctx.Err(); err != nil {
		return err
	}
	if err := ctx.Err(); err != nil {
		return err
	}
	if m.config.Listening.Ownership == "external" {
		return nil
	}
	m.mu.Lock()
	defer m.mu.Unlock()
	if m.asr != nil {
		if !m.asr.Exited() {
			return nil
		}
		m.asr = nil
	}
	u, _ := url.Parse(m.config.Listening.URL)
	listener, err := net.Listen("tcp", u.Host)
	if err != nil {
		return fmt.Errorf("recognition port is occupied: %w", err)
	}
	listener.Close()
	child, err := process.Start(m.ctx, m.config.Listening.Binary, filepath.Dir(m.config.Listening.Binary), []string{"-m", m.config.Listening.Model, "--host", u.Hostname(), "--port", u.Port(), "-t", "2", "-l", "en", "-bs", "1", "-bo", "1", "-nt", "-nc", "-nlp"}, m.log)
	if err != nil {
		return err
	}
	ready, cancel := context.WithTimeout(ctx, 90*time.Second)
	defer cancel()
	if err = child.Ready(ready, m.config.Listening.URL); err != nil {
		child.Stop()
		return err
	}
	m.asr = child
	return nil
}

func (m *Manager) Diagnostics() string { return m.log.Name() }

func (m *Manager) DeliveryCues() []string       { return m.tts.DeliveryCues() }
func (m *Manager) DeliveryInstructions() string { return m.tts.DeliveryInstructions() }
