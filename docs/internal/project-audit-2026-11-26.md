# GEKTOR project and harness audit

26 November 2026. Baseline: `60365eeabd158bf32419d8b8b268542277eec898`, clean
at the start. Production source and existing documentation were not changed.
This report and its evidence are new files.

Subsequent fixes are recorded in [the remediation record](audit-remediation-2026-11-26.md).
Findings below describe the audited revision.

GEKTOR is substantially stronger than at the November 20 audit. Shared execution,
explicit verification, archive/context separation and public terminal controls
are useful foundations. The immediate weaknesses are recovery fidelity,
expensive presentation work on the UI thread, and validation/docs that did not
follow the latest feature changes. A new architecture or implementation language
would not solve those problems.

## Scope and method

Three independent reviewers examined runtime recovery, architecture/public
controls, and TUI/documentation. Review covered representative execution,
cancellation, resume, compaction, tool scheduling, permissions, reports, public
package consumption, visual evidence generation and release boundaries.
New deterministic probes exercise real constructors with fake model responses.
No live model benchmark or competitor execution was performed.

The [Markdown inventory](audit-2026-11-26/documentation-inventory.json) classifies
169 tracked Markdown files, including history, skills and fixtures. Semantic
review concentrated on current user/contributor entry points and changed
contracts. It is not a claim that every reference paragraph was verified.
Project tooling checked published links and anchors separately. Old audits and
dated measurements are historical evidence, not automatically stale guidance.

The previous remediation record already records public package extraction and
the user's waiver of native terminal review. Those are not reported as undone
work here. No new native-terminal acceptance verdict is claimed.

## Confirmed implementation findings

Priority reflects repair order. P1 is a substantial interaction regression;
P2 findings affect particular recovery/consumption paths or evidence fidelity.

### F1 — P1: code formatting blocks live transcript updates

[prettier.go at the audited revision](https://github.com/keshon/gektor/blob/60365eeabd158bf32419d8b8b268542277eec898/pkg/tui/components/markdown/prettier.go), lines 43–64,
compiles bundles before starting its per-fence timeout and creates a fresh
runtime for each fence. [transcript_stream.go](../../internal/tui/agent/transcript_stream.go),
line 99, renders the growing answer again on a flush, including closed fences
whose source has not changed. The completed-block cache does not protect this
live path. The scheduled flush interval is 16 ms.

On this Windows host, four valid JavaScript fences containing 38,036 bytes took
897.7 ms and 903.8 ms per render. One 9,509-byte fence took 285.8 ms cold.
Even ten tiny fences totaling 410 bytes took 82.5 ms. These are local diagnostic
measurements, not stable benchmarks or comparisons with another product.

Cache formatting by source/language/options and perform costly work outside the
update loop. Display syntax-colored original source while formatting is pending.
Reject stale results after edits, resize, session replacement and shutdown.
Bound total work for a response, not only each individual fence. Preserve the
single-row tool badges and their independent inspection targets.

Acceptance: streaming suffix updates must not rerun formatters for unchanged
fences; cancellation/input can be processed while formatting is pending.
See [timings](audit-2026-11-26/markdown-latency-output.txt) and
[probe source](audit-2026-11-26/markdown-latency.go.txt).

### F2 — P2: question recovery drops new user input

[internal/harness/session.go](../../internal/harness/session.go), line 123,
calls `ResumeWithAnswer` in the pending-question branch without passing the
supplied note or images. The TUI startup path in
[agent.go](../../internal/tui/agent/agent.go), line 348, supplies both.

The probe resumes a saved question with an answer, the correction
“Do not deploy. Only inspect staging.” and an attachment. Neither correction
nor image reaches the model request. This is silent input loss, not proof that
an actual deployment occurred.

Close the pending assistant/tool group with its answer, then preserve the new
user turn and attachments before generation. Do not put a user message ahead of
unanswered tool calls. Test explicit ResumeAnswer and operator-gate paths,
including cancellation while waiting for the answer.

### F3 — P2: checklist recovery restores attempted actions, not accepted state

[internal/agent/state.go](../../internal/agent/state.go), lines 195–207,
replays the last checklist call's arguments without checking its recorded result.
[run_lifecycle.go](../../internal/agent/run_lifecycle.go), line 176, provides
only the current model projection.

Two probes fail: a rejected “mark done” update replaces a previously accepted
pending item; and a compacted checklist disappears on resume even though its
accepted call remains in the full archive. Both produce an empty OpenTodos
report. This concerns checklist correctness, not a demonstrated file-access
policy bypass.

Restore the latest successfully executed checklist from the original archive,
or persist a separately owned authoritative checklist snapshot. Rejected,
cancelled, withheld and unanswered calls must not become accepted state.
Define conservative handling for older records without explicit result status.

F2/F3 have [three failing regression probes](audit-2026-11-26/harness-resume-failures.txt).
Run without modifying production files:

```powershell
pwsh -NoProfile -File docs/internal/audit-2026-11-26/harness-resume-repro.ps1
```

Expected on the audited revision: exit 1 and all three audit assertions fail.

### F4 — P2: the external-consumer check omits embedded assets

[scripts/check-tui-package.go](../../scripts/check-tui-package.go), line 62,
copies and hashes only `.go` files. Markdown now embeds Prettier bundles and
Clang WASM. The documented checker exits 1:

```text
pattern clang-format/plugin.wasm: no matching files found
```

Copy and hash required embedded inputs, including their licenses, and run the
checker in CI. The ordinary test suite and current CI do not invoke it.

The public imports themselves work against a complete checkout: an independent
external module imported screen and markdown, constructed controls and rendered
JSON successfully. This is a broken verification snapshot, not evidence that
all public consumers fail. See [the check record](audit-2026-11-26/public-package-check.txt).

### F5 — P2 design risk: display formatting repairs broken Rust

The embedded Rust formatter renders
`fn main(){let x=1 println!("{}",x)}` with an inserted semicolon after `1`.
The stored Markdown remains unchanged, but the visible snippet conceals the
syntax error. Commander user messages also use Markdown rendering; Modern user
messages remain literal. This behavior is acknowledged in the package README,
so it is a presentation policy issue rather than an undocumented storage change.

For a coding/debugging tool, keep original code authoritative in the view.
Prefer syntax coloring without repair by default, or make a formatted preview
explicit and optional with a raw view. Validate copy/export behavior against the
original source. See [observed output](audit-2026-11-26/markdown-probe-output.txt).

## What is holding up

- Prior timeout-history and background-output ownership fixes remain in place.
  Normal errors retain history; reviewed process-log access respects session scope.
- Verification has structured dispositions and bounded evidence. Report snapshots
  and outcome publication copy mutable receipt data. A completed run is distinct
  from a passed verification command or a correct overall task.
- Tool validation precedes permission checks and execution. Concurrent read work
  joins before ordered result recording; interrupted calls are closed without
  blindly replaying them. No new security-boundary bypass was confirmed in the
  reviewed paths. This is not a comprehensive security assessment.
- Public TUI controls are real reusable code, not renamed application files.
  Dialog state is private, identities are bound to their owner, and snapshots and
  supplied data are copied. The unrelated workbench exercises that boundary.
- Workflow execution, persistence and release staging retain explicit ownership.
  Current root-module public imports avoid an unnecessary second module.
- The gallery regenerated byte-for-byte and the website checks/build passed.
  Published documentation is materially easier to navigate than before.

Maintainability still needs restraint. The Agent screen has 60 production files;
its transcript state, presentation and caching are a plausible cohesive internal
extraction. `Controls` exposes 95 methods, so public stability needs an ordinary
host API review, not merely moving the package again. These counts are review
signals, not automatic defects. Callback-heavy role configuration and separate
observation/accounting paths remain costs; avoid replacing them with an equally
broad generic framework.

## Documentation findings and proposed cleanup

These are current-document issues. The November 20 reports and their version
pins should remain dated evidence rather than being rewritten to today's state.

| ID | Location at audited revision | Drift and reader impact | Proposed correction |
|---|---|---|---|
| D1 | `docs/design/tui/README.md:13,53,138`; `docs/development/README.md:33`; `docs/development/tui.md:18,163` | Maintained guidance still directs contributors to edit and re-export FigCraft despite the screenshot/gallery workflow | Retire the obsolete asset workflow as a unit; keep current gallery instructions |
| D2 | `docs/guide/sessions.md:63` | Says title generation follows the first answer; it now precedes the task, with an eight-second timeout | Describe upfront generation, fallback and rename protection; explain possible first-turn delay |
| D3 | `ARCHITECTURE.md:185` | Says invalid checkpoints leave previous context intact; non-cancellation failures can use a fitted mechanical projection | Match `internal/agent/checkpoint.go:24–51` and the already-correct sessions guide; distinguish cancellation from summary failure |
| D4 | `docs/reference/configuration.md:47` | States API keys are read from the environment; the same page later documents saved provider keys and key modes | Describe environment or saved-key selection and link to canonical precedence; keep the accurate statement that GEKTOR does not load `.env` |
| D5 | `docs/reference/rpc.md:153` | Lists only completed/failed/cancelled outcomes and text verification; omits `incomplete` and `verification_receipt` | Document the emitted contract, nullable older receipts and explicit verification dispositions; this matters to external clients |
| D6 | `internal/tui/README.md:89` | Test/race/vet/build recipes omit extracted public controls | Include `pkg/tui/...`, following the current testing guide |
| D7 | `pkg/tui/components/markdown/README.md:4` | Says the renderer retains no state; compiled JS caches and a process-lifetime WASM runtime now affect behavior | Say no conversation state is retained; document formatter cache/warm-up lifetime and raw fallback |
| D8 | `docs/design/tui/README.md:80` | Historical mockup prose describes current narrow modals reserving composer space; runtime now lends it to the modal | Remove the runtime assertion with archive retirement; keep interaction rules in the TUI reference |

The gallery currently contains 12 scenes, but no 40×20 comparison or narrow
Commander capture. Behavioral tests cover additional sizes. Extend the gallery
if it is the maintained visual review surface; do not describe it as native
keyboard/mouse acceptance. The previous user waiver of native review remains a
scope decision, not a passing result.

### FigCraft retirement boundary

Remove `docs/design/tui/gektor-ui.figcraft`, its 13 mockup WebPs,
`update-composer.py`, `update-session-workspaces.py` and its JSON input, plus
archive-only README content and incoming editing instructions. Check for other
incoming references before deletion. Git history retains the old design source.

Keep `gallery.html`, `gallery.css`, `gallery-ansi.css` and `export-gallery.py`.
The website consumes these current captures and styles. Keep the design README
path as a short gallery/offline-regeneration guide so existing links still work.
The runtime `skills/figcraft` capability is separate from the obsolete GEKTOR
mockup project; this request is not a reason to remove a user's design tool.

### Documentation ownership and editing rules

| Maintained home | Owns |
|---|---|
| README and docs/start.md | Product introduction and first successful run |
| docs/guide | User tasks, expected behavior and recovery |
| docs/reference | Flags, authentication precedence, wire contracts and persisted formats |
| ARCHITECTURE.md and docs/development | Current implementation boundaries and contributor verification |
| pkg/tui README files | Public imports, API ownership, formatter behavior and consumer checks |
| docs/design/tui | Current gallery inputs, output and regeneration |
| docs/internal | Dated findings, probes, comparison protocols and measurements |

Apply these rules during the cleanup:

1. Give each current behavior one canonical home and link from summaries.
2. Treat source types/defaults as authority for flags, outcomes and key modes.
3. Keep dated measurements tied to their revision; do not silently modernize them.
4. Distinguish source snapshots, rendered fixtures, native review and live model runs.
5. Include extracted public packages in contributor validation commands.
6. Include embedded assets in build proofs and source fingerprints.
7. Document original versus formatted code explicitly wherever evidence is shown.
8. Preserve useful URLs when retiring a workflow; remove obsolete editing steps.
9. Keep runtime skills and test prompts separate from human-documentation cleanup.
10. Check semantic contracts as well as links; a green link checker is insufficient.

## Comparison with current harnesses

Fresh read-only sparse checkouts were taken from upstream default branches.
The older supplied OpenCode checkout was not used as current evidence. These
are source comparisons, not released-version parity or solve-rate benchmarks.

| Project | Reviewed revision | Role in this comparison |
|---|---|---|
| GEKTOR | `60365eeabd158bf32419d8b8b268542277eec898` | Current working baseline |
| OpenCode | `93e053ff898c56366ab7ab63dea1102c07dcf34f` | Full coding product, current core/CLI paths |
| Goose / oi-kit | `d1438fde28d6669e0f34cbd2ae90c5fbe903f8d1` | Rust host application and reusable state-machine operations |
| Orca Harness | `d89bf274c225075ae3f3b1f305a4717e05990ab7` | Smaller Rust kernel, SDK and host separation |

### OpenCode

Its current coordinator separates interrupt acknowledgement from settled cleanup
and provides `awaitIdle`; it serializes a session while allowing different
sessions to run concurrently. Its compaction interface supports background
precomputation, retained recent context, and native provider replacements.
These are useful reference contracts for lifecycle and context evolution.
[Coordinator source](https://github.com/anomalyco/opencode/blob/93e053ff898c56366ab7ab63dea1102c07dcf34f/packages/core/src/session/run-coordinator.ts),
[compaction source](https://github.com/anomalyco/opencode/blob/93e053ff898c56366ab7ab63dea1102c07dcf34f/packages/core/src/session/compaction.ts).

Its CLI includes ACP, and permission composition explicitly separates overrides,
deny restrictions and fallback approvals. GEKTOR's stdio RPC is useful but custom,
and its current RPC run method starts a fresh task rather than attaching to a
persisted session. ACP is a practical integration gap if editor clients become
a target. Permission prompts in either product must not be confused with OS
isolation.
[ACP entry](https://github.com/anomalyco/opencode/blob/93e053ff898c56366ab7ab63dea1102c07dcf34f/packages/cli/src/commands/handlers/acp.ts),
[permission policy](https://github.com/anomalyco/opencode/blob/93e053ff898c56366ab7ab63dea1102c07dcf34f/packages/core/src/permission/policy.ts).

Do not carry forward a blanket claim that current OpenCode editing supplies LSP
diagnostics: the reviewed core edit plugin still explicitly defers LSP,
snapshots/undo and watcher events. Other versions or execution paths may differ.
[Current edit seam](https://github.com/anomalyco/opencode/blob/93e053ff898c56366ab7ab63dea1102c07dcf34f/packages/core/src/tool/plugin/edit.ts#L253-L256).

### Goose and oi-kit

Goose now presents oi-kit as its reusable Rust runtime, marked alpha. The host
owns interface, policy and storage; Goose also exposes an ACP subprocess for
other applications. This is a stronger external agent integration story than
GEKTOR's internal Go harness and custom RPC.
[Project overview](https://github.com/aaif-goose/goose/blob/d1438fde28d6669e0f34cbd2ae90c5fbe903f8d1/README.md).

The state machine takes a session loader and effect handler, derives next work
from persisted conversation, and represents interrupted tool responses. Tool
approval is an explicit operation that yields while confirmation is unanswered.
These are relevant ideas for testing recovery as a transition over durable facts.
They do not prove exactly-once external effects or stronger real-world outcomes.
[State machine](https://github.com/aaif-goose/goose/blob/d1438fde28d6669e0f34cbd2ae90c5fbe903f8d1/crates/oi-kit-agent/src/machine.rs),
[approval operation](https://github.com/aaif-goose/goose/blob/d1438fde28d6669e0f34cbd2ae90c5fbe903f8d1/crates/oi-kit-agent/src/tool_approval.rs).

### Orca Harness

Orca separates core execution, extensions and an SDK. Its dispatcher explicitly
models parallel, serial and keyed calls, preserves completed sibling results
during cancellation and restores deterministic model-visible ordering. GEKTOR
already implements ordered concurrent/exclusive execution; keyed scheduling is
a possible extension only when a real operation needs it.
[Dispatcher](https://github.com/okikorg/orca-harness/blob/d89bf274c225075ae3f3b1f305a4717e05990ab7/crates/harness-core/src/dispatcher.rs).

The SDK exposes session shutdown, detached subagents, run handles and separate
execution/persistence results. Its sandbox/provider boundary also makes remote
execution an explicit substitution point. GEKTOR currently delegates sequentially
in a shared workspace and documents that its workspace is not a shell sandbox.
More parallel agents require isolation and ownership work, not just a goroutine.
[SDK](https://github.com/okikorg/orca-harness/blob/d89bf274c225075ae3f3b1f305a4717e05990ab7/crates/sdk/README.md),
[run outcome](https://github.com/okikorg/orca-harness/blob/d89bf274c225075ae3f3b1f305a4717e05990ab7/crates/sdk/src/run.rs),
[sandbox contract](https://github.com/okikorg/orca-harness/blob/d89bf274c225075ae3f3b1f305a4717e05990ab7/crates/harness-core/src/sandbox.rs).

Its deterministic compaction avoids a summary model call, but bounds retained
requests and recovery references. That is a tradeoff, not a universal improvement
over GEKTOR's checkpoint plus mechanical fallback and archive search.
[Compaction implementation](https://github.com/okikorg/orca-harness/blob/d89bf274c225075ae3f3b1f305a4717e05990ab7/crates/extensions/src/compact.rs).

### Position and next investment

| Area | GEKTOR assessment | Next useful investment |
|---|---|---|
| Run loop and measured evidence | Credible existing machinery; recovery holes remain | Fix F2/F3 and add accepted-state replay tests |
| Local-model operation | Provider inspection, explicit budgets and context introspection are useful | Measure task success/recovery and prompt cost; local API compatibility alone is not a differentiator |
| Integration | Public controls work; agent harness remains internal and RPC is custom | Consider ACP for a concrete editor consumer before a broad public Go harness SDK |
| Delegation | Bounded sequential child work limits shared-workspace contention | Add isolation before parallel mutating agents or persistent child sessions |
| Context | Verbatim instructions, archive search, checkpoint and mechanical fallback | Regression-test long-session retrieval and cancellation around compaction |
| Product identity | Agent execution plus reviewed durable workflows and inspectable results | Demonstrate one complete task-to-repeatable-workflow journey with independent acceptance |
| TUI | Reuse and visual hierarchy improved; live formatting can stall it | Fix latency and source fidelity; preserve packed tool calls |

The comparison supports improving contracts and integration, not rewriting in
Rust or copying every competitor subsystem. No evidence here ranks solve rate,
memory use, model latency or total cost. Published competitor microbenchmarks
were not reproduced. A future performance comparison needs identical model,
quantization, inference settings, tasks and independent artifact acceptance;
translate trace semantics instead of comparing tool names literally.

## Validation and limits

| Check | Result |
|---|---|
| `go test -p 1 ./...` | Pass, including public controls; cached results reused where valid |
| `go vet ./...` | Pass |
| `go test -race -p 1 ./internal/agent ./internal/harness ./internal/agentrpc ./pkg/tui/...` | Pass; not a full-repository race run |
| `go run ./cmd/eval -dry` | 45 probe definitions valid; no model inference |
| `npm run check` in website | Pass: 76 Markdown files, 37 published articles, 10 tests |
| `npm run build` in website | Pass: 38 HTML pages and 38 agent files at root hosting base |
| Gallery regeneration and selected TUI fixtures | Pass; generated HTML/CSS match tracked bytes |
| Public snapshot consumer checker | Fail, F4; complete-checkout external consumer passes |
| Three new resume assertions | Fail as expected, F2/F3 |
| Markdown probes | Executed; demonstrate latency and source repair, not correctness passes |

No production patch, FigCraft deletion, dependency upgrade, commit or push was
performed. No native terminal session, Linux execution, live model run, upstream
test suite, deployment or new vulnerability scan was performed in this audit.
CI configuration was inspected; its remote run status was not verified.

Recommended order: fix resume fidelity and streaming stalls, repair the public
consumer gate, make raw/formatted code explicit, retire FigCraft and update
current contracts, then choose an integration or measured harness experiment.
The evidence supports those bounded changes before another major feature layer.
