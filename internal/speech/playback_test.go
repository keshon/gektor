package speech

import (
	"bytes"
	"context"
	"encoding/binary"
	"os"
	"os/exec"
	"path/filepath"
	"testing"
	"time"
)

func TestPlaybackWAVDecodesModelRatesAndRejectsTruncation(t *testing.T) {
	for _, rate := range []uint32{16000, 24000} {
		wav := pcmWAV(pcmFrame(true))
		binary.LittleEndian.PutUint32(wav[24:], rate)
		binary.LittleEndian.PutUint32(wav[28:], rate*2)
		format, pcm, err := decodePlaybackWAV(wav)
		if err != nil || format.rate != rate || format.channels != 1 || !bytes.Equal(pcm, wav[44:]) {
			t.Fatalf("format=%v err=%v", format, err)
		}
		if _, _, err = decodePlaybackWAV(wav[:len(wav)-1]); err == nil {
			t.Fatal("accepted truncated WAV")
		}
	}
}

func TestContinuousPlaybackCompletesAndInterrupts(t *testing.T) {
	if _, err := exec.LookPath("ffplay"); err != nil {
		t.Skip("ffplay not installed")
	}
	t.Setenv("SDL_AUDIODRIVER", "dummy")
	for _, interrupt := range []bool{false, true} {
		ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		p := NewPlayback(ctx, "ffplay")
		wav := pcmWAV(bytes.Repeat(pcmFrame(true), 10))
		for i := 0; i < 4; i++ {
			if err := p.Enqueue(ctx, wav); err != nil {
				cancel()
				t.Fatal(err)
			}
		}
		if interrupt {
			p.cancel()
		}
		if err := p.Finish(); err != nil {
			cancel()
			t.Fatal(err)
		}
		if ctx.Err() != nil {
			cancel()
			t.Fatal("playback failed to finish within deadline")
		}
		cancel()
	}
}

// SDL disk output captures what the device callback receives, rather than merely
// asserting that ffplay exits successfully (which can hide a truncated tail).
func TestPlaybackDrainsFinalSamples(t *testing.T) {
	if _, err := exec.LookPath("ffplay"); err != nil {
		t.Skip("ffplay not installed")
	}
	target := filepath.Join(t.TempDir(), "device.pcm")
	t.Setenv("SDL_AUDIODRIVER", "disk")
	t.Setenv("SDL_DISKAUDIOFILE", target)
	pcm := make([]byte, 48000)
	for i := 19200; i < 24000; i++ {
		binary.LittleEndian.PutUint16(pcm[i*2:], 9000)
	}
	wav := pcmWAV(pcm)
	binary.LittleEndian.PutUint32(wav[24:], 24000)
	binary.LittleEndian.PutUint32(wav[28:], 48000)
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	player := NewPlayback(ctx, "ffplay")
	if err := player.Enqueue(ctx, wav); err != nil {
		t.Fatal(err)
	}
	if err := player.Finish(); err != nil {
		t.Fatal(err)
	}
	output, err := os.ReadFile(target)
	if err != nil {
		t.Fatal(err)
	}
	samples := 0
	for i := 0; i+1 < len(output); i += 2 {
		if binary.LittleEndian.Uint16(output[i:]) == 9000 {
			samples++
		}
	}
	if samples != 4800 {
		t.Fatalf("device received %d of 4800 final speech samples", samples)
	}
}

func TestPlaybackOnsetRampPreservesRestAndTail(t *testing.T) {
	pcm := make([]byte, 960)
	for i := 0; i < len(pcm); i += 2 {
		binary.LittleEndian.PutUint16(pcm[i:], 9000)
	}
	tail := append([]byte(nil), pcm[480:]...)
	fadePlaybackStart(pcm, playbackFormat{rate: 24000, channels: 1})
	if binary.LittleEndian.Uint16(pcm) != 0 || binary.LittleEndian.Uint16(pcm[478:]) != 9000 {
		t.Fatal("invalid onset ramp")
	}
	if !bytes.Equal(tail, pcm[480:]) {
		t.Fatal("ramp modified later speech")
	}
}

func pcmFrame(loud bool) []byte {
	frame := make([]byte, 640)
	if loud {
		for i := 0; i < len(frame); i += 2 {
			binary.LittleEndian.PutUint16(frame[i:], 2200)
		}
	}
	return frame
}
func pcmWAV(data []byte) []byte {
	wav := testWAV()[:44]
	binary.LittleEndian.PutUint32(wav[4:], uint32(len(data)+36))
	binary.LittleEndian.PutUint32(wav[40:], uint32(len(data)))
	return append(wav, data...)
}
