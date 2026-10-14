package chat

import (
	"github.com/keshon/tars/internal/llm"
	"os"
	"path/filepath"
)

// CompactAndSave retains the opening exchange and complete recent turns.
// The previous archive is backed up before changing the active snapshot.
// This is mechanical compaction, not an LLM-generated memory summary.
func (c *Conversation) CompactAndSave(path string, keepTurns int) (int, int, error) {
	before := len(c.history)
	if keepTurns <= 0 || before <= 2*(keepTurns+1) {
		return before, before, nil
	}
	backup, err := os.CreateTemp(filepath.Dir(path), "before-compact-*.json")
	if err != nil {
		return before, before, err
	}
	backupPath := backup.Name()
	backup.Close()
	if err := c.Save(backupPath); err != nil {
		os.Remove(backupPath)
		return before, before, err
	}
	next := *c
	next.history = append([]llm.Message(nil), c.history[:2]...)
	next.history = append(next.history, c.history[before-2*keepTurns:]...)
	if err := next.Save(path); err != nil {
		return before, before, err
	}
	c.history = next.history
	return before, len(c.history), nil
}
