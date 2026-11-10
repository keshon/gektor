package service

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/json"
	"fmt"
	"path/filepath"
	"sort"
	"time"

	"github.com/keshon/gektor/internal/workflow/store"
)

type SnapshotInspection struct {
	State            string   `json:"state"` // unchanged, changed, unavailable
	ChangedFiles     []string `json:"changed_files,omitempty"`
	UnavailableFiles []string `json:"unavailable_files,omitempty"`
	Reason           string   `json:"reason,omitempty"`
}

// InspectSnapshot compares only files captured by the frozen dependency bundle.
// A changed parent also covers added/removed calls; no new dependency is read.
// Authorization must be noninteractive. This never refreshes the saved source.
func (s Service) InspectSnapshot(ctx context.Context, saved store.Schedule) SnapshotInspection {
	out := SnapshotInspection{State: "unchanged"}
	root := saved.Source
	var b recipeBundle
	if err := json.Unmarshal(root, &b); err != nil {
		return SnapshotInspection{State: "unavailable", Reason: "Invalid saved snapshot"}
	}
	files := map[string]json.RawMessage{}
	if b.Root != nil {
		root = b.Root
		for path, raw := range b.Recipes {
			files[path] = raw
		}
	}
	files[saved.RecipePath] = root
	paths := make([]string, 0, len(files))
	for path := range files {
		paths = append(paths, path)
	}
	sort.Strings(paths)
	for _, path := range paths {
		current, err := s.Snapshot(ctx, path)
		if err != nil {
			out.UnavailableFiles = append(out.UnavailableFiles, path)
			continue
		}
		before, err1 := canonicalJSON(files[path])
		after, err2 := canonicalJSON(current)
		if err1 != nil || err2 != nil || !bytes.Equal(before, after) {
			out.ChangedFiles = append(out.ChangedFiles, filepath.ToSlash(path))
		}
	}
	if len(out.UnavailableFiles) > 0 {
		out.State = "unavailable"
		out.Reason = "Some snapshot files could not be read with current permissions"
	} else if len(out.ChangedFiles) > 0 {
		out.State = "changed"
	}
	return out
}

func canonicalJSON(raw []byte) ([]byte, error) {
	d := json.NewDecoder(bytes.NewReader(raw))
	d.UseNumber()
	var value any
	if err := d.Decode(&value); err != nil {
		return nil, err
	}
	if !json.Valid(raw) {
		return nil, fmt.Errorf("invalid JSON")
	}
	return json.Marshal(value)
}

type JobInspection struct {
	ID           string     `json:"id"`
	Schedule     string     `json:"schedule_id,omitempty"`
	Recipe       string     `json:"recipe_path"`
	Status       string     `json:"status"`
	RunID        string     `json:"run_id,omitempty"`
	Created      time.Time  `json:"created"`
	ScheduledFor *time.Time `json:"scheduled_for,omitempty"`
	Error        string     `json:"error,omitempty"`
}

func InspectJob(j store.Job) JobInspection {
	return JobInspection{ID: j.ID, Schedule: j.Schedule, Recipe: j.RecipePath, Status: j.Status, RunID: j.RunID, Created: j.Created, ScheduledFor: j.ScheduledFor, Error: j.Error}
}

type ScheduleInspection struct {
	ID             string                     `json:"id"`
	Path           string                     `json:"recipe_path"`
	Cron           string                     `json:"cron"`
	Timezone       string                     `json:"timezone"`
	Paused         bool                       `json:"paused"`
	Next           time.Time                  `json:"next_due"`
	Revision       uint64                     `json:"revision"`
	Inputs         map[string]json.RawMessage `json:"inputs"`
	Overlap        string                     `json:"overlap"`
	Missed         string                     `json:"missed"`
	SnapshotSHA256 string                     `json:"snapshot_sha256"`
	Snapshot       SnapshotInspection         `json:"snapshot"`
	LatestJob      *JobInspection             `json:"latest_job,omitempty"`
	LatestRun      *RunInspection             `json:"latest_run,omitempty"`
	RunError       string                     `json:"run_error,omitempty"`
}

// InspectSchedules shares the agent/TUI read model. Observations are read from
// saved state; a worker lease indicates ownership, never a health guarantee.
func (s Service) InspectSchedules(ctx context.Context, db *store.Store, schedules []store.Schedule, jobs []store.Job) ([]ScheduleInspection, error) {
	out := make([]ScheduleInspection, 0, len(schedules))
	for _, saved := range schedules {
		if err := ctx.Err(); err != nil {
			return nil, err
		}
		v := ScheduleInspection{ID: saved.ID, Path: saved.RecipePath, Cron: saved.Cron, Timezone: saved.Timezone,
			Paused: saved.Paused, Next: saved.Next, Revision: saved.Revision, Inputs: saved.Inputs,
			Overlap: saved.Overlap, Missed: saved.Missed, SnapshotSHA256: fmt.Sprintf("%x", sha256.Sum256(saved.Source)), Snapshot: s.InspectSnapshot(ctx, saved)}
		var latestRunJob *store.Job
		for _, job := range jobs {
			if job.Schedule != saved.ID {
				continue
			}
			if v.LatestJob == nil || job.Created.After(v.LatestJob.Created) || (job.Created.Equal(v.LatestJob.Created) && job.ID > v.LatestJob.ID) {
				j := InspectJob(job)
				v.LatestJob = &j
			}
			if job.RunID != "" && (latestRunJob == nil || job.Created.After(latestRunJob.Created) || (job.Created.Equal(latestRunJob.Created) && job.ID > latestRunJob.ID)) {
				copy := job
				latestRunJob = &copy
			}
		}
		if latestRunJob != nil {
			r, err := db.Run(latestRunJob.RunID)
			if err != nil {
				v.RunError = err.Error()
			} else {
				summary := InspectRun(r)
				v.LatestRun = &summary
			}
		}
		out = append(out, v)
	}
	return out, nil
}
