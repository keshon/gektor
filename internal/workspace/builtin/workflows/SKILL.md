---
name: workflows
description: Create, validate, run and inspect GEKTOR recipes; connect node outputs and manage native schedule triggers.
---

# GEKTOR workflows

In normal Agent mode, enable `native_workflows`, then read `workflow_schema`.
Draft-only roles use their available schema tool or supplied contracts instead.
The operation contracts
describe inputs, outputs, execution effects, concurrency and retry safety. Use those contracts;
do not search for an external runtime. `script_transform` runs JavaScript in a
bundled interpreter, with JSON `input` and a JSON return value. It has no file,
network, environment or module access. Python and Node.js are not needed.

Use recipe version 4. A recipe has `version`, `name`, `steps`, and optional
`inputs` and `outputs`. Steps use `id`, `tool`, `args` and documented behavior
fields. Steps do not have an `outputs` field.

For a collection, supply `args: {}` and `for_each` as an array of complete
argument objects or a reference to one. Set `concurrency` to 1–32 only when the
operation contract declares `concurrent: true`; omission runs one item at a time.
Use `on_error: "collect"` when operation failures must become reportable results.
The output is `{items: [{index, status, output?, error?}]}` in input order.
Select `/items/0/output` for the first item's value when it succeeded. A completed map
can contain failed items; inspect statuses before claiming all checks succeeded.
Permissions, validation, malformed outputs, persistence and cancellation still
stop the run. These fields cannot be used outside `for_each`. A map accepts at
most 10,000 items subject to the ordinary argument and output byte limits.

`inputs` declares types and defaults. `outputs` contains returned VALUES or
references, not type declarations. `{"type":"integer"}` in outputs returns
that literal object; it does not describe or select an integer result.

This complete recipe returns `{"doubled":6}` with its default input:

```json
{
  "version": 4,
  "name": "Double count",
  "inputs": {"count": {"type": "integer", "default": 3}},
  "steps": [{
    "id": "double",
    "tool": "script_transform",
    "args": {"input": {"$input": "count"}, "code": "return input * 2;"}
  }],
  "outputs": {"doubled": {"$step": "double"}}
}
```

References are JSON objects, never string interpolation. `{"$step":"double"}`
uses the entire return value. Add `"pointer":"/total"` only if that value is
an object with a `total` field. There is no automatic `/output` wrapper.
For a script returning `{"total":6}`, that pointer selects `6`.
Use `docx_read`, `xlsx_read` and `pdf_read` for document content; they are native
read-only nodes. XLSX without `sheet` lists sheet names. Follow `next_offset`
while `more` is true. Finish every item with `next_text_offset` too, even when
`more` is false: repeat its zero-based item `offset` with `text_offset` copied
from `next_text_offset`; `limit` defaults to 1 and must be 1. Text positions count
Unicode code points. Append chunks to the same fields/cell positions; keep the
source unchanged. Empty or omitted fields in a truncated item may still be
unread; finish continuation before claiming absence. XLSX cell type describes
the stored value, not formula presence. Reads are bounded and never fetch continuations automatically.
Read only needed content and retain useful findings during long scans. No OCR or
legacy DOC/XLS.
Archive nodes support explicit `format`: zip (default), tar, tar.gz and gzip.
GZIP is one file. No external runtime is needed.

Paths are workspace-relative. Read a registered record schema with
`workflow_schema(record_kind)` before using records; only the operator can
register schemas. Do not invent record kinds. Artifacts store run outputs;
they are not ordinary workspace files.

For recipe tasks, inspect sources as needed, then save the recipe and call
`workflow_validate` with required input values. Produce outputs through
`workflow_run`; do not perform the same writes directly first. Manual runs
need no worker service.
Validation checks structure, types and references; it does not prove the recipe
fulfills the request. When execution is authorized, call `workflow_run`, inspect
its actual declared outputs with `workflow_inspect`, and compare them with a
known expected result. For this example test count 3 and a different count.
When execution is forbidden, report that result correctness remains unverified.
`workflow_inspect` defaults to a compact summary. Use `view:"steps"`, then
`view:"step"` with `step_id` for details. `pointer` selects within declared outputs
or a step output before the 8 KiB value limit; `view:"full"` returns the full record.
Use `workflow_status` to find recent jobs and run IDs, then inspect failures.
Do not repeat effectful steps merely to diagnose an error. Retries require
the operation's retry-safe contract.

Schedules are native triggers stored outside recipe JSON. Do not put a cron
operation in `steps`. `workflow_schedule` creates, updates, pauses, resumes and
removes them; `workflow_schedules` lists current settings and snapshot revisions.
An every-minute trigger uses `"cron":"* * * * *"` and an explicit time zone
such as `UTC`. Use `"paused":true` when asked to leave it disabled. Creation
does not run the recipe or start a service. A trigger freezes the recipe, called
recipes and inputs. For an update, first list schedules and pass its
`expected_revision`; choose explicitly whether to refresh the saved recipe.

The operator must keep `gektor workflow serve -workspace <workspace>` running.
Its own permission rules govern execution. Filter `workflow_status` by
`schedule_id` for settings, latest job/run, outputs and snapshot changes.
`changed` requires an explicit snapshot refresh; `unavailable` means source
comparison could not finish. Inspection does not request more file access.
Execution success and the meaning of declared outputs are separate.
Use `workflow_status` to distinguish
a saved trigger, a held worker lease, a queued job and a completed run. A held
lease means a worker process owns the queue; it does not prove that the worker
is healthy or that a recipe succeeded. Do not substitute an OS scheduler.

The dedicated recipe-author role and editor AI proposal are draft-only. They
cannot execute recipes or register schedules. Prepare and validate the recipe,
then explain the handoff to the normal Agent or the Schedule card in the TUI.
