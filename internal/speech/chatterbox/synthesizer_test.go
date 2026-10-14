package chatterbox

import (
	"bytes"
	"context"
	"encoding/binary"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"os/exec"
	"strings"
	"sync"
	"testing"
	"time"
)

func fakeWAV(id uint64) []byte {
	b := make([]byte, 46)
	copy(b, "RIFF")
	binary.LittleEndian.PutUint32(b[4:], 38)
	copy(b[8:], "WAVEfmt ")
	binary.LittleEndian.PutUint32(b[16:], 16)
	binary.LittleEndian.PutUint16(b[20:], 1)
	binary.LittleEndian.PutUint16(b[22:], 1)
	binary.LittleEndian.PutUint32(b[24:], 24000)
	binary.LittleEndian.PutUint32(b[28:], 48000)
	binary.LittleEndian.PutUint16(b[32:], 2)
	binary.LittleEndian.PutUint16(b[34:], 16)
	copy(b[36:], "data")
	binary.LittleEndian.PutUint32(b[40:], 2)
	binary.LittleEndian.PutUint16(b[44:], uint16(id))
	return b
}

// This subprocess models a late successful completion racing cancellation.
// No Python, model downloads, or CUDA are needed for protocol tests.
func TestChatterboxProtocolHelper(t *testing.T) {
	mode := os.Args[len(os.Args)-1]
	if !strings.HasPrefix(mode, "chatterbox-helper-") {
		return
	}
	encoder, decoder := json.NewEncoder(os.Stdout), json.NewDecoder(os.Stdin)
	var mu sync.Mutex
	jobs := map[uint64]chan struct{}{}
	encode := func(value any) { mu.Lock(); defer mu.Unlock(); _ = encoder.Encode(value) }
	encode(response{Type: "ready"})
	for {
		var request struct {
			Op   string
			ID   uint64
			Text string
		}
		if err := decoder.Decode(&request); err != nil {
			os.Exit(0)
		}
		if request.Op == "cancel" {
			mu.Lock()
			if event := jobs[request.ID]; event != nil {
				close(event)
			}
			mu.Unlock()
			continue
		}
		event := make(chan struct{})
		mu.Lock()
		jobs[request.ID] = event
		mu.Unlock()
		if request.Text == "slow" {
			fmt.Fprintln(os.Stderr, "slow-started")
		}
		go func(id uint64, text string) {
			delay := 10 * time.Millisecond
			if text == "slow" {
				delay = 5 * time.Second
			}
			select {
			case <-time.After(delay):
			case <-event:
			}
			mu.Lock()
			delete(jobs, id)
			mu.Unlock()
			if mode == "chatterbox-helper-wrong-id" {
				id++
			}
			encode(response{ID: id, Status: "ok", Audio: fakeWAV(id)})
		}(request.ID, request.Text)
	}
}

func fakeSynth(t *testing.T, mode string) *Synthesizer {
	t.Helper()
	exe, err := os.Executable()
	if err != nil {
		t.Fatal(err)
	}
	s := &Synthesizer{parent: context.Background(), log: io.Discard,
		command: func(ctx context.Context) *exec.Cmd {
			return exec.CommandContext(ctx, exe, "-test.run=^TestChatterboxProtocolHelper$", "--", "chatterbox-helper-"+mode)
		}}
	t.Cleanup(s.Close)
	return s
}

func TestCancellationDrainsOldAudioAndPreservesResidentWorker(t *testing.T) {
	s := fakeSynth(t, "late-audio")
	started := make(chan struct{}, 1)
	s.log = signalLog{started}
	ctx, end := context.WithTimeout(context.Background(), 5*time.Second)
	defer end()
	if _, err := s.Synthesize(ctx, "warm", "en", ""); err != nil {
		t.Fatal(err)
	}
	owner := s.done
	turn, cancel := context.WithCancel(ctx)
	finished := make(chan error, 1)
	go func() {
		audio, err := s.Synthesize(turn, "slow", "en", "")
		if audio != nil {
			finished <- errors.New("cancelled turn emitted audio")
			return
		}
		finished <- err
	}()
	select {
	case <-started:
	case <-ctx.Done():
		t.Fatal("slow request never started")
	}
	cancel()
	if err := <-finished; !errors.Is(err, context.Canceled) {
		t.Fatal(err)
	}
	audio, err := s.Synthesize(ctx, "fresh", "en", "")
	if err != nil || binary.LittleEndian.Uint16(audio[44:]) != 3 {
		t.Fatalf("wrong fresh output: %v", err)
	}
	if s.done != owner {
		t.Fatal("cancellation restarted the resident worker")
	}
}

type signalLog struct{ started chan struct{} }

func (l signalLog) Write(p []byte) (int, error) {
	if bytes.Contains(p, []byte("slow-started")) {
		select {
		case l.started <- struct{}{}:
		default:
		}
	}
	return len(p), nil
}

func TestResponseIDMismatchStopsWorker(t *testing.T) {
	s := fakeSynth(t, "wrong-id")
	ctx, end := context.WithTimeout(context.Background(), 5*time.Second)
	defer end()
	if _, err := s.Synthesize(ctx, "hello", "en", ""); err == nil || !strings.Contains(err.Error(), "ID mismatch") {
		t.Fatal(err)
	}
	if s.cancel != nil {
		t.Fatal("invalid protocol worker was retained")
	}
}

func TestWAVValidationRejectsTruncatedAndInvalidPCM(t *testing.T) {
	valid := fakeWAV(1)
	if !validWAV(valid) {
		t.Fatal("valid PCM rejected")
	}
	for _, offset := range []int{4, 20, 22, 24, 28, 32, 34, 40} {
		bad := append([]byte(nil), valid...)
		bad[offset] ^= 1
		if validWAV(bad) {
			t.Fatalf("invalid field at %d accepted", offset)
		}
	}
	if validWAV(valid[:45]) {
		t.Fatal("truncated audio accepted")
	}
}
