// Package output owns retained tool logs and session-scoped recovery references.
package output

import (
	"context"
	"crypto/rand"
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"time"
	"unicode/utf8"

	"github.com/keshon/gektor/internal/userdirs"
)

const outputFileLimit = 16 * 1024 * 1024
const outputSessionLimit = 128 * 1024 * 1024

type Store struct {
	dir      string
	session  string
	mu       sync.Mutex
	reserved int64
}
type contextKey struct{}

func New(session string) *Store {
	owner := session
	if session == "" {
		session = randomOutputID()
	}
	sum := sha256.Sum256([]byte(session))
	return &Store{dir: filepath.Join(userdirs.Sub("output"), hex.EncodeToString(sum[:12])), session: owner}
}
func (s *Store) Context(ctx context.Context) context.Context {
	return context.WithValue(ctx, contextKey{}, s)
}
func FromContext(ctx context.Context) *Store {
	s, _ := ctx.Value(contextKey{}).(*Store)
	return s
}
func randomOutputID() string { var b [16]byte; _, _ = rand.Read(b[:]); return hex.EncodeToString(b[:]) }

type Writer struct {
	store   *Store
	id      string
	file    *os.File
	written int
	problem error
}

func (s *Store) Create() (*Writer, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if err := os.MkdirAll(s.dir, 0700); err != nil {
		return nil, err
	}
	entries, err := os.ReadDir(s.dir)
	if err != nil {
		return nil, err
	}
	var used int64
	for _, entry := range entries {
		info, err := entry.Info()
		if err != nil {
			continue
		}
		if time.Since(info.ModTime()) > 7*24*time.Hour {
			_ = os.Remove(filepath.Join(s.dir, entry.Name()))
			continue
		}
		used += info.Size()
	}
	if used+s.reserved > outputSessionLimit-outputFileLimit {
		return nil, fmt.Errorf("session output quota reached; existing output was retained")
	}
	id := randomOutputID()
	f, err := os.OpenFile(filepath.Join(s.dir, id+".log"), os.O_CREATE|os.O_EXCL|os.O_WRONLY, 0600)
	if err != nil {
		return nil, err
	}
	s.reserved += outputFileLimit
	return &Writer{store: s, id: id, file: f}, nil
}
func (f *Writer) Write(p []byte) (int, error) {
	n := len(p)
	if f.problem != nil {
		return n, nil
	}
	remaining := outputFileLimit - f.written
	if len(p) > remaining {
		p = p[:remaining]
		f.problem = fmt.Errorf("output retention limit %d bytes reached; later bytes were not saved", outputFileLimit)
	}
	written, err := f.file.Write(p)
	f.written += written
	if err != nil {
		f.problem = err
	}
	return n, nil
}
func (f *Writer) Finish() string {
	if err := f.file.Close(); err != nil && f.problem == nil {
		f.problem = err
	}
	f.store.mu.Lock()
	f.store.reserved -= outputFileLimit
	f.store.mu.Unlock()
	note := fmt.Sprintf("Full output: read_output {\"id\":%q,\"offset\":0,\"limit\":8192} (%d bytes retained; expires after 7 days)", f.id, f.written)
	if f.problem != nil {
		note += "\nOutput storage warning: " + f.problem.Error()
	}
	return note
}

// Limit uses the store bound to ctx for adapter-produced results.
func Limit(ctx context.Context, text string, limit int) string {
	return FromContext(ctx).Limit(text, limit)
}

// Limit retains oversized output and returns a bounded model-facing preview.
// A nil store returns a preview with an explicit unavailable-storage notice.
func (s *Store) Limit(text string, limit int) string {
	if len(text) <= limit {
		return text
	}
	note := "Full output unavailable: no session output store."
	if s != nil {
		if f, err := s.Create(); err == nil {
			_, _ = f.Write([]byte(text))
			note = f.Finish()
		} else {
			note = "Output storage warning: " + err.Error()
		}
	}
	return TruncateMiddle(text, max(1, limit-len(note)-2)) + "\n" + note
}

// Scope identifies output belonging to the same session, including reconstructed stores.
func (s *Store) Scope() string     { return s.dir }
func (s *Store) SessionID() string { return s.session }

// Failed reports retention or close errors, which are also included by Finish.
func (f *Writer) Failed() bool { return f.problem != nil }

// Discard removes a finished output that did not need a recovery reference.
func (f *Writer) Discard() error { return os.Remove(f.file.Name()) }

type Page struct {
	Text  string `json:"text"`
	Next  int    `json:"next_offset"`
	Total int64  `json:"total_bytes"`
	EOF   bool   `json:"eof"`
}

func (s *Store) Read(id string, offset, limit int) (Page, error) {
	in := struct {
		ID            string
		Offset, Limit int
	}{id, offset, limit}
	if len(in.ID) != 32 || strings.Trim(in.ID, "abcdef0123456789") != "" || in.Offset < 0 {
		return Page{}, fmt.Errorf("invalid output id or offset")
	}
	if in.Limit == 0 {
		in.Limit = 8192
	}
	if in.Limit < 1 || in.Limit > 49152 {
		return Page{}, fmt.Errorf("limit must be 1–49152")
	}
	if s == nil {
		return Page{}, fmt.Errorf("no session output store")
	}
	f, err := os.Open(filepath.Join(s.dir, in.ID+".log"))
	if err != nil {
		return Page{}, fmt.Errorf("output unavailable in this session (unknown id or expired): %w", err)
	}
	defer f.Close()
	info, err := f.Stat()
	if err != nil {
		return Page{}, err
	}
	if time.Since(info.ModTime()) > 7*24*time.Hour {
		return Page{}, fmt.Errorf("output expired after 7 days")
	}
	if int64(in.Offset) > info.Size() {
		return Page{}, fmt.Errorf("offset exceeds output size %d", info.Size())
	}
	data, err := io.ReadAll(io.NewSectionReader(f, int64(in.Offset), int64(in.Limit)))
	if err != nil {
		return Page{}, err
	}
	if len(data) > 0 && !utf8.RuneStart(data[0]) {
		return Page{}, fmt.Errorf("offset is inside a UTF-8 character; use the returned next_offset")
	}
	if int64(in.Offset+len(data)) < info.Size() && len(data) > 0 {
		last := len(data) - 1
		for last > 0 && !utf8.RuneStart(data[last]) {
			last--
		}
		if !utf8.FullRune(data[last:]) {
			data = data[:last]
		}
		if len(data) == 0 {
			return Page{}, fmt.Errorf("limit is too small for the next UTF-8 character; increase it")
		}
	}
	result := Page{string(data), in.Offset + len(data), info.Size(), int64(in.Offset+len(data)) == info.Size()}
	return result, nil
}

// TruncateMiddle shortens s to at most max bytes, keeping the start and end
// with " ... " in the middle. max <= 0 means no limit.
func TruncateMiddle(s string, max int) string {
	if max <= 0 || len(s) <= max {
		return s
	}
	const sep = " ... "
	sepLen := len(sep)
	if max <= sepLen+2 {
		if max <= 3 {
			return s[:max]
		}
		return s[:max-3] + "..."
	}
	side := (max - sepLen) / 2
	return s[:side] + sep + s[len(s)-side:]
}
