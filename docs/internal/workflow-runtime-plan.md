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
  terminal. Artifact publication is serialized per run until transactional
  reservations replace the current quota scan.
- Invocation persistence: started with streaming artifact publication. Payloads
  are copied through bounded buffers while hashing; MIME detection retains only
  the prefix. Failed or cancelled publication removes its temporary directory.
  Quota scans and the run publication lock remain until transactional
  reservations replace them. SQLite run/invocation/attempt storage, incremental
  checkpoints and queue transactions are still pending. Changing the database
  alone would leave whole-run checkpoint serialization quadratic for maps.
- Later stages: pending.

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
