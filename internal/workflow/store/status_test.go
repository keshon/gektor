package store

import (
	"context"
	"encoding/json"
	"errors"
	"reflect"
	"strings"
	"testing"
	"time"

	"github.com/keshon/gektor/internal/workflow"
)

func TestStatusCountsRetainedMetadataAndReservedArtifactBytes(t *testing.T) {
	s, base, work := testStore(t)
	ctx := t.Context()
	first := time.Date(2026, 11, 19, 12, 0, 0, 100, time.UTC)
	for i, state := range []string{"queued", "queued", "failed", "waiting_review", "waiting"} {
		job, err := s.Enqueue(ctx, "private-recipe.json", json.RawMessage(`{"private":"source"}`), nil)
		if err != nil {
			t.Fatal(err)
		}
		err = s.UpdateJob(ctx, job.ID, func(j *Job) error {
			j.Status = state
			j.Created = first.Add(time.Duration(1-i) * time.Nanosecond)
			return nil
		})
		if err != nil {
			t.Fatal(err)
		}
	}
	for _, paused := range []bool{true, false} {
		id := "active"
		if paused {
			id = "paused"
		}
		err := s.PutSchedule(ctx, Schedule{
			ID: id, RecipePath: "private-recipe.json", Source: json.RawMessage(`{}`),
			Cron: "0 9 * * *", Timezone: "UTC", Paused: paused,
		})
		if err != nil {
			t.Fatal(err)
		}
	}
	writer, err := s.NewRun()
	if err != nil {
		t.Fatal(err)
	}
	defer writer.Close()
	due := time.Now().Add(-time.Minute)
	err = writer.Save(workflow.Run{Status: "waiting", WakeAt: &due})
	if err != nil {
		t.Fatal(err)
	}
	_, err = writer.Artifacts("save").Put(ctx, "private.txt", strings.NewReader("secret"))
	if err != nil {
		t.Fatal(err)
	}
	_, err = writer.reserveArtifact(ctx, "pending")
	if err != nil {
		t.Fatal(err)
	}
	tick := first.In(time.FixedZone("offset", 3*60*60))
	err = s.SaveWorkerTick(ctx, tick)
	if err != nil {
		t.Fatal(err)
	}

	other, err := OpenStore(base, work)
	if err != nil {
		t.Fatal(err)
	}
	defer other.Close()
	status, err := other.Status(ctx)
	if err != nil {
		t.Fatal(err)
	}
	wantJobs := map[string]int64{"queued": 2, "failed": 1, "waiting_review": 1, "waiting": 1}
	if !reflect.DeepEqual(status.Jobs, wantJobs) || status.OldestQueued == nil || !status.OldestQueued.Equal(first) {
		t.Fatal("incorrect queue counts or nanosecond order", status)
	}
	if status.Runs["waiting"] != 1 || status.DueWaits != 1 || status.Schedules != 2 || status.PausedSchedules != 1 {
		t.Fatal("incorrect saved run, deadline or schedule counts", status)
	}
	if status.Artifacts["published"] != (ArtifactUsage{Count: 1, Bytes: 6}) || status.Artifacts["writing"] != (ArtifactUsage{Count: 1, Bytes: MaxArtifactBytes}) {
		t.Fatal("published bytes and reservations were conflated", status.Artifacts)
	}
	if status.WorkerLeaseHeld || status.LastWorkerTick == nil || !status.LastWorkerTick.Equal(tick) {
		t.Fatal("retained tick was treated as worker ownership", status)
	}

	// Invalid snapshot shapes must not affect a metadata-only observation.
	_, err = s.db.Exec(`UPDATE jobs SET record=json_set(CAST(record AS TEXT), '$.source', 'not a recipe object', '$.inputs', 'not an input object')`)
	if err != nil {
		t.Fatal(err)
	}
	status, err = other.Status(ctx)
	if err != nil || !reflect.DeepEqual(status.Jobs, wantJobs) {
		t.Fatal("observation hydrated unrelated snapshots", status, err)
	}

	isolated, err := OpenStore(base, t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	defer isolated.Close()
	empty, err := isolated.Status(ctx)
	if err != nil || len(empty.Jobs) != 0 || empty.LastWorkerTick != nil || len(empty.Artifacts) != 0 {
		t.Fatal("observation crossed workspace boundary", empty, err)
	}
}

func TestStatusSeparatesWorkerLeaseAndTickAndHonorsCancellation(t *testing.T) {
	s, _, _ := testStore(t)
	ctx := t.Context()
	lease, err := s.DaemonLease()
	if err != nil {
		t.Fatal(err)
	}
	defer lease.Close()
	tick := time.Now().Add(-time.Hour)
	err = s.SaveWorkerTick(ctx, tick)
	if err != nil {
		t.Fatal(err)
	}
	err = s.SaveWorkerTick(ctx, time.Time{})
	if err != nil {
		t.Fatal(err)
	}
	status, err := s.Status(ctx)
	if err != nil || !status.WorkerLeaseHeld || status.LastWorkerTick != nil {
		t.Fatal("startup retained another worker's tick", status, err)
	}
	cancelled, cancel := context.WithCancel(ctx)
	cancel()
	_, err = s.Status(cancelled)
	if !errors.Is(err, context.Canceled) {
		t.Fatal("cancelled observation was accepted", err)
	}
}
