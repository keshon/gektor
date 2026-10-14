package speech

import (
	"encoding/binary"
	"testing"
)

func testWAV() []byte {
	b := make([]byte, 44+3200)
	copy(b, "RIFF")
	binary.LittleEndian.PutUint32(b[4:], uint32(len(b)-8))
	copy(b[8:], "WAVEfmt ")
	binary.LittleEndian.PutUint32(b[16:], 16)
	binary.LittleEndian.PutUint16(b[20:], 1)
	binary.LittleEndian.PutUint16(b[22:], 1)
	binary.LittleEndian.PutUint32(b[24:], 16000)
	binary.LittleEndian.PutUint32(b[28:], 32000)
	binary.LittleEndian.PutUint16(b[32:], 2)
	binary.LittleEndian.PutUint16(b[34:], 16)
	copy(b[36:], "data")
	binary.LittleEndian.PutUint32(b[40:], 3200)
	return b
}
func TestRejectMalformedAudio(t *testing.T) {
	b := testWAV()
	if err := ValidateInput(b); err != nil {
		t.Fatal(err)
	}
	for _, v := range [][]byte{b[:20], append([]byte{}, b[:len(b)-1]...), make([]byte, 44)} {
		if ValidateInput(v) == nil {
			t.Fatal("malformed audio accepted")
		}
	}
	binary.LittleEndian.PutUint32(b[24:], 48000)
	if ValidateInput(b) == nil {
		t.Fatal("wrong rate accepted")
	}
}

func TestReferenceDuration(t *testing.T) {
	if ValidateReference(testWAV()) == nil {
		t.Fatal("short reference accepted")
	}
	b := make([]byte, 44+96000)
	copy(b, testWAV()[:44])
	binary.LittleEndian.PutUint32(b[4:], uint32(len(b)-8))
	binary.LittleEndian.PutUint32(b[40:], 96000)
	if err := ValidateReference(b); err != nil {
		t.Fatal(err)
	}
}

func TestLongInputAndReferenceLimits(t *testing.T) {
	for _, seconds := range []int{40, 120, 121} {
		b := make([]byte, 44+16000*2*seconds)
		copy(b, testWAV()[:44])
		binary.LittleEndian.PutUint32(b[4:], uint32(len(b)-8))
		binary.LittleEndian.PutUint32(b[40:], uint32(len(b)-44))
		err := ValidateInput(b)
		if (seconds <= 120) != (err == nil) {
			t.Fatalf("%d: %v", seconds, err)
		}
		if ValidateReference(b) == nil {
			t.Fatal("long voice reference accepted")
		}
	}
}
