# Direct execution and contract consistency plan

Date: 2026-11-11
Status: Approved; stages 1–4 implemented and verified.
Stages 5–6 remain planned.
Baseline: `267065c`.

## Goal

Make ordinary execution readable as a sequence of named operations with clear
state ownership. Reduce callbacks that hide orchestration or pair operations
that must stay together. Improve schema readability and behavioral consistency
without another package reorganization.

Use the [Readable Go skill](../../.agents/skills/go-readable/SKILL.md).
Rclone's useful reference is explicit contracts and reusable behavioral tests,
not its exact directory layout, configuration style, or compatibility machinery.

## First checkpoint

Schema construction now uses named input, retry, step, and root declarations.
A migration-only comparison checks decoded JSON against the previous schema;
the ordinary service/node validation tests retain the behavioral coverage.

Workspace change observation now has a concrete state owner with `Begin` and
`Observe`. Agent consumes that two-method contract without importing Workspace.
Roles constructs the concrete observer; normal finalization and budget closing
call it directly. The function-returning-function API is removed. Tests cover
repeated runs, cancellation, failed scans retaining the baseline, and observation
failures preventing successful Agent outcomes.

The first checkpoint was committed and pushed as `f6d32fa`.

Checkpoint verification passed: source tests (`./cmd/... ./internal/...`), race
tests for Workspace, Agent, roles, and Workflow service, source vet/build, and
all 44 dry eval probes. Schema migration comparisons passed for both empty and
populated registries; the temporary old-schema implementation was then removed.
The new observer regression tests also passed under the race detector after
the final baseline-reset adjustment. No live model requests were made.

## Scope and limits

Work in bounded stages across `internal/agent`, `internal/roles`, the directly
affected tool/workspace owners, Workflow schema construction, and their callers.
Update harness, CLI, TUI, RPC, and evaluation wiring when an internal API changes.

Preserve serialized history, recipe and tool schemas, event formats, permission
decisions, role restrictions, cancellation, resource limits, and lazy activation.
Do not invoke live model evaluations as part of the ordinary verification suite.
Do not retain old internal APIs through compatibility wrappers.

No new framework, generic host, event bus, dependency container, or catch-all
runtime package. Do not replace callback fields with function-bearing structs,
one interface per function, or a large interface that exposes all old internals.
Keep the current package layout unless an actual dependency conflict is found.

## Callback decisions

Prefer a direct method on the object that owns the state. Use a consumer-defined
interface only when the consumer genuinely supports alternative implementations
or must stay independent of the concrete feature package. The implementation
must own useful behavior, not just forward calls to function fields.

Prioritize these existing sites:

- `agent.Config.BeginChanges` and `runState.observeChanges`: paired lifecycle
  operations currently represented as a function returning a function.
- `PrepareTools`, `PreviewTools`, `SaveCapabilities`, `RestoreCapabilities`,
  `SaveBrowser`, and `RestoreBrowser`: related lifecycle operations installed
  separately, with preparation/restoration failures partly hidden in closures.
- `ToolContext`, `LimitOutput`, `Instructions`, `Resources`, and `Notifications`:
  supplied operations whose concrete owners already exist in tools/workspace.
- `Verify`: an execution operation distinct from observation of its result.
- `OnStep`, `OnResult`, `OnToolFinished`, streaming, usage, and progress hooks:
  genuine observers; they must not silently control execution or persistence.

Keep third-party callback APIs at their adapters. Do not change an established
permission Gate, Suspender, or Workflow node executor merely to reduce a count;
change it only if the reader's path actually improves. Avoid converting small
local closures, such as defer cleanup, into artificial types.

## Stage 1: Make schema construction readable

Start with `internal/workflow/service/schema.go`. Replace the dense JSON literal
and repeated nested map assertions with a readable schema representation and
named local values. A formatted embedded schema is acceptable if it improves
inspection; a general schema-builder DSL is not required.

Keep schema authority in the same domain owner. Preserve required fields,
defaults, limits, reference rules, additional-property behavior, and registered
node names. Compare decoded schema meaning, not property order or whitespace.
Use existing validation cases and add cases only for uncovered contracts.

Acceptance: a maintainer can find and edit a recipe field without following
unchecked chains of map assertions or changing another schema copy.

## Stage 2: Replace the change-observation closure with its state owner

Introduce a concrete observation object in `workspace/changes.go`. Construction
selects the workspace; `Begin(ctx)` captures the baseline and `Observe(ctx)`
reports subsequent changes. The object owns the inventory and digest cache.

Agent consumes a small change-observation interface with those two operations.
Workspace returns its concrete implementation; it does not import Agent.
Method return signatures must match exactly: do not rely on covariant interface
returns or create a shared contract package to make a factory method compile.

The normal path should be visible as:

```go
if err := observation.Begin(workCtx); err != nil {
    return err
}
// Run work, including failure/cancellation handling.
paths, err := observation.Observe(finalCtx)
```

This sketch illustrates ownership, not a replacement for the existing finalizer.
Keep baseline capture and final observation at their current lifecycle points.
Begin occurs per run, including Resume. Repeated observations advance the
baseline only after success. Final observation remains bounded and can complete
after work cancellation. Preserve ignored/tracked file handling, partial setup
failure behavior, and the prohibition on overlapping runs using the same state.

Migrate real callers and tests, then remove the function-returning-function API.
Do not expose filesystem inventory or move workspace access into the loop.

Acceptance: no stored observation closure, and tests still prove actual effects
and final observation after cancellation.

## Second checkpoint

Agent now receives the existing dynamic catalog through `Config.Catalog` and
the browser selection owner through `Config.Browser`. Static registries still
use only `Config.Tools`. Restore, prepare, preview, activation evidence, and
checkpoint persistence call those owners directly. No extra runtime model or
adapter package was introduced.

Planner, Interactive, and Subagent return composition errors at construction.
CLI inspection, sessions, eval execution, and delegated child construction handle
those errors before execution. Evals retain construction failures in `RunError`.
Capability loaders remain deferred until the existing activation/preparation
paths; source validation during composition does not connect integrations.

Regression coverage includes rejected role composition before activation/model
work, forbidden child tools rejected at construction, delegated construction
failure, eval failure recording, and restoration/persistence when eager
preparation fails. Existing browser, catalog, prompt-parity, and child tests
continue through the real role constructors.

Checkpoint verification passed: full source tests and race tests
(`./cmd/... ./internal/...`), source vet/build, and all 44 dry eval probes.
The final affected-package rerun passed after test setup cleanup. No live model
requests were made.

## Stage 3: Give catalog/browser lifecycle explicit ownership

Trace `roles.configureTools`, `tools.Capabilities`, browser selection, Agent
startup, schema preview, capability activation, and snapshot saving together.
Write the proposed construction and run sequence before changing signatures.

Use the existing catalog/browser objects as owners. Agent may consume a small
catalog lifecycle contract, but it must expose meaningful operations and hide
registration/restoration decisions. Do not create a parallel runtime model that
duplicates the catalog's registry or simply renames its callback bundle.

Propagate tool-composition errors explicitly from role construction rather than
capturing `composeErr` in preparation/restoration functions. Where necessary,
migrate role constructors to return errors and update all callers in the same
stage. Do not eagerly activate integrations to validate construction.

Preserve the current sequence: validate wiring, restore browser selection,
restore capabilities, establish the run, begin effect observation, restore the
checklist, then prepare eager tools. Preserve preview without activation,
Planner/Interactive prompt parity, child restrictions, activation evidence, and
snapshot ownership. Browser and capability state must still be saved at every
relevant checkpoint, not only after a successful run.

Before implementation, verify that static tool registries remain straightforward
and need no invented no-op lifecycle implementation. If the proposed boundary
cannot achieve this, narrow the stage rather than adding adapters.

Acceptance: ordinary role construction and startup show error propagation and
lifecycle ordering directly, with fewer independently installed operations.

## Stage 4: Replace remaining execution plumbing selectively

Trace each supplied operation listed above from its concrete owner to the loop.
Prefer direct calls where the owner is already available. Otherwise use a narrow
consumer contract only when it hides a coherent operation and retains the
dependency direction. Do not pass every service through one new interface.

Handle verification as execution with a measured result, not an observer.
Preserve one configured verification run, recorded output, failure evidence,
and the absence of an automatic extra model turn.

For result limiting/context preparation, preserve attachment grants, tool-call
identity, archive/output-store lifetime, and model-context limits. For resource
and instruction lookup, preserve authorization of resolved resources and both
ends of a move. Keep notification polling separate from mutation of history.

Acceptance: each changed operation is easier to follow from construction to its
call site; there is no replacement universal environment or forwarding layer.

## Third checkpoint

Workspace argument interpretation now lives in `tools.WorkspacePaths`, supplied
as `Config.Paths`. It keeps scoped instruction loading and canonical permission
resources together. The Workflow save tool supplies its fixed destination through
its own resource contract, removing the role-level exception.

`Config.Outputs` receives the existing OutputStore, which binds the run context
and retains oversized tool results. Adapters obtain that same store from context.
The unused tool-name argument to the adapter output limiter is removed.
`Config.Notifications` receives BackgroundProcesses directly; polling still
consumes session-local completion notices, and Agent alone appends their history.

Verification remains a supplied operation: configured command execution and
Workflow completion validation have different ownership and no paired lifecycle.
The Workflow check is now the named `authorValidation.verifySaved` operation
over its saved-recipe state. The small context-enrichment callback remains for
probed vision metadata; output lifetime is no longer hidden in that callback.
No generic execution host or function-wrapper type was added.

Regressions cover both move scopes, instruction deduplication and attachment
skipping, output-context binding before vision enrichment and deadlines, tool
call identity, and result limiting. Existing permission, Workflow authoring,
output paging/isolation, and completion-arrival tests retain their assertions.

Checkpoint verification passed: full source tests and race tests
(`./cmd/... ./internal/...`), source vet/build, all 44 dry eval probes, and the
final notification-arrival fixture rerun under the race detector. No live model
requests were made.

## Stage 5: Simplify observer wiring without changing delivery

Review genuine observers last, after execution operations are separated. Use an
explicit observer object where it replaces a repeated bundle across real CLI,
TUI, RPC, and child-observation consumers. Avoid making every consumer implement
unrelated methods, and avoid a second event model duplicating existing events.

Keep observer delivery synchronous/ordered where it is today. Preserve transient
tool output, execution-finished versus recorded-result ordering, streaming,
parent/child separation, payload ownership, and cancellation. Do not introduce
channels or background delivery merely to remove function fields.

Acceptance: observer adaptation lives at the frontend boundary, construction is
readable, and removing observation cannot change authorization or work outcomes.

## Stage 6: Share behavioral checks for real common contracts

Start with one repeated contract after the corresponding ownership change.
Prefer a small test helper used by at least two real implementations, with
explicit fixture setup. Do not invent production abstractions to share tests.

Candidates: tool schema/name validation and refusal before effects; catalog
restore/activation/preview consistency; normalized provider cancellation/error
handling where both protocols promise the same behavior. Keep protocol-specific
replay/streaming tests and feature-specific node behavior separate.

Do not impose retry safety, idempotence, read-only behavior, or identical output
shape on all nodes/tools. Test declared guarantees and their enforcement.

Acceptance: shared tests detect a violated common promise in multiple real
implementations. They do not merely check struct fields or file organization.

## Migration and verification

Complete one stage per reviewable change. Capture existing behavioral coverage
before moving APIs; preserve assertions and remove old internal paths directly.
Do not combine the observer change with catalog lifecycle or schema changes.

For each stage, run affected tests and build all callers. Use race checks for
observation, activation, cancellation, or delivery changes. Before final handoff:

```sh
go test ./cmd/... ./internal/...
go test -race ./cmd/... ./internal/...
go vet ./cmd/... ./internal/...
go build ./cmd/... ./internal/...
go run ./cmd/eval -dry
```

Use source-package paths because local ignored evaluation results can contain
saved Go inputs. Inspect CI requirements independently; passing local commands
does not establish live integration or model behavior.

Update affected package READMEs and architecture/convention rules with their
actual new contracts. In review, trace one normal run and one failed/cancelled
run from construction to final persistence. Compare the number of ownership
decisions the reader must hold at once; callback counts and line counts alone
are not acceptance criteria.
