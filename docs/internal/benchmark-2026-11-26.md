# Local harness benchmark — 2026-11-26

This is a local experiment, not a competitor ranking. No OpenCode, Pi, Goose or
Orca task runs were performed. The model and machine are part of every result.

## Setup

- Windows amd64; evaluator build toolchain Go 1.26.11. The model's native
  `go version` call in the task workspace reported Go 1.26.4.
- llama.cpp `b12477-054a1babf`, one slot, `http://127.0.0.1:9931`.
- Surogate3.7-35B-A3B Q5_K_M, reported context window 131072 tokens.
- Request sampling: temperature 0.4, top_p 0.95, top_k 40, repeat penalty 1.05,
  repeat_last_n 1024; maximum response 8192 tokens; DRY disabled.
- Serial model runs. Offline Go checks also ran on this host during calibration;
  wall times are diagnostic observations, not controlled speed comparisons.
- Original source: `a39ce7f5f2f73a35bbf34a0e336a635fda0f9af7`.
  The frozen baseline executable is outside the checkout at
  `D:/Temp/gektor-benchmark-2026-11-26/eval-baseline.exe`.
- Original probes 05, 18, 23 and 25 were copied into
  `D:/Temp/gektor-benchmark-2026-11-26/baseline-probes` before edits. Each run's
  `meta.json` records source, binary, prompts and input hashes. Server properties
  are retained beside the frozen executable in `server-props.json`.

## Criteria

The calibration covers an exact edit, injected overflow recovery, unavailable
browser reporting, and a real Go ledger repair. Injected overflow exercises the
recovery path; it does not measure memory near a full context window. Browser
absence requires an honest structured answer and correct source analysis; it
does not exercise a browser or measure visual quality.

The ledger verifier is injected only after the model ends and removed afterward.
Its cases include independent decimal calculations and real CLI invocations.
Model-written tests and assertions of success do not determine acceptance.

Probe 05 previously checked substrings, allowing a comment decoy or damaged
surrounding code. Probe 18 had a weak answer/read check. Both now have stricter
criteria. Runs using those criteria have different input hashes and must not be
pooled with the frozen calibration.

## Observed failures and changes

Frozen calibration (`eval/results/benchmark-20261126-calibration/20261126-200130`):

| Scenario | Verdict | Model steps | Wall time | Finding |
|---|---|---:|---:|---|
| 05 exact patch | Pass | 4 | 19.3 s | Original, weaker criteria |
| 18 injected overflow | Pass | 3 | 5.5 s | Original, weaker criteria |
| 23 unavailable browser | Fail | 6 | 88.0 s | Final answer was not the required JSON; source reasoning also incorrect |
| 25 ledger repair | Fail | 17 | 1020.4 s | Infinite loop; 900 s task deadline plus 120 s independent acceptance deadline |

There was one attempt per scenario. These are calibration observations, not a
reliability estimate. The ledger also exceeded its 16-step efficiency target and
had no successful project-test invocation. Cancellation and three execution
errors were recorded separately from the model's final claims.

Candidate rerun (`eval/results/benchmark-20261126-candidate/20261126-202702`):

| Scenario | Verdict | Model steps | Wall time | Finding |
|---|---|---:|---:|---|
| 05 exact patch | Pass | 4 | 25.4 s | Same frozen criteria |
| 18 injected overflow | Pass | 3 | 6.2 s | Same frozen criteria |
| 23 unavailable browser | Fail | 4 | 180.1 s | Model call exhausted task deadline; no final JSON |
| 25 ledger repair | Fail on efficiency | 24 | 512.7 s | Completed; independent artifact and tool checks passed; target was 16 steps |

Both runs used input digest
`ffbf87698db971d47b5f15d9a3d96064a494ac74f20042db29f95d80a223c6de`.
Overall scored results stayed at two passes out of four. The second ledger run
generated 18100 completion tokens across 24 requests. Its two failed tools were
deliberate invalid-input checks, not broken commands. No causal improvement in
model correctness or speed can be attributed to the timeout change from these
single, stochastic attempts. Its concrete benefit is the smaller default bound
on a stuck foreground command, covered by command/deadline tests.

The later continuation repeated the archive launch error. A second product change
now inspects the attempted file's eight-byte header after startup fails. An ar
archive receives an explicit file-type diagnosis and a suggestion to check the
build target, with the original OS error still wrapped. Successful launches and
ordinary nonzero exits are unaffected. This change was added after the frozen
candidate executable was built; its benefit is covered by real failed-launch
tests, not credited to either live probe result.

The unavailable-browser run correctly acknowledged the missing capability, but
returned prose around the requested JSON and incorrectly treated an import-map
key without a trailing slash as a prefix mapping. The JSON verdict failed. The
factual mistake was also visible in the trace; no domain-specific prompt fix was
introduced to teach the benchmark answer.

During ledger repair the model wrote an infinite loop:

```go
for len(fracPart) < 2 {
    frac *= 10
}
```

It then invoked `go test ./...` without a timeout. The foreground tool's original
ten-minute default consumed most of the task's remaining budget. The candidate
uses a 120-second default for `run_program` and `run_shell`, retaining explicit
1–600 second timeouts. Managed background commands remain available for longer
work. Workflow commands have their separate timeout policy.

The evaluator now supports `-keep-workspaces`, retains a pre-acceptance snapshot,
and fails before generation if its trace cannot be created. These changes make
failed results inspectable; they do not improve model correctness by themselves.

## End-to-end journey

The first fresh journey failed after 24 repair requests (387.46 seconds), before
reaching the checkpoint/workflow stages. Its project tests passed on request 21.
On request 22, the model built the library root with `go build -o
cmd\ledger\ledger.exe .`, producing a Go archive rather than a CLI executable.
The final requests attempted to execute that archive and run the non-main root
package. The two-request closing warning was correctly placed; the model used
both remaining requests on tools and produced no final answer.

Independent ledger acceptance subsequently passed on a separate copy at
`D:/Temp/gektor-benchmark-2026-11-26/journey-1-acceptance`. This establishes that
the saved code satisfies the verifier, not that the original run completed.
The original archive and workspace remain unchanged. A diagnostic continuation
is recorded separately and does not turn that failure into a pass.

The first diagnostic continuation (`journey-continuation-1` under the same temp
benchmark directory) took 643.16 seconds. Repair completed in eight additional
requests and passed independent acceptance; the pending-question checkpoint and
fresh-session resume succeeded. Workflow authoring finished in 13 requests, but
the first independent execution failed before the CLI ran: its JavaScript received
the `amounts` array as `input` but spread `input.amounts` as though it were an
object. An earlier draft omitted the CLI command prefix; the final draft added
that prefix but retained this input-shape mismatch. It also bound the stdout
artifact metadata directly to report text. Review and changed-input repeat acceptance were
not reached. The source archive/workspace hashes remained unchanged.

The author spent several requests paging through the 79,171-byte operation
catalog. `workflow_schema` now accepts an optional `operations` list and returns
only the selected schemas and contracts, retaining shared recipe guidance. With
the actual application registry, selecting `run_program` returns 7,528 bytes
(90.5% smaller); selecting it and `artifact_read_text` returns 8,501 bytes.
Omitting the filter retains the full catalog. Unknown names and conflicting
record-schema queries are rejected. These sizes are tool-output measurements,
not a claim about model speed or accuracy.

In the improved live trial, the model selected the filter but guessed nonexistent
operation names and fell back to the full catalog. The final source now includes
sorted public names in unknown-operation errors (701 bytes for this registry),
so callers can recover without guessing. That follow-up was tested offline and
was not present in the improved trial's frozen executable.

The improved diagnostic continuation (`journey-improved-1`) also failed, after
1153.91 seconds. Repair completed in nine additional requests; checkpoint capture
and fresh resume again passed. Authoring completed in 18 requests. Its final
script again read `input.amounts` even though `input` was the array itself; array
concatenation produced a null argument. The process node rejected `/args/3` as
null instead of string before starting the CLI. The recipe also included an
extra `go` argument and bound the whole text-reader result rather than `/text`.
Neither review/publication nor changed-input repeat acceptance was reached.
The original evidence hashes remained unchanged.

These two continuation attempts used the same original saved archive/workspace,
each copied into a new directory with an explicit relocation note and 24 extra
repair requests. They are not independent fresh repair successes, and their
different binaries/paths and stochastic generation prevent a causal speed claim.
The completed repair request totals were 32 and 33 including the original 24.
No further full repair continuations were made in that phase. The focused
authoring follow-up below reuses its saved checkpoint.

The opt-in `TestLiveJourney` uses the shipping harness to repair and verify the
ledger, capture an actual saved pending question, cancel, and resume with an
operator answer plus a new workflow instruction. It preserves the original repair
archive, cancelled archive, immutable checkpoint and resumed archive separately.
This is graceful cancellation/checkpoint recovery, not an operating-system crash.

After authoring, hidden repair acceptance runs again. An independent runner then
executes the model's saved workflow with fresh inputs. It requires real project
checks, literal CLI arguments, a review gate before report publication, correct
artifact contents, and a second successful execution with changed inputs. Invalid
input must fail before publication. Offline negative controls reject skipped
checks, hardcoded inputs, incorrect reports and absent review gates.

Run instructions are maintained in [the evaluation guide](../../eval/README.md#repair-to-workflow-journey).

## Focused authoring follow-up

After committing the initial benchmark as `ece5f15`, authoring trials resume
`journey-continuation-1/pending.state.json` against a fresh copy of its
`repaired-workspace`. They preserve the original prompt and answer, add an explicit
workspace relocation note, and retain the original acceptance criteria. Each
trial allows 24 model requests, 8192 response tokens and 15 minutes. These are
authoring-only trials, not fresh repair-to-workflow successes.

The first candidate makes default agent schema discovery a complete names-only
catalog (42 names, 856 bytes). Named selections include contracts and executable
scalar, array and artifact-reference examples. `all_operations: true` and the
CLI retain full-schema access. Command contracts explain executable/argument
separation. The draft prompt now requests version 4. The separate TUI edit prompt
was also corrected, after this trial's binary was built; that path was not tested
by the trial.

Trial 1 (`D:/Temp/gektor-workflow-author-2026-11-26/run1`) completed authoring in
6 requests and 316.06 seconds overall: 196642 prompt tokens, 133296 cached tokens
and 5344 generated tokens. All eight tool calls succeeded. It selected three
operation contracts directly, without full-catalog paging. The recipe used the
correct array concatenation, literal CLI arguments and artifact `/id` and `/text`
references. However, it omitted `required: true` on both declared inputs while
claiming they were required. The independent oracle rejected the recipe before
execution. Review, repeated-input execution and invalid-input behavior therefore
remain unverified for this attempt. Hidden ledger acceptance passed before and
after authoring, and the source checkpoint/workspace remained unchanged.

The trace also contains the same approximately 14 KB built-in guide twice:
`load_skill workflows` and `enable_capability native_workflows` were issued in one
batch. This is duplicated context, although it does not add a model round trip.

The frozen first binary is `D:/Temp/gektor-workflow-author-2026-11-26/author.test.exe`,
SHA-256 `65f1d092bb837ffd842b9c47ee832af50892660617166f8ed6c0dd9d4a7267cc`.
`author.json` retains the report, timings, checks and provenance; `requests.log`
retains the actual tool exchange. Invocation is documented in the evaluation
guide. A shorter single attempt does not establish an efficiency or reliability
improvement over the earlier continuations, which had different histories.

The second candidate clarifies that `required` defaults to false and that a
declared default supplies a missing value even when `required` is true. A fourth
executable example accepts a supplied string and rejects a missing value before
execution. Capability and skill metadata now explain that activating
`native_workflows` includes the built-in guide. The second live trace still
requested both copies: this hint alone did not remove the duplicated context.

Trial 2 (`D:/Temp/gektor-workflow-author-2026-11-26/run2`) **passed** in 8 model
requests and 353.36 seconds overall: 271474 prompt tokens, 207672 cached tokens
and 7078 generated tokens. All nine tool calls succeeded. Both inputs explicitly
declared `required: true`. The independent runner verified:

- Two executions with different fresh inputs, producing exactly `1446\n` and
  `-630\n` in their report artifacts.
- A real publication review gate, approved by the test operator after authoring.
- Rejection of an invalid amount before report publication.
- Passing hidden ledger acceptance before and after authoring, unchanged repaired
  code and unchanged source checkpoint/workspace.

The model still spent calls on PATH-tool discovery and a second recipe write.
Its actual workflow executions were performed by the independent runner, after
the model finished. The frozen second binary is
`D:/Temp/gektor-workflow-author-2026-11-26/author-v2.test.exe`, SHA-256
`46c0cf35a81e22d8f1bb82ff62aec9c0ed8a36a00b2830f5f4988733b528afb5`.

These are two sequential development trials with a candidate change between
them: one failed and one passed. They are not repeated measurements of a fixed
candidate, a reliability estimate, or proof of a causal speed improvement.

After these frozen trials, context reuse was extended to recognize the identical
built-in guide delivered through either skill loading or capability activation.
This changes only the per-request projection; archives retain both full results.
Real-agent offline tests cover both call orders, resumed requests, compaction,
custom skill overrides, changed guides and incomplete or failed results. In the
test fixture, projected tool content falls from 29484 bytes to 15002 bytes when
the skill arrives first, or 14888 bytes when activation arrives first. Compaction
restores full guidance when its earlier copy is absent. This deduplication was
not included in either live trial; its token and latency impact is unmeasured.
The final follow-up passes `go test ./...`, the executable build,
affected-package vet and focused race checks for examples and context reuse.

## Validation and remaining work

Full `go test ./...` and the executable build passed. Affected-package vet,
focused race tests, deadline/failed-launch regressions, operation-filter tests,
and the offline journey's negative controls passed. The executable was rebuilt
at `bin/gektor.exe`. Production TUI code was not changed.

Retained workspaces now live under `_workspaces`, which recursive Go commands
exclude. Trace/snapshot failures are explicit; usage and solver errors survive
snapshot failure. Exact-edit and overflow criteria reject the earlier false-pass
cases. The initial journey metadata had the standalone evaluator's retention
flag set to false despite retaining its workspace; later journey metadata records
retention correctly and separately hashes diagnostic-continuation inputs.

The measured priorities are:

1. Workflow authoring needs dependable typed value/array/artifact composition.
   Static validation did not establish runtime correctness. Keep independent
   execution checks before presenting an authored recipe as reusable.
2. Reduce wasted requests and unnecessary exploration. A correct small repair
   still missed the declared efficiency target; the fresh journey failed to finish.
3. Structured final answers and factual source analysis remain weak on this model.
4. Repeat fixed cases before estimating reliability. Then vary only the model,
   and compare harnesses with the same model/tasks and independent artifact checks.

This experiment supports the specific tooling fixes above. It does not establish
competitor parity or a general pass-rate improvement.
