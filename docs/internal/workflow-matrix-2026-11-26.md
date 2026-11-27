# Repeated local workflow authoring — 2026-11-26

This investigation follows the [initial harness benchmark](benchmark-2026-11-26.md).
It measures normal Agent workflow authoring with a local model and independent
execution checks. It does not compare competitors or measure TUI interactions.

## Method

Three tasks are attempted twice per candidate, serially against the same local
llama server at `http://127.0.0.1:9931`, using Surogate3.7-35B-A3B Q5_K_M and a
131072-token context window. Each attempt allows 24 requests, 8192 response tokens
and 15 minutes. Sampling remains temperature 0.4, top_p 0.95, top_k 40, repeat
penalty 1.05 and repeat_last_n 1024. Actual request logs and model metadata are
retained. The model must save and validate a recipe without executing it.

| Task | Independent acceptance |
|---|---|
| Labels | Required inputs without defaults; actual `script_transform` execution; trimming, uppercase, supplied prefix, order and duplicates; changed inputs and empty array. |
| Text report | Required inputs without defaults; artifact write/read followed by reviewed publication; exact Unicode/newline text and empty text; current-run artifact metadata. |
| Ledger report | Existing repaired project and genuine pending-question checkpoint; hidden ledger acceptance; actual tests and CLI; fresh positive/refund inputs; report approval; invalid amount fails before publication. |

Labels and text report start fresh sessions in empty workspaces. Ledger resumes
the same saved checkpoint and repaired project used by the earlier investigation;
the repair itself is excluded. Standalone cases reject missing required inputs
before execution. Positive recipes and incorrect-output negative controls test
their oracle offline. The ledger oracle retains its earlier negative controls.

Standalone oracle checks were tightened while the first frozen binary ran: the
final checks require successful author validation, actual script execution,
artifact report steps (allowing pure script intermediates) and current-run metadata. Original artifacts
were retained, and all four baseline workspaces passed a copy-only regrade.
Neither task prompts nor model inputs changed during that baseline. The frozen
binary hash identifies executed code; source hashes in metadata describe the
checkout at measurement time and therefore include later test-only edits.

An initial setup-only attempt under `baseline/` failed because the runner had not
created the empty workspace directory. It made no model requests. That evidence
was retained, the runner was corrected, and model trials started under `candidate1/`.

## Baseline

Production source was commit `3bd3948`; the frozen test binary adds the matrix
runner. All six authoring attempts passed independent acceptance.

| Task / attempt | Requests | Seconds | Prompt tokens | Cached tokens | Generated tokens |
|---|---:|---:|---:|---:|---:|
| Labels 1 | 7 | 115.60 | 85055 | 59627 | 2266 |
| Labels 2 | 12 | 208.54 | 219626 | 188128 | 5076 |
| Text report 1 | 9 | 206.66 | 139768 | 110121 | 5222 |
| Text report 2 | 9 | 168.56 | 148914 | 115850 | 3616 |
| Ledger 1 | 14 | 579.14 | 680754 | 600408 | 12938 |
| Ledger 2 | 13 | 589.70 | 528352 | 447338 | 14029 |

Correctness was consistent on this small fixed set; request cost was not. The
same labels task used 7 and 12 requests. Traces contain repeated capability
searches, PATH-program searches for workflow tools, context inspection after
activation, and full-catalog queries for recipes needing only a few operations.
Ledger 1 spent several requests paging the full schema. Some extra checks were
useful: Labels 2 deliberately checked rejection of missing required inputs.
These are distinguished from unsuccessful Git status in an empty non-Git workspace.

## Deduplication in real requests

All six baseline trials loaded the built-in guide through both skill loading and
capability activation. Outbound model requests retained at most one complete copy
of the guide across all message roles. Later duplicate tool results became short
reminders, while archived results retained their full bodies.

Matching each projected result to its archived tool-call ID measured **623182
UTF-8 bytes avoided across 43 shortened message instances in 64 requests**. This
is repeated transmitted content, not distinct source bytes or a measured token
reduction. Both loading orders occurred in actual runs. No paired experiment
disabled deduplication, so its causal latency effect is unmeasured.

## Guidance candidate

The second candidate's production changes are limited to workflow schema
descriptions and the bundled guide. `operations` now explains that selected contracts include recipe format
and examples; `all_operations` explicitly identifies the complete catalog and
its possible paging cost. Early guidance distinguishes capability activation
from node-contract lookup. It also ties extra reads and status checks to an
unresolved question, a failed result or a user-required check.

Selected and complete schema queries remain supported. Runtime validation,
execution, review gates and model settings are unchanged. The guide explicitly
limits successful save/validation to a structural authoring handoff and retains
the requirement to report runtime correctness as unverified when execution is
forbidden. The same six tasks are repeated against a separate frozen binary.

All six guidance-candidate trials passed the same independent acceptance checks.
Both candidates' eight standalone workspaces also passed the final copy-only
regrade. All four ledger runs preserved their original source evidence and
passed both fresh report cases, publication review and invalid-input rejection.

| Task / attempt | Requests | Seconds | Prompt tokens | Cached tokens | Generated tokens |
|---|---:|---:|---:|---:|---:|
| Labels 1 | 9 | 126.65 | 102279 | 76904 | 2775 |
| Labels 2 | 8 | 117.72 | 86243 | 67859 | 2992 |
| Text report 1 | 8 | 140.78 | 90152 | 70611 | 3747 |
| Text report 2 | 8 | 138.11 | 118046 | 87083 | 2834 |
| Ledger 1 | 14 | 533.40 | 624715 | 540383 | 11861 |
| Ledger 2 | 13 | 444.70 | 450315 | 372360 | 9141 |

| Measure | Baseline | Guidance candidate |
|---|---:|---:|
| Passed authoring trials | 6/6 | 6/6 |
| Model requests | 64 | 60 |
| Cumulative prompt tokens | 1802469 | 1471750 |
| Cached prompt tokens | 1521472 | 1215200 |
| Generated tokens | 43147 | 33350 |
| Total elapsed seconds, rounded | 1868 | 1501 |
| Full-catalog queries | 5 | 3 |
| Capability searches | 11 | 11 |
| System-info calls | 10 | 11 |
| Context-inspection calls | 3 | 3 |
| Web searches | 0 | 1 |
| Output-page reads | 5 | 4 |

The improvement is modest: four fewer requests, all from the standalone tasks.
Both ledger attempts retained their original request counts and still requested
the full catalog. One labels attempt became slower and searched the web for local
workflow schemas, receiving unrelated documentation before using local contracts.
The observed elapsed-time reduction is not a controlled speed measurement.

Real-request deduplication also held in the guidance candidate: at most one full
guide, with **552276 UTF-8 bytes avoided across 36 shortened message instances**.
Across both candidates this is 1175458 bytes across 79 instances. No original
archive was shortened. A different guide length and fewer requests affect these
totals; larger byte savings are not themselves a better performance score.

The same model and context size were recorded throughout. Standalone task prompts
were unchanged; all ledger attempts shared the original prompt, pending archive
and repaired-workspace hashes. The order was fixed rather than randomized, and
model sampling/cache state varied. These twelve passes establish correctness on
this set, not a general reliability rate or proof that guidance caused the gains.

Discovery gaps observed in this experiment included queries such as `artifacts workflow`,
`artifact write read text` and `operation contract`: the owning package summary
does not advertise that vocabulary clearly. The current change does not eliminate
repeated discovery or full-catalog paging. Improving that metadata and separating
bulk catalog export from ordinary authoring lookup are follow-up experiments,
not changes measured here. The [2026-11-27 follow-up](workflow-discovery-2026-11-27.md)
subsequently tested improved capability metadata: both ledger reruns passed,
using 16 requests combined without full-catalog queries. Separating bulk export
remains untested. Dedicated draft/edit roles were not tested.

## Artifacts and reproduction

Local evidence is under `D:/Temp/gektor-matrix-2026-11-26`:

- Baseline standalone trials: `candidate1/`; ledger trials: `ledger-1/`, `ledger-2/`.
- Guidance standalone trials: `candidate2/`; ledger trials:
  `candidate2-ledger-1/`, `candidate2-ledger-2/`.
- Baseline binary: `matrix-v1.test.exe`, SHA-256
  `28e9bba5526006898e65b9a014641a7d91ab4c22f09e582be7900f8d71e7aef6`.
- Guidance binary: `matrix-v2.test.exe`, SHA-256
  `7d52e2de1233b4f4fb315d4c6d4ccd540a6bf8b6049c0dfaeae273445ba8c1d0`.
- Each trial retains requests, archives, metadata, reports and its workspace.
  Ledger reports also record source-preservation hashes and workflow run evidence.
- `baseline-dedup-final6.json` records per-request guide counts and byte savings.
  `candidate2-dedup-final6.json` records the second candidate;
  `actual-response-tool-counts.json` excludes historical repair calls, and
  `comparison.json` retains the trial metrics and input identities.
  Both guide versions are preserved separately, alongside the production patch
  and the final matrix-runner source used for the second build.

The [evaluation guide](../../eval/README.md#repeated-workflow-tasks) documents the
opt-in runners. Compare requests and correctness first. Wall time is affected by
cache state, model randomness and other work on this host. Two attempts per task
are insufficient for a reliability estimate or a causal performance claim.

Final `go test ./...`, affected-package `go vet`, executable build and diff checks
passed. Positive/negative oracle tests and both copy-only regrades passed.
Production TUI code and packed single-row tool calls were unchanged.
