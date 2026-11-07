package llm

import (
	"bytes"
	"image"
	"image/png"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestMisnamedImageUsesDetectedMIMEAndCacheExtension(t *testing.T) {
	var data bytes.Buffer
	if err := png.Encode(&data, image.NewNRGBA(image.Rect(0, 0, 2, 3))); err != nil {
		t.Fatal(err)
	}
	p := filepath.Join(t.TempDir(), "download.jpg")
	if err := os.WriteFile(p, data.Bytes(), 0600); err != nil {
		t.Fatal(err)
	}
	wire, err := EncodeImageFile(p)
	if err != nil || !strings.HasPrefix(wire, "data:image/png;base64,") {
		t.Fatal(wire, err)
	}
	saved, err := SnapshotImage(p, t.TempDir())
	if err != nil || filepath.Ext(saved) != ".png" {
		t.Fatal(saved, err)
	}
	original, _ := os.ReadFile(p)
	if !bytes.Equal(original, data.Bytes()) {
		t.Fatal("source modified")
	}
	if err := os.WriteFile(p, []byte("<html>not an image</html>"), 0600); err != nil {
		t.Fatal(err)
	}
	if _, err := EncodeImageFile(p); err == nil {
		t.Fatal("sent HTML as pixels")
	}
}
