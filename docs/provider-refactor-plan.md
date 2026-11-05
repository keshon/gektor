# Provider and protocol boundary refactor

Status: implemented locally; offline verification and live ChatGPT acceptance passed.
Date: 2026-11-05.
Review baseline: `codex/chatgpt-backend`, commit `4ff7da3`.
Review history: initial implementation audit, independent second opinion, then
three sequential reviewers (third, fourth, fifth), each receiving the cumulative
findings of its predecessor and inspecting the code independently. The final
three reviews were read-only and ran no additional tests or live requests.

## Objective

Keep one agent loop while making provider selection declarative and giving
protocol adapters, authentication, and application composition distinct owners.
Adding a provider using an existing protocol should mostly require a descriptor.
Adding a new protocol requires a typed implementation and contract tests.

Use typed Go descriptors first. Do not introduce a plugin framework, external
YAML registry, configurable protocol language, or unrelated agent/TUI rewrite.

## Problems established by the review

1. ChatGPT is registered as a dialect but bypasses Chat Completions through
   special cases in `Server.Chat` and `Server.Stream`.
2. Provider IDs, endpoint restrictions, defaults, and supported settings are
   repeated across config, CLI, discovery, and TUI code.
3. Responses transport selects the global credential store and active account
   on each request rather than receiving a launch-bound credential source.
4. Responses interruption discards text already delivered to streaming callbacks.
   The agent cannot archive the partial answer. A focused regression probe
   reproduced this failure.
5. Responses decoding merges message items and keeps only the last phase;
   replay also hoists reasoning ahead of all messages. A focused regression
   probe reproduced commentary/final-answer boundary loss.

Existing `go test ./...` and `go vet ./...` passed during the review. Temporary
regression probes were removed. Live OAuth and hosted inference were not tested.

## Implemented directory structure

This is the affected portion of the final tree. Existing unrelated packages
remain in place. Tests live beside their implementations; filenames can be
combined where that improves navigation without changing ownership.

```text
cmd/
  gektor/                    # CLI flags, launch entrypoint, auth commands
  eval/                      # Uses the same composition and provider catalog
internal/
  llm/                       # Provider-neutral agent contracts; no adapter imports
    client.go                # Messages, requests, responses, Client, Streamer
    capabilities.go          # Resolved capabilities and model facts
    state.go                 # Versioned opaque replay-state envelope
    errors.go                # Shared error classification
    retry.go                 # Shared retry policy
    context.go               # Provider-neutral context accounting
    images.go                # Shared image helpers, no provider selection
    leakparse.go             # Neutral recovery helpers currently used by agent
    arguments.go             # Shared tool-argument normalization
    think.go                 # Shared reasoning normalization
    chatcompletions/
      client.go              # Chat Completions client and request encoding
      stream.go              # Chat Completions SSE decoding
      dialect.go             # Variations within this protocol only
      openai_dialect.go      # Standard compatible request fields
      llamacpp_dialect.go     # llama.cpp request grammar and sampling
      koboldcpp_dialect.go    # Kobold request grammar and sampling
      images.go              # Chat Completions image wire format
    responses/
      client.go              # Responses transport with injected credentials
      responses.go           # Wire encoding, SSE assembly, validated ordered replay
  providers/
    catalog.go               # Typed descriptors, endpoint/settings policy, lookup
    llamacpp/
      inspection.go          # Loaded model, context, vision and model list
    koboldcpp/
      inspection.go          # Kobold metadata endpoints
    openaicompat/
      inspection.go          # Model discovery, capability metadata and fallbacks
    internal/probe/
      http.go                # Shared HTTP metadata helpers, no provider selection
    openai/
      oauth.go               # ChatGPT registration, login, logout, renewal
      store.go               # Account-bound token sources and credential storage
      store_windows.go       # Windows credential protection and locking
      store_unix.go          # Unix credential storage and locking
      models.go              # Authenticated ChatGPT model catalog
  app/
    backend.go               # Descriptor + profile + account -> client/runtime
    discovery.go             # Discovery orchestration and run refresh callbacks
    inspection.go            # Inspector factories, interface and backend detection
    transport.go             # HTTP/auth/header wiring shared by launch paths
    services.go              # Existing application lifecycle
  config/
    profile.go               # Persisted choices, validated against catalog
    ...
  tui/
    settings/
      catalog.go             # UI projection of provider metadata
      fields.go              # Fields derived from supported settings
      ...
  agent/                     # Existing loop, tools, permissions, compaction
  roles/                     # Run environment and local model-facts application
  run/                       # Refreshes facts at run/resume boundary
docs/
  provider-refactor-plan.md
```

`providers/openai` remains an account-service package. Its existence is useful;
it must not become a second inference implementation. Generic OpenAI-compatible
API behavior belongs to `llm/chatcompletions`, while Responses belongs to its
own protocol adapter. A protocol package must not import a credential store.

## Ownership and dependencies

- `llm` defines the language spoken by the agent. It imports neither concrete
  adapters nor `providers`. Keep protocol-specific payloads opaque here.
- `llm/chatcompletions` and `llm/responses` implement that contract. They own
  encoding, stream decoding, protocol errors, and native replay semantics.
- `providers` declares available integrations. Descriptors contain data and
  typed strategy identifiers, not executable UI logic or embedded credentials.
  Its root package must not import config, app, adapters, or account services.
  A single explicit constructor switch in app is acceptable.
- `providers/openai` owns account operations and refresh serialization. An
  injected token source is bound to a registration and verified subject.
  Refresh resolves that registration under the existing cross-process lock;
  changing the global selection cannot redirect a running client. Logout,
  removal, or identity replacement must fail rather than select another account.
  A narrow token function/interface is sufficient; no custom transport framework.
- `app` wires catalog entries to adapter constructors and account services.
  This is the single place where concrete implementations meet. It also owns
  discovery orchestration and supplies the pre-run model-facts callback.
- `config` validates saved choices against descriptors. CLI and TUI present the
  same defaults, endpoint policy, auth mode, and supported settings.
- Model discovery returns model facts separately from provider defaults. Vision
  support and context size can be supported, unsupported, or unknown; preserve
  their source rather than turning an estimate into a measured fact.

Do not expose one oversized backend interface. Keep `Client`/`Streamer`, and
introduce only the small metadata/usage interfaces actually needed by callers.
Replace existing `*llm.Server` assumptions in launch, sessions, and eval paths.
Keep usage counters client-owned initially, with narrow usage access where needed.
Keep agent-used recovery helpers neutral; moving recovery into adapters would
be a separate behavior change, not part of mechanical extraction.

## Declarative descriptor

A descriptor should identify the provider, display label, protocol, auth mode,
default endpoint, endpoint restrictions, dialect where applicable, model
discovery strategy, supported generation settings, and fallback policies.
Consumers derive choices and visibility from these facts, not string comparisons
such as `kind == "chatgpt"` spread throughout the application.

Endpoint restrictions for OAuth remain enforced at construction and profile
validation through the same policy. Capabilities must describe whether output
limits are server-enforced; a context-budget reservation is not an output cap.
Keep model-specific facts and user overrides separate from static descriptors.
Generation settings may also depend on the selected model and route. Do not
create a generic form engine or a combinatorial set of capability booleans.

The Responses package initially implements the current ChatGPT integration.
Its namespaces, sampling/output-limit restrictions, and subscription errors are
explicit integration rules, not universal properties of the Responses protocol.
Do not add hypothetical policies or claim arbitrary Responses endpoint support.

## Discovery and run snapshots

Discovery and inference share connection identity, endpoint/header policy, and
bound credentials. They can use different timeouts: discovery must remain bounded
without shortening inference requests.

Carry a narrow optional `RefreshModelFacts(context.Context)` callback on
`roles.Env`, implemented by application composition and returning neutral llm
facts with explicit known/unknown values. A small helper merges known results
into a local environment copy, preserving configured overrides and established
precedence. Keep partial-success and best-effort behavior.

Invoke it in `run.Session.build` for Run/Resume, before direct CLI role
construction, and at the corresponding eval boundary where needed. Do not probe
inside every role constructor: subagents inherit the parent run's facts snapshot.
Facts remain stable during a run; no background capability mutation is introduced.

TUI settings application must replace the client and its refresh callback
together. A new connection must never retain the previous connection's probe.

## Replay contract

The canonical transcript remains authoritative. Native replay must never restore
actions or text that the agent deliberately removed or changed.

Retain ordered native Responses output separately from display content. Issue a
versioned envelope only after complete validation that the native items correspond
to their normalized projection. Do not certify raw output that decoding rejected,
dropped, repaired, or did not understand. Copy native bytes on capture and treat
the envelope as immutable; avoid shared mutable backing slices.

Record integration/endpoint, model, non-secret registration/subject scope, and a
deterministic fingerprint of the original model-facing projection: role, content,
images, and ordered valid calls including IDs, names, and arguments. Exclude UI
and delivery metadata. Byte-sensitive arguments are sufficient initially; no
elaborate JSON canonicalization is required.

At request time, first apply ordinary model-message filtering, then check envelope
version, compatibility scope, and projection equality. Only then replay native
items. Otherwise discard the entire envelope, including reasoning and phase
metadata, and encode the normalized transcript. Preserve valid call IDs and
paired results. Never resurrect rejected calls or surgically reconcile reasoning.

This covers `forceAnswer`, recovered leaked calls, rejected-call filtering,
content edits, interruption, and compaction. Tests must inspect actual subsequent
request JSON for absence of removed calls, not merely check a nil state field.

Continue loading legacy `provider_state` JSON, but ignore native state without
established provenance while preserving normal content/calls. Never invent an
account identity. Compaction must preserve complete call/result groups.

Whole-envelope fallback is intentionally lossy. Verify its wire integrity offline;
actual ChatGPT acceptance after encrypted reasoning is discarded requires live
acceptance. If rejected, return an actionable compatibility error. Never retry
using invalidated native state.

Interrupted streams return partial text/reasoning with their error on every
terminal failure path: cancellation, EOF, scanner errors, malformed events/output,
and failed/incomplete events. A late invalid tool item cannot erase text already
shown. Clear native replay state on failure; incomplete calls must never execute.

## Replay token accounting

Count normalized text and calls once. Native metadata reports only additional
replay overhead, not the total cost of raw items containing that same text.
Projection-invalid state contributes no native overhead.

The neutral estimator currently runs before adapter encoding and lacks bound
account/model compatibility context. Accept conservative overestimation for state
that later fails compatibility checks. Wire validation remains mandatory.
Add budgeting regressions for valid overhead, no double-counting, and conservative
fallback behavior. Introduce a narrow pre-budget compatibility normalization only
if measured overestimation materially causes premature compaction; do not add a
general request-preparation framework now.

## Implementation sequence

1. **Permanent regressions and bounded stream fixes.** Preserve partial output
   on all failure paths; establish ordered-decoding tests. Do not enable new
   reusable native-state persistence before compatibility identity exists.
2. **Application construction and account binding.** Introduce composition around
   current clients first. Route CLI, TUI/settings, RPC, and eval through it;
   inject bound credentials and establish compatibility identity and usage access.
3. **Ordered replay and invalidation.** Add validated persisted envelopes,
   projection checks, normalized fallback, legacy-state handling, mutation and
   compaction coverage, and additional native-overhead accounting.
4. **Typed catalog.** Consolidate provider IDs, defaults, endpoint/auth policy,
   discovery strategy, and supported settings. Preserve profile serialization.
5. **Discovery and run snapshots.** Centralize discovery and replace concrete
   server probing with the injected callback and local known-facts merge.
6. **Mechanical protocol extraction.** Separate implementations and their tests
   after callers use stable contracts. Keep neutral helpers in llm.
7. **Compatibility and documentation.** Complete offline checks and update
   architecture/CLI/TUI docs. Report live endpoint acceptance separately.

Keep each step reviewable and working. Do not start with a large file move that
hides correctness fixes inside mechanical changes.

## Acceptance checks

- Protocol-specific tests cover completed answers, tool round trips, usage,
  overflow, and retry classification. Extract common helpers only when justified.
- Stream tests cover multiple phases/interleaved items, cancellation, EOF,
  malformed SSE, failed/incomplete events, valid text followed by invalid tool
  output, partial archival, and no execution from interrupted responses.
- Replay tests cover exact ordered round trips, save/resume, rejected calls,
  forced answers, recovered calls, content/call mutation, version/model/account/
  endpoint changes, legacy state, compaction, and tool call/result pairing.
- Auth tests cover account selection changes, bound-account renewal, concurrent
  refresh serialization, logout/removal, replacement identity, and withdrawn
  permissions. Use isolated stores.
- Facts/discovery tests cover shared connection identity, partial success,
  unknown versus unsupported vision, local model replacement between runs,
  stable facts during a run, settings client/probe replacement, and CLI/TUI/RPC
  paths. Preserve `TestLocalBackendWindowAndModelRefreshBetweenTurns` behavior.
- Catalog tests establish agreement between profile validation, CLI choices,
  endpoint policies, and settings visibility/defaults.
- Existing llama/Kobold behavior, grammar/sampling, discovery, streaming, and
  prefix-stability tests continue to pass.
- Run `go test ./...` and `go vet ./...`; run focused race tests where supported.
- Perform an explicitly identified live ChatGPT smoke test when authenticated:
  answer, local tool round trip, cancellation/resume, a subsequent turn, and
  normalized continuation after native replay invalidation.
  Report live acceptance separately from offline test results.
- No credentials in profiles, logs, or transcripts. No production code changes
  were authorized by saving the plan alone; implementation was subsequently requested.

## Sequential review conclusions

- **Third opinion:** accepted the boundary split; added projection-checked replay,
  account-registration pinning, native-overhead accounting, dependency-light
  catalog rules, and discovery timeout separation.
- **Fourth opinion:** required validation before issuing replay envelopes;
  selected whole-envelope fallback; corrected the assumption that an adapter
  alone can remove incompatible overhead before neutral budgeting; moved account
  binding ahead of persistent replay.
- **Fifth opinion:** selected `roles.Env` for the refresh hook to cover direct
  CLI as well as sessions; required atomic client/probe replacement in settings,
  immutable native bytes, and partial-output preservation on all failure paths.

All three supported the architecture, but this is a sequential critique chain,
not three blind independent votes. Recommendations were retained for their code
evidence and concrete behavioral consequences rather than agreement alone.

Normalized tool continuation after discarding native reasoning passed live
acceptance below. The practical impact of conservative replay-cost overestimation
remains unmeasured; it does not justify broadening the architecture without evidence.


## Implementation record

- Created `plan-review-chain` in the personal Codex skills directory and validated
  its frontmatter and resources with the skill-creator validator.
- Implemented independent protocol packages; the neutral package contains no
  provider or adapter imports. Existing wire tests moved with their adapters.
- Added application construction, registration-bound credentials, typed provider
  catalog, injected discovery, and per-run facts snapshots. Settings retain their
  existing relaunch lifecycle, so client and probe are replaced together.
- Implemented validated ordered replay, projection-based fallback, legacy-state
  loading, additional-overhead estimates, and partial output preservation through
  both streaming and retry paths.
- Kept `think.go` and argument normalization neutral as well as `leakparse.go`:
  the existing recovery implementation shares those helpers.
- Kept protocol files grouped where practical; did not add an unused shared
  contract-test framework or a file for every conceptual responsibility.
- At initial offline verification, local auth was not connected. This limitation
  was subsequently resolved by the live acceptance run recorded below.

- The implementation audit found two additional issues, both corrected with
  regression tests: probes/discovery now share inference auth and extra headers;
  strict evaluation requires actual backend evidence instead of a configured
  context fallback. Startup metadata requests have bounded timeouts.
- A rejected normalized continuation now provides a recovery instruction without
  retrying incompatible native state. Bound-account tests also cover local logout
  and withdrawn permissions.

Final offline verification (2026-11-05): `go test ./...`, `go vet ./...`,
`go test -race ./internal/llm/... ./internal/providers/openai ./internal/app`,
`git diff --check`, and the personal skill validator all passed. Changes remain
uncommitted. Live acceptance was performed after the user authenticated; see below.

## Follow-up: complete the protocol/inspection boundary

The initial extraction left backend inspection in Chat Completions. The follow-up
separates the request dialect (grammar and sampling fields) from model inspection.
The provider catalog now selects `Inspection` independently of `Dialect`.
`app` composes the inference client and inspector using the same authenticated
HTTP connection. Inspection implementations live under the provider packages;
model heuristics, discovery endpoints and backend detection no longer live in
`llm/chatcompletions`. The protocol client exposes no metadata probe methods.

Moved inspection tests with their responsibilities, retained composition tests
for credentials and discovery, and extended import-boundary enforcement to the
new provider packages. This remains a typed Go catalog with small behavioral
implementations, not a configuration language for arbitrary wire formats.

Follow-up verification passed: `go test ./...`, `go vet ./...`,
`go test -race ./internal/app ./internal/providers/... ./internal/llm/chatcompletions`,
and `git diff --check`. Live-service acceptance subsequently passed as recorded below.

## Live acceptance (2026-11-05)

Authenticated model discovery and `auth test gpt-6.1-sol` succeeded. The opt-in
`TestChatGPTLiveAcceptance` then passed against `gpt-6.1-sol` in about 25 seconds:

- The real agent loop called an isolated local file-reading tool exactly once
  and returned its randomly generated marker.
- Two valid native replay envelopes survived saving/loading. A subsequent turn
  recovered the marker without another tool execution.
- Invalidating saved replay-envelope versions forced normalized tool history;
  the live service accepted continuation and recovered the same marker.
- Cancellation after 24 visible output bytes preserved the exact partial answer
  in the session archive without calls or native state. Resume then completed.

The test uses temporary files and only the fixture-reading tool. It is skipped
unless `GEKTOR_LIVE_CHATGPT_MODEL` is explicitly set; repeat it with:

```powershell
$env:GEKTOR_LIVE_CHATGPT_MODEL = 'gpt-6.1-sol'
try {
    go test ./internal/app -run '^TestChatGPTLiveAcceptance$' -count=1 -v -timeout 4m
} finally {
    Remove-Item Env:GEKTOR_LIVE_CHATGPT_MODEL
}
```

The ordinary app/conventions tests and app vet passed after adding this test.
`gektor.exe` was rebuilt for the existing launcher. These results establish the
listed flows on the tested model and authenticated account, not an exhaustive
matrix of models, account revocation or interactive TUI controls.

## Reasoning controls and skill recovery

ChatGPT effort levels now come from the authenticated model catalog. CLI flags
and Settings > Generation control effort and visible summaries; profiles retain
both values. Explicit settings are validated before inference. An opt-in live
test on 2026-11-05 passed with `gpt-6.1-sol`, high effort and summaries enabled:
43 summary bytes streamed separately from the 566-byte answer.

Skill discovery keeps valid entries when another guide is malformed and reports
warnings through the catalog and `system_info`. The description limit defaults
to 1024 Unicode characters and can be set with `GEKTOR_SKILL_DESCRIPTION_LIMIT`.
The 16 KiB frontmatter limit is unchanged.
