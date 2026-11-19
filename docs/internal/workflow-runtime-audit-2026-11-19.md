# Workflow runtime audit and local acceptance

Date: 2026-11-19. Branch: `codex/workflow-runtime-v4`.
Baseline: `8cae30e`, plus the checkpoint-failure fix committed with this report.
This is dated audit evidence; maintained contracts remain in
[the architecture](../../ARCHITECTURE.md) and
[recipe reference](../reference/recipes.md).

## Result and scope

Stages 1–8 of the [implementation plan](workflow-runtime-plan.md) have local
acceptance coverage. One correctness defect found during this audit is fixed:
a failed run checkpoint could be treated as an ordinary job failure, allowing an
unsaved completion to be reported as successful while the worker dispatched more
jobs. The fix keeps the last durable record available for explicit recovery.

The review followed execution, persistence and ownership paths through the engine,
service, store, worker, recipe compiler, authoring, CLI/API and TUI consumers.
It checked cancellation and leases, frozen sources, permission and credential
revalidation, nested/map/repeat scopes, reviews, waits, recovery, retries, fixtures,
artifact budgets and retention against their existing tests. HTTP credentials and
redirect authorization were inspected as representative external boundaries;
existing SSH contract tests were included in the source-wide run.

This was a targeted architecture review, not a line-by-line review of every node
adapter. External providers, live-model proposal quality and production throughput
were not evaluated. A subsequent independent Astra storage review found the
three defects recorded below; its fixes passed the full store race suite and vet.

## Independent storage review

- Queue pressure could evict an interrupted job and its submission deduplication
  key. Eviction now selects only completed, failed or cancelled jobs; a queue
  full of recoverable jobs rejects admission. Regression coverage checks both
  duplicate submission and a full interrupted queue.
- Artifact cleanup could report success when SQLite ignored a delete, leaving
  reservations charged. Cleanup now verifies that the affected rows are gone.
  SQL fault tests retain the charge, reject further publication on the stranded
  writer and prove recovery after the fault is removed.
- Artifact retention used `EditRun`, whose reconciliation completed abandoned
  deletions before retention could account for them. Retention now acquires the
  run lease and owns cleanup directly. Process-exit tests cover both deletion
  intent and missing payloads, including idempotent repeated cleanup.

## Finding: failed checkpoints could become successful jobs

Severity: high. The service classified job-table failures specially, but did not
classify run-checkpoint failures the same way. The engine could return an
in-memory `completed` status when its final save failed. Queue execution used that
summary to complete the job and swallowed the storage failure as though it were a
recorded operation error.

A SQLite trigger rejecting the final completion checkpoint reproduced the defect:
both queued jobs executed and were logged as completed, then the test timed out
waiting for the worker to stop. A related engine path could save a terminal
failure after a checkpoint error. With a transient fault, nested coordinators
could overwrite the last durable evidence even though an effect was uncertain.

The correction stays within the existing owners:

- The engine remembers its first checkpoint error and refuses further writes for
  that execution. It joins owned collection workers and returns a running record
  rather than claiming an unsaved terminal status.
- The service uses one private persistence error type for actual storage failures
  in starts, checkpoints, continuations and job updates. Its `Unwrap` preserves
  the original cause for `errors.Is` and `errors.As`.
- Queue execution marks the affected job interrupted and returns the storage
  failure to the worker. Ordinary recorded operation failures still allow queue
  dispatch to continue.
- The worker's existing shutdown cancels and joins peer jobs before releasing its
  workspace lease. Recovery remains explicit; the fix adds no automatic replay.

Regression checks inject faults at attempt start, attempt outcome and completion
using the real SQLite store. They assert stopped dispatch, interrupted evidence
and successful operator recovery without duplicated effects. Additional tests
cover review/recovery error identity, unlinked wait continuation, peer cleanup
before lease release, and a single transient post-effect fault across root,
collection, child, mapped-child and repeated-child scopes.

Some existing engine assertions changed from `failed` to `running` after a failed
checkpoint. Their effect/count assertions remain: the changed status preserves
recovery evidence instead of asserting a terminal state that was never committed.

## Architecture assessment

The package boundaries still describe real ownership. The service coordinates
authorized execution; the engine owns scheduling and checkpoints; recipe prepares
graphs and bindings; store owns transactions, locks and durable records; daemon
owns worker lifetime. Nodes own external contracts, while CLI, API, Agent tools
and TUI adapt requests and present results. The audit does not justify another
package split, generic execution framework or callback abstraction.

The engine's main loop is a maintenance hotspot, but its sequential control flow
keeps dispatch, interruption and final persistence together. Splitting it into
small forwarding helpers would add navigation without removing responsibility.
The private persistence error is justified by a concrete consumer: the worker
boundary must distinguish a storage fault from an ordinary recorded operation
failure.

## Verification

Windows, Go 1.26.11. All checks below passed on the fix:

```text
go test -p 1 ./cmd/... ./internal/...
go test -race -p 1 ./internal/workflow/engine ./internal/workflow/service ./internal/workflow/store ./internal/workflow/daemon ./internal/workflow/api ./internal/workflow/fixture ./internal/tui/workflow
go test -race ./internal/workflow/daemon -run 'TestCheckpointFailure|TestWorkerStopsOnRunCheckpoint' -count=1
go vet ./internal/workflow/... ./internal/tui/workflow ./cmd/gektor
go build -o <temporary executable> ./cmd/gektor
```

The final daemon race command includes the peer-cleanup regression added after
the broader race run. Source scope excludes archived evaluation sources.

The compiled CLI was also exercised with a process-local temporary user profile
and temporary stores:

- `collection-report` validates and completes with two successful items, one
  collected failure and outputs `[4,6]`.
- `fixture-test` passes with live HTTP explicitly denied; its real script returns
  the expected result. The keyed-join fixture also passes.
- `repeated-subworkflow` completes with `done: true` and cursor `3`.
- Status observes the completed run and its three artifacts. Explicit retention
  removes that old terminal run and its artifacts; a second status confirms it.

Only temporary stores were pruned. Existing user history was not touched. These
examples do not establish live SSH, HTTP-provider or model quality.

The website publication failure reproduced at the `8cae30e` baseline is fixed.
The publisher now tracks nested label tokens within their parent link's range
and edits destinations without replacing the label. Regression coverage includes
inline code, reference links and linked images. `npm run check` passes all nine
publication tests and the path/anchor check; `npm run build` verifies 36 HTML
pages and 36 agent files.

## Paused acceptance work

The user deferred further testing on 2026-11-19. Astra's fixes are committed as
`11eef13`. No combined API-import recipe or new acceptance test was added in this
session. The branch remains unmerged.

Resume with a deterministic paginated API import covering a simulated 429,
durable wait, worker restart, deduplicated writes and a final report. Then try
authoring and correcting that workflow through the TUI with the user's actual
model. The saved endpoint `http://127.0.0.1:9931` refused connections during this
session; no live proposal request ran. Record observed usability problems and
fix them before the final branch checks and merge. Existing component/race checks
do not substitute for this combined acceptance case or a live authoring trial.

## Limits and follow-up

- Worker job and operation permits apply to work dispatched by that process.
  Independent CLI runs and operator continuations are outside those permits.
  Artifact budgets span processes sharing the same workspace in the same store.
- Queue claims and point updates avoid loading all snapshots. Some control,
  scheduler, listing and linked-job paths still hydrate retained job snapshots.
  There is no representative large-history throughput benchmark yet.
- Inspection pagination projects a hydrated saved run. It is neither an indexed
  invocation query nor a pinned snapshot across requests.
- Artifact budgets cover retained payloads and reservations. They are not CPU,
  memory or whole-database disk limits. Explicit retention removes artifacts
  before history; a later history failure requires repeating cleanup.
- Status reports saved observations, a separately sampled lease and the last
  successful scheduler tick. It does not prove readiness or successful effects.
- Current checks cover this Windows checkout. They do not certify all platforms
  or external providers.

These limits do not require new abstractions now. Measure a slow retained-history
path before adding queries or caches. The independent storage findings above are
addressed; this review does not establish that every possible storage fault has
been exercised.
