---
name: figcraft
description: Create or edit editable UI and website mockups with the Figcraft CLI; export and check PNG previews.
---

# Figcraft mockups

1. Use `run_program` with `program` and literal `args`. Resolve `figcraft-cli`
   on the current host's PATH; use `system_info` for unknown OS/environment.
   On Windows only, also check `C:\Program Files\ArtCraft\FigCraft\figcraft-cli.exe`.
   On Linux, use the installed Linux executable, without `.exe`; do not reuse
   Windows paths. If absent, ask for its location rather than installing it.
   Run the resolved CLI's `--help` once. Plan has no shell execution: build in Act.
2. Read the target project's README and relevant assets. Decide canvas size,
   content, spacing, typography and palette before writing layers. Keep the
   requested mockup scope; creating a mockup does not require a web server.
3. Read [references/cli.md](references/cli.md) before the first build, through
   `load_skill` with `id="figcraft", resource="references/cli.md"`.
   It contains an executable two-pass example. Use it instead of probing ID syntax.
4. Use `write_file` for UTF-8 command JSON inside the workspace. Create frames
   first, save a base `.figcraft`, and read each returned `result.id`. Then use
   those numeric IDs as `parent` in content commands. Run content with
   `--in base.figcraft --out mockup.figcraft`. IDs are document-local, not names.
   **Do not invent `as`, `$page`, `${page}`, or result-reference syntax.**
5. Build with `frame.create`, `node.create` and `text.create`. Query an unfamiliar
   command using `commands --filter TEXT --json` before using it. Do not dump the
   entire catalog or guess presets/command names. Explicit width/height avoids
   preset discovery. Prefer `props.fill: "#RRGGBB"` for a solid color.
6. Check the native exit code and every command result. `ok:false` or
   `rolledBack:true` means that build failed; fix the reported cause. A CLI
   `wrote ...` message on stderr can be success. After two failures of one
   operation, return to its documented schema or report the blocker; do not
   keep trying spellings. After a successful probe, build the requested mockup.
7. Export each requested frame with `export ... --node FRAME_NAME -o preview.png`.
   Run `info ... --json` to confirm frames/dimensions/missing fonts. Keep the
   same `--no-system-fonts` setting in build and export when using bundled fonts.
   Read the PNG with `read_file` when image input is enabled. Check clipping,
   overlaps, wrapping, contrast and spacing; fix and export again if needed.
   With text-only input, report structural checks without claiming visual QA.
8. Deliver the `.figcraft`, PNG preview(s), and reproducible command JSON or
   generator. Name the actual files. Distinguish a static mockup from working
   links, responsive HTML, or an implemented website.

- A fresh CLI process has fresh state unless `--in` is supplied. Reuse actual
  returned IDs when editing; do not assume allocation sequences in existing files.
- Coordinates are relative to the parent frame. Set text width, font size and
  line height intentionally. Keep text/shapes as native layers for editability.
- `read_file` is workspace-scoped. Prefer CLI help for installed program details;
  an authorized external install can be inspected with `run_shell` when needed.
  Write outputs inside the task workspace; do not install or launch services.
- Windows PowerShell `Out-File -Encoding utf8` can add a BOM rejected by Figcraft.
  Prefer `write_file`. Do not merge stderr into JSON or pipe native commands
  through formatting/filtering when checking success. See the reference for a
  shell examples for the current OS.
