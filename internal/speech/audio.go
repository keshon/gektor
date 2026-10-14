package speech

import (
	"context"
	"encoding/binary"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"sync"

	"github.com/keshon/tars/internal/process"
)

type Recording struct {
	progress        *captureProgress
	ready           chan struct{}
	command         *exec.Cmd
	microphone      string
	input           io.WriteCloser
	directory, path string
	once            sync.Once
}

func NewRecording(ctx context.Context, binary, microphone string, limits ...int) (*Recording, error) {
	if runtime.GOOS != "windows" {
		return nil, fmt.Errorf("terminal capture currently requires Windows DirectShow")
	}
	if strings.TrimSpace(microphone) == "" {
		return nil, fmt.Errorf("set a microphone in Voice > Speech settings")
	}
	directory, err := os.MkdirTemp("", "tars-recording-")
	if err != nil {
		return nil, err
	}
	seconds := 120
	if len(limits) > 0 && limits[0] > 0 {
		seconds = limits[0]
	}
	path := filepath.Join(directory, "input.wav")
	cmd := exec.CommandContext(ctx, binary, "-hide_banner", "-loglevel", "error", "-progress", "pipe:2", "-y", "-f", "dshow", "-i", "audio="+microphone, "-t", fmt.Sprint(seconds), "-ac", "1", "-ar", "16000", "-c:a", "pcm_s16le", path)
	ready := make(chan struct{})
	progress := &captureProgress{ready: ready}
	cmd.Stderr = progress
	input, err := cmd.StdinPipe()
	if err != nil {
		os.RemoveAll(directory)
		return nil, err
	}
	return &Recording{progress: progress, ready: ready, microphone: microphone, command: cmd, input: input, directory: directory, path: path}, nil
}
func (r *Recording) Stop() {
	r.once.Do(func() { _, _ = io.WriteString(r.input, "q\n"); _ = r.input.Close() })
}
func (r *Recording) Run() ([]byte, error) {
	defer os.RemoveAll(r.directory)
	defer r.input.Close()
	if err := process.Run(r.command); err != nil {
		return nil, fmt.Errorf("microphone %q: %w (check Voice > Speech settings)", r.microphone, fmt.Errorf("%w: %s", err, r.progress.pending))
	}
	return os.ReadFile(r.path)
}

type Playback struct {
	ctx    context.Context
	cancel context.CancelFunc
	binary string
	queue  chan []byte
	done   chan error
}

func NewPlayback(ctx context.Context, binary string) *Playback {
	ctx, cancel := context.WithCancel(ctx)
	p := &Playback{ctx: ctx, cancel: cancel, binary: binary, queue: make(chan []byte, 2), done: make(chan error, 1)}
	go func() { p.done <- p.run() }()
	return p
}

// One raw PCM stream keeps the audio device open across generated WAV chunks.
// Format is validated locally; stream probing must not wait for future chunks.
func (p *Playback) run() error {
	var first []byte
	select {
	case <-p.ctx.Done():
		return nil
	case wav, ok := <-p.queue:
		if !ok {
			return nil
		}
		first = wav
	}
	format, pcm, err := decodePlaybackWAV(first)
	if err != nil {
		return err
	}
	fadePlaybackStart(pcm, format)
	reader, writer, err := os.Pipe()
	if err != nil {
		return err
	}
	defer reader.Close()
	defer writer.Close()
	ctx, cancel := context.WithCancel(p.ctx)
	defer cancel()
	cmd := exec.CommandContext(ctx, p.binary, "-nodisp", "-autoexit", "-loglevel", "error", "-probesize", "32", "-analyzeduration", "0", "-f", "s16le", "-ar", fmt.Sprint(format.rate), "-ac", fmt.Sprint(format.channels), "-i", "pipe:0")
	cmd.Stdin = reader
	cmd.Stdout = io.Discard
	cmd.Stderr = io.Discard
	done := make(chan error, 1)
	go func() {
		err := process.Run(cmd)
		reader.Close() // Unblock writes if the player fails before consuming PCM.
		done <- err
	}()
	joined := false
	defer func() {
		cancel()
		writer.Close()
		if !joined {
			<-done
		}
	}()
	for {
		if _, err = writer.Write(pcm); err != nil {
			if p.ctx.Err() != nil {
				return nil
			}
			return fmt.Errorf("speaker stream: %w", err)
		}
		select {
		case <-p.ctx.Done():
			return nil
		case wav, ok := <-p.queue:
			if !ok {
				// FFplay auto-exits when decoded frames are consumed, before the
				// device necessarily drains. Trailing silence protects real samples.
				if _, err = writer.Write(make([]byte, int(format.rate*format.channels*2)*400/1000)); err != nil {
					if p.ctx.Err() != nil {
						return nil
					}
					return fmt.Errorf("speaker drain: %w", err)
				}
				writer.Close()
				err = <-done
				joined = true
				if p.ctx.Err() != nil {
					return nil
				}
				if err != nil {
					return fmt.Errorf("speaker playback: %w", err)
				}
				return nil
			}
			var next playbackFormat
			next, pcm, err = decodePlaybackWAV(wav)
			if err != nil {
				return err
			}
			if next != format {
				return fmt.Errorf("speech format changed during playback")
			}
		}
	}
}

type playbackFormat struct{ rate, channels uint32 }

// WAVs may contain metadata chunks; only sample bytes belong in the PCM stream.
func decodePlaybackWAV(wav []byte) (playbackFormat, []byte, error) {
	var format playbackFormat
	if len(wav) < 12 || string(wav[:4]) != "RIFF" || string(wav[8:12]) != "WAVE" || uint64(binary.LittleEndian.Uint32(wav[4:]))+8 != uint64(len(wav)) {
		return format, nil, fmt.Errorf("invalid playback WAV")
	}
	var pcm []byte
	for pos := 12; pos+8 <= len(wav); {
		size := int(binary.LittleEndian.Uint32(wav[pos+4:]))
		start := pos + 8
		if size > len(wav)-start {
			return format, nil, fmt.Errorf("truncated playback WAV")
		}
		chunk := wav[start : start+size]
		switch string(wav[pos : pos+4]) {
		case "fmt ":
			if size < 16 || binary.LittleEndian.Uint16(chunk) != 1 || binary.LittleEndian.Uint16(chunk[14:]) != 16 {
				return format, nil, fmt.Errorf("playback requires PCM16 WAV")
			}
			format = playbackFormat{binary.LittleEndian.Uint32(chunk[4:]), uint32(binary.LittleEndian.Uint16(chunk[2:]))}
		case "data":
			pcm = append(pcm, chunk...)
		}
		pos = start + size + size%2
	}
	if format.rate < 8000 || format.rate > 192000 || format.channels < 1 || format.channels > 2 || len(pcm) == 0 || len(pcm)%int(format.channels*2) != 0 {
		return format, nil, fmt.Errorf("invalid playback PCM format")
	}
	return format, pcm, nil
}
func (p *Playback) Enqueue(ctx context.Context, audio []byte) error {
	select {
	case <-p.ctx.Done():
		return nil
	default:
	}
	select {
	case <-p.ctx.Done():
		return nil
	case <-ctx.Done():
		return ctx.Err()
	case err := <-p.done:
		p.done <- err
		if err == nil {
			return fmt.Errorf("playback worker exited")
		}
		return err
	case p.queue <- audio:
		return nil
	}
}

// Only the turn producer closes the queue, after it stops publishing audio.
func (p *Playback) Finish() error { close(p.queue); return <-p.done }

// Ramp the first 10 ms only: avoid a discontinuity when opening the audio device.
func fadePlaybackStart(pcm []byte, format playbackFormat) {
	frames := min(int(format.rate)/100, len(pcm)/(int(format.channels)*2))
	if frames < 2 {
		return
	}
	for frame := 0; frame < frames; frame++ {
		for channel := 0; channel < int(format.channels); channel++ {
			offset := (frame*int(format.channels) + channel) * 2
			sample := int16(binary.LittleEndian.Uint16(pcm[offset:]))
			binary.LittleEndian.PutUint16(pcm[offset:], uint16(int16(int(sample)*frame/(frames-1))))
		}
	}
}

func (p *Playback) Cancel() { p.cancel() }

type captureProgress struct {
	ready   chan struct{}
	once    sync.Once
	pending string
}

func (p *captureProgress) Write(b []byte) (int, error) {
	p.pending += string(b)
	if strings.Contains(p.pending, "out_time_") {
		p.once.Do(func() { close(p.ready) })
	}
	if len(p.pending) > 2048 {
		p.pending = p.pending[len(p.pending)-2048:]
	}
	return len(b), nil
}
func (r *Recording) Ready() <-chan struct{} { return r.ready }
