# Recipe format and execution

Look up recipe structure, data mappings, conditions, reviews and reusable calls. The [workflow guide](../guide/workflows.md) covers opening and running a recipe.

## Recipes and storage

A recipe is a JSON file that describes steps, their inputs and their connections.
A run is one execution of that recipe. It records the input values, results and
status separately from the recipe file.

For the everyday TUI flow, read the [terminal guide](../guide/workflows.md#recipes-and-runs).
This page covers operation names, data formats and execution rules.

Recipes live inside the selected workspace. Runs and other workflow state live
under `~/.gektor/workflows/`, separated by workspace. See
[saved data](configuration.md#saved-data) for the locations.
There is no shared template library yet. Copy a recipe into another workspace
to use it as a starting point, then check its paths and profiles.

Schedules start workflows at set times. Manage them in the
[TUI editor](../guide/automation.md#schedule-a-workflow), through agent tools or with the
[CLI and service](cli.md#workflow-queue-and-service). The workflow service must
be running for scheduled jobs to execute.

## Image recipe example

Ask GEKTOR:

> Create a recipe for input.png: inspect it, preview 320px and 800px variants,
> then write those variants to a new directory called web-images. Save the
> recipe as recipe.json, validate that file, then run it once and report the saved record path.

```json
{
  "version": 4,
  "name": "Prepare web images",
  "steps": [
    {"id": "inspect", "tool": "image_inspect", "args": {"paths": ["input.png"]}},
    {"id": "preview", "tool": "image_preview", "args": {"paths": ["input.png"], "widths": [320, 800]}},
    {"id": "write", "tool": "image_variants", "args": {"paths": ["input.png"], "widths": [320, 800], "out_dir": "web-images"}}
  ]
}
```

Runs, invocations, attempts, queued jobs and schedules are stored transactionally
in `~/.gektor/workflows/<workspace-hash>/workflow.sqlite`.
The workspace hash uses its absolute, symlink-resolved path (case-folded on
Windows). Moving a workspace starts a separate history. The store must be outside
the workspace. `database_path` is absolute; `run_id` is the inspection handle.
The SQLite driver is pure Go and builds with `CGO_ENABLED=0`. Collection
checkpoints update the changed invocation rather than rewriting the full run.
Artifact payloads remain in per-run files. Existing JSON run histories and queue
files are not read or imported.

Use `gektor workflow runs -workspace .` to list runs, and
`gektor workflow inspect -workspace . <run-id>` for the full record. The model
can use `workflow_inspect` with `{"run_id":"..."}` for a compact summary.
The summary includes execution status, timing, step counts, failures and declared
outputs. A recipe's output, such as `status: stale`, keeps its own meaning;
`completed` only describes execution.

Use `view: "steps"` for a step list, or `view: "step"` with `step_id` for one
step's resolved arguments and output. `view: "full"` returns the full stored
record. Summary and step values use `{value, bytes, omitted}`: `value` contains
the JSON value when present; values over 8 KiB are omitted with their byte size.
Use `pointer` to select a small field before that limit is applied. In summary
view it selects inside the declared outputs; in step view it selects inside
that step's output. For example:

```json
{"run_id":"...","pointer":"/summary/status"}
```

Inspection reports
`interrupted` when a persisted running run no longer holds its OS process lock;
the recorded status remains unchanged. A crash can leave partial step effects.
Each completed step is saved before the next step is prepared. If saving fails,
execution stops; an effect may have happened even when its result could not be
persisted. Writes use a synced temporary file and rename; this is not a power-loss recovery
or workspace rollback guarantee.

Steps run sequentially and stop on failure; remaining steps are marked skipped.
Inspect outputs before deciding to rerun;
the runner does not replay interrupted effects automatically. Earlier completed writes
remain if a later step fails. Each rerun needs fresh output directories.

The tool response contains step outcomes, item results and source hashes; full
outputs and recipe source remain in the run record. Start a fresh GEKTOR session
when an update changes tool schemas: old saved activations may be incompatible.

## Inputs and data flow

Version 4 is the current recipe format. Other formats are not supported.
Recipes support typed inputs and references to dependency outputs. For example:

```json
{
  "version": 4,
  "name": "Resize and compare",
  "inputs": {
    "source": {"type": "string", "required": true},
    "destination": {"type": "string", "default": "web-images"}
  },
  "steps": [
    {"id": "write", "tool": "image_variants", "args": {
      "paths": [{"$input": "source"}], "widths": [320],
      "out_dir": {"$input": "destination"}
    }},
    {"id": "compare", "tool": "image_compare", "args": {
      "paths": [{"$input": "source"}, {"$step": "write", "pointer": "/items/0/output"}]
    }}
  ]
}
```

Validate and execute with the same inputs:
`{"recipe_path":"recipe.json","inputs":{"source":"input.png"}}`.
Each run independently reads the saved recipe and binds its inputs.

Input types are `string`, `integer` (signed 64-bit JSON integer), `number`,
`boolean`, `array` and `object`. Unknown inputs, invalid defaults and missing
required values fail before execution. An omitted optional input cannot be
referenced unless it has a default. Input values cannot contain the reserved
`$input` or `$step` reference objects. No string interpolation or code evaluation
is performed.
Reference-like strings such as `"{$input:source}"` are rejected in
image paths; use `{"$input":"source"}` as the entire value instead.

`pointer` uses JSON Pointer syntax: `/items/0/output` selects a field in an
earlier result; `~1` escapes `/` and `~0` escapes `~`. An omitted or empty pointer
selects the whole result. References determine execution order regardless of array position. Self references,
unknown references and dependency cycles are rejected.
Missing output fields and wrong resolved argument types fail the dependent step;
remaining steps are skipped. Validation checks templates, not future output
values or file existence.

There are at most 32 inputs, 64 KiB of bound inputs or resolved step arguments,
64 levels of argument nesting and 1 MiB of output per step. Oversized output
fails the run and is truncated in the record. Run records retain the original
definition, bound inputs and each step's resolved arguments. The user store is
outside ordinary workspace file access, but programs running as the operator
can still change it. Do not pass secrets as inputs.

## Graphs, iteration and reviews

Array order is not a dependency. Use `depends_on: ["prepare"]` for an ordering
edge; `$step` references also create edges. The validator rejects cycles and
unknown dependencies. Independent ready nodes execute in stable array order.
Execution is currently sequential.

`when` accepts a boolean or a typed input/output reference resolving to one.
False skips that node. `merge: "all"` (default) requires all dependencies to
complete; `merge: "any"` requires at least one. A reference to a skipped output
still fails; merges do not invent missing values. The run stops on failure.

`for_each` accepts up to 10,000 complete argument objects, or a reference resolving
to that array. The ordinary 64 KiB argument limit also applies. Set `args` to `{}`.
For `workflow_call`, use the child input mapping described under reusable subworkflows.
Every item is validated and authorized separately. `concurrency` defaults to 1
and accepts 1–32. Values greater than 1 require the operation's explicit
`concurrent` contract; this is independent of read-only and retry-safe status.
The bundled script and HTTP operations support concurrent invocations.

A run has at most 100,000 invocation slots, counting recipe steps and all
collection items. A collection reserves its entire array before any of its
items execute. If it does not fit, that step fails even with `on_error: "collect"`;
earlier effects remain recorded. Failed and skipped items retain their slots,
and review continuation retains the same budget. Retries reuse an item's slot
and remain subject to the separate attempt limit.

Results retain input order, even when items finish out of order. Each item has
`index`, `status`, optional `output` and optional `error`. For example:

```json
{"items":[{"index":0,"status":"completed","output":4},{"index":1,"status":"failed","error":"site unavailable"}]}
```

`on_error` defaults to `stop`, which cancels and joins in-flight work after the
first observed failure. `on_error: "collect"` retains operation failures and
continues other items so a report can inspect both successes and failures.
Operations classify their own collectable failures. The bundled script and HTTP
nodes do this; unclassified errors from other operations remain terminal.
Permission, validation, malformed-output and checkpoint failures still stop the
run. Cancellation is never collected as success. A completed map can contain
failed items; inspect their statuses. Total collection output is bounded to 1 MiB.
Checkpoints precede effects and preserve partial outcomes. All started workers
are joined before execution returns. Cancellation cannot undo effects already
performed. `concurrency` and `on_error` apply only to `for_each` steps.

The [collection report example](../../examples/workflows/collection-report/README.md)
runs this behavior without a model or network.

`review: true` pauses before an operation, after resolving its arguments.
Use `workflow inspect` to examine it, then `workflow review -decision approve
<run-id> <step-id>` to continue the saved snapshot. Completed steps are not
repeated. `-decision reject` cancels the remaining work. Permissions are checked
again when continuing. The agent cannot approve its own run.

`retry: {"max_attempts": 2, "delay_ms": 250}` is allowed only for nodes explicitly
declared retry-safe. There are at most three attempts and a ten-second delay.
Optional `codes` restricts retries to exact operation failure codes, for example
`retry: {"max_attempts": 2, "delay_ms": 250, "codes": ["http_request_failed"]}`.
Omitted or empty `codes` accepts any typed operation failure. Codes are
case-sensitive, unique, at most 32 entries of 1–64 bytes, without surrounding
whitespace. Unknown codes match nothing; they do not grant retry safety.
An operation without a code uses `operation_failed`. `http_status` covers all
unsuccessful HTTP statuses, so it does not distinguish a 404 from a 503.
Every attempt records its error and typed failure code, including attempts before
a later success. Error routes run only after retry exhaustion or a nonmatching
failure; finalizers follow the settled result. Cancellation (including typed
context cancellation/deadline errors) and checkpoint failures stop retries.
Permission and output-contract failures are not
retried. Record writes and model calls are not automatically
retried. A crash during an operation remains interrupted; the service does not
infer whether an external effect happened.

`node_version` defaults to 1. A recipe requesting another version fails unless
that exact version is registered. This is separate from the recipe format version.

## Error routes and finalizers

An error route names the ordinary step whose operation failure should trigger it:

```json
{
  "id": "notify",
  "tool": "artifact_write",
  "on_failure": "fetch",
  "args": {
    "name": "failure.txt",
    "text": {"$step": "fetch", "pointer": "/error/message"}
  }
}
```

`on_failure` adds an ordering dependency on `fetch`. The route runs only when
that step has a typed operation failure; success or skipping leaves the route
skipped. Additional dependencies still follow the normal `merge` rules. Routes
may have conditions, reviews, waits, collections or child calls. A route targets
an ordinary step, not another error route or finalizer. Several routes may
observe the same source failure. For a multi-step error-handling graph, use a
child recipe as the route's operation.

A typed failure is recorded separately as `failure:{"code":"…","message":"…"}`.
For references, the failed step exposes `{"error":{"code":"…","message":"…"}}`.
The original partial operation output remains in the stored invocation's
`output` for inspection; it is not returned as a successful `$step` value.
Collection item records and outcome arrays also retain typed `failure` metadata.

Native operation failures use these codes:

| Node | Codes |
| --- | --- |
| HTTP | `http_status`, `http_request_failed`, `http_response_failed`, `http_response_too_large` (body or selected header limit) |
| Isolated script | `script_failed` |
| Callback wait | `callback_expired` |
| Native process | `process_exit`, `process_output_too_large` |
| Model generation | `model_request_failed`, `model_incomplete`, `model_response_too_large` |

Process exit/overflow failures are routable only after stdout/stderr artifacts
are saved. Failed artifact writes remain immediate stops. Startup failures,
process supervision errors and timeouts are not classified as operation failures.
Model profile resolution, request limits and structured-output validation also
remain immediate stops. A provider error, incomplete generation or oversized
response is routable, but model/process nodes still forbid automatic retries.
HTTP URL/DNS destination policy denials remain immediate stops. Context
cancellation and deadline errors retain their identity through HTTP redaction.

Nodes without a specific code use `operation_failed` when explicitly marking an
operation failure. Only explicitly typed failures are routable; other nodes'
plain errors stop execution. This boundary prevents a permission, schema, path,
artifact-store or checkpoint failure from silently becoming ordinary error-branch
data. Record/artifact nodes retain this conservative boundary; no blanket error
wrapping is applied. A typed failure does not establish whether an external effect
happened and does not grant permission to retry it.

After a typed operation failure, remaining ordinary work in that scope is
skipped. Matching error routes may execute, followed by finalizers. Neither an
error route nor successful finalization turns the run into success: the scope
finishes `failed` and reports its first operation failure. Later route/finalizer
failures remain inspectable without replacing that primary cause. Successful
`on_error:"collect"` collections still return their item outcomes normally; they
do not trigger an error route on the collection container.

A finalizer is an ordinary operation marked `finally:true`:

```json
{"id":"finish","tool":"artifact_write","finally":true,"args":{"name":"finished.txt","text":"finished"}}
```

Finalizers run after ordinary steps and error routes settle, on success or typed
operation failure. Their position in the saved file does not move them ahead of
that work. Explicit dependencies and references can order finalizers relative to
each other; ordinary work and error routes cannot depend on a finalizer. A step
cannot combine `finally` and `on_failure`. `depends_on` on a finalizer orders
settled work rather than requiring success. A reference still needs a value:
referencing a skipped step or a nonexistent field is a contract error. Prefer
static finalizer arguments or references known to exist on both paths.

Each route and finalizer keeps its own permission and argument/output checks.
Cancellation, permission denial, invalid contracts and checkpoint failures stop
execution immediately; they do not dispatch more routes or finalizers. Such an
immediate-stop error takes precedence in the returned error; earlier operation
failures remain in inspection. Finalizers are not process-exit cleanup and do not run while a process is stopped.

Reviews and durable waits retain the original failure until continuation.
Completed routes/finalizers are not repeated. A process stopped during their
active effects remains interrupted and needs explicit operator recovery. Known
typed failures can be retained during that recovery while remaining uncertain
operations still require decisions. Frozen child scopes use the same rules;
an uncollected child failure reaches its caller as a typed operation failure.
Declared output collection is skipped when the scope fails.

See the [error-routing example](../../examples/workflows/error-routing/README.md)
for a model-free recipe that writes failure and finalization artifacts.

## Durable deadline waits

`workflow_wait` persists a deadline and returns control to its caller:

```json
{"id":"delay","tool":"workflow_wait","args":{"delay_ms":60000}}
```

`delay_ms` is required and accepts integer values from zero to 604,800,000
(seven days). The deadline is recorded once with millisecond precision. The run
and step become `waiting`; summaries and inspection expose `resume_at`. No worker
or run lease is retained while waiting. Even a zero delay yields through the
persisted wait state rather than sleeping inside the operation.

Keep `gektor workflow serve -workspace <workspace>` running to continue due waits.
This applies to manual runs as well as queued or scheduled jobs. The worker
prioritizes queued jobs and checks due waits when that queue is empty; a deadline
is eligibility to resume, not a precise delivery-time guarantee. Without the
worker, the run stays waiting until a worker starts. The worker's current policy
must permit `workflow_run` and the remaining operations. Continuation rechecks
those permissions and uses the frozen recipe; completed operations are preserved.

The wait and its deadline commit in one SQLite transaction. Claiming a due run
holds its existing execution lease. Starting continuation consumes the wake-up
before further effects. A process exiting while waiting leaves it resumable;
exiting after execution starts leaves an interrupted run requiring inspection.
No unknown external effect is automatically replayed. The CLI run timeout governs
that active CLI invocation; it is not a stored wall-clock deadline for later
worker continuations.

Waits work inside ordinary child calls, serial child maps and repeated child
workflows. `workflow_wait` cannot use `for_each` or `retry`; place the wait inside
a child recipe instead. A review may precede a wait or occur after it. Cancelling
a waiting queued job makes it claimable immediately so the worker can persist
cancellation without executing remaining operations. Waiting jobs remain active
for queue retention and schedule-overlap rules. At most 1,000 runs may wait in a
workspace store. Waiting-run artifacts are retained.

See the [durable wait example](../../examples/workflows/durable-wait/README.md).

## Callback waits

```json
{"id":"approval","tool":"workflow_wait_callback","args":{"channel":"approval","timeout_ms":60000}}
```

`channel` is an operator-configured HTTP callback channel; `timeout_ms` is a
required integer from 1 to 604,800,000 (seven days). The run becomes `waiting`
and releases execution. Inspection and run summaries expose a `callback` receipt
with `wait_id`, qualified `step_id`, `channel` and `expires_at`. The wait ID is
public routing identity; the channel bearer token grants delivery access.

The worker resumes after an event is accepted, or fails the wait on expiry
without an event. Completed output is `{"event_id":"…","payload":…}`. Bind a
following step with `{"$step":"approval","pointer":"/payload/approved"}`.
Treat payloads as untrusted JSON data. They cannot replace the frozen recipe.

Child calls, serial maps, repeats and preceding reviews use the same continuation
path as deadline waits. The wait node itself cannot use `for_each` or `retry`.
Current worker permissions must allow the callback wait and remaining operations.
The same waiting-run limits and job cancellation rules apply. `resume_at` is the
next worker eligibility time: initially expiry, then immediate after acceptance.
The receipt's `expires_at` remains the original delivery deadline.

See [callback delivery](http-api.md#callback-waits) for authentication, limits,
duplicate responses and the early-delivery restriction. No event is buffered
before its wait checkpoint exists.

## Recovering interrupted execution

A process that stops during active work leaves the run `interrupted`. GEKTOR
cannot know whether an SSH command, request or write finished remotely. Inspect
the run, artifacts and external target before choosing a recovery action.

`workflow inspect <run-id>` lists uncertain invocation IDs in `uncertain`; child
and collection IDs keep their full scope, for example `call/batch[1]`. Agent
inspection also exposes uncertain operations and recovery history. Inspection
never retries anything.

Only the operator CLI can recover a run. Create a workspace-relative JSON file:

```json
{
  "note": "Checked the target: the request completed successfully",
  "decisions": [
    {
      "step_id": "send",
      "action": "complete",
      "output": {"accepted": true}
    }
  ]
}
```

Then run:

```text
gektor workflow recover -workspace work -recovery recovery.json <run-id>
```

Each uncertain invocation needs exactly one decision:

- `complete` records the verified JSON output without executing the operation.
  The output must satisfy the registered node's output schema. This does not
  create missing artifacts or verify remote success for you.
- `retry` explicitly permits another execution, which may repeat an external
  effect. Omit `output`. Current permissions still apply.

Completed steps and collection items retain their outputs. Child scopes, maps,
repeats, reserved invocation slots and attempt history are preserved. Pure call
binding and wait bookkeeping resume through the existing executor. If a process
stopped between effects and no invocation is uncertain, use an empty `decisions`
array with a note to continue pending work.

The entire decisions document is limited to 64 KiB. A nonblank note of at most
2,000 bytes is required; a run supports at most 32 recovery records. The time,
note, actions and supplied-output SHA-256 digests persist before resumed effects.
Full outputs stay on their invocation, outside the audit history. If execution
stops again, inspect the new checkpoint before making further decisions.

Recovery takes the existing exclusive run lease and rechecks frozen recipe
identity, current registry contracts and permissions. Completed, failed,
cancelled, waiting and actively owned runs cannot be recovered this way. An
unfinished scope containing an untyped failed or cancelled operation is also
rejected. Known typed failures remain settled so pending error routes and
finalizers can continue; this command does not retry those failures. A linked job
with pending cancellation cannot be recovered. Linked jobs retain their original
run ID and cancellation monitor during continuation. A failed recovery commit
prevents dispatch. Recovery is not available to Agent tools or HTTP callers.

## Reusable subworkflows

`workflow_call` invokes a saved recipe using a literal workspace-relative
`recipe_path` and an explicit `inputs` object. Values may use the caller's `$input`
and `$step` references. Called recipes declare an `outputs` object; these named
values become the call step's output. Internal step outputs stay private to the
child's namespace. A top-level recipe may also declare `outputs`, returned in the
completed CLI summary.

Calls instantiate separate execution scopes at runtime from their frozen
definitions. A skipped call creates no child scope. References inside the child
resolve against its own inputs and results. Inspection and artifact ownership use
qualified IDs (`daily/save`); artifacts belong to the parent run. Each child
operation keeps its own permission check. Calling the recipe checks
`workflow_call` against its normalized path, including when continuing a review;
reading its source checks `read_file`. Allowing a call does not override denials
on its operations. A call's `review: true` gates entry at `daily`; reviews inside
the child use their qualified step IDs. Inspection includes call containers,
child operations and output collectors. Rejecting a pending review cancels the
whole run and its paused scopes. Completed child work is preserved on approval.

Scope metadata, invocation state and attempts have separate SQLite rows.
A nested item checkpoint updates that item and enclosing execution statuses
without rewriting frozen scope definitions or unrelated items. All scopes share
the run's 100,000 invocation slots. Calls execute serially; leaf collections inside
the active scope retain their 1–32 worker limit.

Queue submission and scheduling freeze the root recipe and its dependency closure.
Manual runs freeze them at startup. The run hash covers that bundle; the compiled
definition is persisted for review continuation, which does not reread recipe files.
Other workspace inputs, programs and profiles remain live unless captured as artifacts.
Paths inside child recipes remain relative to the workspace, not the child file's
directory. Validation never executes a child. Static inputs are type-checked before
execution; values from earlier steps are checked at the call boundary.

Call limits:

- Four nested calls.
- 16 authored steps per recipe.
- 128 authored step occurrences across the root and called recipes.
- A 64 KiB serialized recipe bundle.
- No recursive calls.

Step IDs cannot contain `/` or begin with `@`; those namespaces belong to composition.
Each local step ID has a 64-byte limit; qualified inspection IDs include its
enclosing call path.

A call can use `for_each` to invoke the same frozen recipe for each child input
object. Keep the literal destination in `args` and put inputs in the array:

```json
{"id":"batch","tool":"workflow_call","args":{"recipe_path":"child.json"},"for_each":[{"text":"first"},{"text":"second"}],"on_error":"collect"}
```

Mapped calls reserve all item slots before starting and instantiate each child
scope when its turn arrives. Child steps consume the same remaining invocation
budget; exhaustion stops execution and preserves earlier effects. Duplicate input
objects have separate identities. Outcomes use the ordinary ordered `items`
envelope. Inspection and artifact ownership use IDs such as `batch[0]/save`.

Calls accept concurrency 1. Each child finishes or pauses before another starts;
leaf collections inside it retain their own concurrency limit. A map's `review`
gates the whole array once. Reviews inside a child pause that item and preserve
earlier completed or collected-failure items. Approval rechecks permissions and
continues the saved child. Rejection cancels pending items and paused scopes.
Calls cannot use `retry`; ordinary nodes inside a child retain their retry rules.
`on_error: "collect"` retains only explicitly classified execution failures;
permission, validation, output-contract, budget and persistence errors remain terminal.

A call can instead use `repeat` for pagination or short polling:

```json
{"id":"pages","tool":"workflow_call","args":{"recipe_path":"page.json","inputs":{"cursor":0}},"repeat":{"max_iterations":100,"until":"/done","next_inputs":"/next","delay_ms":100}}
```

The first iteration uses `args.inputs`. After each child completes, `until`
selects a boolean using a JSON Pointer into its declared output. `true` ends the
repeat and returns that entire final child output. Otherwise, `next_inputs`
selects a complete input object for the next call, replacing the previous inputs.
Missing/null values and wrong types fail explicitly. The next-input pointer is
not read after `until` becomes true. Reaching the limit while it is false fails;
it is not a successful partial result.

All three fields `max_iterations`, `until` and `next_inputs` are required.
The iteration limit is 1–10,000, subject to the shared run invocation budget.
`delay_ms` defaults to zero and accepts 0–10,000 ms between iterations. This delay
occupies the execution slot and is cancelled with the run; it is not durable
and does not provide restart recovery. Repeats cannot combine with `for_each`,
`concurrency`, `on_error` or `retry`. Child operations retain their own retry rules.

Each iteration has its own checkpoint, child scope, permissions and artifact
ownership (`pages[0]/save`). A repeat's `review` gates entry once; child reviews
pause the current iteration. Approval retains completed iterations and rejection
cancels the paused scopes. Interrupted effects are never automatically replayed.
Only the final child output becomes the repeat's output; inspect iteration history
or publish artifacts when earlier pages must remain available.

The [repeated subworkflow example](../../examples/workflows/repeated-subworkflow/README.md)
uses deterministic local pages without a model or remote service.

The [mapped subworkflow example](../../examples/workflows/mapped-subworkflow/README.md)
runs mixed outcomes and duplicate inputs without a model.

Every declared output
must resolve; an output referring to a skipped step fails collection.
Constant and input-only outputs are still collected when all effect steps skip.
A skipped call does not collect its child's outputs.

See [the reusable report example](../../examples/workflows/subworkflow/README.md).
