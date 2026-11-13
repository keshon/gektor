# Workflow runtime implementation plan

Date: 2026-11-13. Starting revision: `6948888`.
Branch: `codex/workflow-runtime-v4`.

## Scope and decisions

Build a local workflow runtime with collection processing, reusable child
workflows, explicit failures, durable waits, credentials and inspectable runs.
AI authors and repairs recipes; execution does not require a model unless a
recipe explicitly calls one.

The sole consumer approved breaking changes. There is one current recipe format,
executor and store contract. Do not add legacy executors, recipe converters,
old-history readers or compatibility aliases. Existing personal data is not
deleted by this work. Update shipped examples, authoring instructions and tests
when a contract changes.

Keep feature services and shared operation contracts. The workflow service owns
authorization and coordination; the engine owns scheduling; the store owns
persistence. Agent, CLI, API and TUI remain consumers of those owners. Go is an
implementation choice, not evidence of performance superiority.

## Delivery sequence

1. **Collection execution.** Add bounded parallel item execution with one
   coordinator owning checkpoints. Keep results in input order. Explicitly
   declare concurrent-safe nodes independently of retry safety and read-only
   status. Collect execution failures only when requested; authorization,
   validation, output-contract and persistence failures stop dispatch. Cancel
   and join in-flight work before returning. Publish the same policies to
   schema discovery and the editor. Test cancellation, failed persistence,
   permission denial, ordering and a 500-item workload.
2. **Invocation persistence.** Replace whole-run JSON checkpoints and the queue
   control document with SQLite execution metadata inside `workflow/store`.
   Store run, invocation, attempt, review, wake-up and claim identities. Keep
   artifact bytes outside the database. Stream artifact publication, reserve
   quotas transactionally and reconcile abandoned files. Do not migrate old
   data. Tests kill helper processes at commit/publication boundaries and
   measure 100, 1,000 and 10,000-item persistence cost.
3. **Runtime composition.** Stop flattening all child invocations into a static
   graph. Freeze definitions once, instantiate child scopes at runtime, map a
   child recipe over a collection and share execution budgets across nesting.
   Add explicit bounded repeat state for pagination and polling. Specify empty
   collections, null/missing outputs, skipped branches and keyed joins. Tests
   cover nested maps, duplicate input values and generated-work limits.
4. **Recovery and control flow.** Persist waits and release execution slots.
   Resume deadlines, callbacks and operator reviews with duplicate-event
   protection. Introduce typed failures, error routes, finalization, bounded
   backoff and selective retries. Distinguish replay-safe work from unknown
   external effects. An interrupted SSH command is never blindly repeated.
   Test service restart, callback duplication and lost external responses.
5. **Connections and credentials.** Extend `internal/credentials` with named,
   typed metadata, revisions, revocation and execution-time resolution. Retain
   protocol ownership in SSH, HTTP and MCP. Freeze non-secret destinations;
   resolve current secrets immediately before use and reauthorize changed
   bindings. Keep secrets out of arguments, snapshots and supported operation
   diagnostics. Address Unix protection explicitly; existing owner-only files
   are not encryption. Add OAuth refresh serialization when integrating OAuth.
6. **HTTP and artifacts.** Add explicit response modes, query/body formats,
   authorized bounded redirects, response headers and artifact-backed bodies.
   Check every redirect destination and never forward credentials across
   origins implicitly. Keep rate-limit orchestration visible in the workflow.
   Test soft 404s, 429s, redirects, large/binary bodies and cancellation.
7. **Authoring and operations.** Move reusable proposal/validation work from
   the TUI into a headless workflow owner. Add structured diagnostics,
   invocation/attempt inspection, captured-input tests and an explicit fixture
   mode. Add map/loop/error controls and named connections to the editor.
   External workflow operations require explicit schemas, resources, lifecycle
   and execution policy; activated Agent tools do not become nodes implicitly.
8. **Service throughput.** Add bounded concurrent jobs, workspace-wide resource
   budgets, retention and health/queue metrics. Multi-host workers, multi-user
   administration, broad connector coverage and a browser editor are separate
   product milestones, not prerequisites for a useful local runtime.

## Reference checklist from the supplied n8n screenshots

Use the screenshots as a capability inventory, not an execution specification.
The remaining work must define item identity, ordering, missing/null values,
branch readiness, merge keys and restart behavior before exposing palette entries.

- Flow controls: filter, if/switch, batches/repeat, merge, child workflows,
  stop/error and durable wait. Child mapping and bounded repeats are available;
  durable waits, richer routing and data joins remain in the delivery sequence.
- Data operations: set/rename fields, limit, deduplicate, split, aggregate,
  summarize, compare datasets and sort. Add small deterministic nodes with explicit
  schemas and size limits; scripts remain available for custom transformations.
- Formats/utilities: date/time, compression, file conversion/extraction, crypto,
  images, HTML, Markdown and XML. Reuse existing operation owners where present;
  prioritize workflow semantics and connections before broad converter coverage.

## Acceptance recipes

- Profile scan: 500 site rules, mixed outcomes, bounded concurrency and a report
  accounting for every input. No model or specialist scanner hides orchestration.
- API import: pagination, 429 delays, restart and deduplicated writes.
- Document batch: valid and invalid inputs, bounded storage/memory and aggregation.
- SSH maintenance: named connection, host verification, review and unknown outcome.
- Callback job: persistent wait, restart and duplicate authenticated callbacks.

Use deterministic local services and fixture transports first. Run model-backed
authoring evals separately after the corresponding deterministic recipe works.
Measure actual peak concurrency, cancellation, checkpoint I/O and memory rather
than claiming performance from implementation language.

## Verification and progress

Follow `docs/development/testing.md`. Run focused workflow/TUI checks for each
slice, race checks for changed lifecycle code, then the configured repository
checks. No deployment, live credential use, commit or push is implied.

- Branch and implementation plan created.
- Collection execution: complete. Format 4 is the only executable recipe format;
  shipped examples, authoring instructions and fixtures use it. Maps expose
  concurrency and failure collection in the editor and operation catalog.
  Script and HTTP nodes explicitly classify collectable operation failures.
  Unclassified errors, secondary authorization and storage failures remain
  terminal. Artifact writers share transactional reservations rather than a
  lock covering the whole stream.
- Invocation persistence: runs, invocations and attempts now occupy separate
  SQLite rows through `modernc.org/sqlite`, with WAL mode, full synchronization,
  foreign keys, a bounded busy timeout and one pooled connection per store
  handle. Queue and schedule mutations share an immediate transaction and write
  only changed rows. They still inspect bounded in-memory collections (1,000
  jobs and 100 schedules); indexed queue queries remain a throughput improvement.
  Collection checkpoints identify the changed step/item. Static recipe metadata
  and unrelated invocations are not serialized for each item. Initialization,
  finalization and explicit review snapshots can save the complete record.
  Inspection uses `database_path` plus `run_id`; JSON run/queue files are not
  read or imported. Process leases continue to distinguish active effects from
  interrupted work, without automatic replay.
- Artifact persistence: complete for the current runtime. Metadata and count/byte
  reservations live in SQLite. A producer reserves up to 16 MiB before streaming,
  syncs and closes its payload file, then publishes metadata and actual size in
  one update. Unused bytes are released at publication. Waiters use cancellable
  notifications when in-flight reservations temporarily consume capacity.
  Payloads stream concurrently through bounded buffers; the full-size reservation
  policy can admit at most 16 such streams under the 256 MiB run budget.
  Only published rows appear in reads/listings. Cleanup checks committed state
  before removing bytes, preserving an artifact if a failed call actually
  committed. Cleanup failures stop new writes on that writer until recovery.
  Starting execution and the daemon reconcile unfinished publications under each
  run lease; active producers are skipped. Retention records deletion intent
  before removing files, allowing restart to finish the operation. Existing
  file-based metadata is not imported or used as a fallback. Durable workflow
  wakeups belong to the later control-flow stage rather than unused tables.
- Generated-work bound: the current executor now reserves at most 100,000
  invocation slots across recipe steps and collection items. Admission checks
  the complete next array before allocating its records or starting workers.
  Accounting comes from the persisted invocation tree, so review continuation
  retains earlier reservations, including failed/skipped items. Retries reuse
  their invocation slot and retain the separate attempt limit. This closes the
  per-run multiplication of per-collection limits. Nested scopes consume the
  same bound; workspace-wide concurrency remains a separate milestone.
- Runtime composition: ordinary calls now instantiate frozen child scopes during
  execution. Local input/result namespaces stay intact, and runtime qualification
  supplies permission/artifact/inspection IDs. Static graph flattening, binding
  nodes and compiler gates were removed. SQLite stores scope headers separately
  from invocations; nested checkpoints update one target and ancestor statuses
  atomically without rewriting frozen scope snapshots. Reviews find the actual
  child operation; continuation retains completed work and rechecks each entered
  call's permission. Rejecting review cancels paused scopes. Skipped calls do not
  instantiate children. Child scopes share the run invocation budget. Calls remain
  serial, so nested leaf collections cannot multiply worker pools.
- Mapped child calls: implemented with one fixed destination and an array of child
  input objects. The coordinator executes calls serially, retains ordered outcomes
  and shares invocation/artifact budgets. A child review pauses its item; approval
  preserves completed and collected-failure items, and rejection cancels remaining
  items and scopes. Only typed operation failures are collectable. Boundaries use
  IDs such as `batch[0]/save`, including through nested maps. Calls do not retry.
- Bounded repeats: implemented for serial frozen workflow calls. Each iteration
  persists a child scope, reads an explicit boolean completion pointer and uses
  an explicit next-input object. Limit exhaustion is terminal. Short delays are
  cancellable but occupy the active slot; durable waits remain pending. Concurrent
  child calls require a shared scheduler rather than independent worker pools.

Verified the model-free collection example through the CLI service: two
successes, one retained failure and ordered values `[4, 6]`. Deterministic
engine tests account for 500 mixed results and cover bounded overlap,
cancellation/joining, checkpoint failures before and after effects, retries,
empty collections and terminal control failures.

Passed `go test -p 1 ./cmd/... ./internal/...` (98 test packages), workflow,
subprocess and editor race tests, source-scoped vet, eval dry-run (45 probes),
documentation links and `git diff --check`. The unrestricted `go test ./...`
also discovers archived source under `eval/results` with obsolete imports;
those evidence files were not changed. A parallel source test run encountered
a temporary Git fixture access-denied error; the complete serial run passed.

The streaming artifact slice passed workflow package tests, store race tests,
store vet, documentation links and diff checks. New cases cover exact empty and
16 MiB payloads, one-byte overflow, bounded read buffers, byte/hash/MIME integrity,
source interruption, cancellation and temporary-file cleanup.

SQLite checks cover a 1,000-item run whose item checkpoint inserts exactly one
invocation, with a trigger rejecting static snapshot rewrites. An injected SQL
failure verifies rollback of run status and invocation changes together. A child
process exits without closing SQLite or releasing its run lease; inspection
retains the acknowledged item and reports the run as interrupted.

The persistence benchmark (`BenchmarkInvocationCheckpoint`, one iteration each)
measured approximately 98.5 ms for 100 items, 946.8 ms for 1,000 and 18.26 s for
10,000 on Go 1.26.11, Windows/amd64, Intel i5-9600KF. Each case saves initialization, one
completed checkpoint per item and finalization. The race suite ran concurrently;
these are development observations, not isolated throughput measurements or a
comparison with datastore. The SQL trigger test establishes checkpoint scope;
timings do not establish linear wall-clock scaling.

SQLite verification passed: source-scoped tests and vet, workflow/editor race
tests, eval dry-run (45 probes), documentation links and diff checks. A build of
`./cmd/... ./internal/...` and store tests also passed with `CGO_ENABLED=0`.
Read transactions preserve a snapshot while another handle checkpoints; the
connection configuration test checks WAL, full synchronization and foreign keys.
Unrestricted module tidying encounters the same obsolete imports in archived
eval snapshots as the unrestricted tests. `go mod tidy -e` updated dependency
metadata while reporting those unresolved archive imports; archives were not
changed.

Artifact publication tests cover simultaneous producers, visibility before
publication, count and byte limits, exact fit and zero-byte payloads, cancelled
quota waits, ignored/failed SQL publication, failed cleanup and recovery, and
preservation of committed publications. Helper processes exit without cleanup
after reservation, during streaming, after file sync, after publication, after
retention intent and after payload deletion. Recovery reclaims unfinished
files/reservations and preserves
published bytes. These checks exercise process termination, not physical power
loss. Payload files are synced before SQLite publication; directory metadata
durability on Windows has not been independently established by this suite.

This slice passed the source-scoped suite and vet, workflow/editor race checks,
store tests and the source build with `CGO_ENABLED=0`, the 45-probe eval dry-run,
documentation links and diff checks.

The run-budget checks cover exact fit, exhaustion before worker dispatch,
an empty collection at the limit, retained failed/skipped slots, retry reuse,
and review continuation from a serialized checkpoint. Source-scoped tests and
vet, engine race tests, the 45-probe eval dry-run and diff checks passed.

Runtime scope checks cover frozen nested input/output namespaces, call-entry and
child reviews, rejection, revoked call permissions, and completed artifacts that
are not replayed. The terminal review form skips approved enclosing boundaries;
agent step inspection selects qualified child IDs. A child invocation of the
model-free collection report retains duplicate inputs and returns three successes,
one collected failure and values `[4, 4, 6]`. Engine tests check shared invocation
capacity, checkpoint failure before child effects and joined workers on cancellation.
SQLite triggers establish that a nested item checkpoint inserts one invocation
without rewriting frozen run/scope headers; injected failures roll back ancestor
statuses and the target together. Numeric argument text survives status updates.
The process-exit lease test covers both root and nested acknowledged items.

Scope verification passed source-scoped tests and vet, workflow/editor and CLI
race tests, the 45-probe eval dry-run and diff checks. Store tests and the source
build passed with `CGO_ENABLED=0`; nested checkpoint/process-exit tests also passed
in that configuration.

## Handoff after mapped child calls

Mapped calls now use `args.recipe_path` as a fixed destination and `for_each`
as child input objects. The serial coordinator preserves indexed identity,
ordered outcomes and the shared invocation budget. It checkpoints child scopes
inside item records, resumes pending reviews without replaying completed items,
and cancels pending items on rejection or terminal failure. Only typed operation
failures may be collected. Concurrent child calls and call retries are rejected.

The runnable example is `examples/workflows/mapped-subworkflow/recipe.json`.
It returns three successes, one retained failure and `[4, 4, 6]`. Tests cover
500 child scopes, nested mapped reviews, frozen dependencies, duplicate inputs,
rejection, array admission, exhaustion after earlier effects, cancellation,
checkpoint faults, output bounds and terminal permission/validation failures.
The terminal review form and inspection use qualified IDs such as
`batch[0]/inner[0]/save`.

The bounded repeat slice below follows this checkpoint. Next implementation:
durable waits and recovery/control-flow work. Concurrent child calls still need
a shared scheduler. Named connections/credentials remain a separate planned
milestone. The screenshot checklist above records the broader flow, data and
format capabilities; those entries are not claims of implemented parity.


## Handoff after bounded repeats

`workflow_call` now supports `repeat` with required `max_iterations`, `until`
and `next_inputs` fields, plus optional `delay_ms`. Completion must be a boolean;
next inputs must be an object and replace, rather than merge with, prior inputs.
True completion returns the final child output without reading next inputs.
Missing/null control values fail. False completion at the iteration limit fails.
Repeats cannot combine with maps, concurrency, failure collection or call retries.

The coordinator checkpoints each new iteration before effects and each outcome
before reading control fields. Stable indexed identities reuse the invocation,
child-scope and artifact persistence contracts. Completed iterations are not
replayed on review continuation. New calls reauthorize their destination and
operations; next inputs are type-checked and cannot inject structured references.
Short delays (at most ten seconds) are cancellable and occupy the current slot.
They are not durable waits or restart recovery. Unknown external effects still
require explicit intervention, never automatic replay.

The runnable fixture is `examples/workflows/repeated-subworkflow/recipe.json`.
It produces three artifacts and returns the third page. The editor preserves and
edits repeat policy; graph details and inspection distinguish its child scopes.
CLI approval guidance now selects the qualified pending review instead of an
ancestor call marked waiting by its descendant.

Next: durable waits, releasing worker capacity and defining deduplicated wake-up
and recovery transitions. Concurrent child calls, keyed joins and broader error
routing remain separate work. Do not reinterpret an interrupted operation as a
safe retry when adding continuation.


Bounded-repeat verification passed `go test -p 1 ./cmd/... ./internal/...`,
source-scoped vet, engine/service/store/editor/CLI race tests and a source build
with `CGO_ENABLED=0`. The eval dry-run selected 45 valid probes. Documentation
links and diff checks passed. Deterministic cases cover 500 iterations, nested
reviews, serialized continuation, revocation, invalid next inputs and reference
injection, control-value errors, cancellation and budget/checkpoint failures.
The shipped example passed through the CLI service with counts three and five;
SQLite-backed review approval/rejection retained the expected artifact counts
and frozen child definitions. No model-backed authoring eval or physical
power-loss test was performed for this slice.
