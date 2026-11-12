# Resumable document reads

Status: implemented and validated on 2026-11-09.
Investigated on 2026-11-09 against commit `65e0ed9`.

## Decision

Extend the three existing readers with the same second paging coordinate:
`offset` selects an item; `text_offset` selects a position inside that item's
extracted text. Keep the existing structured results and item pagination.
Do not introduce another tool, a fragment response mode, or a cursor service.

The change is local to the document integration, its shared contracts, tests,
and instructions. Agent and workflow execution already use the same operation.

## Current behavior and cause

`documents.Request` carries `path`, `sheet`, `offset`, and `limit` through
`Service.Read` to an isolated parser worker. The parsers return `Block`, `Cell`,
or `PDFPage` items. `adapters.Operation` supplies both agent and workflow schemas.

All readers have a 32 KiB content-text budget per response. DOCX text/cell
values and XLSX values/formulas also have independent 4 KiB caps. The parsers
clip text, mark the item `truncated`, then advance `next_offset` past it.
The discarded suffix cannot be requested through these readers.

Two details matter for a complete fix:

- A DOCX item can be an entire table row containing many cell strings.
- An XLSX item can contain both a stored value and formula text.

Adding an offset to just paragraph/page text would leave those cases broken.

## Proposed public contract

### Request

Add optional `text_offset` to the existing request. Its unit is Unicode code
points, consistent with history's character offsets; the content budget stays
in UTF-8 bytes. Callers copy returned offsets rather than computing them.

- Absent: existing item-page request, with the same default item limit.
- Present, including zero: read exactly one item at `offset`.
  An omitted `limit` defaults to 1; an explicit limit other than 1 is rejected.
- Negative offsets are rejected. An item index outside the document or a text
  offset beyond that item's extracted text is rejected in text-continuation mode.
- Offset equal to text length is valid and returns an empty completed slice.
- XLSX text continuation requires an exact `sheet`; sheet listing stays unchanged.

Example, after block 7 reports `next_text_offset: 32768`:

```json
{"path":"report.docx","offset":7,"text_offset":32768}
```

The number is illustrative. Copy the actual returned value, including for
non-ASCII text. Repeat with the next returned text position until complete,
then continue the original item page using its saved `next_offset`.

### Response

Keep `blocks`, `cells`, and `pages` and their existing identity/metadata fields.
Add optional integer fields to each item, not the result envelope:

- `text_offset`: position at which this item's returned slice starts; omit at zero.
- `next_text_offset`: present only if this item has unread extracted text.

Keep `truncated`; it is true exactly when `next_text_offset` is present.
Top-level `total`, `offset`, `next_offset`, and `more` keep their item-level
meaning. In particular, `more: false` can coexist with an incomplete last item.
Instructions must say to finish item continuations as well as item pages.

There is no second response envelope or per-format continuation syntax.

### Structured items

An item's textual fields have a fixed traversal order:

1. DOCX paragraph: `text`.
2. DOCX table row: `cells` in their original order.
3. XLSX cell: `value`, then `formula`.
4. PDF page: `text`.

`text_offset` counts across these fields without inserting separators or
flattening them in the response. Each returned slice stays in its original
field/cell. Consumed fields and fields not reached yet contain empty strings;
DOCX row cell positions are retained. Existing optional empty-field omission
rules can stay. XLSX address, type and formula metadata stay attached.

To reconstruct content, append each chunk to the same named field/cell, never
concatenate a whole table row and try to split it again. This also covers a
32 KiB boundary crossing from one table cell to another or from value to formula.
PDF `no_text` continues to describe the full extracted page, not the chunk.

### One content budget

Replace independent 4 KiB body-field clipping with an ordered slice of the item
under the remaining 32 KiB response budget. Stop at the first incomplete field;
do not skip its suffix to include later fields. This produces a contiguous
position that one `next_text_offset` can describe.

This is an intentional small behavior change: a long field can occupy more of
the response, and later row cells/formulas may arrive on continuation calls.
Small documents keep their existing output; the total text budget does not grow.
The separate PDF metadata limit remains 4 KiB and still fails on oversized metadata.

Every incomplete nonempty chunk must advance. If the remaining response budget
cannot fit the next complete Unicode character, finish the page before that item
rather than emit an empty, non-advancing continuation. Empty items still advance
item pagination normally.

## Affected files and entities

### Reader implementation: four existing files, one small helper

- `internal/capabilities/documents/documents.go`
  - `Request`: add `TextOffset *int` to distinguish absence from explicit zero.
  - `validate`: apply continuation defaults and validate single-item requests.
  - `Service.Read`, `workerRequest`, `Serve`: existing JSON transport carries the
    new request automatically; keep their worker and file-access boundaries.
- `internal/capabilities/documents/text.go` (new)
  - One unexported helper for slicing ordered string parts by character position
    under a byte budget; return structured parts and the next text position.
  - No interfaces, registries, configurable chunk sizes, or cross-package API.
- `internal/capabilities/documents/docx.go`
  - `Block`: add the two optional text-position fields.
  - `office.docx`: use the helper before appending an item; retain row cell slots.
- `internal/capabilities/documents/xlsx.go`
  - `Cell`: add the same fields.
  - `office.xlsx`: slice value/formula in order and preserve existing cell metadata.
- `internal/capabilities/documents/pdf.go`
  - `PDFPage`: add the same fields.
  - `readPDF`: slice the extracted text; preserve full-page `no_text` semantics.
- `internal/capabilities/documents/office.go`
  - No expected edit. Keep `clip` for bounded PDF metadata; new body pagination
    belongs in `text.go`, since PDF and Office readers both use it.

### Public contract and instructions: four existing files

- `internal/capabilities/documents/contract/contract.go`
  - `Operation.Schema`, `OutputSchema`, `Description`: describe the new optional
    input and item metadata, consistent across all three operations.
  - Keep the same operation names and permission resources.
- `internal/tools/packages_builtin.go`
  - Update only native-document instructions to explain both paging levels.
- `internal/workspace/builtin/workflows/SKILL.md`
  - Explain how to finish an incomplete item before advancing item pages.
- `docs/workflows.md`
  - Replace the documented unrecoverable-truncation limitation with the contract,
    traversal order, continuation example, and unchanged-file requirement.

### Verification: two existing test files, one helper test file

- `internal/capabilities/documents/text_test.go` (new)
  - Exercise the shared slicing algorithm with Unicode and field boundaries.
- `internal/capabilities/documents/documents_test.go`
  - Add reconstruction regressions for all three formats and revise the old
    test that specifically assumes 4 KiB body clipping.
- `internal/tools/documents_integration_test.go`
  - Extend the existing agent/workflow parity test with a long-item continuation,
    schema validation, and permission-denial coverage for a continuation call.

Expected scope: 10 existing-file edits and 2 new code/test files. This planning
document is additional. Five existing-file edits are implementation/contracts;
the remaining edits are instructions and tests.

### Reused without redesign

- `agenttools.Tool` and `nodes.Definitions` already wrap `adapters.Operation`.
  No new wrappers or operation registrations are needed.
- `Block`, `Cell`, and `PDFPage` remain the returned entities; no new result type.
- `DOCXResult`, `XLSXResult`, and `PDFResult` retain their envelope fields.
- Keep workflow recipe version 3 and document node version 1: new input/output
  fields are optional, old calls remain accepted, and existing field meanings stay.
- No expected edits to agent loops, roles, TUI, session history, permission policy,
  workspace access, worker lifecycle, automation engine, or application composition.
- Existing short-document eval fixtures and `cmd/eval/documents_test.go` are
  regression checks to run, not files requiring new fixture infrastructure.

## Compatibility and deliberate limits

- Existing item pagination advances as before. Consumers wanting complete text
  must follow each item's `next_text_offset` as well as top-level item pagination.
- No state migration, persisted cursor, content cache, file snapshot or new dependency.
- Every request reopens and reparses the source as today. Continuation is reliable
  for an unchanged file. It is not a transactional read across file edits; users
  must restart reading after edits. This limitation is documented, not hidden.
- Completion means all text the existing parsers extract. It does not add DOCX
  headers/footnotes, OCR, PDF rendering, nested-table support, formula evaluation,
  or support for previously unsupported files.
- Keep the 16 MiB file/Office expansion limits, 32 KiB content budget, worker
  timeout, parser containment, permissions and read-only execution behavior.
- Repeated parsing costs time for very large items. Accept that existing tradeoff
  for this bounded fix; do not add a cache until measured workloads justify it.

## Implementation sequence

1. Add the shared slicing helper, request validation, and reconstruction-focused
   unit tests. Use ordinary functions and existing structs.
2. Wire the three parsers into that helper and add item position metadata. Cover
   multi-field items and preserve current short-document behavior.
3. Update shared schemas and instructions together. Extend integration tests so
   agent calls and workflow nodes accept and return the identical contract.
4. Run the acceptance checks below and inspect the diff for changes outside the
   listed scope. Stop after delivering the implementation for review.

## Context-window guard

Continuation is pull-based: one tool call returns one bounded slice. Neither
the reader nor the workflow node automatically follows continuations, prefetches
the document, or concatenates all chunks into a model-facing response.
The 32 KiB limit is a content-byte bound, not a token count; JSON and metadata
add overhead. It applies independently of whether the model has a 65k, 128k,
or 256k context window.

Reuse the existing model-independent agent guards rather than adding a document
token-budget subsystem:

- `internal/agent/tool_results.go` bounds model-facing tool results to a shared
  48 KiB allowance per call batch. `tools.LimitOutput` retains larger output in
  the existing output store and returns a `read_output` recovery reference.
  A document continuation does not bypass this path.
- `internal/agent/run.go` projects the next prompt before each model request.
  `internal/agent/compact.go` triggers compaction at the configured threshold
  (85% by default) and caps generation against estimated remaining context.
  These protections depend on a correctly configured/discovered context limit
  and enabled compaction; token estimates are not exact for every local tokenizer.
- Existing overflow recovery can compact and retry; persistent overflow stops
  the run. Continuation does not promise that an entire document stays in active
  context, or that compaction retains every extracted fact.

The document instructions should encourage reading only the needed items and
recording useful findings before continuing a long scan. This is task guidance,
not a new automatic summarization mechanism or correctness guarantee.

Include an integration check where batched document results hit the existing
output limiter: the model must be able to recover the saved JSON, including its
continuation fields, through `read_output`. Keep the context-budget regression
tests in the validation run. Do not change agent compaction or introduce adaptive
document chunk sizes as part of this feature. If those checks expose a harness
defect, record it separately before expanding this scope.

## Acceptance checks

- Reconstruct a long DOCX paragraph, a row with multiple long cells, an XLSX
  cell with long value and formula, and a long PDF page exactly, field by field.
- Exercise mixed ASCII, Cyrillic, emoji and combining characters at boundaries;
  all chunks are valid UTF-8, with no dropped or duplicated code points.
- Cover an item truncated by the remaining response budget, not just a large
  individual item; prove that every continuation advances or completes.
- Cover empty items, empty table cells, exact-boundary completion, final-item
  truncation with top-level `more: false`, invalid text offsets, and XLSX listing.
- Keep old item offsets, identities, formulas/metadata, and short-file results.
- Validate actual continuation JSON against both input and output schemas.
- Verify the same continuation via agent and workflow execution, with an unchanged
  source file, and confirm that source permissions cannot be bypassed.

Suggested implementation validation commands:

```powershell
go test ./internal/capabilities/documents/...
go test ./internal/tools -run '^TestDocumentsLazyDiscoveryAgentWorkflowAndResources$'
go test ./cmd/eval -run '^TestDocumentEvalFixturesThroughAgentAndWorkflow$'
go test ./internal/conventions
go test ./...
go vet ./...
git diff --check
```

The plan itself changes no runtime behavior. Model-driven evals and live model
reliability are separate from proving the reader contract and are not claimed here.

## Approved evaluation addition

Add probe `39-documents-continuation` to the document suite. Generate deterministic
DOCX, XLSX and PDF fixtures with required evidence beyond the first item slice;
require successful nonzero text continuation calls and evidence-backed answers.
Reuse the existing probe runner, trace assertions and generated-fixture mechanism.
This adds `cmd/eval/documents.go` and the probe JSON, and updates the suite list,
its count test, document fixture tests, and eval documentation. No new eval engine.

## Validation record (2026-11-09)

- Focused reader, Unicode reconstruction, tool/workflow parity, output recovery,
  permissions and generated-eval-fixture tests passed.
- `go vet ./...` passed. `go test -p 1 ./...` passed across the full repository.
  The initial parallel run had unrelated workspace-observation timeouts and a
  Git access-denied failure; serial rerun required no source changes to those areas.
- Eval dry run validated all 35 probes, including the three-document suite.
- Local backend: Cyber-Tiel-Coder-35B-A3B UD-Q4_K_M, context 98,304 tokens.
- Initial probe budget: 360 seconds. Run 1 timed out after 8 completed steps,
  with prompt usage at 19%; it remains a failed run. Remaining short-budget
  repetitions were stopped. Results: `eval/results/20261109-113357/`.
- Probe time allowance increased to 900 seconds; evidence/trace assertions,
  task, fixtures and step allowance stayed unchanged. The calibration rerun passed: 153.5 seconds, 6 model steps, zero tool failures,
  all three required continuation traces and final values present. Peak prompt
  usage was approximately 20% (20,169 tokens). Results:
  `eval/results/20261109-114056/`. One passing run does not measure model
  reliability or force compaction.

### Additional repetitions and instruction correction

Two requested repetitions against the same 98,304-token backend are saved under
`eval/results/20261109-120756/`: run 1 passed in 185.3 seconds (6 steps); run 2
failed in 175.9 seconds (7 steps). The second run incorrectly claimed that an
XLSX string cell had no formula while the cell still reported `truncated: true`
and `next_text_offset`. It omitted the required XLSX continuation and marker.

Clarified the existing shared operation descriptions and document instructions:
empty/omitted fields on truncated items can be unread, and the XLSX stored-value
type does not establish formula absence. Parser behavior, schemas, fixtures,
assertions and time/step budgets remain unchanged. Two further repetitions are
required before commit/push; failures are preserved rather than replaced.

### Final validation after clarification

Both fresh repetitions passed under unchanged probe criteria and the 900-second
allowance: 190.8 seconds / 7 steps and 193.8 seconds / 7 steps. Both completed
all three continuation checks and returned all requested values, with zero tool
failures. Peak context usage was approximately 23% of 98,304 tokens. Results:
`eval/results/20261109-121602/` (including `summary.json` and request traces).

`go test -p 1 ./...`, `go vet ./...`, and whitespace checks passed again on the
clarified version. The earlier failure and timeout remain recorded above.
The user authorized commit and push to `main` after these checks passed.
