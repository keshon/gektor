# Workflows

A workflow is a saved recipe: a versioned JSON list of steps. Gektor can write
it as a normal workspace file, read it later, validate it and run it once.

Use `gektor workflow draft recipe.json [agent flags] "task"` for a constrained
authoring session. It can inspect or preview sources and save only that recipe;
it cannot execute the recipe, write image variants or launch processes. Schema
details are loaded through `workflow_schema`, rather than added to every prompt.
The author must actually save and successfully validate the selected recipe
before its run can report success. Verification revalidates the current file
with the same input arguments, so a later edit cannot reuse stale validation.
A save alone checks only the definition's structure. Validation does not establish
image compatibility or execute any operation. A draft needing unknown input
values remains available on disk but cannot pass verification yet.

Run `gektor workflow validate [flags] recipe.json` and
`gektor workflow run [flags] recipe.json` to execute without a model.
`-inputs inputs.json` supplies a workspace JSON object. See the
[CLI reference](cli.md#saved-workflows) for permissions, flags and drafting limits.

## Build

Gektor pins the Pixelita public Go API to a published Git revision in go.mod.
No sibling checkout or separately installed image executable is required.

```powershell
go build -o bin/gektor.exe ./cmd/gektor
```

This leaves the existing root executable unchanged.

## Tools

Enable `native_images` through `enable_capability` to load these schemas:

| Tool | Purpose | Writes |
|---|---|---|
| `image_inspect` | Format, dimensions, metadata | None |
| `image_preview` | Preview optimization or explicit width variants | None |
| `image_variants` | Create explicit width variants | New output directory |
| `image_compare` | PSNR and SSIM for two images | None |

Inputs are explicit files, up to 16 per call, 64 MiB and 40 million pixels each. Variant widths are
explicit, up to eight per call. Formats are `keep` and `webp`. The output directory
must not exist. Source replacement, deletion and destination overwrite are not
exposed. Image operations use staged copies; workspace access goes through
Gektor's rooted file API. Results distinguish inspected, previewed, written,
blocked and failed outcomes. Each result includes source SHA-256 hashes before
and after processing; `unchanged: true` means the bytes matched. An unavailable
after-hash is reported as an error, not evidence of preservation. These checks
describe the operation, not subsequent edits by other processes.
Metrics do not establish visual quality.

Unsupported metadata, including embedded ICC profiles on resize, returns a
blocked tool result with the actual reason. A blocked preview stops the recipe
before writing. `keep` means keep the file format; it does not add colour-profile
conversion support. Do not remove profiles merely to make a recipe succeed.

Enable `native_workflows` for schema lookup, validation, execution, run inspection
and structured records. `workflow_validate` and `workflow_run` accept
`{"recipe_path":"recipe.json"}` with an optional `inputs` object for version 2.
They read that saved workspace file, up to
64 KiB, and reject unknown fields. Validation
checks structure, supported operations, argument schemas, IDs and path scope.
It does not predict whether future input files exist or encoding will succeed.
The runner permits these four image operations plus `record_read` and
`record_write`, with the same permissions as direct calls. It validates step templates and authorizes known arguments
before starting. Output-dependent arguments are validated and authorized after
resolution, immediately before that step. Earlier effects remain if a later
value or permission check fails.
Execution reads and revalidates the current file; prior validation does not
freeze it. Each run stores the exact source text, its SHA-256 hash and the parsed
definition. The returned hash identifies the version actually executed.

## First recipe

Ask Gektor:

> Create a recipe for input.png: inspect it, preview 320px and 800px variants,
> then write those variants to a new directory called web-images. Save the
> recipe as recipe.json, validate that file, then run it once and report the saved record path.

```json
{
  "version": 1,
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
can use `workflow_inspect` with `{"run_id":"..."}`. Inspection reports
`interrupted` when a persisted running run no longer holds its OS process lock;
the recorded status remains unchanged. A crash can leave partial step effects.
Writes use a synced temporary file and rename; this is not a power-loss recovery
or workspace rollback guarantee. Legacy `.gektor/workflow-runs` files remain
readable with normal file tools and are not imported into this store.

Steps run sequentially and stop on failure; remaining steps are marked skipped.
Inspect outputs before deciding to rerun;
the runner does not retry or resume automatically. Earlier completed writes
remain if a later step fails. Each rerun needs fresh output directories.

The tool response contains step outcomes, item results and source hashes; full
outputs and recipe source remain in the run record. Start a fresh Gektor session
when an update changes tool schemas: old saved activations may be incompatible.

## Inputs and data flow

Version 1 remains supported. Version 2 adds typed inputs and references to earlier
step outputs. For example:

```json
{
  "version": 2,
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
Reference-like strings such as `"{$input:source}"` are rejected in version-2
image paths; use `{"$input":"source"}` as the entire value instead.

`pointer` uses JSON Pointer syntax: `/items/0/output` selects a field in an
earlier result; `~1` escapes `/` and `~0` escapes `~`. An omitted or empty pointer
selects the whole result. Forward, self and unknown references are rejected.
Missing output fields and wrong resolved argument types fail the dependent step;
remaining steps are skipped. Validation checks templates, not future output
values or file existence.

There are at most 32 inputs, 64 KiB of bound inputs or resolved step arguments,
64 levels of argument nesting and 1 MiB of output per step. Oversized output
fails the run and is truncated in the record. Run records retain the original
definition, bound inputs and each step's resolved arguments. The user store is
outside ordinary workspace file access, but programs running as the operator
can still change it. Do not pass secrets as inputs.

## Structured records

Records hold bounded JSON objects, such as image metadata or a draft for later
review. Each workspace has its own record kinds and registered JSON schemas.
Register a workspace schema file explicitly:

```powershell
gektor workflow register-schema -workspace . assets assets.schema.json
```

For example, `assets.schema.json`:

```json
{"type":"object","properties":{"width":{"type":"integer","minimum":1}},"required":["width"],"additionalProperties":false}
```

The schema must declare `type: object`; schema and data limits are 64 KiB each.
Local schema references are supported; loading external schemas is disabled.
A kind's schema is immutable. Register a new kind to change its shape.
The agent can read a registered schema through
`workflow_schema {"record_kind":"assets"}`. No schema-registration tool is
exposed; registration is an explicit CLI operation.

Direct tools and recipe steps use the same arguments:

| Tool | Arguments | Result |
|---|---|---|
| `record_write` | `{"kind":"assets","id":"image","expected_revision":0,"data":{"width":320}}` | Entry with revision 1 |
| `record_read` | `{"kind":"assets","id":"image"}` | Entry with `data`, `revision`, schema hash and update time |

Revision 0 creates; updates require the current revision. Concurrent or stale
updates fail without overwriting. Workflow references can pass `/revision` from
a read to a later write. A repeated create fails rather than silently replacing
data. Kind and ID use 1–64 lowercase ASCII letters, digits, `_` or `-`; Windows
device names and the ID `schema` are reserved. Permission resources are `kind/id`,
for example `-deny "record_write=assets/*"`.

Record data is validated against the registered schema immediately before writing.
`workflow_validate` checks operation arguments and reference templates; it does
not establish that a record exists, its data matches a schema, or a revision is
still current. Each write is atomic individually; there is no transaction across
steps and no automatic retry after conflicts. Records live under
`~/.gektor/workflows/<workspace-hash>/records/<kind>/`.

## Remaining plan

1. Exercise manual image recipes in real tasks and refine the output contract.
2. Exercise the constrained authoring role, then add conditions with defined
   skip behavior when real recipes need them.
3. Add bounded model steps and explicit review queues on top of structured records.
4. Add scheduled runs with pause, run-now, cancellation and overlap prevention.
5. Add approval-aware external actions and reconcile ambiguous outcomes before
   retrying. Start with a draft-only social workflow.

Scheduling, social publishing and browser changes are outside this slice.
Version-2 recipes support data references but have no model steps or branches.
