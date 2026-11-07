package tools

import (
	"bufio"
	"context"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"regexp"
	"strings"
)

// grepSearch owns one search's output budget across every selected file.
// Context windows are local to scanFile, so unrelated files never merge.
type grepSearch struct {
	pattern                             *regexp.Regexp
	before, after                       int
	matches, searched, skippedSensitive int
	outputCapped                        bool
	out                                 strings.Builder
}

// looksBinary mirrors the heuristic git itself uses: if a null byte shows
// up in the first chunk of a file, treat it as binary and skip it
// entirely. Without this, grep_files will happily regex-scan a compiled
// binary (it did, in practice: a wildcard pattern matched across a long
// stretch of beacon.exe with no newlines, and raw non-UTF8 bytes —
// including control characters — went straight into the conversation
// history as a "match"). That's not just wasted context: it's exactly
// the kind of corrupted history that can derail everything after it.
func looksBinary(f *os.File) (bool, error) {
	buf := make([]byte, grepSniffBytes)
	n, err := f.Read(buf)
	if err != nil && !errors.Is(err, io.EOF) {
		return false, err
	}
	if _, seekErr := f.Seek(0, 0); seekErr != nil {
		return false, seekErr
	}
	for _, b := range buf[:n] {
		if b == 0 {
			return true, nil
		}
	}
	return false, nil
}

func (s *grepSearch) scanFile(ctx context.Context, f *os.File, rel string) error {
	if binary, err := looksBinary(f); err != nil {
		return err
	} else if binary {
		return nil
	}
	s.searched++

	scanner := bufio.NewScanner(f)
	scanner.Buffer(make([]byte, 64*1024), 1024*1024)
	lineNum := 0
	type contextLine struct {
		number int
		text   string
	}
	before := make([]contextLine, 0, s.before)
	lastPrinted, afterUntil := 0, 0
	emit := func(n int, text string, match bool) bool {
		if n <= lastPrinted {
			return true
		}
		separator := '-'
		if match {
			separator = ':'
		}
		row := fmt.Sprintf("%s%c%d%c%s\n", rel, separator, n, separator, text)
		if (s.before > 0 || s.after > 0) && s.out.Len() > 0 && (lastPrinted == 0 || n > lastPrinted+1) {
			row = "--\n" + row
		}
		if s.out.Len()+len(row) > readMaxBytes {
			s.outputCapped = true
			return false
		}
		s.out.WriteString(row)
		lastPrinted = n
		return true
	}
	for scanner.Scan() {
		if err := ctx.Err(); err != nil {
			return err
		}
		lineNum++
		line := scanner.Text()
		match := s.pattern.MatchString(line)
		if len(line) > grepMaxLineLen {
			line = strings.ToValidUTF8(line[:grepMaxLineLen], "") + "...(truncated)"
		}
		if match {
			for _, previous := range before {
				if !emit(previous.number, previous.text, false) {
					return filepath.SkipAll
				}
			}
			if !emit(lineNum, line, true) {
				return filepath.SkipAll
			}
			s.matches++
			afterUntil = lineNum + s.after
			if s.matches >= grepMaxMatches {
				break
			}
		} else if lineNum <= afterUntil {
			if !emit(lineNum, line, false) {
				return filepath.SkipAll
			}
		}
		if s.before > 0 {
			if len(before) == s.before {
				copy(before, before[1:])
				before[len(before)-1] = contextLine{lineNum, line}
			} else {
				before = append(before, contextLine{lineNum, line})
			}
		}
	}
	return scanner.Err()
}

func (s *grepSearch) result() string {
	if s.skippedSensitive > 0 {
		fmt.Fprintf(&s.out, "(skipped %d sensitive files; use read_file to request permission)\n", s.skippedSensitive)
	}
	if s.matches == 0 && s.skippedSensitive == 0 && !s.outputCapped {
		if s.searched == 0 {
			return "no searchable text files (check path/glob; use include_ignored=true for ignored files)"
		}
		return "no matches"
	}
	if s.matches >= grepMaxMatches {
		s.out.WriteString("... (capped at 200 matches, narrow your pattern or path)\n")
	}
	if s.outputCapped {
		s.out.WriteString("... (output capped at 48 KiB; narrow your pattern or path)\n")
	}
	return s.out.String()
}
