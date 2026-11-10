# Internal cleanup implementation

Branch: `codex/internal-architecture-cleanup`. Baseline: `154f232`.

The accepted architecture audit and naming proposal are the scope. Keep model,
tool, workflow, permission and wire behavior; remove retired compatibility paths
directly. No shim packages or replacement frameworks.

## Target ownership

- `agent`: model/tool loop.
- `harness` (formerly `run`): active execution, gates and reporting.
- `conversation` (formerly `session`): durable history and conversation titles.
- `agentrpc` (formerly `agentstdio`): stdio RPC transport.
- `workflow` (formerly `automation`): workflow contracts, execution and storage.
- `userdirs`, `executables`, `retrospective`: explicit replacements for product
  path lookup, executable discovery and archived-run review package names.
- `app`: construction/discovery composition; prepared profile naming reflects
  what it does, without a duplicate unused client field.
- Capability services/contracts remain independent. Node adapters share one
  concrete adapter package; agent adapters live with agent tool composition.
- Workflow terminal state, message dispatch, view and asynchronous effects have
  one concrete screen owner; root TUI retains navigation and shared overlays.

## Sequence and acceptance

1. Share strict profile decoding; share HTTP request/error preparation and honor
   streaming retry delays/cancellation and the total retry bound (B1/B3/D4).
2. Deliver events directly in process and serialize at wire boundaries; preserve
   event field names/order and diagnostic prefill buckets (B2/D1).
3. Supply authorization/store dependencies during tool construction, including
   nested workflows and derived file/image outputs (D3).
4. Apply accepted package/type names and title/prepared-profile ownership; update
   imports, package-layer checks, command help/docs and repository references.
5. Consolidate tiny capability adapters, remove retired preferences and old
   archive/catalog/browser restoration branches, and trim obsolete historical
   comments without removing current correctness/security explanations (D5-D7).
6. Extract workflow screen ownership and domain status presentation; preserve
   focus, dialogs, asynchronous result admission, cancellation, undo/redo,
   scheduling and graph interactions (D2/D6).
7. Close audit findings with evidence and refresh architecture/development docs.

Use focused behavior regressions, production tests and vet (`./cmd/...
./internal/...`), affected race tests, all dry eval probes and both distribution
targets. Saved ignored eval source is not part of the package test walk. No live
model evals or GitHub release publishing are required.

## Progress

- Baseline committed as `154f232`; cleanup and verification complete on the
  separate branch, ready for integration.
- B1: command and workflow profile loading use the same strict decoder. Unknown
  keys, extra JSON documents and oversized profiles fail before client creation.
- B2/D1: native events reach in-process consumers directly. JSON serialization
  remains at RPC/JSONL and persisted diagnostic boundaries. Prefill diagnostics
  retain distinct 0/1024/2048 buckets.
- B3/D4: unary and streaming Chat Completions share request and HTTP error
  preparation. A retryable untouched stream consumes the same retry budget and
  honors the backend delay and cancellation before a unary retry.
- D3: concrete factories receive store, policy and authorization dependencies
  before tool construction. The post-construction mutation scan is gone.
  Workflow agent adapters receive a registry factory from composition.
- D2/D6: the concrete workflow screen owns state, dispatch, rendering and async
  operations. Root navigation supplies shared UI callbacks. Screen shutdown
  cancels its operations; graph rendering consumes domain-neutral labels/tones.
- D5: five node adapters share `capabilities/nodes`; agent wrappers live in
  `tools`. Services and operation contracts retain their existing boundaries.
  Production package count under `internal` is 78 after removing `rules`, down
  from the audited 88.
- D6: all seven root renames are applied directly. Prepared profile, HTTP client
  and settings edit-session names match their responsibility; conversation title
  helpers have one owner.
- D7: retired preference fields, array snapshots, browser/catalog restoration
  migrations and old graph-layout decoding are removed. Current catalogs restore
  lazily. Schedule revisions remain monotonic through deletion and recreation.
- Minor ledger notes: terminal truncation now measures ANSI-aware cell width;
  local inspector structs no longer store unused configured model names.

The report's possible future MCP transport split and combined llama metadata
request were optimization ideas, not defects or acceptance requirements. Neither
requires another package or framework for this cleanup; existing contracts stay
in place. No follow-up migration or compatibility facade is required.

## Verification

- Production tests: `go test -p 1 ./cmd/... ./internal/...` passed.
- Static analysis: `go vet -p 1 ./cmd/... ./internal/...` passed.
- Race tests passed for agent, events, harness, both LLM protocols, tools, roles,
  TUI and its components/settings/editor, workflow adapters and workflow store.
- `go run ./cmd/eval -dry`: 44 probes passed; no model requests.
- `build.cmd dist` and `go run ./cmd/release -target linux-amd64 dist` passed.
  Both outputs are development distributions from this branch, not a published
  GitHub release.
- `git diff --check HEAD` passed. Regression coverage includes strict workflow
  profiles, native/wire event projections, prefill buckets, unary/SSE request and
  HTTP error parity, retry delays/budgets/cancellation, derived-output permissions,
  current catalog restoration, schedule generations and workflow-screen shutdown.

These checks exercise fake clients, local HTTP fixtures and production terminal
models. They do not claim a live terminal walkthrough or a new live-model score.

## Focused checks and permission-log follow-up

The focused review supersedes the original keep verdict for `rules`. GEKTOR
ships no language-specific source validation or formatter selection. `rules` and
the separate post-edit `gofmt` subprocess are removed. Projects provide their
own verification command; generic content findings remain separate from it.

`permissionlog.WrapGate` is the single context-aware observer of operator gate
decisions. It preserves the decision, error and context, and remains best-effort.
The `-audit` option and JSONL fields retain their current names and meaning;
automatic policy decisions do not pass through the operator gate.

`checks.Reporter` owns both post-edit checks and the final sweep. Both read at
most 256 KiB plus an overflow byte, skip oversized files with an explicit
`scan-skipped` finding, and emit at most 20 findings plus an overflow marker per
pass. Only delivered findings enter deduplication. Tools pass a changed path to
the reporter instead of rereading/scanning files or expanding findings into
primitive callback arguments. Unused `Rule.Name` and `Reporter.Rules` APIs are
removed. Focused regressions cover the exact read limit, oversized files, both
finding caps, deduplication, lack of automatic language checks and gate context.

Follow-up verification passed: the full production test suite, vet, package and
documentation boundary checks, race tests for checks/permissionlog/tools/roles/
TUI/harness/CLI, and all 44 dry eval probes. No live model evaluations ran.
