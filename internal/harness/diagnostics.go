package harness

import (
	"encoding/json"
	"os"
	"path/filepath"
	"sync"
	"time"

	"github.com/keshon/gektor/internal/events"
)

// diagnosticLog owns the metadata log for one run and forwards original events.
// The mutex guards the file, size, progress signature, and disabled state.
// Session closes it after execution has stopped publishing events.
type diagnosticLog struct {
	mu           sync.Mutex
	file         *os.File
	path         string
	size         int64
	lastProgress string
	disabled     bool
	started      time.Time
	runID        string
	session      *Session
	next         events.Sink
}

func (s *Session) diagnostics() *diagnosticLog {
	if s.cfg.StateFile == "" {
		return nil
	}

	path := filepath.Join(filepath.Dir(s.cfg.StateFile), "diagnostics.jsonl")
	err := os.MkdirAll(filepath.Dir(path), 0700)
	if err != nil {
		diagnosticWarning(s.cfg.Events, "Session diagnostics could not be opened")
		return nil
	}

	file, err := os.OpenFile(path, os.O_CREATE|os.O_APPEND|os.O_WRONLY, 0600)
	if err != nil {
		diagnosticWarning(s.cfg.Events, "Session diagnostics could not be opened")
		return nil
	}

	started := time.Now()
	log := &diagnosticLog{
		file:    file,
		path:    path,
		started: started,
		runID:   started.UTC().Format("20060102T150405.000000000Z"),
		session: s,
		next:    s.cfg.Events,
	}
	info, err := file.Stat()
	if err == nil {
		log.size = info.Size()
	}
	// A failed stat leaves size unknown; writes still report their own failures.

	log.write("run_start", nil)
	s.cfg.Events = log

	return log
}

func diagnosticWarning(sink events.Sink, text string) {
	if sink == nil {
		return
	}

	event := events.Event{Name: "local_warning", Fields: map[string]any{"text": text}}
	sink.Handle(event)
}

func (d *diagnosticLog) disable() {
	d.disabled = true
	diagnosticWarning(d.next, "Session diagnostics stopped: log could not be written")
}

func (d *diagnosticLog) Handle(event events.Event) {
	fields := diagnosticFields(event)
	if fields != nil {
		d.write(event.Name, fields)
	}

	if d.next != nil {
		d.next.Handle(event)
	}
}

func (d *diagnosticLog) write(kind string, fields map[string]any) {
	d.mu.Lock()
	defer d.mu.Unlock()

	if d.disabled {
		return
	}

	if kind == "progress" {
		// Keep phase boundaries and final counts, grouping prefill by 1024 tokens.
		boundary := make(map[string]any, len(fields))
		for key, value := range fields {
			if key != "completion" && key != "processed" {
				boundary[key] = value
			}
		}
		if fields["phase"] == "prefill" {
			processed, ok := fields["processed"].(int)
			if ok {
				boundary["processed_bucket"] = processed / 1024
			}
		}

		signature, err := json.Marshal(boundary)
		if err != nil {
			return
		}
		if string(signature) == d.lastProgress {
			return
		}
		d.lastProgress = string(signature)
	}

	record := map[string]any{
		"time":       time.Now().UTC().Format(time.RFC3339Nano),
		"elapsed_ms": time.Since(d.started).Milliseconds(),
		"run":        d.runID,
		"event":      kind,
	}
	for key, value := range fields {
		record[key] = value
	}
	data, err := json.Marshal(record)
	if err != nil {
		return
	}

	if d.size+int64(len(data)+1) > 2<<20 {
		// Rotation is best effort. Rename/open failures disable diagnostics only.
		_ = d.file.Close()
		_ = os.Remove(d.path + ".1")
		err = os.Rename(d.path, d.path+".1")
		if err != nil {
			d.disable()
			return
		}
		d.file, err = os.OpenFile(d.path, os.O_CREATE|os.O_APPEND|os.O_WRONLY, 0600)
		if err != nil {
			d.disable()
			return
		}
		d.size = 0
	}

	n, err := d.file.Write(append(data, '\n'))
	if err != nil {
		d.disable()
	}
	d.size += int64(n)
}

// Close restores the frontend sink and ends this run's best-effort log.
func (d *diagnosticLog) Close() {
	if d == nil {
		return
	}

	d.write("run_end", nil)
	d.session.cfg.Events = d.next

	d.mu.Lock()
	defer d.mu.Unlock()
	// Diagnostics must never replace the measured execution outcome.
	if d.file != nil {
		_ = d.file.Close()
	}
}

func diagnosticFields(ev events.Event) map[string]any {
	fields := map[string]any{}
	var keys []string
	switch ev.Name {
	case "progress":
		keys = []string{"phase", "subphase", "request", "attempt", "request_id", "http_status", "duration_ms", "outcome", "prompt", "completion", "cached", "cache_known", "processed", "estimated"}
	case "usage":
		keys = []string{"step", "prompt", "completion", "cached", "estimated"}
	case "tool_result":
		keys = []string{"call_id", "failed"}
	case "outcome":
		keys = []string{"outcome"}
	case "step":
		keys = []string{"step"}
		if calls, ok := ev.Fields["tool_calls"].([]any); ok {
			safe := []map[string]any{}
			for _, value := range calls {
				if call, ok := value.(map[string]any); ok {
					safe = append(safe, map[string]any{"id": call["id"], "name": call["name"]})
				}
			}
			fields["tool_calls"] = safe
		}
	default:
		return nil
	}
	for _, key := range keys {
		if value, ok := ev.Fields[key]; ok {
			fields[key] = value
		}
	}
	return fields
}
