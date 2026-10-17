package agent

import (
	"context"
	"encoding/json"
	"errors"
	"slices"
	"strings"

	"github.com/keshon/tars/internal/llm"
	"github.com/keshon/tars/internal/prompts"
	"github.com/keshon/tars/internal/session"
)

type Agent struct {
	cfg Config

	// LastRunMutations counts observed mutation batches. Embedders without
	// BeginChanges fall back to successful mutating tool calls.
	LastRunMutations int

	report RunReport
}

// MutatedPaths returns the workspace paths mutated by the last run,
// for session-end sweeps. Empty when nothing mutated.
func (a *Agent) MutatedPaths() []string {
	return a.report.MutatedPaths
}

// RunReport records the outcome, observed effects, and real check output.
type RunReport struct {
	Outcome      string
	Verification string
	Warnings     []string
	// Steps is how many model round-trips the run performed.
	Steps int

	// MutatedPaths includes changes made by file tools, shells, MCP, and delegates.
	// Without an observer, these are paths from successful mutating calls.
	MutatedPaths []string

	// Final is the model's final answer text ("" if the run errored out).
	Final string

	// WrapUp is the text-only closing summary captured on budget death,
	// separate from Final because Final == "" on error is pinned by
	// report_test.go and mission retry keys off the error, not the text.
	WrapUp string

	// MaxSteps is the budget enforced this run — base plus todo funding —
	// so observers read the enforced line, not the configured one.
	MaxSteps int

	// OpenTodos is the checklist still unchecked when the run ended.
	// Snapshot on the MaxSteps path only: that is the one exit where
	// unfinished acknowledged work needs an operator decision.
	OpenTodos []string

	// LastPromptTokens is the backend-reported prompt size of the last
	// completed call — how full the context actually got.
	LastPromptTokens int
}

// ToolNames returns the names of the tools this agent can call, sorted.
// Asserted on in tests: a harness that builds its agent by hand can
// otherwise ship a different tool set than the one it means to.
func (a *Agent) ToolNames() []string {
	if a.cfg.Tools == nil {
		return nil
	}
	return a.cfg.Tools.Names()
}

// Report returns measured facts about the most recent Run/Resume,
// including a partially filled report for a run that errored mid-way.
func (a *Agent) Report() RunReport {
	return a.report
}

// Run executes the agent loop on a single task and returns the model's
// final answer once it stops requesting tools. images attaches pictures
// (absolute paths) to the opening user message for vision-capable
// backends; imageless runs are unchanged.
func (a *Agent) Run(ctx context.Context, task string, images ...string) (string, error) {
	return a.run(ctx, []llm.Message{
		{Role: llm.RoleSystem, Content: a.cfg.System},
		{Role: llm.RoleUser, Content: task, Images: images},
	})
}

// Resume continues from a previously saved history (see LoadState) after
// a run was interrupted — crashed, killed, or hit MaxSteps. note is
// appended as a fresh user message; the full history is already there, so
// it need not summarise what happened.
func (a *Agent) Resume(ctx context.Context, history []llm.Message, note string, images ...string) (string, error) {
	history = a.resumeHistory(history)
	history = append(history, llm.Message{Role: llm.RoleUser, Content: note, Images: images})
	return a.run(ctx, history)
}

// PausedOnQuestion reports whether history ends on an assistant message
// with an unanswered question to a human — how to tell "interrupted by a
// question" from "interrupted by a crash". A nil registry falls back to
// matching a "question" argument, which is all a caller reading a saved
// session off disk can do: the tool set that produced it is long gone.
func PausedOnQuestion(reg *Registry, history []llm.Message) (callID string, question string, ok bool) {
	if len(history) == 0 {
		return "", "", false
	}
	last := history[len(history)-1]
	if last.Role != llm.RoleAssistant {
		return "", "", false
	}
	q := reg.Questioner()
	for _, tc := range last.ToolCalls {
		if q != nil {
			if tc.Name == q.Name() {
				return tc.ID, q.Question(tc.Arguments), true
			}
			continue
		}
		var args struct {
			Question string `json:"question"`
		}
		if json.Unmarshal(tc.Arguments, &args) == nil && strings.TrimSpace(args.Question) != "" {
			return tc.ID, strings.TrimSpace(args.Question), true
		}
	}
	return "", "", false
}

// ResumeWithAnswer continues from a history that ended with an unanswered
// ask_user. answer becomes the matching role:tool message, not a user
// message: a backend expects every tool_call answered before anything
// else, and a naked user turn after one is wire-invalid.
func (a *Agent) ResumeWithAnswer(ctx context.Context, history []llm.Message, callID, answer string) (string, error) {
	history = a.resumeHistory(history)
	history = append(history, llm.Message{
		Role:       llm.RoleTool,
		ToolCallID: callID,
		Content:    answer,
	})
	return a.run(ctx, history)
}

// LoadState reads a history previously written via Config.StateFile.
func LoadState(path string) ([]llm.Message, error) {
	return session.ReadHistory(path)
}

// SaveState writes a checkpoint atomically without running an agent.
func SaveState(path string, history []llm.Message) error {
	return session.WriteSnapshot(path, history)
}

// saveState is best-effort: a failed snapshot write should never abort a
// run that's otherwise working fine.
func (a *Agent) saveState(history []llm.Message) {
	if a.cfg.StateFile == "" {
		return
	}
	if err := a.persistState(history); err != nil {
		warning := "session persistence failed: " + err.Error()
		if !containsStr(a.report.Warnings, warning) {
			a.report.Warnings = append(a.report.Warnings, warning)
			if a.cfg.OnNudge != nil {
				a.cfg.OnNudge("persistence-error", warning)
			}
		}
	}
}

func (a *Agent) persistState(history []llm.Message) error {
	return session.WriteSnapshot(a.cfg.StateFile, history)
}

type runState struct {
	observeChanges                                                                     func() ([]string, error)
	observationErr                                                                     error
	stuckSteps, exploratorySteps, mutatingSucceeded, warnedThreshold, lastPromptTokens int
	consecutiveSameToolCount                                                           int
	// warnedSteps tracks the highest step-budget threshold already
	// warned about (75/90 of the enforced limit), mirroring
	// warnedThreshold for tokens.
	warnedSteps int
	// lastSingleResult fingerprints the previous same-tool step's
	// outcome, so the loop counter accumulates identical results, not
	// mere repetition: three fresh reads are exploration, three times
	// the same bytes are grinding. See trackSingleToolLoop.
	lastSingleResult                                                          uint64
	verifiedOnce, searchFatigueWarned, blockFinishDueToVerify, toolLoopWarned bool
	emptyFinishRetried                                                        bool
	// overflowRetried allows one compact-and-retry per run. A second
	// overflow means the window cannot hold this session however it is cut.
	overflowRetried                                   bool
	lastSignature, verifyFailedOutput, lastSingleTool string
	mutatedPaths                                      []string

	// zeroWriteFinishes counts how many times the model has tried to end
	// the run having written nothing, while VerifyOnZeroWrites says the
	// run was supposed to write. See MaxZeroWriteRefusals.
	zeroWriteFinishes int

	// todoBounces counts text-only finishes bounced for unchecked todos
	// since the last tool work. See maxTodoBounces.
	todoBounces int

	// todoFunded counts step budget granted for acknowledged todo work.
	// The high-water marks keep rewrites from double-funding: deleting an
	// item doesn't refund, re-adding it past the mark doesn't re-grant.
	todoFunded  int
	todoOpenMax int
	todoDoneMax int
	// todoClosingGranted latches the delivery handshake per all-done
	// episode, so reopening items earns its own close.
	todoClosingGranted bool

	// toolCalls gates the first verify round together with
	// claimsFileEffects: a finish with no tool work and no file-effect
	// claims is chit-chat. The one-shot stays armed for a later turn.
	toolCalls int

	// thinkWraps counts wrap-up rounds this run: finishes attempted with
	// more reasoning than ReasoningBudget allows. Bounded by
	// MaxThinkWraps; past it the run degrades to finishing as-is.
	thinkWraps int

	// pendingBudget holds a context-usage notice that lost its slot to a
	// more urgent nudge, to be delivered on the next step that has one
	// free. See interject.
	pendingBudget string

	// failedCalls counts consecutive identical failures per call, cleared
	// on success or on any workspace mutation. See maxIdenticalAttempts.
	failedCalls map[string]int

	// stuckNudges counts how many times this run has been told it is
	// stuck, so the wording can escalate instead of repeating.
	stuckNudges int

	// idempotentSeen maps signature (name+args) of successful idempotent
	// calls to the step that ran them, cleared on any mutation. Exact
	// repeats short-circuit: a weak model re-reading the same bytes five
	// times is told so, instead of getting five copies.
	idempotentSeen map[string]int

	// wrotePaths maps write_file path to the step that wrote it. A second
	// write to the same path is refused. Added 2026-07-14: a weak model
	// rewrote package.json with identical content in a loop, ignoring the
	// softer loop nudges, because each "ok" reset stuckSteps.
	wrotePaths map[string]int
}

// ErrMaxSteps marks a run that did real work but ran out of step budget,
// as opposed to infrastructure failure where none happened. Retry loops
// must separate them: a budget death can converge, a dead backend just
// burns retries on connection errors.
var ErrMaxSteps = errors.New("reached max steps")

func containsStr(list []string, s string) bool {
	for _, v := range list {
		if v == s {
			return true
		}
	}
	return false
}

// Tools returns the run's registry, so a caller holding an Agent can ask
// what it was offered — which question tool, whether a checklist exists.
func (a *Agent) Tools() *Registry { return a.cfg.Tools }

// situationHints names, per nudge situation, the tools that could help.
	// A situation whose tools are all absent yields nothing.
var situationHints = map[string][]string{
	"truncated":     {"write_file", "patch_file", "patch_lines"},
	"search-fatigue": {"ask_user"},
	"compact":       {"delegate_task"},
}

var situationPrompts = map[string]string{
	"truncated":      prompts.WriteHint,
	"search-fatigue": prompts.AskHint,
	"compact":        prompts.DelegateHint,
}

// hintResolver answers from a registry which of a situation's tools are
// actually registered. It is the default Config.ToolHint; a caller that
// wants different advice replaces it.
func hintResolver(reg *Registry) func(string) string {
	return func(situation string) string {
		if !reg.HasAny(situationHints[situation]...) {
			return ""
		}
		return situationPrompts[situation]
	}
}

// writesFiles reads Config.MutatingTools rather than hardcoding a name,
// so the rewrite guard covers whichever writer a registry offers.
func (a *Agent) writesFiles(name string) bool {
	return slices.Contains(a.cfg.MutatingTools, name)
}

// hint asks the run's owner which registered tool suits this situation.
// Empty when nothing does, or when the run wired no resolver.
func (a *Agent) hint(situation string) string {
	if a.cfg.ToolHint == nil {
		return ""
	}
	if line := strings.TrimSpace(a.cfg.ToolHint(situation)); line != "" {
		return " " + line
	}
	return ""
}

func (a *Agent) resumeHistory(history []llm.Message) []llm.Message {
	history = append([]llm.Message(nil), history...)
	if len(history) > 0 && history[0].Role == llm.RoleSystem {
		history[0].Content = a.cfg.System
	}
	return history
}
