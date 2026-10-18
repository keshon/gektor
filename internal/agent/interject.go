package agent

import (
	"github.com/keshon/tars/internal/harness"
	"github.com/keshon/tars/internal/llm"
	"github.com/keshon/tars/internal/prompts"
)

// stepOutcome carries the parts of a completed step that interject can't
// read off runState.
type stepOutcome struct {
	// repeat is true when this step's tool calls were byte-identical to
	// the previous step's — it picks StuckRepeating over StuckFailing.
	repeat bool

	// budget is a context-usage notice for this step, or "" for none.
	budget string
}

// markNudge reports harness text and returns it marked, for call sites
// (interject) that append history themselves. A kind the level drops
// returns "" so the caller appends nothing.
func (a *Agent) markNudge(kind, text string) string {
	marked := harnessText(text)
	a.notifyNudge(kind, marked)
	if !nudgeAllowed(a.cfg.Nudges, kind) {
		return ""
	}
	return marked
}

// interject picks at most ONE thing to say to the model at the end of a
// step that made tool calls.
//
// It replaces six independent append sites, all of which could fire in
// the same step: a worker could receive a search-fatigue nudge, a
// tool-loop warning, a budget notice and a stuck escalation at once, in
// source order rather than importance order. A 4B-active model given four
// corrections simultaneously follows none of them — and some of them
// contradict each other outright ("broaden your search" against "stop
// searching and act"). One signal, chosen by severity, is the whole point.
//
// The latches keep each advisory to once per run; without them a nudge
// repeats every step for as long as its counter stays over the line,
// which is its own kind of noise.
func (a *Agent) interject(st *runState, step int, o stepOutcome) string {
	// A budget notice fires once per threshold crossed, so it has to
	// queue rather than be dropped when something more urgent takes the
	// slot — otherwise the run never hears about it again.
	if o.budget != "" {
		st.pendingBudget = o.budget
	}

	switch {
	case st.stuckSteps >= a.cfg.MaxStuckSteps:
		// Hardest signal: nothing has worked for MaxStuckSteps running.
		//
		// This has no latch, unlike the nudges below, because being stuck
		// can recur for a new reason. What it must not do is say the same
		// thing twice: a live worker received the identical StuckFailing
		// text four times in one run and ignored all four. After the
		// first, the wording escalates; after the second, the loop stops
		// talking and lets the step budget end the run, because a third
		// copy of advice already refused twice is only context.
		st.stuckSteps = 0
		st.lastSignature = "" // the nudge itself breaks the repeat chain
		st.stuckNudges++
		switch {
		case st.stuckNudges == 1 && o.repeat:
			return a.markNudge(NudgeStuck, prompts.StuckRepeating+a.stepTag(st, step))
		case st.stuckNudges == 1:
			return a.markNudge(NudgeStuck, prompts.StuckFailing+a.stepTag(st, step))
		case st.stuckNudges == 2:
			return a.markNudge(NudgeStuck, prompts.StuckEscalated+a.stepTag(st, step))
		default:
			return ""
		}

	case st.consecutiveSameToolCount >= maxSameToolSteps && !st.toolLoopWarned:
		st.toolLoopWarned = true
		return a.markNudge(NudgeStuck, prompts.ToolLoop)

	case st.exploratorySteps >= a.cfg.MaxExploratorySteps && !st.searchFatigueWarned:
		st.searchFatigueWarned = true
		msg := prompts.SearchFatigue + a.hint("search-fatigue")
		return a.markNudge(NudgeStuck, msg)
	}

	msg := st.pendingBudget
	st.pendingBudget = ""
	if msg != "" {
		return a.markNudge(NudgeBudget, msg)
	}
	return msg
}

// Nudge kinds tag loop-generated harness text for display filtering
// and audit. Single source of truth (docs/rpc.md lists the same set);
// untyped so call sites need no conversions.
const (
	NudgeOverflow  = "overflow"
	NudgeTruncated = "truncated"
	NudgeThinkWrap = "think-wrap"
	NudgeLeak      = "leak"
	NudgeVerify    = "verify"
	NudgeRefusal   = "refusal"
	NudgeBudget    = "budget"
	NudgeStuck     = "stuck"
	NudgeTodo      = "todo"
	NudgeClosing   = "closing"
	// NudgeWrapUp is the closing turn's instruction. It is harness text
	// and was appended bare, so the model read a loop instruction as
	// something the user had typed.
	NudgeWrapUp = "wrap-up"
	// NudgeCompact carries the compaction notice so an observer can see
	// that context was dropped while the transcript still shows the turns
	// that went with it.
	//
	// Unlike every other kind it is not advice and cannot be turned off:
	// see allows.
	NudgeCompact = "compact"
)

// NudgeLevel selects how much of the harness the model hears. The levels
// are not a quality ladder; they split by cost.
//
// A nudge delivered by interject rides a turn the loop was taking anyway:
// the step already had tool calls, so another model call happens whatever
// the harness says, and the notice costs ~40 tokens. A nudge that answers
// a finish — truncated, think-wrap, todo, closing, overflow — returns no
// answer, so the loop calls the model again, and that is a whole extra
// turn. Tars is tuned for weak local models, where both pay for
// themselves; a strong model pays the turns for nothing.
//
// The level itself, and the vocabulary the preferences file has to spell, are
// declared once in config; these are aliases rather than a second copy.
type NudgeLevel = harness.NudgeLevel

const (
	NudgeFull     = harness.NudgeFull
	NudgeRecovery = harness.NudgeRecovery
	NudgeOff      = harness.NudgeOff
)

// ParseNudgeLevel reads a -nudges value.
func ParseNudgeLevel(s string) (NudgeLevel, bool) { return harness.Parse(s) }

// NudgeLevelName renders a level back to its flag value.
func NudgeLevelName(l NudgeLevel) string { return l.Name() }

// nudgeClass says what a kind of harness text costs, which is what actually
// decides whether a level may deliver it.
type nudgeClass uint8

const (
	// classFact is not advice. It states something about the conversation
	// the model is in the middle of, in the same class as a tool result,
	// and cannot be switched off: a model told to reason over a window
	// that is quietly incomplete answers from messages that are no longer
	// there.
	classFact nudgeClass = iota
	// classRecovery answers a finish and therefore costs a whole turn.
	// These are what stop a bad ending from ending the run, so dropping
	// them means a stuck loop spends every remaining step failing
	// identically. That is why "off" drops them and why they are the only
	// thing "recovery" keeps.
	classRecovery
	// classAdvisory rides a turn the model was already having, so it costs
	// nothing to deliver. A strong model pays only the attention.
	classAdvisory
)

// nudgeClasses is total over every kind the loop can produce.
//
// The previous hand-maintained advisory set was not: a new kind that nobody
// remembered to add silently became a recovery nudge, so "recovery" meant
// something different depending on when the code was written. Classifying
// explicitly makes adding a nudge a decision, and
// TestEveryNudgeKindHasAClass fails if it is not one.
var nudgeClasses = map[string]nudgeClass{
	// Ride an existing turn: delivered at the end of a step that made
	// tool calls, without another round trip.
	NudgeStuck:  classAdvisory,
	NudgeBudget: classAdvisory,

	// Answer a finish: each one is a turn the model did not otherwise
	// spend.
	NudgeOverflow:  classRecovery,
	NudgeTruncated: classRecovery,
	NudgeThinkWrap: classRecovery,
	NudgeLeak:      classRecovery,
	NudgeVerify:    classRecovery,
	NudgeRefusal:   classRecovery,
	NudgeTodo:      classRecovery,
	NudgeClosing:   classRecovery,
	NudgeWrapUp:    classRecovery,

	NudgeCompact: classFact,
}

// nudgeAllowed reports whether a kind should reach the model at this level.
//
// It is a function rather than a method because NudgeLevel is an alias for
// config's type, and Go will not let a package define methods on a type it
// does not own.
//
// A fact always passes: "off" means no advice, not silence about what
// happened to the conversation. Of what is left, full delivers everything,
// recovery keeps the turn-costing notices and drops the free advice, and off
// drops all of it.
func nudgeAllowed(level NudgeLevel, kind string) bool {
	switch nudgeClasses[kind] {
	case classFact:
		return true
	case classRecovery:
		return level != NudgeOff
	case classAdvisory:
		return level == NudgeFull
	}
	return level == NudgeFull
}

// nudges reports whether a finish-bounce of this kind should happen at
// all. Callers ask before bouncing, not only before speaking: a bounce
// costs a whole turn, so a suppressed one that still called the model
// again would spend a turn to say nothing.
func (a *Agent) nudges(kind string) bool { return nudgeAllowed(a.cfg.Nudges, kind) }

// harnessText marks loop-generated text as harness provenance. The
// model that mused "probably meta instructions from the user" was
// reading bare instructions; "[harness] ..." names the author inline,
// in history, events, and transcripts alike.
func harnessText(text string) string {
	return harnessPrefix + text
}

// harnessPrefix is the author tag harnessText writes. Anything carrying
// it is the loop talking, not the user, and readers that must tell them
// apart match on it rather than on the full marker.
const harnessPrefix = "[harness] "

// notifyNudge reports harness text to the observer, nil-safe.
func (a *Agent) notifyNudge(kind, text string) {
	if a.cfg.OnNudge != nil {
		a.cfg.OnNudge(kind, text)
	}
}

// nudge appends harness text to history and reports it. Every
// loop-generated message goes through here (or markNudge below) so
// observers see exactly what the model saw — never a paraphrase.
//
// A kind the level suppresses still gets reported to observers: turning
// the harness down is a change to what the model is told, not an
// erasure of the record that something was suppressed.
func (a *Agent) nudge(history []llm.Message, kind, text string) []llm.Message {
	marked := harnessText(text)
	a.notifyNudge(kind, marked)
	if !nudgeAllowed(a.cfg.Nudges, kind) {
		return history
	}
	return append(history, llm.Message{Role: llm.RoleUser, Content: marked})
}
