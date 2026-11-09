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
  "version": 3,
  "name": "Prepare web images",
  "steps": [
    {"id": "inspect", "tool": "image_inspect", "args": {"paths": ["input.png"]}},
    {"id": "preview", "tool": "image_preview", "args": {"paths": ["input.png"], "widths": [320, 800]}},
    {"id": "write", "tool": "image_variants", "args": {"paths": ["input.png"], "widths": [320, 800], "out_dir": "web-images"}}
  ]
}
```

Each run saves its definition, digest, resolved arguments, per-step outputs and
status under `~/.gektor/workflows/<workspace-hash>/runs/<run-id>/run.json`.
The workspace hash uses its absolute, symlink-resolved path (case-folded on
Windows). Moving a workspace starts a separate history. The store must be outside
the workspace. `record_path` is absolute; `run_id` is the inspection handle.

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

Version 3 supports typed inputs and references to dependency outputs. Earlier
unreleased recipe formats are not supported. For example:

```json
{
  "version": 3,
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

`for_each` accepts up to 100 complete argument objects, or a reference resolving
to that array. Set `args` to `{}`. Items execute sequentially using the selected
node; every item is validated and authorized separately. The result is
`{"items": [...]}`. Each iteration is saved before the next starts, so partial
progress remains inspectable. Total iteration output is bounded to 1 MiB.

`review: true` pauses before an operation, after resolving its arguments.
Use `workflow inspect` to examine it, then `workflow review -decision approve
<run-id> <step-id>` to continue the saved snapshot. Completed steps are not
repeated. `-decision reject` cancels the remaining work. Permissions are checked
again when continuing. The agent cannot approve its own run.

`retry: {"max_attempts": 2, "delay_ms": 250}` is allowed only for nodes explicitly
declared retry-safe. There are at most three attempts and a ten-second delay.
Every attempt is recorded. Record writes and model calls are not automatically
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

Calls expand into one execution graph before execution. Child IDs are qualified
(`daily/save`), artifacts belong to the parent run, and every child operation keeps
its own permission check. Calling the recipe also checks `workflow_call` against
its normalized path; reading its source checks `read_file`. Allowing a call does
not override denials on its operations. A call's `review: true` gates entry at
`daily/@inputs`; reviews inside the child use their qualified step IDs. Inspection
shows input-binding and output-collection steps as well as the child operations.
Rejecting any pending review cancels the whole run.

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
- 128 expanded steps, including call boundaries.
- A 64 KiB serialized recipe bundle.
- No recursive calls.

Step IDs cannot contain `/` or begin with `@`; those namespaces belong to composition.
Qualified IDs still have the 64-byte limit.

A call cannot use `for_each` or `retry`;
ordinary nodes inside a child retain their iteration and retry rules. Declare each
call explicitly when invoking the same recipe more than once.

Every declared output
must resolve; an output referring to a skipped step fails collection.
Constant and input-only outputs are still collected when all effect steps skip.
A skipped call does not collect its child's outputs.

See [the reusable report example](../../examples/workflows/subworkflow/README.md).
