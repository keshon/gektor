package store

import (
	"encoding/json"
	"errors"
	"fmt"
	"sync"
	"testing"
	"time"
)

func TestSubmissionDeduplicationAcrossHandles(t *testing.T) {
	db, base, work := testStore(t)
	other, err := OpenStore(base, work)
	if err != nil {
		t.Fatal(err)
	}
	defer other.Close()
	var wg sync.WaitGroup
	for i := 0; i < 12; i++ {
		s := db
		if i%2 == 0 {
			s = other
		}
		wg.Go(func() {
			_, err := s.EnqueueOnce(t.Context(), "recipe.json", json.RawMessage(`{}`), nil, "key", "hash")
			if err != nil {
				t.Error(err)
			}
		})
	}
	wg.Wait()
	jobs, err := db.Jobs(t.Context())
	if err != nil || len(jobs) != 1 {
		t.Fatal(jobs, err)
	}
	if _, err := other.EnqueueOnce(t.Context(), "recipe.json", json.RawMessage(`{}`), nil, "key", "different"); !errors.Is(err, ErrSubmissionConflict) {
		t.Fatal(err)
	}
	reopened, err := OpenStore(base, work)
	if err != nil {
		t.Fatal(err)
	}
	defer reopened.Close()
	repeated, err := reopened.EnqueueOnce(t.Context(), "recipe.json", json.RawMessage(`{}`), nil, "key", "hash")
	if err != nil || repeated.ID != jobs[0].ID {
		t.Fatal(repeated, err)
	}
}

func TestInterruptedSubmissionSurvivesQueueEviction(t *testing.T) {
	s, _, _ := testStore(t)
	job, err := s.EnqueueOnce(t.Context(), "recipe", json.RawMessage(`{}`), nil, "key", "hash")
	if err != nil {
		t.Fatal(err)
	}
	err = s.UpdateJob(t.Context(), job.ID, func(j *Job) error {
		j.Status = "interrupted"
		j.Created = time.Now().Add(-time.Hour)
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
	err = s.control(t.Context(), func(c *control) error {
		for i := range 999 {
			id := fmt.Sprintf("terminal-%d", i)
			c.Jobs[id] = Job{ID: id, Status: "completed", Source: json.RawMessage(`{}`), Created: time.Now()}
		}
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
	_, err = s.Enqueue(t.Context(), "new", json.RawMessage(`{}`), nil)
	if err != nil {
		t.Fatal(err)
	}
	repeated, err := s.EnqueueOnce(t.Context(), "recipe", json.RawMessage(`{}`), nil, "key", "hash")
	if err != nil || repeated.ID != job.ID {
		t.Fatal("interrupted submission lost", repeated, err)
	}

	err = s.control(t.Context(), func(c *control) error {
		for id, j := range c.Jobs {
			j.Status = "interrupted"
			c.Jobs[id] = j
		}
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
	_, err = s.Enqueue(t.Context(), "overflow", json.RawMessage(`{}`), nil)
	if err == nil {
		t.Fatal("evicted a recoverable job from a full queue")
	}
}
