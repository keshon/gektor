---
name: pixelita
description: Inspect, optimize, resize, convert and compare image assets with pixelita; visual judgment requires vision.
---

# Image assets

1. Find pixelita on PATH; Windows fallback:
   `D:\Projects\dev\keshon\pixelita\bin\pixelita.exe`.
   Use run_program with literal args; run_shell only for shell syntax. Query
   `pixelita capabilities --json` before assuming operations/flags. Do not install
   or build it automatically. If unavailable, report that blocker and its source:
   https://github.com/keshon/pixelita.
2. Inspect with `pixelita inspect --json <paths>`. Add --measure only for encoding
   estimates; large directories can be expensive. skipped/headers_only is a
   successful metadata inspection.
3. Preview with `pixelita optimize --json <paths>`; it writes nothing. Inspect
   strategy, skips and errors. Add --apply and an explicit --out-dir to perform
   an authorized operation. Default to separate outputs; source replacement,
   deletion and overwriting existing outputs require authorization for those effects.
4. Report measured bytes, dimensions and fidelity; confirm writes from the apply
   result. Preview savings are estimates. Inspect each failed/skipped item, not
   just process exit. JSON contains schema/items/summary; capabilities includes
   build metadata.

- --format webp opts into conversion; --widths 640,1280 opts into resizing.
  Both remain previews without --apply. Generate variants from originals and
  update consuming references when names/formats change.
- Compare with `pixelita compare --json --strict-size original.png optimized.png`.
  PSNR/SSIM measure similarity, not semantic or visual correctness.
- pixelita view produces a preview file, not LLM vision. Read images with
  read_file only when image input is enabled; otherwise claim no visual judgment.
  Metadata/optimization/comparison need no projector or backend switch.
- Keep asset/output paths inside the task workspace. Plan has no shell access:
  describe operations there and execute in Act. For other flags, use
  `pixelita <command> --help`; report missing capabilities rather than guessing.
