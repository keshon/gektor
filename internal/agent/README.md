# Working on Agent execution

`agent` owns the model/tool execution loop, its public contracts, and runtime
state. It does not assemble concrete tools or select providers. Task roles are
constructed in `internal/roles`; the isolated archive reviewer is constructed
in `internal/retrospective`. Frontends coordinate sessions through these owners.

## Reading path

1. Start with `config.go` and `agent.go`: construction, `Run`, `Resume`, and
   `ResumeWithAnswer`. `report.go` defines the observed outcome exposed to callers.
2. Follow `run.go` for the ordinary model/tool cycle. `run_lifecycle.go` owns
   preparation/finalization and per-run state; `budget.go` handles deadlines.
3. For tools, read the contracts in `tool.go`, then `registry.go` for registration,
   schemas, and invocation. `tool_policy.go`, `tool_validation.go`,
   `tool_execution.go`, and `tool_results.go` cover admission, execution ordering,
   and recording. `dispatch.go` handles calls through the capability dispatcher.
4. For model context, read `context.go`, `context_reuse.go`, and `capabilities.go`.
   `compact.go` decides when to compact and bounds response tokens;
   `compact_history.go` selects complete history groups; `compact_facts.go`
   carries extractive facts forward. `checkpoint.go` handles model-produced
   checkpoints, and `compact_file.go` provides explicit saved-file compaction.
5. For persistence and continuation, read `state.go`, `questions.go`, `inbox.go`,
   and `suspend.go`. `conversation.Snapshot` owns archive/context representation;
   this package coordinates it with runtime state and pending tool calls.
6. For completion and recovery, follow `finish.go`, `repeat.go`, and the error
   paths in `run.go`. Verification and measured effects determine the outcome;
   a model's final text is not itself evidence of successful work.

## Contracts to preserve

- Preserve the order of policy checks, argument validation, effects, result
  recording, and persistence. A refused or malformed call still needs a paired,
  recordable tool result without executing its operation.
- Concurrent read calls may overlap; exclusive calls separate those batches and
  remain in model order. Results must retain their original call association.
- Working context may be shortened or compacted; the full execution archive
  remains evidence. Do not split tool calls from their results or lose standing
  user instructions, steering, images, or carried facts during continuation.
- Token projection, response headroom, compaction thresholds/cooldowns, and
  overflow recovery work together. Trace the actual outgoing request before
  changing one of these limits.
- Cancellation and budget exhaustion retain accepted input and observed effects.
  Partial model responses and failed tool calls must not become false success.
- `Config.Changes` supplies a stateful change observer. Agent calls `Begin` at
  run preparation and `Observe` during finalization and budget closing. The
  observer owns its baseline and must support bounded scans after cancellation.
- A static registry needs only `Config.Tools`. Dynamic catalogs additionally
  supply `Config.Catalog`: the existing catalog owns activation and saved state.
  Startup restores browser selection, restores capabilities, establishes the run,
  then prepares eager tools. Preview must not activate or connect integrations.
  `Config.Browser` owns selection only; persistence captures both owners at
  checkpoints, including failed runs.
- `Config.Paths` supplies workspace argument interpretation. Tool-specific
  resources win; the fallback must authorize both ends of a move. Scoped
  instructions arrive before a mutation is admitted.
- `Config.Outputs` binds the same store to tool execution and result retention.
  Bind it before deriving run deadlines and preserve attachment grants and call
  identity. `Config.Notifications` polls an existing background owner; Agent
  limits the returned text and records it in history.
- `Verify` is one supplied execution operation. Its measured result is recorded
  after the final answer, including failure output, without another model turn.
  `ToolContext` remains a small adapter for context metadata such as probed vision.
  Neither operation needs a function-wrapping interface.
- Role restrictions apply at execution, including dynamically activated tools.
  Keep concrete tool decisions outside the loop and use its supplied contracts.
- Keep related runtime state private. File organization provides reading paths;
  it does not require subpackages, exported internals, or forwarding layers.

## Tests and verification

Tests follow behavior: registry/validation, execution ordering, completion,
reports, observers, state/resume, questions, and compaction. Shared model/tool
stubs live in `test_helpers_test.go`; compaction fixtures live in
`compact_helpers_test.go`. Prefer existing behavioral regressions over tests
that assert filenames or implementation shape.

From the repository root:

```sh
go test ./internal/agent
go test -race ./internal/agent
go test ./cmd/... ./internal/...
```

Changes to public contracts also require checking roles, tools, harness, and
frontend callers. Follow the
[Readable Go skill](../../.agents/skills/go-readable/SKILL.md).
