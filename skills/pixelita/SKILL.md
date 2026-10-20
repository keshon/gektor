---
name: pixelita
description: Inspect, optimize, resize, convert and compare image assets with the pixelita CLI. Read for image size, format or compression tasks; visual interpretation still requires a vision-capable model.
---

# Image assets via run_shell

1. Locate `pixelita` on PATH and run `pixelita capabilities --json` to check
   the installed build and supported operations. On this Windows setup the
   local fallback is `D:\Projects\dev\keshon\pixelita\bin\pixelita.exe`.
   Quote the executable path and paths containing spaces. Do not install or
   build anything automatically if the executable is missing.
   If neither PATH nor the local fallback provides the binary, tell the user
   pixelita is unavailable and link its source and build instructions:
   https://github.com/keshon/pixelita. Stop pixelita-specific work there.
2. Start with `pixelita inspect --json <paths>`. Add `--measure` only when
   actual encoding estimates are needed; a large directory can be expensive.
   `skipped` with code `headers_only` is a successful metadata inspection.
3. Preview a requested optimization with `pixelita optimize --json <paths>`.
   This does not write results. Read the selected strategy, skips and errors.
4. Apply the requested operation with `--apply` and an explicit `--out-dir`.
   Use a separate output directory by default. Source replacement, deletion
   and existing-output overwrites require authorization for those effects.
5. Report measured byte savings, dimensions and fidelity metrics. Confirm
   written outputs from the apply result; preview savings are estimates.

Use the existing `run_shell`; there is no dedicated image tool in TARS.
Plan has no shell access: describe the operation there and execute it in Act.
Run commands from the task workspace and keep asset/output paths within it.

## Commands

Replace `pixelita` below with the quoted full executable path when it is not
on PATH. These examples use the canonical CLI, not the separate `img-*` tools.

```text
pixelita inspect --json public/images
pixelita optimize --json --out-dir public/images-optimized public/images
pixelita optimize --apply --json --out-dir public/images-optimized public/images
pixelita optimize --json --format webp --out-dir public/images-webp public/images
pixelita optimize --json --widths 640,1280 --out-dir public/images-sized public/images
pixelita compare --json --strict-size original.png optimized.png
```

- `--format webp` opts into format changes; `--widths` opts into resizing.
  Add `--apply` only when ready to write the previewed operation.
- Generate variants from originals. When changing formats or filenames,
  update consuming references; generating files alone does not update a site.
- JSON carries `schema`, `items`, `summary`, item statuses and metrics.
  Capabilities also reports build metadata. Read `failed`/`skipped` outcomes
  rather than treating an exited process as proof that every file changed.
- PSNR/SSIM describe measured similarity, not semantic or visual correctness.
  Without vision, do not claim to read screenshots, judge layout, or see
  artifacts. `pixelita view` produces a preview file; it does not give a
  text-only model sight. Do not read binary images with `read_file`.
- Do not add a projector to a text-only model or switch backends just to run
  these operations. Inspecting, optimizing and comparing need no LLM vision.
- Use `pixelita <command> --help` for flags beyond these examples. Stop and
  report a missing capability instead of guessing flags or changing tools.
