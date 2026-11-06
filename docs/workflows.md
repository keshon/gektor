# Workflows

A workflow is a saved recipe: a versioned JSON graph of steps. Gektor can write
it as a normal workspace file, read it later, validate it and run it directly or through the local queue.

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
`gektor workflow run [flags] recipe.json` to execute without an agent session. Only `model_generate` constructs a model client.
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
`{"recipe_path":"recipe.json"}` with an optional typed `inputs` object. Only recipe version 3 is supported.
They read that saved workspace file, up to
64 KiB, and reject unknown fields. Validation
checks structure, supported operations, argument schemas, IDs and path scope.
It does not predict whether future input files exist or encoding will succeed.
The catalog includes these four image operations, `record_read`, `record_write`,
`script_transform`, `model_generate` and `http_request`. Image and record permissions match direct calls. It validates step templates and authorizes known arguments
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
can use `workflow_inspect` with `{"run_id":"..."}`. Inspection reports
`interrupted` when a persisted running run no longer holds its OS process lock;
the recorded status remains unchanged. A crash can leave partial step effects.
Each completed step is saved before the next step is prepared. If saving fails,
execution stops; an effect may have happened even when its result could not be
persisted. Writes use a synced temporary file and rename; this is not a power-loss recovery
or workspace rollback guarantee. Legacy `.gektor/workflow-runs` files remain
readable with normal file tools and are not imported into this store.

Steps run sequentially and stop on failure; remaining steps are marked skipped.
Inspect outputs before deciding to rerun;
the runner does not replay interrupted effects automatically. Earlier completed writes
remain if a later step fails. Each rerun needs fresh output directories.

The tool response contains step outcomes, item results and source hashes; full
outputs and recipe source remain in the run record. Start a fresh Gektor session
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

## Run a record recipe without AI

Save this schema as `notes.schema.json`:

```json
{"type":"object","properties":{"text":{"type":"string"}},"required":["text"],"additionalProperties":false}
```

Save this recipe as `note.json`:

```json
{
  "version": 3,
  "name": "Save and read a note",
  "inputs": {"text": {"type": "string", "default": "First workflow"}},
  "steps": [
    {"id": "save", "tool": "record_write", "args": {
      "kind": "notes", "id": "first", "expected_revision": 0,
      "data": {"text": {"$input": "text"}}
    }},
    {"id": "read", "tool": "record_read", "args": {
      "kind": "notes", "id": {"$step": "save", "pointer": "/id"}
    }}
  ]
}
```

From that workspace:

```powershell
gektor workflow register-schema notes notes.schema.json
gektor workflow validate note.json
gektor workflow run -yes -allow "record_write=notes/*" note.json
gektor workflow record notes first
gektor workflow runs
gektor workflow inspect <run-id>
```

No model configuration or TUI is involved. The record survives process restarts.
Running this create recipe again fails with a revision conflict; it does not
replace the saved note. To update it, use its current revision in
`expected_revision`, or reference the revision returned by a preceding read step.

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

## JavaScript

`script_transform` takes `code` (a function body) and JSON `input`. Return a JSON
value, for example `return {total: input.items.reduce((n, x) => n + x.price, 0)};`.
There is no Node.js runtime, package loading, filesystem, network or environment
API. Use a separate node for effects. JavaScript numbers use IEEE-754; preserve
large identifiers as strings.

Scripts run in a separate Goja worker with a two-second evaluation deadline,
five-second process deadline, a 128 MiB soft Go heap target and OS memory limits
(256 MiB process memory on Windows; 2 GiB address space on Unix). Input/code are
limited to 64 KiB each; output is limited to 1 MiB. These are resource boundaries,
not a general OS sandbox for hostile native code.

## Model nodes

`model_generate` takes a workspace `profile`, explicit `prompt`, optional JSON
`input`, optional `output_schema`, `max_tokens` and `timeout_seconds`. It makes
one model request with no agent tools or hidden chat history. Output contains
`text`, `model`, `usage`, and `data` when a schema is supplied. Invalid structured
output and truncated responses fail the node. Profile output/context limits
apply; the default node timeout is 120 seconds. One model node runs at a time
within a process. Separate service processes have separate limits.

The request carries both schema encodings used by the existing providers.
KoboldCpp supports a stringified schema in the grammar field from
[v1.90.2](https://github.com/LostRuins/koboldcpp/releases/tag/v1.90.2).

## Background execution

See [the CLI reference](cli.md#workflow-queue-and-service) for submit, serve,
schedules, cancellation and review commands. Queue entries freeze recipe bytes
and inputs. Referenced workspace files, model profiles and external services
remain live resources; a snapshot does not freeze their contents.

The current backend has no HTTP listener, webhook ingress,
credential vault or visual editor. Named credentials currently reference
environment values; they are not a secret vault. Those remain separate implementation
steps. No TUI node-editor behavior is introduced here.

## HTTP requests

`http_request` accepts a public HTTP(S) URL, method (default GET), optional JSON
body, `Accept`/`Content-Type` headers, a named credential and a 1–120 second
`timeout_seconds` (default 30). Redirects are not followed. Non-2xx statuses fail
the step with a bounded response retained for inspection. Responses are limited
to 256 KiB; output contains `status_code`, `body`, and `data` for valid JSON.
No HTTP request is automatically retried, including POSTs with ambiguous outcomes.

It shares URL and DNS validation with native network tools: private destinations,
loopback and environment proxies are excluded. Resolved addresses are checked
before dialing. Each request requires permission for `METHOD URL`; credentials
require a second permission for their name and origin. Secrets are loaded only
after authorization and are redacted from response/error text when echoed
verbatim. Never put secrets in recipe URLs, bodies or ordinary arguments.

[Repository report example](../examples/workflows/repository-report/README.md)
shows fetch → transform → optional model summary → operator review → record.
## CLI review guidance

When `workflow run` or `workflow review` pauses for review, stderr shows inspection,
approval and rejection commands with the actual run ID and pending step. Commands
retain the workspace and existing allow/deny rules; approval does not grant additional
permissions. Stdout remains JSON. Use the run ID, not a step name, for `inspect` and
`review`; `workflow runs -workspace <dir>` lists runs for that workspace.

## Run artifacts

Artifacts store file bytes outside the workspace, under the producing run. Their
metadata includes `id`, `run_id`, `step_id`, `name`, `media_type`, `size`, `sha256`
and `created`. Names are labels, not filesystem paths. MIME type is detected from
the bytes. Artifact IDs only resolve within the current run; no cross-run lookup
is exposed to nodes.

- `artifact_import`: copy a regular workspace `path`. Its permission resource is
  the normalized workspace-relative path under `artifact_import`.
- `artifact_read_text`: read an artifact `id` as UTF-8 text, rejecting binary data
  and files larger than 32 KiB. Returns `text` and `artifact` metadata. Its permission
  resource is the ID under `artifact_read_text`.
- `artifact_write`: save explicit `name` and `text`; its permission resource is
  the name under `artifact_write`. Returns metadata, including the new ID.

Use `{"$step":"capture","pointer":"/id"}` to pass an imported artifact to
another node. Text still obeys ordinary recipe argument and output limits. Binary
files can be captured and exported, but `artifact_read_text` is not a binary decoder.
Existing image nodes still accept workspace paths; this step does not change their
input contract.

Each artifact is limited to 16 MiB; a run may retain 256 artifacts and 256 MiB total.
Writes publish complete data and metadata together. Reads and exports verify size
and checksum. Import and write nodes do not permit automatic retries, because another
attempt could create a second artifact. Completed artifacts survive review pauses
and service restarts. Failed steps may leave published artifacts as execution evidence.

Use `workflow artifacts` and `workflow export-artifact` to retrieve files. Explicit
`workflow prune-artifacts -older-than 168h` removes artifacts from terminal runs
finished more than seven days ago. It preserves run history and excludes active,
interrupted and waiting-review runs. References in old run history remain as evidence
after pruning; their payloads are no longer available.

See [the log report example](../examples/workflows/log-report/README.md) for a complete
AI-free file pipeline with review and export.

## Local programs

`run_program` executes a native executable with literal `program` and `args`.
`workdir` defaults to the workspace root and must name a directory inside it.
`timeout_seconds` defaults to 60, with a range of 1–600. Optional `stdin_artifact`
feeds captured bytes from the current run to stdin; otherwise stdin is closed.
There is no automatic shell expansion. On Windows, `.cmd`, `.bat` and `.ps1`
require an explicitly named interpreter. The node is finite; it does not launch
a background service.

Permissions use the existing `run_program` identity and the resource spelling
`program arg1 arg2`. This is a permission/display string, never an executed shell
command. The normal policy applies: defaults are permissive, explicit denies and
destructive-command prompts are honored, and unattended execution denies prompts.
An allow rule is not an allowlist for every other program. Use policy deny rules
when restricting a service. Review gates remain separate from permissions.

The child inherits the environment with the same credential-name scrubbing used
by native agent tools. This is not a sandbox: programs and explicit interpreters
run with the user's OS permissions and can access files or network services.
Workspace confinement applies to selecting `workdir`, not to everything the child
can do. Program files and PATH resolution remain live resources.

Results contain `exit_code`, `truncated`, and `stdout`/`stderr` artifact metadata.
Nonzero exits, startup errors, deadlines, cancellation and output overflow fail
the step. Each stream retains at most 1 MiB; excess output stops the process and
sets `truncated: true`. Capture is byte-preserving, including binary output. Tree
cleanup uses the shared Windows job / Unix process-group implementation, with
bounded pipe draining. Diagnostics are persisted with a separate five-second
deadline even after cancellation. Artifact storage failures also fail the step.

No automatic retries are allowed: a failed process may already have changed files
or contacted a service. Stdout/stderr are artifacts; other generated workspace files
can be captured by a subsequent `artifact_import` step after successful execution.
See [the local program example](../examples/workflows/process-report/README.md).
