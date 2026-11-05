package responses

import (
	"encoding/json"
	"github.com/keshon/gektor/internal/llm"
	"image"
	"image/png"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestResponsesMultipleImagesAndResume(t *testing.T) {
	dir := t.TempDir()
	paths := []string{}
	for _, name := range []string{"one.png", "two.png"} {
		p := filepath.Join(dir, name)
		f, _ := os.Create(p)
		png.Encode(f, image.NewRGBA(image.Rect(0, 0, 2, 2)))
		f.Close()
		paths = append(paths, p)
	}
	messages := []llm.Message{{Role: llm.RoleUser, Images: paths}}
	saved, _ := json.Marshal(messages)
	var restored []llm.Message
	json.Unmarshal(saved, &restored)
	c := newTestClient("https://api.openai.com/v1", "test")
	raw, err := c.responseRequest(llm.ChatRequest{Messages: restored})
	if err != nil {
		t.Fatal(err)
	}
	if strings.Count(string(raw), `"type":"input_image"`) != 2 || strings.Count(string(raw), "data:image/png;base64,") != 2 {
		t.Fatalf("missing images: %s", raw)
	}
	if strings.Contains(string(raw), `"type":"input_text"`) {
		t.Fatal("image-only request includes an empty text block")
	}
	os.Remove(paths[0])
	if _, err = c.responseRequest(llm.ChatRequest{Messages: restored}); err == nil {
		t.Fatal("missing image silently discarded")
	}
}
func TestResponsesRejectsBMPBeforeRequest(t *testing.T) {
	if _, err := encodeImage("shot.bmp"); err == nil || !strings.Contains(err.Error(), "PNG") {
		t.Fatal(err)
	}
}
