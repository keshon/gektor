package roles

import (
	"context"
	"path/filepath"
	"time"

	"github.com/keshon/gektor/internal/agent"
	"github.com/keshon/gektor/internal/checks"
	"github.com/keshon/gektor/internal/events"
	"github.com/keshon/gektor/internal/llm"
	"github.com/keshon/gektor/internal/permission"
	"github.com/keshon/gektor/internal/prompts"
	"github.com/keshon/gektor/internal/tools"
	toolbackground "github.com/keshon/gektor/internal/tools/background"
	toolimages "github.com/keshon/gektor/internal/tools/images"
	tooloutput "github.com/keshon/gektor/internal/tools/output"
	"github.com/keshon/gektor/internal/workspace"
)

// Env supplies shared launch dependencies, limits, and observers to task-role
// constructors. Callers own its services and build a snapshot for each run.
type Env struct {
	// ChildEvents borrows a sink for child envelopes (call_id, kind, fields).
	// Child activity stays separate from parent transcript and usage.
	ChildEvents       events.Sink
	RefreshModelFacts func(context.Context) llm.ModelFacts
	Identity          string
	IdentityName      string
	UserName          string
	Client            llm.Client
	WS                *workspace.Workspace
	Procs             *toolbackground.Processes
	Outputs           *tooloutput.Store
	MaxTokens         int
	Temperature       float64
	ContextLimit      int
	Policy            permission.Policy
	BackendKind       string

	// Vision reports that the backend can process pictures, so read_file
	// may attach an image instead of returning binary metadata. It comes
	// from application model discovery, never from a guess.
	Vision      bool
	VisionKnown bool

	// Model names the backend model for display (status, logs). Local
	// servers ignore it on the wire (they serve one model); it still
	// answers "what am I talking to". Empty renders as a dash.
	Model  string
	Stream bool

	MaxSteps         int
	DelegateSteps    int
	DelegateMaxSteps int
	RunTimeout       time.Duration
	Inbox            *agent.Inbox
	OnInput          func(llm.Message)

	// MCPTools are discovered MCP tools appended to the full tool set
	// (Interactive, Subagent). Planner advertises them too, so that its
	// prompt matches Interactive's, but refuses to run them: a mode switch
	// must not change the prompt prefix.
	MCPTools          []agent.Tool
	Browser           *tools.Browser
	CapabilitySources func() []*tools.Capability
	Capabilities      []*tools.Capability
	// NativeExposure is an internal eval override; nil preserves eager web tools.
	NativeExposure map[string]tools.Exposure

	// OnDelta receives streamed answer text for live display. OnReasoning
	// receives streamed deliberation, which arrives first.
	OnDelta     func(chunk string)
	OnReasoning func(chunk string)

	// Gate receives the active context for operator permission decisions.
	// Nil denies calls that require an operator answer.
	Gate permission.Gate

	// OnStep receives parent-run steps tagged with the role label. Delegated
	// steps use ChildEvents and never invoke this parent hook.
	OnStep func(label string, step int, msg llm.Message)

	OnResult       func(agent.ToolResult)
	OnToolFinished func(callID string)
	OnToolOutput   func(callID, text string)

	// Rules are the deterministic checks this run applies to written
	// files. Project-specific validation uses the configured verify command.
	Rules []checks.Rule

	// Checks collects the run's findings: post-write scans from wrapped
	// file tools, plus the deep session-end sweep. Nil disables both, and
	// tools then run unwrapped, identical to uninstrumented.
	Checks *checks.Reporter

	// OnUsage, if set, receives backend token counts per step.
	// See agent.Config.OnUsage.
	OnUsage    func(step int, usage llm.Usage)
	OnProgress func(llm.Progress)
	OnPrompt   func(llm.ContextReport)

	// OnNudge receives loop-generated harness text (verify rounds,
	// refusals, leak notices, wrap-ups, stuck escalations) tagged by
	// kind. Nil disables reporting; the [harness] prefix applies
	// regardless, so provenance never depends on observers.
	OnNudge   func(kind, text string)
	OnContext func(llm.Message)
	OnLocal   func(string)

	// StateDir, when set, is where transcripts go; a role's stateFile
	// argument is resolved against it. Empty means the caller passes
	// whole paths, or none at all.
	StateDir string
	// WorkflowDir overrides the user workflow store for isolated evaluations.
	WorkflowDir string
}

// Sweep runs the deep session-end pass over the paths a run changed,
// catching writes that no tool reported as a post-write edit. Call it
// once per run, after the last turn, with every path the run mutated.
func (e Env) Sweep(paths []string) {
	if e.Checks != nil {
		e.Checks.Sweep(e.WS, paths)
	}
}

func (e Env) policy() permission.Policy {
	if len(e.Policy.Rules) == 0 {
		return permission.Default()
	}
	return e.Policy
}

// Refreshed returns one run's snapshot; subagents inherit it without probing.
func (e Env) Refreshed(ctx context.Context) Env {
	if e.RefreshModelFacts == nil {
		return e
	}
	facts := e.RefreshModelFacts(ctx)
	if facts.ContextKnown && facts.ContextLimit > 0 {
		e.ContextLimit = facts.ContextLimit
	}
	if facts.ModelKnown {
		e.Model = facts.Model
	}
	if facts.VisionKnown {
		e.Vision = facts.Vision
		e.VisionKnown = true
	}
	return e
}

// Execution settings shared by all roles, without parent input or event routing.
func (e Env) executionConfig(system, stateFile string) agent.Config {
	store := e.Outputs
	if store == nil {
		store = tooloutput.New(e.statePath(stateFile))
	}

	project, skills := e.WS.InstructionSections()
	core := system + "\n\n" + prompts.Environment(e.WS.Root(), e.Vision) + e.browserContext()

	return agent.Config{
		System: core + project + skills + "\n\n" + e.Identity,
		SystemSections: []llm.PromptSection{
			{Name: "core_instructions", Text: core},
			{Name: "project_instructions", Text: project},
			{Name: "skill_metadata", Text: skills},
			{Name: "identity", Text: "\n\n" + e.Identity},
		},

		Client:       e.Client,
		MaxTokens:    e.MaxTokens,
		Temperature:  e.Temperature,
		ContextLimit: e.ContextLimit,
		StateFile:    e.statePath(stateFile),
		Workspace:    e.WS.Root(),

		Changes:        e.WS.NewChangeObserver(),
		ToolContext:    e.visionContext,
		Outputs:        store,
		Summarize:      true,
		Paths:          tools.WorkspacePaths{WS: e.WS},
		Policy:         e.policy(),
		BeforeToolCall: e.Gate,
	}
}

// Parent runs consume operator input and report directly to the frontend.
// Children attach only their separate event route in observeSubagent.
func (e Env) configureParent(cfg *agent.Config, label string) {
	cfg.Inbox = e.Inbox
	cfg.OnInput = e.OnInput
	cfg.Notifications = e.Procs

	cfg.OnStep = nil
	if e.OnStep != nil {
		cfg.OnStep = func(step int, message llm.Message) {
			e.OnStep(label, step, message)
		}
	}
	cfg.OnResult = e.OnResult
	cfg.OnToolOutput = e.OnToolOutput
	cfg.OnToolFinished = e.OnToolFinished
	cfg.OnUsage = e.OnUsage
	cfg.OnProgress = e.OnProgress
	cfg.OnPrompt = e.OnPrompt
	cfg.OnNudge = e.OnNudge
	cfg.OnContext = e.OnContext
	cfg.OnLocal = e.OnLocal

	cfg.Stream = e.Stream
	cfg.OnDelta = e.OnDelta
	cfg.OnReasoning = e.OnReasoning
}

func (e Env) statePath(name string) string {
	switch {
	case name == "":
		return ""
	case e.StateDir == "":
		return name
	default:
		return filepath.Join(e.StateDir, name)
	}
}

// visionContext supplies the probed model capability to image-producing adapters.
func (e Env) visionContext(ctx context.Context) context.Context {
	return toolimages.VisionContext(ctx, e.Vision)
}
