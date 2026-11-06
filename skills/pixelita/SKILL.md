---
name: pixelita
description: Inspect, preview, resize and compare images with Gektor's native Pixelita tools; save reusable image recipes.
---

# Native image tools

1. Enable `native_images`. Use `image_inspect` for metadata, `image_preview` for
   write-free estimates, `image_variants` for explicit widths in a new out_dir,
   and `image_compare` for two files. Use the published argument schemas.
2. For reusable recipes enable `native_workflows`. Save JSON with only version,
   name and steps for version 1; version 2 also allows typed inputs and references
   to earlier outputs. Each step has id, tool and args. Validate and run
   that saved file with `recipe_path`. Validation checks structure, not image
   compatibility. The runner stops on a failed or blocked operation.
3. Preserve originals. Report unsupported_metadata as a capability limitation;
   keep/apply/retry cannot bypass it. Do not strip profiles or probe CLI alternatives.
   If native capabilities are absent, report that this build lacks the integration.
4. Report written outputs separately from previews. Claim source preservation
   only from matching before/after hashes. PSNR/SSIM are measurements, not visual
   approval. Read images with read_file for visual judgment only when image input
   is enabled. No executable lookup, installation or backend switch is needed.
