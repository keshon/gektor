# Workflow capability discovery — 2026-11-27

This follow-up targets the ledger discovery overhead recorded in the
[workflow comparison](workflow-matrix-2026-11-26.md). The two new trials both
passed independent workflow acceptance. Their combined request count fell from
27 to 16; neither requested or paged the full operation catalog.

## Change and regression checks

Production changes are limited to the `native_workflows` capability description.
It now describes workflow node operation contracts, text artifact read/write,
review gates, records and scheduling. Its opening route—enable, then
`workflow_schema`—and guide-included hint survive the 48-rune startup summary.
The full description directs authors to `operations:[names]` for needed contracts.
The bundled guide, schemas, complete-catalog option and runtime behavior are
unchanged from `df87b6a`.

Discovery scores each matching distinct query term across capability ID,
description and known Agent tool names, then ranks results. It does not require
every query term to match. Missing vocabulary therefore often weakens ranking
rather than producing no results. In the previous trace, `artifact write read
review` ranked the image capability first and workflows second; `recipe node
operation schema` already ranked workflows first. These distinctions matter when
interpreting repeated searches.

The regression test now uses the real composed catalog. Ten queries cover old
node/cron/schedule vocabulary and the observed artifact/schema/contract searches.
It checks returned capability IDs, first-place workflow ranking for `artifact
write read review` and `operation contract`, passive discovery without activation,
and the startup summary's guide and schema route. No query-matching algorithm or
permission rule changed.

## Controlled inputs and scope

Each trial uses `TestLiveWorkflowAuthor` with the same genuine pending-question
checkpoint and repaired workspace from
`D:/Temp/gektor-benchmark-2026-11-26/journey-continuation-1`. The model receives the
same original task and operator answer, plus the required workspace relocation
note. The original task, checkpoint and repaired-workspace hashes match all four
compared trials. Source evidence remained unchanged after both new attempts.

The reported model remains Surogate3.7-35B-A3B Q5_K_M on the local llama server at
`http://127.0.0.1:9931`, with a 131072-token context window. Sampling is unchanged:
temperature 0.4, top_p 0.95, top_k 40, repeat penalty 1.05 and repeat_last_n 1024.
Budgets remain 24 requests, 8192 response tokens and 15 minutes per attempt.
The two attempts ran serially against one frozen binary, without retrying to obtain
a pass. No model calls were made by the reviewing agent.

The author must save and validate without executing or approving the workflow.
The independent runner subsequently requires real project tests and CLI calls,
two changed input sets, exact report contents, review before publication, and
rejection of an invalid amount before publication. Both new attempts produced
`1446\n` and `-630\n` in their approved report artifacts. Hidden project acceptance
passed before and after authoring; repaired code was preserved.

## Results

The previous pair is the guidance candidate from the earlier comparison, not the
older initial baseline. Seconds cover each recorded attempt including its oracle.

| Attempt | Passed | Requests | Seconds | Prompt tokens | Cached tokens | Generated tokens |
|---|---|---:|---:|---:|---:|---:|
| Previous ledger 1 | Yes | 14 | 533.40 | 624715 | 540383 | 11861 |
| Previous ledger 2 | Yes | 13 | 444.70 | 450315 | 372360 | 9141 |
| Discovery ledger 1 | Yes | 9 | 409.00 | 291344 | 226964 | 9024 |
| Discovery ledger 2 | Yes | 7 | 307.33 | 218644 | 158221 | 6173 |

| Combined measure | Previous pair | Discovery pair |
|---|---:|---:|
| Passed attempts | 2/2 | 2/2 |
| Model requests | 27 | 16 |
| Full-catalog queries | 2 | 0 |
| Output-page reads | 4 | 0 |
| Capability searches | 4 | 3 |
| System-info calls | 6 | 2 |
| Context-inspection calls | 3 | 0 |
| Prompt tokens | 1075030 | 509988 |
| Generated tokens | 21002 | 15197 |
| Elapsed seconds | 978.10 | 716.33 |

Both new attempts selected the three needed command/artifact contracts directly.
All three capability searches returned workflows first. In the first attempt,
the composite query `workflow schema operations artifact` returned workflows
ahead of files. The model still refined the recipe with a second write and made
post-validation read/list calls; the change does not eliminate all overhead.

The first attempt loaded the guide only through activation, so no duplicate
delivery needed shortening. The second used activation and `load_skill`; existing
deduplication shortened five later projected tool results, avoiding 77275 UTF-8
bytes of repeated transmitted content. Outbound requests retained at most one
complete guide, while archives retained full results. This is byte accounting
against the archived representation, not a token or latency estimate.

These are two development trials per candidate, on different days with evolving
cache state and stochastic sampling. The request and catalog reductions are
observations, not a general reliability rate or a causal speed measurement. The
regression tests establish the discovery behavior directly; longer matched trials
would be needed to estimate consistency. Full-catalog export remains available.

## Evidence and validation

Local evidence is under `D:/Temp/gektor-discovery-2026-11-27`:

- `ledger-1/` and `ledger-2/` retain `author.json`, `meta.json`, requests, archives,
  copied workspaces and workflow runs.
- `ledger-discovery.test.exe` SHA-256:
  `29ee4889acc502ffce7d715e10aa2aa4562594a554aa001695fbcee68ec16f3a`.
- `frozen-source.patch` records the source change used for the build;
  `frozen-workflow-guide.md` is unchanged from the previous guidance candidate,
  SHA-256 `2b35ce4e809473d0a4a5b6a870166827b5eb220acb966be5952a8683975ccd5b`.
- Each trial's `independent-dedup.json`, plus `independent-comparison.json` and
  `independent-discovery-results.json`, records the separate trace audit.
- Previous evidence remains under
  `D:/Temp/gektor-matrix-2026-11-26/candidate2-ledger-1` and
  `D:/Temp/gektor-matrix-2026-11-26/candidate2-ledger-2`.

Full `go test ./...`, affected-package vet and the executable build passed before
the frozen trials. Afterward, the ranking assertions were strengthened and the
focused discovery/budget tests, vet and diff checks passed. No production code
changed during or after the trials. Invocation is documented in the
[evaluation guide](../../eval/README.md#focused-workflow-authoring).
