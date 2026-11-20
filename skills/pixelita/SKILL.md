---
name: pixelita
description: Inspect, preview, resize and compare images with GEKTOR's native Pixelita tools; save reusable image recipes.
---

# Native image tools

1. Enable `native_images`. Use `image_inspect` for metadata, `image_preview` for
   write-free estimates, `image_optimize` for original-size optimization or WebP
   conversion, and `image_variants` for explicit widths in a new out_dir,
   and `image_compare` for pixel fidelity, not object identity. Use `image_attachments`
   for pasted pictures, `image_view` for pixels/crops and `image_download` for references.
   Load `visual-investigation` for identification or visual comparison.
2. For reusable recipes enable `native_workflows`, then call `workflow_schema`.
   Use the current version 4 format with name, steps, optional inputs and outputs.
   Each step has id, tool and args. Validate and run
   that saved file with `recipe_path`. Validation checks structure, not image
   compatibility. The runner stops on a failed or blocked operation.
3. Preserve originals. For ICC-blocked WebP conversion or resizing, preview with
   `color_profile: "srgb"` to convert supported RGB matrix/TRC profiles. Use the
   same setting when writing. Unsupported profiles and animation remain blocked;
   report the actual reason. For same-size JPEG optimization, `format: "keep"`
   without widths retains the profile and original pixels. Do not strip profiles
   or probe CLI alternatives.
   If native capabilities are absent, report that this build lacks the integration.
4. Report written outputs separately from previews. Claim source preservation
   only from matching before/after hashes. PSNR/SSIM are measurements, not visual
   approval. Read images with read_file for visual judgment only when image input
   is enabled. No executable lookup, installation or backend switch is needed.
