package store

import (
	"context"
	"crypto/rand"
	"encoding/json"
	"fmt"
	"sort"
	"time"
	_ "time/tzdata"

	"github.com/robfig/cron/v3"
)

func nextSchedule(s Schedule, now time.Time) (time.Time, error) {
	location, err := time.LoadLocation(s.Timezone)
	if err != nil {
		return time.Time{}, err
	}
	parsed, err := cron.ParseStandard(s.Cron)
	if err != nil {
		return time.Time{}, err
	}
	next := parsed.Next(now.In(location))
	if next.IsZero() {
		return time.Time{}, fmt.Errorf("schedule has no next occurrence")
	}
	return next.UTC(), nil
}
func (s *Store) PutSchedule(ctx context.Context, v Schedule) error {
	return s.putSchedule(ctx, v, nil)
}

// UpdateSchedule replaces a trigger atomically. Scheduler ticks may advance Next
// while a form is open; edits by another operator must not be overwritten.
func (s *Store) UpdateSchedule(ctx context.Context, v, expected Schedule) error {
	return s.putSchedule(ctx, v, &expected)
}

func (s *Store) putSchedule(ctx context.Context, v Schedule, expected *Schedule) error {
	if err := validateSnapshot(v.Source, v.Inputs); err != nil {
		return err
	}
	if !validKey(v.ID) {
		return fmt.Errorf("invalid schedule ID")
	}
	if v.Overlap == "" {
		v.Overlap = "skip"
	}
	if v.Missed == "" {
		v.Missed = "skip"
	}
	if v.Overlap != "skip" && v.Overlap != "queue" {
		return fmt.Errorf("overlap must be skip or queue")
	}
	if v.Missed != "skip" && v.Missed != "once" {
		return fmt.Errorf("missed must be skip or once")
	}
	next, err := nextSchedule(v, time.Now())
	if err != nil {
		return err
	}
	v.Next = next
	return s.control(ctx, func(c *control) error {
		old, exists := c.Schedules[v.ID]
		if expected != nil {
			if !exists || !sameSchedule(old, *expected) {
				return fmt.Errorf("schedule changed; refresh before editing")
			}
		} else if exists {
			return fmt.Errorf("schedule exists; remove it before replacing")
		}
		if !exists && len(c.Schedules) >= 100 {
			return fmt.Errorf("schedule limit reached (100)")
		}
		v.Revision = c.nextScheduleRevision()
		c.Schedules[v.ID] = v
		return nil
	})
}
func (s *Store) Schedules(ctx context.Context) ([]Schedule, error) {
	out := []Schedule{}
	err := s.control(ctx, func(c *control) error {
		for _, v := range c.Schedules {
			out = append(out, v)
		}
		return nil
	})
	sort.Slice(out, func(i, j int) bool { return out[i].ID < out[j].ID })
	return out, err
}
func (s *Store) SetSchedulePaused(ctx context.Context, id string, paused bool) error {
	return s.control(ctx, func(c *control) error {
		v, ok := c.Schedules[id]
		if !ok {
			return fmt.Errorf("unknown schedule")
		}
		v.Paused = paused
		v.Revision = c.nextScheduleRevision()
		if !paused {
			var err error
			v.Next, err = nextSchedule(v, time.Now())
			if err != nil {
				return err
			}
		}
		c.Schedules[id] = v
		return nil
	})
}
func (s *Store) DeleteSchedule(ctx context.Context, id string) error {
	return s.deleteSchedule(ctx, id, nil)
}

func (s *Store) DeleteScheduleIfUnchanged(ctx context.Context, expected Schedule) error {
	return s.deleteSchedule(ctx, expected.ID, &expected)
}

func sameSchedule(a, b Schedule) bool {
	a.Next, b.Next = time.Time{}, time.Time{}
	x, _ := json.Marshal(a)
	y, _ := json.Marshal(b)
	return string(x) == string(y)
}

func (s *Store) deleteSchedule(ctx context.Context, id string, expected *Schedule) error {
	return s.control(ctx, func(c *control) error {
		old, ok := c.Schedules[id]
		if !ok {
			return fmt.Errorf("unknown schedule")
		}
		if expected != nil && !sameSchedule(old, *expected) {
			return fmt.Errorf("schedule changed; refresh before removing")
		}
		delete(c.Schedules, id)
		return nil
	})
}

// Tick advances the trigger and queues its snapshot in one atomic update.
func (s *Store) Tick(ctx context.Context, now time.Time) error {
	return s.control(ctx, func(c *control) error {
		for id, v := range c.Schedules {
			if v.Paused || v.Next.After(now) {
				continue
			}
			due := v.Next
			next, err := nextSchedule(v, now)
			if err != nil {
				return err
			}
			v.Next = next
			c.Schedules[id] = v
			if v.Missed == "skip" && now.Sub(due) > time.Minute {
				continue
			}
			overlap := false
			for _, j := range c.Jobs {
				if j.Schedule == id && (j.Status == "queued" || j.Status == "running" || j.Status == "waiting_review") {
					overlap = true
					break
				}
			}
			if overlap && v.Overlap == "skip" {
				continue
			}
			if err := makeJobRoom(c); err != nil {
				return err
			}
			due = due.UTC()
			j := Job{ID: rand.Text(), RecipePath: v.RecipePath, Source: v.Source, Inputs: v.Inputs, Status: "queued", Created: now.UTC(), Schedule: id, ScheduledFor: &due}
			c.Jobs[j.ID] = j
		}
		return nil
	})
}

// Keep generations across deletion so a stale editor cannot modify a replacement trigger.
func (c *control) nextScheduleRevision() uint64 {

	c.ScheduleRevision++
	return c.ScheduleRevision
}
