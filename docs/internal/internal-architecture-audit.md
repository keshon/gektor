# Internal architecture audit

2026-11-10. Independent skeptical review of the current working tree, including untracked `agentstdio` and `release` source. This is a package-by-package architecture review with representative execution-path inspection, not a claim that every statement or possible behavior has been verified.

## Findings first

### B1 — P2: workflow model profiles silently accept misspelled settings

`internal/app/automation.go:62-68` opens a rooted workspace profile and decodes it with `json.Unmarshal`, followed by `Profile.Validate`. `internal/config/profile.go:108-125` deliberately rejects unknown fields with `DisallowUnknownFields`. The workflow `model_generate` node reaches the first path through its resolver; the command/settings loaders reach the second.

A valid profile with `generation.temprature` instead of `temperature` passes the workflow loader, then uses the zero/default temperature. The ordinary profile loader rejects it. Validation of the decoded struct cannot recover a dropped JSON key. This is a confirmed source-level behavior inconsistency; no backend request was needed to establish it.

Simpler target: one strict `config.Decode(io.Reader)` implementation, called by the existing file loader and the rooted workflow reader. Keep workspace access and the workflow's 64 KiB bound in their current owners. This preserves rooted access, all provider settings and current validation. Cost: small; risk: previously accepted invalid workflow profiles begin reporting their bad keys, as intended by the documented strict profile contract.

### B2 — P2: the in-process event conversion discards prefill progress samples

`internal/run/session.go:83` constructs `events.New(events.EventWriter(...))`. `internal/events/events.go:38` serializes the event; `internal/events/vocabulary.go:46` immediately decodes it into `map[string]any`, converting integers to `float64`. `internal/run/diagnostics.go:60-61` then asserts that `processed` is an `int`. The assertion always fails on this production route, so changes in the 1024-token processed bucket never participate in the diagnostic signature.

An isolated temporary test exercised a real `run.Session` with a stub client emitting otherwise identical prefill samples at 0, 1024 and 2048 processed tokens. Only one sample reached `diagnostics.jsonl`. The temporary test was removed after reproduction. This reduces the diagnostic evidence available during a long/stalled prefill; the live progress event itself still reaches the frontend.

Small immediate repair: decode numeric fields consistently. Better local simplification is D1 below. Preserve the metadata allowlist, suppression of token-by-token logging, and bounded rotation. Cost: small; risk: slightly more diagnostic samples, bounded by the existing bucket/rotation policies.

### B3 — P2: a streamed HTTP rate-limit response triggers an immediate unary retry

The unary HTTP path reads `Retry-After` and `retry-after-ms` at `internal/llm/chatcompletions/client.go:376-383`. The streaming path at `internal/llm/chatcompletions/stream.go:31-34` constructs an `APIError` without those fields. More fundamentally, `internal/agent/run.go:367-379` falls straight from a retryable pre-output streaming HTTP failure into the first call of `ChatWithRetry`; that first unary call has no wait at all.

For a server returning HTTP 429 with a requested delay, enabling streaming therefore sends an additional generation request immediately. The deliberate rule permitting fallback before any output is sensible; the missing delay is a separate defect. Unary retries can subsequently respect their own response headers, but the first retry has already happened. This is source-traced, not timed against a live provider.

Target: share HTTP error decoding and apply the existing bounded retry policy before the fallback request when an HTTP response requested a wait. Retain the no-fallback-after-visible-output rule. Cost: small to medium; risk: cancellation and whether the first HTTP error consumes the retry budget need explicit tests.

### D1 — Medium design debt: JSON is being used as an in-process event bus

The B2 round trip is intentional: comments in `events/vocabulary.go:26-29` claim it guarantees frontend schema parity. That is an unnecessarily expensive way to obtain parity, and B2 demonstrates the resulting type drift. Every delta/progress event incurs JSON encoding, buffer handling and map decoding; complex values such as context reports lose their Go types and must be reconstructed by consumers. This is architectural awkwardness, not evidence that event costs dominate model latency.

Use one event constructor/sequence owner with a callback sink. Serialize that same event only at JSONL/RPC boundaries; compare wire projections in contract tests. A direct callback should own/copy map data as necessary. No generic event framework or one-interface-per-event hierarchy is needed. Preserve event names, field names, ordering, sequence semantics and wire compatibility. Cost: medium because TUI/CLI consumers currently expect JSON-shaped numbers/maps. Do not replace the emitter without updating those consumers together.

### D2 — High design debt: the root TUI owns an entire second application screen

`internal/tui` contains approximately 15,113 production lines. The 3,462 lines in root `workflows*.go` mostly remain methods on the global `*model`, even though `workflowWorkspace` already groups screen state (`internal/tui/workflows.go:20-48`). The root dispatches workflow-specific messages (`internal/tui/update.go:107-117`), constructs workflow services (`internal/tui/workflows.go:104-106`), runs asynchronous jobs and gates (`internal/tui/workflows_run.go:27-158`), edits connections, schedules and parameters, and renders the graph/inspector. `internal/tui/workflow` currently owns only the pure recipe editor/graph mapping.

The problem is broad ownership and coupling, not a 300-line file limit. Adding another root file does not improve it. Move the complete workflow screen state plus Update/View/effect coordination behind a concrete screen model, while the root owns navigation and shared application overlays. A narrow callback for requesting a form or routing a permission response is enough; do not introduce a router/DI framework. Preserve draft undo/redo, frozen recipe execution, review gates, scheduling and shared popup geometry. Cost: several focused changes over multiple days; risk: keyboard focus, modal return paths, cancellation and stale-result admission. This is the highest-value structural work, but it removes little total code. It makes future changes local.

### D3 — Medium design debt: authorization is wired by a second pass over concrete tool types

Factories create tools in `internal/tools/packages_builtin.go`; `roles.configureTools` calls `tools.Compose` at `internal/roles/roles.go:404`, then `tools.BindWorkflowPolicy` at line 419. The latter scans every native package and mutates eight concrete tool shapes (`internal/tools/workflow_policy.go:26-62`). Despite its name, it also wires file extraction and image publication. A new wrapper/tool must be remembered in both construction and this mutation scan.

Current file/image services fail closed when their output callback is missing, so this review does not label the existing scan an authorization bypass. It is avoidable temporal coupling. Add the relevant store and authorization functions to the existing concrete factory dependencies and construct fully initialized tools once. Reuse the same policy helper for direct workflow authoring. Preserve exact permission names/resources and per-derived-output authorization. Cost: medium; risk: Plan restrictions, workflow nested permissions and image/file publication need their existing focused contract tests. No setter interface, reflection or registration framework is needed.

### D4 — Medium design debt: duplicate transport preparation has already drifted

`internal/llm/chatcompletions/client.go:296-368` and `internal/llm/chatcompletions/stream.go:71-134` independently build sampling/grammar/messages/tools/auth requests. Their non-200 error handling also differs (B3). These are genuinely parallel implementations of the same request contract; the protocol split between Chat Completions and Responses is not.

Extract one private request builder plus one HTTP-error decoder inside `chatcompletions`. Stream adds its SSE flags/headers and keeps its own accumulator; unary keeps its normal response decoder. Preserve wire payloads, explicit dialect sampling and image policy. Cost: small to medium; risk: request-field parity should be compared for every existing dialect. Do not combine the two different protocols into a universal wire model.

### D5 — Low design debt: capability adapter packages are more fragmented than their responsibilities require

Each capability has service, contract, tool and node packages. The shared contract earns its place: schemas, normalized resources, retry safety and validation are consumed by both execution paths. The service earns its independence: it owns rooted IO/parsing/publication and imports neither execution engine. In contrast, the five `*/nodes` packages total only 89 lines, and the five `*/tools` packages total 85 lines. Most wrappers only embed `contract.Operation`, choose concurrency, or copy methods into `automation.Node`; images additionally resolve session attachments, and social keeps a distinct permission/tool name.

A conservative reduction is to put the five node-adapter files together in one `internal/capabilities/nodes` package, with explicit `DocumentDefinitions`, `ImageDefinitions`, etc. This removes four package boundaries and changes composition imports, without coupling services to either engine. If reducing path count is a priority, tool adapters can join the existing `internal/tools` composition package: `roles` already imports it, and no service needs to import it. That removes another five packages, but slightly enlarges an already broad package. Neither change materially reduces algorithmic complexity. Cost: small; risk: preserve optional interfaces, image attachment grants, concurrency, permission names and persisted tool catalogs. Move directly; do not leave compatibility wrappers.

I do not recommend replacing these adapters with a generic operation framework. Approximately 174 lines are too little boilerplate to justify another abstraction hierarchy. Keeping the current split is also defensible if feature-local navigation is preferred; this is lower priority than D1-D4.

### D6 — Low design debt: several names and ownership seams are misleading

- `app.ConfigureAgent` claims to compose a thinking client, but `internal/app/services.go:30-57` only validates/clones the profile and compiles identity. Its caller assigns `prepared.Client` afterwards (`cmd/gektor/agent.go:399-406`), and the TUI obtains its runtime client from `Env`. Rename this operation around profile/identity preparation, and remove the redundant `Services.Client` field if its current absence of readers is retained. Keep `app.Backend` and `roles.Env`; they own different things. Cost/risk: small.
- `workspace/state.go:19-51` owns conversation title formatting and title-file persistence; `session.TitleFor` duplicates formatting specifically because `session` cannot import `workspace` (`internal/session/session.go:172-175`). Move title helpers into `session` and update CLI/TUI/RPC consumers. `session` already owns conversation persistence; the title path is outside the rooted workspace. No extra package is needed. Cost/risk: small; preserve the 60-rune first-line behavior.
- `tui/settings.Workspace` means staged settings, not `workspace.Workspace`. `SettingsDraft`/`EditSession` would communicate ownership better. Pure naming preference, not a correctness problem.
- `llm/chatcompletions.Server` is an HTTP client. Rename to `Client` during a local cleanup; do not introduce an alias layer. Pure naming clarity, small mechanical change.
- The supposedly domain-neutral graph renderer interprets workflow statuses (`internal/tui/components/graph/card.go:99-136`). Format status text/style in the workflow screen; the graph should render the supplied label/state styling. Small boundary correction, no new framework.

### D7 — Low design debt: compatibility and historical explanation remain after their purpose ended

`config.Harness.UnmarshalJSON` accepts retired `nudges`/`think_budget` fields (`internal/config/preferences.go:54-76`); `Appearance.Density` remains a persisted retired field (`internal/config/appearance.go:9-10`). These are actual compatibility paths despite strict-profile messaging elsewhere. Remove them directly if old preference migration is outside the supported outcome. Old conversation-array decoding and browser/catalog migration are different: deleting those changes the ability to reopen old sessions, so first decide whether that outcome is still required. Do not invent a permanent migration framework.

Many source comments recount previous implementations rather than state current contracts (for example `app/services.go:26-29` and long form/popup comments). Shorten obsolete history and retain the invariant/evidence in tests or development docs. This would reduce visual bulk, not remove features. Cost/risk: small; do not remove useful reasons for ordering, bounds or security decisions.

## Verdict on the suspicious package names

`agent`, `roles`, `run`, `session`, `app` and `agentstdio` are not six implementations of one agent. The direction inspected is frontend → run → roles → agent → llm/session/permission. Tools implement the agent's interface and are selected by roles; application composition constructs protocol clients and workflow catalogs. `agentstdio` frames requests and owns cancellation/EOF/draining; it does not implement the model loop. `session` is durable data, while `run.Session` is the active execution facade. This is conventional Go separation around a real import-cycle constraint.

Keep these boundaries. A name such as `run.Driver` could make the two session meanings clearer, but would not simplify behavior. Merging them would put protocol/configuration/frontends into the core loop or create a catchall runtime package. The present broad catchalls worth addressing are `tools` and especially the root `tui`, not the existence of `agentstdio`.

`tools` mixes concrete tools with MCP transport, browser installation/profile lifetime, background processes, output storage and capability catalogs. A future coherent extraction is a transport-only MCP package, keeping `MCPTool` adaptation in tools and manager persistence in `mcpmanager`. That requires removing its dependence on tool-specific output/image helpers first. It is medium-cost work, not a reason to split every file into its own package today. Do D3 first; it clarifies construction before another package move.

## What is necessary complexity

The inspected registries are maps of actual callable tools/nodes with collision/schema checks, not speculative plugin loaders. `llm.Client` has multiple production implementations and test doubles. `automation.Handler` separates execution/authorization from graph preparation. `ArtifactAccess` scopes access to a real run/step. Image input grants distinguish session attachments from workspace files. These interfaces encode present behavior.

The typed provider catalog plus protocol dialects is appropriately bounded. OAuth account binding and native Responses replay have different requirements from a local Chat Completions endpoint; collapsing them would lose semantics. Small local probe adapters are understandable and have no agent dependency. There is room to fetch llama `/props` once per inspection instead of independently for context/model/vision, but no need for a provider plugin framework.

The workflow subsystem is a second product surface: typed inputs/output references, branches, retries, review, nested calls, queueing, schedules, records and immutable artifacts. Its size is mostly the cost of those outcomes. The engine itself is about 313 lines; compiler/preparation and persistence have separate, real responsibilities. Replacing them with a loop over `[]func()` would lose review recovery, frozen dependencies and durable effects. Removing workflow features could shrink the application substantially, but that is a product scope cut, not equivalent simplification.

Process, worker and program discovery also differ. `process` owns reusable lifetime/environment primitives and bounded native execution. `worker` uses the current executable and a narrow JSON/resource-limit protocol for hostile parsers/scripts. Agent process tools retain streaming/full-output artifacts and background lifetimes. Do not blindly merge these runners: their output and cancellation contracts differ. Windows `unsafe` usage is confined to OS interop in inspected files. Production reflection is limited to equality comparisons in archive/context inspection; no reflection-based DI or generic factory framework was found.

Release packaging is normal standard-library code: concrete manifest, temporary stage, Go/Git subprocesses, ZIP/TAR and byte verification. Its approximately 509 lines include archive generation and verification, not an extensible build system. Keep it. Failure during a multi-target release can leave one completed target before the next fails; the package does not implement a transaction for the whole release. This is a limitation, not evidence for another orchestration layer.

## Current changes and verification

The CLI paused-history fork repair is sound on inspection: `Continue` and `Resume` have explicit different semantics (`internal/run/session.go:244-285`), and both share the same private continuation implementation. CLI forks and ordinary TUI follow-ups use `Continue`; interrupted recovery can answer the saved question through `Resume`. Do not collapse the two methods merely because their bodies are small.

The new command adapter test calls the actual TUI configuration helper. The TUI package tests instantiate the production `newModel` and its session factory, checking task/images, identity, streaming, Plan mutation refusal, findings and verification. These are materially stronger than constructing `run.Config` and naming the test TUI parity. They still do not open a terminal or prove every interactive focus/cancellation path.

Independent bounded command run during this review:

```text
go test ./internal/run ./internal/events ./internal/agentstdio ./internal/tui ./cmd/gektor -run 'TestResumeSharesSavedAnswerPromptAndReport|TestCLIForkOfPausedQuestionPreservesNewTurn|TestTUIProductionFactory|TestTUILaunchPreservesPreparedRunConfiguration|TestLaunchConfigurationAndBehaviorMatchAcrossFrontends' -count=1
```

Selected tests passed in `run`, `tui` and `cmd/gektor`. `events` and `agentstdio` compiled but had no matching tests in that invocation; this is not a claim that their tests ran. The B2 temporary reproduction passed and was removed. No live backend/model calls, browser sessions, OAuth login, network evals, production edits, commits or pushes were performed by this reviewer. Broader test/vet/race results reported by the parent are independent evidence and are not presented here as this reviewer's runs.

## Coverage and limits

Inventory: 88 Go package directories, 375 production Go files and 56,676 physical lines including comments/blanks. Counting the trailing newline as another split segment gives 57,051; these are the same files. Platform files for both operating systems are included. `conventions` has 12 production documentation lines but no runtime implementation. This explains why package count alone exaggerates runtime structure.

Every package below received responsibility/dependency/consumer inspection and source inspection of its public boundary plus representative behavior. Large packages were traced through the paths identified above; this is not line-by-line verification of all 56,676 lines. Parser variants, every UI interaction and every rare IO/concurrency failure were not exhaustively checked. The ledger records coverage, not a clean bill of health for every function. No absolute absence-of-bugs or absence-of-overengineering claim is justified.

Ledger sizes use the same physical-line metric, excluding `_test.go`. Consumers are current production importers; command-only packages are explicitly included. `Keep` means the boundary presently earns its place, not that it can never be improved. `Simplify` is tied to a concrete finding above. `Merge` is an optional path-count reduction, not required correctness work.

## Complete package ledger

1. **`internal/agent` — 4,036 lines.** Model/tool loop, recovery, compaction, inbox and archive projection. Consumers: `cmd/eval`, `cmd/gektor`, `internal/agentstdio`, `internal/automation/agenttools` and 10 other production package(s). **Verdict:** Keep; one actual loop, with real ordering and persistence invariants.

2. **`internal/agentstdio` — 275 lines.** Stdio RPC framing, single active run, operator replies and EOF/cancellation lifetime. Consumers: `cmd/gektor`. **Verdict:** Keep; transport boundary, no duplicated agent loop.

3. **`internal/app` — 841 lines.** Protocol/client composition, provider discovery, identity preparation and workflow catalog assembly. Consumers: `cmd/eval`, `cmd/gektor`, `internal/automation/agenttools`, `internal/tui`. **Verdict:** Simplify B1/D6; keep composition, clarify misleading preparation/Services shape.

4. **`internal/audit` — 66 lines.** Best-effort JSONL permission decision logging. Consumers: `cmd/gektor`, `internal/tui`. **Verdict:** Keep; small opt-in observer, no execution policy duplication.

5. **`internal/automation` — 392 lines.** Recipe/run/node/input/artifact contracts, registry and schema validation. Consumers: `cmd/eval`, `internal/app`, `internal/automation/agenttools`, `internal/automation/engine` and 23 other production package(s). **Verdict:** Keep; shared values/contract checks, no engine dependency.

6. **`internal/automation/agenttools` — 706 lines.** Agent-facing workflow authoring, run, schema, status, record and schedule tools. Consumers: `cmd/gektor`, `internal/roles`, `internal/tools`. **Verdict:** Simplify; accept a prepared service/factory instead of importing app to reconstruct its catalog (tools.go:27-29).

7. **`internal/automation/api` — 420 lines.** Authenticated HTTP submission/status/cancellation over a durable queue. Consumers: `cmd/gektor`. **Verdict:** Keep; ingress does not execute nodes or approve reviews.

8. **`internal/automation/credentials` — 121 lines.** Origin-bound names referencing environment credentials for HTTP nodes. Consumers: `cmd/gektor`, `internal/app`, `internal/automation/nodes/http`. **Verdict:** Keep; different outcome from storing opaque MCP secret values; do not merge stores merely because names match.

9. **`internal/automation/daemon` — 76 lines.** Workspace scheduler/queue lease, ticking, claiming and service execution. Consumers: `cmd/gektor`. **Verdict:** Keep; concrete small worker owner, not a second execution engine.

10. **`internal/automation/engine` — 313 lines.** Sequential graph execution, checkpointing, review, branches, retries and iteration. Consumers: `internal/automation/service`. **Verdict:** Keep; 313 lines implement required semantics directly.

11. **`internal/automation/nodes/artifact` — 114 lines.** Workflow artifact import/write/read descriptors. Consumers: `internal/app`. **Verdict:** Keep; run-scoped access is supplied through ArtifactAccess.

12. **`internal/automation/nodes/http` — 139 lines.** Bounded public HTTP node with origin-bound credentials and response policy. Consumers: `internal/app`. **Verdict:** Keep; transport/policy differ materially from inference HTTP.

13. **`internal/automation/nodes/model` — 98 lines.** One explicit bounded model request with profile resolution and optional output schema. Consumers: `internal/app`. **Verdict:** Keep; no inherited agent conversation/tools; global single-call slot is a deliberate throughput limitation.

14. **`internal/automation/nodes/process` — 107 lines.** Workflow native-process adapter and stdout/stderr artifact retention. Consumers: `internal/app`. **Verdict:** Keep; reuses process primitives and preserves failed-run diagnostics.

15. **`internal/automation/nodes/record` — 17 lines.** Two record operation descriptors over shared records operations. Consumers: `internal/app`. **Verdict:** Keep or fold with other built-in node descriptors when reducing package count; no independent runtime.

16. **`internal/automation/nodes/script` — 30 lines.** Workflow descriptor for isolated JavaScript transform execution. Consumers: `internal/app`. **Verdict:** Keep or fold descriptor registration; keep worker runtime independent.

17. **`internal/automation/nodes/workflow` — 52 lines.** Authorable workflow-call descriptor and internal binding/output nodes. Consumers: `internal/app`. **Verdict:** Keep; compiler nodes have concrete nested-call semantics, not spare extensibility.

18. **`internal/automation/recipe` — 439 lines.** Typed input binding, JSON references/pointers and stable topological preparation. Consumers: `internal/automation/agenttools`, `internal/automation/engine`, `internal/automation/nodes/workflow`, `internal/automation/service` and 2 other production package(s). **Verdict:** Keep; real graph boundary shared with UI and engine.

19. **`internal/automation/records` — 112 lines.** Shared schema-validated record read/write operation and resource contract. Consumers: `internal/automation/agenttools`, `internal/automation/nodes/record`, `internal/tools`. **Verdict:** Keep; avoids duplicate agent/node storage logic.

20. **`internal/automation/script` — 95 lines.** Goja evaluation and narrow subprocess worker protocol. Consumers: `cmd/eval`, `cmd/gektor`, `internal/automation/nodes/script`. **Verdict:** Keep; bounded interpreter execution is a real feature boundary.

21. **`internal/automation/service` — 1,338 lines.** Authorized recipe snapshots/compilation/execution/review and queue coordination. Consumers: `internal/app`, `internal/automation/agenttools`, `internal/automation/api`, `internal/automation/daemon` and 1 other production package(s). **Verdict:** Keep; public JSON tool-like entry methods can be narrowed later, but compiler/store/engine separation is justified.

22. **`internal/automation/store` — 1,265 lines.** Workspace-scoped run/record/artifact persistence, leases, queue and schedules. Consumers: `cmd/eval`, `cmd/gektor`, `internal/automation/agenttools`, `internal/automation/api` and 4 other production package(s). **Verdict:** Keep; large but coherent durability owner; splitting files into packages would expose locking internals.

23. **`internal/capabilities/documents` — 920 lines.** Bounded DOCX/XLSX/PDF parsing, item/text continuation and worker IO. Consumers: `cmd/eval`, `cmd/gektor`, `internal/capabilities/documents/contract`, `internal/capabilities/documents/nodes` and 2 other production package(s). **Verdict:** Keep; reusable service independent of both execution engines.

24. **`internal/capabilities/documents/contract` — 124 lines.** Shared JSON schema, validation, resources, retry/read-only metadata for documents. Consumers: `internal/capabilities/documents/nodes`, `internal/capabilities/documents/tools`, `internal/roles`, `internal/tools`. **Verdict:** Keep; shared semantics earn this boundary; automation dependency is contract/schema support, not engine coupling.

25. **`internal/capabilities/documents/nodes` — 19 lines.** Workflow descriptor adaptation of shared documents operations. Consumers: `internal/app`. **Verdict:** Merge optional D5; one combined capability-node adapter package preserves domain independence.

26. **`internal/capabilities/documents/tools` — 11 lines.** Agent adaptation of shared documents operations. Consumers: `internal/roles`, `internal/tools`. **Verdict:** Merge optional D5 into existing tools composition; retain behavior-bearing methods and exact names.

27. **`internal/capabilities/files` — 970 lines.** Bounded downloads/archive listing/creation/extraction and rooted publication. Consumers: `internal/capabilities/files/contract`, `internal/capabilities/files/nodes`, `internal/roles`, `internal/tools`. **Verdict:** Keep; reusable service independent of both execution engines.

28. **`internal/capabilities/files/contract` — 166 lines.** Shared JSON schema, validation, resources, retry/read-only metadata for files. Consumers: `internal/capabilities/files/nodes`, `internal/capabilities/files/tools`, `internal/roles`, `internal/tools`. **Verdict:** Keep; shared semantics earn this boundary; automation dependency is contract/schema support, not engine coupling.

29. **`internal/capabilities/files/nodes` — 21 lines.** Workflow descriptor adaptation of shared files operations. Consumers: `internal/app`. **Verdict:** Merge optional D5; one combined capability-node adapter package preserves domain independence.

30. **`internal/capabilities/files/tools` — 16 lines.** Agent adaptation of shared files operations. Consumers: `internal/roles`, `internal/tools`. **Verdict:** Merge optional D5 into existing tools composition; retain behavior-bearing methods and exact names.

31. **`internal/capabilities/images` — 538 lines.** Rooted input staging, Pixelita processing, source verification and output publication. Consumers: `internal/capabilities/images/contract`, `internal/capabilities/images/nodes`, `internal/capabilities/images/tools`. **Verdict:** Keep; reusable service independent of both execution engines.

32. **`internal/capabilities/images/contract` — 104 lines.** Shared JSON schema, validation, resources, retry/read-only metadata for images. Consumers: `internal/app`, `internal/automation/agenttools`, `internal/capabilities/images/nodes`, `internal/capabilities/images/tools` and 1 other production package(s). **Verdict:** Keep; shared semantics earn this boundary; automation dependency is contract/schema support, not engine coupling.

33. **`internal/capabilities/images/nodes` — 18 lines.** Workflow descriptor adaptation of shared images operations. Consumers: `internal/app`. **Verdict:** Merge optional D5; one combined capability-node adapter package preserves domain independence.

34. **`internal/capabilities/images/tools` — 30 lines.** Agent adaptation of shared images operations; includes explicit session attachment input grants. Consumers: `internal/roles`, `internal/tools`. **Verdict:** Merge optional D5 into existing tools composition; retain behavior-bearing methods and exact names.

35. **`internal/capabilities/media` — 1,114 lines.** Native YouTube and explicit yt-dlp/FFmpeg operations with bounded staging/publication. Consumers: `internal/app`, `internal/capabilities/media/contract`, `internal/capabilities/media/nodes`, `internal/tools`. **Verdict:** Keep; reusable service independent of both execution engines.

36. **`internal/capabilities/media/contract` — 223 lines.** Shared JSON schema, validation, resources, retry/read-only metadata for media. Consumers: `internal/capabilities/media/nodes`, `internal/capabilities/media/tools`, `internal/tools`. **Verdict:** Keep; shared semantics earn this boundary; automation dependency is contract/schema support, not engine coupling.

37. **`internal/capabilities/media/nodes` — 18 lines.** Workflow descriptor adaptation of shared media operations. Consumers: `internal/app`. **Verdict:** Merge optional D5; one combined capability-node adapter package preserves domain independence.

38. **`internal/capabilities/media/tools` — 15 lines.** Agent adaptation of shared media operations. Consumers: `internal/tools`. **Verdict:** Merge optional D5 into existing tools composition; retain behavior-bearing methods and exact names.

39. **`internal/capabilities/social` — 213 lines.** Bounded FxTwitter/public-post retrieval and status normalization. Consumers: `internal/app`, `internal/capabilities/social/contract`, `internal/capabilities/social/nodes`, `internal/tools`. **Verdict:** Keep; reusable service independent of both execution engines.

40. **`internal/capabilities/social/contract` — 61 lines.** Shared JSON schema, validation, resources, retry/read-only metadata for social. Consumers: `internal/capabilities/social/nodes`, `internal/capabilities/social/tools`, `internal/tools`. **Verdict:** Keep; shared semantics earn this boundary; automation dependency is contract/schema support, not engine coupling.

41. **`internal/capabilities/social/nodes` — 13 lines.** Workflow descriptor adaptation of shared social operations. Consumers: `internal/app`. **Verdict:** Merge optional D5; one combined capability-node adapter package preserves domain independence.

42. **`internal/capabilities/social/tools` — 13 lines.** Agent adaptation of shared social operations. Consumers: `internal/tools`. **Verdict:** Merge optional D5 into existing tools composition; retain behavior-bearing methods and exact names.

43. **`internal/checks` — 248 lines.** Report-only deterministic findings, deduplication and bounded changed-file sweep. Consumers: `cmd/gektor`, `internal/roles`, `internal/rules`, `internal/run` and 1 other production package(s). **Verdict:** Keep; shared rules/reporting owner rather than loop self-verification.

44. **`internal/clipboard` — 183 lines.** Windows clipboard image acquisition and bounded PNG/DIB normalization. Consumers: `internal/tui`. **Verdict:** Keep; OS-specific adapter with a clear non-Windows result.

45. **`internal/config` — 533 lines.** Strict profiles, preferences, editing and atomic save behavior. Consumers: `cmd/eval`, `cmd/gektor`, `internal/app`, `internal/roles` and 3 other production package(s). **Verdict:** Simplify B1/D7; one strict decoder, remove retired keys if no migration outcome is required.

46. **`internal/conventions` — 12 lines.** Test-only enforcement of documentation/package/build rules; production doc stub. Consumers: no production importer (test organization). **Verdict:** Keep as test organization; do not count it as an application service.

47. **`internal/credentials` — 143 lines.** Opaque origin/header-bound MCP secrets with Windows DPAPI protection. Consumers: `internal/mcpmanager`, `internal/tools`. **Verdict:** Keep; not the same contract as automation environment references or OAuth rotating accounts.

48. **`internal/events` — 193 lines.** JSONL vocabulary, sequence owner and in-process event adaptation. Consumers: `cmd/gektor`, `internal/agentstdio`, `internal/run`. **Verdict:** Simplify D1; keep wire ownership, remove serialization as internal dispatch mechanism.

49. **`internal/gektor` — 26 lines.** Resolve paths under the user data directory. Consumers: `cmd/gektor`, `internal/app`, `internal/roles`, `internal/tools` and 1 other production package(s). **Verdict:** Keep; 26-line product-specific path authority, optional userdirs rename is stylistic.

50. **`internal/httptransport` — 279 lines.** Public/loopback URL policy and DNS-validated HTTP dialing. Consumers: `internal/automation/nodes/http`, `internal/capabilities/files`, `internal/capabilities/media`, `internal/capabilities/social` and 1 other production package(s). **Verdict:** Keep; several actual consumers; model endpoints intentionally use separate trusted connection policy.

51. **`internal/llm` — 1,334 lines.** Provider-neutral messages/requests, replay state, context estimation, attachments, parsing and retries. Consumers: `cmd/eval`, `cmd/gektor`, `internal/agent`, `internal/app` and 10 other production package(s). **Verdict:** Keep; concrete cross-protocol contract. Shared schema/default constants can be deduplicated locally if touched.

52. **`internal/llm/chatcompletions` — 1,147 lines.** Chat Completions client, unary/SSE decoding and three request dialects. Consumers: `internal/app`. **Verdict:** Simplify B3/D4; share preparation/errors, retain dialects and separate stream accumulator; rename Server to Client.

53. **`internal/llm/responses` — 518 lines.** OAuth Responses request encoding, stream parsing and scoped native-item replay. Consumers: `internal/app`. **Verdict:** Keep; protocol semantics cannot be reduced to a Chat Completions dialect.

54. **`internal/mcpmanager` — 374 lines.** Saved MCP definitions, secrets references and launch-scoped connection administration. Consumers: `cmd/gektor`, `internal/tui`. **Verdict:** Keep; persistence/admin differs from wire transport and per-session activation.

55. **`internal/permission` — 230 lines.** Last-match rules, resource matching, operator answer parsing and authorization. Consumers: `cmd/eval`, `cmd/gektor`, `internal/agent`, `internal/agentstdio` and 5 other production package(s). **Verdict:** Keep; common decision path, deliberately permissive default is policy, not missing DI.

56. **`internal/process` — 265 lines.** Native finite process execution, process groups, window policy and environment scrubbing. Consumers: `internal/automation/nodes/process`, `internal/capabilities/media`, `internal/programs`, `internal/tools`. **Verdict:** Keep; shared low-level owner; agent streaming/background contracts remain different.

57. **`internal/programs` — 203 lines.** Operator executable settings, version probing and bounded observation cache. Consumers: `internal/app`, `internal/capabilities/media`, `internal/tools`. **Verdict:** Keep; PATH resolution/configuration differs from running arbitrary commands.

58. **`internal/prompts` — 127 lines.** Embedded instructions, presets, prompt digest and platform/environment text. Consumers: `cmd/eval`, `cmd/gektor`, `internal/agent`, `internal/app` and 4 other production package(s). **Verdict:** Keep; plain embedded files, no unnecessary template engine.

59. **`internal/providers` — 123 lines.** Fixed typed provider metadata and endpoint/reasoning constraints. Consumers: `cmd/gektor`, `internal/app`, `internal/config`, `internal/tui` and 1 other production package(s). **Verdict:** Keep; useful declarative catalog without dynamic plugins.

60. **`internal/providers/internal/probe` — 72 lines.** Shared provider metadata HTTP helpers and model-list response shape. Consumers: `internal/providers/koboldcpp`, `internal/providers/llamacpp`, `internal/providers/openaicompat`. **Verdict:** Keep; private helper with three real inspection consumers.

61. **`internal/providers/koboldcpp` — 54 lines.** Kobold metadata endpoints and known vision refusal. Consumers: `internal/app`. **Verdict:** Keep; small protocol-independent adapter; model field can be removed if unused.

62. **`internal/providers/llamacpp` — 65 lines.** llama props/model/vision inspection. Consumers: `internal/app`. **Verdict:** Keep; consider fetching one props snapshot per inspection instead of three identical endpoint reads.

63. **`internal/providers/openai` — 659 lines.** Verified OAuth login/logout, rotating credentials, account binding and model catalog. Consumers: `cmd/gektor`, `internal/app`. **Verdict:** Keep; lifecycle is separate from inference client; DPAPI helper duplication is small localized IO boilerplate.

64. **`internal/providers/openaicompat` — 153 lines.** Compatible model discovery plus explicit heuristic capability/context fallback. Consumers: `internal/app`. **Verdict:** Keep; label heuristics as estimates; unknown model default does not prove its actual window.

65. **`internal/release` — 509 lines.** Concrete runtime manifest, staging, reproducible archive bytes and verification. Consumers: `cmd/release`. **Verdict:** Keep; ordinary packager with no runtime dependencies or plugin framework.

66. **`internal/retrospect` — 598 lines.** Isolated archive review, evidence-checked proposals and approved skill publication/undo. Consumers: `internal/tui`. **Verdict:** Keep for the experimental feature; deletion is a product scope cut, not equivalent simplification.

67. **`internal/roles` — 591 lines.** Build interactive/planner/subagent/retrospective/workflow-author policies and tool sets. Consumers: `cmd/eval`, `cmd/gektor`, `internal/retrospect`, `internal/run` and 1 other production package(s). **Verdict:** Keep; simplify factory dependencies D3; retain role boundary resolving agent/tool import direction.

68. **`internal/rules` — 57 lines.** Go formatting/syntax finding implementation. Consumers: `cmd/gektor`. **Verdict:** Keep; small implementation of checks.Rule; merging with checks is possible but marginal.

69. **`internal/run` — 661 lines.** Active session facade, role selection, gates, event wiring, checks and final reports. Consumers: `cmd/gektor`, `internal/agentstdio`, `internal/tui`. **Verdict:** Simplify B2/D1; keep Continue versus Resume as separate public intents.

70. **`internal/session` — 502 lines.** Durable history/context snapshots, runs, child archives and inspection metadata. Consumers: `cmd/gektor`, `internal/agent`, `internal/retrospect`, `internal/roles` and 2 other production package(s). **Verdict:** Keep; absorb title persistence D6 rather than importing workspace.

71. **`internal/tools` — 9,147 lines.** Agent tool composition plus native tools, adapters, browser/background/output lifetimes. Consumers: `cmd/eval`, `cmd/gektor`, `internal/mcpmanager`, `internal/roles` and 1 other production package(s). **Verdict:** Simplify D3; broad cohesive cohorts warrant future MCP transport extraction, not one package per tool.

72. **`internal/tui` — 15,113 lines.** Root terminal navigation, transcript, dialogs, application actions and workflow screen. Consumers: `cmd/gektor`. **Verdict:** Simplify D2; extract complete workflow screen ownership, retain root focus/navigation authority.

73. **`internal/tui/components/cell` — 64 lines.** Terminal-cell fitting and block geometry. Consumers: `internal/tui`, `internal/tui/components/completion`, `internal/tui/components/form`, `internal/tui/components/gate` and 6 other production package(s). **Verdict:** Keep; multiple independent control consumers. Truncate currently counts runes despite its cell-oriented comment; clarify/correct when touched.

74. **`internal/tui/components/completion` — 271 lines.** Completion list selection, popup geometry and pointer interaction. Consumers: `internal/tui`. **Verdict:** Keep; concrete present control, not application command execution.

75. **`internal/tui/components/composer` — 131 lines.** Multiline input history/recall and caret token replacement. Consumers: `internal/tui`. **Verdict:** Keep; builds on textarea rather than replacing its editing engine.

76. **`internal/tui/components/form` — 467 lines.** Field editor/navigation, actions, rendering and shared choices. Consumers: `internal/tui`, `internal/tui/components/schemaform`, `internal/tui/components/table`, `internal/tui/settings`. **Verdict:** Keep; real reuse by settings and workflow forms; optional rendering hooks serve existing users.

77. **`internal/tui/components/gate` — 196 lines.** Permission/question option selection and bounded popup rendering. Consumers: `internal/tui`. **Verdict:** Keep; parent owns answers/authorization, component owns interaction.

78. **`internal/tui/components/graph` — 626 lines.** Node/port drawing, bounded orthogonal routing and graph navigation. Consumers: `internal/tui`, `internal/tui/workflow`. **Verdict:** Keep; move workflow status presentation out (D6). A* routing is visual product complexity, not an execution framework.

79. **`internal/tui/components/pane` — 292 lines.** Pane frames, split rectangles, tabs, notices and toolbar geometry. Consumers: `internal/tui`. **Verdict:** Keep; small shared rendering/layout cohort.

80. **`internal/tui/components/popup` — 397 lines.** Popup surfaces, overlay splicing, menu hits, scrollbars and shortcut layout. Consumers: `internal/tui`, `internal/tui/components/completion`, `internal/tui/components/form`, `internal/tui/components/gate` and 3 other production package(s). **Verdict:** Keep; reused widely, no application persistence/services.

81. **`internal/tui/components/schemaform` — 119 lines.** Map supported JSON object properties into form fields and preserve unknown JSON. Consumers: `internal/tui`. **Verdict:** Keep; deliberately narrow adapter, not a universal schema-driven UI platform.

82. **`internal/tui/components/selection` — 43 lines.** List cursor/offset clamping and movement. Consumers: `internal/tui`, `internal/tui/components/table`, `internal/tui/settings`. **Verdict:** Keep; 43 lines with settings/table consumers; merging into popup would add an unrelated dependency.

83. **`internal/tui/components/table` — 615 lines.** Searchable selectable rows, direct/overflow actions and matching hit geometry. Consumers: `internal/tui`. **Verdict:** Keep; substantial present behavior shared by multiple application dialogs.

84. **`internal/tui/components/transcript` — 463 lines.** Part-based visible-only viewport, following/anchoring and action presentation. Consumers: `internal/tui`. **Verdict:** Keep; custom viewport has a concrete large-history rendering rationale.

85. **`internal/tui/settings` — 1,662 lines.** Staged profile edits, settings forms and asynchronous model/file pickers. Consumers: `internal/tui`. **Verdict:** Keep; real screen-component boundary; rename Workspace staging type for clarity (D6).

86. **`internal/tui/workflow` — 540 lines.** Pure recipe edit/undo, connection mutations and graph mapping. Consumers: `internal/tui`. **Verdict:** Keep and grow into complete workflow screen ownership D2, preserving pure editor helpers.

87. **`internal/worker` — 131 lines.** Current-executable JSON worker process and platform memory/resource limits. Consumers: `internal/automation/script`, `internal/capabilities/documents`. **Verdict:** Keep; parser/script worker contract differs from general native process results.

88. **`internal/workspace` — 881 lines.** Rooted file access, instruction/skill discovery and observed workspace changes. Consumers: `cmd/eval`, `cmd/gektor`, `internal/agentstdio`, `internal/app` and 19 other production package(s). **Verdict:** Keep; move conversation title persistence to session D6; no need to fragment rooted access into utility packages.

## Recommended sequence

1. Fix B1/B2/B3 with focused behavior checks. These are defects, not reasons for a broad architecture rewrite.
2. Remove the event encode/decode round trip and fully initialize tool dependencies at construction (D1/D3). Keep existing external event/tool names and permission behavior.
3. Share the Chat Completions request builder/error decoder (D4).
4. Extract the workflow screen as a concrete state/effect owner (D2), one navigation path at a time. This improves ownership more than reducing directory count.
5. Apply low-risk naming/title/retired-setting cleanup (D6/D7), then decide whether the optional adapter-package consolidation is worth the import churn (D5). No legacy shim packages.

The application can be made more ordinary without sacrificing outcomes. Its core agent/provider/workflow boundaries are mostly earned; its event bus, late authorization wiring and root-screen ownership are not equally convincing. Most safe savings are fewer parallel paths and clearer owners, not thousands of removable feature lines. A smaller product would require choosing which real capabilities to stop supporting.
