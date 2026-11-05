package run

import (
	"encoding/json"
	"os"
	"path/filepath"
	"sync"
	"time"

	"github.com/keshon/gektor/internal/events"
)

// Diagnostics stores a bounded metadata-only timeline beside session state.
// Never persist event text, tool arguments, outputs, URLs or backend error bodies.
func (s *Session) diagnostics() func() {
	if s.cfg.StateFile == "" {
		return func() {}
	}
	path := filepath.Join(filepath.Dir(s.cfg.StateFile), "diagnostics.jsonl")
	_ = os.MkdirAll(filepath.Dir(path), 0700)
	file, err := os.OpenFile(path, os.O_CREATE|os.O_APPEND|os.O_WRONLY, 0600)
	previous := s.cfg.OnEvent
	if err != nil {
		if previous != nil {
			previous(events.Event{Name: "local_warning", Fields: map[string]any{"text": "Session diagnostics could not be opened"}})
		}
		return func() {}
	}
	var mu sync.Mutex
	var size int64
	if info, err := file.Stat(); err == nil {
		size = info.Size()
	}
	lastProgress := ""
	disabled := false
	disable := func() {
		disabled = true
		if previous != nil {
			previous(events.Event{Name: "local_warning", Fields: map[string]any{"text": "Session diagnostics stopped: log could not be written"}})
		}
	}
	started := time.Now()
	runID := started.UTC().Format("20060102T150405.000000000Z")
	write := func(kind string, fields map[string]any) {
		mu.Lock()
		defer mu.Unlock()
		if disabled {
			return
		}
		if kind == "progress" {
			signature, _ := json.Marshal(fields)
			if string(signature) == lastProgress {
				return
			}
			lastProgress = string(signature)
		}
		record := map[string]any{"time": time.Now().UTC().Format(time.RFC3339Nano), "elapsed_ms": time.Since(started).Milliseconds(), "run": runID, "event": kind}
		for k, v := range fields {
			record[k] = v
		}
		data, err := json.Marshal(record)
		if err != nil {
			return
		}
		if size+int64(len(data)+1) > 2<<20 {
			_ = file.Close()
			_ = os.Remove(path + ".1")
			if err := os.Rename(path, path+".1"); err != nil {
				disable()
				return
			}
			file, err = os.OpenFile(path, os.O_CREATE|os.O_APPEND|os.O_WRONLY, 0600)
			if err != nil {
				disable()
				return
			}
			size = 0
		}
		if file != nil {
			n, err := file.Write(append(data, '\n'))
			if err != nil {
				disable()
			}
			size += int64(n)
		}
	}
	write("run_start", nil)
	s.cfg.OnEvent = func(ev events.Event) {
		if fields := diagnosticFields(ev); fields != nil {
			write(ev.Name, fields)
		}
		if previous != nil {
			previous(ev)
		}
	}
	return func() {
		write("run_end", nil)
		s.cfg.OnEvent = previous
		if file != nil {
			_ = file.Close()
		}
	}
}

func diagnosticFields(ev events.Event) map[string]any {
	fields := map[string]any{}
	var keys []string
	switch ev.Name {
	case "progress":
		keys = []string{"phase", "subphase", "request", "attempt", "request_id", "http_status", "duration_ms", "outcome"}
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
