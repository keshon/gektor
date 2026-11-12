// Package output owns retained tool logs and bounded capture buffers.
package output

import (
	"context"
	"fmt"
	"strings"
	"sync"
	"unicode/utf8"
)

const PreviewBytes = 48 * 1024

// Buffer is an io.Writer safe for a running process to write to while
// check_background concurrently reads it.
type Buffer struct {
	mu    sync.Mutex
	head  strings.Builder
	tail  strings.Builder
	total int
	file  *Writer
	note  string
}

func (s *Buffer) Write(p []byte) (int, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	appendBounded(&s.head, &s.tail, &s.total, p)
	if s.file != nil {
		_, _ = s.file.Write(p)
	}
	return len(p), nil
}

func (s *Buffer) String() string {
	s.mu.Lock()
	defer s.mu.Unlock()
	return boundedOutput(s.head.String(), s.tail.String(), s.total) + s.note
}

// since returns retained bytes after cursor. Gaps from eviction are explicit;
// an unfinished UTF-8 character is left for the next read while running.
func (s *Buffer) Since(cursor int, final bool) (string, int, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	head, tail := s.head.String(), s.tail.String()
	end := s.total
	if !final {
		last := tail[max(0, len(tail)-utf8.UTFMax):]
		if len(tail) < utf8.UTFMax && s.total <= PreviewBytes {
			last = head[max(0, len(head)-(utf8.UTFMax-len(tail))):] + tail
		}
		if len(last) > 0 {
			start := len(last) - 1
			for start > 0 && !utf8.RuneStart(last[start]) {
				start--
			}
			if !utf8.FullRuneInString(last[start:]) {
				end -= len(last) - start
			}
		}
	}
	if cursor < 0 || cursor > end {
		return "", 0, fmt.Errorf("cursor must be between 0 and %d", end)
	}
	var out strings.Builder
	if cursor < len(head) {
		out.WriteString(head[cursor:min(len(head), end)])
	}
	tailStart := s.total - len(tail)
	if gap := tailStart - max(cursor, len(head)); gap > 0 {
		fmt.Fprintf(&out, "\n...(output truncated: %d bytes no longer retained)\n", gap)
	}
	if end > tailStart {
		out.WriteString(tail[max(cursor-tailStart, 0) : end-tailStart])
	}
	return strings.ToValidUTF8(out.String(), "�") + s.note, end, nil
}

// Keep both the beginning and recent tail without retaining unbounded logs.
func boundedOutput(head, tail string, total int) string {
	if total <= PreviewBytes {
		return head + tail
	}
	return head + "\n...(output truncated; recent tail follows)\n" + tail
}

func appendBounded(head, tail *strings.Builder, total *int, p []byte) {
	*total += len(p)
	half := PreviewBytes / 2
	if head.Len() < half {
		n := half - head.Len()
		if n > len(p) {
			n = len(p)
		}
		head.Write(p[:n])
		p = p[n:]
	}
	if len(p) == 0 {
		return
	}
	if len(p) >= half {
		tail.Reset()
		tail.Write(p[len(p)-half:])
		return
	}
	previous := tail.String()
	if len(previous)+len(p) > half {
		previous = previous[len(previous)+len(p)-half:]
	}
	tail.Reset()
	tail.WriteString(previous)
	tail.Write(p)
}

// Capture owns a retained log until Finish; readers may inspect it while running.
func Capture(ctx context.Context) *Buffer {
	b := &Buffer{}
	store := FromContext(ctx)
	if store != nil {
		file, err := store.Create()
		if err != nil {
			b.note = "\nOutput storage warning: " + err.Error()
		} else {
			b.file = file
		}
	}
	return b
}
func (s *Buffer) Size() int { s.mu.Lock(); defer s.mu.Unlock(); return s.total }
func (s *Buffer) Finish() {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.file != nil {
		s.note = "\n" + s.file.Finish()
		s.file = nil
	}
}
