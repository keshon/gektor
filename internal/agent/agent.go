package agent

import (
	"context"
	"encoding/json"
	"errors"
	"os"
	"reflect"
	"strings"

	"github.com/keshon/gektor/internal/llm"
	"github.com/keshon/gektor/internal/prompts"
	"github.com/keshon/gektor/internal/session"
)

type Agent struct {
	cfg           Config
	snapshot      session.Snapshot
	checkpointLen int

	// allowed is the read-only allowlist, nil when unrestricted.
	allowed map[string]bool

	// LastRunMutations counts changed paths. Embedders without
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

	// MaxSteps is the budget enforced this run — fixed operator limit —
	// so observers read the enforced line, not the configured one.
	MaxSteps int

	// OpenTodos is the checklist still unchecked when the run ended.
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
	history := []llm.Message{
		{Role: llm.RoleSystem, Content: a.cfg.System},
		{Role: llm.RoleUser, Content: task, Images: images},
	}
	a.snapshot, a.checkpointLen = session.New(history), len(history)
	return a.run(ctx, history)
}

// Resume continues from a previously saved history (see LoadState) after
// a run was interrupted — crashed, killed, or hit MaxSteps. note is
// appended as a fresh user message; the full history is already there, so
// it need not summarise what happened.
func (a *Agent) Resume(ctx context.Context, history []llm.Message, note string, images ...string) (string, error) {
	var err error
	history, err = a.restoreHistory(history)
	if err != nil {
		return "", err
	}
	history = a.closePendingCalls(a.resumeHistory(history), "", "")
	if strings.TrimSpace(note) != "" || len(images) > 0 {
		history = append(history, llm.Message{Role: llm.RoleUser, Content: note, Images: images})
	}
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
	var err error
	history, err = a.restoreHistory(history)
	if err != nil {
		return "", err
	}
	history = a.closePendingCalls(a.resumeHistory(history), callID, answer)
	return a.run(ctx, history)
}

// LoadState reads a history previously written via Config.StateFile.
func LoadState(path string) ([]llm.Message, error) {
	return session.ReadContext(path)
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
	a.captureHistory(history)
	return a.snapshot.Save(a.cfg.StateFile)
}

func (a *Agent) restoreHistory(history []llm.Message) ([]llm.Message, error) {
	a.snapshot = session.New(history)
	if a.cfg.StateFile != "" {
		s, err := session.Load(a.cfg.StateFile)
		if err == nil && (sameHistory(history, s.History) || sameHistory(history, s.Context())) {
			a.snapshot = s
		} else if err != nil && !os.IsNotExist(err) {
			return nil, err
		}
	}
	history = a.snapshot.Context()
	a.checkpointLen = len(history)
	return history, nil
}

func sameHistory(a, b []llm.Message) bool {
	if len(a) > 0 && len(b) > 0 && a[0].Role == llm.RoleSystem && b[0].Role == llm.RoleSystem {
		return reflect.DeepEqual(a[1:], b[1:])
	}
	return reflect.DeepEqual(a, b)
}

func (a *Agent) captureHistory(history []llm.Message) {
	if a.snapshot.Version == 0 {
		a.snapshot = session.New(history)
		a.checkpointLen = len(history)
		return
	}
	if len(history) > a.checkpointLen {
		a.snapshot.History = append(a.snapshot.History, history[a.checkpointLen:]...)
	}
	if len(history) > 0 && len(a.snapshot.History) > 0 {
		a.snapshot.History[0] = history[0]
	}
	a.checkpointLen = len(history)
}

func (a *Agent) compactContext(previous, next []llm.Message) {
	a.captureHistory(previous)
	a.snapshot.Compact(next)
	a.checkpointLen = len(next)
}

// Never replay a tool whose completion is unknown: it may already have changed
// the workspace. Close only the interrupted trailing group with explicit results.
func (a *Agent) closePendingCalls(history []llm.Message, answerID, answer string) []llm.Message {
	start := len(history) - 1
	for start >= 0 && history[start].Role == llm.RoleTool {
		start--
	}
	if start < 0 || history[start].Role != llm.RoleAssistant {
		return history
	}
	answered := map[string]bool{}
	for _, msg := range history[start+1:] {
		answered[msg.ToolCallID] = true
	}
	for _, call := range history[start].ToolCalls {
		if answered[call.ID] {
			continue
		}
		failed := true
		result := llm.Message{Role: llm.RoleTool, ToolCallID: call.ID, ToolFailed: &failed, Content: "error: execution interrupted; completion is unknown. Inspect the workspace before retrying this action."}
		if call.ID == answerID {
			result.Content = answer
			failed = false
		}
		history = append(history, result)
		if a.cfg.OnResult != nil {
			a.cfg.OnResult(ToolResult{CallID: call.ID, Output: result.Content, Failed: failed})
		}
	}
	return history
}

type runState struct {
	observeChanges                                         func() ([]string, error)
	observationErr                                         error
	mutatingSucceeded, lastPromptTokens, lastCompactTokens int
	emptyFinishRetried, overflowRetried                    bool
	mutatedPaths                                           []string

	// repeatSignature is the previous step's call signature and repeatCount
	// is how many consecutive steps have now issued it. A model that stops
	// learning keeps asking the same question, so the loop stops handing it
	// tools after maxRepeatedSteps. Cleared on mutation: repeating a call
	// across a write is work, not a spin.
	repeatSignature string
	repeatCount     int

	// idempotentSeen maps an idempotent call's canonical arguments to the
	// step that ran it. An exact repeat is refused instead of served the
	// same bytes again, and the map is dropped on mutation or compaction,
	// because the refusal promises a result that is still valid.
	idempotentSeen     map[string]int
	lastPromptEstimate int
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
var situationHints = map[string][]string{"truncated": {"write_file", "patch_file"}}
var situationPrompts = map[string]string{"truncated": prompts.WriteHint}

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

// mayRun reports whether this role is allowed to execute a tool. Unrestricted
// roles allow every name the registry knows.
func (a *Agent) mayRun(name string) bool {
	return a.allowed == nil || a.allowed[name]
}

// withDirective keeps the role's stance at the end of the conversation. A
// stance is a harness turn rather than part of System because System is the
// prompt's first message: changing it when the role changes makes a caching
// backend re-read the entire conversation. The turn is only appended when the
// conversation ended in a different stance, so resuming in the same role adds
// nothing, and calling this after compaction restores a stance the compaction
// dropped.
func (a *Agent) withDirective(history []llm.Message) []llm.Message {
	if a.cfg.Directive == "" {
		return history
	}
	mine := harnessPrefix + a.cfg.Directive
	other := ""
	if a.cfg.Supersedes != "" {
		other = harnessPrefix + a.cfg.Supersedes
	}
	last := ""
	for i := len(history) - 1; i >= 0; i-- {
		if history[i].Role != llm.RoleUser {
			continue
		}
		if history[i].Content == mine {
			last = mine
			break
		}
		if other != "" && history[i].Content == other {
			last = other
			break
		}
	}
	if last == mine {
		return history
	}
	return append(history, llm.Message{Role: llm.RoleUser, Content: mine})
}
