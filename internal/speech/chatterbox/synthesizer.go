// Package chatterbox owns a resident Chatterbox CUDA process and its cancellation protocol.
package chatterbox

import (
	"bufio"
	"context"
	_ "embed"
	"encoding/binary"
	"encoding/json"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"sync"
	"time"

	"github.com/keshon/tars/internal/process"
	"github.com/keshon/tars/internal/speech"
)

//go:embed worker.py
var workerSource []byte

type response struct {
	Type   string `json:"type"`
	ID     uint64 `json:"id"`
	Status string `json:"status"`
	Audio  []byte `json:"wav"`
	Error  string `json:"error"`
}

type Synthesizer struct {
	mu                    sync.Mutex
	parent                context.Context
	binary, model, script string
	variant               string
	log                   io.Writer
	input                 io.WriteCloser
	scanner               *bufio.Scanner
	output                io.ReadCloser
	cancel                context.CancelFunc
	done                  chan error
	next                  uint64
	command               func(context.Context) *exec.Cmd
}

func New(ctx context.Context, binary, model, directory string, log io.Writer) (*Synthesizer, error) {
	return NewVariant(ctx, binary, model, directory, "nano", log)
}

func NewVariant(ctx context.Context, binary, model, directory, variant string, log io.Writer) (*Synthesizer, error) {
	if variant != "nano" && variant != "turbo" {
		return nil, fmt.Errorf("unknown Chatterbox variant %q", variant)
	}
	script := filepath.Join(directory, "chatterbox-worker.py")
	if err := os.WriteFile(script, workerSource, 0600); err != nil {
		return nil, err
	}
	return &Synthesizer{parent: ctx, binary: binary, model: model, script: script, variant: variant, log: log}, nil
}

func (s *Synthesizer) Voices() []speech.Voice {
	variant := s.modelVariant()
	name := strings.ToUpper(variant[:1]) + variant[1:] + " - built-in reference voice"
	return []speech.Voice{{ID: variant, Name: name, Language: "en", Default: true}}
}

func (s *Synthesizer) stop() {
	if s.cancel == nil {
		return
	}
	s.cancel()
	<-s.done
	s.input.Close()
	s.output.Close()
	s.cancel = nil
}

func (s *Synthesizer) Close() { s.mu.Lock(); defer s.mu.Unlock(); s.stop() }

func (s *Synthesizer) read() (response, error) {
	var reply response
	if !s.scanner.Scan() {
		if err := s.scanner.Err(); err != nil {
			return reply, err
		}
		return reply, io.EOF
	}
	err := json.Unmarshal(s.scanner.Bytes(), &reply)
	return reply, err
}

func (s *Synthesizer) start(ctx context.Context) error {
	if s.cancel != nil {
		return nil
	}
	parent, cancel := context.WithCancel(s.parent)
	cmd := exec.CommandContext(parent, s.binary, "-I", "-u", s.script, "--model", s.model, "--variant", s.modelVariant())
	if s.command != nil {
		cmd = s.command(parent)
	}
	cmd.Env = append(os.Environ(), "PYTHONUTF8=1", "PYTHONNOUSERSITE=1", "HF_HUB_OFFLINE=1")
	input, err := cmd.StdinPipe()
	if err != nil {
		cancel()
		return err
	}
	output, err := cmd.StdoutPipe()
	if err != nil {
		input.Close()
		cancel()
		return err
	}
	cmd.Stderr = s.log
	s.input, s.output, s.cancel = input, output, cancel
	s.scanner = bufio.NewScanner(output)
	s.scanner.Buffer(make([]byte, 65536), 4<<20)
	s.done = make(chan error, 1)
	go func() { s.done <- process.Run(cmd) }()
	ready := make(chan error, 1)
	go func() {
		reply, err := s.read()
		if err == nil && reply.Type != "ready" {
			err = fmt.Errorf("invalid Chatterbox ready message")
		}
		ready <- err
	}()
	select {
	case err := <-ready:
		if err != nil {
			s.stop()
		}
		return err
	case <-ctx.Done():
		s.stop()
		<-ready
		return ctx.Err()
	}
}

func (s *Synthesizer) Synthesize(ctx context.Context, text, language, voice string) ([]byte, error) {
	if language != "en" {
		return nil, fmt.Errorf("Chatterbox supports English speech only")
	}
	if voice != "" && voice != s.modelVariant() {
		return nil, fmt.Errorf("unknown Chatterbox voice %q", voice)
	}
	if len(text) == 0 || len(text) > 8192 {
		return nil, fmt.Errorf("invalid phrase length")
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	startup, endStartup := context.WithTimeout(ctx, 2*time.Minute)
	err := s.start(startup)
	endStartup()
	if err != nil {
		return nil, fmt.Errorf("Chatterbox startup: %w", err)
	}
	s.next++
	id := s.next
	if err := json.NewEncoder(s.input).Encode(map[string]any{"op": "synthesize", "id": id, "text": text}); err != nil {
		s.stop()
		return nil, err
	}
	type result struct {
		reply response
		err   error
	}
	finished := make(chan result, 1)
	go func() { reply, err := s.read(); finished <- result{reply, err} }()
	deadline, end := context.WithTimeout(ctx, 45*time.Second)
	defer end()
	var reply result
	select {
	case reply = <-finished:
	case <-deadline.Done():
		// Acknowledgement is consumed before the next request; old audio cannot
		// leak into the next turn. Unresponsive workers are terminated.
		_ = json.NewEncoder(s.input).Encode(map[string]any{"op": "cancel", "id": id})
		timer := time.NewTimer(750 * time.Millisecond)
		select {
		case ack := <-finished:
			if ack.err != nil || ack.reply.ID != id || (ack.reply.Status != "ok" && ack.reply.Status != "cancelled") {
				s.stop()
			}
		case <-timer.C:
			s.stop()
			<-finished
		}
		timer.Stop()
		return nil, deadline.Err()
	}
	if reply.err != nil {
		s.stop()
		return nil, fmt.Errorf("Chatterbox worker: %w", reply.err)
	}
	if reply.reply.ID != id {
		s.stop()
		return nil, fmt.Errorf("Chatterbox response ID mismatch")
	}
	if ctx.Err() != nil {
		return nil, ctx.Err()
	}
	if reply.reply.Status != "ok" {
		s.stop()
		return nil, fmt.Errorf("Chatterbox: %s: %s", reply.reply.Status, reply.reply.Error)
	}
	if !validWAV(reply.reply.Audio) {
		s.stop()
		return nil, fmt.Errorf("invalid Chatterbox WAV")
	}
	return reply.reply.Audio, nil
}

func validWAV(b []byte) bool {
	return len(b) >= 46 && len(b) <= 3_000_000 && string(b[:4]) == "RIFF" && string(b[8:16]) == "WAVEfmt " &&
		binary.LittleEndian.Uint32(b[4:8]) == uint32(len(b)-8) && binary.LittleEndian.Uint32(b[16:20]) == 16 &&
		binary.LittleEndian.Uint16(b[20:22]) == 1 && binary.LittleEndian.Uint16(b[22:24]) == 1 &&
		binary.LittleEndian.Uint32(b[24:28]) == 24000 && binary.LittleEndian.Uint32(b[28:32]) == 48000 &&
		binary.LittleEndian.Uint16(b[32:34]) == 2 && binary.LittleEndian.Uint16(b[34:36]) == 16 &&
		string(b[36:40]) == "data" && binary.LittleEndian.Uint32(b[40:44]) == uint32(len(b)-44) && len(b)%2 == 0
}

func (s *Synthesizer) modelVariant() string {
	if s.variant == "" {
		return "nano"
	}
	return s.variant
}
