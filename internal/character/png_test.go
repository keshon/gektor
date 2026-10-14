package character

import (
	"bytes"
	"encoding/base64"
	"encoding/binary"
	"hash/crc32"
	"image"
	"image/png"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func textChunk(key, value string) []byte {
	body := append(append([]byte(key), 0), []byte(value)...)
	chunk := make([]byte, 12+len(body))
	binary.BigEndian.PutUint32(chunk, uint32(len(body)))
	copy(chunk[4:], "tEXt")
	copy(chunk[8:], body)
	binary.BigEndian.PutUint32(chunk[8+len(body):], crc32.ChecksumIEEE(chunk[4:8+len(body)]))
	return chunk
}
func cardPNG(t *testing.T, chunks ...[]byte) []byte {
	t.Helper()
	var b bytes.Buffer
	if err := png.Encode(&b, image.NewRGBA(image.Rect(0, 0, 1, 1))); err != nil {
		t.Fatal(err)
	}
	data := b.Bytes()
	out := append([]byte(nil), data[:len(data)-12]...)
	for _, chunk := range chunks {
		out = append(out, chunk...)
	}
	return append(out, data[len(data)-12:]...)
}
func encodedCard(raw string) string { return base64.StdEncoding.EncodeToString([]byte(raw)) }
func TestPNGCardsShareJSONLoadingAndKeepSource(t *testing.T) {
	for _, raw := range []string{`{"name":"Ada","description":"Hello {{user}}"}`, `{"spec":"chara_card_v2","spec_version":"2.0","data":{"name":"Ada","description":"Hello {{user}}"}}`} {
		data := cardPNG(t, textChunk("chara", encodedCard(raw)))
		path := filepath.Join(t.TempDir(), "Ada.PNG")
		if err := os.WriteFile(path, data, 0600); err != nil {
			t.Fatal(err)
		}
		card, err := Load(path)
		if err != nil || card.Name != "Ada" || !strings.Contains(card.Instruction("Alex", "Base"), "Hello Alex") {
			t.Fatal(card, err)
		}
		after, _ := os.ReadFile(path)
		if !bytes.Equal(data, after) {
			t.Fatal("PNG source modified")
		}
	}
}
func TestCCV3MetadataTakesPrecedence(t *testing.T) {
	data := cardPNG(t, textChunk("chara", encodedCard(`{"name":"Legacy"}`)), textChunk("ccv3", encodedCard(`{"spec":"chara_card_v3","spec_version":"3.0","data":{"name":"Modern"}}`)))
	path := filepath.Join(t.TempDir(), "card.png")
	os.WriteFile(path, data, 0600)
	card, err := Load(path)
	if err != nil || card.Name != "Modern" {
		t.Fatal(card, err)
	}
}
func TestPNGImportErrors(t *testing.T) {
	good := cardPNG(t, textChunk("chara", encodedCard(`{"name":"Ada"}`)))
	badCRC := append([]byte(nil), good...)
	badCRC[len(badCRC)-1] ^= 1
	cases := [][]byte{
		cardPNG(t), good[:len(good)-1], badCRC,
		cardPNG(t, textChunk("chara", "not base64!")),
		cardPNG(t, textChunk("chara", encodedCard("not JSON"))),
		cardPNG(t, textChunk("chara", encodedCard(strings.Repeat("x", maxCardBytes+1)))),
	}
	for i, data := range cases {
		path := filepath.Join(t.TempDir(), "card.png")
		os.WriteFile(path, data, 0600)
		if _, err := Load(path); err == nil {
			t.Fatalf("case %d accepted", i)
		}
	}
}

func TestDuplicateMetadataSelectionAndWarnings(t *testing.T) {
	for _, test := range []struct {
		chunks   [][]byte
		want     string
		warnings int
	}{
		{[][]byte{textChunk("chara", encodedCard(`{"name":"Ada"}`)), textChunk("CHARA", encodedCard(`{"name":"Ada"}`))}, "Ada", 0},
		{[][]byte{textChunk("chara", encodedCard(`{"name":"Ada"}`)), textChunk("chara", encodedCard(`{"name":"Other"}`))}, "Ada", 1},
		{[][]byte{textChunk("chara", "broken"), textChunk("chara", encodedCard(`{"name":"Ada"}`))}, "Ada", 1},
	} {
		path := filepath.Join(t.TempDir(), "card.png")
		os.WriteFile(path, cardPNG(t, test.chunks...), 0600)
		c, err := Load(path)
		if err != nil || c.Name != test.want || len(c.Warnings) != test.warnings {
			t.Fatal(c, err)
		}
	}
}
