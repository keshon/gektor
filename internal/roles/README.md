# Working on agent roles

`roles` assembles task-capable agents from the execution loop in `internal/agent`
and concrete tools. Frontends and evaluations use these constructors so tool
availability, instructions, permissions, and execution limits agree.

## Reading path

- `env.go`: shared launch inputs, observer hooks, permission defaults, model-fact
  refresh, and the final changed-file sweep.
- `config.go`: common Agent configuration, prompt sections, scoped instructions,
  resource authorization, output handling, and transcript paths.
  It constructs a Workspace change observer for the Agent; the loop starts and
  samples that observer through its small consumer contract. Workspace argument
  lookup lives in `tools.WorkspacePaths`; output retention and background polling
  use the existing OutputStore and BackgroundProcesses owners.
- `conversation.go`: Interactive and Planner construction, questions, delegation,
  verification, and mode-specific directives.
- `subagent.go` and `child.go`: delegated execution limits, parent-input isolation,
  child archives, and separately routed observer events.
- `tools.go`: capability catalog composition, activation, restoration, and browser
  availability. Tool implementations remain in their feature packages.
- `workflow.go`: restricted Workflow authoring and validation of the selected
  recipe before accepting the author's result. `authorValidation.verifySaved`
  repeats validation at completion; the save tool owns its permission destination.

## Contracts to preserve

- Task constructors return `(*agent.Agent, error)`. Handle tool-composition
  errors before starting or inspecting an agent. Construction and preview do not
  activate integrations. The composed catalog and browser selection are passed
  directly to Agent through its consumer contracts; they retain their own state.
- Planner and Interactive share the conversation prompt/tool prefix. Planner's
  execution allowlist enforces read-only behavior; instructions alone do not.
- Subagents cannot delegate or ask the parent's operator. Their budget and inbox
  are independent of the parent, and their activity is not parent evidence.
  `ChildEvents` receives child envelopes through `events.Sink`; `childObserver`
  binds the parent call ID without invoking parent stream/usage hooks. Child
  archives provide a separate evidence route.
- Permission policy, context-aware gates, resource extraction, and changed-file
  checks must reach the actual composed tools, including activated capabilities.
- Model facts are refreshed for a run; children inherit the snapshot without
  probing independently. Preserve caller-owned service and observer lifetimes.
- Keep construction here, execution mechanics in `agent`, and feature behavior
  in the tool or capability that owns it. The isolated archived-session reviewer
  is constructed by `retrospective`, not through this task environment.

## Tests

Conversation, prompt parity, policy, delegation, and catalog tests exercise the
real constructors. Shared fake model clients live in `test_helpers_test.go`.
Composition tests belong here when they verify the assembled role rather than
an individual tool implementation. Live extension tests remain separate.

From the repository root, run `go test ./internal/roles`; use
`go test -race ./internal/roles` for observer, delegation, or lifetime changes.
Follow the [Readable Go skill](../../.agents/skills/go-readable-keshon/SKILL.md).
