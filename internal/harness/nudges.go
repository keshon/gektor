// Package harness owns the vocabulary shared by the loop that decides what
// the model is told and the preferences file that has to spell the same
// choice on disk.
//
// It is a leaf on purpose. The loop is not allowed to import the config
// package - that is what lets internal/tools implement agent.Tool without a
// cycle - and the config package should not have to know what a nudge is. So
// the shared words live here, where both can reach them, and neither has to
// reach for the other.
package harness

import "strings"

// NudgeLevel is how much harness advice the model hears.
//
//	NudgeFull     everything (default)
//	NudgeRecovery only the notices that turn a bad ending into another
//	              attempt; the free advice is dropped
//	NudgeOff      no advice at all. The compaction notice still arrives:
//	              it is a fact about the conversation, not a suggestion.
type NudgeLevel int

const (
	NudgeFull NudgeLevel = iota
	NudgeRecovery
	NudgeOff
)

// Parse reads a stored or flagged value. Unknown names fail rather than
// silently defaulting, so a typo cannot quietly disable the harness.
func Parse(s string) (NudgeLevel, bool) {
	switch strings.ToLower(strings.TrimSpace(s)) {
	case "full", "all":
		return NudgeFull, true
	case "recovery", "essential":
		return NudgeRecovery, true
	case "off", "none":
		return NudgeOff, true
	}
	return NudgeOff, false
}

// Name renders a level back to its stored spelling.
func (l NudgeLevel) Name() string {
	switch l {
	case NudgeRecovery:
		return "recovery"
	case NudgeOff:
		return "off"
	}
	return "full"
}
