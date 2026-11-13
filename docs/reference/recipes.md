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
Every attempt is recorded. Permission and output-contract failures are not
retried. Record writes and model calls are not automatically
retried. A crash during an operation remains interrupted; the service does not
infer whether an external effect happened.

`node_version` defaults to 1. A recipe requesting another version fails unless
that exact version is registered. This is separate from the recipe format version.

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

The [mapped subworkflow example](../../examples/workflows/mapped-subworkflow/README.md)
runs mixed outcomes and duplicate inputs without a model.

Every declared output
must resolve; an output referring to a skipped step fails collection.
Constant and input-only outputs are still collected when all effect steps skip.
A skipped call does not collect its child's outputs.

See [the reusable report example](../../examples/workflows/subworkflow/README.md).
