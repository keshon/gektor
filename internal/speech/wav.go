package speech

import (
	"encoding/binary"
	"errors"
)

const MaxInputSeconds = 120
const MaxInputBytes = 4_000_000

func ValidateInput(b []byte) error {
	if len(b) < 44 || string(b[:4]) != "RIFF" || string(b[8:12]) != "WAVE" {
		return errors.New("send a PCM WAV file")
	}
	if int(binary.LittleEndian.Uint32(b[4:8]))+8 != len(b) {
		return errors.New("incomplete WAV")
	}
	formatOK, dataOK := false, false
	for pos := 12; pos+8 <= len(b); {
		size := int(binary.LittleEndian.Uint32(b[pos+4 : pos+8]))
		start := pos + 8
		if size > len(b)-start {
			return errors.New("invalid WAV chunk")
		}
		switch string(b[pos : pos+4]) {
		case "fmt ":
			if size < 16 {
				return errors.New("invalid WAV format")
			}
			f := b[start : start+size]
			formatOK = binary.LittleEndian.Uint16(f) == 1 && binary.LittleEndian.Uint16(f[2:]) == 1 && binary.LittleEndian.Uint32(f[4:]) == 16000 && binary.LittleEndian.Uint16(f[14:]) == 16
		case "data":
			dataOK = size >= 3200 && size <= 16000*2*MaxInputSeconds && size%2 == 0
		}
		pos = start + size + size%2
	}
	if !formatOK || !dataOK {
		return errors.New("expected mono 16 kHz PCM16 WAV, 0.1-120 seconds")
	}
	return nil
}

// ValidateReference requires enough speech for a useful voice prompt.
func ValidateReference(b []byte) error {
	if err := ValidateInput(b); err != nil {
		return err
	}
	for pos := 12; pos+8 <= len(b); {
		size := int(binary.LittleEndian.Uint32(b[pos+4 : pos+8]))
		if string(b[pos:pos+4]) == "data" && (size < 16000*2*3 || size > 16000*2*30) {
			return errors.New("voice reference must contain 3вЂ“30 seconds of mono 16 kHz PCM16 speech")
		}
		pos += 8 + size + size%2
	}
	return nil
}
