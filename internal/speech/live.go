package speech

import (
	"bytes"
	"context"
	"encoding/binary"
	"fmt"
	"github.com/keshon/tars/internal/process"
	"github.com/keshon/tars/internal/speech/vad"
	"math"
	"os/exec"
	"runtime"
	"strings"
)

const microphoneRate = 16000
const microphoneFrame = 320 // 20 ms, PCM16 mono

type LiveCapture struct {
	binary     string
	root       string
	neural     bool
	ctx        context.Context
	cancel     context.CancelFunc
	microphone string
}

func NewLive(ctx context.Context, binary, microphone, root string, neural bool) (*LiveCapture, error) {
	if runtime.GOOS != "windows" {
		return nil, fmt.Errorf("live terminal capture requires Windows DirectShow")
	}
	if strings.TrimSpace(microphone) == "" {
		return nil, fmt.Errorf("select a microphone with /mic DEVICE")
	}
	ctx, cancel := context.WithCancel(ctx)
	return &LiveCapture{ctx: ctx, cancel: cancel, microphone: microphone, binary: binary, root: root, neural: neural}, nil
}
func (c *LiveCapture) Run(emit func([]byte, *bool)) error {
	var classifier *vad.Client
	if c.neural {
		var err error
		classifier, err = vad.Start(c.ctx, c.root)
		if err != nil {
			return err
		}
		defer classifier.Close()
	}
	var vadErr error
	cmd := exec.CommandContext(c.ctx, c.binary, "-hide_banner", "-loglevel", "error", "-f", "dshow", "-audio_buffer_size", "50", "-probesize", "32", "-analyzeduration", "0", "-i", "audio="+c.microphone, "-ac", "1", "-ar", "16000", "-f", "s16le", "-flush_packets", "1", "pipe:1")
	var diagnostic bytes.Buffer
	cmd.Stderr = &diagnostic

	cmd.Stdout = &microphoneWriter{emit: func(pcm []byte) {
		if vadErr != nil {
			return
		}
		if classifier == nil {
			emit(pcm, nil)
			return
		}
		speech, err := classifier.Speech(pcm)
		if err != nil {
			if c.ctx.Err() != nil {
				return
			}
			vadErr = err
			c.cancel()
			return
		}
		emit(pcm, &speech)
	}}
	err := process.Run(cmd)
	if vadErr != nil {
		return fmt.Errorf("speech detector stopped: %w", vadErr)
	}
	if c.ctx.Err() != nil {
		return nil
	}
	if err != nil {
		return fmt.Errorf("live microphone: %w: %s", err, strings.TrimSpace(diagnostic.String()))
	}
	return fmt.Errorf("microphone stream ended")
}

// exec waits for its stdout copy goroutine, so complete frames are delivered before shutdown.
type microphoneWriter struct {
	pending []byte
	emit    func([]byte)
}

func (w *microphoneWriter) Write(data []byte) (int, error) {
	n := len(data)
	w.pending = append(w.pending, data...)
	for len(w.pending) >= microphoneFrame*2 {
		frame := append([]byte(nil), w.pending[:microphoneFrame*2]...)
		w.pending = w.pending[microphoneFrame*2:]
		w.emit(frame)
	}
	return n, nil
}

// detector owns turn boundaries and pre-roll in the terminal update loop.
type Detector struct {
	pre, parts                     []byte
	candidate, quiet, voiced       int
	candidateGap, candidateElapsed int
	active                         bool
	noise, level                   float64
	floor                          float64
	startSamples                   int
	endSamples, maxSeconds         int
}

func (d *Detector) reset() {
	noise := d.noise
	*d = Detector{noise: noise, floor: d.floor, startSamples: d.startSamples, endSamples: d.endSamples, maxSeconds: d.maxSeconds}
	if d.noise == 0 {
		d.noise = .003
	}
}
func (d *Detector) push(pcm []byte) (bool, []byte) { return d.PushFrame(pcm, nil) }
func (d *Detector) PushFrame(pcm []byte, classified *bool) (bool, []byte) {
	if len(pcm) == 0 || len(pcm)%2 != 0 {
		return false, nil
	}
	if d.noise == 0 {
		d.noise = .003
	}
	samples := len(pcm) / 2
	energy := 0.0
	for i := 0; i < len(pcm); i += 2 {
		v := float64(int16(binary.LittleEndian.Uint16(pcm[i:]))) / 32768
		energy += v * v
	}
	rms := math.Sqrt(energy / float64(samples))
	d.level = rms
	threshold := d.threshold()
	if d.active {
		threshold = max(threshold*0.6, d.noise*2)
	} // Retain quieter syllables after confirmed onset.
	speech := rms > threshold
	if classified != nil {
		speech = *classified
	}
	if !d.active {
		if classified == nil && !speech && rms < d.threshold()*0.5 {
			d.noise = .99*d.noise + .01*min(rms, .01)
		}
		d.pre = append(d.pre, pcm...)
		preBytes := 25600
		if classified != nil {
			preBytes = 38400
		} // 1.2 seconds protects quiet words before neural activation.
		if len(d.pre) > preBytes {
			d.pre = append([]byte(nil), d.pre[len(d.pre)-preBytes:]...)
		}
		if speech {
			d.candidate += samples
			d.candidateGap = 0
		} else if d.candidate > 0 {
			d.candidateGap += samples
		}
		if d.candidate > 0 {
			d.candidateElapsed += samples
		}
		// Speech includes consonants and short gaps; do not require one long vowel.
		// Bound the evidence window to the retained pre-roll.
		if d.candidateGap >= microphoneRate*80/1000 || d.candidateElapsed > microphoneRate*800/1000 {
			d.candidate, d.candidateGap, d.candidateElapsed = 0, 0, 0
		}
		if d.candidate < d.onsetSamples() {
			return false, nil
		}
		d.active = true
		d.parts = d.pre
		d.pre = nil
		d.voiced = d.candidate
		return true, nil
	}
	d.parts = append(d.parts, pcm...)
	if speech {
		d.quiet = 0
		d.voiced += samples
	} else {
		d.quiet += samples
	}
	if d.quiet < d.endpointSamples() && len(d.parts) < microphoneRate*d.recordingSeconds()*2 {
		return false, nil
	}
	parts := d.parts
	valid := d.voiced >= d.onsetSamples() // Keep every utterance that was long enough to interrupt.
	d.reset()
	if !valid {
		return false, nil
	}
	return false, capturedWAV(parts)
}
func capturedWAV(pcm []byte) []byte {
	wav := make([]byte, 44+len(pcm))
	copy(wav, "RIFF")
	binary.LittleEndian.PutUint32(wav[4:], uint32(len(wav)-8))
	copy(wav[8:], "WAVEfmt ")
	binary.LittleEndian.PutUint32(wav[16:], 16)
	binary.LittleEndian.PutUint16(wav[20:], 1)
	binary.LittleEndian.PutUint16(wav[22:], 1)
	binary.LittleEndian.PutUint32(wav[24:], microphoneRate)
	binary.LittleEndian.PutUint32(wav[28:], microphoneRate*2)
	binary.LittleEndian.PutUint16(wav[32:], 2)
	binary.LittleEndian.PutUint16(wav[34:], 16)
	copy(wav[36:], "data")
	binary.LittleEndian.PutUint32(wav[40:], uint32(len(pcm)))
	copy(wav[44:], pcm)
	return wav
}
func mergeUtterances(first, second []byte) ([]byte, error) {
	return mergeUtterancesLimit(first, second, 29)
}

func mergeUtterancesLimit(first, second []byte, seconds int) ([]byte, error) {
	if len(first) == 0 {
		return second, nil
	}
	if len(first) < 44 || len(second) < 44 {
		return nil, fmt.Errorf("invalid pending voice input")
	}
	if len(first)+len(second)-88 > microphoneRate*seconds*2 {
		return nil, fmt.Errorf("pending speech exceeded %d seconds; earlier audio kept, latest segment not added", seconds)
	}
	pcm := append(append([]byte(nil), first[44:]...), second[44:]...)
	return capturedWAV(pcm), nil
}

func (d *Detector) threshold() float64 {
	floor := d.floor
	if floor == 0 {
		floor = .012
	}
	return max(floor, d.noise*3.5)
}
func (d *Detector) onsetSamples() int {
	if d.startSamples == 0 {
		return 3200
	}
	return d.startSamples
}

func (d *Detector) endpointSamples() int {
	if d.endSamples > 0 {
		return d.endSamples
	}
	return 17600
}
func (d *Detector) recordingSeconds() int {
	if d.maxSeconds > 0 {
		return d.maxSeconds
	}
	return 120
}

func (c *LiveCapture) Close()                   { c.cancel() }
func (c *LiveCapture) Context() context.Context { return c.ctx }
func NewDetector(floor float64, startMS, endMS, maxSeconds int) *Detector {
	return &Detector{floor: floor, startSamples: microphoneRate * startMS / 1000, endSamples: microphoneRate * endMS / 1000, maxSeconds: maxSeconds}
}
func (d *Detector) Active() bool { return d.active }
func MergeAudio(a, b []byte, maxSeconds int) ([]byte, error) {
	if len(b) == 0 {
		return a, nil
	}
	return mergeUtterancesLimit(a, b, maxSeconds)
}
