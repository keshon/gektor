# Workflow runtime implementation plan

Date: 2026-11-13. Starting revision: `6948888`.
Branch: `codex/workflow-runtime-v4`.

## Start here: current handoff — 2026-11-16

Continue on `codex/workflow-runtime-v4`. This handoff accompanies the committed
error-route and finalization implementation. Callback waits (`c385ce0`),
operator recovery (`92bae29`), and this implementation are pushed together.
Older "next" paragraphs below are historical and superseded by this section.

### Completed and pushed

- Collections, SQLite invocation/attempt persistence, transactional streaming
  artifacts and the shared 100,000-invocation budget.
- Frozen runtime child scopes and serial mapped child calls, preserving reviews,
  ordered results and artifact ownership across continuation.
- `4efec98`: bounded child repeats. Explicit completion and next-input pointers,
  stable iteration identities, limits and review continuation without replay.
- `177661b`: durable deadline waits through `workflow_wait`. Waiting releases the
  execution lease; the worker resumes due runs under its current permissions.
  Root, child, mapped and repeated waits use the same persistence/claim path.
- `d958dcf`: MCP import accepts explicit `http`, `streamable-http` and `stdio`
  types. Unknown-field decode errors are no longer reported as a missing
  `mcpServers` object. Credential handling and transport validation remain intact.
- The branch includes the SSH interruption/redaction and native YouTube stream
  fallback fixes from `d4e0f79`, merged in `ef430bc`.
- `ba931e1`: Windows and Bash build launchers now delegate to `cmd/build`.
  Preserve this later change when continuing; do not restore the old scripts.

### Callback checkpoint — c385ce0

Authenticated callback waits now extend stage 4 through
`workflow_wait_callback`. Required arguments are a named channel and timeout of
1–604800000 ms. Inspection and run summaries expose a unique wait receipt with
its qualified step ID, channel and expiry. API config has separate `callbacks`
channels and distinct bearer credentials. Callback-only configuration is allowed.

`POST /callbacks/{channel}/{run_id}/{wait_id}` accepts an event ID and a bounded
JSON payload. API requests only persist; the existing worker claims due runs and
rechecks permissions. Acceptance and wake-up commit together. Identical event ID
and normalized JSON deduplicate while the run is retained, including after
consumption. Different events conflict. Early/wrong-target delivery is unavailable;
first delivery after cancellation, closure or expiry is rejected. No provider
registration or early-event buffering is implemented.

Receipts and accepted event identities live in `store/callbacks.go`, independently
of invocation subtree replacement. Snapshots cannot overwrite acceptance or push
its wake-up later. Limits: 32 KiB per normalized/submitted payload, 1 MiB retained
payload per run, 1,000 receipts per run. Payloads remain untrusted data. Timeout
without an accepted event fails before downstream work. HTTP acknowledgements do
not promise execution success or exactly-once external effects.

Tests cover root/child/map/repeat continuation, preceding reviews, revoked run,
wait and downstream permissions, expiry, cancellation before and after acceptance,
concurrent duplicate delivery, transactional registration/acceptance rollback,
stale snapshots, worker restart and accepted-event recovery after process exit.
The native worker and run lease remain the only executor path.

### Recovery checkpoint — 92bae29

`workflow recover -workspace <dir> -recovery recovery.json <run-id>` is an
operator-only continuation path for interrupted runs. It is absent from Agent
and HTTP adapters. The file supplies a nonblank note and exactly one `retry` or
verified-output `complete` decision per uncertain leaf invocation. Inspection
lists qualified uncertain IDs, including leaf collection items. Empty decisions
are allowed only when no effect is uncertain (for example, a stop between steps).

Recovery holds the existing run lease, validates frozen source/definition identity
and current contracts, and retains completed effects, scopes, reviews, artifacts,
reserved invocation slots and attempts. Decisions, timestamp, note and supplied
output digests commit before dispatch. Full outputs stay on their invocation.
Requests are at most 64 KiB; notes at most 2,000 bytes; histories at most 32 records.

Leaf collection execution now retains prior item records and ordered results;
it does not recreate all items on continuation. Only pending items dispatch, and
attempt numbers continue across explicit retries. A failure during continuation
must not mark an already completed later item skipped. Call binding and wait
bookkeeping are pure control work; accepted callbacks hydrate across an
interrupted consumption checkpoint and recheck permissions before recovery.

Known terminal failures/cancellations, actively leased runs and linked jobs with
pending cancellation are rejected. An unfinished scope containing an untyped
failed or cancelled leaf is outside this slice. Known typed failures remain
settled while pending error routes/finalizers continue; they are not retried.
This is not general failure retry or automatic replay. Linked jobs retain the
same run ID and use the existing joined cancellation monitor. A stale terminal
job record can be reconciled from its exclusively leased interrupted run before
operator continuation.

Separate-process tests stop after an effect but before result commit, then prove
root/child/map/repeat/leaf-collection recovery without replaying completed work.
Engine tests cover mixed decisions for multiple uncertain items, atomic decision
checkpoints, failed commits and a stop after decisions but before dispatch.
Service tests cover current policy, invalid decisions/output schemas, active
leases, linked jobs/cancellation and accepted callback consumption. The CLI test
completes an interrupted artifact write from its verified existing output.

### Current implementation slice: typed error routes and finalization

Recipes now accept `on_failure:"source-id"` on an error route and `finally:true`
on a finalizer. Graph validation adds the source edge and orders finalizers after
ordinary work/error routes. Routes target ordinary steps only; ordinary work
cannot depend on a finalizer. No separate executor or reference syntax was added.

Only explicit `ExecutionError` operation failures route. They persist typed
`failure:{code,message}` metadata; references to a failed source receive
`{error:{code,message}}`. The partial operation output remains separate in the
stored invocation. HTTP/script/callback-expiry paths have native codes; untyped
node errors still stop immediately. Collection item records and outcome arrays
retain typed failure metadata as well.

After a typed failure, remaining ordinary work is skipped; matching error routes
and finalizers may execute. The scope remains failed and reports its first
operation failure. Later failures remain inspectable. Output collection is
skipped on failed scopes. Permissions/contracts remain checked for every effect.
Cancellation, contract/permission errors and any checkpoint failure are immediate
stops. A run-local checkpoint guard prevents a joined operation/storage error
from being misclassified as routable. Finalizers are not process-exit cleanup.

Known failures survive review, deadline/callback waits and explicit operator
recovery. Restoring partial collections/calls/repeats aggregates retained typed
item failures without replay or erasing the failure at the parent boundary.
Existing exclusive leases, shared budgets and interrupted-effect decisions stay
in place. Successful `on_error:"collect"` containers still return item data and
do not activate a container failure route.

Tests cover success/error ordering, first-failure preservation, nested calls,
maps/repeats/collections, finalizer failure, denied routes/finalizers, output
contract failure, cancellation, joined storage failures, reviews/waits,
callback expiry and recovery between leaf and parent checkpoints. The shipped
`examples/workflows/error-routing` fixture runs through the real CLI and isolated
script worker, producing failure/finalization artifacts without a model.

Next: selective retry policy for explicit operation codes, without automatically
retrying uncertain effects or bypassing a node's RetrySafe contract. Broader
native-node failure classification and authoring UI support remain explicit
follow-ups, not implied by this slice. Named connections and credentials are
stage 5; richer HTTP operations are stage 6. Concurrent jobs, concurrent child
calls and their shared scheduler remain later work. Keyed joins and the broader
node inventory are also unfinished.

### Constraints to preserve

- Recipe format 4 and one current store/executor contract. No old-format loaders,
  conversion machinery or compatibility aliases. Do not delete personal data.
- Read `.agents/skills/go-style/SKILL.md` and
  `.agents/skills/go-architecture/SKILL.md` before Go edits. Keep ownership explicit,
  ordinary flow linear and logical paragraphs separated. Do not add a framework
  or split cohesive code simply to reduce file length.
- There is one active execution path per run. Child calls are serial; only leaf
  collections have bounded parallel workers. Do not multiply worker pools.
- `repeat.delay_ms` is a short in-process delay. `workflow_wait` is the durable
  pause. Neither mechanism supplies automatic recovery for interrupted effects.
- Deadline waits are limited to seven days and 1,000 waiting runs per store.
  Queued jobs take priority over due waits. A deadline makes a run eligible;
  it does not guarantee exact execution time.
- Manual waits are discovered by the worker without creating a queue job.
  Existing wait cancellation is job-based; do not assume every waiting run has
  a job ID. The CLI run timeout is not persisted across worker continuations.
- Callback delivery is channel-scoped and targets one registered receipt. Tokens
  are separate from submission/operator credentials. Do not add early buffering
  or infer provider-specific registration from this endpoint.
- n8n screenshots are useful for later node/editor coverage, not required for
  this runtime slice. The checklist below is an inventory, not a parity claim.

### Where to continue reading

| Responsibility | Files |
| --- | --- |
| Execution and pause/resume | [engine.go](../../internal/workflow/engine/engine.go), [recovery.go](../../internal/workflow/engine/recovery.go), [scope.go](../../internal/workflow/engine/scope.go), [callmap.go](../../internal/workflow/engine/callmap.go), [repeat.go](../../internal/workflow/engine/repeat.go) |
| Durable state and claims | [callbacks.go](../../internal/workflow/store/callbacks.go), [waits.go](../../internal/workflow/store/waits.go), [runs.go](../../internal/workflow/store/runs.go), [jobs.go](../../internal/workflow/store/jobs.go), [sqlite.go](../../internal/workflow/store/sqlite.go) |
| Continuation and authorization | [recovery.go](../../internal/workflow/service/recovery.go), [waits.go](../../internal/workflow/service/waits.go), [reviews.go](../../internal/workflow/service/reviews.go), [jobs.go](../../internal/workflow/service/jobs.go) |
| Worker and HTTP ingress | [daemon.go](../../internal/workflow/daemon/daemon.go), [api.go](../../internal/workflow/api/api.go), [callbacks.go](../../internal/workflow/api/callbacks.go), [server.go](../../internal/workflow/api/server.go) |
| Wait operation contract | [workflow.go](../../internal/workflow/nodes/workflow/workflow.go), [run.go](../../internal/workflow/run.go), [handler.go](../../internal/workflow/service/handler.go) |

The `waits_test.go` files beside engine/service/store/daemon cover deadline
behavior, nested continuation, process exit, leases, rollback and cancellation.
[workflow_wait_test.go](../../cmd/gektor/workflow_wait_test.go) runs the shipped
fixture through the CLI and continuation service. The recipe contracts live in
[the reference](../reference/recipes.md); runnable fixtures are
[repeated child calls](../../examples/workflows/repeated-subworkflow/README.md)
[durable waits](../../examples/workflows/durable-wait/README.md) and
[error routes](../../examples/workflows/error-routing/README.md).

### Verification checkpoint and restart commands

For error routes/finalization, the full source-scoped test suite, affected runtime
race checks, source-scoped vet, a CGO-free build, the 45-probe eval dry-run and
documentation checks passed. The real CLI executes the shipped error-routing
fixture with the isolated JavaScript worker and no model. Review/wait/recovery,
nested collections and joined operation/checkpoint failures have deterministic
coverage. Documentation checks now cover 68 files and 35 published articles.
No model-backed authoring or remote-provider reliability claim follows from these
checks. The full CLI race-suite limitation described below remains separate.

For operator recovery, the full source-scoped suite, focused CLI recovery tests,
source-scoped vet, CGO-free build, 45-probe eval dry-run and documentation checks
passed. Runtime engine/service/store/worker race checks passed, as did the focused
CLI recovery/deadline race run. The unrestricted CLI race suite is not green:
`TestMappedSubworkflowExampleNeedsNoModel`,
`TestChildScopeRunsCollectionReportWithoutModel` and
`TestCollectionReportExampleRunsWithoutBackend` hit the Windows 256 MiB worker
process memory cap in instrumented JavaScript subprocesses. Native tests of those
fixtures pass; no sandbox limit was changed to make race checks pass. Review the
worker/instrumentation budget separately before claiming the full CLI race suite.
Separate-process recovery tests and injected SQLite checkpoint failures prove
explicit decisions persist before effects and completed work is retained. They
do not prove remote exactly-once execution or provider integrations.

For the callback slice, the full source-scoped test suite and affected
store/service/API/worker/engine race checks passed, along with source-scoped vet,
a CGO-free build, the 45-probe eval dry-run and documentation checks (67 files,
35 published articles). These are deterministic runtime checks,
not model-backed callback authoring, provider integration or physical power-loss
tests. Resume verification with:

```text
git status --short
go test -p 1 ./cmd/... ./internal/...
go test -race ./internal/workflow/... ./internal/tui/workflow ./cmd/gektor
go vet ./cmd/... ./internal/...
go run ./cmd/eval -dry
node website/scripts/check-docs.mjs
git diff --check
```

`build.cmd help` or `bash build.sh help` describes the current shared build
commands. Source scoping avoids archived Go evidence under `eval/results` with
obsolete imports; do not rewrite or delete that evidence to make an unrestricted
`go test ./...` pass. Keep model/network evals separate from deterministic tests.

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
  callback waits, richer routing and data joins remain in the delivery sequence.
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
  file-based metadata is not imported or used as a fallback. Deadline wake-ups
  now have their own indexed table in the control-flow implementation below.
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
  cancellable but occupy the active slot; deadline waits are implemented separately.
  Concurrent child calls require a shared scheduler rather than independent pools.

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


## Handoff after durable deadline waits

`workflow_wait` records a bounded relative deadline once and releases execution.
The run/step use `waiting`, and inspection/summaries expose `resume_at`. Waits
propagate through ordinary, mapped and repeated child scopes. Review boundaries
remain distinct. Deadline waits do not use per-run sleeping goroutines.

SQLite stores one indexed wake-up per waiting run, in the same transaction as
its checkpoint; a failed write rolls both back. The existing run lease excludes
simultaneous claims. Resumption under the worker's current policy clears the
wake-up before active execution. A committed waiting run survives process exit;
a run interrupted after continuation begins is not replayed automatically.
The worker checks due waits after queued jobs. Manual waits are discovered too,
without creating a second job for the run. At most 1,000 runs may wait per store.

Queue cancellation makes a waiting job due immediately and records cancellation
without downstream effects. Waiting jobs are not evicted or ignored by schedule
overlap checks. Artifacts survive waiting. The wait itself rejects maps/retries;
put it inside a child recipe for collection processing. CLI guidance and terminal
cards distinguish timed waiting from operator review.

The runnable fixture is `examples/workflows/durable-wait/recipe.json`.
Next: authenticated callback waits and duplicate-event handling, explicit recovery
of interrupted effects, typed error routes/finalization and selective retry policy.
Current deadlines are not a callback mechanism or an exactly-once guarantee for
remote effects. Concurrent jobs and shared child scheduling remain later work.


Deadline-wait verification passed the source-scoped suite, vet and CGO-free build,
plus workflow engine/service/store/daemon/editor/CLI race checks. The 45-probe
eval dry-run and documentation/diff checks passed. Tests cover root, child, mapped
and repeated waits; review-to-wait continuation; frozen completed work; changed
permissions; cancellation including the running-to-waiting race; two-claim
exclusion; early/duplicate continuation; failed wait publication and SQLite
rollback. A helper process exits without cleanup in waiting and running states:
only the committed wait is claimable afterward. Worker restart tests preserve
prior effects and allow other queued work while a run waits. The shipped CLI
fixture retains exactly one artifact before and one after the deadline. These
are deterministic runtime checks, not model-backed authoring or power-loss tests.
