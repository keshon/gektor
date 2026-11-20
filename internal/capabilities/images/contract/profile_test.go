package contract

import (
	"bytes"
	"compress/zlib"
	"encoding/binary"
	"encoding/json"
	"hash/crc32"
	"image"
	"image/color"
	"image/png"
	"strings"
	"testing"

	"github.com/keshon/gektor/internal/capabilities/images"
	"github.com/keshon/gektor/internal/workspace"
)

// A synthetic RGB matrix/TRC profile keeps the integration fixture independent
// of a user's images and installed profiles. Color accuracy belongs to Pixelita.
func matrixProfilePNG(t *testing.T) []byte {
	t.Helper()
	profile := make([]byte, 132+6*12)
	copy(profile[4:], "test")
	profile[8] = 2
	copy(profile[12:], "mntrRGB XYZ ")
	copy(profile[36:], "acsp")
	for i, v := range []uint32{63190, 65536, 54061} {
		binary.BigEndian.PutUint32(profile[68+i*4:], v)
	}
	binary.BigEndian.PutUint32(profile[128:], 6)
	tags := []string{"rXYZ", "gXYZ", "bXYZ", "rTRC", "gTRC", "bTRC"}
	xyz := [3][3]uint32{{28576, 14581, 913}, {25239, 46983, 6362}, {9375, 3973, 46787}}
	for i, tag := range tags {
		var data []byte
		if i < 3 {
			data = make([]byte, 20)
			copy(data, "XYZ ")
			for j, v := range xyz[i] {
				binary.BigEndian.PutUint32(data[8+j*4:], v)
			}
		} else {
			data = make([]byte, 14)
			copy(data, "curv")
			binary.BigEndian.PutUint32(data[8:], 1)
			binary.BigEndian.PutUint16(data[12:], 563) // gamma 2.199
		}
		off := 132 + i*12
		copy(profile[off:], tag)
		binary.BigEndian.PutUint32(profile[off+4:], uint32(len(profile)))
		binary.BigEndian.PutUint32(profile[off+8:], uint32(len(data)))
		profile = append(profile, data...)
		for len(profile)%4 != 0 {
			profile = append(profile, 0)
		}
	}
	binary.BigEndian.PutUint32(profile, uint32(len(profile)))
	var compressed bytes.Buffer
	w := zlib.NewWriter(&compressed)
	if _, err := w.Write(profile); err != nil {
		t.Fatal(err)
	}
	if err := w.Close(); err != nil {
		t.Fatal(err)
	}
	payload := append([]byte("test\x00\x00"), compressed.Bytes()...)
	chunk := make([]byte, len(payload)+12)
	binary.BigEndian.PutUint32(chunk, uint32(len(payload)))
	copy(chunk[4:], "iCCP")
	copy(chunk[8:], payload)
	binary.BigEndian.PutUint32(chunk[len(chunk)-4:], crc32.ChecksumIEEE(chunk[4:len(chunk)-4]))
	img := image.NewNRGBA(image.Rect(0, 0, 128, 128))
	for y := range 128 {
		for x := range 128 {
			img.SetNRGBA(x, y, color.NRGBA{R: uint8(x / 8 * 16), G: uint8(y / 8 * 16), B: 128, A: 255})
		}
	}
	var encoded bytes.Buffer
	encoder := png.Encoder{CompressionLevel: png.NoCompression}
	if err := encoder.Encode(&encoded, img); err != nil {
		t.Fatal(err)
	}
	out := append([]byte(nil), encoded.Bytes()[:33]...)
	out = append(out, chunk...)
	return append(out, encoded.Bytes()[33:]...)
}

func TestProfileConversionReachesNativeOperations(t *testing.T) {
	ws, err := workspace.New(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	defer ws.Close()
	original := matrixProfilePNG(t)
	if err := ws.WriteFile("profile.png", original, 0600); err != nil {
		t.Fatal(err)
	}
	service := images.Service{WS: ws}
	preview := Operation{Service: service, Kind: "preview"}
	out, err := preview.Run(t.Context(), json.RawMessage(`{"paths":["profile.png"],"format":"webp"}`))
	if err == nil || !strings.Contains(out, "color_profile srgb") {
		t.Fatalf("missing supported recovery: %s / %v", out, err)
	}
	for _, tc := range []struct {
		kind, args, format string
		width              int
	}{
		{"preview", `{"paths":["profile.png"],"format":"webp","color_profile":"srgb"}`, "", 0},
		{"optimize", `{"paths":["profile.png"],"format":"webp","color_profile":"srgb","out_dir":"converted"}`, "webp", 128},
		{"variants", `{"paths":["profile.png"],"widths":[64],"color_profile":"srgb","out_dir":"resized"}`, "png", 64},
	} {
		out, err := (Operation{Service: service, Kind: tc.kind, AuthorizeOutput: allowOutput}).Run(t.Context(), json.RawMessage(tc.args))
		if err != nil {
			t.Fatalf("%s: %s / %v", tc.kind, out, err)
		}
		var result struct {
			Items []struct {
				Output  string
				Metrics struct{ ColorConversion struct{ Target string } }
			}
		}
		if err := json.Unmarshal([]byte(out), &result); err != nil || len(result.Items) != 1 || result.Items[0].Metrics.ColorConversion.Target != "sRGB" {
			t.Fatalf("%s / %v", out, err)
		}
		if tc.width == 0 {
			continue
		}
		f, err := ws.Open(result.Items[0].Output)
		if err != nil {
			t.Fatal(err)
		}
		img, format, err := image.Decode(f)
		f.Close()
		if err != nil || format != tc.format || img.Bounds().Dx() != tc.width {
			t.Fatalf("bad %s output: %s / %v", tc.kind, format, err)
		}
	}
	after, err := ws.ReadFile("profile.png")
	if err != nil || !bytes.Equal(original, after) {
		t.Fatal("source profile or pixels changed", err)
	}
}
