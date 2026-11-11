package agent

import (
	"github.com/keshon/gektor/internal/conversation"
	"github.com/keshon/gektor/internal/llm"
)

// CompactFile mechanically compacts a saved snapshot in place: the
// same compactHistory the loop uses, invoked on demand (/compact)
// instead of at the usage threshold. Writes only when something actually
// dropped, through the same atomic writer as saveState. An error leaves the
// snapshot untouched.
//
// The compacted history is returned rather than left for the caller to read
// back: it was already decoded here, and the /compact path read the same
// file a second time purely to get it.
func CompactFile(stateFile string, keepSteps int) (before, after int, history []llm.Message, err error) {
	snapshot, loadErr := conversation.Load(stateFile)
	err = loadErr
	if err != nil {
		return 0, 0, nil, err
	}
	history = snapshot.Context()
	before = len(history)
	compacted := compactHistory(history, keepSteps)
	if len(compacted) < len(history) {
		compacted, err = retainArchivedInstructions(snapshot, compacted)
		if err != nil {
			return before, before, history, err
		}
	}
	after = len(compacted)
	if after >= before {
		return before, after, history, nil
	}
	snapshot.Compact(compacted)
	if err := snapshot.Save(stateFile); err != nil {
		return before, after, history, err
	}
	return before, after, compacted, nil
}
