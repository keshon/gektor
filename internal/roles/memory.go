package roles

import (
	"context"
	"fmt"
	"time"

	"github.com/keshon/gektor/internal/memory"
)

// Each role construction takes one stable snapshot. A follow-up rebuilds it,
// so edits and deletions replace the previous session's system memory.
func (e Env) memoryContext() string {
	directory := e.MemoryDir
	var err error
	if directory == "" {
		directory, err = memory.DefaultDir()
		if err != nil {
			return e.memoryUnavailable(err)
		}
	}
	store, err := memory.OpenExisting(directory)
	if err != nil {
		return e.memoryUnavailable(err)
	}
	if store == nil {
		return ""
	}
	defer store.Close()
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()
	snapshot, err := store.Snapshot(ctx, e.WS.Root(), memory.DefaultBudget)
	if err != nil {
		return e.memoryUnavailable(err)
	}
	if snapshot.Text == "" {
		return ""
	}
	return "\n\n" + snapshot.Text
}

func (e Env) memoryUnavailable(err error) string {
	if e.OnLocal != nil {
		e.OnLocal(fmt.Sprintf("Persistent memory unavailable: %v", err))
	}
	return "\n\nPersistent memory could not be loaded. Do not infer saved preferences or facts from that absence."
}
