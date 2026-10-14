package speech

import (
	"context"
	"encoding/binary"
	"github.com/keshon/tars/internal/speech/vad"
	"os"
	"testing"
)

func frameLevel(value int16) []byte {
	frame := make([]byte, 640)
	for i := 0; i < len(frame); i += 2 {
		binary.LittleEndian.PutUint16(frame[i:], uint16(value))
	}
	return frame
}
func TestLivePreRollAndEndpoint(t *testing.T) {
	d := NewDetector(0, 200, 1100, 120)
	no, yes := false, true
	quiet := frameLevel(100)
	for i := 0; i < 20; i++ {
		if onset, _ := d.PushFrame(quiet, &no); onset {
			t.Fatal("noise triggered")
		}
	}
	voiced := frameLevel(10000)
	onsets := 0
	for i := 0; i < 10; i++ {
		onset, _ := d.PushFrame(voiced, &yes)
		if onset {
			onsets++
		}
	}
	if onsets != 1 || !d.Active() {
		t.Fatal("speech onset missing")
	}
	var wav []byte
	for i := 0; i < 55; i++ {
		_, wav = d.PushFrame(quiet, &no)
	}
	if len(wav) <= 44 || d.Active() {
		t.Fatal("endpoint did not finish")
	}
	if int16(binary.LittleEndian.Uint16(wav[44:])) != 100 {
		t.Fatal("first quiet words/pre-roll lost")
	}
	if err := ValidateInput(wav); err != nil {
		t.Fatal(err)
	}
}
func TestNeuralGateRejectsMovementNoise(t *testing.T) {
	d := NewDetector(0, 200, 1100, 120)
	no := false
	for i := 0; i < 100; i++ {
		if onset, _ := d.PushFrame(frameLevel(20000), &no); onset {
			t.Fatal("non-speech activated microphone")
		}
	}
}
func TestInstalledSilero(t *testing.T) {
	root := os.Getenv("TARS_VAD_TEST_ROOT")
	if root == "" {
		t.Skip("native CPU acceptance opt-in")
	}
	c, err := vad.Start(context.Background(), root)
	if err != nil {
		t.Fatal(err)
	}
	defer c.Close()
	for i := 0; i < 20; i++ {
		voiced, err := c.Speech(make([]byte, 640))
		if err != nil {
			t.Fatal(err)
		}
		if voiced {
			t.Fatal("silence classified as speech")
		}
	}
}
