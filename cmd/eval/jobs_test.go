package main

import (
	"fmt"
	"slices"
	"sync"
	"testing"
)

func TestProbeJobsPreserveSerialOrderAndExclusiveIsolation(t *testing.T) {
	probes := []Probe{{Name: "first"}, {Name: "exclusive", Exclusive: true}, {Name: "last"}}
	for _, workers := range []int{1, 2} {
		t.Run(fmt.Sprint(workers), func(t *testing.T) {
			var mu sync.Mutex
			var order []string
			seen := map[string]bool{}
			runProbeJobs(probes, 2, workers, func(j evalJob) {
				mu.Lock()
				defer mu.Unlock()
				key := fmt.Sprintf("%s/%d", j.probe.Name, j.run)
				if seen[key] {
					t.Errorf("job repeated: %s", key)
				}
				seen[key] = true
				order = append(order, j.probe.Name)
			})
			if len(seen) != 6 {
				t.Fatalf("jobs lost: %v", seen)
			}
			if workers == 1 {
				if !slices.Equal(order, []string{"first", "first", "exclusive", "exclusive", "last", "last"}) {
					t.Fatalf("serial order changed: %v", order)
				}
			} else if !slices.Equal(order[:2], []string{"exclusive", "exclusive"}) {
				t.Fatalf("ordinary jobs started before exclusive jobs finished: %v", order)
			}
		})
	}
}
