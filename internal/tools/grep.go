package tools

import (
	"bufio"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"regexp"
	"strings"

	"github.com/keshon/gektor/internal/agent"
	"github.com/keshon/gektor/internal/permission"
	"github.com/keshon/gektor/internal/workspace"
)

// GrepFiles searches file contents by regex instead of requiring read_file
// on every candidate — the same "remove the failure surface" reasoning as
// move_file: a Go-native search can't be defeated by cmd.exe vs sh syntax
// differences the way shelling out to findstr/grep would be.
type GrepFiles struct{ WS *workspace.Workspace }

func (GrepFiles) Name() string         { return "grep_files" }
func (GrepFiles) Mode() agent.ToolMode { return agent.Concurrent }

// Idempotent: a search returns the same matches until something mutates.
func (GrepFiles) Idempotent() bool { return true }
func (GrepFiles) Description() string {
	return "Search a file or directory (recursive) for a Go regex pattern, or exact text with literal=true. Explicit files and ignored directory roots are searched even when Git ignores them. Returns " +
		"path:line:content for each match, capped at 200 matches; long lines are truncated. " +
		"before_context and after_context add up to 10 surrounding lines each; context uses path-line-content. Overlapping windows are merged and total output is bounded. " +
		"Binary files (compiled executables, images, etc) are skipped automatically. Use this " +
		"instead of read_file-ing many files just to find where something is."
}
func (GrepFiles) Schema() json.RawMessage {
	return json.RawMessage(`{
		"type": "object",
		"properties": {
			"path": {"type": "string", "description": "file or directory to search; defaults to the workspace root"},
			"pattern": {"type": "string", "description": "regular expression"},
			"literal": {"type":"boolean", "description":"match exact text instead of regex; avoids escaping punctuation"},
			"glob": {"type": "string", "description": "optional filename glob filter, e.g. *.go"},
			"before_context": {"type":"integer","minimum":0,"maximum":10},
			"after_context": {"type":"integer","minimum":0,"maximum":10},
			"include_ignored": {"type":"boolean", "description":"include generated and hidden directories; default false"}
		},
		"required": ["pattern"],
		"additionalProperties": false
	}`)
}

const (
	grepMaxMatches = 200
	grepMaxLineLen = 500  // truncate pathologically long matched lines (minified JS, etc)
	grepSniffBytes = 8000 // how much of a file to check for binary content before scanning
)

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

func (t GrepFiles) Run(ctx context.Context, args json.RawMessage) (string, error) {
	var in struct {
		Path           string `json:"path"`
		Pattern        string `json:"pattern"`
		Literal        bool   `json:"literal"`
		Glob           string `json:"glob"`
		IncludeIgnored bool   `json:"include_ignored"`
		Before         int    `json:"before_context"`
		After          int    `json:"after_context"`
	}
	if err := json.Unmarshal(args, &in); err != nil {
		return "", fmt.Errorf("bad arguments: %w", err)
	}
	if err := rejectUnknownFields(args, "path", "pattern", "literal", "glob", "include_ignored", "before_context", "after_context"); err != nil {
		return "", err
	}
	if in.Before < 0 || in.Before > 10 || in.After < 0 || in.After > 10 {
		return "", fmt.Errorf("before_context and after_context must be between 0 and 10")
	}
	if strings.TrimSpace(in.Path) == "" {
		in.Path = "."
	}
	if strings.TrimSpace(in.Pattern) == "" {
		return "", fmt.Errorf("pattern is required and must be non-empty")
	}
	pattern := in.Pattern
	if in.Literal {
		pattern = regexp.QuoteMeta(pattern)
	}
	re, err := regexp.Compile(pattern)
	if err != nil {
		return "", fmt.Errorf("bad pattern: %w", err)
	}
	root, err := t.WS.Resolve(in.Path)
	if err != nil {
		return "", err
	}
	info, err := os.Stat(root)
	if err != nil {
		return "", err
	}
	if _, err := filepath.Match(in.Glob, ""); err != nil {
		return "", fmt.Errorf("bad filename glob: %w", err)
	}

	var files map[string]bool
	if !in.IncludeIgnored && info.IsDir() {
		files, err = t.WS.FileSet(ctx)
		if err != nil {
			return "", err
		}
	}
	dirs := workspace.FileDirectories(files)
	// An explicitly selected ignored directory is a deliberate search scope.
	// Root-wide discovery still honors ignore rules, including nested ignores.
	if rel, _ := filepath.Rel(t.WS.Root(), root); rel != "." && dirs != nil && !dirs[rel] {
		files, dirs = nil, nil
	}
	skippedSensitive := 0
	searched := 0
	var out strings.Builder
	matches := 0
	outputCapped := false
	walkErr := filepath.Walk(root, func(p string, info os.FileInfo, err error) error {
		if err := ctx.Err(); err != nil {
			return err
		}
		if err != nil {
			return err
		}
		if matches >= grepMaxMatches {
			return filepath.SkipAll
		}
		if info.IsDir() {
			if dirs != nil {
				rel, _ := filepath.Rel(t.WS.Root(), p)
				if !dirs[rel] {
					return filepath.SkipDir
				}
			}
			if p != root && !in.IncludeIgnored && (info.Name() == ".git" || info.Name() == ".gektor" || info.Name() == ".tars" || (files == nil && workspace.IgnoredDirectory(info.Name()))) {
				return filepath.SkipDir
			}
			return nil
		}
		if in.Glob != "" {
			if ok, _ := filepath.Match(in.Glob, info.Name()); !ok {
				return nil
			}
		}
		relPath, _ := filepath.Rel(t.WS.Root(), p)
		if files != nil && !files[relPath] {
			return nil
		}
		resolved, err := t.WS.Resolve(relPath)
		if err != nil {
			return err
		}
		if !info.Mode().IsRegular() {
			return nil
		}
		if permission.SensitivePath(p) {
			skippedSensitive++
			return nil
		}
		f, err := os.Open(resolved)
		if err != nil {
			return err
		}
		defer f.Close()

		if binary, err := looksBinary(f); err != nil {
			return err
		} else if binary {
			return nil
		}
		searched++

		rel, _ := filepath.Rel(t.WS.Root(), p)
		scanner := bufio.NewScanner(f)
		scanner.Buffer(make([]byte, 64*1024), 1024*1024)
		lineNum := 0
		type contextLine struct {
			number int
			text   string
		}
		before := make([]contextLine, 0, in.Before)
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
			if (in.Before > 0 || in.After > 0) && out.Len() > 0 && (lastPrinted == 0 || n > lastPrinted+1) {
				row = "--\n" + row
			}
			if out.Len()+len(row) > readMaxBytes {
				outputCapped = true
				return false
			}
			out.WriteString(row)
			lastPrinted = n
			return true
		}
		for scanner.Scan() {
			if err := ctx.Err(); err != nil {
				return err
			}
			lineNum++
			line := scanner.Text()
			match := re.MatchString(line)
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
				matches++
				afterUntil = lineNum + in.After
				if matches >= grepMaxMatches {
					break
				}
			} else if lineNum <= afterUntil {
				if !emit(lineNum, line, false) {
					return filepath.SkipAll
				}
			}
			if in.Before > 0 {
				if len(before) == in.Before {
					copy(before, before[1:])
					before[len(before)-1] = contextLine{lineNum, line}
				} else {
					before = append(before, contextLine{lineNum, line})
				}
			}
		}
		return scanner.Err()
	})
	if walkErr != nil {
		return out.String(), fmt.Errorf("search incomplete: %w", walkErr)
	}
	if skippedSensitive > 0 {
		fmt.Fprintf(&out, "(skipped %d sensitive files; use read_file to request permission)\n", skippedSensitive)
	}
	if matches == 0 && skippedSensitive == 0 && !outputCapped {
		if searched == 0 {
			return "no searchable text files (check path/glob; use include_ignored=true for ignored files)", nil
		}
		return "no matches", nil
	}
	if matches >= grepMaxMatches {
		out.WriteString("... (capped at 200 matches, narrow your pattern or path)\n")
	}
	if outputCapped {
		out.WriteString("... (output capped at 48 KiB; narrow your pattern or path)\n")
	}
	return out.String(), nil
}
