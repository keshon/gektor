package checks

import (
	"fmt"
	"io"
	"os"
	"strconv"

	"github.com/keshon/gektor/internal/workspace"
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
	rules []Rule
	sink  func(Scope, Finding)
}

// NewReporter returns a Reporter running rules and delivering to sink. A
// nil sink keeps the dedupe cache but emits nowhere; nil rules scan
// nothing, which is the honest state for a frontend with no checks
// configured.
func NewReporter(rules []Rule, sink func(Scope, Finding)) *Reporter {
	return &Reporter{rules: rules, sink: sink}
}

// Rules returns the rules this reporter scans with, so a frontend that
// also scans post-write has one source for the set.
func (r *Reporter) Rules() []Rule {
	if r == nil {
		return nil
	}
	return r.rules
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
		all = append(all, Scan(r.rules, p, data)...)
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

// cache dedupes findings across repeated scans: key is rule + path +
// line, so the same finding reported twice surfaces once. Owned by one
// Reporter, which is what makes the deep pass quiet on findings the
// per-edit scans already reported.
type cache struct {
	seen map[string]struct{}
}

// filter drops already-seen findings, remembering the new ones.
func (c *cache) filter(in []Finding) []Finding {
	if c.seen == nil {
		c.seen = map[string]struct{}{}
	}
	var out []Finding
	for _, f := range in {
		k := f.Rule + "\x00" + f.Path + "\x00" + strconv.Itoa(f.Line)
		if _, ok := c.seen[k]; ok {
			continue
		}
		c.seen[k] = struct{}{}
		out = append(out, f)
	}
	return out
}

// Clamp caps findings for display: at most max, plus the dropped count
// so the caller can say "and N more" instead of silently losing them.
func Clamp(in []Finding, max int) (out []Finding, dropped int) {
	if max < 0 {
		max = 0
	}
	if len(in) <= max {
		return in, 0
	}
	return in[:max], len(in) - max
}

func readCapped(path string) ([]byte, error) {
	f, err := os.Open(path)
	if err != nil {
		return nil, err
	}
	defer f.Close()
	return io.ReadAll(io.LimitReader(f, sweepReadMax))
}
