package command

import (
	"strings"
	"testing"
	"unicode/utf8"

	tooloutput "github.com/keshon/gektor/internal/tools/output"
)

func TestCommandOutputStreamsSplitUTF8AndBoundsPreview(t *testing.T) {
	var chunks []string
	out := &commandOutput{onOutput: func(text string) {
		if !utf8.ValidString(text) {
			t.Errorf("invalid UTF-8 chunk: %q", text)
		}
		chunks = append(chunks, text)
	}}
	for _, b := range []byte("привет 😀\n") {
		out.Write([]byte{b})
	}
	if got := strings.Join(chunks, ""); got != "привет 😀\n" {
		t.Fatalf("preview=%q", got)
	}
	out.Write([]byte(strings.Repeat("x", tooloutput.PreviewBytes*3) + "last log line"))
	out.flush()
	preview := strings.Join(chunks, "")
	if len(preview) > tooloutput.PreviewBytes+200 || strings.Count(preview, "live preview limit reached") != 1 {
		t.Fatalf("unbounded preview or repeated notice: %d bytes", len(preview))
	}
	if !strings.HasSuffix(out.String(), "last log line") {
		t.Fatal("final result lost recent output")
	}
}
