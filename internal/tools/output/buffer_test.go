package output

import (
	"strings"
	"testing"
)

func TestBackgroundCursorRetainsDeltasAndReportsEviction(t *testing.T) {
	buffer := &Buffer{}
	buffer.Write([]byte("first\n"))
	out, cursor, err := buffer.Since(0, false)
	if err != nil || out != "first\n" {
		t.Fatalf("initial output: %q %v", out, err)
	}
	buffer.Write([]byte("second\n"))
	for range 2 {
		delta, next, err := buffer.Since(cursor, false)
		if err != nil || delta != "second\n" || next != len("first\nsecond\n") {
			t.Fatalf("cursor was destructive or repeated old output: %q %d %v", delta, next, err)
		}
	}
	buffer.Write([]byte(strings.Repeat("x", PreviewBytes*3) + "last"))
	out, cursor, err = buffer.Since(cursor, false)
	if err != nil || !strings.Contains(out, "output truncated") || !strings.HasSuffix(out, "last") || len(out) > PreviewBytes+200 {
		t.Fatalf("eviction lost its diagnostic or newest output: bytes=%d %v", len(out), err)
	}
	buffer.Write([]byte("new"))
	out, next, err := buffer.Since(cursor, false)
	if err != nil || out != "new" || next != cursor+3 {
		t.Fatalf("delta after eviction: %q %d %v", out, next, err)
	}
	out, _, err = buffer.Since(next, false)
	if err != nil || out != "" {
		t.Fatalf("idle poll repeated output: %q %v", out, err)
	}
}

func TestBackgroundCursorPreservesSplitUTF8AcrossBufferSegments(t *testing.T) {
	buffer := &Buffer{}
	buffer.Write([]byte(strings.Repeat("a", PreviewBytes/2-1)))
	_, cursor, _ := buffer.Since(0, false)
	for i, b := range []byte("😀") {
		buffer.Write([]byte{b})
		out, next, err := buffer.Since(cursor, false)
		want := ""
		if i == len("😀")-1 {
			want = "😀"
		}
		if err != nil || out != want {
			t.Fatalf("split character at byte %d: %q %v", i, out, err)
		}
		cursor = next
	}
	buffer.Write([]byte{0xf0})
	out, next, _ := buffer.Since(cursor, false)
	if out != "" || next != cursor {
		t.Fatal("acknowledged an incomplete UTF-8 character")
	}
	out, next, _ = buffer.Since(cursor, true)
	if out != "�" || next != cursor+1 {
		t.Fatal("final incomplete character was not flushed")
	}
}
