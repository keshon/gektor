package store

import (
	"encoding/json"
	"errors"
	"sync"
	"testing"
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
