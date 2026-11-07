package tools

import (
	"context"
	"encoding/json"
	"fmt"
	"io/fs"
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
func (GrepFiles) Description() string {
	return "Search recursively with a Go regex, or exact text with literal=true. Returns path:line:content, at most 200 matches; truncates long lines and skips binaries. Context windows merge. Explicit files and directory roots are searched even if ignored."
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

type grepArguments struct {
	Path           string `json:"path"`
	Pattern        string `json:"pattern"`
	Literal        bool   `json:"literal"`
	Glob           string `json:"glob"`
	IncludeIgnored bool   `json:"include_ignored"`
	Before         int    `json:"before_context"`
	After          int    `json:"after_context"`
}

func (t GrepFiles) Run(ctx context.Context, args json.RawMessage) (string, error) {
	in, re, err := parseGrepArguments(args)
	if err != nil {
		return "", err
	}
	root, err := t.WS.Resolve(in.Path)
	if err != nil {
		return "", err
	}
	info, err := t.WS.Stat(root)
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
	search := grepSearch{pattern: re, before: in.Before, after: in.After}
	searchPath, _ := t.WS.Relative(in.Path)
	walkErr := fs.WalkDir(t.WS.FS(), filepath.ToSlash(searchPath), func(name string, entry fs.DirEntry, err error) error {
		p := filepath.Join(t.WS.Root(), filepath.FromSlash(name))
		var info os.FileInfo
		if err == nil {
			info, err = entry.Info()
		}
		if err := ctx.Err(); err != nil {
			return err
		}
		if err != nil {
			return err
		}
		if search.matches >= grepMaxMatches {
			return filepath.SkipAll
		}
		if info.IsDir() {
			if dirs != nil {
				rel, _ := filepath.Rel(t.WS.Root(), p)
				if !dirs[rel] {
					return filepath.SkipDir
				}
			}
			if p != root && !in.IncludeIgnored && (info.Name() == ".git" || info.Name() == ".gektor" || (files == nil && workspace.IgnoredDirectory(info.Name()))) {
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
			search.skippedSensitive++
			return nil
		}
		f, err := t.WS.Open(resolved)
		if err != nil {
			return err
		}
		defer f.Close()

		return search.scanFile(ctx, f, relPath)
	})
	if walkErr != nil {
		return search.out.String(), fmt.Errorf("search incomplete: %w", walkErr)
	}
	return search.result(), nil
}

func parseGrepArguments(args json.RawMessage) (grepArguments, *regexp.Regexp, error) {
	var in grepArguments
	if err := json.Unmarshal(args, &in); err != nil {
		return in, nil, fmt.Errorf("bad arguments: %w", err)
	}
	if err := rejectUnknownFields(args, "path", "pattern", "literal", "glob", "include_ignored", "before_context", "after_context"); err != nil {
		return in, nil, err
	}
	if in.Before < 0 || in.Before > 10 || in.After < 0 || in.After > 10 {
		return in, nil, fmt.Errorf("before_context and after_context must be between 0 and 10")
	}
	if strings.TrimSpace(in.Path) == "" {
		in.Path = "."
	}
	if strings.TrimSpace(in.Pattern) == "" {
		return in, nil, fmt.Errorf("pattern is required and must be non-empty")
	}
	pattern := in.Pattern
	if in.Literal {
		pattern = regexp.QuoteMeta(pattern)
	}
	re, err := regexp.Compile(pattern)
	if err != nil {
		return in, nil, fmt.Errorf("bad pattern: %w", err)
	}
	return in, re, nil
}
