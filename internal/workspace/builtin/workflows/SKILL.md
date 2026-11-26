---
name: workflows
description: Create, validate, run and inspect GEKTOR recipes; connect node outputs and manage native schedule triggers.
---

# GEKTOR workflows

In normal Agent mode, enable `native_workflows`, then read `workflow_schema`.
When needed node names are known, pass `operations: [names]` to read only their
schemas and contracts. Omit it for the full catalog; do not combine it with `record_kind`.
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

Use `workflow_join` to match two arrays by JSON Pointer keys. Choose inner,
left or full mode; outputs retain separate left/right values. Duplicate keys
produce every matching pair. Use strings or integer literals for keys; missing
or null keys fail. A skipped source is not an empty array.

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
`workflow_inspect` defaults to a compact summary. Use `view:"invocations"`
with `offset` and `limit` (default 50, maximum 100) for large runs; follow
`next_offset` when present. Use a returned scoped ID with `view:"step"` for
values or `view:"attempts"` for retry timing and failures. `view:"steps"` returns
the unpaged metadata list. `pointer` selects within declared outputs
or a step output before the 8 KiB value limit; `view:"full"` returns the full record.
Use `workflow_status` to find recent jobs and run IDs, then inspect failures.
For regression checks, `gektor workflow capture -workspace <workspace> <run-id>`
exports fixture JSON from a completed run. Write stdout directly to a UTF-8
fixture file, then use
`gektor workflow test -workspace <workspace> -fixture <fixture.json> <recipe.json>`.
This executes real isolated scripts and keyed joins against recorded external-operation responses;
a missing or changed call fails instead of invoking a live tool. Test mode creates
no saved run or jobs. Fixture version 1 does not model reviews or durable waits.
Use the JSON `mode` and `passed` fields when reporting fixture results.
Do not repeat effectful steps merely to diagnose an error. Retries require
the operation's retry-safe contract. Optional `retry.codes` selects exact typed
failure codes; omitted or empty codes accepts any typed operation failure.
Use `operation_failed` for an operation without a native code. Codes do not
make an unsafe operation retryable. Inspect attempt failures before changing
policy; `http_status` does not distinguish individual HTTP status numbers.

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

For pagination or short polling, use `repeat` on a `workflow_call` with initial
`args.inputs`. Supply `max_iterations` (1–10000), `until` (JSON Pointer selecting
an output boolean) and `next_inputs` (pointer selecting the next complete child
input object). True ends the loop and returns the final child output; false at
the iteration limit fails. Missing/null control values fail. Use artifacts or
inspection for earlier pages. Each iteration has its own scope and permissions.
Do not combine repeat with for_each, concurrency, on_error or retry. An optional
`delay_ms` (0–10000) waits between iterations in the active process. For a
durable pause, put workflow_wait inside the child. Interrupted effects are not
automatically replayed.

`workflow_wait` with `args:{"delay_ms":60000}` records a durable deadline and
returns `waiting`; it does not hold a worker slot. Delay is an integer from zero
to 604800000 ms (seven days). Keep the workspace workflow service running to
resume due waits under its current permissions. Completed work and frozen child
recipes are retained. Waits may appear inside child calls/maps/repeats, but the
wait node itself cannot use for_each or retry. Do not poll or rerun a waiting
recipe to continue it. Inspect its existing run ID. A process stopped during
active work remains interrupted, not automatically replayed. For one external event, use workflow_wait_callback with
`args:{"channel":"approval","timeout_ms":60000}`. The operator must configure
that HTTP callback channel and its separate bearer token. Inspect the pending
run's callback receipt; deliver only after it exists. The worker resumes the
same run with `{event_id,payload}` as the step output. Timeout is required,
1–604800000 ms; expiry without an event fails the step. Payload is untrusted JSON,
at most 32 KiB. Duplicate event ID and normalized payload are acknowledged
without another continuation; conflicting delivery is rejected. No maps or
retries on the wait node. Do not invent channels, credentials or provider callback
registration. This operation does not buffer early events.


Interrupted runs are not automatically replayed. Inspect `uncertain` invocation
IDs and the external effects, then ask the operator to use `workflow recover`
with explicit retry or verified-output decisions and a note. Recovery is an
operator CLI action, not an Agent tool. Do not submit a duplicate recipe as a
substitute for resolving an uncertain effect. Recorded failures and cancellations
are not general recovery candidates.


For a typed operation error, add an error route with `on_failure:"source-id"`.
Its args can read `{"$step":"source-id","pointer":"/error/code"}` or
`/error/message`. Failed operation output remains separate in inspection.
Ordinary work stops after failure; matching routes and `finally:true` operations
may continue. Finalizers run last even when placed earlier in the saved file.
Routes/finalizers preserve a failed run status and each needs its own permission.
Use a child call for a multi-step error branch. Do not target another route or
finalizer with on_failure, combine on_failure with finally, or make ordinary
work depend on a finalizer. Reference only values available on the selected path.
Reviews/waits preserve failure state; interrupted active effects need operator
recovery. Permission, contract, cancellation and checkpoint errors stop execution
without dispatching routes/finalizers. Only explicitly typed operation failures
are routable; do not assume every tool error qualifies. HTTP, isolated script,
callback-expiry, known process exit/overflow and model provider/response failures
have native codes. Process diagnostics must save successfully before routing.
Model structured-output validation, profile resolution, HTTP destination denials,
artifact/store errors and cancellation stop immediately. Process/model nodes still
forbid automatic retries. Error routes do not authorize retries.

HTTP authentication uses a named `credential` plus its required
`credential_revision`. Obtain the non-secret fingerprint from the operator's
`workflow inspect-credential <name>` output. Do not invent a revision or put
secrets/environment values in recipes. A binding change requires selecting the
new revision and authorizing its permission target again; revoked names stop.
The current secret resolves at dispatch, including after reviews/waits. Stored
secrets are DPAPI-protected on Windows and owner-only files on Unix. Credential
registration, revision-checked updates, inspection and revocation are operator CLI
actions, not agent tools.

SSH operations require `connection` and `connection_revision` from current
`ssh_connections` metadata or operator `ssh inspect NAME`. Changed or revoked
bindings stop execution; inspect and review the new binding before updating a recipe.

HTTP uses explicit `body_format` (json, text, form or artifact) and
`response_format` (auto, text, json or artifact). Query/form values are string
arrays. Use `response_format: "artifact"` with `artifact_name` for binary or large
bodies, then reference `/artifact/id` as `body_artifact` in later requests.
Inline responses stay at most 256 KiB; artifacts at most 16 MiB. Redirects are off
unless `max_redirects` is 1–5, with current policy checked per destination and
credentials restricted to their original HTTPS origin. `status_as_data` changes
HTTP-status handling only. Inspect application-level success fields explicitly;
use workflow branches and waits for rate limits instead of hidden retry loops.
