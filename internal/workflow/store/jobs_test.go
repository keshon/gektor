package store

import (
	"context"
	"encoding/json"
	"errors"
	"strings"
	"sync"
	"testing"
	"time"
)

func TestPointJobUpdatesAndIndexedClaimsPreserveSnapshotsAndNanosecondOrder(t *testing.T) {
	db, _, _ := testStore(t)
	source := json.RawMessage(`{"id":9007199254740993}`)
	first, err := db.Enqueue(t.Context(), "first.json", source, nil)
	if err != nil {
		t.Fatal(err)
	}
	second, err := db.Enqueue(t.Context(), "second.json", source, nil)
	if err != nil {
		t.Fatal(err)
	}
	base := time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC)
	err = db.UpdateJob(t.Context(), first.ID, func(job *Job) error { job.Created = base.Add(200 * time.Nanosecond); return nil })
	if err != nil {
		t.Fatal(err)
	}
	err = db.UpdateJob(t.Context(), second.ID, func(job *Job) error { job.Created = base.Add(100 * time.Nanosecond); return nil })
	if err != nil {
		t.Fatal(err)
	}
	stop := errors.New("keep this update private")
	err = db.UpdateJob(t.Context(), second.ID, func(job *Job) error { job.Status = "failed"; return stop })
	if !errors.Is(err, stop) {
		t.Fatal(err)
	}
	job, err := db.Job(t.Context(), second.ID)
	if err != nil || job.Status != "queued" || string(job.Source) != string(source) {
		t.Fatal("point update lost rollback or snapshot", job, err)
	}
	claimed, ok, err := db.Claim(t.Context())
	if err != nil || !ok || claimed.ID != second.ID || string(claimed.Source) != string(source) {
		t.Fatal("claim lost sub-millisecond ordering or exact snapshot", claimed, ok, err)
	}
	_, err = db.Job(t.Context(), "missing")
	if err == nil {
		t.Fatal("missing job returned a zero record")
	}
	rows, err := db.db.QueryContext(t.Context(), `EXPLAIN QUERY PLAN SELECT id, json_extract(CAST(record AS TEXT), '$.created')
		FROM jobs WHERE json_extract(CAST(record AS TEXT), '$.status') = 'queued'`)
	if err != nil {
		t.Fatal(err)
	}
	defer rows.Close()
	indexed := false
	for rows.Next() {
		var id, parent, unused int
		var detail string
		err = rows.Scan(&id, &parent, &unused, &detail)
		if err != nil {
			t.Fatal(err)
		}
		indexed = indexed || strings.Contains(detail, "jobs_queue")
	}
	err = rows.Err()
	if err != nil || !indexed {
		t.Fatal("claims do not use the queue metadata index", indexed, err)
	}
}

func TestQueueClaimRecoveryAndCancellation(t *testing.T) {
	s, base, work := testStore(t)
	ctx := t.Context()
	j, err := s.Enqueue(ctx, "a.json", json.RawMessage(`{}`), nil)
	if err != nil {
		t.Fatal(err)
	}
	other, err := OpenStore(base, work)
	if err != nil {
		t.Fatal(err)
	}
	defer other.Close()
	var wg sync.WaitGroup
	ids := make(chan string, 2)
	for _, db := range []*Store{s, other} {
		wg.Go(func() {
			got, ok, e := db.Claim(ctx)
			if e != nil {
				t.Error(e)
			}
			if ok {
				ids <- got.ID
			}
		})
	}
	wg.Wait()
	close(ids)
	count := 0
	for id := range ids {
		count++
		if id != j.ID {
			t.Fatal(id)
		}
	}
	if count != 1 {
		t.Fatalf("claimed %d times", count)
	}
	lease, err := s.DaemonLease()
	if err != nil {
		t.Fatal(err)
	}
	if second, e := other.DaemonLease(); e == nil {
		second.Close()
		t.Fatal("duplicate service lease")
	}
	lease.Close()
	if err := s.RecoverJobs(ctx); err != nil {
		t.Fatal(err)
	}
	jobs, _ := s.Jobs(ctx)
	if jobs[0].Status != "interrupted" {
		t.Fatal(jobs)
	}
	j, err = s.Enqueue(ctx, "b.json", json.RawMessage(`{}`), nil)
	if err != nil {
		t.Fatal(err)
	}
	if err = s.CancelJob(ctx, j.ID); err != nil {
		t.Fatal(err)
	}
	if _, ok, err := s.Claim(ctx); err != nil || ok {
		t.Fatal(ok, err)
	}
}
func TestScheduleAtomicTickOverlapAndMissed(t *testing.T) {
	s, _, _ := testStore(t)
	ctx := t.Context()
	v := Schedule{ID: "hourly", RecipePath: "a.json", Source: json.RawMessage(`{}`), Cron: "0 * * * *", Timezone: "Europe/Moscow", Missed: "once", Overlap: "skip"}
	if err := s.PutSchedule(ctx, v); err != nil {
		t.Fatal(err)
	}
	schedules, _ := s.Schedules(ctx)
	due := schedules[0].Next
	for i := 0; i < 2; i++ {
		if err := s.Tick(ctx, due); err != nil {
			t.Fatal(err)
		}
	}
	jobs, _ := s.Jobs(ctx)
	if len(jobs) != 1 {
		t.Fatal(jobs)
	}
	if err := s.Tick(ctx, due.Add(time.Hour)); err != nil {
		t.Fatal(err)
	}
	jobs, _ = s.Jobs(ctx)
	if len(jobs) != 1 {
		t.Fatal("overlap", jobs)
	}
	if err := s.CancelJob(ctx, jobs[0].ID); err != nil {
		t.Fatal(err)
	}
	if err := s.Tick(ctx, due.Add(8*time.Hour)); err != nil {
		t.Fatal(err)
	}
	jobs, _ = s.Jobs(ctx)
	if len(jobs) != 2 {
		t.Fatal("catch-up storm", jobs)
	}
	if err := s.SetSchedulePaused(ctx, "hourly", true); err != nil {
		t.Fatal(err)
	}
	if err := s.Tick(ctx, due.Add(20*time.Hour)); err != nil {
		t.Fatal(err)
	}
	jobs, _ = s.Jobs(ctx)
	if len(jobs) != 2 {
		t.Fatal(jobs)
	}
}
func TestControlReadAndEmptyClaimDoNotRewrite(t *testing.T) {
	s, _, _ := testStore(t)
	ctx := context.Background()
	j, err := s.Enqueue(ctx, "a", json.RawMessage(`{}`), nil)
	if err != nil {
		t.Fatal(err)
	}
	if err := s.CancelJob(ctx, j.ID); err != nil {
		t.Fatal(err)
	}
	var before, after int64
	err = s.db.QueryRow("SELECT total_changes()").Scan(&before)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := s.Jobs(ctx); err != nil {
		t.Fatal(err)
	}
	if _, _, err := s.Claim(ctx); err != nil {
		t.Fatal(err)
	}
	if err := s.Tick(ctx, time.Now()); err != nil {
		t.Fatal(err)
	}
	err = s.db.QueryRow("SELECT total_changes()").Scan(&after)
	if err != nil {
		t.Fatal(err)
	}
	if before != after {
		t.Fatal("read rewrote durable state")
	}
}
