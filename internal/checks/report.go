package checks

import (
	"fmt"
	"io"
	"path/filepath"
	"strconv"

	"github.com/keshon/gektor/internal/workspace"
)

// Scope identifies the observation pass.
type Scope string

const (
	PerEdit    Scope = "per-edit"
	SessionEnd Scope = "session-end"
)

const (
	sweepMaxFiles   = 64
	scanMaxFindings = 20
	scanMaxBytes    = 256 * 1024
)

// Reporter owns file scanning, bounds and deduplication for one run. It is used
// sequentially by exclusive write tools and the final changed-file sweep.
type Reporter struct {
	cache cache
	rules []Rule
	sink  func(Scope, Finding)
}

func NewReporter(rules []Rule, sink func(Scope, Finding)) *Reporter {
	return &Reporter{rules: rules, sink: sink}
}

// Check inspects a file after a successful edit without changing the tool result.
func (r *Reporter) Check(ws *workspace.Workspace, path string) {
	r.scan(PerEdit, ws, []string{path})
}

// Sweep also covers changes made by commands, external tools and background jobs.
func (r *Reporter) Sweep(ws *workspace.Workspace, paths []string) {
	r.scan(SessionEnd, ws, paths)
}

func (r *Reporter) scan(scope Scope, ws *workspace.Workspace, paths []string) {
	if r == nil || r.sink == nil || len(r.rules) == 0 || ws == nil || len(paths) == 0 {
		return
	}
	if len(paths) > sweepMaxFiles {
		r.emit(scope, Finding{Rule: "sweep-skipped", Summary: fmt.Sprintf("%d files changed; sweep bound at %d", len(paths), sweepMaxFiles)})
		return
	}
	kept, dropped := 0, 0
	for _, path := range paths {
		relative, err := ws.Relative(path)
		if err != nil {
			continue
		}
		path = filepath.ToSlash(relative)
		data, oversized, err := readCapped(ws, path)
		if err != nil {
			// Deleted, unreadable or outside the workspace.
			continue
		}
		var findings []Finding
		if oversized {
			findings = []Finding{{Rule: "scan-skipped", Path: path, Summary: fmt.Sprintf("file exceeds %d bytes; content checks skipped", scanMaxBytes)}}
		} else {
			findings = Scan(r.rules, path, data)
		}
		for _, finding := range findings {
			if r.cache.contains(finding) {
				continue
			}
			if kept >= scanMaxFindings {
				dropped++
				continue
			}
			r.emit(scope, finding)
			kept++
		}
	}
	if dropped > 0 {
		marker := "scan-truncated"
		if scope == SessionEnd {
			marker = "sweep-truncated"
		}
		// A limit notice describes this pass, so it must not disappear because a
		// previous edit also exceeded the limit.
		r.sink(scope, Finding{Rule: marker, Summary: fmt.Sprintf("and %d more findings withheld", dropped)})
	}
}

// Only delivered findings enter the cache; withheld results do not grow it.
func (r *Reporter) emit(scope Scope, finding Finding) {
	if r.cache.contains(finding) {
		return
	}
	if r.cache.seen == nil {
		r.cache.seen = make(map[string]struct{})
	}
	r.cache.seen[findingKey(finding)] = struct{}{}
	r.sink(scope, finding)
}

type cache struct{ seen map[string]struct{} }

func findingKey(f Finding) string        { return f.Rule + "\x00" + f.Path + "\x00" + strconv.Itoa(f.Line) }
func (c *cache) contains(f Finding) bool { _, ok := c.seen[findingKey(f)]; return ok }

func readCapped(ws *workspace.Workspace, path string) ([]byte, bool, error) {
	f, err := ws.Open(path)
	if err != nil {
		return nil, false, err
	}
	defer f.Close()
	data, err := io.ReadAll(io.LimitReader(f, scanMaxBytes+1))
	if len(data) > scanMaxBytes {
		return nil, true, err
	}
	return data, false, err
}
