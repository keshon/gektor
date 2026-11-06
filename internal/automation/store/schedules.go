package store

import (
	"context"
	"crypto/rand"
	"fmt"
	"github.com/robfig/cron/v3"
	"sort"
	"time"
	_ "time/tzdata"
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
		if _, ok := c.Schedules[v.ID]; ok {
			return fmt.Errorf("schedule exists; remove it before replacing")
		}
		if len(c.Schedules) >= 100 {
			return fmt.Errorf("schedule limit reached (100)")
		}
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
	return s.control(ctx, func(c *control) error {
		if _, ok := c.Schedules[id]; !ok {
			return fmt.Errorf("unknown schedule")
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
			j := Job{ID: rand.Text(), RecipePath: v.RecipePath, Source: v.Source, Inputs: v.Inputs, Status: "queued", Created: now.UTC(), Schedule: id}
			c.Jobs[j.ID] = j
		}
		return nil
	})
}
