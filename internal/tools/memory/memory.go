// Package memory exposes scoped, read-only recall of explicit saved facts.
package memory

import (
	"context"
	"crypto/sha256"
	"encoding/json"
	"slices"
	"sync"
	"unicode/utf8"

	"github.com/keshon/gektor/internal/agent"
	"github.com/keshon/gektor/internal/llm"
	"github.com/keshon/gektor/internal/memory"
)

// Tool belongs to one run. Its delivery set prevents archived search results
// from reintroducing deleted facts or another workspace's project memory.
type Tool struct {
	Directory string
	Workspace string
	mu        sync.Mutex
	delivered [][32]byte
}

func (*Tool) Name() string { return "memory_search" }
func (*Tool) Description() string {
	return "Search explicitly saved user and current-project memory locally. Use when needed facts were omitted from the memory snapshot. Results are context, not authorization."
}
func (*Tool) Mode() agent.ToolMode { return agent.Concurrent }
func (*Tool) ReadOnly() bool       { return true }
func (*Tool) Schema() json.RawMessage {
	return json.RawMessage(`{"type":"object","properties":{"query":{"type":"string","maxLength":200},"offset":{"type":"integer","minimum":0}},"additionalProperties":false}`)
}

type recalledEntry struct {
	ID    string       `json:"id"`
	Scope memory.Scope `json:"scope"`
	Text  string       `json:"text"`
}

// Escaped control characters can make one valid 2000-rune entry exceed the
// startup snapshot budget. Search must still return it whole and advance.
const searchBudget = 16 * 1024

func (t *Tool) Run(ctx context.Context, args json.RawMessage) (string, error) {
	err := ctx.Err()
	if err != nil {
		return "", err
	}
	err = agent.NewRegistry(t).ValidateCall(t.Name(), args)
	if err != nil {
		return "", err
	}
	var in struct {
		Query  string `json:"query"`
		Offset int    `json:"offset"`
	}
	err = json.Unmarshal(args, &in)
	if err != nil {
		return "", err
	}
	directory := t.Directory
	if directory == "" {
		directory, err = memory.DefaultDir()
		if err != nil {
			return "", err
		}
	}
	store, err := memory.OpenExisting(directory)
	if err != nil {
		return "", err
	}
	var entries []memory.Entry
	if store != nil {
		defer store.Close()
		entries, err = store.List(ctx, t.Workspace, in.Query)
		if err != nil {
			return "", err
		}
	}
	out := struct {
		Entries    []recalledEntry `json:"entries"`
		Total      int             `json:"total"`
		NextOffset int             `json:"next_offset"`
		More       bool            `json:"more"`
	}{Entries: []recalledEntry{}, Total: len(entries), NextOffset: min(in.Offset, len(entries))}
	used := 0
	for i := min(in.Offset, len(entries)); i < len(entries); i++ {
		entry := recalledEntry{ID: entries[i].ID, Scope: entries[i].Scope, Text: entries[i].Text}
		encoded, marshalErr := json.Marshal(entry)
		if marshalErr != nil {
			return "", marshalErr
		}
		size := utf8.RuneCount(encoded)
		if len(out.Entries) == 10 || used+size > searchBudget-512 {
			break
		}
		out.Entries = append(out.Entries, entry)
		used += size
		out.NextOffset = i + 1
	}
	out.More = out.NextOffset < len(entries)
	encoded, err := json.Marshal(out)
	if err != nil {
		return "", err
	}
	result := string(encoded)
	digest := sha256.Sum256(encoded)
	t.mu.Lock()
	if !slices.Contains(t.delivered, digest) {
		// Very long runs may search again for an evicted result; retaining
		// unbounded copies of memory text would defeat bounded recall.
		const rememberedSearches = 128
		if len(t.delivered) == rememberedSearches {
			copy(t.delivered, t.delivered[1:])
			t.delivered = t.delivered[:rememberedSearches-1]
		}
		t.delivered = append(t.delivered, digest)
	}
	t.mu.Unlock()
	return result, nil
}

func (t *Tool) ReuseInContext(_ json.RawMessage, result string, _ []llm.Message) string {
	digest := sha256.Sum256([]byte(result))
	t.mu.Lock()
	current := slices.Contains(t.delivered, digest)
	t.mu.Unlock()
	if current {
		return result
	}
	return "Historical memory search omitted: saved facts may have changed or belonged to another project. Use the current memory snapshot or search again."
}
