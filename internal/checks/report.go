package checks

import (
	"fmt"
	"io"
	"os"

	"github.com/keshon/tars/internal/workspace"
)

// Scope names the pass a finding came from.
type Scope string

const (
	// PerEdit is a scan of one file right after a tool wrote it.
	PerEdit Scope = "per-edit"
	// SessionEnd is the deep pass over every file the run changed.
	SessionEnd Scope = "session-end"
)

// Sweep bounds. Both overflow as a named marker finding rather than a
// silent drop, so the reader learns the pass was cut short.
const (
	sweepMaxFiles    = 64
	sweepMaxFindings = 20
	// sweepReadMax bounds one swept file. The bound on file count alone
	// still let a run that touched one large asset read all of it.
	sweepReadMax = 256 * 1024
)

// Reporter collects deterministic findings across a run. Post-write
// scans and the session-end sweep share one cache, so a file dirty
// throughout a run is reported once rather than once per pass that
// noticed it.
//
// One per run. A nil *Reporter is inert, so a frontend with no sink
// needs no special case: it simply never scans.
type Reporter struct {
	cache cache
	sink  func(Scope, Finding)
}

// NewReporter returns a Reporter delivering to sink. A nil sink keeps
// the dedupe cache but emits nowhere.
func NewReporter(sink func(Scope, Finding)) *Reporter {
	return &Reporter{sink: sink}
}

// PerEdit receives one post-write finding. It doubles as the sink the
// tools package scans into, which is why it carries the primitive
// signature rather than a Finding.
func (r *Reporter) PerEdit(rule, path string, line int, summary string) {
	if r == nil {
		return
	}
	r.one(PerEdit, rule, path, line, summary)
}

// Sweep re-scans every file the run changed. Post-write scans only see
// content a tool addressed by "path"; this catches shell redirections,
// MCP writes and background jobs, which no tool ever reports.
func (r *Reporter) Sweep(ws *workspace.Workspace, paths []string) {
	if r == nil || ws == nil || len(paths) == 0 {
		return
	}
	if len(paths) > sweepMaxFiles {
		r.one(SessionEnd, "sweep-skipped", "", 0,
			fmt.Sprintf("%d files changed; sweep bound at %d", len(paths), sweepMaxFiles))
		return
	}
	var all []Finding
	for _, p := range paths {
		full, err := ws.Resolve(p)
		if err != nil {
			continue // deleted or escaped since: nothing to scan
		}
		data, err := readCapped(full)
		if err != nil {
			continue
		}
		all = append(all, ScanFile(p, data)...)
	}
	kept, dropped := Clamp(r.cache.filter(all), sweepMaxFindings)
	r.emit(SessionEnd, kept...)
	if dropped > 0 {
		r.one(SessionEnd, "sweep-truncated", "", 0,
			fmt.Sprintf("and %d more findings withheld", dropped))
	}
}

// one emits a single finding unless the cache has already seen it.
func (r *Reporter) one(scope Scope, rule, path string, line int, summary string) {
	r.emit(scope, r.cache.filter([]Finding{{Rule: rule, Path: path, Line: line, Summary: summary}})...)
}

func (r *Reporter) emit(scope Scope, kept ...Finding) {
	for _, f := range kept {
		if r.sink != nil {
			r.sink(scope, f)
		}
	}
}

func readCapped(path string) ([]byte, error) {
	f, err := os.Open(path)
	if err != nil {
		return nil, err
	}
	defer f.Close()
	return io.ReadAll(io.LimitReader(f, sweepReadMax))
}