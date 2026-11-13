// Package store persists workflow definitions, runs, schedules, and queued jobs.
package store

import (
	"bytes"
	"crypto/rand"
	"crypto/sha256"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"sync"
	"time"

	"github.com/keshon/gektor/internal/workflow"
)

// Store keeps workflow state separate from workspace files. It is not a
// security boundary against programs running with the operator's account.
type Store struct {
	root      *os.Root
	path      string
	workspace string
}

func OpenStore(base, workspace string) (*Store, error) {
	if base == "" {
		return nil, fmt.Errorf("workflow store directory is required")
	}

	canonical, err := filepath.EvalSymlinks(workspace)
	if err != nil {
		return nil, err
	}

	canonical, err = filepath.Abs(canonical)
	if err != nil {
		return nil, err
	}

	if runtime.GOOS == "windows" {
		canonical = strings.ToLower(canonical)
	}

	if err := os.MkdirAll(base, 0700); err != nil {
		return nil, err
	}

	resolved, err := filepath.EvalSymlinks(base)
	if err != nil {
		return nil, err
	}

	resolved, err = filepath.Abs(resolved)
	if err != nil {
		return nil, err
	}

	comparison := resolved
	if runtime.GOOS == "windows" {
		comparison = strings.ToLower(comparison)
	}

	if rel, err := filepath.Rel(canonical, comparison); err == nil && (rel == "." || (rel != ".." && !strings.HasPrefix(rel, ".."+string(filepath.Separator)))) {
		return nil, fmt.Errorf("workflow store must be outside the workspace")
	}

	key := fmt.Sprintf("%x", sha256.Sum256([]byte(canonical)))
	baseRoot, err := os.OpenRoot(resolved)
	if err != nil {
		return nil, err
	}

	defer baseRoot.Close()

	if err := baseRoot.MkdirAll(key, 0700); err != nil {
		return nil, err
	}

	root, err := baseRoot.OpenRoot(key)
	if err != nil {
		return nil, err
	}

	return &Store{
		root:      root,
		path:      filepath.Join(resolved, key),
		workspace: canonical,
	}, nil
}

func (s *Store) Close() error { return s.root.Close() }

// RunWriter holds a process lease until Close. A process exit releases it;
// the persistent lock file itself is never removed.
type RunWriter struct {
	artifactMu sync.Mutex // serializes quota checks and artifact publication
	store      *Store
	id         string
	lease      *os.File
}

func (s *Store) NewRun() (*RunWriter, error) {
	id := rand.Text()
	dir := filepath.Join("runs", id)
	if err := s.root.MkdirAll("runs", 0700); err != nil {
		return nil, err
	}

	if err := s.root.Mkdir(dir, 0700); err != nil {
		return nil, err
	}

	lease, err := s.lock(filepath.Join(dir, "lock"))
	if err != nil {
		return nil, err
	}

	w := &RunWriter{
		store: s,
		id:    id,
		lease: lease,
	}
	if err := w.Save(workflow.Run{Status: "running", Started: time.Now().UTC()}); err != nil {
		w.Close()
		return nil, err
	}

	return w, nil
}

func (w *RunWriter) ID() string   { return w.id }
func (w *RunWriter) Path() string { return filepath.Join(w.store.path, "runs", w.id, "run.json") }
func (w *RunWriter) Close() error { return w.lease.Close() }
func (w *RunWriter) Save(r workflow.Run) error {
	if _, err := w.lease.Stat(); err != nil {
		return fmt.Errorf("run writer closed: %w", err)
	}

	r.RunID, r.Workspace = w.id, w.store.workspace
	return w.store.write(filepath.Join("runs", w.id, "run.json"), r)
}

type RunState struct {
	Status string       `json:"status"`
	Record workflow.Run `json:"record"`
}

func (s *Store) Run(id string) (RunState, error) {
	if !validKey(id) {
		return RunState{}, fmt.Errorf("invalid run ID")
	}

	dir := filepath.Join("runs", id)
	lease, err := s.lock(filepath.Join(dir, "lock"))
	if err != nil && !errors.Is(err, errLocked) {
		if os.IsNotExist(err) {
			return RunState{}, missingRun(id)
		}

		return RunState{}, err
	}

	active := errors.Is(err, errLocked)
	if lease != nil {
		defer lease.Close()
	}

	var r workflow.Run
	if err := s.read(filepath.Join(dir, "run.json"), 128<<20, &r); os.IsNotExist(err) {
		// The process may have died between reserving its directory and its
		// first snapshot. Keep that run visible without inventing evidence.
		status := "interrupted"
		if active {
			status = "running"
		}

		return RunState{Status: status, Record: workflow.Run{RunID: id, Workspace: s.workspace}}, nil
	} else if err != nil {
		return RunState{}, err
	}

	if r.RunID != id || r.Workspace != s.workspace {
		return RunState{}, fmt.Errorf("run identity mismatch")
	}

	status := r.Status
	if !active && status == "running" {
		status = "interrupted"
	}

	return RunState{Status: status, Record: r}, nil
}

type RunSummary struct {
	RecipePath string    `json:"recipe_path,omitempty"`
	Started    time.Time `json:"started"`
	ID         string    `json:"id"`
	Name       string    `json:"name"`
	Status     string    `json:"status"`
	Error      string    `json:"error,omitempty"`
}

func (s *Store) Runs() ([]RunSummary, error) {
	f, err := s.root.Open("runs")
	if os.IsNotExist(err) {
		return []RunSummary{}, nil
	}

	if err != nil {
		return nil, err
	}

	defer f.Close()

	entries, err := f.ReadDir(-1)
	if err != nil {
		return nil, err
	}

	out := make([]RunSummary, 0, len(entries))

	for _, e := range entries {
		if !e.IsDir() {
			continue
		}

		r, err := s.Run(e.Name())
		if err != nil {
			out = append(out, RunSummary{
				ID:     e.Name(),
				Status: "unreadable",
				Error:  err.Error(),
			})
			continue
		}

		out = append(out, RunSummary{
			ID:         e.Name(),
			Name:       r.Record.Definition.Name,
			Status:     r.Status,
			RecipePath: r.Record.RecipePath,
			Started:    r.Record.Started,
		})
	}

	return out, nil
}

func validKey(key string) bool {
	if len(key) == 0 || len(key) > 64 {
		return false
	}

	for _, c := range key {
		if !(c >= 'a' && c <= 'z' || c >= 'A' && c <= 'Z' || c >= '0' && c <= '9' || c == '-' || c == '_') {
			return false
		}
	}

	// Windows device names also apply to file names with extensions.
	upper := strings.ToUpper(key)

	return upper != "CON" && upper != "PRN" && upper != "AUX" && upper != "NUL" && !(len(upper) == 4 && (strings.HasPrefix(upper, "COM") || strings.HasPrefix(upper, "LPT")) && upper[3] >= '0' && upper[3] <= '9')
}

func (s *Store) read(path string, limit int64, out any) error {
	f, err := s.root.Open(path)
	if err != nil {
		return err
	}

	defer f.Close()

	b, err := io.ReadAll(io.LimitReader(f, limit+1))
	if err != nil {
		return err
	}

	if int64(len(b)) > limit {
		return fmt.Errorf("stored JSON exceeds %d bytes", limit)
	}

	return json.Unmarshal(b, out)
}

func (s *Store) write(path string, value any) (err error) {
	var buffer bytes.Buffer
	encoder := json.NewEncoder(&buffer)
	encoder.SetEscapeHTML(false)
	err = encoder.Encode(value)
	if err != nil {
		return err
	}

	data := bytes.TrimSuffix(buffer.Bytes(), []byte{'\n'})
	tmp := filepath.Join(filepath.Dir(path), ".tmp-"+rand.Text())
	f, err := s.root.OpenFile(tmp, os.O_CREATE|os.O_EXCL|os.O_WRONLY, 0600)
	if err != nil {
		return err
	}

	defer func() {
		if e := s.root.Remove(tmp); e != nil && !os.IsNotExist(e) {
			err = errors.Join(err, e)
		}
	}()

	_, writeErr := f.Write(data)
	if writeErr == nil {
		writeErr = f.Sync()
	}

	if err := errors.Join(writeErr, f.Close()); err != nil {
		return err
	}

	return s.root.Rename(tmp, path)
}

var errLocked = errors.New("workflow state is in use")

func missingRun(id string) error {
	return fmt.Errorf("run %q not found in this workspace; use workflow runs with the same -workspace to find its run ID (a step name is not a run ID)", id)
}

func (s *Store) lock(path string) (*os.File, error) {
	f, err := s.root.OpenFile(path, os.O_CREATE|os.O_RDWR, 0600)
	if err != nil {
		return nil, err
	}

	if err := lockFile(f); err != nil {
		f.Close()
		return nil, err
	}

	return f, nil
}

// EditRun holds the same lease as execution while an operator records a review.
func (s *Store) EditRun(id string) (*RunWriter, workflow.Run, error) {
	if !validKey(id) {
		return nil, workflow.Run{}, fmt.Errorf("invalid run ID")
	}

	lease, err := s.lock(filepath.Join("runs", id, "lock"))
	if err != nil {
		if os.IsNotExist(err) {
			return nil, workflow.Run{}, missingRun(id)
		}

		return nil, workflow.Run{}, err
	}

	var r workflow.Run
	if err := s.read(filepath.Join("runs", id, "run.json"), 128<<20, &r); err != nil {
		lease.Close()
		return nil, r, err
	}

	if r.RunID != id || r.Workspace != s.workspace {
		lease.Close()
		return nil, r, fmt.Errorf("run identity mismatch")
	}

	return &RunWriter{
		store: s,
		id:    id,
		lease: lease,
	}, r, nil
}
