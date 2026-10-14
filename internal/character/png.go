package character

import (
	"bytes"
	"encoding/base64"
	"encoding/binary"
	"encoding/json"
	"fmt"
	"hash/crc32"
	"io"
)

const maxCardBytes = 65536
const maxPNGBytes = 32 << 20

var pngSignature = []byte{137, 80, 78, 71, 13, 10, 26, 10}

// readPNGCard extracts metadata without decoding pixels or altering the image.
// CCv3 takes precedence over a compatibility chara chunk when both exist.
func readPNGCard(r io.Reader) ([]byte, error) { return readPNGCardWarnings(r, nil) }

func readPNGCardWarnings(r io.Reader, warnings *[]string) ([]byte, error) {
	data, err := io.ReadAll(io.LimitReader(r, maxPNGBytes+1))
	if err != nil {
		return nil, err
	}
	if len(data) > maxPNGBytes {
		return nil, fmt.Errorf("character PNG exceeds 32 MiB")
	}
	if len(data) < 8 || !bytes.Equal(data[:8], pngSignature) {
		return nil, fmt.Errorf("invalid character PNG signature")
	}
	cards := make(map[string][]byte)
	sawImage := false
	for pos := 8; pos < len(data); {
		if len(data)-pos < 12 {
			return nil, fmt.Errorf("truncated character PNG chunk")
		}
		size := int64(binary.BigEndian.Uint32(data[pos : pos+4]))
		if size > int64(len(data)-pos-12) {
			return nil, fmt.Errorf("truncated character PNG payload")
		}
		end := pos + 8 + int(size)
		kind := string(data[pos+4 : pos+8])
		body := data[pos+8 : end]
		if crc32.ChecksumIEEE(data[pos+4:end]) != binary.BigEndian.Uint32(data[end:end+4]) {
			return nil, fmt.Errorf("invalid character PNG %s checksum", kind)
		}
		if pos == 8 && (kind != "IHDR" || size != 13) {
			return nil, fmt.Errorf("character PNG requires an IHDR header")
		}
		if kind == "IDAT" {
			sawImage = true
		}
		if kind == "tEXt" {
			key, value, ok := bytes.Cut(body, []byte{0})
			key = bytes.ToLower(key)
			if ok && (string(key) == "chara" || string(key) == "ccv3") {
				decoded, decodeErr := decodePNGMetadata(value)
				if decodeErr != nil {
					addCardWarning(warnings, fmt.Sprintf("Skipped invalid PNG %s metadata: %v", key, decodeErr))
					pos = end + 4
					continue
				}
				if old, exists := cards[string(key)]; exists {
					if !sameCardJSON(old, decoded) {
						addCardWarning(warnings, fmt.Sprintf("Conflicting PNG %s metadata: first valid card retained.", key))
					}
					pos = end + 4
					continue
				}
				cards[string(key)] = decoded
			}
		}
		pos = end + 4
		if kind == "IEND" {
			if size != 0 || !sawImage || pos != len(data) {
				return nil, fmt.Errorf("invalid character PNG ending")
			}
			if card, ok := cards["ccv3"]; ok {
				return card, nil
			}
			if card, ok := cards["chara"]; ok {
				return card, nil
			}
			return nil, fmt.Errorf("PNG contains no embedded character card (chara or ccv3 metadata)")
		}
	}
	return nil, fmt.Errorf("character PNG has no IEND chunk")
}

func addCardWarning(warnings *[]string, text string) {
	if warnings != nil && len(*warnings) < 8 {
		*warnings = append(*warnings, text)
	}
}
func decodePNGMetadata(value []byte) ([]byte, error) {
	if len(value) > base64.StdEncoding.EncodedLen(maxCardBytes) {
		return nil, fmt.Errorf("embedded card exceeds 64 KiB")
	}
	decoded, err := base64.StdEncoding.Strict().DecodeString(string(value))
	if err != nil {
		decoded, err = base64.RawStdEncoding.Strict().DecodeString(string(value))
	}
	if err != nil {
		return nil, fmt.Errorf("invalid base64")
	}
	if len(decoded) > maxCardBytes {
		return nil, fmt.Errorf("embedded card exceeds 64 KiB")
	}
	if _, err := decodeCard(decoded); err != nil {
		return nil, err
	}
	return decoded, nil
}
func sameCardJSON(a, b []byte) bool {
	compact := func(data []byte) []byte {
		var value any
		decoder := json.NewDecoder(bytes.NewReader(data))
		decoder.UseNumber()
		_ = decoder.Decode(&value)
		encoded, _ := json.Marshal(value)
		return encoded
	}
	return bytes.Equal(compact(a), compact(b))
}
