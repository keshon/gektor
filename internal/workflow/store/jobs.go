package store

import (
	"bytes"
	"context"
	"crypto/rand"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"time"

	"github.com/keshon/gektor/internal/workflow"
)

type Job struct {
	ScheduledFor   *time.Time                 `json:"scheduled_for,omitempty"`
	ID             string                     `json:"id"`
	RecipePath     string                     `json:"recipe_path"`
	Source         json.RawMessage            `json:"source"`
	Inputs         map[string]json.RawMessage `json:"inputs,omitempty"`
	Status         string                     `json:"status"`
	Created        time.Time                  `json:"created"`
	RunID          string                     `json:"run_id,omitempty"`
	Error          string                     `json:"error,omitempty"`
	Cancel         bool                       `json:"cancel_requested,omitempty"`
	Schedule       string                     `json:"schedule,omitempty"`
	SubmissionKey  string                     `json:"submission_key,omitempty"`
	SubmissionHash string                     `json:"submission_hash,omitempty"`
}
type Schedule struct {
	Revision   uint64                     `json:"revision"`
	ID         string                     `json:"id"`
	RecipePath string                     `json:"recipe_path"`
	Source     json.RawMessage            `json:"source"`
	Inputs     map[string]json.RawMessage `json:"inputs,omitempty"`
	Cron       string                     `json:"cron"`
	Timezone   string                     `json:"timezone"`
	Next       time.Time                  `json:"next"`
	Paused     bool                       `json:"paused"`
	Missed     string                     `json:"missed"`
	Overlap    string                     `json:"overlap"`
}
type control struct {
	ScheduleRevision uint64              `json:"schedule_revision,omitempty"`
	Jobs             map[string]Job      `json:"jobs"`
	Schedules        map[string]Schedule `json:"schedules"`
}

func (s *Store) control(ctx context.Context, fn func(*control) error) error {
	var lease *os.File
	for {
		if err := ctx.Err(); err != nil {
			return err
		}
		var err error
		lease, err = s.lock("control.lock")
		if err == nil {
			break
		}
		if !errors.Is(err, errLocked) {
			return err
		}
		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-time.After(25 * time.Millisecond):
		}
	}
	defer lease.Close()
	c := control{Jobs: map[string]Job{}, Schedules: map[string]Schedule{}}
	if err := s.read("control.json", 256<<20, &c); err != nil && !os.IsNotExist(err) {
		return err
	}
	if c.Jobs == nil {
		c.Jobs = map[string]Job{}
	}
	if c.Schedules == nil {
		c.Schedules = map[string]Schedule{}
	}
	before, err := json.Marshal(c)
	if err != nil {
		return err
	}
	if err := fn(&c); err != nil {
		return err
	}
	after, err := json.Marshal(c)
	if err != nil {
		return err
	}
	if bytes.Equal(before, after) {
		return nil
	}
	return s.write("control.json", c)
}
func (s *Store) Enqueue(ctx context.Context, path string, source json.RawMessage, inputs map[string]json.RawMessage) (Job, error) {
	return s.EnqueueOnce(ctx, path, source, inputs, "", "")
}

var ErrSubmissionConflict = errors.New("idempotency key already used for a different submission")

// EnqueueOnce deduplicates retained jobs atomically. Keys are opaque hashes supplied
// by ingress; eviction of terminal jobs also ends their deduplication window.
func (s *Store) EnqueueOnce(ctx context.Context, path string, source json.RawMessage, inputs map[string]json.RawMessage, key, hash string) (Job, error) {
	j := Job{ID: rand.Text(), RecipePath: path, Source: source, Inputs: inputs, Status: "queued", Created: time.Now().UTC()}
	if len(key) > 128 || len(hash) > 128 || (key == "") != (hash == "") {
		return Job{}, fmt.Errorf("invalid submission identity")
	}
	j.SubmissionKey, j.SubmissionHash = key, hash
	err := s.control(ctx, func(c *control) error {
		if key != "" {
			for _, previous := range c.Jobs {
				if previous.SubmissionKey == key {
					if previous.SubmissionHash != hash {
						return ErrSubmissionConflict
					}
					j = previous
					return nil
				}
			}
		}
		if err := makeJobRoom(c); err != nil {
			return err
		}
		if err := validateSnapshot(source, inputs); err != nil {
			return err
		}
		c.Jobs[j.ID] = j
		return nil
	})
	return j, err
}
func (s *Store) Jobs(ctx context.Context) ([]Job, error) {
	var jobs []Job
	err := s.control(ctx, func(c *control) error {
		for _, j := range c.Jobs {
			jobs = append(jobs, j)
		}
		return nil
	})
	sort.Slice(jobs, func(i, j int) bool { return jobs[i].Created.Before(jobs[j].Created) })
	return jobs, err
}
func (s *Store) UpdateJob(ctx context.Context, id string, fn func(*Job) error) error {
	return s.control(ctx, func(c *control) error {
		j, ok := c.Jobs[id]
		if !ok {
			return fmt.Errorf("unknown job %s", id)
		}
		if err := fn(&j); err != nil {
			return err
		}
		c.Jobs[id] = j
		return nil
	})
}
func (s *Store) Claim(ctx context.Context) (Job, bool, error) {
	var chosen Job
	err := s.control(ctx, func(c *control) error {
		for _, j := range c.Jobs {
			if j.Status == "queued" && (chosen.ID == "" || j.Created.Before(chosen.Created)) {
				chosen = j
			}
		}
		if chosen.ID != "" {
			chosen.Status = "running"
			c.Jobs[chosen.ID] = chosen
		}
		return nil
	})
	return chosen, chosen.ID != "", err
}
func (s *Store) CancelJob(ctx context.Context, id string) error {
	return s.UpdateJob(ctx, id, func(j *Job) error {
		if j.Status == "waiting_review" {
			return fmt.Errorf("job awaits operator review; reject its pending run step")
		}
		if j.Status == "queued" {
			j.Status = "cancelled"
		}
		if j.Status == "running" {
			j.Cancel = true
		}
		return nil
	})
}

// RecoverJobs reconciles stale queue state against the durable run, without
// interrupting a continuation owned by another live CLI process.
func (s *Store) RecoverJobs(ctx context.Context) error { return s.recoverJobs(ctx, true) }

// ReconcileRuns repairs abandoned CLI continuations while the daemon stays alive.
// Unlinked claims belong to the active worker and are only recovered at startup.
func (s *Store) ReconcileRuns(ctx context.Context) error { return s.recoverJobs(ctx, false) }
func (s *Store) recoverJobs(ctx context.Context, includeUnlinked bool) error {
	return s.control(ctx, func(c *control) error {
		for id, j := range c.Jobs {
			if j.Status != "running" && j.Status != "waiting_review" {
				continue
			}
			if j.RunID != "" {
				if !validKey(j.RunID) {
					return fmt.Errorf("invalid linked run ID")
				}
				lease, err := s.lock(filepath.Join("runs", j.RunID, "lock"))
				if errors.Is(err, errLocked) {
					continue
				}
				if err != nil {
					return err
				}
				var run workflow.Run
				err = s.read(filepath.Join("runs", j.RunID, "run.json"), 128<<20, &run)
				lease.Close()
				if err != nil {
					return err
				}
				if run.RunID != j.RunID || run.Workspace != s.workspace {
					return fmt.Errorf("run identity mismatch")
				}
				switch run.Status {
				case "completed", "failed", "cancelled", "waiting_review":
					j.Status = run.Status
				default:
					j.Status = "interrupted"
				}
			} else {
				if !includeUnlinked {
					continue
				}
				j.Status = "interrupted"
			}
			if j.Status == "interrupted" {
				j.Error = "execution stopped; inspect effects before submitting again"
			}
			c.Jobs[id] = j
		}
		return nil
	})
}

// DaemonLease prevents two local schedulers from claiming the same workspace.
func (s *Store) DaemonLease() (*os.File, error) { return s.lock("daemon.lock") }

// WorkerLeaseHeld reports ownership, not worker health or successful execution.
func (s *Store) WorkerLeaseHeld() (bool, error) {
	lease, err := s.DaemonLease()
	if errors.Is(err, errLocked) {
		return true, nil
	}
	if err != nil {
		return false, err
	}
	return false, lease.Close()
}

// Retain the newest 1000 jobs; active jobs are never evicted. Run records remain.
func makeJobRoom(c *control) error {
	if len(c.Jobs) < 1000 {
		return nil
	}
	var oldest Job
	for _, j := range c.Jobs {
		if j.Status != "queued" && j.Status != "running" && j.Status != "waiting_review" && (oldest.ID == "" || j.Created.Before(oldest.Created)) {
			oldest = j
		}
	}
	if oldest.ID == "" {
		return fmt.Errorf("queue limit reached (1000 active jobs)")
	}
	delete(c.Jobs, oldest.ID)
	return nil
}
func validateSnapshot(source json.RawMessage, inputs map[string]json.RawMessage) error {
	if len(source) > 64<<10 || !json.Valid(source) {
		return fmt.Errorf("invalid recipe snapshot")
	}
	b, err := json.Marshal(struct {
		Source json.RawMessage
		Inputs map[string]json.RawMessage
	}{source, inputs})
	if err != nil {
		return err
	}
	if len(inputs) > 32 || len(b) > 128<<10 {
		return fmt.Errorf("snapshot exceeds 32 input values or 128 KiB encoded")
	}
	return nil
}
