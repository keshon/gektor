package store

import (
	"encoding/json"
	"testing"
	"time"
)

func TestUpdateScheduleKeepsQueuedSnapshotAndRejectsStaleEdits(t *testing.T) {
	s, _, _ := testStore(t)
	ctx := t.Context()
	v := Schedule{ID: "daily", RecipePath: "recipe.json", Source: json.RawMessage(`{"version":3}`), Cron: "0 9 * * *", Timezone: "Europe/Moscow"}
	if err := s.PutSchedule(ctx, v); err != nil {
		t.Fatal(err)
	}
	all, err := s.Schedules(ctx)
	if err != nil {
		t.Fatal(err)
	}
	before := all[0]
	if err := s.Tick(ctx, before.Next); err != nil {
		t.Fatal(err)
	}
	changed := before
	changed.Cron, changed.Paused = "30 10 * * *", true
	changed.Source = json.RawMessage(`{"version":3,"name":"new"}`)
	// Passing through a UI message compacts embedded JSON; it is still the same snapshot.
	b, _ := json.Marshal(before)
	var expected Schedule
	if err := json.Unmarshal(b, &expected); err != nil {
		t.Fatal(err)
	}
	if err := s.UpdateSchedule(ctx, changed, expected); err != nil {
		t.Fatal(err)
	}
	if err := s.UpdateSchedule(ctx, before, before); err == nil {
		t.Fatal("stale edit overwrote the trigger")
	}
	all, _ = s.Schedules(ctx)
	if !all[0].Paused || all[0].Cron != changed.Cron {
		t.Fatal(all)
	}
	if err := s.Tick(ctx, before.Next.Add(48*time.Hour)); err != nil {
		t.Fatal(err)
	}
	jobs, err := s.Jobs(ctx)
	if err != nil || len(jobs) != 1 || string(jobs[0].Source) != string(before.Source) {
		t.Fatal("updating or pausing changed an already queued job", jobs, err)
	}
	invalid := all[0]
	invalid.Cron = "not cron"
	if err := s.UpdateSchedule(ctx, invalid, all[0]); err == nil {
		t.Fatal("invalid update accepted")
	}
	if err := s.DeleteSchedule(ctx, v.ID); err != nil {
		t.Fatal(err)
	}
	if err := s.UpdateSchedule(ctx, changed, all[0]); err == nil {
		t.Fatal("deleted schedule recreated by a stale form")
	}
}

func TestScheduleReplacementDoesNotReuseRevision(t *testing.T) {
	s, _, _ := testStore(t)
	v := Schedule{ID: "daily", RecipePath: "recipe.json", Source: json.RawMessage(`{}`), Cron: "0 9 * * *", Timezone: "UTC"}
	if err := s.PutSchedule(t.Context(), v); err != nil {
		t.Fatal(err)
	}
	all, _ := s.Schedules(t.Context())
	old := all[0]
	if err := s.DeleteSchedule(t.Context(), v.ID); err != nil {
		t.Fatal(err)
	}
	if err := s.PutSchedule(t.Context(), v); err != nil {
		t.Fatal(err)
	}
	all, _ = s.Schedules(t.Context())
	if all[0].Revision <= old.Revision {
		t.Fatal("replacement reused revision", old, all[0])
	}
	if err := s.UpdateSchedule(t.Context(), v, old); err == nil {
		t.Fatal("stale edit changed replacement")
	}
	if err := s.DeleteScheduleIfUnchanged(t.Context(), old); err == nil {
		t.Fatal("stale delete removed replacement")
	}
}

func TestLegacyScheduleDeletionPreservesGeneration(t *testing.T) {
	s, _, _ := testStore(t)
	v := Schedule{ID: "legacy", Revision: 17, RecipePath: "recipe.json", Source: json.RawMessage(`{}`), Cron: "0 9 * * *", Timezone: "UTC"}
	if err := s.write("control.json", map[string]any{"schedules": map[string]Schedule{v.ID: v}}); err != nil {
		t.Fatal(err)
	}
	if err := s.DeleteSchedule(t.Context(), v.ID); err != nil {
		t.Fatal(err)
	}
	if err := s.PutSchedule(t.Context(), v); err != nil {
		t.Fatal(err)
	}
	all, err := s.Schedules(t.Context())
	if err != nil || len(all) != 1 || all[0].Revision <= v.Revision {
		t.Fatal("legacy generation reused", all, err)
	}
	if err := s.DeleteScheduleIfUnchanged(t.Context(), v); err == nil {
		t.Fatal("stale deletion removed replacement")
	}
}
