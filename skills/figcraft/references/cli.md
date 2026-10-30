# Tested CLI recipe

Use `load_skill(id="figcraft", resource="references/cli.md")` to read this file.
The command JSON and CLI arguments below are shared across platforms. Resolve the
executable on the current host; paths below are workspace-relative. Create `mockup/`
first. This recipe was exercised on Windows; Linux execution has not been tested here.

## Tool calls and discovery

Gektor `run_program` arguments (no shell quoting inside the strings):

```json
{"program":"figcraft-cli","args":["--help"]}
```

This example assumes the executable is on PATH. Otherwise use its discovered
absolute path in `program`, still without shell quotes. Discover with `run_shell`:

- Linux/POSIX shell: `command -v figcraft-cli`.
- Windows/PowerShell: `Get-Command figcraft-cli -CommandType Application -ErrorAction SilentlyContinue`.
  If absent, check `C:\Program Files\ArtCraft\FigCraft\figcraft-cli.exe` with
  `Test-Path -LiteralPath`. This is a Windows fallback, not a portable default.

If no executable is found, request the install location. Do not guess Linux
package paths, invoke a Windows binary on Linux, or install software automatically.
Use the resolved executable for every call; the `args` arrays stay the same.

For an unfamiliar property, use the same tool with args:

```text
["commands", "--filter", "node.set", "--json"]
```

The command catalog describes schemas, not layer instances. To inspect a layer,
use a command script containing `node.get` with its numeric `id`.

## Pass 1: create frames and obtain IDs

Write `mockup/base.json` with `write_file`:

```json
[
  {"command":"frame.create","params":{"name":"Desktop","width":1440,"height":1000,"props":{"fill":"#F4F3ED","clip":true}}}
]
```

Run with `run_program` args:

```text
["run", "mockup/base.json", "--out", "mockup/base.figcraft", "--no-system-fonts"]
```

The tested fresh document returns:

```json
{"command":"frame.create","ok":true,"result":{"id":2},"step":0}
```

Read the actual result; `2` below is an example, not a global frame identifier.
For several canvases, create all frames in this pass and record each returned ID.

## Pass 2: add the design to the saved document

Write `mockup/content.json`. Substitute the frame ID from pass 1 for each `parent`.
This tiny example verifies the interface; replace its copy and add the layers
needed for the requested design rather than delivering it as a finished website.

```json
[
  {"command":"node.create","params":{"type":"rectangle","name":"Accent","parent":2,"x":80,"y":80,"width":48,"height":6,"props":{"fill":"#E95A28"}}},
  {"command":"text.create","params":{"name":"Heading","parent":2,"text":"Your code. Your model.","x":80,"y":130,"width":1120,"props":{"fontFamily":"Inter","fontStyle":"SemiBold","fontSize":72,"lineHeight":82,"fill":"#20221F","textAutoResize":"height"}}},
  {"command":"text.create","params":{"name":"Description","parent":2,"text":"A coding agent for the models you run.","x":80,"y":252,"width":900,"props":{"fontFamily":"Inter","fontSize":22,"lineHeight":32,"fill":"#696C63","textAutoResize":"height"}}},
  {"command":"node.create","params":{"type":"rectangle","name":"Primary button","parent":2,"x":80,"y":340,"width":200,"height":56,"props":{"fill":"#E95A28","cornerRadius":4}}},
  {"command":"text.create","params":{"name":"Button label","parent":2,"text":"Get started","x":100,"y":355,"width":160,"props":{"fontFamily":"Inter","fontStyle":"SemiBold","fontSize":18,"lineHeight":26,"fill":"#FFFFFF","textAutoResize":"height"}}}
]
```

Run args:

```text
["run", "mockup/content.json", "--in", "mockup/base.figcraft", "--out", "mockup/mockup.figcraft", "--no-system-fonts"]
["info", "mockup/mockup.figcraft", "--json"]
["export", "mockup/mockup.figcraft", "-o", "mockup/desktop.png", "--node", "Desktop", "--scale", "1", "--no-system-fonts"]
```

These are three separate invocations. Export a named frame, rather than a page
containing several boards. Exporting to a directory can produce several files.
`info` reports frame dimensions and missing fonts; it does not prove visual quality.

For edits, use `node.set` with the actual `id` and properties at the top level:

```json
[{"command":"node.set","params":{"id":2,"fill":"#FFFFFF"}}]
```

Apply with `run changes.json --in existing.figcraft --out revised.figcraft`.
Use a separate output until replacement is intended. For nested groups/components,
inspect the relevant schemas; do not extrapolate numeric allocation from this sample.

## Shell fallbacks and errors

Prefer `run_program`. When shell logic is necessary, preserve the native status:

Linux/POSIX shell, when the CLI is on PATH (otherwise quote its resolved path):

```sh
exec figcraft-cli run mockup/content.json --in mockup/base.figcraft --out mockup/mockup.figcraft --no-system-fonts
```

Windows/PowerShell, using the installation fallback:

```powershell
& 'C:\Program Files\ArtCraft\FigCraft\figcraft-cli.exe' run mockup/content.json --in mockup/base.figcraft --out mockup/mockup.figcraft --no-system-fonts
$figcraftExit = $LASTEXITCODE
exit $figcraftExit
```

Do not append a successful `Get-Content`, `Get-ChildItem` or `Select-String` and
mistake its success for Figcraft's. Do not pipe `2>&1` into `ConvertFrom-Json`:
stderr includes human-readable status. Native stdout from `run` is JSON lines,
one result per command, not one enclosing JSON array. A generator should capture
stdout/stderr separately, check exit code, and inspect each result's `ok` field.

Prefer `write_file` on either OS. If PowerShell-generated JSON is necessary,
write UTF-8 without BOM:

```powershell
[IO.File]::WriteAllText($jsonPath, $jsonText, [Text.UTF8Encoding]::new($false))
```

Known failures: unknown presets, nonnumeric parent references, and BOM-prefixed
JSON. Correct these directly; no alias syntax is needed to finish a mockup.
