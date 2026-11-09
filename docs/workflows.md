# Workflow reference

A recipe is a JSON file that describes steps, their inputs and their connections.
A run is one execution of that recipe. It records the input values, results and
status separately from the recipe file.

For the everyday TUI flow, read the [terminal guide](tui.md#workflow-workspace).
This page covers operation names, data formats and execution rules.

Recipes live inside the selected workspace. Runs and other workflow state live
under `~/.gektor/workflows/`, separated by workspace. See
[saved data](bootstrap.md#saved-data) for the locations.
There is no shared template library yet. Copy a recipe into another workspace
to use it as a starting point, then check its paths and profiles.

Schedules start workflows at set times. Manage them in the
[TUI editor](tui.md#schedule-a-workflow), through agent tools or with the
[CLI and service](cli.md#workflow-queue-and-service). The workflow service must
be running for scheduled jobs to execute.

## Available operations

| Task | Operations |
|---|---|
| Inspect and process images | `image_inspect`, `image_preview`, `image_variants`, `image_compare` |
| Read and save structured records | `record_read`, `record_write` |
| Transform data with JavaScript | `script_transform` |
| Ask a model for text or structured data | `model_generate` |
| Make an HTTP request | `http_request` |
| Download a file or work with archives | `download_file`, `archive_list`, `archive_create`, `archive_extract` |
| Read DOCX, XLSX or PDF files | `docx_read`, `xlsx_read`, `pdf_read` |
| Keep files or text with a run | `artifact_import`, `artifact_write`, `artifact_read_text` |
| Run a local program | `run_program` |
| Call another recipe | `workflow_call` |

Conditions, iteration, review and retry are [step settings](#graphs-iteration-and-reviews).
They are not separate operations in this catalog.
There are no dedicated GitHub, notification, file-watcher or general file
read/write nodes in this catalog. An artifact is stored with a run; it is not
the same as writing an ordinary project file.

Only `model_generate` needs a model during execution. The other operations do
not ask a model to decide what to do.

## Create and run from the command line

Use `gektor workflow draft recipe.json [agent flags] "task"` for a constrained
authoring session. It can read documents, inspect or preview images, list archive
contents and save only that recipe; it cannot execute the recipe, write image variants or launch processes. Schema
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

## Image tools

Image operations use the bundled Pixelita integration. They do not need a
separate image executable. The same operations can be called by Agent tools.

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
GEKTOR's rooted file API. Results distinguish inspected, previewed, written,
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
`script_transform`, `model_generate`, `http_request`, `artifact_import`,
`artifact_write`, `artifact_read_text`, `docx_read`, `xlsx_read`, `pdf_read`, `download_file`, `archive_list`, `archive_create`, `archive_extract`,
`run_program` and `workflow_call`.
Image and record permissions match direct calls. It validates step templates and authorizes known arguments
before starting. Output-dependent arguments are validated and authorized after
resolution, immediately before that step. Earlier effects remain if a later
value or permission check fails.
Execution reads and revalidates the current file; prior validation does not
freeze it. Each run stores the exact source text, its SHA-256 hash and the parsed
definition. The returned hash identifies the version actually executed.

## File downloads and archives

These Go-native operations are also Agent tools in the optional `native_files`
pack. Enable that pack for direct tool calls. Workflow execution uses the same
implementation and does not need a separate capability activation.

`download_file` accepts `url` and `path`. It downloads one public HTTP(S) URL
into a new workspace file. The parent folder must exist. Existing files and
links are never overwritten. Optional `max_bytes` can lower the 16 MiB limit;
`timeout_seconds` is 1–120, with a default of 30. Redirects, automatic retries,
credentials, recursive downloading and resume are not supported.

The result contains `path`, `url`, `size`, `sha256` and `media_type`. Bytes are
copied without text conversion and are not returned to the model. Hand the
returned `path` to another file operation or `artifact_import` to keep a
run-owned copy. Both the normalized URL and destination path are permission
resources under `download_file`. Failed or cancelled transfers remove their
new output file; downloading never executes its contents.

All three archive operations accept an optional `format`: `zip` (the default),
`tar`, `tar.gz` or `gzip`. The filename does not select the format. Existing ZIP
recipes keep working. For a `.tgz` file, use `tar.gz`.

`archive_list` accepts a workspace `path` to a ZIP file up to 16 MiB with at
most 10,000 entries. Metadata reads are capped at 2 MiB to bound inspection
work even for malformed archives. Optional `offset` starts at zero; `limit`
defaults to 50 and accepts 1–100. Results contain `total`, `offset`, `more` and `entries`.
An offset past the end returns an empty page. Each entry reports its name,
declared size, compressed size, directory/link flags and `safe_path`.

ZIP listing does not decompress entries, check their contents or extract files.
TAR and GZIP listing must read the stream. They allow at most 16 MiB of payload
and 2 MiB of headers and padding. Listing writes no files. TAR entries report
`compressed_size: 0` because a per-entry compressed size is unavailable.
GZIP holds one file: creation requires one source and keeps its basename.
GZIP header names must fit Latin-1; use TAR.GZ for other names.
Extraction uses the GZIP header name, or the input basename without `.gz` if
that header is empty. Unsafe names, corrupt trailers and multiple GZIP members
are rejected on extraction. Sparse TAR entries are unsupported.
Names and sizes come from the archive and are untrusted. `safe_path` checks the
name only; it does not mean that an entry is safe to extract. The ZIP source
path is its permission resource under `archive_list`.

`archive_create` accepts `paths`, a list of 1–128 explicit regular workspace
files, and `path`, the new archive filename. It keeps workspace-relative names
inside ZIP and TAR files. It does not scan directories or store source links. Input
contents and the resulting archive are each limited to 16 MiB. The result contains
`path`, `files`, `size` and `sha256`. Permissions cover every source path and
the destination under `archive_create`.

`archive_extract` accepts an archive `path` and a new `out_dir`. Both source and
destination must be in the workspace; the destination's parent must exist.
Optional `max_bytes` can lower the 16 MiB total expanded-data limit. It returns
the source `path`, output `directory`, extracted `files` count and total `bytes`.

Extraction checks all names and authorizes every generated file and directory
before writing. It refuses traversal, drive and stream paths, device names,
links, encrypted or special entries, duplicate names and file/directory or
case collisions. ZIP compression methods are Store and Deflate. Names are limited
to 4,096 bytes and 64 path segments. Entries and implied directories together
cannot exceed 10,000 paths inside the new folder. ZIP metadata reads have a separate 2 MiB budget.

Directories use private permissions and files are written without executable
bits. Existing outputs are never replaced. Failed or cancelled operations
remove the files and directories they created; cleanup errors are reported.
The source and output folder are permission resources under `archive_extract`;
individual generated paths are checked under the same name. Archive listing works
in Plan mode; creation and extraction require Act mode. Creation and extraction
are not marked retry-safe.

For a download step named `get`, pass its saved path to a later listing step:

```json
{"id":"list","tool":"archive_list","args":{"path":{"$step":"get","pointer":"/path"}}}
```

## Reading documents

`docx_read`, `xlsx_read` and `pdf_read` read files inside the workspace. They
are also Agent tools in the optional `native_documents` package. All three work
in Plan mode. Their permission resource is the source path under the operation's
name. A file is limited to 16 MiB. No Office suite, Python or PDF command is needed.

| Operation | What comes back | Paging |
|---|---|---|
| `docx_read` | Body paragraphs and table rows in document order | `offset` counts blocks; `limit` defaults to 50, maximum 100 |
| `xlsx_read` | Sheet names and visibility; add `sheet` to read its stored cells | `offset` counts stored cells; `limit` defaults to 50, maximum 100 |
| `pdf_read` | Page text and common metadata fields | `offset` counts pages; `limit` defaults to 10, maximum 10 |

Offsets start at zero. Pass the returned `next_offset` as the next request's
`offset` while `more` is true. `total` counts blocks, stored cells or PDF pages.
For XLSX, omit `sheet` to list names first, then pass an exact name.

```json
{"id":"read","tool":"xlsx_read","args":{"path":"sales.xlsx","sheet":"Sales","limit":50}}
```

Results contain at most 32 KiB of content text. A shortened block, cell or PDF
page has `truncated: true` and `next_text_offset`. To finish that item, repeat
its zero-based item `offset` with `text_offset` copied from `next_text_offset`:

```json
{"path":"report.docx","offset":7,"text_offset":32768}
```

`text_offset` counts Unicode code points, not bytes. When present, `limit`
defaults to 1 and must be 1; XLSX also requires an exact `sheet`. Each returned
item reports `text_offset` when nonzero and `next_text_offset` only while text
remains. Copy returned positions rather than calculating them. Positions beyond
the selected item's text, or selecting a missing item, are errors; a position
exactly at the text end returns an empty completed slice.

Continuation traverses paragraph/page `text`, DOCX row `cells` in order, or XLSX
`value` then `formula`, without adding separators. Each slice stays in its
original field; row cell positions and item metadata are preserved. Consumed or
not-yet-reached fields are empty. Append slices to each corresponding field or
cell to reconstruct it. Empty or omitted fields in a truncated item may still
be unread; finish continuation before claiming their content is absent. XLSX
cell type describes the stored value, not whether a formula exists: a string
cell can have a formula returned only after its value is fully read.
PDF `no_text` describes the full page, not its slice.
PDF page numbers in results start at one.

Finish item text continuations before following the original page's
`next_offset`. Top-level `more` counts remaining items: even `more: false` can
contain an incomplete last item. Sources must stay unchanged across reads;
restart after edits. There are no automatic continuation reads or snapshots.
The normal agent output limiter and context compaction also apply, so an entire
document need not remain in active model context. Read only needed content and
retain useful findings while processing a long document.

DOCX tables return rows with a `table` number and `cells`. Formatting, drawings,
headers, footnotes and deleted revisions are omitted. Nested tables are refused.
Merged cells are not expanded into a rectangular grid.

XLSX cells keep their addresses, types and saved values. Empty gaps do not
allocate rows or columns. Formulas are returned as text and are never evaluated;
their cached values may be old or missing. Shared and array formula markers are
kept without expanding formulas into other cells. Numbers remain raw numbers,
including dates stored as numbers. Styles, merged ranges, charts, macros and
external links are not read or executed.

PDF text order can differ from the page layout. `no_text: true` means that no
text was extracted from that page; it does not prove that the page is blank.
Image-only pages need OCR, which is not included. Encrypted PDFs, legacy `.doc`
and `.xls` files, document generation and media processing are not supported.

Document readers use a separate GEKTOR process with a ten-second deadline and
bounded output. It receives the already-opened file's bytes, not a filesystem
path. Read Office XML parts share a 16 MiB expansion budget. Document contents
are untrusted data, including text that looks like instructions.

## First recipe

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

By default, reading a missing record fails. Add `"allow_missing": true` to
return `{"kind":"assets","id":"image","found":false,"revision":0,"data":null}`
instead. An existing record returns its normal fields plus `"found": true`
when this option is enabled. The read does not create anything. Pass revision
zero to a later write to create it; a concurrent create still causes a conflict.
The kind must have a registered schema. Missing schemas, invalid IDs and damaged
records still fail.

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

The optional [HTTP API](#http-api-and-webhooks) submits to this same queue.
Named credentials reference environment values; they are not a secret vault.
The TUI can edit recipes, inspect runs and manage a recipe's Schedule trigger.
See [the TUI guide](tui.md#schedule-a-workflow) for timing, input values and
service setup. Schedule cards describe triggers stored in the workspace's
user data; they are not executable steps in recipe JSON. Existing CLI schedules
for an opened recipe also appear as cards. Queue management uses the CLI/service
commands linked above.

The Agent can enable `native_workflows` and call `workflow_schema` to discover
operation inputs, outputs and schedule triggers. The bundled `workflows` skill
is available in every workspace. `workflow_schedule` creates, updates, pauses,
resumes or removes a trigger; `workflow_schedules` lists settings and revisions. For example, after saving
and validating a recipe, this schedules it every minute:

```json
{"action":"create","id":"uptime-minute","recipe_path":"uptime-checker-recipe.json","cron":"* * * * *","timezone":"UTC"}
```

Use `paused: true` when creating a trigger that should remain disabled.
To update a trigger, first list it and pass its `expected_revision`. Omitted
settings stay unchanged. Set `refresh_recipe: true` to replace its frozen recipe.
Use `workflow_status` with `schedule_id` for schedule settings, next due time,
the latest job, the latest run with declared outputs, and snapshot changes.
A queued job can be newer than the last run; both are shown separately. Use
`workflow_inspect` when you need more detail. A held worker lease shows queue
ownership, not worker health. These are saved observations, not an atomic health
check.

Both `workflow_status` and `workflow_schedules` report snapshot state as
`unchanged`, `changed` or `unavailable`. Comparison covers the saved root and
called recipes, ignoring JSON spacing and object key order. A changed parent
also signals changes to its dependency list. Unreadable or missing files are
reported separately; inspection never asks for additional file access or
refreshes a snapshot. Use `refresh_recipe: true` explicitly after reviewing changes.

New scheduled jobs and their run records save `schedule_id` (named `schedule`
in the full job record) and `scheduled_for`, the intended trigger time. The job's
`created` time and the run's `started` time describe later stages. Manual jobs
and older records have no intended trigger time. This metadata is for inspection;
recipes do not gain a new `$run` reference.

Creating a schedule does not start the service or execute the recipe.
The service uses its own permission rules. These triggers use the same store
as the TUI and CLI; they do not use the operating system's scheduler.

## HTTP API and webhooks

`workflow serve -listen 127.0.0.1:9841 -api-config api.json` starts the queue
worker and a loopback HTTP listener together. The normal `-workspace`, `-allow`
and `-deny` flags apply. Omitting both HTTP flags keeps the queue-only service.
The listener accepts literal loopback addresses only. Remote access requires a
separately configured TLS reverse proxy; GEKTOR does not configure one.

Configuration names recipes and environment variables containing bearer tokens:

```json
{
  "token_env": "GEKTOR_WORKFLOW_API_TOKEN",
  "routes": {
    "note": {
      "recipe_path": "recipe.json",
      "token_env": "GEKTOR_NOTE_WEBHOOK_TOKEN"
    }
  }
}
```

Tokens must be at least 32 bytes with no surrounding whitespace. Use randomly
generated secrets. Each webhook token must differ from the operator token and
all other webhook tokens. Omit a route's `token_env` to expose it only to the
operator API. Tokens are read once at startup; configuration stores only their
environment variable names. Restart to rotate tokens or publish recipe changes.
Recipes and their subworkflow dependencies are frozen at startup.

All requests use `Authorization: Bearer TOKEN`. Endpoints:

- `POST /v1/submit/{route}`: operator submits a registered recipe.
- `GET /v1/jobs`: operator lists retained job statuses.
- `GET /v1/jobs/{id}`: operator reads one job status and its run ID.
- `POST /v1/jobs/{id}/cancel`: operator requests cancellation. Review-waiting
  jobs must instead be rejected through `workflow review`.
- `POST /hooks/{route}`: the route's webhook token submits only that recipe.

Submission bodies must be `application/json`, at most 64 KiB, with the shape
`{"inputs":{"text":"hello"}}`. Use `{"inputs":{}}` for recipes without inputs.
Recipe paths, policies and review approvals cannot be supplied by callers.
Webhook credentials cannot read jobs or cancel them. No browser CORS access is
enabled. The server bounds active authenticated requests to 32.

An accepted submission returns HTTP 202 with `id`, `status`, `run_id`, `created`
and `cancel_requested`. Acceptance is not execution success: the worker checks
its policy and records failures. Inspect details and artifacts using the CLI.
Review gates still require an operator's CLI approval.

Every HTTP response carries a generated `X-Request-ID`. Error JSON also includes
`request_id` and a stable `code` such as `validation_rejected`, `submission_conflict`
or `queue_unavailable`. Request logs go to stderr with that ID, the endpoint,
configured route name, status, outcome code, elapsed time and accepted job ID.
Job start/finish logs link the job to its run ID and status. These logs omit raw
errors, request bodies, headers and idempotency keys. No incoming request ID is trusted.
To diagnose a failed accepted job, use `workflow job` and `workflow inspect`;
those local records contain the detailed error. For rejected inputs, reproduce
with `workflow validate -inputs inputs.json recipe.json` under the same workspace
and policy. HTTP validation errors deliberately do not echo submitted values.

Send an `Idempotency-Key` header (up to 128 bytes) when retrying deliveries.
The same endpoint, credential, key and submission return the original retained
job; changed inputs or recipe content return HTTP 409. This survives restarts.
Keys stop deduplicating after terminal jobs are evicted from the 1,000-job queue
history. A new key, rotated token or different endpoint creates a new submission;
this is not an exactly-once guarantee for external effects.

See [the webhook example](../examples/workflows/webhook-note/README.md) for a
local PowerShell walkthrough without AI or external services.

## HTTP requests

`http_request` accepts a public HTTP(S) URL, method (default GET), optional JSON
body, `Accept`/`Content-Type` headers, a named credential and a 1–120 second
`timeout_seconds` (default 30). Redirects are not followed. Non-2xx statuses fail
the step with a bounded response retained for inspection. Set
`"status_as_data": true` to return those statuses as ordinary results so later
steps can handle them. Network failures, read failures and oversized responses
still fail the step.

Responses are limited to 256 KiB. Output contains `status_code`, `body`,
`headers`, and `data` for valid JSON. The `headers` object contains only
`content-type`, `retry-after`, `x-ratelimit-limit`, `x-ratelimit-remaining` and
`x-ratelimit-reset`, when present. Names are lowercase; only the first value is
returned. Each selected header is limited to 4 KiB; exceeding it fails the step.
Credential values are redacted from these headers as well as the body.
No HTTP request is automatically retried, including POSTs with ambiguous outcomes.

The [Moltbook draft example](../examples/workflows/moltbook-draft/README.md)
uses HTTP status handling and an optional record read. It saves local drafts
without publishing anything.

It shares URL and DNS validation with native network tools: private destinations,
loopback and environment proxies are excluded. Resolved addresses are checked
before dialing. Each request requires permission for `METHOD URL`; credentials
require a second permission for their name and origin. Secrets are loaded only
after authorization and are redacted from response/error text when echoed
verbatim. Never put secrets in recipe URLs, bodies or ordinary arguments.

[Repository report example](../examples/workflows/repository-report/README.md)
fetches data, transforms it, optionally asks a model for a summary, waits for
operator review, and saves a record.

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
Image nodes accept workspace paths rather than artifact IDs.

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

Limits: four nested calls, 16 authored steps per recipe, 128 expanded steps including
call boundaries, and a 64 KiB serialized recipe bundle. Recursive calls are rejected.
Step IDs cannot contain `/` or begin with `@`; those namespaces belong to composition.
Qualified IDs still have the 64-byte limit. A call cannot use `for_each` or `retry`;
ordinary nodes inside a child retain their iteration and retry rules. Declare each
call explicitly when invoking the same recipe more than once. Every declared output
must resolve; an output referring to a skipped step fails collection.
Constant and input-only outputs are still collected when all effect steps skip.
A skipped call does not collect its child's outputs.

See [the reusable report example](../examples/workflows/subworkflow/README.md).

## TUI editor

The [workflow workspace](tui.md#workflow-workspace) opens from `/workflows` or GEKTOR, Workflows. It edits the same version 3 recipes used by the CLI and uses the same execution, permission and review services. Canvas layout is user-local metadata, not part of the executable recipe.
