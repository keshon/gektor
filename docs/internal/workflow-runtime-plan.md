# Workflow runtime implementation plan

Date: 2026-11-13. Starting revision: `6948888`.
Branch: `codex/workflow-runtime-v4`.

## Start here: current handoff — 2026-11-19

Continue on `codex/workflow-runtime-v4`. Selective retries (`9622fe3`) and native
failure classification (`399a177`) are committed and pushed. The first stage 5
slice (pinned/revocable HTTP bindings) and shared SSH/MCP source/session
lifecycle checks are committed as `339f5ef`. The SSH named-binding slice, saved
MCP binding pins and integrated acceptance checkpoint are committed and pushed
as `3aa7ce4`. Operator HTTP updates are committed and pushed as `b0a52d0`.
The stage 6 HTTP/artifact slice below is included in this checkpoint commit.
Stage 6 is committed and pushed as `dbd051a`. Stage 7 has started: the TUI
now calls `internal/workflow/authoring` for bounded, tool-free model proposals
and registry-backed draft shape validation. The screen retains cancellation,
review, stale-draft checks, and application; proposals do not save or execute.
Strict envelope validation rejects missing/unknown fields and wrong types.
Existing CLI `workflow draft` remains the Agent-backed save flow, not a duplicate
of the interactive proposal/review operation.

Affected authoring/TUI tests and vet pass. Deterministic provider substitutes
cover token preflight, deadlines, malformed envelopes, truncation, unexpected
tool calls, cancellation errors and registered recipe shape validation. This
is not a live-model quality evaluation or complete execution validation.

Shared diagnostics now live in `internal/workflow/diagnostics.go`. Schema leaf
locations are available to CLI and TUI, with typed `StepError` context for recipe
preparation and argument-schema failures. Literal mapped items identify their
`/for_each/<index>` locations. TUI navigation consumes this report and still
never edits a recipe in response to an error. Older runtime messages retain
best-effort navigation; not every runtime/graph error has typed node context.

CLI `workflow validate` prints a JSON failure report when its service call
fails and retains the original error/nonzero exit. Pre-service flag/input errors
still use the existing stderr path. Reports do not claim that permission/read
failures establish invalid recipe syntax. Validation creates no run storage.

The shared diagnostics checkpoint `c6e5e4c` passed source-scoped
`go test -p 1 ./cmd/... ./internal/...`, affected vet, build, documentation and
diff checks. Nested attribution is checked with the affected workflow, service,
CLI and TUI suites, Agent tools, API, daemon, engine and convention suites,
plus affected vet, build and documentation checks. Concurrency and resource
lifetimes did not change.

Nested source attribution is now typed through `workflow.RecipeError`. Each
diagnostic can carry its failing `recipe_path`, distinct from the CLI report's
requested root path. Freeze/read and frozen compilation retain the child source
rather than relabelling its failure as the caller's. Original error identity
remains available through `errors.Is`/`errors.As`. TUI navigation checks source
identity before selecting a node; child errors with the same node ID or schema
index no longer focus a root node. It displays the failing filename without
changing the current draft.

Deterministic checks cover three-level calls with child shape/argument/missing
file failures, frozen child errors after saved-file changes, child permission
denial identity, and root/child TUI navigation with colliding node IDs.

Invocation/attempt inspection now uses `service.InspectRecord` for both CLI
and Agent adapters. `view=invocations` pages scoped IDs/statuses/counts without
argument/output payloads (default 50, maximum 100), and `view=attempts` returns
the selected invocation's stored retry timing/failures. Existing step/summary
pointer narrowing and 8 KiB value omission remain. CLI keeps its full-record
default; Agent tools keep summary. TUI Result shows direct and first-ten-item
attempt histories, failure codes, timing and item IDs. Inspection never resumes
or mutates a run. Attempts expose existing metadata, not invented per-attempt
argument/output snapshots.

Pagination is a projection of the hydrated saved run, not a new indexed store
query or a pinned multi-request snapshot. Active runs can change between pages.
Store-read throughput/budgets remain stage 8 work. Deterministic tests cover
page bounds and large offsets, scoped item/child IDs, stored history across
CLI and Agent adapters, malformed selections, TUI retry details and unchanged
record values. Service, Agent tool, CLI, TUI, API, daemon and convention suites
pass, along with affected vet, executable build, docs and diff checks. No new
concurrency was introduced.

Captured-input fixture tests now have explicit CLI commands: `workflow capture`
exports version 1 fixture JSON from a completed run, and `workflow test -fixture`
uses those values against the current recipe. `workflow/fixture` owns strict
call/response matching and consumed-attempt state. `service.Test` uses the existing
compiler and engine with a separate handler; only allowlisted isolated-script, join and
call/output builtins execute, while every external operation must match a
fixture. External executors are never called and no durable run/jobs are created.
The application registry supplies the explicit pure-operation allowlist; the
service imports no concrete node adapters. Fixture matching imports only workflow
definitions, enforced by the package-boundary check.
Missing, changed, exhausted or unused calls and invalid outputs fail closed.
Expected typed failures, retries and scoped mapped child calls use the normal engine.

Fixture v1 intentionally rejects reviews and durable waits because it has no
approval/continuation events; capture also rejects operator recovery and unfinished
runs. It preserves inputs and typed retry failures without inventing per-attempt
outputs. Fixture comparison preserves exact numeric literals and ignores object
key ordering. The file limit is 4 MiB. `examples/workflows/fixture-test` demonstrates
an HTTP fixture plus actual script logic. Ordinary execution paths remain live;
fixture results explicitly carry `mode: fixture` and `passed`.

Checks cover registered-executor traps, CLI capture-to-test regression detection,
changed script outputs, typed failures and retries, mapped child IDs, strict
mismatches, output schemas, read permissions, absence of workspace/store writes,
concurrent response consumption and the shipped example with HTTP denied.
Source-scoped `go test -p 1 ./cmd/... ./internal/...` passes, as do affected
fixture/service race tests, vet, executable build, documentation and diff checks.

The editor's Behavior dialog now opens focused Execution, Map, Loop and Retry
forms. Execution includes error routes and finalizers. Loop/retry policies expose
individual controls and an enable switch; disabling removes the policy. Clearing
map items clears its associated policy. Properties displays these execution
settings. Forms preserve unrelated operation arguments/policies and draft-only
ownership, reject stale/frozen targets, and use the existing undo path. Applying
still does not validate, save or execute; V performs the shared validation.
Tests cover chooser completion, structured policy edits/removal, preservation of
unrelated settings, undo and stale/frozen submissions. Source-scoped tests,
affected vet, executable build, documentation and diff checks pass. No new
concurrency was introduced. The fixture architecture reference now correctly
names the service method rather than a nonexistent package function.

Named-binding editor controls now cover the existing HTTP request and SSH
exec/upload/download nodes. Node / Saved connection reads metadata, displays the
origin or endpoint/port, and pins the inspected name/revision. Apply rechecks
selected metadata, rejects changes/revocation and mismatched literal HTTP origins,
and preserves unrelated arguments through the existing draft undo path. HTTP
credentials can be removed. Dynamic URL references retain runtime origin checks.
Mapped nodes still require connection fields in each item's complete arguments;
the picker does not write ineffective fields into the empty map base arguments.
No connection, credential resolution, server start or remote command occurs here.
MCP remains an explicitly separate Agent binding with its existing lifecycle;
there is no implicit MCP workflow node or new adapter in this editor checkpoint.
Tests cover metadata-only listing, selection/removal, explicit SSH destination
choice, pin changes, revocation, origin mismatch, stale/frozen drafts, undo and
preservation of other arguments. Affected TUI/workflow/SSH/application/CLI and
convention tests, affected vet, executable build, documentation and diff checks
pass. Concurrency and runtime credential resolution are unchanged.

Stage 7 implementation is covered. Keyed joins now use a pure workflow_join node,
with inner/left/full modes and JSON Pointer keys. Keys are exact strings or integer
literals, never coerced; missing/null/decimal/exponent keys fail. Duplicate keys
produce all matching pairs in deterministic left/right order, bounded to 10,000
pairs and 1 MiB. Existing 64 KiB resolved arguments still apply. No scheduler state
or implicit skipped-output fallback was added. Normal dependencies skip joins with
skipped sources; completed joins survive review continuation without rerunning.
Fixture mode executes joins for real and rejects substituted join responses.
Runtime join data/expansion failures are typed join_failed errors; cancellation
retains cancellation identity. The editor failure-source label was corrected:
on_failure names the source whose failure this node handles, not a destination.
Tests cover exact large integers, escaped/root pointers, duplicate ordering,
empty/skipped inputs, key and byte/row bounds, concurrent execution, cancellation,
failure routes, stored review continuation and fixture regression detection.
Source-scoped tests, join/service race checks, affected vet, executable build,
the shipped CLI fixture example, documentation and diff checks pass.

Stage 8 now has bounded worker jobs and shared worker-operation permits. Serve
owns N execution loops plus one scheduler under the existing workspace lease.
CLI max-jobs defaults to 1 and max-operations to 32; both are bounded to 1–32.
Due continuations and new queue jobs alternate per loop. Waiting/review states
release their job slot. Non-concurrent nodes serialize by operation name across
the worker's jobs, while maps also retain their per-map bound. A worker or
scheduler failure cancels peers; Serve joins every loop before releasing its
lease, then reconciles claims cancelled before run creation. Operation failures
remain recorded per job and do not stop other jobs.
The registry limit owns no goroutines and does not alter the original registry.

Hot queue operations no longer hydrate every saved recipe: cancellation reads
one job, updates touch one job transactionally, and claims inspect indexed
status/creation metadata before loading only the chosen snapshot. Selection
retains exact timestamp order, including nanoseconds, with stable ID ties.
The SQLite index is additive; stored row shapes and snapshot values are unchanged.

Source-scoped tests, registry/store/daemon race checks, affected vet, executable
build, documentation and diff checks pass. Tests cover job/operation caps,
non-concurrent node serialization, cancellation without leaked permits, joined
shutdown before lease release, queue draining after a typed operation failure,
exact snapshot values, transactional rollback and indexed claim ordering.

The bounded worker checkpoint is committed and pushed as `53cff32`.

Stage 8 status observations now share `store.Status` between CLI `workflow status`
and operator-only `GET /v1/status`. One database transaction reads retained
job/run counts by saved status, oldest queued timestamp, schedule counts, due
waits and artifact counts/bytes by publication state. It does not hydrate payloads,
execute, reconcile or cancel work. Writing/deleting reservations remain visible.
Saved running counts are not live-executor counts. A separate worker lease probe
and persisted successful scheduler tick distinguish ownership from progress;
startup clears the old tick and shutdown retains the last successful observation.
HTTP 200 means observation succeeded, not readiness or successful remote effects.

The status slice passes full store/daemon race suites, affected CLI/API and
convention tests, affected vet, executable build, documentation and diff checks.
Tests cover persisted ticks across store handles, startup reset, joined worker
shutdown, retained counts, due deadlines, exact queue timestamp order, reserved
artifact bytes, workspace isolation, cancelled reads, payload-free observations,
operator-only HTTP access and storage-error responses. No broad acceptance audit
or live-provider evaluation is claimed by these checks.

The status checkpoint is committed and pushed as `72a35e7`.

Stage 8 now includes shared workspace artifact budgets: 1 GiB and 4,096 artifacts
per workspace in the same store. Byte/count admission shares the existing immediate
SQLite reservation transaction with per-run limits. Published, writing and deleting
rows all count; files are removed before quota is released. Quota waits retain
local notifications and poll every 250 ms for changes from other runs/processes.
Cancellation does not leak reservations. These budgets cover retained artifact
payloads/reservations, not all database/workspace storage, CPU or memory.

Explicit `workflow prune-runs -older-than <positive duration>` removes old terminal
run history, artifacts and linked terminal jobs. It uses each run's lease and saved
status/finish metadata rather than hydrating invocation trees. Leased, waiting,
reviewable, interrupted, unfinished and linked non-terminal jobs/runs are retained.
Artifact cleanup shares the existing deletion-intent path with prune-artifacts;
history/job deletion commits together afterward. A history failure can leave
artifacts already removed and history retained; repeating the command finishes.
SQL deletion failures/ignored deletes roll back linked job removal. Callbacks,
invocations, attempts, scopes and wakeups cascade with the run. Pruning history
ends its callback/submission deduplication windows. Recipes, schedules, structured
records and persistent lease files remain; SQLite pages are reused, not vacuumed.
Retention remains explicit and disabled by default; no background age policy runs.

Source-scoped `go test -p 1 ./cmd/... ./internal/...`, the full store race suite,
focused quota/retention race checks, affected vet, executable build, documentation
and diff checks pass. Boundary tests use metadata fixtures rather than writing a
gigabyte. Two real subprocesses contend for the last reservation and reuse it
after release; cancellation and pending-deletion accounting are also checked.
Retention tests cover strict age boundaries, leased/recoverable/unfinished runs,
linked active jobs, SQL aborts/ignored deletes, atomic run/job rollback, detail
cascades, repeat cleanup, quota reuse and expired deduplication windows. CLI age
validation and existing artifact export/review behavior pass. Only temporary
test stores were pruned; existing user history was not touched.

Stage 8 is committed and pushed as `8cae30e`. It covers bounded worker concurrency,
workspace artifact budgets, explicit retention and status observations.

The [architecture audit and local acceptance checks](workflow-runtime-audit-2026-11-19.md)
cover the completed plan. They found a persistence failure that could report an
unsaved completion as successful and continue queue dispatch. The engine now
stops writes after the first checkpoint failure; the service preserves the storage
error's identity and marks the job interrupted. The worker cancels and joins peers
before releasing its lease. SQL fault tests cover attempt start, outcome and final
completion, explicit recovery, review/wait continuation and nested scopes.

Source-scoped tests, workflow/TUI race checks, affected vet, executable build and
temporary-home CLI examples pass. The audit records remaining throughput limits
and the boundaries of this local acceptance. No live-provider or model-quality
evaluation was performed. There is no outstanding implementation slice in stages
1–8; new work should address a concrete finding rather than extend the plan by
default. An optional independent store/quota review has not been incorporated.

Worker permits currently cover only work dispatched by this process; independent
CLI runs and operator continuations are outside them. Do not claim process-local
permits are persistent or cross-process workspace budgets.
Keep this distinction from the SQLite artifact budgets, which do span processes.

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

### Selective retry checkpoint — 2026-11-18

Committed as `9622fe3`: optional `retry.codes` filters exact typed
operation failure codes. Omitted or empty codes preserves the existing policy.
The handler validates at most 32 unique, nonblank codes of 1–64 bytes without
surrounding whitespace. Unknown codes simply do not match. Existing RetrySafe,
attempt/delay limits and per-attempt permission/output checks remain in force.
Calls and waits still cannot retry. No interrupted effect is automatically replayed.

Attempts retain typed failure metadata, including failures before a later success.
Context cancellation/deadline errors never qualify, even inside ExecutionError.
Cancellation observed after the outcome checkpoint returns the context error;
checkpoint failures stop before another attempt. Error routes see only the final
settled failure, after exhaustion or a nonmatching code.

Tests cover matching/nonmatching/default/omitted codes, persisted attempt metadata,
unsafe nodes, invalid code lists, eventual success, cancellation and failed outcome
checkpoints. The recipe reference and builtin authoring skill describe the policy.

Verification: the full source-scoped test suite, engine/service race tests,
source-scoped vet, a CGO-free build, 45-probe eval dry-run and documentation
checks pass. No model or remote-provider test was needed for this policy.

### Native failure classification checkpoint - 2026-11-18

Committed as `399a177`. Process nonzero exits and output overflow now
carry `process_exit` / `process_output_too_large` only after both diagnostic
artifacts save. Joined artifact/encoding errors remain plain immediate stops.
Startup, supervision, local timeout and parent cancellation errors do not route.
Process RetrySafe remains false.

Model provider errors, incomplete generations/unexpected tool calls and oversized
responses carry `model_request_failed`, `model_incomplete` and
`model_response_too_large`. Profile resolution, request bounds and output schema
failures stay plain. Context cancellation/deadline errors stay identifiable.
Model RetrySafe remains false; no global queue/lifetime changes were made.

HTTP redaction now retains cancellation/deadline identity. Shared HTTP transport
exposes ErrDestinationDenied for URL/DNS policy rejections; the HTTP node returns
that as a plain, redacted stop instead of wrapping it as a network failure.
Oversized selected headers use the existing http_response_too_large code.
Artifact and record nodes are deliberately unchanged: their errors include path,
contract and store failures that must not be blanket-wrapped. Script and callback
classification remains as implemented earlier.

Tests cover process diagnostic persistence, artifact failure, overflow, cancellation
and a real service/store run through an error route and finalizer. Model tests
exercise provider/response failures against plain config/schema stops. HTTP tests
cover policy rejection, redaction and cancellation identity; transport tests prove
DNS denial occurs before a connection.

Verification: the full source-scoped test suite passes. Affected native nodes and
HTTP transport pass race checks; source-scoped vet, a CGO-free build, documentation
checks and diff whitespace checks also pass. No remote provider was contacted.

### Stage 5 first slice: pinned HTTP credential bindings - 2026-11-18

Shared `internal/credentials.Source` selects exactly one environment or protected
stored reference and resolves live bytes. HTTP bindings retain origin/header
ownership in `internal/workflow/credentials`; SSH and MCP retain theirs. No
universal connection registry, OAuth framework or duplicate secret store was added.

HTTP metadata inspection returns a SHA-256 binding fingerprint, source kind,
protection and revocation status, without secret/environment/reference values.
Registration remains create-only. Protected stdin registration cleans the secret
file if binding registration fails. Binding metadata is bounded and strictly
decoded. Revoke creates a durable name tombstone; repeats are harmless, and the
name cannot be reused through this API. Already-resolved requests can finish.

HTTP args now require credential_revision whenever credential is present. The
permission resource includes the fingerprint. Resolution checks the pinned
binding, exact origin and revocation before reading the secret, then rechecks the
binding before returning it. Environment value rotation does not change metadata;
changing its source/origin/header/prefix does. Old unpinned authenticated recipes
must add a revision; there is no alternate unpinned execution path.

CLI operator actions are register-credential (-env or -secret-stdin),
inspect-credential and revoke-credential. Unix protection is explicitly owner-only
files, not encryption; Windows uses DPAPI. Recipes/checkpoints contain neither
secret bytes nor raw opaque references. No new agent tool/schema count was added.

Tests cover source selection, live rotation, stale/missing pins, immutable names,
revocation, protected value cleanup, malformed metadata, safe CLI output and
real service/store review continuation. The latter proves rotation succeeds,
rebind/revoke sends no request, and credential errors dispatch no error branch or
finalizer. Permission resources include the pinned revision.

Verification: source-scoped package tests pass, with the CLI flag documentation
convention rechecked after correcting its descriptions. Shared/HTTP credential
and HTTP-node race checks, source-scoped vet, a CGO-free build, Markdown checks
and diff whitespace checks pass. Tests use isolated stores and a local fake HTTP
transport; no personal credential files or remote services were used.

### Shared secret lifecycle checkpoint - 2026-11-18

Implemented in the working tree alongside HTTP bindings. Environment/stored source
selection now passes through internal/credentials.Source for HTTP, SSH, MCP stored
headers and adapter environment references. Stored HTTP values also carry a
purpose/origin/header envelope, checked by the HTTP owner after shared resolution;
a reference swap cannot redirect that secret to another origin or header. Protocol owners still enforce SSH
purpose/encryption and MCP origin/header checks. SSH still requires DPAPI for
stored passwords/passphrases; this does not introduce a Unix plaintext fallback.

A small shared Revocations store persists permanent markers in caller-selected
namespaces. Opaque Store.Revoke disables future resolution and checks again after
decryption; Delete does not remove revocation history. HTTP name tombstones now
live in credentials/http-revoked; opaque reference markers live in each protected
store's revoked subdirectory. No released data migration or credential deletion
is performed. This API is store-level; there is no new SSH/MCP operator command.

MCP sessions retain connection-time secrets, so per-call checks compare their live
references against the captured values before transport dispatch. HTTP sessions
reject revoked/deleted stored references; stdio sessions reject missing/changed
environment sources. Changed values require an explicit reconnect, with no tool
replay. Existing calls and session-close cleanup can finish. Header/reference maps
are copied before retaining the check. There is one private check owned by each
session; no common connection manager or extra worker pool was added.

Tests exercise shared revocation across reopened stores, retained tombstones after
delete, MCP purpose/origin checks, actual SSH authentication before source
revocation, a connected TLS MCP session blocked before its next tool request, and
a real stdio child requiring reconnect after environment rotation. They preserve
current SSH immutable profile snapshots and protocol redaction. Existing SSH/MCP credential IDs and payload formats remain unchanged. The new,
unreleased HTTP stored-secret format is purpose/origin/header-bound.

Verification: full source-scoped tests pass. Affected shared/HTTP/SSH/MCP
credential and adapter race checks pass, including the final HTTP binding tests.
Source-scoped vet, CGO-free build, Markdown and whitespace checks pass. All stores,
SSH servers, TLS MCP endpoints and stdio children in tests are isolated fixtures.

### Stage 5 SSH named bindings — 2026-11-18

SSH discovery and operator `ssh inspect NAME` expose a metadata revision and
revocation status without resolving secrets. Commands and transfers require
`connection_revision` in both Agent and workflow contracts. Current profile
metadata is read during authorization and again before connecting; transfer
local-file authorization cannot silently switch the binding. Session grants
retain the revision. Profile changes require discovery and explicit recipe/pin
updates; there is no unpinned execution path.

`ssh revoke NAME` permanently disables the name through `ssh.json.revoked` markers,
including in existing services and after profile deletion/recreation. Secrets,
private keys and known_hosts contents are still read at connection time; their
contents are not hashed into metadata revisions. Already-started operations may
finish. Existing secret IDs and protected payload formats remain unchanged.

Verification: source-scoped `go test -p 1 ./cmd/... ./internal/...` passes.
Affected SSH, permission and tool race tests pass, including dispatch after
resource review. CLI inspect/revoke, live discovery, permanent named revocation,
changed metadata, authentication, transfers and cancellation are covered with
isolated fixtures. Source vet, CGO-free build, docs links and whitespace pass.
This SSH slice is included in the credential checkpoint commit.

### Stage 5 MCP bindings and acceptance checkpoint — 2026-11-18

Saved MCP adapters now retain an immutable revision of their reference-only
server definition. The manager owns metadata reads and permanent named revocation;
the adapter consumes a two-method binding interface for checks and permission
targets. HTTP and stdio sessions check live metadata before tool dispatch. Initial
connection checks run before and after catalog discovery. Revisions supplement
existing argument/path permission resources; they do not replace them. Exposure
switches keep the same revision, while transport/reference/description changes
require configuration reload and a new permission target.

Operator `mcp inspect ID` reports current endpoint/executable metadata, revision,
enabled and revocation state without secrets or command arguments. `mcp revoke ID`
writes a permanent marker under `mcp/revoked`, observed across manager instances
and after delete/recreate. Bundled integrations, explicit capability files and
`-mcp` launch arguments retain their existing launch-scoped ownership; this slice
does not invent saved bindings for them or expose Agent MCP tools as workflow nodes.

Acceptance checks added:
- Real local HTTP and stdio MCP sessions reject changes, disable, deletion and
  revocation from another manager before their next tool call.
- A local TLS HTTP workflow executes, waits for a callback, deduplicates delivery,
  reopens persisted state, reconstructs its registry and reaches operator review.
  Rotated secrets resolve at resumed dispatch; revoked bindings send no request.
- A real local SSH workflow executes once, pauses for review and reconstructs its
  registry before continuation. Completed commands do not replay; changed/revoked
  bindings block the next command. Neither workflow checkpoint contains secrets.
- Local TLS fixture routing lives only in the test transport. Production public
  URL validation and TLS verification remain enabled.

Verification: full source-scoped tests (`go test -p 1 ./cmd/... ./internal/...`),
affected race checks, source vet, CGO-free build, docs and whitespace checks pass.
The built CLI also passes saved-MCP save/inspect/revoke checks with an isolated
home. No personal bindings, remote MCP servers or model endpoints were used.
SSH/MCP changes and this acceptance checkpoint are included in this commit.

Assessment: this milestone now has integrated evidence for effect preservation,
callback deduplication, review continuation and current binding checks. Protocol
owners still own resolution and revocation; no universal connection manager,
implicit MCP workflow nodes or extra executor was introduced. Stage 5 is not
fully complete: operator HTTP updates, connection/editor UI and OAuth are still
separate follow-ups. Continue the delivery sequence before the broader audit.

### Operator HTTP binding updates — 2026-11-18

`workflow update-credential` accepts a complete replacement configuration and the
required `-revision` from inspection. Environment and protected-stdin sources
share existing validation and dispatch semantics. A managed update takes an OS
file lock, checks the current revision/revocation, writes and syncs a sibling
temporary file, rechecks and atomically replaces the definition. Lock files are
persistent metadata; closing the handle releases the OS lock. There is no stale
lock marker to remove after a process exits.

Missing names, stale revisions and revoked names cannot be updated. Failed stored
updates remove their newly protected value, while successful updates retain old
values that other bindings may reference. Reviewed HTTP execution now exercises
the real update API rather than editing its JSON fixture directly.

Verification: affected credential, CLI and reviewed HTTP tests pass, including
competing writers, stale revisions, permanent revocation and failed-secret cleanup.
Affected race checks, vet, CGO-free build, docs checks and CLI flag coverage pass.
The Unix lock implementation is cross-compiled; execution checks ran on Windows.

### Stage 6 HTTP and artifacts — 2026-11-18

HTTP request bodies now support JSON, text, URL-encoded forms and current-run
artifacts. Explicit query arrays merge into the URL before permission checks.
Responses have auto/text/JSON/artifact modes, selected extra headers and bounded
sizes. Binary/large responses stream through existing ArtifactAccess.Put; uploads
use its existing verified Read operation (at most 16 MiB). No alternate artifact
store or HTTP executor was added. Artifacts remain run-owned across review.

Redirects stay disabled by default. max_redirects permits 1–5 hops, each resolved
and public-URL checked before the workflow service authorizes the exact next
request. A small execution-scoped authorizer sits beside the existing artifact
access in context; the service retains policy and interactive serialization.
Credentialed chains stay on one HTTPS origin and re-resolve the current pinned
source per hop. POST 301/302 and non-HEAD 303 drop the body; 307/308 retain it.
The single request context owns the whole chain's timeout, with no hidden retries.

Inline responses redact source values (literal, JSON-escaped and URL-encoded).
Binary publication rejects matching values across read boundaries and discards
incomplete artifacts. Permission/credential/store errors remain terminal; HTTP
status, transport, inline body/encoding/JSON/size and redirect-limit failures retain
explicit operation codes. There is no business-error heuristic for a 2xx body.

Focused checks cover query/form/text encoding, content-type overrides, soft 404
bodies, 429 status handling, per-hop authorization, private/cross-origin targets,
credential rotation, redirect budgets and 307 replay semantics. A real run store
covers binary upload/download, streaming limits, secret/cancellation cleanup and
an artifact handoff across operator review. The shipped http-artifact example and
canonical HTTP reference describe the contract.

Verification: full source-scoped tests pass. Final affected HTTP/service/authoring
checks and affected race tests pass; source vet, CGO-free build and documentation
checks pass. The built CLI validates the shipped example using an isolated home,
without a model or public network request. This stage 6 slice is included in the
checkpoint commit; it is not the deferred broader project audit.

Next: stage 7 headless authoring/validation, structured diagnostics, inspection,
captured-input fixtures and editor coverage. Concurrent jobs, child scheduling,
workspace resource budgets and retention/health remain stage 8. Keyed joins and
broader node inventory still need their explicit semantics. OAuth serialization
is required when an OAuth integration is introduced. Continue implementation
before the broader audit and acceptance pass.

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
