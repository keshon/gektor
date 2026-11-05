package openai

import (
	"encoding/json"
	"testing"
)

func TestModelVisionMetadata(t *testing.T) {
	for _, tc := range []struct {
		raw           string
		vision, known bool
	}{{`{"slug":"test","input_modalities":["text","image"]}`, true, true}, {`{"slug":"test","input_modalities":["text"]}`, false, true}, {`{"slug":"test"}`, false, false}, {`{"slug":"test","input_modalities":[]}`, false, true}} {
		var m Model
		if err := json.Unmarshal([]byte(tc.raw), &m); err != nil {
			t.Fatal(err)
		}
		got, err := m.Vision()
		if got != tc.vision || (err == nil) != tc.known {
			t.Fatalf("%s: %v %v", tc.raw, got, err)
		}
	}
}
