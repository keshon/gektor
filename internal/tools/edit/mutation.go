package edit

import (
	"fmt"
	"path/filepath"
	"runtime"
	"strings"
	"sync"
)

type pathLock struct {
	mu    sync.Mutex
	users int // Includes callers waiting to acquire mu.
}

// Entries live only while a caller owns or waits for the path lock.
var pathLocks = struct {
	mu sync.Mutex
	m  map[string]*pathLock
}{m: map[string]*pathLock{}}

// withPathLock takes a validated absolute workspace path.
func withPathLock(path string, fn func() (string, error)) (string, error) {
	key := mutationPathKey(path)
	pathLocks.mu.Lock()
	l, ok := pathLocks.m[key]
	if !ok {
		l = &pathLock{}
		pathLocks.m[key] = l
	}
	l.users++
	pathLocks.mu.Unlock()
	l.mu.Lock()
	defer func() {
		l.mu.Unlock()
		pathLocks.mu.Lock()
		l.users--
		if l.users == 0 {
			delete(pathLocks.m, key)
		}
		pathLocks.mu.Unlock()
	}()
	return fn()
}

func mutationPathKey(path string) string {
	if resolved, err := filepath.EvalSymlinks(path); err == nil {
		path = resolved
	} else if parent, err := filepath.EvalSymlinks(filepath.Dir(path)); err == nil {
		path = filepath.Join(parent, filepath.Base(path))
	}
	path = filepath.Clean(path)
	if runtime.GOOS == "windows" {
		path = strings.ToLower(path)
	}
	return path
}

// normalizeContent strips a BOM and converts CRLF to LF for matching.
// Returns the normalized text and the file's original line ending.
func normalizeContent(data []byte) (text, ending string) {
	s := string(data)
	s = strings.TrimPrefix(s, "\xef\xbb\xbf")
	ending = "\n"
	if strings.Contains(s, "\r\n") {
		ending = "\r\n"
	}
	return strings.ReplaceAll(s, "\r\n", "\n"), ending
}

// denormalize converts LF back to the file's original line ending.
func denormalize(s, ending string) string {
	if ending == "\r\n" {
		return strings.ReplaceAll(s, "\n", "\r\n")
	}
	return s
}

// guardEdit rejects empty or no-op edits before touching disk.
func guardEdit(tool, oldContent, newContent string) error {
	if strings.TrimSpace(oldContent) == "" {
		return fmt.Errorf("%s: old_content is empty — match the exact text to replace", tool)
	}
	if oldContent == newContent {
		return fmt.Errorf("%s: old_content and new_content are identical — nothing would change", tool)
	}
	return guardWriteSize(tool, newContent)
}

// writeMaxBytes caps one file-writing call's new content. Refused,
// never truncated: a cut write corrupts silently, and legitimate
// artifacts rarely approach a megabyte (reads cap at 48K; the loop
// re-reads what it writes).
const writeMaxBytes = 1024 * 1024

// guardWriteSize refuses oversized writes before they touch disk.
func guardWriteSize(tool, content string) error {
	if len(content) > writeMaxBytes {
		return fmt.Errorf("%s: content %d bytes exceeds write cap %d — split the write", tool, len(content), writeMaxBytes)
	}
	return nil
}

// unifiedDiff describes the single changed region using complete file lines.
// End-of-file markers matter: a final newline is not an extra empty line.
func unifiedDiff(path, before, after string) string {
	split := func(s string) []string {
		if s == "" {
			return nil
		}
		return strings.Split(strings.TrimSuffix(s, "\n"), "\n")
	}
	oldLines, newLines := split(before), split(after)
	oldNL, newNL := strings.HasSuffix(before, "\n"), strings.HasSuffix(after, "\n")
	start := 0
	for start < min(len(oldLines), len(newLines)) && oldLines[start] == newLines[start] {
		start++
	}
	if start > 0 && ((start == len(oldLines) && !oldNL) || (start == len(newLines) && !newNL) || oldNL != newNL) {
		start--
	}
	oldEnd, newEnd := len(oldLines), len(newLines)
	for oldEnd > start && newEnd > start && oldLines[oldEnd-1] == newLines[newEnd-1] {
		if oldEnd == len(oldLines) && newEnd == len(newLines) && oldNL != newNL {
			break
		}
		oldEnd--
		newEnd--
	}
	cs, ce := max(start-3, 0), min(oldEnd+3, len(oldLines))
	oldCount, newCount := ce-cs, start-cs+(newEnd-start)+(ce-oldEnd)
	oldStart, newStart := cs+1, cs+1
	if oldCount == 0 {
		oldStart = cs
	}
	if newCount == 0 {
		newStart = cs
	}
	var b strings.Builder
	fmt.Fprintf(&b, "--- %s\n+++ %s\n@@ -%d,%d +%d,%d @@\n", path, path, oldStart, oldCount, newStart, newCount)
	line := func(prefix string, lines []string, n int, finalNL bool) {
		b.WriteString(prefix + lines[n] + "\n")
		if n == len(lines)-1 && !finalNL {
			b.WriteString("\\ No newline at end of file\n")
		}
	}
	for n := cs; n < start; n++ {
		line(" ", oldLines, n, oldNL)
	}
	for n := start; n < oldEnd; n++ {
		line("-", oldLines, n, oldNL)
	}
	for n := start; n < newEnd; n++ {
		line("+", newLines, n, newNL)
	}
	for n := oldEnd; n < ce; n++ {
		line(" ", oldLines, n, oldNL)
	}
	return b.String()
}

// isUTF16 reports a BOM-prefixed UTF-16 file, which text patches refuse:
// byte-matching UTF-8 old_content against UTF-16 bytes never hits, and a
// silent re-encode would corrupt the file for its real consumer.
//
// Without this guard the model sees "old_content not found" forever and
// has no way to learn why: a live run circled for seven steps re-reading
// the same file, because every re-read came back as mojibake it could not
// match against the plain text it had just been shown.
func isUTF16(data []byte) bool {
	return len(data) >= 2 && ((data[0] == 0xFF && data[1] == 0xFE) ||
		(data[0] == 0xFE && data[1] == 0xFF))
}

// refuseUTF16 is the patch-side guard: name the encoding and the fix
// instead of letting the caller fail a match that can never succeed.
func refuseUTF16(tool, path string, data []byte) error {
	if !isUTF16(data) {
		return nil
	}
	enc := "utf-16le"
	if data[0] == 0xFE {
		enc = "utf-16be"
	}
	return fmt.Errorf("%s: %s is %s (BOM-prefixed, not UTF-8) — old_content can never match its bytes. "+
		"Convert it first (e.g. run_shell: python -c \"open('%s','rb').read().decode('utf-16').encode('utf-8')\" "+
		"written to the same path), then patch it", tool, path, enc, path)
}
