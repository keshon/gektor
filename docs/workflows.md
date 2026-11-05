# Image workflows

A workflow is a saved recipe: a versioned JSON list of steps. Gektor can write
it as a normal workspace file, read it later, validate it and run it once.

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

Enable `native_workflows` for `workflow_validate` and `workflow_run`. Both accept
only `{"recipe_path":"recipe.json"}`. They read that saved workspace file, up to
64 KiB, and reject unknown fields. Validation
checks structure, supported operations, argument schemas, IDs and path scope.
It does not predict whether future input files exist or encoding will succeed.
The runner permits only these four image operations, with the same permissions
as direct calls. It validates and authorizes every step before starting.
Execution reads and revalidates the current file; prior validation does not
freeze it. Each run stores the exact source text, its SHA-256 hash and the parsed
definition. The returned hash identifies the version actually executed.

## First recipe

Ask Gektor:

> Create a recipe for input.png: inspect it, preview 320px and 800px variants,
> then write those variants to a new directory called web-images. Validate the
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

Each run saves its definition, digest, per-step outputs and status under
`.gektor/workflow-runs/<run-id>/run.json` in the workspace. Steps run sequentially
and stop on failure; remaining steps are marked skipped. Process interruption
mid-step leaves a running record. Inspect outputs before deciding to rerun;
the runner does not retry or resume automatically. Earlier completed writes
remain if a later step fails. Each rerun needs fresh output directories.

The tool response contains step outcomes, item results and source hashes; full
outputs and recipe source remain in the run record. Start a fresh Gektor session
when an update changes tool schemas: old saved activations may be incompatible.

## Remaining plan

1. Exercise manual image recipes in real tasks and refine the output contract.
2. Add explicit inputs and step-output references, then conditions with defined
   skip behavior when real recipes need them.
3. Add SDA-style typed records for drafts, review queues and bounded persistent
   notes. Reuse the prototype's ideas rather than importing its application server.
4. Add scheduled runs with pause, run-now, cancellation and overlap prevention.
5. Add approval-aware external actions and reconcile ambiguous outcomes before
   retrying. Start with a draft-only social workflow.

Scheduling, social publishing and browser changes are outside this slice.
Recipes currently contain static arguments and no model steps.
